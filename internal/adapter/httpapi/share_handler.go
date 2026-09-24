package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/httpapi/middleware"
	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
)

const (
	visitorCookie    = "yp_vid"
	visitorHeader    = "X-Visitor-Id" // Next.js SSR forward cookie ẩn danh của browser
	visitorCookieAge = 365 * 24 * time.Hour
)

type shareHandler struct {
	uc  ShareUseCase
	log *slog.Logger
}

/* ============================== Public ============================== */

func (h *shareHandler) resolve(c *gin.Context) {
	start := time.Now()
	setShareHeaders(c)

	view, err := h.uc.Resolve(c.Request.Context(), c.Param("code"), viewerFrom(c))
	resolveDuration.WithLabelValues(strconv.Itoa(statusOf(err))).Observe(time.Since(start).Seconds())
	if err != nil {
		writeError(c, h.log, err)
		return
	}
	c.JSON(http.StatusOK, toSharedViewResponse(view))
}

func (h *shareHandler) page(c *gin.Context) {
	start := time.Now()
	setShareHeaders(c)

	viewer := viewerFrom(c)
	if _, err := c.Cookie(visitorCookie); err != nil && c.GetHeader(visitorHeader) == "" {
		id := randomID()
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(visitorCookie, id, int(visitorCookieAge.Seconds()), "/", "", c.Request.TLS != nil, true)
		viewer.VisitorID = id
	}

	view, err := h.uc.Resolve(c.Request.Context(), c.Param("code"), viewer)
	status := statusOf(err)
	resolveDuration.WithLabelValues(strconv.Itoa(status)).Observe(time.Since(start).Seconds())
	if status == http.StatusInternalServerError {
		h.log.Error("httpapi: render share page failed", "err", err)
	}
	renderPage(c, status, view)
}

/* ============================== Owner ============================== */

func (h *shareHandler) create(c *gin.Context) {
	var req createShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST"})
		return
	}

	var expiresAt *time.Time
	if req.ExpiresInDays > 0 {
		t := time.Now().AddDate(0, 0, req.ExpiresInDays)
		expiresAt = &t
	}

	link, created, err := h.uc.Create(c.Request.Context(), share.CreateInput{
		OwnerID:      middleware.UserID(c),
		ResourceType: domain.ResourceType(req.ResourceType),
		ResourceID:   req.ResourceID,
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		writeError(c, h.log, err)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, toLinkResponse(link, h.uc.URL(link.Code)))
}

func (h *shareHandler) updateStatus(c *gin.Context) {
	var req updateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_STATUS"})
		return
	}

	link, err := h.uc.SetStatus(c.Request.Context(), middleware.UserID(c), c.Param("code"), domain.Status(req.Status))
	if err != nil {
		writeError(c, h.log, err)
		return
	}
	c.JSON(http.StatusOK, toLinkResponse(link, h.uc.URL(link.Code)))
}

func (h *shareHandler) delete(c *gin.Context) {
	if err := h.uc.Delete(c.Request.Context(), middleware.UserID(c), c.Param("code")); err != nil {
		writeError(c, h.log, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *shareHandler) stats(c *gin.Context) {
	st, err := h.uc.Stats(c.Request.Context(), middleware.UserID(c), c.Param("code"))
	if err != nil {
		writeError(c, h.log, err)
		return
	}
	c.JSON(http.StatusOK, statsResponse{TotalViews: st.TotalViews, UniqueToday: st.UniqueToday})
}

/* ============================== Helpers ============================== */

// setShareHeaders: tắt share phải có hiệu lực ngay → cấm browser/CDN cache; không cho Google index bài làm.
func setShareHeaders(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Robots-Tag", "noindex, nofollow")
	c.Header("Referrer-Policy", "no-referrer")
}

func viewerFrom(c *gin.Context) domain.Viewer {
	vid := c.GetHeader(visitorHeader)
	if vid == "" {
		vid, _ = c.Cookie(visitorCookie)
	}
	if vid == "" {
		vid = c.ClientIP() + "|" + c.Request.UserAgent()
	}
	return domain.Viewer{UserID: middleware.UserID(c), VisitorID: vid, UserAgent: c.Request.UserAgent()}
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
