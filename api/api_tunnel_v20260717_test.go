package center

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// tunnelV20260717TestRouter wires the new endpoint without auth middleware
// so the integration test can drive it directly, the same way
// TestApiK2Relays_ExcludesPrivateNodes does for /api/relays.
func tunnelV20260717TestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v20260717/tunnels", api_v20260717_tunnels)
	return r
}

// TestV20260717TunnelsShape asserts the new endpoint's response shape:
//   - protocol == "k2s" (ProtocolDisplay maps k2v5 → k2s)
//   - ipType == "residential" (from node.ip_type)
//   - serverUrl starts with "k2v5://" (wire scheme preserved)
//   - echConfigList key absent (C3: no ECH on this endpoint)
//
// Drives the real handler against dev MySQL (integration test).
func TestV20260717TunnelsShape(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	uniq := time.Now().Format("20060102150405.000000")

	// Create a shared-pool k2v5 node with residential IP type.
	node := SlaveNode{
		Ipv4:        "10.99.17." + uniq[len(uniq)-2:],
		SecretToken: "v2-node-s-" + uniq,
		Country:     "JP",
		Region:      "japan",
		Name:        "v2-node-" + uniq,
		Class:       NodeClassShared,
		IPType:      IPTypeResidential,
	}
	require.NoError(t, db.Get().Create(&node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })

	serverURL := "k2v5://v2-test-" + uniq + ".example:443?pin=sha256:abc"
	tun := SlaveTunnel{
		Domain:      "v2-test-" + uniq + ".example",
		SecretToken: "v2-tun-s-" + uniq,
		Name:        "v2-tun-" + uniq,
		Protocol:    TunnelProtocolK2V5,
		Port:        443,
		NodeID:      node.ID,
		IsTest:      BoolPtr(false),
		HasRelay:    BoolPtr(false),
		ServerURL:   serverURL,
	}
	require.NoError(t, db.Get().Create(&tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

	r := tunnelV20260717TestRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v20260717/tunnels", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code, "handler must return 200, body=%s", w.Body.String())

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope), "response body must be valid JSON")

	dataRaw, ok := envelope["data"]
	require.True(t, ok, "response must have 'data' key")

	dataBytes, err := json.Marshal(dataRaw)
	require.NoError(t, err)

	var resp struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(dataBytes, &resp))
	require.NotEmpty(t, resp.Items, "expected at least one tunnel")

	// Find the tunnel we just created (there may be others in dev DB).
	var it map[string]any
	for _, item := range resp.Items {
		if urlStr, _ := item["serverUrl"].(string); strings.Contains(urlStr, uniq) {
			it = item
			break
		}
	}
	require.NotNil(t, it, "could not find the tunnel we created in response items")

	if it["protocol"] != "k2s" {
		t.Errorf("protocol=%v want k2s (ProtocolDisplay)", it["protocol"])
	}
	if it["ipType"] != "residential" {
		t.Errorf("ipType=%v want residential", it["ipType"])
	}
	url, _ := it["serverUrl"].(string)
	if !strings.HasPrefix(url, "k2v5://") {
		t.Errorf("serverUrl=%v must keep k2v5:// scheme", it["serverUrl"])
	}
	if _, hasECH := it["echConfigList"]; hasECH {
		t.Error("v20260717 must not carry echConfigList")
	}
}
