package share

import (
	"io"
	"log/slog"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/phuocnguyendev/youpass-share-service/internal/domain"
)

const (
	dbTimeout    = 2 * time.Second
	maxCodeRetry = 3
)

type Deps struct {
	Links   LinkRepository
	Cache   LinkCache
	Content ContentReader
	Views   ViewTracker
	BaseURL string // https://youpass.vn/s/
	Logger  *slog.Logger
	NewCode CodeGenerator // mặc định domain.NewCode
	Clock   Clock         // mặc định time.Now
}

// Service cài đặt các use case: Create, Resolve, SetStatus, Delete, Stats.
type Service struct {
	links   LinkRepository
	cache   LinkCache
	content ContentReader
	views   ViewTracker
	baseURL string
	log     *slog.Logger
	newCode CodeGenerator
	now     Clock
	sf      singleflight.Group
}

func NewService(d Deps) *Service {
	s := &Service{
		links: d.Links, cache: d.Cache, content: d.Content, views: d.Views,
		baseURL: d.BaseURL, log: d.Logger, newCode: d.NewCode, now: d.Clock,
	}
	if s.log == nil {
		s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if s.newCode == nil {
		s.newCode = domain.NewCode
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

func (s *Service) URL(code string) string { return s.baseURL + code }
