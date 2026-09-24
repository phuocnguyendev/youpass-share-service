package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// Viewer là người mở link chia sẻ.
type Viewer struct {
	UserID    int64  // 0 nếu chưa đăng nhập
	VisitorID string // cookie ẩn danh / X-Visitor-Id / IP + User-Agent
	UserAgent string
}

// Key định danh người xem để dedupe view. Hash để không lưu IP thô.
func (v Viewer) Key() string {
	if v.UserID > 0 {
		return "u" + strconv.FormatInt(v.UserID, 10)
	}
	sum := sha256.Sum256([]byte(v.VisitorID))
	return "a" + hex.EncodeToString(sum[:8])
}

var botSignatures = []string{
	"bot", "crawler", "spider", "preview", "facebookexternalhit",
	"zalo", "slackbot", "telegrambot", "whatsapp", "discordbot", "curl/", "wget/",
}

func (v Viewer) IsBot() bool {
	ua := strings.ToLower(strings.TrimSpace(v.UserAgent))
	if ua == "" {
		return true
	}
	for _, sig := range botSignatures {
		if strings.Contains(ua, sig) {
			return true
		}
	}
	return false
}
