package center

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUsageRecorder_UpsertByNodeID verifies the recorder: epoch follow, used
// max within epoch, quota_total adoption, last_report_at bump. Integration.
func TestUsageRecorder_UpsertByNodeID(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	node := seedSlaveNodeForUsageTest(t, "203.0.113.91") // helper below
	t.Cleanup(func() { db.Get().Unscoped().Where("node_id = ?", node.ID).Delete(&NodeUsage{}) })

	// cycle 1000, used 1GB, limit 2TB
	r1 := recordUsage(t, node, NodeUsageRequest{EpochID: 1000, CumulativeBytes: 1 << 30, QuotaTotalBytes: 2 << 40, Seq: 1, Ts: 1700000000})
	assert.Equal(t, usageReportIntervalSec, int(r1.NextReportInterval))
	u := loadNodeUsage(t, node.ID)
	assert.Equal(t, int64(1000), u.Epoch)
	assert.Equal(t, int64(1<<30), u.UsedBytes)
	assert.Equal(t, int64(2<<40), u.QuotaTotalBytes)
	assert.Greater(t, u.LastReportAt, int64(0))

	// same epoch, smaller cumulative (reorder) → used unchanged (max)
	recordUsage(t, node, NodeUsageRequest{EpochID: 1000, CumulativeBytes: 1 << 20, QuotaTotalBytes: 2 << 40, Seq: 2})
	assert.Equal(t, int64(1<<30), loadNodeUsage(t, node.ID).UsedBytes, "max within epoch")

	// new epoch → follow + reset used to reported
	recordUsage(t, node, NodeUsageRequest{EpochID: 1001, CumulativeBytes: 5 << 20, QuotaTotalBytes: 2 << 40, Seq: 3})
	u = loadNodeUsage(t, node.ID)
	assert.Equal(t, int64(1001), u.Epoch)
	assert.Equal(t, int64(5<<20), u.UsedBytes, "new epoch resets used to reported")
}

func seedSlaveNodeForUsageTest(t *testing.T, ip string) *SlaveNode {
	t.Helper()
	db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
	n := &SlaveNode{Ipv4: ip, SecretToken: "usage-test-secret", Country: "US", Region: "us-east-1", Name: "usage-test"}
	require.NoError(t, db.Get().Create(n).Error)
	t.Cleanup(func() { db.Get().Unscoped().Where("id = ?", n.ID).Delete(&SlaveNode{}) })
	return n
}

func loadNodeUsage(t *testing.T, nodeID uint64) NodeUsage {
	t.Helper()
	var u NodeUsage
	require.NoError(t, db.Get().Where("node_id = ?", nodeID).First(&u).Error)
	return u
}

// recordUsage drives the handler with an authenticated slave context for `node`.
func recordUsage(t *testing.T, node *SlaveNode, req NodeUsageRequest) NodeUsageResponse {
	t.Helper()
	return callUsageHandler(t, node, req)
}

// callUsageHandler invokes api_slave_node_report_usage with a gin test context
// where ReqSlaveNode(c) returns `node` (it reads the "i_am_the_node" key set by
// SlaveAuthRequired). It builds the POST /slave/usage JSON body in-process (no
// network) and unmarshals resp.Data into NodeUsageResponse.
func callUsageHandler(t *testing.T, node *SlaveNode, req NodeUsageRequest) NodeUsageResponse {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/slave/usage", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("i_am_the_node", node)

	api_slave_node_report_usage(c)

	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.Equal(t, ErrorNone, ErrorCode(resp.Code), "usage 应成功: %s", resp.Message)

	data, err := ParseResponseData[NodeUsageResponse](w)
	require.NoError(t, err)
	return *data
}
