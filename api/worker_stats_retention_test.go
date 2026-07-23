package center

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestStatsRetentionWindows pins the retention constants: slave_node_loads is
// consumed as MAX(id)-only (30d is slack), admin reports cap at 90d lookback
// so the stat tables keep a 30-day buffer past that.
func TestStatsRetentionWindows(t *testing.T) {
	assert.Equal(t, 30, nodeLoadRetentionDays)
	assert.Equal(t, 120, statsRetentionDays)
	// Every target must use a positive window; reported_at only for the two
	// client-clock tables (their created_at contains garbage timestamps).
	for _, tgt := range statsRetentionTargets {
		assert.Positive(t, tgt.retentionDays, tgt.table)
		if tgt.table == "stat_connections" || tgt.table == "stat_app_opens" {
			assert.Equal(t, "reported_at", tgt.column, tgt.table)
		}
	}
}

// TestDeleteInBatches_LoopsUntilDrained seeds more old rows than the batch
// size to prove the loop keeps deleting until none remain, and that newer
// rows survive. The seed rows sit in 1999 with a year-2000 cutoff so the
// sweep touches ONLY them — a real-data database (e.g. an imported prod
// copy) must not get its genuine history deleted row-pair by row-pair.
func TestDeleteInBatches_LoopsUntilDrained(t *testing.T) {
	skipIfNoConfig(t)
	const testNodeID = 9910
	// 开头也清一次：先前被中断的测试跑(Cleanup 未执行)会留下残留行
	db.Get().Where("node_id = ?", testNodeID).Delete(&SlaveNodeLoad{})
	t.Cleanup(func() {
		db.Get().Where("node_id = ?", testNodeID).Delete(&SlaveNodeLoad{})
	})

	ancient := time.Date(1999, 6, 1, 0, 0, 0, 0, time.UTC)
	cutoff := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Now().AddDate(0, 0, -5)
	for range 5 {
		require.NoError(t, db.Get().Create(&SlaveNodeLoad{NodeID: testNodeID, Load: 10, CreatedAt: ancient}).Error)
	}
	require.NoError(t, db.Get().Create(&SlaveNodeLoad{NodeID: testNodeID, Load: 10, CreatedAt: recent}).Error)

	deleted, err := deleteInBatches("slave_node_loads", "created_at", cutoff, 2) // batch < rows → forces looping
	require.NoError(t, err)
	assert.Equal(t, int64(5), deleted)

	var remaining []SlaveNodeLoad
	db.Get().Where("node_id = ?", testNodeID).Find(&remaining)
	require.Len(t, remaining, 1, "only the recent row survives")
	assert.WithinDuration(t, recent, remaining[0].CreatedAt, time.Minute)
}

// TestStatsRetentionCleanup_SweepsStatTablesByReportedAt verifies the handler
// end-to-end on a reported_at table: old-by-reported_at rows go, recent stay.
func TestStatsRetentionCleanup_SweepsStatTablesByReportedAt(t *testing.T) {
	skipIfNoConfig(t)
	const marker = "stats-retention-test-hash"
	db.Get().Where("device_hash = ?", marker).Delete(&StatAppOpen{})
	t.Cleanup(func() {
		db.Get().Where("device_hash = ?", marker).Delete(&StatAppOpen{})
	})

	oldTime := time.Now().AddDate(0, 0, -(statsRetentionDays + 10))
	recentTime := time.Now().AddDate(0, 0, -10)
	// autoCreateTime only fills zero values — explicit ReportedAt is kept.
	require.NoError(t, db.Get().Create(&StatAppOpen{
		DeviceHash: marker, OS: "test", AppVersion: "0", CreatedAt: oldTime, ReportedAt: oldTime,
	}).Error)
	require.NoError(t, db.Get().Create(&StatAppOpen{
		DeviceHash: marker, OS: "test", AppVersion: "0", CreatedAt: recentTime, ReportedAt: recentTime,
	}).Error)

	require.NoError(t, handleStatsRetentionCleanup(context.Background(), nil))

	var rows []StatAppOpen
	db.Get().Where("device_hash = ?", marker).Find(&rows)
	require.Len(t, rows, 1, "row past reported_at retention purged, recent kept")
	assert.WithinDuration(t, recentTime, rows[0].ReportedAt, time.Minute)
}
