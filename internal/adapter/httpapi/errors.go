package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
)

var resolveDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "share_resolve_duration_seconds",
	Help:    "Thời gian resolve link chia sẻ, theo HTTP status.",
	Buckets: []float64{.001, .0025, .005, .01, .02, .05, .1, .25, .5, 1},
}, []string{"status"})

// errorMappings là nơi DUY NHẤT map lỗi domain → HTTP status + mã lỗi.
var errorMappings = []struct {
	err     error
	status  int
	code    string
	message string
}{
	{domain.ErrLinkNotFound, http.StatusNotFound, "SHARE_NOT_FOUND", ""},
	{domain.ErrSubmissionNotFound, http.StatusNotFound, "SUBMISSION_NOT_FOUND", ""},
	{domain.ErrLinkGone, http.StatusGone, "SHARE_UNAVAILABLE", "Bài làm này đã được tắt chia sẻ hoặc đã bị xoá"},
	{domain.ErrForbidden, http.StatusForbidden, "FORBIDDEN", ""},
	{domain.ErrInvalidResourceType, http.StatusBadRequest, "INVALID_RESOURCE_TYPE", ""},
	{domain.ErrInvalidStatus, http.StatusBadRequest, "INVALID_STATUS", ""},
}

func statusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	for _, m := range errorMappings {
		if errors.Is(err, m.err) {
			return m.status
		}
	}
	return http.StatusInternalServerError
}

func writeError(c *gin.Context, log *slog.Logger, err error) {
	for _, m := range errorMappings {
		if errors.Is(err, m.err) {
			body := gin.H{"code": m.code}
			if m.message != "" {
				body["message"] = m.message
			}
			c.JSON(m.status, body)
			return
		}
	}
	log.Error("httpapi: request failed", "route", c.FullPath(), "err", err)
	c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR"})
}
