package center

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// adminNodeTestRouter wires both the slave upsert endpoint (to create a node)
// and the admin update endpoint (no admin middleware — isolates ip_type behaviour).
func adminNodeTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/slave/nodes/:ipv4", api_slave_node_upsert)
	r.PUT("/app/nodes/:ipv4", api_admin_update_node)
	return r
}

// strptr returns a pointer to s (mirrors brief's illustrative helper).
func strptr(s string) *string { return &s }

// upsertNodeForTest creates a node via PUT /slave/nodes/:ipv4 and registers cleanup.
func upsertNodeForTest(t *testing.T, r *gin.Engine, ipv4 string, req SlaveNodeUpsertRequest) {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	httpReq := httptest.NewRequest(http.MethodPut, "/slave/nodes/"+ipv4, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httpReq)
	require.Equal(t, http.StatusOK, w.Code, "upsert HTTP status, body=%s", w.Body.String())

	var node SlaveNode
	require.NoError(t, db.Get().Unscoped().Where("ipv4 = ?", ipv4).First(&node).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("node_id = ?", node.ID).Delete(&SlaveTunnel{})
		db.Get().Unscoped().Delete(&node)
	})
}

// adminUpdateNodeForTest calls PUT /app/nodes/:ipv4 with the given request.
func adminUpdateNodeForTest(t *testing.T, r *gin.Engine, ipv4 string, req AdminUpdateNodeRequest) {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	httpReq := httptest.NewRequest(http.MethodPut, "/app/nodes/"+ipv4, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httpReq)
	require.Equal(t, http.StatusOK, w.Code, "admin update HTTP status, body=%s", w.Body.String())

	var env map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), "response: %s", w.Body.String())
	code, _ := env["code"].(float64)
	require.Equal(t, float64(0), code, "admin update response code must be 0, body=%s", w.Body.String())
}

// reloadNode reads the SlaveNode back from dev MySQL.
func reloadNode(t *testing.T, ipv4 string) SlaveNode {
	t.Helper()
	var node SlaveNode
	require.NoError(t, db.Get().Unscoped().Where("ipv4 = ?", ipv4).First(&node).Error)
	return node
}

// TestAdminUpdateNodeIPType asserts that PUT /app/nodes/:ipv4 correctly writes
// ip_type through NormalizeIPType (C1): valid → stored as-is; invalid → "unknown".
// Drives the real handler against dev MySQL (integration test).
func TestAdminUpdateNodeIPType(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	uniq := fmt.Sprintf("%d", time.Now().UnixNano())
	ipv4 := fmt.Sprintf("10.91.%s.1", uniq[len(uniq)-3:])

	r := adminNodeTestRouter()
	upsertNodeForTest(t, r, ipv4, SlaveNodeUpsertRequest{
		Country:     "US",
		Name:        "iptype-admin-test-" + uniq,
		SecretToken: "tok-admin-" + uniq,
	})

	// Valid value must land as-is.
	adminUpdateNodeForTest(t, r, ipv4, AdminUpdateNodeRequest{IPType: strptr("non_residential")})
	require.Equal(t, "non_residential", reloadNode(t, ipv4).IPType)

	// Invalid value must normalise to "unknown" (C1).
	adminUpdateNodeForTest(t, r, ipv4, AdminUpdateNodeRequest{IPType: strptr("garbage")})
	require.Equal(t, "unknown", reloadNode(t, ipv4).IPType)

	// Nil IPType must not touch ip_type (nil-guard: only Name changes).
	adminUpdateNodeForTest(t, r, ipv4, AdminUpdateNodeRequest{Name: strptr("renamed-node")})
	require.Equal(t, "unknown", reloadNode(t, ipv4).IPType)
}
