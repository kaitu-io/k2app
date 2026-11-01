package center

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// seedCapacityNode creates a NodeUsage row (and optionally a CloudInstance with
// the given provider) for ip, and registers cleanup of everything keyed by ip.
func seedCapacityNode(t *testing.T, ip, provider string) {
	t.Helper()
	cleanup := func() {
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNodeLoad{})
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&NodeUsage{})
		db.Get().Unscoped().Where("ip_address = ?", ip).Delete(&CloudInstance{})
	}
	cleanup()
	t.Cleanup(cleanup)
	require.NoError(t, db.Get().Create(&NodeUsage{NodeID: 1, Ipv4: ip}).Error)
	if provider != "" {
		require.NoError(t, db.Get().Create(&CloudInstance{
			Provider: provider, AccountName: "cap-test", InstanceID: generateId("ci"),
			Name: "cap-test", IPAddress: ip, Region: "test",
		}).Error)
	}
}

// seedLinearSamples inserts n samples with busy = b + k*mbps, mbps = 20+10*i
// (split evenly up/down), created age ago.
func seedLinearSamples(t *testing.T, ip string, cores, version, n int, b, k float64, age time.Duration) {
	t.Helper()
	rows := make([]SlaveNodeLoad, 0, n)
	for i := 0; i < n; i++ {
		mbps := 20 + 10*float64(i)
		rows = append(rows, SlaveNodeLoad{
			NodeID: 1, Ipv4: ip, CreatedAt: time.Now().Add(-age),
			MetricsVersion: version, CPUCores: cores,
			CPUPercent: b + k*mbps, BandwidthUpMbps: mbps / 2, BandwidthDownMbps: mbps / 2,
		})
	}
	require.NoError(t, db.Get().CreateInBatches(rows, 50).Error)
}

func usageByIp(t *testing.T, ip string) NodeUsage {
	t.Helper()
	var u NodeUsage
	require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&u).Error)
	return u
}

func TestRefreshCapacity_FittedNode(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ip := "10.99.41.1"
	seedCapacityNode(t, ip, "")
	seedLinearSamples(t, ip, 4, 1, 40, 5, 0.1, time.Hour)

	require.NoError(t, refreshNodeCapacities(context.Background()))

	u := usageByIp(t, ip)
	assert.Equal(t, "fitted", u.CapacitySource)
	assert.Greater(t, u.CapacitySamples, 0)
	assert.InDelta(t, 750, u.CapacityMbps, 1)
	assert.NotZero(t, u.CapacityAt)
}

func TestRefreshCapacity_GroupFallback(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	prov, cores := "tpgrp", 77
	ips := []string{"10.99.42.1", "10.99.42.2", "10.99.42.3"}
	for i, k := range []float64{0.1, 0.08, 0.2} {
		seedCapacityNode(t, ips[i], prov)
		seedLinearSamples(t, ips[i], cores, 1, 40, 5, k, time.Hour)
	}
	idle := "10.99.42.4"
	seedCapacityNode(t, idle, prov)
	seedLinearSamples(t, idle, cores, 1, 5, 5, 0.1, time.Hour) // too few samples

	require.NoError(t, refreshNodeCapacities(context.Background()))

	var fitted []float64
	for _, ip := range ips {
		u := usageByIp(t, ip)
		require.Equal(t, "fitted", u.CapacitySource, ip)
		fitted = append(fitted, u.CapacityMbps)
	}
	want, ok := medianFloat(fitted)
	require.True(t, ok)
	u := usageByIp(t, idle)
	assert.Equal(t, "group", u.CapacitySource)
	assert.InDelta(t, want, u.CapacityMbps, 1e-6)
	assert.Zero(t, u.CapacitySamples)
}

func TestRefreshCapacity_NoGroup(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ip := "10.99.43.1"
	seedCapacityNode(t, ip, "tpnone")
	seedLinearSamples(t, ip, 66, 1, 5, 5, 0.1, time.Hour)
	// pretend it was fitted before: must be recomputed
	require.NoError(t, db.Get().Model(&NodeUsage{}).Where("ipv4 = ?", ip).
		Updates(map[string]any{"capacity_source": "fitted", "capacity_mbps": 999}).Error)

	require.NoError(t, refreshNodeCapacities(context.Background()))

	u := usageByIp(t, ip)
	assert.Equal(t, "none", u.CapacitySource)
	assert.Zero(t, u.CapacityMbps)
}

func TestRefreshCapacity_IgnoresVersionZero(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ip := "10.99.44.1"
	seedCapacityNode(t, ip, "")
	seedLinearSamples(t, ip, 4, 0, 40, 5, 0.1, time.Hour)

	require.NoError(t, refreshNodeCapacities(context.Background()))

	u := usageByIp(t, ip)
	assert.Equal(t, "none", u.CapacitySource)
	assert.Zero(t, u.CapacityMbps)
}

func TestRefreshCapacity_WindowIs14Days(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ip := "10.99.45.1"
	seedCapacityNode(t, ip, "")
	seedLinearSamples(t, ip, 4, 1, 40, 5, 0.1, 15*24*time.Hour)

	require.NoError(t, refreshNodeCapacities(context.Background()))
	assert.Equal(t, "none", usageByIp(t, ip).CapacitySource)

	seedLinearSamples(t, ip, 4, 1, 40, 5, 0.1, 13*24*time.Hour)
	require.NoError(t, refreshNodeCapacities(context.Background()))
	u := usageByIp(t, ip)
	assert.Equal(t, "fitted", u.CapacitySource)
	assert.Equal(t, 40, u.CapacitySamples, "only the 13-day-old samples count")
}

func TestNodeProviderFromName(t *testing.T) {
	assert.Equal(t, "bwh", nodeProviderFromName("us-la.bwh.wm03"))
	assert.Equal(t, "", nodeProviderFromName("bad"))
	assert.Equal(t, "", nodeProviderFromName(""))
}

func TestCapacityGroupKey(t *testing.T) {
	assert.Equal(t, "bwh/4", capacityGroupKey("bwh", 4))
	assert.Equal(t, "", capacityGroupKey("", 4))
	assert.Equal(t, "", capacityGroupKey("bwh", 0))
}

func TestUpdateNodePressure_WritesRow(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ip := "10.99.46.1"
	seedCapacityNode(t, ip, "")
	require.NoError(t, db.Get().Model(&NodeUsage{}).Where("ipv4 = ?", ip).
		Updates(map[string]any{"capacity_mbps": 1000, "quota_total_bytes": 777}).Error)

	now := time.Now().Unix()
	h := SlaveTunnelHealth{MetricsVersion: 1, CPUPercent: 40, BandwidthUpMbps: 300, BandwidthDownMbps: 200}
	require.NoError(t, updateNodePressure(context.Background(), ip, h, now))

	u := usageByIp(t, ip)
	assert.InDelta(t, 0.5, u.PressureRatio, 1e-9) // 500/1000 beats 40/80
	assert.Equal(t, now, u.PressureAt)
	assert.EqualValues(t, 777, u.QuotaTotalBytes, "quota columns untouched")
}

func TestUpdateNodePressure_NoUsageRowIsNoop(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	assert.NoError(t, updateNodePressure(context.Background(), "10.99.46.99",
		SlaveTunnelHealth{MetricsVersion: 1, CPUPercent: 50}, time.Now().Unix()))
}

func TestReportStatus_VersionZeroSkipsPressure(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ip := "10.99.47.1"
	node := seedSlaveNodeForUsageTest(t, ip)
	cleanupLoadRows(t, ip, node.ID)
	seedCapacityNode(t, ip, "")
	require.NoError(t, db.Get().Model(&NodeUsage{}).Where("ipv4 = ?", ip).
		Updates(map[string]any{"pressure_at": 12345, "pressure_ratio": 0.4}).Error)

	postStatusReport(t, node, `{"updatedAt":1,"health":{"cpuUsage":90}}`)
	u := usageByIp(t, ip)
	assert.EqualValues(t, 12345, u.PressureAt)
	assert.InDelta(t, 0.4, u.PressureRatio, 1e-9)

	postStatusReport(t, node, `{"updatedAt":1,"health":{"metricsVersion":1,"cpuPercent":40}}`)
	u = usageByIp(t, ip)
	assert.Greater(t, u.PressureAt, int64(12345), "v1 report updates pressure")
}
