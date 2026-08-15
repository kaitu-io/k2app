package center

import (
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

// adminTunnelTestRouter wires the slave upsert endpoints (node + tunnel) and
// the admin tunnel list endpoint without admin middleware (isolates behaviour).
func adminTunnelTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/slave/nodes/:ipv4", api_slave_node_upsert)
	r.GET("/app/tunnels", api_admin_list_tunnels)
	return r
}

// callAdminListTunnels calls GET /app/tunnels?protocol=<proto> for one page
// and returns the raw body. pageSize is pinned to the server-side maximum:
// the shared dev DB accumulates tunnels from other tests, so the default
// page of 10 can silently push this test's seed out of the response.
func callAdminListTunnels(t *testing.T, proto string, page int) []byte {
	t.Helper()
	r := adminTunnelTestRouter()
	url := fmt.Sprintf("/app/tunnels?page=%d&pageSize=100", page)
	if proto != "" {
		url += "&protocol=" + proto
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "admin tunnel list HTTP status, body=%s", w.Body.String())
	return w.Body.Bytes()
}

// setupTestDB initialises the config and skips if config.yml is unavailable.
// It mirrors testInitConfig+skipIfNoConfig but uses the t.Helper convention.
func setupTestDB(t *testing.T) {
	t.Helper()
	testInitConfig()
	skipIfNoConfig(t)
}

// createNodeWithTunnelForTest seeds a SlaveNode with one k2v5 SlaveTunnel
// into dev MySQL and registers cleanup. Returns the seeded node.
func createNodeWithTunnelForTest(t *testing.T, ipv4 string, ipType string) SlaveNode {
	t.Helper()
	uniq := fmt.Sprintf("%d", time.Now().UnixNano())

	node := SlaveNode{
		Ipv4:    ipv4,
		Name:    "tunnel-iptype-test-" + uniq,
		Country: "US",
		IPType:  ipType,
	}
	require.NoError(t, db.Get().Create(&node).Error)

	tunnel := SlaveTunnel{
		NodeID:   node.ID,
		Protocol: TunnelProtocolK2V5,
		Name:     "tun-" + uniq,
		Domain:   "t" + uniq + ".example.com",
		Port:     10001,
	}
	require.NoError(t, db.Get().Create(&tunnel).Error)

	t.Cleanup(func() {
		db.Get().Unscoped().Where("node_id = ?", node.ID).Delete(&SlaveTunnel{})
		db.Get().Unscoped().Delete(&node)
	})
	return node
}

// TestAdminTunnelsDisplayK2sAndIPType verifies:
//   - "k2s" filter alias resolves to k2v5 tunnels (C4)
//   - admin response shows protocol="k2s" (C2, via ProtocolDisplay)
//   - admin response carries ipType field (Task 6 requirement)
func TestAdminTunnelsDisplayK2sAndIPType(t *testing.T) {
	setupTestDB(t)

	uniq := fmt.Sprintf("%d", time.Now().UnixNano())
	ipv4 := fmt.Sprintf("10.92.%s.1", uniq[len(uniq)-3:])
	// Seed DB BEFORE querying so db.Get() is already connected when the handler runs.
	createNodeWithTunnelForTest(t, ipv4, IPTypeResidential)

	// C4: filter by display alias "k2s" must return k2v5 tunnels.
	// Walk every page: result ordering is unspecified, so the seed can land
	// on any page of a residue-laden dev DB.
	var found map[string]any
	var firstPageItems int
	for page := 1; found == nil; page++ {
		raw := callAdminListTunnels(t, "k2s", page)

		// Response shape: { "code": 0, "data": { "items": [...], "pagination": {...} } }
		var resp struct {
			Code int `json:"code"`
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(raw, &resp), "unmarshal response: %s", string(raw))
		require.Equal(t, 0, resp.Code, "response code must be 0, body=%s", string(raw))
		if page == 1 {
			firstPageItems = len(resp.Data.Items)
			require.NotEmpty(t, resp.Data.Items, "k2s 别名应过滤到 k2v5 隧道")
		}
		if len(resp.Data.Items) == 0 {
			break // exhausted all pages without finding the seed
		}
		for _, item := range resp.Data.Items {
			node, _ := item["node"].(map[string]any)
			if node != nil {
				if ipv4Val, _ := node["ipv4"].(string); ipv4Val == ipv4 {
					found = item
					break
				}
			}
		}
	}
	require.NotNil(t, found, "seeded tunnel must appear in k2s-filtered results (first page had %d items)", firstPageItems)

	// C2: protocol must be displayed as "k2s", not the wire value "k2v5"
	require.Equal(t, "k2s", found["protocol"], "admin protocol field must be k2s, not k2v5")

	// Task 6: ipType must be present (omitempty means it appears when non-empty)
	_, hasIPType := found["ipType"]
	require.True(t, hasIPType, "admin tunnel must carry ipType")
	require.Equal(t, IPTypeResidential, found["ipType"], "ipType must match node's ip_type")
}
