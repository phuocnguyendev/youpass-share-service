package share

import (
	"context"
	"errors"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
)

// Create tạo link chia sẻ, trả (link, created, err).
// Idempotent: bài đã có link còn sống thì trả link cũ với created=false.
func (s *Service) Create(ctx context.Context, in CreateInput) (*domain.Link, bool, error) {
	if !in.ResourceType.Valid() {
		return nil, false, domain.ErrInvalidResourceType
	}

	ok, err := s.content.IsOwner(ctx, in.ResourceType, in.ResourceID, in.OwnerID)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, domain.ErrForbidden
	}

	existing, err := s.links.FindByResource(ctx, in.ResourceType, in.ResourceID)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, domain.ErrLinkNotFound) {
		return nil, false, err
	}

	for attempt := 0; attempt < maxCodeRetry; attempt++ {
		code, err := s.newCode()
		if err != nil {
			return nil, false, err
		}
		link := domain.NewLink(code, in.OwnerID, in.ResourceType, in.ResourceID, in.ExpiresAt)

		err = s.links.Insert(ctx, link)
		switch {
		case err == nil:
			return link, true, nil
		case errors.Is(err, ErrDuplicateCode):
			continue // trùng mã (cực hiếm) → sinh mã khác
		case errors.Is(err, ErrAlreadyShared):
			// 2 request tạo đồng thời: request thua trả link của request thắng
			existing, err := s.links.FindByResource(ctx, in.ResourceType, in.ResourceID)
			return existing, false, err
		default:
			return nil, false, err
		}
	}
	return nil, false, ErrCodeExhausted
}
