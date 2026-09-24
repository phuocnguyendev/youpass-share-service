// Package httpapi là delivery adapter: chuyển HTTP request thành lời gọi use case và
// chuyển kết quả/lỗi domain thành HTTP response.
package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/httpapi/middleware"
	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/token"
	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
)

// ShareUseCase là input port mà handler cần — handler phụ thuộc interface, không phụ thuộc Service cụ thể.
type ShareUseCase interface {
	Create(ctx context.Context, in share.CreateInput) (*domain.Link, bool, error)
	Resolve(ctx context.Context, code string, v domain.Viewer) (*share.SharedView, error)
	SetStatus(ctx context.Context, ownerID int64, code string, st domain.Status) (*domain.Link, error)
	Delete(ctx context.Context, ownerID int64, code string) error
	Stats(ctx context.Context, ownerID int64, code string) (*share.Stats, error)
	URL(code string) string
}

type SubmissionUseCase interface {
	Delete(ctx context.Context, userID, submissionID int64) error
}

type Deps struct {
	Share           ShareUseCase
	Submission      SubmissionUseCase
	Tokens          *token.Manager
	Limiter         middleware.Limiter
	RateLimitPerMin int64
	Logger          *slog.Logger
	ReadyCheck      func(ctx context.Context) error
	EnableDevToken  bool     // chỉ bật ở development
	TrustedProxies  []string // IP của Nginx/ALB để ClientIP() đúng
}

func NewRouter(d Deps) (*gin.Engine, error) {
	if d.Logger == nil {
		d.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	r := gin.New()
	if err := r.SetTrustedProxies(d.TrustedProxies); err != nil {
		return nil, err
	}
	r.Use(gin.Recovery(), middleware.RequestLogger(d.Logger))

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		if d.ReadyCheck != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
			defer cancel()
			if err := d.ReadyCheck(ctx); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	required := middleware.RequireAuth(d.Tokens)
	optional := middleware.OptionalAuth(d.Tokens)
	limiter := middleware.RateLimit(d.Limiter, "share", d.RateLimitPerMin, time.Minute)

	if d.EnableDevToken {
		r.POST("/api/v1/dev/token", devToken(d.Tokens))
	}

	sh := &shareHandler{uc: d.Share, log: d.Logger}
	r.GET("/s/:code", optional, limiter, sh.page) // demo; production: Next.js SSR render
	api := r.Group("/api/v1/shares")
	api.GET("/:code", optional, limiter, sh.resolve)
	api.POST("", required, sh.create)
	api.PATCH("/:code", required, sh.updateStatus)
	api.DELETE("/:code", required, sh.delete)
	api.GET("/:code/stats", required, sh.stats)

	if d.Submission != nil {
		sub := &submissionHandler{uc: d.Submission, log: d.Logger}
		r.DELETE("/api/v1/submissions/:id", required, sub.delete)
	}

	return r, nil
}

// devToken cấp token để test API ở môi trường development.
func devToken(m *token.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			UserID int64 `json:"userId" binding:"required,gt=0"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST"})
			return
		}
		signed, exp, err := m.Issue(req.UserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"accessToken": signed, "expiresAt": exp})
	}
}
