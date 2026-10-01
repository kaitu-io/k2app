package center

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-sql-driver/mysql"
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
	idCh      chan FunnelIdentity
	flushReq  chan chan struct{}
	startOnce sync.Once
	dropped   atomic.Int64
	lastWarn  atomic.Int64 // unix nano
}

func newFunnelQueue(capacity int) *funnelQueue {
	return &funnelQueue{
		ch:       make(chan FunnelEvent, capacity),
		idCh:     make(chan FunnelIdentity, capacity),
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

// run 是写入 goroutine 的守护循环：循环体 panic 时记日志并重启，
// 分析代码的故障绝不能带崩进程。
func (q *funnelQueue) run() {
	for {
		q.loop()
	}
}

func (q *funnelQueue) loop() {
	batch := make([]FunnelEvent, 0, funnelBatchSize)
	defer func() {
		if r := recover(); r != nil {
			log.Errorf(context.Background(), "[Funnel] writer panic recovered: %v (batch of %d lost)", r, len(batch))
		}
	}()
	ticker := time.NewTicker(funnelFlushInterval)
	defer ticker.Stop()
	flush := func() {
		if len(batch) > 0 {
			b := batch
			batch = batch[:0]
			writeFunnelBatch(b)
		}
	}
	for {
		select {
		case ev := <-q.ch:
			batch = append(batch, ev)
			if len(batch) >= funnelBatchSize {
				flush()
			}
		case id := <-q.idCh:
			writeFunnelIdentity(id)
		case <-ticker.C:
			flush()
		case ack := <-q.flushReq:
			func() {
				defer close(ack)
				for drained := false; !drained; {
					select {
					case ev := <-q.ch:
						batch = append(batch, ev)
						if len(batch) >= funnelBatchSize {
							flush()
						}
					case id := <-q.idCh:
						writeFunnelIdentity(id)
					default:
						drained = true
					}
				}
				flush()
			}()
		}
	}
}

// funnelRowDataErrors：某一行的数据本身写不进去的 MySQL 错误号（其余行是好的）。
var funnelRowDataErrors = map[uint16]bool{
	1048: true, // column cannot be null
	1264: true, // out of range value
	1265: true, // data truncated
	1292: true, // truncated incorrect value
	1366: true, // incorrect string value
	1406: true, // data too long
}

// funnelIsRowDataError：错误是否出在某一行的数据上。连接断开、超时、锁等待、表不存在等
// 都不是——那些错误逐行重试只会把同一个错误再来 N 遍。
func funnelIsRowDataError(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && funnelRowDataErrors[me.Number]
}

// funnelInsertEvents 写一批事件；Eid 唯一索引冲突 = 重复投递，忽略。包级变量仅为测试注入失败。
// 每次调用都从 db.Get() 新起一条链：GORM 链式对象复用会带上上次的 Statement 状态。
var funnelInsertEvents = func(rows []FunnelEvent) error {
	return db.Get().Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, funnelBatchSize).Error
}

// writeFunnelBatch：分析写失败只记日志，不影响任何产品路径。
//   - 某一行数据有问题（如超长字段）：逐行重试，只丢坏行，不拖垮同批好行。
//   - 其它错误（连接级故障等）：整批记一条告警后丢弃。故障期间每批只有一行日志，而不是 101 行。
func writeFunnelBatch(batch []FunnelEvent) {
	err := funnelInsertEvents(batch)
	if err == nil {
		return
	}
	if !funnelIsRowDataError(err) {
		log.Warnf(context.Background(), "[Funnel] batch write failed, dropped %d events: %v", len(batch), err)
		return
	}
	log.Warnf(context.Background(), "[Funnel] batch write of %d events hit a bad row (%v); retrying row by row", len(batch), err)
	for i := range batch {
		err := funnelInsertEvents(batch[i : i+1])
		if err == nil {
			continue
		}
		if !funnelIsRowDataError(err) {
			log.Warnf(context.Background(), "[Funnel] row-by-row retry aborted, dropped %d remaining events: %v", len(batch)-i, err)
			return
		}
		log.Warnf(context.Background(), "[Funnel] dropped bad event %q: %v", batch[i].Event, err)
	}
}

func writeFunnelIdentity(row FunnelIdentity) {
	if err := db.Get().Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		log.Warnf(context.Background(), "[Funnel] link identity failed: %v", err) // 未标记 seen，下次仍会重试
		return
	}
	funnelMarkSeen(funnelSeenKey{row.Kind, row.AnonID, row.UserID})
}

// funnelFlushForTest 同步写完队列里目前所有事件与身份关联（仅测试用）。
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

func funnelMarkSeen(key funnelSeenKey) {
	funnelSeenMu.Lock()
	defer funnelSeenMu.Unlock()
	if len(funnelSeen) >= funnelSeenCap {
		funnelSeen = map[funnelSeenKey]struct{}{}
	}
	funnelSeen[key] = struct{}{}
}

// linkFunnelIdentity 记录 匿名身份 ↔ 用户。幂等、非阻塞：
// 调用方只做进程内已见检查，写库交给写入 goroutine（队列满即丢，不等待）。
// ctx 仅为签名兼容；写入是异步的，不使用请求 ctx。
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
	q := funnelQ
	q.startOnce.Do(func() { go q.run() })
	select {
	case q.idCh <- FunnelIdentity{Kind: kind, AnonID: anonID, UserID: userID, Brand: string(brand)}:
	default:
		// 队列满：丢弃，下次调用还会重试（未标记 seen）
	}
}
