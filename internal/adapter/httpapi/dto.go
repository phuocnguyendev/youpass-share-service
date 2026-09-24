package httpapi

import (
	"time"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
)

/* ---------- Request ---------- */

type createShareRequest struct {
	ResourceType  string `json:"resourceType" binding:"required"`
	ResourceID    int64  `json:"resourceId" binding:"required,gt=0"`
	ExpiresInDays int    `json:"expiresInDays" binding:"omitempty,min=1,max=365"`
}

type updateStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=active disabled"`
}

/* ---------- Response ---------- */

type linkResponse struct {
	Code         string     `json:"code"`
	URL          string     `json:"url"`
	Status       string     `json:"status"`
	ResourceType string     `json:"resourceType"`
	ResourceID   int64      `json:"resourceId"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

func toLinkResponse(l *domain.Link, url string) linkResponse {
	return linkResponse{
		Code: l.Code, URL: url, Status: string(l.Status),
		ResourceType: string(l.ResourceType), ResourceID: l.ResourceID,
		ExpiresAt: l.ExpiresAt, CreatedAt: l.CreatedAt,
	}
}

type sharedViewResponse struct {
	Code      string    `json:"code"`
	OwnerName string    `json:"ownerName"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	BandScore float32   `json:"bandScore"`
	Feedback  string    `json:"feedback"`
	SharedAt  time.Time `json:"sharedAt"`
}

func toSharedViewResponse(v *share.SharedView) sharedViewResponse {
	return sharedViewResponse{
		Code: v.Code, OwnerName: v.OwnerName, Type: string(v.Type), Title: v.Title,
		Content: v.Content, BandScore: v.BandScore, Feedback: v.Feedback, SharedAt: v.SharedAt,
	}
}

type statsResponse struct {
	TotalViews  int64 `json:"totalViews"`
	UniqueToday int64 `json:"uniqueToday"`
}
