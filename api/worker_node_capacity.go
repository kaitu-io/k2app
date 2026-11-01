package center

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// TaskTypeNodeCapacityRefresh is the daily cron that refits every node's
// throughput capacity M from the last 14 days of metrics samples.
const TaskTypeNodeCapacityRefresh = "node:capacity_refresh"

const (
	capacityWindow     = 14 * 24 * time.Hour
	capacityReadBatch  = 5000
	capacitySourceFit  = "fitted"
	capacitySourceGrp  = "group"
	capacitySourceNone = "none"
)

func handleNodeCapacityRefresh(ctx context.Context, _ []byte) error {
	return refreshNodeCapacities(ctx)
}

// capacityGroupKey groups nodes of the same provider and core count; "" means
// the node cannot be grouped.
func capacityGroupKey(provider string, cores int) string {
	if provider == "" || cores <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/%d", provider, cores)
}

// nodeProviderFromName extracts the provider from a node name such as
// "us-la.bwh.wm03" (region.provider.id); "" when the name has no such shape.
func nodeProviderFromName(name string) string {
	parts := strings.Split(name, ".")
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

// nodeProvider prefers the CloudInstance provider for ipv4 and falls back to
// the node name.
func nodeProvider(ipv4 string) string {
	var ci CloudInstance
	if err := db.Get().Where("ip_address = ?", ipv4).First(&ci).Error; err == nil && ci.Provider != "" {
		return ci.Provider
	}
	var n SlaveNode
	if err := db.Get().Where("ipv4 = ?", ipv4).First(&n).Error; err == nil {
		return nodeProviderFromName(n.Name)
	}
	return ""
}

type nodeCapacityResult struct {
	ipv4     string
	groupKey string
	fitted   bool
	mbps     float64
	used     int
}

// loadCapacitySamples streams the node's v1+ samples within the window in
// batches, returning them and the core count of the newest sample.
func loadCapacitySamples(ipv4 string, since time.Time) ([]capacitySample, int, error) {
	var samples []capacitySample
	var rows []SlaveNodeLoad
	var cores int
	var cursor uint64
	for {
		rows = rows[:0]
		err := db.Get().
			Select("id", "cpu_cores", "cpu_percent", "steal_percent", "bandwidth_up_mbps", "bandwidth_down_mbps").
			Where("ipv4 = ? AND metrics_version >= ? AND created_at >= ? AND id > ?", ipv4, 1, since, cursor).
			Order("id ASC").Limit(capacityReadBatch).Find(&rows).Error
		if err != nil {
			return nil, 0, err
		}
		for _, r := range rows {
			samples = append(samples, capacitySample{
				Mbps:     r.BandwidthUpMbps + r.BandwidthDownMbps,
				BusyPct:  r.CPUPercent,
				StealPct: r.StealPercent,
			})
			cores = r.CPUCores
			cursor = r.ID
		}
		if len(rows) < capacityReadBatch {
			return samples, cores, nil
		}
	}
}

// refreshNodeCapacities refits M for every row in node_usages. Nodes whose own
// fit fails take the median of this run's fitted nodes in the same
// (provider, cores) group, else none. A failing node is logged and skipped; the
// last error is returned.
func refreshNodeCapacities(ctx context.Context) error {
	since := time.Now().Add(-capacityWindow)
	now := time.Now().Unix()
	var lastErr error

	var results []nodeCapacityResult
	var usages []NodeUsage
	err := db.Get().Select("id", "ipv4").Where("ipv4 <> ''").
		FindInBatches(&usages, capacityReadBatch, func(tx *gorm.DB, _ int) error {
			for _, u := range usages {
				samples, cores, err := loadCapacitySamples(u.Ipv4, since)
				if err != nil {
					log.Errorf(ctx, "[NODE-CAPACITY] load samples ipv4=%s: %v", u.Ipv4, err)
					lastErr = err
					continue
				}
				r := nodeCapacityResult{ipv4: u.Ipv4, groupKey: capacityGroupKey(nodeProvider(u.Ipv4), cores)}
				r.mbps, r.used, r.fitted = fitNodeCapacity(samples)
				results = append(results, r)
			}
			return nil
		}).Error
	if err != nil {
		return fmt.Errorf("scan node_usages: %w", err)
	}

	byGroup := map[string][]float64{}
	for _, r := range results {
		if r.fitted && r.groupKey != "" {
			byGroup[r.groupKey] = append(byGroup[r.groupKey], r.mbps)
		}
	}

	for _, r := range results {
		source, mbps, samples := capacitySourceNone, 0.0, 0
		switch {
		case r.fitted:
			source, mbps, samples = capacitySourceFit, r.mbps, r.used
		case r.groupKey != "":
			if m, ok := medianFloat(byGroup[r.groupKey]); ok {
				source, mbps = capacitySourceGrp, m
			}
		}
		if err := db.Get().Model(&NodeUsage{}).Where("ipv4 = ?", r.ipv4).UpdateColumns(map[string]any{
			"capacity_mbps":    mbps,
			"capacity_source":  source,
			"capacity_samples": samples,
			"capacity_at":      now,
		}).Error; err != nil {
			log.Errorf(ctx, "[NODE-CAPACITY] write ipv4=%s: %v", r.ipv4, err)
			lastErr = err
		}
	}
	log.Infof(ctx, "[NODE-CAPACITY] refreshed %d nodes", len(results))
	return lastErr
}

// updateNodePressure folds the current report into the node's smoothed
// pressure. Capacity comes from the NodeUsage row; a missing row is a no-op.
func updateNodePressure(ctx context.Context, ipv4 string, h SlaveTunnelHealth, now int64) error {
	var u NodeUsage
	if err := db.Get().Where("ipv4 = ?", ipv4).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	raw := rawPressure(h.BandwidthUpMbps+h.BandwidthDownMbps, u.CapacityMbps, h.CPUPercent, h.MemoryUsage)
	next := nextPressure(u.PressureRatio, u.PressureAt, raw, now)
	return db.Get().Model(&NodeUsage{}).Where("id = ?", u.ID).UpdateColumns(map[string]any{
		"pressure_ratio": next,
		"pressure_at":    now,
	}).Error
}
