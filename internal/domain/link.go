// Package domain chứa entity và quy tắc nghiệp vụ cốt lõi.
// Không phụ thuộc framework, DB, cache hay HTTP — chỉ dùng thư viện chuẩn.
package domain

import "time"

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

func (s Status) Valid() bool { return s == StatusActive || s == StatusDisabled }

// ResourceType là loại bài làm được phép chia sẻ.
type ResourceType string

const (
	ResourceWriting  ResourceType = "writing_submission"
	ResourceSpeaking ResourceType = "speaking_submission"
	ResourceTest     ResourceType = "test_result"
)

func (t ResourceType) Valid() bool {
	switch t {
	case ResourceWriting, ResourceSpeaking, ResourceTest:
		return true
	}
	return false
}

// Link là entity link chia sẻ: https://youpass.vn/s/{Code}
type Link struct {
	ID           int64
	Code         string
	OwnerID      int64
	ResourceType ResourceType
	ResourceID   int64
	Status       Status
	ExpiresAt    *time.Time
	DeletedAt    *time.Time
	ViewCount    int64
	CreatedAt    time.Time
}

func NewLink(code string, ownerID int64, rt ResourceType, resourceID int64, expiresAt *time.Time) *Link {
	return &Link{
		Code:         code,
		OwnerID:      ownerID,
		ResourceType: rt,
		ResourceID:   resourceID,
		Status:       StatusActive,
		ExpiresAt:    expiresAt,
	}
}

// Accessible: link chỉ xem được khi đang bật, chưa xoá và chưa hết hạn.
func (l *Link) Accessible(now time.Time) error {
	if l.DeletedAt != nil || l.Status != StatusActive {
		return ErrLinkGone
	}
	if l.ExpiresAt != nil && now.After(*l.ExpiresAt) {
		return ErrLinkGone
	}
	return nil
}

func (l *Link) IsOwnedBy(userID int64) bool { return userID > 0 && l.OwnerID == userID }

// CountsViewFrom: không tính lượt chủ bài tự xem và crawler lấy preview (Facebook, Zalo...).
func (l *Link) CountsViewFrom(v Viewer) bool { return !l.IsOwnedBy(v.UserID) && !v.IsBot() }
