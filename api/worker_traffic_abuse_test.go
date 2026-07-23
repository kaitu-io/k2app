package center

import (
	"testing"
	"time"

	"github.com/spf13/viper"
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
	// 隐私政策承诺"流量用量统计保留 2 个月"——保留期常量必须与之对齐。
	assert.Equal(t, 60, trafficRetentionDays)

	oldDate := time.Now().In(cnZone).AddDate(0, 0, -70).Format("2006-01-02")
	recentDate := time.Now().In(cnZone).AddDate(0, 0, -10).Format("2006-01-02")
	t.Cleanup(func() {
		db.Get().Where("node_ipv4 = ?", "10.96.0.2").Delete(&DeviceTrafficDaily{})
	})
	seedTraffic(t, oldDate, "old-d1", "10.96.0.2", 9300, 1, 1)
	seedTraffic(t, recentDate, "new-d1", "10.96.0.2", 9300, 1, 1)
	require.NoError(t, cleanupTrafficRetention(trafficRetentionDays))
	var dates []string
	db.Get().Model(&DeviceTrafficDaily{}).Where("node_ipv4 = ?", "10.96.0.2").Pluck("date", &dates)
	assert.Equal(t, []string{recentDate}, dates, "70-day-old row purged, 10-day-old row kept")
}

func TestTrafficAbuseThresholdDefault(t *testing.T) {
	orig := viper.Get("traffic.abuse_monthly_gb")
	t.Cleanup(func() { viper.Set("traffic.abuse_monthly_gb", orig) })

	viper.Set("traffic.abuse_monthly_gb", 0) // 未配置/非正数 → 默认 100 GB
	assert.Equal(t, int64(100)<<30, trafficAbuseThresholdBytes())
	viper.Set("traffic.abuse_monthly_gb", 250)
	assert.Equal(t, int64(250)<<30, trafficAbuseThresholdBytes())
}
