package redisstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/memstore"
	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func newRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb, mr
}

// viewSink ghi nhận view được flush (thay cho PostgreSQL).
type viewSink struct {
	views map[int64]int64
	fail  atomic.Bool
}

func newSink() *viewSink { return &viewSink{views: map[int64]int64{}} }

func (s *viewSink) AddViews(_ context.Context, deltas map[int64]int64) error {
	if s.fail.Load() {
		return errors.New("db unavailable")
	}
	for id, n := range deltas {
		s.views[id] += n
	}
	return nil
}

/* ============================== LinkCache ============================== */

func TestLinkCache_RoundTripAndNegative(t *testing.T) {
	rdb, _ := newRedis(t)
	c := NewLinkCache(rdb, discard)
	ctx := context.Background()

	if _, err := c.Get(ctx, "Ab3xK9pQ"); !errors.Is(err, share.ErrCacheMiss) {
		t.Fatalf("expected miss, got %v", err)
	}

	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	in := &domain.Link{ID: 7, Code: "Ab3xK9pQ", OwnerID: 1, ResourceType: domain.ResourceWriting,
		ResourceID: 101, Status: domain.StatusDisabled, ExpiresAt: &exp}
	if err := c.Set(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := c.Get(ctx, "Ab3xK9pQ")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 7 || got.Status != domain.StatusDisabled || !got.ExpiresAt.Equal(exp) {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	_ = c.SetNotFound(ctx, "Zz9Zz9Zz")
	if _, err := c.Get(ctx, "Zz9Zz9Zz"); !errors.Is(err, domain.ErrLinkNotFound) {
		t.Fatalf("expected negative hit, got %v", err)
	}
}

func TestLinkCache_InvalidateDeletesTwice(t *testing.T) {
	rdb, _ := newRedis(t)
	c := NewLinkCache(rdb, discard)
	ctx := context.Background()
	l := &domain.Link{ID: 1, Code: "Ab3xK9pQ", Status: domain.StatusActive}

	_ = c.Set(ctx, l)
	if err := c.Invalidate(ctx, l.Code); err != nil {
		t.Fatal(err)
	}
	// Giả lập request đọc chậm ghi lại giá trị cũ ngay sau lần xoá đầu
	_ = c.Set(ctx, l)
	time.Sleep(secondDelete + 200*time.Millisecond)

	if _, err := c.Get(ctx, l.Code); !errors.Is(err, share.ErrCacheMiss) {
		t.Fatalf("stale value must be removed by delayed second delete, got %v", err)
	}
}

/* ============================== CachedContentReader ============================== */

type countingReader struct {
	*memstore.ContentReader
	calls atomic.Int64
}

func (r *countingReader) Snapshot(ctx context.Context, rt domain.ResourceType, id int64) (*domain.Snapshot, error) {
	r.calls.Add(1)
	return r.ContentReader.Snapshot(ctx, rt, id)
}

func TestCachedContentReader_CachesAndInvalidates(t *testing.T) {
	rdb, _ := newRedis(t)
	next := &countingReader{ContentReader: memstore.NewContentReader()}
	c := NewCachedContentReader(next, rdb, discard)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := c.Snapshot(ctx, domain.ResourceWriting, 101); err != nil {
			t.Fatal(err)
		}
	}
	if n := next.calls.Load(); n != 1 {
		t.Fatalf("expected 1 load, got %d", n)
	}

	next.MarkDeleted(101)
	_ = c.Invalidate(ctx, domain.ResourceWriting, 101)
	if _, err := c.Snapshot(ctx, domain.ResourceWriting, 101); !errors.Is(err, domain.ErrSubmissionNotFound) {
		t.Fatalf("expected not found after invalidate, got %v", err)
	}
}

/* ============================== ViewCounter ============================== */

func TestViewCounter_DedupesSameVisitor(t *testing.T) {
	rdb, _ := newRedis(t)
	sink := newSink()
	vc := NewViewCounter(rdb, sink, discard, time.Hour)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, _ = vc.record(ctx, 7, "visitor-a")
	}
	_, _ = vc.record(ctx, 7, "visitor-b")
	_, _ = vc.record(ctx, 7, "visitor-c")

	pending, unique, err := vc.Pending(ctx, 7)
	if err != nil || pending != 3 || unique != 3 {
		t.Fatalf("expected 3/3, got pending=%d unique=%d err=%v", pending, unique, err)
	}

	if n, err := vc.flush(ctx); err != nil || n != 3 || sink.views[7] != 3 {
		t.Fatalf("expected 3 flushed, got n=%d db=%d err=%v", n, sink.views[7], err)
	}
	if pending, _, _ := vc.Pending(ctx, 7); pending != 0 {
		t.Fatalf("counter must reset after flush, got %d", pending)
	}
}

func TestViewCounter_RestoresWhenStoreFails(t *testing.T) {
	rdb, _ := newRedis(t)
	sink := newSink()
	vc := NewViewCounter(rdb, sink, discard, time.Hour)
	ctx := context.Background()

	_, _ = vc.record(ctx, 9, "a")
	_, _ = vc.record(ctx, 9, "b")

	sink.fail.Store(true)
	if _, err := vc.flush(ctx); err == nil {
		t.Fatal("expected error when store fails")
	}
	if pending, _, _ := vc.Pending(ctx, 9); pending != 2 {
		t.Fatalf("views must be restored to Redis, got %d", pending)
	}

	sink.fail.Store(false)
	if n, err := vc.flush(ctx); err != nil || n != 2 || sink.views[9] != 2 {
		t.Fatalf("expected retry flush of 2, got n=%d err=%v", n, err)
	}
}

func TestViewCounter_FlushesManyLinksInBatches(t *testing.T) {
	rdb, _ := newRedis(t)
	sink := newSink()
	vc := NewViewCounter(rdb, sink, discard, time.Hour)
	ctx := context.Background()
	const links = flushBatch + 250

	for id := int64(1); id <= links; id++ {
		_, _ = vc.record(ctx, id, "v")
	}
	if n, err := vc.flush(ctx); err != nil || n != links {
		t.Fatalf("expected %d flushed, got %d (%v)", links, n, err)
	}
}

func TestViewCounter_DrainsBufferOnShutdown(t *testing.T) {
	rdb, _ := newRedis(t)
	sink := newSink()
	vc := NewViewCounter(rdb, sink, discard, time.Hour)

	for i := 0; i < 50; i++ {
		vc.Track(11, fmt.Sprintf("visitor-%d", i))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // shutdown ngay: phải drain buffer + flush lần cuối rồi mới thoát
	vc.Run(ctx, 4)

	if sink.views[11] != 50 {
		t.Fatalf("expected 50 views persisted on shutdown, got %d", sink.views[11])
	}
}

func TestViewCounter_TrackNeverBlocks(t *testing.T) {
	rdb, _ := newRedis(t)
	vc := NewViewCounter(rdb, newSink(), discard, time.Hour)
	for i := 0; i < eventBuffer+100; i++ { // không có worker → buffer đầy
		vc.Track(1, "v")
	}
	if n := len(vc.events); n != eventBuffer {
		t.Fatalf("expected buffer capped at %d, got %d", eventBuffer, n)
	}
}

/* ============================== RateLimiter ============================== */

func TestRateLimiter(t *testing.T) {
	rdb, _ := newRedis(t)
	l := NewRateLimiter(rdb)
	ctx := context.Background()

	var denied int
	for i := 0; i < 8; i++ {
		if ok, err := l.Allow(ctx, "share:1.2.3.4", 5, time.Minute); err != nil {
			t.Fatal(err)
		} else if !ok {
			denied++
		}
	}
	if denied != 3 {
		t.Fatalf("expected 3 denied (limit 5 of 8), got %d", denied)
	}
	if ok, _ := l.Allow(ctx, "share:5.6.7.8", 5, time.Minute); !ok {
		t.Fatal("other IP must not be affected")
	}
}
