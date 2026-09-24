// Package token cấp và xác thực access token (JWT HS256).
package token

import (
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuer = "youpass"

var (
	ErrInvalid = errors.New("token: invalid")
	ErrExpired = errors.New("token: expired")
)

type Manager struct {
	secret []byte
	ttl    time.Duration
}

func NewManager(secret string, ttl time.Duration) *Manager {
	return &Manager{secret: []byte(secret), ttl: ttl}
}

func (m *Manager) Issue(userID int64) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(m.ttl)
	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		Issuer:    issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	return signed, exp, err
}

// Verify trả userID; ErrExpired khi hết hạn (FE dựa vào đây để refresh), ErrInvalid cho mọi lỗi khác.
func (m *Manager) Verify(raw string) (int64, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return m.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
	)
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return 0, ErrExpired
	case err != nil:
		return 0, ErrInvalid
	}

	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalid
	}
	return id, nil
}
