package center

import (
	"context"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// TaskTypeStatsRetentionCleanup is the daily Asynq cron task that sweeps
// unbounded time-series tables past their retention window. These tables grew
// without any cleanup (slave_node_loads alone reached ~300MB / 1.6M rows) while
// their readers only look at recent data.
const TaskTypeStatsRetentionCleanup = "stats:retention_cleanup"

// Retention windows, derived from what readers actually consume:
//   - slave_node_loads: scoring reads only MAX(id) per node (logic_node_load.go),
//     no historical queries exist — 30 days is pure slack.
//   - stat_* / connection_ratings: admin reports look back at most 90 days
//     (parseRangeDays caps at "90d") — 120 days keeps a 30-day buffer.
const (
	nodeLoadRetentionDays = 30
	statsRetentionDays    = 120
)

// statsRetentionBatchSize bounds each DELETE so the first run (clearing months
// of backlog) never holds a long transaction or stalls replication.
const statsRetentionBatchSize = 50000

// statsRetentionTarget names one table to sweep and which time column its
// readers filter on: stat_app_opens/stat_connections report on reported_at
// (server-authoritative; created_at is client clock and contains garbage like
// 2019 timestamps), the rest report on created_at.
type statsRetentionTarget struct {
	table         string
	column        string
	retentionDays int
}

var statsRetentionTargets = []statsRetentionTarget{
	{"slave_node_loads", "created_at", nodeLoadRetentionDays},
	{"stat_connections", "reported_at", statsRetentionDays},
	{"stat_app_opens", "reported_at", statsRetentionDays},
	{"stat_k2s_downloads", "created_at", statsRetentionDays},
	{"connection_ratings", "created_at", statsRetentionDays},
}

// deleteInBatches removes rows where column < cutoff, batchSize rows per
// statement, until none remain. Returns total rows deleted.
func deleteInBatches(table, column string, cutoff time.Time, batchSize int) (int64, error) {
	var total int64
	for {
		res := db.Get().Exec(
			fmt.Sprintf("DELETE FROM `%s` WHERE `%s` < ? LIMIT %d", table, column, batchSize),
			cutoff,
		)
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if res.RowsAffected < int64(batchSize) {
			return total, nil
		}
	}
}

// handleStatsRetentionCleanup is the Asynq cron handler for
// TaskTypeStatsRetentionCleanup. A failing table logs and continues so one
// broken target never blocks the others.
func handleStatsRetentionCleanup(ctx context.Context, _ []byte) error {
	now := time.Now()
	for _, t := range statsRetentionTargets {
		cutoff := now.AddDate(0, 0, -t.retentionDays)
		deleted, err := deleteInBatches(t.table, t.column, cutoff, statsRetentionBatchSize)
		if err != nil {
			log.Errorf(ctx, "[STATS-RETENTION] %s: deleted %d then failed: %v", t.table, deleted, err)
			continue
		}
		if deleted > 0 {
			log.Infof(ctx, "[STATS-RETENTION] %s: deleted %d rows older than %d days", t.table, deleted, t.retentionDays)
		}
	}
	return nil
}
