package middleware

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type Limiter interface {
	Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, error)
}

// RateLimit theo IP. Limiter lỗi → fail-open (không chặn người dùng thật khi Redis gặp sự cố).
func RateLimit(l Limiter, scope string, limit int64, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		allowed, err := l.Allow(c.Request.Context(), scope+":"+c.ClientIP(), limit, window)
		if err == nil && !allowed {
			c.Header("Retry-After", strconv.Itoa(int(window.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"code": "RATE_LIMITED"})
			return
		}
		c.Next()
	}
}
