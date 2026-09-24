// Package memstore cài đặt các port bằng bộ nhớ trong — dùng cho unit test use case và HTTP
// mà không cần PostgreSQL/Redis. Tất cả đều an toàn khi dùng đồng thời.
package memstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
)

var ErrUnavailable = errors.New("memstore: unavailable")

/* ============================== LinkRepository + ViewStore ============================== */

type LinkRepository struct {
	mu     sync.Mutex
	links  map[string]*domain.Link
	nextID int64

	FindCalls    atomic.Int64
	FindDelay    time.Duration // giả lập DB chậm
	FailAddViews atomic.Bool
}

func NewLinkRepository() *LinkRepository {
	return &LinkRepository{links: map[string]*domain.Link{}}
}

func clone(l *domain.Link) *domain.Link { cp := *l; return &cp }

func (r *LinkRepository) Insert(_ context.Context, l *domain.Link) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.links[l.Code]; ok {
		return share.ErrDuplicateCode
	}
	for _, x := range r.links {
		if x.ResourceType == l.ResourceType && x.ResourceID == l.ResourceID && x.DeletedAt == nil {
			return share.ErrAlreadyShared
		}
	}
	r.nextID++
	l.ID, l.CreatedAt = r.nextID, time.Now()
	r.links[l.Code] = clone(l)
	return nil
}

func (r *LinkRepository) FindByCode(_ context.Context, code string) (*domain.Link, error) {
	r.FindCalls.Add(1)
	if r.FindDelay > 0 {
		time.Sleep(r.FindDelay)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if l, ok := r.links[code]; ok {
		return clone(l), nil
	}
	return nil, domain.ErrLinkNotFound
}

func (r *LinkRepository) FindByResource(_ context.Context, rt domain.ResourceType, id int64) (*domain.Link, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.links {
		if l.ResourceType == rt && l.ResourceID == id && l.DeletedAt == nil {
			return clone(l), nil
		}
	}
	return nil, domain.ErrLinkNotFound
}

func (r *LinkRepository) owned(code string, ownerID int64) (*domain.Link, bool) {
	l, ok := r.links[code]
	return l, ok && l.OwnerID == ownerID && l.DeletedAt == nil
}

func (r *LinkRepository) FindOwned(_ context.Context, code string, ownerID int64) (*domain.Link, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l, ok := r.owned(code, ownerID); ok {
		return clone(l), nil
	}
	return nil, domain.ErrLinkNotFound
}

func (r *LinkRepository) UpdateStatus(_ context.Context, code string, ownerID int64, st domain.Status) (*domain.Link, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.owned(code, ownerID)
	if !ok {
		return nil, domain.ErrLinkNotFound
	}
	l.Status = st
	return clone(l), nil
}

func (r *LinkRepository) SoftDelete(_ context.Context, code string, ownerID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.owned(code, ownerID)
	if !ok {
		return domain.ErrLinkNotFound
	}
	now := time.Now()
	l.DeletedAt = &now
	return nil
}

func (r *LinkRepository) AddViews(_ context.Context, deltas map[int64]int64) error {
	if r.FailAddViews.Load() {
		return ErrUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.links {
		l.ViewCount += deltas[l.ID]
	}
	return nil
}

// ViewsOf trả view_count đã ghi của link (dùng trong test).
func (r *LinkRepository) ViewsOf(linkID int64) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.links {
		if l.ID == linkID {
			return l.ViewCount
		}
	}
	return 0
}

/* ============================== LinkCache ============================== */

type LinkCache struct {
	mu       sync.Mutex
	items    map[string]*domain.Link
	negative map[string]bool
	Fail     atomic.Bool // giả lập Redis sập
}

func NewLinkCache() *LinkCache {
	return &LinkCache{items: map[string]*domain.Link{}, negative: map[string]bool{}}
}

func (c *LinkCache) Get(_ context.Context, code string) (*domain.Link, error) {
	if c.Fail.Load() {
		return nil, ErrUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.negative[code] {
		return nil, domain.ErrLinkNotFound
	}
	if l, ok := c.items[code]; ok {
		return clone(l), nil
	}
	return nil, share.ErrCacheMiss
}

func (c *LinkCache) Set(_ context.Context, l *domain.Link) error {
	if c.Fail.Load() {
		return ErrUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[l.Code] = clone(l)
	delete(c.negative, l.Code)
	return nil
}

func (c *LinkCache) SetNotFound(_ context.Context, code string) error {
	if c.Fail.Load() {
		return ErrUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.negative[code] = true
	return nil
}

func (c *LinkCache) Invalidate(_ context.Context, code string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, code)
	delete(c.negative, code)
	return nil
}

/* ============================== ContentReader ============================== */

type ContentReader struct {
	mu      sync.RWMutex
	owners  map[int64]int64 // submissionID → ownerID
	deleted map[int64]bool
}

// NewContentReader với dữ liệu mẫu: user 1 sở hữu bài 101, 102; user 2 sở hữu bài 201.
func NewContentReader() *ContentReader {
	return &ContentReader{
		owners:  map[int64]int64{101: 1, 102: 1, 201: 2},
		deleted: map[int64]bool{},
	}
}

func (c *ContentReader) IsOwner(_ context.Context, _ domain.ResourceType, id, userID int64) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.owners[id] == userID && !c.deleted[id], nil
}

func (c *ContentReader) Snapshot(_ context.Context, _ domain.ResourceType, id int64) (*domain.Snapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if _, ok := c.owners[id]; !ok || c.deleted[id] {
		return nil, domain.ErrSubmissionNotFound
	}
	return &domain.Snapshot{
		OwnerName: "Minh Anh", Title: fmt.Sprintf("Submission #%d", id), Content: "essay", BandScore: 7.5,
	}, nil
}

func (c *ContentReader) MarkDeleted(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleted[id] = true
}

/* ============================== ViewTracker ============================== */

// ViewTracker ghi nhận view đồng bộ trong bộ nhớ, có dedupe theo visitor.
type ViewTracker struct {
	mu      sync.Mutex
	tracked int
	seen    map[string]bool
	pending map[int64]int64
	unique  map[int64]map[string]bool
}

func NewViewTracker() *ViewTracker {
	return &ViewTracker{seen: map[string]bool{}, pending: map[int64]int64{}, unique: map[int64]map[string]bool{}}
}

func (t *ViewTracker) Track(linkID int64, visitor string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tracked++
	if t.unique[linkID] == nil {
		t.unique[linkID] = map[string]bool{}
	}
	t.unique[linkID][visitor] = true
	key := fmt.Sprintf("%d:%s", linkID, visitor)
	if !t.seen[key] {
		t.seen[key] = true
		t.pending[linkID]++
	}
}

func (t *ViewTracker) Pending(_ context.Context, linkID int64) (int64, int64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pending[linkID], int64(len(t.unique[linkID])), nil
}

// Tracked là tổng số lần Track được gọi (kể cả bị dedupe).
func (t *ViewTracker) Tracked() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tracked
}

/* ============================== Rate limiter ============================== */

type RateLimiter struct {
	mu     sync.Mutex
	counts map[string]int64
}

func NewRateLimiter() *RateLimiter { return &RateLimiter{counts: map[string]int64{}} }

func (l *RateLimiter) Allow(_ context.Context, key string, limit int64, _ time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.counts[key]++
	return l.counts[key] <= limit, nil
}

/* ============================== Compile-time checks ============================== */

var (
	_ share.LinkRepository = (*LinkRepository)(nil)
	_ share.ViewStore      = (*LinkRepository)(nil)
	_ share.LinkCache      = (*LinkCache)(nil)
	_ share.ContentReader  = (*ContentReader)(nil)
	_ share.ViewTracker    = (*ViewTracker)(nil)
)
