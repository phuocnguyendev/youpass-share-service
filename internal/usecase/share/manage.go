package share

import (
	"context"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
)

// SetStatus tắt/bật link. Người không phải chủ nhận ErrLinkNotFound (không lộ việc link tồn tại).
func (s *Service) SetStatus(ctx context.Context, ownerID int64, code string, st domain.Status) (*domain.Link, error) {
	if !st.Valid() {
		return nil, domain.ErrInvalidStatus
	}
	link, err := s.links.UpdateStatus(ctx, code, ownerID, st)
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, code)
	return link, nil
}

// Delete xoá mềm link. Mã đã xoá không bao giờ được cấp lại.
func (s *Service) Delete(ctx context.Context, ownerID int64, code string) error {
	if err := s.links.SoftDelete(ctx, code, ownerID); err != nil {
		return err
	}
	s.invalidate(ctx, code)
	return nil
}

// invalidate chạy SAU khi repository commit. Lỗi cache không làm hỏng thao tác của người dùng
// (TTL là giới hạn cuối); production: đẩy vào outbox để retry.
func (s *Service) invalidate(ctx context.Context, code string) {
	if err := s.cache.Invalidate(ctx, code); err != nil {
		s.log.Error("share: cache invalidate failed", "err", err)
	}
}

// Stats = view đã ghi DB + view còn chờ trong tracker (gần real-time).
func (s *Service) Stats(ctx context.Context, ownerID int64, code string) (*Stats, error) {
	link, err := s.links.FindOwned(ctx, code, ownerID)
	if err != nil {
		return nil, err
	}
	pending, uniqueToday, err := s.views.Pending(ctx, link.ID)
	if err != nil {
		return nil, err
	}
	return &Stats{TotalViews: link.ViewCount + pending, UniqueToday: uniqueToday}, nil
}
