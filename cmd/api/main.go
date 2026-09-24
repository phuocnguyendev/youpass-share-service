// Composition root: nơi DUY NHẤT biết mọi implementation cụ thể và nối chúng với nhau.
//
//	domain ← usecase ← adapter (pgstore, redisstore, httpapi, token) ← infrastructure / main
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/httpapi"
	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/pgstore"
	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/redisstore"
	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/token"
	"github.com/phuocnguyendev/youpass-share-service/internal/infrastructure/config"
	"github.com/phuocnguyendev/youpass-share-service/internal/infrastructure/datastore"
	"github.com/phuocnguyendev/youpass-share-service/internal/infrastructure/logger"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/submission"
	"github.com/phuocnguyendev/youpass-share-service/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(cfg.Env)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	/* ---------- Infrastructure ---------- */
	db, err := datastore.ConnectPostgres(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := datastore.Migrate(ctx, db, migrations.FS, log); err != nil {
		return err
	}

	rdb, err := datastore.ConnectRedis(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, log)
	if err != nil {
		return err
	}
	defer rdb.Close()

	/* ---------- Adapters (output) ---------- */
	linkRepo := pgstore.NewLinkRepository(db)
	submissionRepo := pgstore.NewSubmissionRepository(db)
	content := redisstore.NewCachedContentReader(submissionRepo, rdb, log) // decorator: cache trước DB
	linkCache := redisstore.NewLinkCache(rdb, log)
	views := redisstore.NewViewCounter(rdb, linkRepo, log, cfg.FlushInterval)
	limiter := redisstore.NewRateLimiter(rdb)
	tokens := token.NewManager(cfg.JWTSecret, cfg.JWTTTL)

	/* ---------- Use cases ---------- */
	shareUC := share.NewService(share.Deps{
		Links:   linkRepo,
		Cache:   linkCache,
		Content: content,
		Views:   views,
		BaseURL: cfg.ShareBaseURL,
		Logger:  log,
	})
	submissionUC := submission.NewService(submissionRepo, content)

	/* ---------- Adapter (input): HTTP ---------- */
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	router, err := httpapi.NewRouter(httpapi.Deps{
		Share:           shareUC,
		Submission:      submissionUC,
		Tokens:          tokens,
		Limiter:         limiter,
		RateLimitPerMin: cfg.RateLimitPerMin,
		Logger:          log,
		EnableDevToken:  !cfg.IsProduction(),
		TrustedProxies:  cfg.TrustedProxies,
		ReadyCheck: func(ctx context.Context) error {
			if err := db.Ping(ctx); err != nil {
				return errors.New("postgres unavailable")
			}
			if err := rdb.Ping(ctx).Err(); err != nil {
				return errors.New("redis unavailable")
			}
			return nil
		},
	})
	if err != nil {
		return err
	}

	/* ---------- Background: đếm view ---------- */
	// Context riêng: chỉ dừng SAU khi HTTP server ngừng nhận request,
	// để drain hết view trong buffer và flush lần cuối xuống DB.
	counterCtx, stopCounter := context.WithCancel(context.Background())
	counterDone := make(chan struct{})
	go func() {
		defer close(counterDone)
		views.Run(counterCtx, cfg.ViewWorkers)
	}()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	srvErr := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-srvErr:
		log.Error("http server failed", "err", err)
	}

	/* ---------- Graceful shutdown ---------- */
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil { // 1. ngừng nhận request mới
		log.Error("http shutdown", "err", err)
	}
	stopCounter() // 2. drain buffer view + flush lần cuối
	<-counterDone
	log.Info("bye")
	return nil
}
