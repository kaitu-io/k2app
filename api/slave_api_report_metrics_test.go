package center

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// postStatusReport drives api_slave_report_status with an authenticated slave
// context for `node`. The body is raw JSON so tests can omit fields exactly
// as an old sidecar would.
func postStatusReport(t *testing.T, node *SlaveNode, body string) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/slave/report/status", bytes.NewReader([]byte(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("i_am_the_node", node)
	api_slave_report_status(c)
	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "report: %s", resp.Message)
}

func loadRowsByIpv4(t *testing.T, ip string) []SlaveNodeLoad {
	t.Helper()
	var rows []SlaveNodeLoad
	require.NoError(t, db.Get().Where("ipv4 = ?", ip).Find(&rows).Error)
	return rows
}

func cleanupLoadRows(t *testing.T, ip string, nodeID uint64) {
	t.Cleanup(func() {
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNodeLoad{})
		db.Get().Unscoped().Where("node_id = ?", nodeID).Delete(&SlaveNodeLoad{})
	})
}

func TestReportStatus_StoresMetricsV1Fields(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ip := "10.99.31.1"
	node := seedSlaveNodeForUsageTest(t, ip)
	cleanupLoadRows(t, ip, node.ID)

	postStatusReport(t, node, `{"updatedAt":1,"health":{"metricsVersion":1,"cpuUsage":12.5,"cpuPercent":37.5,"stealPercent":4.25,"cpuCores":8}}`)

	rows := loadRowsByIpv4(t, ip)
	require.Len(t, rows, 1)
	assert.Equal(t, node.ID, rows[0].NodeID)
	assert.Equal(t, ip, rows[0].Ipv4)
	assert.Equal(t, 1, rows[0].MetricsVersion)
	assert.InDelta(t, 37.5, rows[0].CPUPercent, 1e-9)
	assert.InDelta(t, 4.25, rows[0].StealPercent, 1e-9)
	assert.Equal(t, 8, rows[0].CPUCores)
}

func TestReportStatus_OldSidecarStaysVersionZero(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ip := "10.99.31.2"
	node := seedSlaveNodeForUsageTest(t, ip)
	cleanupLoadRows(t, ip, node.ID)

	postStatusReport(t, node, `{"updatedAt":1,"health":{"cpuUsage":50}}`)

	rows := loadRowsByIpv4(t, ip)
	require.Len(t, rows, 1)
	assert.Equal(t, ip, rows[0].Ipv4)
	assert.Equal(t, 0, rows[0].MetricsVersion)
	assert.Zero(t, rows[0].CPUPercent)
	assert.Zero(t, rows[0].StealPercent)
	assert.Zero(t, rows[0].CPUCores)
}

func TestSlaveUnregister_KeepsLoadRows(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ip := "10.99.31.3"
	node := seedSlaveNodeForUsageTest(t, ip)
	cleanupLoadRows(t, ip, node.ID)

	postStatusReport(t, node, `{"updatedAt":1,"health":{"metricsVersion":1,"cpuPercent":10,"cpuCores":2}}`)
	require.Len(t, loadRowsByIpv4(t, ip), 1)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "ipv4", Value: ip}}
	c.Request = httptest.NewRequest("DELETE", "/slave/nodes/"+ip, nil)
	c.Set("i_am_the_node", node)
	api_slave_node_unregister(c)
	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "unregister: %s", resp.Message)

	var n int64
	require.NoError(t, db.Get().Model(&SlaveNode{}).Where("ipv4 = ?", ip).Count(&n).Error)
	require.Zero(t, n, "node itself must be deleted")
	assert.Len(t, loadRowsByIpv4(t, ip), 1, "samples survive self-unregister (keyed by ipv4)")
}

func TestAdminDeleteNode_StillDeletesLoadRows(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ip := "10.99.31.4"
	node := seedSlaveNodeForUsageTest(t, ip)
	cleanupLoadRows(t, ip, node.ID)

	postStatusReport(t, node, `{"updatedAt":1,"health":{"metricsVersion":1,"cpuPercent":10,"cpuCores":2}}`)
	require.Len(t, loadRowsByIpv4(t, ip), 1)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "ipv4", Value: ip}}
	c.Request = httptest.NewRequest("DELETE", "/app/nodes/"+ip, nil)
	api_admin_delete_node(c)
	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "admin delete: %s", resp.Message)

	assert.Len(t, loadRowsByIpv4(t, ip), 0, "admin delete still removes load rows")
}
