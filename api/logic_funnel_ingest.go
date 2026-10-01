package center

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/viper"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm/clause"
)

const (
	funnelQueueCap      = 10000
	funnelBatchSize     = 100
	funnelFlushInterval = 2 * time.Second

	funnelSeenCap       = 10000
	funnelDropWarnEvery = time.Minute
)

// funnelEnabled 是总开关：viper "funnel.enabled"，未设置 = 开启。
func funnelEnabled() bool {
	if !viper.IsSet("funnel.enabled") {
		return true
	}
	return viper.GetBool("funnel.enabled")
}

// funnelQueue 是进程内有界队列 + 惰性启动的单写入 goroutine。
type funnelQueue struct {
	ch        chan FunnelEvent
	flushReq  chan chan struct{}
	startOnce sync.Once
	dropped   atomic.Int64
	lastWarn  atomic.Int64 // unix nano
}

func newFunnelQueue(capacity int) *funnelQueue {
	return &funnelQueue{
		ch:       make(chan FunnelEvent, capacity),
		flushReq: make(chan chan struct{}),
	}
}

// funnelQ 是测试接缝：测试可换成不消费的小队列。
var funnelQ = newFunnelQueue(funnelQueueCap)

// funnelEnqueue 非阻塞入队。未启用或队列满返回 false，绝不等待 DB。
func funnelEnqueue(ev FunnelEvent) bool {
	if !funnelEnabled() {
		return false
	}
	q := funnelQ
	q.startOnce.Do(func() { go q.run() })
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now()
	}
	select {
	case q.ch <- ev:
		return true
	default:
		q.noteDrop()
		return false
	}
}

func (q *funnelQueue) noteDrop() {
	q.dropped.Add(1)
	now := time.Now().UnixNano()
	last := q.lastWarn.Load()
	if now-last < int64(funnelDropWarnEvery) || !q.lastWarn.CompareAndSwap(last, now) {
		return
	}
	n := q.dropped.Swap(0)
	log.Warnf(context.Background(), "[Funnel] dropped %d events (queue full)", n)
}

func (q *funnelQueue) run() {
	batch := make([]FunnelEvent, 0, funnelBatchSize)
	ticker := time.NewTicker(funnelFlushInterval)
	defer ticker.Stop()
	flush := func() {
		if len(batch) > 0 {
			writeFunnelBatch(batch)
			batch = batch[:0]
		}
	}
	for {
		select {
		case ev := <-q.ch:
			batch = append(batch, ev)
			if len(batch) >= funnelBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case ack := <-q.flushReq:
			for drained := false; !drained; {
				select {
				case ev := <-q.ch:
					batch = append(batch, ev)
					if len(batch) >= funnelBatchSize {
						flush()
					}
				default:
					drained = true
				}
			}
			flush()
			close(ack)
		}
	}
}

func writeFunnelBatch(batch []FunnelEvent) {
	// Eid 唯一索引冲突 = 重复投递，忽略；分析写失败只记日志，不影响任何产品路径。
	err := db.Get().Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(batch, funnelBatchSize).Error
	if err != nil {
		log.Warnf(context.Background(), "[Funnel] write %d events failed: %v", len(batch), err)
	}
}

// funnelFlushForTest 同步写完队列里目前所有事件（仅测试用）。
func funnelFlushForTest() {
	q := funnelQ
	q.startOnce.Do(func() { go q.run() })
	ack := make(chan struct{})
	q.flushReq <- ack
	<-ack
}

var (
	funnelSeenMu sync.Mutex
	funnelSeen   = map[funnelSeenKey]struct{}{}
)

type funnelSeenKey struct {
	kind, anonID string
	userID       uint64
}

// linkFunnelIdentity 记录 匿名身份 ↔ 用户。幂等；失败只 warn。
func linkFunnelIdentity(ctx context.Context, kind, anonID string, userID uint64, brand Brand) {
	if anonID == "" || userID == 0 || !funnelEnabled() {
		return
	}
	key := funnelSeenKey{kind, anonID, userID}
	funnelSeenMu.Lock()
	_, seen := funnelSeen[key]
	funnelSeenMu.Unlock()
	if seen {
		return
	}
	row := FunnelIdentity{Kind: kind, AnonID: anonID, UserID: userID, Brand: string(brand)}
	if err := db.Get().Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		log.Warnf(ctx, "[Funnel] link identity failed: %v", err)
		return
	}
	funnelSeenMu.Lock()
	if len(funnelSeen) >= funnelSeenCap {
		funnelSeen = map[funnelSeenKey]struct{}{}
	}
	funnelSeen[key] = struct{}{}
	funnelSeenMu.Unlock()
}
