package datastore

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationLockID = 727_001

func ConnectPostgres(ctx context.Context, url string, log *slog.Logger) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := retry(ctx, log, "postgres", pool.Ping); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Migrate chạy các file *.sql theo thứ tự tên, mỗi file trong 1 transaction.
// Advisory lock: nhiều replica khởi động cùng lúc thì chỉ 1 replica chạy migration.
func Migrate(ctx context.Context, db *pgxpool.Pool, fsys fs.FS, log *slog.Logger) error {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}

	files, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)

	for _, name := range files {
		var applied bool
		if err := conn.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", name,
		).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}

		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		log.Info("migration applied", "version", name)
	}
	return nil
}

// retry chờ dependency sẵn sàng (container DB/Redis khởi động chậm hơn API).
func retry(ctx context.Context, log *slog.Logger, name string, fn func(context.Context) error) error {
	const attempts = 10
	delay := 500 * time.Millisecond

	var err error
	for i := 1; i <= attempts; i++ {
		if err = fn(ctx); err == nil {
			return nil
		}
		log.Warn("dependency not ready, retrying", "dependency", name, "attempt", i, "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Second)
	}
	return fmt.Errorf("%s not ready after %d attempts: %w", name, attempts, err)
}
