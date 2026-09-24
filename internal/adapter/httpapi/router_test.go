package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/httpapi"
	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/memstore"
	"github.com/phuocnguyendev/youpass-share-service/internal/adapter/token"
	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
)

const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/537.36 Chrome/128.0 Safari/537.36"

type api struct {
	router *gin.Engine
	tokens *token.Manager
}

func newAPI(t *testing.T, rateLimit int64) *api {
	t.Helper()
	gin.SetMode(gin.TestMode)

	svc := share.NewService(share.Deps{
		Links:   memstore.NewLinkRepository(),
		Cache:   memstore.NewLinkCache(),
		Content: memstore.NewContentReader(),
		Views:   memstore.NewViewTracker(),
		BaseURL: "https://youpass.vn/s/",
	})
	tokens := token.NewManager("test-secret-0123456789abcdef0123456789", time.Minute)

	r, err := httpapi.NewRouter(httpapi.Deps{
		Share: svc, Tokens: tokens, Limiter: memstore.NewRateLimiter(), RateLimitPerMin: rateLimit,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &api{router: r, tokens: tokens}
}

func (a *api) do(t *testing.T, method, path string, userID int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", browserUA)
	if userID > 0 {
		signed, _, _ := a.tokens.Issue(userID)
		req.Header.Set("Authorization", "Bearer "+signed)
	}
	w := httptest.NewRecorder()
	a.router.ServeHTTP(w, req)
	return w
}

var writing101 = map[string]any{"resourceType": "writing_submission", "resourceId": 101}

func TestAPI_FullLifecycle(t *testing.T) {
	a := newAPI(t, 1000)

	w := a.do(t, http.MethodPost, "/api/v1/shares", 1, writing101)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var link struct{ Code, URL string }
	_ = json.Unmarshal(w.Body.Bytes(), &link)
	if !strings.HasSuffix(link.URL, "/s/"+link.Code) {
		t.Fatalf("unexpected url %q", link.URL)
	}
	if w := a.do(t, http.MethodPost, "/api/v1/shares", 1, writing101); w.Code != http.StatusOK {
		t.Fatalf("idempotent create: expected 200, got %d", w.Code)
	}

	w = a.do(t, http.MethodGet, "/api/v1/shares/"+link.Code, 0, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve: %d %s", w.Code, w.Body)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("Cache-Control = %q", cc)
	}
	if w.Header().Get("X-Robots-Tag") == "" {
		t.Fatal("missing X-Robots-Tag")
	}

	if w := a.do(t, http.MethodPatch, "/api/v1/shares/"+link.Code, 2, map[string]any{"status": "disabled"}); w.Code != http.StatusNotFound {
		t.Fatalf("non-owner disable: expected 404, got %d", w.Code)
	}
	if w := a.do(t, http.MethodPatch, "/api/v1/shares/"+link.Code, 1, map[string]any{"status": "disabled"}); w.Code != http.StatusOK {
		t.Fatalf("disable: %d", w.Code)
	}
	if w := a.do(t, http.MethodGet, "/api/v1/shares/"+link.Code, 0, nil); w.Code != http.StatusGone {
		t.Fatalf("after disable: expected 410, got %d", w.Code)
	}
	if w := a.do(t, http.MethodGet, "/s/"+link.Code, 0, nil); w.Code != http.StatusGone ||
		!strings.Contains(w.Body.String(), "không còn được chia sẻ") {
		t.Fatalf("page after disable: %d", w.Code)
	}

	if w := a.do(t, http.MethodDelete, "/api/v1/shares/"+link.Code, 1, nil); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := a.do(t, http.MethodGet, "/api/v1/shares/"+link.Code, 0, nil); w.Code != http.StatusGone {
		t.Fatalf("after delete: expected 410, got %d", w.Code)
	}
	if w := a.do(t, http.MethodGet, "/api/v1/shares/Zz9Zz9Zz", 0, nil); w.Code != http.StatusNotFound {
		t.Fatalf("unknown code: expected 404, got %d", w.Code)
	}
}

func TestAPI_AuthErrors(t *testing.T) {
	a := newAPI(t, 1000)

	w := a.do(t, http.MethodPost, "/api/v1/shares", 0, writing101)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "TOKEN_MISSING") {
		t.Fatalf("expected 401 TOKEN_MISSING, got %d %s", w.Code, w.Body)
	}

	expired := token.NewManager("test-secret-0123456789abcdef0123456789", -time.Minute)
	signed, _, _ := expired.Issue(1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shares", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+signed)
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "TOKEN_EXPIRED") {
		t.Fatalf("expected 401 TOKEN_EXPIRED, got %d %s", rec.Code, rec.Body)
	}
}

func TestAPI_ValidationErrors(t *testing.T) {
	a := newAPI(t, 1000)
	if w := a.do(t, http.MethodPost, "/api/v1/shares", 1, map[string]any{"resourceType": "admin_secret", "resourceId": 1}); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown resource type: expected 400, got %d", w.Code)
	}
	if w := a.do(t, http.MethodPost, "/api/v1/shares", 2, writing101); w.Code != http.StatusForbidden {
		t.Fatalf("non-owner create: expected 403, got %d", w.Code)
	}
}

func TestAPI_RateLimit(t *testing.T) {
	a := newAPI(t, 5)
	var limited int
	for i := 0; i < 8; i++ {
		if w := a.do(t, http.MethodGet, "/api/v1/shares/Zz9Zz9Zz", 0, nil); w.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited != 3 {
		t.Fatalf("expected 3 rate-limited (limit 5 of 8), got %d", limited)
	}
}
