// Package share chứa use case chia sẻ bài làm.
// Use case chỉ phụ thuộc domain và các port (interface) định nghĩa tại đây;
// adapter (PostgreSQL, Redis, HTTP) cài đặt các port này — dependency hướng vào trong.
package share

import (
	"context"
	"errors"
	"time"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
)

/* ============================== Output ports ============================== */

// LinkRepository là nguồn dữ liệu chính (source of truth) của link chia sẻ.
type LinkRepository interface {
	// Insert gán ID, CreatedAt. Trả ErrDuplicateCode hoặc ErrAlreadyShared khi vi phạm unique.
	Insert(ctx context.Context, l *domain.Link) error
	// FindByCode trả cả link đã tắt/xoá (để trả 410), domain.ErrLinkNotFound nếu không tồn tại.
	FindByCode(ctx context.Context, code string) (*domain.Link, error)
	// FindByResource chỉ trả link chưa xoá.
	FindByResource(ctx context.Context, rt domain.ResourceType, resourceID int64) (*domain.Link, error)
	// FindOwned / UpdateStatus / SoftDelete kiểm tra quyền sở hữu ngay trong truy vấn.
	FindOwned(ctx context.Context, code string, ownerID int64) (*domain.Link, error)
	UpdateStatus(ctx context.Context, code string, ownerID int64, st domain.Status) (*domain.Link, error)
	SoftDelete(ctx context.Context, code string, ownerID int64) error
}

// ViewStore nhận số view đã gom theo lô.
type ViewStore interface {
	AddViews(ctx context.Context, deltas map[int64]int64) error
}

// LinkCache lưu cả trạng thái link → link đã tắt cũng trả 410 ngay từ cache.
type LinkCache interface {
	// Get trả ErrCacheMiss khi chưa có, domain.ErrLinkNotFound khi có negative cache.
	Get(ctx context.Context, code string) (*domain.Link, error)
	Set(ctx context.Context, l *domain.Link) error
	SetNotFound(ctx context.Context, code string) error
	// Invalidate phải loại bỏ cả giá trị cũ do request đọc đồng thời ghi lại.
	Invalidate(ctx context.Context, code string) error
}

// ContentReader đọc bài làm từ module Submission.
type ContentReader interface {
	IsOwner(ctx context.Context, rt domain.ResourceType, resourceID, userID int64) (bool, error)
	// Snapshot trả domain.ErrSubmissionNotFound nếu bài đã bị xoá / chủ bài bị khoá.
	Snapshot(ctx context.Context, rt domain.ResourceType, resourceID int64) (*domain.Snapshot, error)
}

// ViewTracker ghi nhận lượt xem bất đồng bộ.
type ViewTracker interface {
	// Track KHÔNG được block request.
	Track(linkID int64, visitorKey string)
	// Pending trả số view chưa ghi xuống DB và số người xem duy nhất hôm nay.
	Pending(ctx context.Context, linkID int64) (pending, uniqueToday int64, err error)
}

type (
	CodeGenerator func() (string, error)
	Clock         func() time.Time
)

/* ============================== Lỗi hợp đồng port ============================== */

var (
	ErrCacheMiss     = errors.New("share: cache miss")
	ErrDuplicateCode = errors.New("share: duplicate code")
	ErrAlreadyShared = errors.New("share: resource already shared")
	ErrCodeExhausted = errors.New("share: could not allocate unique code")
)
