package share

import (
	"time"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
)

type CreateInput struct {
	OwnerID      int64
	ResourceType domain.ResourceType
	ResourceID   int64
	ExpiresAt    *time.Time
}

// SharedView là dữ liệu công khai trả cho người xem — không có email hay id nội bộ.
type SharedView struct {
	Code      string
	OwnerName string
	Type      domain.ResourceType
	Title     string
	Content   string
	BandScore float32
	Feedback  string
	SharedAt  time.Time
}

type Stats struct {
	TotalViews  int64
	UniqueToday int64
}
