package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger ghi access log. Dùng route pattern (c.FullPath) thay vì path thật
// để mã chia sẻ — vốn là "chìa khoá" xem bài — không bị lộ trong log.
func RequestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		reqID := c.GetHeader("X-Request-Id")
		if reqID == "" {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			reqID = hex.EncodeToString(b)
		}
		c.Header("X-Request-Id", reqID)

		c.Next()

		route := c.FullPath()
		if route == "/healthz" || route == "/readyz" || route == "/metrics" {
			return
		}
		log.Info("http request",
			"request_id", reqID,
			"method", c.Request.Method,
			"route", route,
			"status", c.Writer.Status(),
			"duration_ms", float64(time.Since(start).Microseconds())/1000,
		)
	}
}
