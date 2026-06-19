package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsNodeOverQuota(t *testing.T) {
	cases := []struct {
		name string
		u    *NodeUsage
		want bool
	}{
		{"nil", nil, false},
		{"unlimited (limit 0) never over", &NodeUsage{QuotaTotalBytes: 0, UsedBytes: 1 << 50}, false},
		{"under: 1GB used of 4TB", &NodeUsage{QuotaTotalBytes: 4 << 40, UsedBytes: 1 << 30}, false},
		{"exactly at reserve boundary", &NodeUsage{QuotaTotalBytes: 4 << 40, UsedBytes: (4 << 40) - quotaCutoffReserveBytes}, true},
		{"just under reserve boundary", &NodeUsage{QuotaTotalBytes: 4 << 40, UsedBytes: (4 << 40) - quotaCutoffReserveBytes - 1}, false},
		{"over 100%", &NodeUsage{QuotaTotalBytes: 2 << 40, UsedBytes: 3 << 40}, true},
	}
	for _, c := range cases {
		assert.Equalf(t, c.want, isNodeOverQuota(c.u), "%s", c.name)
	}
}

func TestIsNodeOffline(t *testing.T) {
	const now = int64(1_700_000_000)
	assert.False(t, isNodeOffline(nil, now), "nil → not offline (unknown, don't hide)")
	assert.False(t, isNodeOffline(&NodeUsage{LastReportAt: now - nodeOfflineSeconds + 1}, now), "within window")
	assert.True(t, isNodeOffline(&NodeUsage{LastReportAt: now - nodeOfflineSeconds - 1}, now), "stale past window")
	assert.False(t, isNodeOffline(&NodeUsage{LastReportAt: 0}, now), "never reported (0) → not offline; G2 handles uncapped, not this")
}
