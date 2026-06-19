package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHideRule_UnifiedReserve pins the single hide rule used by tunnel lists.
func TestHideRule_UnifiedReserve(t *testing.T) {
	over := &NodeUsage{QuotaTotalBytes: 2 << 40, UsedBytes: (2 << 40) - quotaCutoffReserveBytes}
	under := &NodeUsage{QuotaTotalBytes: 2 << 40, UsedBytes: 1 << 30}
	assert.True(t, isNodeOverQuota(over))
	assert.False(t, isNodeOverQuota(under))
	assert.False(t, isNodeOverQuota(&NodeUsage{QuotaTotalBytes: 0, UsedBytes: 1 << 50}), "unlimited never hidden")
	// admin bypass + offline are call-site concerns; pin the hide composes them:
	assert.True(t, shouldHideTunnelForUser(over, false, 0))
	assert.False(t, shouldHideTunnelForUser(over, true, 0), "admin sees over-quota")
}
