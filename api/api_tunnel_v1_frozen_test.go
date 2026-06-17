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

// tunnelV1TestRouter wires the legacy /api/tunnels endpoint without auth
// middleware so we can drive the real handler directly (non-admin path —
// exactly the contract every released client depends on).
func tunnelV1TestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/tunnels", api_k2_tunnels)
	return r
}

// TestV1TunnelsFrozen_NoIPType pins the backward-compatibility invariant for
// the legacy /api/tunnels response: it MUST NOT carry the new `ipType` key.
//
// The whole node-ip-type change shares DataSlaveNode/DataSlaveTunnel between v1
// and v20260717; v1 stays byte-frozen only because (a) `ipType` is tagged
// `omitempty` and (b) buildDataSlaveNode never sets it. If a future edit drops
// omitempty or sets IPType in the shared helper, every existing client would
// start receiving `"ipType":""` on the frozen endpoint. This test is the
// regression net for that — it drives the real handler against dev MySQL and
// fails if `ipType` appears anywhere in the v1 payload.
func TestV1TunnelsFrozen_NoIPType(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	uniq := time.Now().Format("20060102150405.000000")

	// A shared-pool node with a residential IP type — the value that WOULD
	// leak into v1 if the freeze ever broke.
	node := SlaveNode{
		Ipv4:        "10.98.17." + uniq[len(uniq)-2:],
		SecretToken: "v1-node-s-" + uniq,
		Country:     "JP",
		Region:      "japan",
		Name:        "v1-node-" + uniq,
		Class:       NodeClassShared,
		IPType:      IPTypeResidential,
	}
	require.NoError(t, db.Get().Create(&node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })

	serverURL := "k2v5://v1-test-" + uniq + ".example:443?pin=sha256:abc"
	tun := SlaveTunnel{
		Domain:      "v1-test-" + uniq + ".example",
		SecretToken: "v1-tun-s-" + uniq,
		Name:        "v1-tun-" + uniq,
		Protocol:    TunnelProtocolK2V5,
		Port:        443,
		NodeID:      node.ID,
		IsTest:      BoolPtr(false),
		HasRelay:    BoolPtr(false),
		ServerURL:   serverURL,
	}
	require.NoError(t, db.Get().Create(&tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

	r := tunnelV1TestRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/tunnels", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code, "handler must return 200, body=%s", w.Body.String())

	// 1) Raw-string guard: the literal key must not appear in the v1 payload at
	//    all. This catches a leak on ANY item, not just the one we created.
	require.NotContains(t, w.Body.String(), "ipType",
		"legacy /api/tunnels must never carry the ipType key (v1 frozen)")

	// 2) Structural guard on our own item, so the test still means something if
	//    the serialization ever changes shape.
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	dataBytes, err := json.Marshal(envelope["data"])
	require.NoError(t, err)
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(dataBytes, &resp))

	var found map[string]any
	for _, item := range resp.Items {
		if urlStr, _ := item["serverUrl"].(string); strings.Contains(urlStr, uniq) {
			found = item
			break
		}
	}
	require.NotNil(t, found, "could not find the tunnel we created in v1 response")
	_, hasIPType := found["ipType"]
	require.False(t, hasIPType, "v1 tunnel item must not contain ipType key")
}
