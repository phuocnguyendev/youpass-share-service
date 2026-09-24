// Package redisstore cài đặt các port bằng Redis: cache link, cache snapshot bài làm,
// bộ đếm view bất đồng bộ và rate limiter.
package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
)

const (
	linkTTL      = time.Hour
	notFoundTTL  = time.Minute
	notFoundMark = "-"
	secondDelete = 500 * time.Millisecond
)

func linkKey(code string) string { return "share:link:" + code }

// withJitter cộng thêm 0–10% để nhiều key không hết hạn cùng lúc.
func withJitter(d time.Duration) time.Duration {
	return d + time.Duration(rand.Int64N(int64(d/10)))
}

// linkRecord là định dạng lưu trong Redis — domain entity không mang tag JSON.
type linkRecord struct {
	ID           int64      `json:"id"`
	Code         string     `json:"code"`
	OwnerID      int64      `json:"owner"`
	ResourceType string     `json:"rtype"`
	ResourceID   int64      `json:"rid"`
	Status       string     `json:"status"`
	ExpiresAt    *time.Time `json:"exp,omitempty"`
	DeletedAt    *time.Time `json:"del,omitempty"`
	CreatedAt    time.Time  `json:"at"`
}

func toRecord(l *domain.Link) linkRecord {
	return linkRecord{
		ID: l.ID, Code: l.Code, OwnerID: l.OwnerID, ResourceType: string(l.ResourceType),
		ResourceID: l.ResourceID, Status: string(l.Status), ExpiresAt: l.ExpiresAt,
		DeletedAt: l.DeletedAt, CreatedAt: l.CreatedAt,
	}
}

func (r linkRecord) toDomain() *domain.Link {
	return &domain.Link{
		ID: r.ID, Code: r.Code, OwnerID: r.OwnerID, ResourceType: domain.ResourceType(r.ResourceType),
		ResourceID: r.ResourceID, Status: domain.Status(r.Status), ExpiresAt: r.ExpiresAt,
		DeletedAt: r.DeletedAt, CreatedAt: r.CreatedAt,
	}
}

type LinkCache struct {
	rdb redis.UniversalClient
	log *slog.Logger
}

func NewLinkCache(rdb redis.UniversalClient, log *slog.Logger) *LinkCache {
	return &LinkCache{rdb: rdb, log: log}
}

var _ share.LinkCache = (*LinkCache)(nil)

func (c *LinkCache) Get(ctx context.Context, code string) (*domain.Link, error) {
	raw, err := c.rdb.Get(ctx, linkKey(code)).Result()
	switch {
	case errors.Is(err, redis.Nil):
		cacheTotal.WithLabelValues("miss").Inc()
		return nil, share.ErrCacheMiss
	case err != nil:
		cacheTotal.WithLabelValues("error").Inc()
		return nil, err
	case raw == notFoundMark:
		cacheTotal.WithLabelValues("negative_hit").Inc()
		return nil, domain.ErrLinkNotFound
	}

	var rec linkRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		cacheTotal.WithLabelValues("miss").Inc()
		return nil, share.ErrCacheMiss // dữ liệu hỏng → coi như miss, đọc lại từ DB
	}
	cacheTotal.WithLabelValues("hit").Inc()
	return rec.toDomain(), nil
}

func (c *LinkCache) Set(ctx context.Context, l *domain.Link) error {
	b, err := json.Marshal(toRecord(l))
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, linkKey(l.Code), b, withJitter(linkTTL)).Err()
}

// SetNotFound: negative cache chống dò mã ngẫu nhiên đánh thẳng vào DB.
func (c *LinkCache) SetNotFound(ctx context.Context, code string) error {
	return c.rdb.Set(ctx, linkKey(code), notFoundMark, notFoundTTL).Err()
}

// Invalidate xoá ngay, rồi xoá lần 2 sau 500ms (delayed double delete) để dọn giá trị cũ
// do request đọc đồng thời ghi lại ngay sau lần xoá đầu.
func (c *LinkCache) Invalidate(ctx context.Context, code string) error {
	key := linkKey(code)
	err := c.rdb.Del(ctx, key).Err()
	time.AfterFunc(secondDelete, func() {
		bg, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := c.rdb.Del(bg, key).Err(); err != nil {
			c.log.Warn("redisstore: second delete failed", "err", err)
		}
	})
	return err
}
