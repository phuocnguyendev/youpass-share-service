// Package submission chứa use case của bài làm liên quan tới chia sẻ.
package submission

import (
	"context"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
)

type Repository interface {
	// SoftDelete xoá bài của chính user, trả loại bài; domain.ErrSubmissionNotFound nếu không có quyền/không tồn tại.
	SoftDelete(ctx context.Context, userID, submissionID int64) (domain.ResourceType, error)
}

type SnapshotInvalidator interface {
	Invalidate(ctx context.Context, rt domain.ResourceType, submissionID int64) error
}

type Service struct {
	repo  Repository
	cache SnapshotInvalidator
}

func NewService(repo Repository, cache SnapshotInvalidator) *Service {
	return &Service{repo: repo, cache: cache}
}

// Delete xoá bài làm rồi invalidate snapshot → mọi link chia sẻ của bài trả 410 ngay.
func (s *Service) Delete(ctx context.Context, userID, submissionID int64) error {
	rt, err := s.repo.SoftDelete(ctx, userID, submissionID)
	if err != nil {
		return err
	}
	return s.cache.Invalidate(ctx, rt, submissionID)
}
