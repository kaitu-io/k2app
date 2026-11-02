package center

import (
	"context"
	"fmt"
	"sort"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	nodePresenceBucketSeconds = 300
	nodePresenceWindowSeconds = 300
	nodeMetricsFreshSeconds   = 600
	// Pressure 1.0 means CPU at the 80% ceiling or throughput at capacity M.
	nodeOverloadPressure = 0.9

	// Presence writes are bounded so a slow database can never pile observation
	// on top of the ingest path: at most this many in flight, each with a hard
	// deadline; anything beyond is dropped.
	nodePresenceMaxInFlight  = 4
	nodePresenceWriteTimeout = 3 * time.Second

	// Bucket aggregates only change when a bucket completes, so readers share one
	// computed copy for that long. The database is shared with other services.
	nodeBucketCacheSeconds = 2 * nodePresenceBucketSeconds
	nodeBucketReadTimeout  = 10 * time.Second
)

var (
	nodePresenceSlots  = make(chan struct{}, nodePresenceMaxInFlight)
	nodePresenceWriter = recordNodePresence
)

// Node status values. Only idle, normal and overloaded count towards balance:
// the others are expected to hold few or no devices.
const (
	nodeStatusOffline    = "offline"
	nodeStatusExhausted  = "exhausted"
	nodeStatusHidden     = "hidden"
	nodeStatusPrivate    = "private"
	nodeStatusOverloaded = "overloaded"
	nodeStatusIdle       = "idle"
	nodeStatusNormal     = "normal"
)

// Verdict values: imbalanced is the acceptance failure (idle and overloaded
// nodes at once); skewed means idle nodes exist while others carry devices, the
// strongest signal available until a node reports trustworthy load metrics.
const (
	nodeVerdictOK         = "ok"
	nodeVerdictSkewed     = "skewed"
	nodeVerdictImbalanced = "imbalanced"
)

func nodePresenceBucket(unix int64) int64 {
	return unix - unix%nodePresenceBucketSeconds
}

// dispatchNodePresence records a committed batch without touching the caller:
// it never blocks, returns nothing and contains panics. A dropped or failed
// write costs one sample and nothing else.
func dispatchNodePresence(ipv4 string, items []DeviceTrafficItem, userByUDID map[string]uint, now int64) {
	select {
	case nodePresenceSlots <- struct{}{}:
	default:
		log.Warnf(context.Background(), "[DEVTRAFFIC] presence dropped ip=%s: writers busy", ipv4)
		return
	}
	go func() {
		defer func() { <-nodePresenceSlots }()
		defer func() {
			if r := recover(); r != nil {
				log.Errorf(context.Background(), "[DEVTRAFFIC] presence panic ip=%s: %v", ipv4, r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), nodePresenceWriteTimeout)
		defer cancel()
		if err := nodePresenceWriter(ctx, ipv4, items, userByUDID, now); err != nil {
			log.Warnf(ctx, "[DEVTRAFFIC] presence ip=%s: %v", ipv4, err)
		}
	}()
}

// recordNodePresence upserts one batch into the 5-minute ledger. Unattributed
// traffic (empty udid) is skipped: it is not a device.
func recordNodePresence(ctx context.Context, ipv4 string, items []DeviceTrafficItem, userByUDID map[string]uint, now int64) error {
	bucket := nodePresenceBucket(now)
	rows := make([]NodeDevicePresence, 0, len(items))
	for _, d := range items {
		if d.UDID == "" {
			continue
		}
		rows = append(rows, NodeDevicePresence{
			Bucket: bucket, NodeIpv4: ipv4, UDID: d.UDID, SessionID: d.SessionID,
			UserID: userByUDID[d.UDID], RxBytes: d.Rx, TxBytes: d.Tx, LastSeenAt: now,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return db.Get().WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "bucket"}, {Name: "node_ipv4"}, {Name: "udid"}, {Name: "session_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"rx_bytes":     gorm.Expr("rx_bytes + VALUES(rx_bytes)"),
			"tx_bytes":     gorm.Expr("tx_bytes + VALUES(tx_bytes)"),
			"user_id":      gorm.Expr("VALUES(user_id)"),
			"last_seen_at": gorm.Expr("VALUES(last_seen_at)"),
		}),
	}).Create(&rows).Error
}

type nodeActivity struct {
	Devices  int
	Sessions int
}

// loadNodeActivity counts devices seen in the rolling window. The two newest
// buckets always cover it, so last_seen_at gives an exact window, not a 5-10
// minute one.
func loadNodeActivity(now int64) (map[string]nodeActivity, error) {
	var rows []struct {
		NodeIpv4 string
		Devices  int
		Sessions int
	}
	err := db.Get().Model(&NodeDevicePresence{}).
		Select("node_ipv4, COUNT(DISTINCT udid) AS devices, COUNT(DISTINCT udid, session_id) AS sessions").
		Where("bucket >= ? AND last_seen_at >= ?", nodePresenceBucket(now)-nodePresenceBucketSeconds, now-nodePresenceWindowSeconds).
		Group("node_ipv4").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]nodeActivity, len(rows))
	for _, r := range rows {
		out[r.NodeIpv4] = nodeActivity{Devices: r.Devices, Sessions: r.Sessions}
	}
	return out, nil
}

type nodeBucketCount struct {
	Bucket   int64
	NodeIpv4 string
	Devices  int
}

// loadNodeBucketCounts returns completed buckets only: the current one is still
// filling and would read as a false dip.
func loadNodeBucketCounts(since, now int64) ([]nodeBucketCount, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nodeBucketReadTimeout)
	defer cancel()
	var rows []nodeBucketCount
	err := db.Get().WithContext(ctx).Model(&NodeDevicePresence{}).
		Select("bucket, node_ipv4, COUNT(DISTINCT udid) AS devices").
		Where("bucket >= ? AND bucket < ?", since, nodePresenceBucket(now)).
		Group("bucket, node_ipv4").Order("bucket ASC").Scan(&rows).Error
	return rows, err
}

// cachedForBucket returns compute's value, shared across readers and Center
// instances until the next bucket completes. The key must include the bucket.
// The cache is an optimisation only: on any cache failure it computes directly.
func cachedForBucket[T any](key string, compute func() (T, error)) (T, error) {
	var cached T
	if ok, err := redis.CacheGet(key, &cached); err == nil && ok {
		return cached, nil
	}
	value, err := compute()
	if err != nil {
		return value, err
	}
	if err := redis.CacheSet(key, value, nodeBucketCacheSeconds); err != nil {
		log.Warnf(context.Background(), "failed to cache %s: %v", key, err)
	}
	return value, nil
}

// loadNodePeaks returns each node's highest device count over the last 24h.
func loadNodePeaks(now int64) (map[string]int, error) {
	key := fmt.Sprintf("node_dist:peaks:%d", nodePresenceBucket(now))
	return cachedForBucket(key, func() (map[string]int, error) {
		rows, err := loadNodeBucketCounts(now-24*3600, now)
		if err != nil {
			return nil, err
		}
		peaks := make(map[string]int)
		for _, r := range rows {
			if r.Devices > peaks[r.NodeIpv4] {
				peaks[r.NodeIpv4] = r.Devices
			}
		}
		return peaks, nil
	})
}

// loadLatestNodeMetrics returns each node's newest sample inside the freshness
// window. A node without one has no entry.
func loadLatestNodeMetrics(ips []string, now int64) (map[string]SlaveNodeLoad, error) {
	out := make(map[string]SlaveNodeLoad, len(ips))
	if len(ips) == 0 {
		return out, nil
	}
	var loads []SlaveNodeLoad
	latest := db.Get().Model(&SlaveNodeLoad{}).Select("MAX(id)").
		Where("ipv4 IN ? AND created_at >= ?", ips, time.Unix(now-nodeMetricsFreshSeconds, 0)).Group("ipv4")
	if err := db.Get().Where("id IN (?)", latest).Find(&loads).Error; err != nil {
		return nil, err
	}
	for _, l := range loads {
		out[l.Ipv4] = l
	}
	return out, nil
}

type nodeStatusInput struct {
	Private       bool
	Visible       bool
	Offline       bool
	OverQuota     bool
	PressureFresh bool
	Pressure      float64
	ActiveDevices int
}

// classifyNodeStatus goes from "not expected to carry users" to load, so an
// exhausted or offline node is never reported as a placement failure.
func classifyNodeStatus(in nodeStatusInput) string {
	switch {
	case in.Private:
		return nodeStatusPrivate
	case in.Offline:
		return nodeStatusOffline
	case !in.Visible:
		return nodeStatusHidden
	case in.OverQuota:
		return nodeStatusExhausted
	case in.PressureFresh && in.Pressure >= nodeOverloadPressure:
		return nodeStatusOverloaded
	case in.ActiveDevices == 0:
		return nodeStatusIdle
	default:
		return nodeStatusNormal
	}
}

// summarizeNodeScope computes balance over the nodes clients may be sent to.
func summarizeNodeScope(scope string, items []NodeDistributionItem) NodeDistributionSummary {
	s := NodeDistributionSummary{Scope: scope, Nodes: len(items), Verdict: nodeVerdictOK}
	counts := make([]float64, 0, len(items))
	for i := range items {
		it := &items[i]
		switch it.Status {
		case nodeStatusIdle:
			s.IdleNodes++
		case nodeStatusOverloaded:
			s.OverloadedNodes++
		case nodeStatusNormal:
		default:
			continue
		}
		s.EligibleNodes++
		s.ActiveDevices += it.ActiveDevices
		counts = append(counts, float64(it.ActiveDevices))
		if it.ActiveDevices > s.MaxDevices {
			s.MaxDevices = it.ActiveDevices
			s.MaxNode = it.Name
		}
	}
	if median, ok := medianFloat(counts); ok {
		s.MedianDevices = median
	}
	if s.ActiveDevices > 0 {
		s.TopShare = float64(s.MaxDevices) / float64(s.ActiveDevices)
	}
	switch {
	case s.IdleNodes > 0 && s.OverloadedNodes > 0:
		s.Verdict = nodeVerdictImbalanced
	case s.IdleNodes > 0 && s.ActiveDevices > 0:
		s.Verdict = nodeVerdictSkewed
	}
	return s
}

// summarizeNodeDistribution returns the whole fleet first, then countries
// ordered by code.
func summarizeNodeDistribution(items []NodeDistributionItem) []NodeDistributionSummary {
	byCountry := map[string][]NodeDistributionItem{}
	for _, it := range items {
		byCountry[it.Country] = append(byCountry[it.Country], it)
	}
	countries := make([]string, 0, len(byCountry))
	for c := range byCountry {
		countries = append(countries, c)
	}
	sort.Strings(countries)
	out := make([]NodeDistributionSummary, 0, len(countries)+1)
	out = append(out, summarizeNodeScope("all", items))
	for _, c := range countries {
		out = append(out, summarizeNodeScope(c, byCountry[c]))
	}
	return out
}

// buildNodeTrend folds per-node bucket counts into one point per bucket,
// keeping only nodes in scope. activeDevices sums nodes, so a device on two
// nodes in the same bucket counts twice: this measures placement, not users.
func buildNodeTrend(rows []nodeBucketCount, names map[string]string) []NodeDistributionTrendPoint {
	var out []NodeDistributionTrendPoint
	for _, r := range rows {
		name, ok := names[r.NodeIpv4]
		if !ok {
			continue
		}
		if len(out) == 0 || out[len(out)-1].Bucket != r.Bucket {
			out = append(out, NodeDistributionTrendPoint{Bucket: r.Bucket, NodesInScope: len(names)})
		}
		p := &out[len(out)-1]
		p.ActiveDevices += r.Devices
		p.ActiveNodes++
		if r.Devices > p.MaxDevices {
			p.MaxDevices = r.Devices
			p.MaxNode = name
		}
	}
	for i := range out {
		if out[i].ActiveDevices > 0 {
			out[i].TopShare = float64(out[i].MaxDevices) / float64(out[i].ActiveDevices)
		}
	}
	return out
}
