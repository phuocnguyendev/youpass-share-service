// Unit test use case với adapter in-memory: không cần PostgreSQL/Redis, chạy trong vài ms.
package share_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/memstore"
	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
)

const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/537.36 Chrome/128.0 Safari/537.36"

var anon = domain.Viewer{VisitorID: "visitor-anon", UserAgent: browserUA}

type fixture struct {
	svc     *share.Service
	links   *memstore.LinkRepository
	cache   *memstore.LinkCache
	content *memstore.ContentReader
	views   *memstore.ViewTracker
}

func newFixture(t *testing.T, opts ...func(*share.Deps)) *fixture {
	t.Helper()
	f := &fixture{
		links:   memstore.NewLinkRepository(),
		cache:   memstore.NewLinkCache(),
		content: memstore.NewContentReader(),
		views:   memstore.NewViewTracker(),
	}
	deps := share.Deps{
		Links: f.links, Cache: f.cache, Content: f.content, Views: f.views,
		BaseURL: "https://youpass.vn/s/",
	}
	for _, o := range opts {
		o(&deps)
	}
	f.svc = share.NewService(deps)
	return f
}

func (f *fixture) create(t *testing.T, ownerID, resourceID int64) *domain.Link {
	t.Helper()
	l, _, err := f.svc.Create(context.Background(), share.CreateInput{
		OwnerID: ownerID, ResourceType: domain.ResourceWriting, ResourceID: resourceID,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return l
}

/* ============================== Create ============================== */

func TestCreate_IsIdempotent(t *testing.T) {
	f := newFixture(t)
	in := share.CreateInput{OwnerID: 1, ResourceType: domain.ResourceWriting, ResourceID: 101}

	first, created, err := f.svc.Create(context.Background(), in)
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	second, created, err := f.svc.Create(context.Background(), in)
	if err != nil || created || second.Code != first.Code {
		t.Fatalf("second: created=%v code=%s/%s err=%v", created, first.Code, second.Code, err)
	}
	if f.svc.URL(first.Code) != "https://youpass.vn/s/"+first.Code {
		t.Fatalf("unexpected url %s", f.svc.URL(first.Code))
	}
}

func TestCreate_RejectsNonOwnerAndInvalidType(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, _, err := f.svc.Create(ctx, share.CreateInput{OwnerID: 2, ResourceType: domain.ResourceWriting, ResourceID: 101}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	if _, _, err := f.svc.Create(ctx, share.CreateInput{OwnerID: 1, ResourceType: "admin_secret", ResourceID: 101}); !errors.Is(err, domain.ErrInvalidResourceType) {
		t.Fatalf("expected ErrInvalidResourceType, got %v", err)
	}
}

func TestCreate_RetriesOnDuplicateCode(t *testing.T) {
	calls := 0
	gen := func() (string, error) { // 2 lần đầu trả mã đã tồn tại
		calls++
		if calls <= 2 {
			return "AAAAAAAA", nil
		}
		return domain.NewCode()
	}
	f := newFixture(t, func(d *share.Deps) { d.NewCode = gen })
	_ = f.links.Insert(context.Background(), domain.NewLink("AAAAAAAA", 2, domain.ResourceWriting, 201, nil))

	l := f.create(t, 1, 101)
	if l.Code == "AAAAAAAA" || calls != 3 {
		t.Fatalf("expected retry to a fresh code, got %s after %d calls", l.Code, calls)
	}
}

func TestCreate_GivesUpAfterMaxRetries(t *testing.T) {
	f := newFixture(t, func(d *share.Deps) { d.NewCode = func() (string, error) { return "AAAAAAAA", nil } })
	_ = f.links.Insert(context.Background(), domain.NewLink("AAAAAAAA", 2, domain.ResourceWriting, 201, nil))

	_, _, err := f.svc.Create(context.Background(), share.CreateInput{OwnerID: 1, ResourceType: domain.ResourceWriting, ResourceID: 101})
	if !errors.Is(err, share.ErrCodeExhausted) {
		t.Fatalf("expected ErrCodeExhausted, got %v", err)
	}
}

/* ============================== Resolve: tốc độ ============================== */

func TestResolve_SingleflightAndCache(t *testing.T) {
	f := newFixture(t)
	l := f.create(t, 1, 101)
	f.links.FindCalls.Store(0)
	f.links.FindDelay = 200 * time.Millisecond // DB chậm → 200 request dồn vào cùng lúc

	const concurrent = 200
	var wg sync.WaitGroup
	errs := make(chan error, concurrent)
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.svc.Resolve(context.Background(), l.Code, anon); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	if got := f.links.FindCalls.Load(); got != 1 {
		t.Fatalf("expected 1 DB query for %d concurrent requests, got %d", concurrent, got)
	}
	_, _ = f.svc.Resolve(context.Background(), l.Code, anon)
	if got := f.links.FindCalls.Load(); got != 1 {
		t.Fatalf("expected cache hit, got %d DB queries", got)
	}
}

func TestResolve_NegativeCacheAndInvalidFormat(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for _, code := range []string{"", "abc", "abc!@#xy", "../../etc"} {
		if _, err := f.svc.Resolve(ctx, code, anon); !errors.Is(err, domain.ErrLinkNotFound) {
			t.Fatalf("%q: expected ErrLinkNotFound, got %v", code, err)
		}
	}
	if got := f.links.FindCalls.Load(); got != 0 {
		t.Fatalf("invalid format must not hit DB, got %d", got)
	}

	for i := 0; i < 3; i++ {
		_, _ = f.svc.Resolve(ctx, "Zz9Zz9Zz", anon)
	}
	if got := f.links.FindCalls.Load(); got != 1 {
		t.Fatalf("expected 1 DB query thanks to negative cache, got %d", got)
	}
}

func TestResolve_FallsBackToRepositoryWhenCacheDown(t *testing.T) {
	f := newFixture(t)
	l := f.create(t, 1, 101)
	f.cache.Fail.Store(true)

	if _, err := f.svc.Resolve(context.Background(), l.Code, anon); err != nil {
		t.Fatalf("must degrade gracefully when cache is down, got %v", err)
	}
}

/* ============================== Resolve: quyền truy cập ============================== */

func TestResolve_DisableTakesEffectImmediately(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	l := f.create(t, 1, 101)
	_, _ = f.svc.Resolve(ctx, l.Code, anon) // warm cache

	if _, err := f.svc.SetStatus(ctx, 1, l.Code, domain.StatusDisabled); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Resolve(ctx, l.Code, anon); !errors.Is(err, domain.ErrLinkGone) {
		t.Fatalf("expected ErrLinkGone right after disable, got %v", err)
	}

	if _, err := f.svc.SetStatus(ctx, 1, l.Code, domain.StatusActive); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Resolve(ctx, l.Code, anon); err != nil {
		t.Fatalf("expected link to work after re-enable, got %v", err)
	}
}

func TestSetStatus_OnlyOwnerAndValidStatus(t *testing.T) {
	f := newFixture(t)
	l := f.create(t, 1, 101)

	if _, err := f.svc.SetStatus(context.Background(), 2, l.Code, domain.StatusDisabled); !errors.Is(err, domain.ErrLinkNotFound) {
		t.Fatalf("non-owner must get ErrLinkNotFound (no existence leak), got %v", err)
	}
	if _, err := f.svc.SetStatus(context.Background(), 1, l.Code, "deleted"); !errors.Is(err, domain.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
}

func TestDelete_LinkGoneAndReshareGetsNewCode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	old := f.create(t, 1, 101)
	_, _ = f.svc.Resolve(ctx, old.Code, anon)

	if err := f.svc.Delete(ctx, 1, old.Code); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Resolve(ctx, old.Code, anon); !errors.Is(err, domain.ErrLinkGone) {
		t.Fatalf("expected ErrLinkGone after delete, got %v", err)
	}

	fresh, created, err := f.svc.Create(ctx, share.CreateInput{OwnerID: 1, ResourceType: domain.ResourceWriting, ResourceID: 101})
	if err != nil || !created || fresh.Code == old.Code {
		t.Fatalf("reshare must get a new code: created=%v old=%s new=%s err=%v", created, old.Code, fresh.Code, err)
	}
}

func TestResolve_Expired(t *testing.T) {
	now := time.Now()
	clock := now
	f := newFixture(t, func(d *share.Deps) { d.Clock = func() time.Time { return clock } })
	exp := now.Add(time.Hour)
	l, _, _ := f.svc.Create(context.Background(), share.CreateInput{
		OwnerID: 1, ResourceType: domain.ResourceWriting, ResourceID: 101, ExpiresAt: &exp,
	})

	if _, err := f.svc.Resolve(context.Background(), l.Code, anon); err != nil {
		t.Fatalf("before expiry: %v", err)
	}
	clock = exp.Add(time.Minute)
	if _, err := f.svc.Resolve(context.Background(), l.Code, anon); !errors.Is(err, domain.ErrLinkGone) {
		t.Fatalf("expected ErrLinkGone after expiry, got %v", err)
	}
}

func TestResolve_GoneWhenSubmissionDeleted(t *testing.T) {
	f := newFixture(t)
	l := f.create(t, 1, 101)
	f.content.MarkDeleted(101)

	if _, err := f.svc.Resolve(context.Background(), l.Code, anon); !errors.Is(err, domain.ErrLinkGone) {
		t.Fatalf("expected ErrLinkGone when submission deleted, got %v", err)
	}
}

/* ============================== Đếm view ============================== */

func TestResolve_TracksOnlyRealViewers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	l := f.create(t, 1, 101)

	_, _ = f.svc.Resolve(ctx, l.Code, domain.Viewer{UserID: 1, UserAgent: browserUA}) // chủ bài
	_, _ = f.svc.Resolve(ctx, l.Code, domain.Viewer{UserAgent: "facebookexternalhit/1.1"})
	_, _ = f.svc.Resolve(ctx, l.Code, domain.Viewer{UserAgent: "Zalo-Preview"})
	if n := f.views.Tracked(); n != 0 {
		t.Fatalf("owner/bot must not be tracked, got %d", n)
	}

	_, _ = f.svc.Resolve(ctx, l.Code, anon)
	if n := f.views.Tracked(); n != 1 {
		t.Fatalf("expected 1 tracked view, got %d", n)
	}
}

func TestStats_CombinesPersistedAndPendingViews(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	l := f.create(t, 1, 101)

	_ = f.links.AddViews(ctx, map[int64]int64{l.ID: 10}) // đã flush trước đó
	for _, v := range []string{"a", "b", "a"} {
		_, _ = f.svc.Resolve(ctx, l.Code, domain.Viewer{VisitorID: v, UserAgent: browserUA})
	}

	st, err := f.svc.Stats(ctx, 1, l.Code)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalViews != 12 || st.UniqueToday != 2 {
		t.Fatalf("expected total=12 unique=2, got %+v", st)
	}
	if _, err := f.svc.Stats(ctx, 2, l.Code); !errors.Is(err, domain.ErrLinkNotFound) {
		t.Fatalf("non-owner must not read stats, got %v", err)
	}
}
