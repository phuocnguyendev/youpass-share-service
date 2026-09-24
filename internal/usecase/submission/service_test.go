package submission_test

import (
	"context"
	"errors"
	"testing"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/submission"
)

type fakeRepo struct{ owner map[int64]int64 }

func (r *fakeRepo) SoftDelete(_ context.Context, userID, id int64) (domain.ResourceType, error) {
	if r.owner[id] != userID {
		return "", domain.ErrSubmissionNotFound
	}
	delete(r.owner, id)
	return domain.ResourceWriting, nil
}

type fakeCache struct{ invalidated []int64 }

func (c *fakeCache) Invalidate(_ context.Context, _ domain.ResourceType, id int64) error {
	c.invalidated = append(c.invalidated, id)
	return nil
}

func TestDelete_InvalidatesSnapshot(t *testing.T) {
	repo := &fakeRepo{owner: map[int64]int64{101: 1}}
	cache := &fakeCache{}
	svc := submission.NewService(repo, cache)

	if err := svc.Delete(context.Background(), 2, 101); !errors.Is(err, domain.ErrSubmissionNotFound) {
		t.Fatalf("non-owner: expected ErrSubmissionNotFound, got %v", err)
	}
	if len(cache.invalidated) != 0 {
		t.Fatal("must not invalidate when delete fails")
	}

	if err := svc.Delete(context.Background(), 1, 101); err != nil {
		t.Fatal(err)
	}
	if len(cache.invalidated) != 1 || cache.invalidated[0] != 101 {
		t.Fatalf("expected snapshot 101 invalidated, got %v", cache.invalidated)
	}
}
