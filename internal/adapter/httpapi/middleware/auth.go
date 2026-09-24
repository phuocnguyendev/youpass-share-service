// Package middleware chứa Gin middleware: xác thực, rate limit, access log.
package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/token"
)

const userIDKey = "auth.userID"

type TokenVerifier interface {
	Verify(raw string) (int64, error)
}

// RequireAuth bắt buộc đăng nhập. Trả mã lỗi rõ ràng để FE biết khi nào cần refresh (TOKEN_EXPIRED).
func RequireAuth(v TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := bearer(c)
		if raw == "" {
			unauthorized(c, "TOKEN_MISSING")
			return
		}
		id, err := v.Verify(raw)
		if err != nil {
			code := "TOKEN_INVALID"
			if errors.Is(err, token.ErrExpired) {
				code = "TOKEN_EXPIRED"
			}
			unauthorized(c, code)
			return
		}
		c.Set(userIDKey, id)
		c.Next()
	}
}

// OptionalAuth parse token nếu có; token sai/hết hạn thì coi như khách, không chặn request.
func OptionalAuth(v TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if raw := bearer(c); raw != "" {
			if id, err := v.Verify(raw); err == nil {
				c.Set(userIDKey, id)
			}
		}
		c.Next()
	}
}

// UserID trả 0 nếu request chưa đăng nhập.
func UserID(c *gin.Context) int64 { return c.GetInt64(userIDKey) }

func bearer(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func unauthorized(c *gin.Context, code string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": code})
}
