package redisstore

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	cacheTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "share_link_cache_total",
		Help: "Kết quả tra cache link: hit | miss | negative_hit | error.",
	}, []string{"result"})

	viewsDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "share_views_dropped_total",
		Help: "Số view bị bỏ do buffer đầy.",
	})

	viewsFlushed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "share_views_flushed_total",
		Help: "Số view đã flush từ Redis xuống PostgreSQL.",
	})

	flushErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "share_views_flush_errors_total",
		Help: "Số lần flush view thất bại (counter đã được trả lại Redis).",
	})
)
