package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/submission"
)

const (
	snapshotTTL     = 10 * time.Minute
	snapshotTimeout = 2 * time.Second
)

func snapshotKey(rt domain.ResourceType, id int64) string {
	return fmt.Sprintf("submission:snapshot:%s:%d", rt, id)
}

type snapshotRecord struct {
	OwnerName string  `json:"owner"`
	Title     string  `json:"title"`
	Content   string  `json:"content"`
	BandScore float32 `json:"band"`
	Feedback  string  `json:"feedback"`
}

// CachedContentReader là decorator: bọc ContentReader thật (PostgreSQL) bằng cache Redis + singleflight.
// Use case không biết có cache — chỉ thấy port ContentReader.
type CachedContentReader struct {
	next share.ContentReader
	rdb  redis.UniversalClient
	log  *slog.Logger
	sf   singleflight.Group
}

func NewCachedContentReader(next share.ContentReader, rdb redis.UniversalClient, log *slog.Logger) *CachedContentReader {
	return &CachedContentReader{next: next, rdb: rdb, log: log}
}

var (
	_ share.ContentReader            = (*CachedContentReader)(nil)
	_ submission.SnapshotInvalidator = (*CachedContentReader)(nil)
)

// IsOwner nằm ở luồng ghi (tạo link) → luôn đọc trực tiếp, không cache.
func (c *CachedContentReader) IsOwner(ctx context.Context, rt domain.ResourceType, id, userID int64) (bool, error) {
	return c.next.IsOwner(ctx, rt, id, userID)
}

func (c *CachedContentReader) Snapshot(ctx context.Context, rt domain.ResourceType, id int64) (*domain.Snapshot, error) {
	key := snapshotKey(rt, id)

	raw, err := c.rdb.Get(ctx, key).Bytes()
	if err == nil {
		var rec snapshotRecord
		if json.Unmarshal(raw, &rec) == nil {
			return &domain.Snapshot{OwnerName: rec.OwnerName, Title: rec.Title, Content: rec.Content,
				BandScore: rec.BandScore, Feedback: rec.Feedback}, nil
		}
	} else if !errors.Is(err, redis.Nil) {
		c.log.Warn("redisstore: snapshot cache unavailable", "err", err)
	}

	v, err, _ := c.sf.Do(key, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), snapshotTimeout)
		defer cancel()

		s, err := c.next.Snapshot(loadCtx, rt, id)
		if err != nil {
			return nil, err
		}
		b, _ := json.Marshal(snapshotRecord{OwnerName: s.OwnerName, Title: s.Title, Content: s.Content,
			BandScore: s.BandScore, Feedback: s.Feedback})
		if err := c.rdb.Set(loadCtx, key, b, snapshotTTL).Err(); err != nil {
			c.log.Warn("redisstore: set snapshot cache failed", "err", err)
		}
		return s, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*domain.Snapshot), nil
}

// Invalidate: gọi khi giáo viên chấm lại, học viên xoá bài hoặc tài khoản bị khoá.
func (c *CachedContentReader) Invalidate(ctx context.Context, rt domain.ResourceType, id int64) error {
	return c.rdb.Del(ctx, snapshotKey(rt, id)).Err()
}
