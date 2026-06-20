package center

import (
	"testing"

	db "github.com/wordgate/qtoolkit/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNodeUsage_CRUD verifies the table exists post-migrate and the unique
// Ipv4 constraint holds (the durable node key). Integration: real dev MySQL.
func TestNodeUsage_CRUD(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	const nodeID = uint64(9_700_001)
	const ip = "203.0.113.201"
	db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&NodeUsage{})
	t.Cleanup(func() { db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&NodeUsage{}) })

	u := &NodeUsage{NodeID: nodeID, Ipv4: ip, Epoch: 100, UsedBytes: 2048, QuotaTotalBytes: 4096, LastReportAt: 1700000000}
	require.NoError(t, db.Get().Create(u).Error)

	var got NodeUsage
	require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&got).Error)
	assert.Equal(t, int64(2048), got.UsedBytes)
	assert.Equal(t, int64(4096), got.QuotaTotalBytes)

	// Unique Ipv4: a second row for the same ip must fail (even with a new node id).
	dup := &NodeUsage{NodeID: nodeID + 1, Ipv4: ip, Epoch: 1}
	assert.Error(t, db.Get().Create(dup).Error, "ipv4 uniqueIndex must reject a duplicate")
}
