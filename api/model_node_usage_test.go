package center

import (
	"testing"

	db "github.com/wordgate/qtoolkit/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNodeUsage_CRUD verifies the table exists post-migrate and the unique
// NodeID constraint holds (1:1 with SlaveNode). Integration: real dev MySQL.
func TestNodeUsage_CRUD(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	const nodeID = uint64(9_700_001)
	db.Get().Unscoped().Where("node_id = ?", nodeID).Delete(&NodeUsage{})
	t.Cleanup(func() { db.Get().Unscoped().Where("node_id = ?", nodeID).Delete(&NodeUsage{}) })

	u := &NodeUsage{NodeID: nodeID, Epoch: 100, UsedBytes: 2048, QuotaTotalBytes: 4096, LastReportAt: 1700000000}
	require.NoError(t, db.Get().Create(u).Error)

	var got NodeUsage
	require.NoError(t, db.Get().Where("node_id = ?", nodeID).First(&got).Error)
	assert.Equal(t, int64(2048), got.UsedBytes)
	assert.Equal(t, int64(4096), got.QuotaTotalBytes)

	// Unique NodeID: a second row for the same node must fail.
	dup := &NodeUsage{NodeID: nodeID, Epoch: 1}
	assert.Error(t, db.Get().Create(dup).Error, "node_id uniqueIndex must reject a duplicate")
}
