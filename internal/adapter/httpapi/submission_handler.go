package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/httpapi/middleware"
)

type submissionHandler struct {
	uc  SubmissionUseCase
	log *slog.Logger
}

// delete: xoá bài gốc → mọi link chia sẻ của bài trả 410 ngay.
func (h *submissionHandler) delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID"})
		return
	}
	if err := h.uc.Delete(c.Request.Context(), middleware.UserID(c), id); err != nil {
		writeError(c, h.log, err)
		return
	}
	c.Status(http.StatusNoContent)
}
