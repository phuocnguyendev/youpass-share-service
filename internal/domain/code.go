package domain

import "crypto/rand"

const (
	codeAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	CodeLength   = 8
	// 248 = 62 * 4 → bỏ byte >= 248 để mỗi ký tự có xác suất như nhau (tránh modulo bias).
	maxUnbiasedByte = 248
)

// NewCode sinh mã 8 ký tự Base62 bằng crypto/rand: không đoán được, không dò tuần tự được.
// Không gian mã 62^8 ≈ 2.18 × 10^14.
func NewCode() (string, error) {
	out := make([]byte, 0, CodeLength)
	buf := make([]byte, CodeLength*2)

	for len(out) < CodeLength {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b >= maxUnbiasedByte {
				continue
			}
			out = append(out, codeAlphabet[b%62])
			if len(out) == CodeLength {
				break
			}
		}
	}
	return string(out), nil
}

// ValidCode loại request rác trước khi chạm cache/DB.
func ValidCode(s string) bool {
	if len(s) != CodeLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
			return false
		}
	}
	return true
}
