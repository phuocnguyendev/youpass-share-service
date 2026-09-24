package share

import (
	"context"
	"errors"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
)

// Resolve mở link chia sẻ (public).
//
//	validate format → cache → (miss) singleflight → repository
//	→ kiểm tra quyền truy cập → snapshot bài làm → ghi nhận view (non-blocking)
func (s *Service) Resolve(ctx context.Context, code string, v domain.Viewer) (*SharedView, error) {
	if !domain.ValidCode(code) {
		return nil, domain.ErrLinkNotFound // không chạm cache/DB
	}

	link, err := s.getLink(ctx, code)
	if err != nil {
		return nil, err
	}
	if err := link.Accessible(s.now()); err != nil {
		return nil, err
	}

	snap, err := s.content.Snapshot(ctx, link.ResourceType, link.ResourceID)
	if errors.Is(err, domain.ErrSubmissionNotFound) {
		return nil, domain.ErrLinkGone // bài gốc đã bị xoá → link chết theo
	}
	if err != nil {
		return nil, err
	}

	if link.CountsViewFrom(v) {
		s.views.Track(link.ID, v.Key())
	}

	return &SharedView{
		Code:      link.Code,
		OwnerName: snap.OwnerName,
		Type:      link.ResourceType,
		Title:     snap.Title,
		Content:   snap.Content,
		BandScore: snap.BandScore,
		Feedback:  snap.Feedback,
		SharedAt:  link.CreatedAt,
	}, nil
}

// getLink: cache-aside + negative cache + singleflight chống cache stampede.
func (s *Service) getLink(ctx context.Context, code string) (*domain.Link, error) {
	link, err := s.cache.Get(ctx, code)
	switch {
	case err == nil:
		return link, nil
	case errors.Is(err, domain.ErrLinkNotFound):
		return nil, err // negative cache hit
	case !errors.Is(err, ErrCacheMiss):
		s.log.Warn("share: cache unavailable, fallback to repository", "err", err)
	}

	v, err, _ := s.sf.Do(code, func() (any, error) {
		// Không dùng ctx của request đầu tiên: client đó huỷ thì các request đang chờ chung không lỗi theo
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dbTimeout)
		defer cancel()

		link, err := s.links.FindByCode(dbCtx, code)
		if errors.Is(err, domain.ErrLinkNotFound) {
			if err := s.cache.SetNotFound(dbCtx, code); err != nil {
				s.log.Warn("share: set negative cache failed", "err", err)
			}
			return nil, err
		}
		if err != nil {
			return nil, err
		}
		if err := s.cache.Set(dbCtx, link); err != nil {
			s.log.Warn("share: set cache failed", "err", err)
		}
		return link, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*domain.Link), nil
}
