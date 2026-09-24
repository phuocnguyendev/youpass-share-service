package datastore

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

func ConnectRedis(ctx context.Context, addr, password string, db int, log *slog.Logger) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  500 * time.Millisecond, // Redis chậm thì fallback DB, không treo request
		WriteTimeout: 500 * time.Millisecond,
		PoolSize:     50,
	})

	if err := retry(ctx, log, "redis", func(ctx context.Context) error {
		return rdb.Ping(ctx).Err()
	}); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	return rdb, nil
}
