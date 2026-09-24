package redisstore

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter: fixed window trên Redis, dùng chung giữa mọi instance API.
type RateLimiter struct{ rdb redis.UniversalClient }

func NewRateLimiter(rdb redis.UniversalClient) *RateLimiter { return &RateLimiter{rdb: rdb} }

func (l *RateLimiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, error) {
	bucket := time.Now().Unix() / int64(window.Seconds())
	k := fmt.Sprintf("rl:%s:%d", key, bucket)

	pipe := l.rdb.Pipeline()
	incr := pipe.Incr(ctx, k)
	pipe.Expire(ctx, k, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return true, err // caller quyết định fail-open
	}
	return incr.Val() <= limit, nil
}
