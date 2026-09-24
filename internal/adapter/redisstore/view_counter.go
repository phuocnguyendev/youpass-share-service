package redisstore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/phuocnguyendev/youpass-share-service/internal/usecase/share"
)

const (
	eventBuffer   = 10_000
	dedupeWindow  = 30 * time.Minute
	uniqueTTL     = 30 * 24 * time.Hour
	flushBatch    = 1000
	dirtyKey      = "share:dirty"
	recordTimeout = time.Second
	finalFlushTTL = 5 * time.Second
)

// Hash tag {id}: mọi key của 1 link nằm cùng slot khi chạy Redis Cluster (Lua đa key hợp lệ).
func viewsKey(id int64) string                { return fmt.Sprintf("share:{%d}:views", id) }
func seenKey(id int64, visitor string) string { return fmt.Sprintf("share:{%d}:seen:%s", id, visitor) }
func uniqueKey(id int64, day string) string   { return fmt.Sprintf("share:{%d}:uv:%s", id, day) }

// trackScript: dedupe + INCR + HyperLogLog trong 1 round-trip, atomic.
// Trả 1 nếu view được tính, 0 nếu cùng người xem đã được tính trong cửa sổ dedupe.
var trackScript = redis.NewScript(`
local counted = 0
if redis.call('SET', KEYS[1], '1', 'NX', 'EX', ARGV[1]) then
  redis.call('INCR', KEYS[2])
  counted = 1
end
redis.call('PFADD', KEYS[3], ARGV[2])
redis.call('EXPIRE', KEYS[3], ARGV[3])
return counted
`)

type viewEvent struct {
	linkID  int64
	visitor string
}

// ViewCounter cài đặt share.ViewTracker:
// Track → buffered channel → worker pool → Redis (Lua) → flusher định kỳ → ViewStore (PostgreSQL).
type ViewCounter struct {
	rdb           redis.UniversalClient
	store         share.ViewStore
	log           *slog.Logger
	events        chan viewEvent
	flushInterval time.Duration
	now           func() time.Time
}

func NewViewCounter(rdb redis.UniversalClient, store share.ViewStore, log *slog.Logger, flushInterval time.Duration) *ViewCounter {
	return &ViewCounter{
		rdb: rdb, store: store, log: log,
		events:        make(chan viewEvent, eventBuffer),
		flushInterval: flushInterval,
		now:           time.Now,
	}
}

var _ share.ViewTracker = (*ViewCounter)(nil)

// Track không bao giờ block request. Buffer đầy → chấp nhận mất vài view thay vì làm chậm API.
func (vc *ViewCounter) Track(linkID int64, visitor string) {
	select {
	case vc.events <- viewEvent{linkID: linkID, visitor: visitor}:
	default:
		viewsDropped.Inc()
	}
}

// Run chạy worker pool + flusher. Khi ctx bị huỷ: drain buffer rồi flush lần cuối (graceful shutdown).
func (vc *ViewCounter) Run(ctx context.Context, workers int) {
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case e := <-vc.events:
					vc.handle(e)
				case <-ctx.Done():
					for {
						select {
						case e := <-vc.events:
							vc.handle(e)
						default:
							return
						}
					}
				}
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(vc.flushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := vc.flush(ctx); err != nil {
					vc.log.Error("redisstore: flush views failed", "err", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	wg.Wait()

	final, cancel := context.WithTimeout(context.Background(), finalFlushTTL)
	defer cancel()
	if n, err := vc.flush(final); err != nil {
		vc.log.Error("redisstore: final flush failed", "err", err)
	} else {
		vc.log.Info("redisstore: view counter stopped", "final_flushed", n)
	}
}

func (vc *ViewCounter) handle(e viewEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), recordTimeout)
	defer cancel()
	if _, err := vc.record(ctx, e.linkID, e.visitor); err != nil {
		vc.log.Warn("redisstore: record view failed", "link_id", e.linkID, "err", err)
	}
}

// record trả true nếu view được tính (không bị dedupe).
func (vc *ViewCounter) record(ctx context.Context, linkID int64, visitor string) (bool, error) {
	day := vc.now().Format("20060102")
	keys := []string{seenKey(linkID, visitor), viewsKey(linkID), uniqueKey(linkID, day)}

	counted, err := trackScript.Run(ctx, vc.rdb, keys,
		int(dedupeWindow.Seconds()), visitor, int(uniqueTTL.Seconds())).Int()
	if err != nil {
		return false, err
	}
	if counted != 1 {
		return false, nil
	}
	return true, vc.rdb.SAdd(ctx, dirtyKey, linkID).Err()
}

// flush chuyển view từ Redis xuống ViewStore theo lô.
//   - SPOP: nhiều instance cùng chạy không flush trùng 1 link.
//   - GETDEL: lấy và reset counter atomic.
//   - Ghi DB lỗi → INCRBY trả lại Redis → không mất view.
func (vc *ViewCounter) flush(ctx context.Context) (int64, error) {
	var total int64

	for {
		ids, err := vc.rdb.SPopN(ctx, dirtyKey, flushBatch).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}

		pipe := vc.rdb.Pipeline()
		cmds := make(map[int64]*redis.StringCmd, len(ids))
		for _, raw := range ids {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				continue
			}
			cmds[id] = pipe.GetDel(ctx, viewsKey(id))
		}
		_, _ = pipe.Exec(ctx) // lỗi được xử lý theo từng lệnh bên dưới

		deltas := make(map[int64]int64, len(cmds))
		var retry []any
		for id, cmd := range cmds {
			n, err := cmd.Int64()
			switch {
			case err == nil && n > 0:
				deltas[id] = n
			case err == nil || errors.Is(err, redis.Nil):
				// không có view mới
			default:
				retry = append(retry, id) // lệnh lỗi → counter chưa bị xoá → xử lý lại lần sau
			}
		}
		if len(retry) > 0 {
			vc.rdb.SAdd(ctx, dirtyKey, retry...)
		}

		if len(deltas) > 0 {
			if err := vc.store.AddViews(ctx, deltas); err != nil {
				flushErrors.Inc()
				vc.restore(ctx, deltas)
				return total, err
			}
			var sum int64
			for _, n := range deltas {
				sum += n
			}
			total += sum
			viewsFlushed.Add(float64(sum))
		}

		if len(ids) < flushBatch {
			return total, nil
		}
	}
}

func (vc *ViewCounter) restore(ctx context.Context, deltas map[int64]int64) {
	pipe := vc.rdb.Pipeline()
	for id, n := range deltas {
		pipe.IncrBy(ctx, viewsKey(id), n)
		pipe.SAdd(ctx, dirtyKey, id)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		vc.log.Error("redisstore: restore view counters failed", "err", err)
	}
}

func (vc *ViewCounter) Pending(ctx context.Context, linkID int64) (pending, uniqueToday int64, err error) {
	pipe := vc.rdb.Pipeline()
	p := pipe.Get(ctx, viewsKey(linkID))
	u := pipe.PFCount(ctx, uniqueKey(linkID, vc.now().Format("20060102")))
	_, _ = pipe.Exec(ctx)

	pending, err = p.Int64()
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0, 0, err
	}
	uniqueToday, err = u.Result()
	if err != nil {
		return 0, 0, err
	}
	return pending, uniqueToday, nil
}
