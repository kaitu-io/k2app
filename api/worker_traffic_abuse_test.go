package center

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func TestTrafficAbuse_FindAndDedup(t *testing.T) {
	skipIfNoConfig(t)
	month := time.Now().In(cnZone).Format("2006-01")
	date := trafficDate(time.Now())
	t.Cleanup(func() {
		db.Get().Where("node_ipv4 = ?", "10.96.0.1").Delete(&DeviceTrafficDaily{})
		db.Get().Where("month = ?", month).Where("user_id IN ?", []uint{9200, 9201}).Delete(&TrafficAbuseAlert{})
	})
	// 阈值取 1 GB 便于测试注入
	thresholdBytes := int64(1) << 30
	seedTraffic(t, date, "ab-d1", "10.96.0.1", 9200, thresholdBytes, 1) // 超
	seedTraffic(t, date, "ab-d2", "10.96.0.1", 9201, 10, 10)           // 不超

	offenders, err := findTrafficAbusers(month, thresholdBytes)
	require.NoError(t, err)
	var hit bool
	for _, o := range offenders {
		assert.NotEqual(t, uint(9201), o.UserID)
		if o.UserID == 9200 {
			hit = true
			assert.Greater(t, o.Bytes, thresholdBytes)
		}
	}
	assert.True(t, hit, "user 9200 must be flagged")

	// 首次记录成功 → 应发通知；重复记录失败 → 不再发
	first, err := recordTrafficAbuseAlert(month, 9200, thresholdBytes+1)
	require.NoError(t, err)
	assert.True(t, first)
	second, err := recordTrafficAbuseAlert(month, 9200, thresholdBytes+2)
	require.NoError(t, err)
	assert.False(t, second, "same month+user must dedup")
}

func TestTrafficRetentionCleanup(t *testing.T) {
	skipIfNoConfig(t)
	oldDate := time.Now().In(cnZone).AddDate(0, 0, -200).Format("2006-01-02")
	t.Cleanup(func() {
		db.Get().Where("node_ipv4 = ?", "10.96.0.2").Delete(&DeviceTrafficDaily{})
	})
	seedTraffic(t, oldDate, "old-d1", "10.96.0.2", 9300, 1, 1)
	require.NoError(t, cleanupTrafficRetention(180))
	var cnt int64
	db.Get().Model(&DeviceTrafficDaily{}).Where("node_ipv4 = ?", "10.96.0.2").Count(&cnt)
	assert.Equal(t, int64(0), cnt)
}
