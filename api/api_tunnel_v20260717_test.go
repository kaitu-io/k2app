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

// TestV20260717ExcludesPrivateNodes pins the highest-consequence invariant of
// the whole endpoint: a private (single-owner dedicated-VPS) node must NEVER
// surface in the shared-pool v20260717 list. If it did, one user's dedicated
// line — its IP, country, server URL — would leak to every App user. v1 guards
// this in-memory (Class == NodeClassPrivate → skip); v2 is a parallel handler
// that must replicate the guard. This test fails loudly if the guard is ever
// dropped from api_v20260717_tunnels. Drives the real handler against dev MySQL.
func TestV20260717ExcludesPrivateNodes(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	uniq := time.Now().Format("20060102150405.000000")

	priv := SlaveNode{
		Ipv4:        "10.97.17." + uniq[len(uniq)-2:],
		SecretToken: "v2-priv-node-" + uniq,
		Country:     "JP",
		Region:      "japan",
		Name:        "v2-priv-node-" + uniq,
		Class:       NodeClassPrivate,
		IPType:      IPTypeResidential,
	}
	require.NoError(t, db.Get().Create(&priv).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&priv) })

	privURL := "k2v5://v2-priv-" + uniq + ".example:443?pin=sha256:abc"
	privTun := SlaveTunnel{
		Domain:      "v2-priv-" + uniq + ".example",
		SecretToken: "v2-priv-tun-" + uniq,
		Name:        "v2-priv-tun-" + uniq,
		Protocol:    TunnelProtocolK2V5,
		Port:        443,
		NodeID:      priv.ID,
		IsTest:      BoolPtr(false),
		HasRelay:    BoolPtr(false),
		ServerURL:   privURL,
	}
	require.NoError(t, db.Get().Create(&privTun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&privTun) })

	r := tunnelV20260717TestRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v20260717/tunnels", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code, "body=%s", w.Body.String())

	// The private node's unique server URL must not appear anywhere in the
	// shared-pool response — neither as a top-level item nor in any node object.
	require.NotContains(t, w.Body.String(), uniq,
		"private node must never surface in shared-pool /api/v20260717/tunnels")
}

// TestV20260717TunnelsCarriesTunnelToken：设备上下文存在时响应携带
// tunnelToken（Phase 0 分发路径之二）；无设备上下文（既有无 auth 测试路径）
// 时字段缺席、不 500。
func TestV20260717TunnelsCarriesTunnelToken(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")

	user := User{UUID: "usr-v2-tt-" + uniq, ExpiredAt: now + 86400}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	device := Device{UDID: "udid-v2-tt-" + uniq, UserID: user.ID, TokenIssueAt: now}
	require.NoError(t, db.Get().Create(&device).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&device) })

	r := gin.New()
	r.GET("/api/v20260717/tunnels", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: user.ID, UDID: device.UDID, Device: &device, User: &user})
		api_v20260717_tunnels(c)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v20260717/tunnels", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code, "body=%s", w.Body.String())

	var envelope struct {
		Data struct {
			TunnelToken string `json:"tunnelToken"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Data.TunnelToken, "设备上下文请求必须携带 tunnelToken")

	// 签出的必须是可通过校验的 tunnel token（真实可用，不是占位字符串）。
	_, _, err := validateTunnelToken(nil, envelope.Data.TunnelToken)
	require.NoError(t, err)

	// 无设备上下文：字段缺席，不失败（兼容既有 TestV20260717TunnelsShape 的驱动方式）。
	r2 := tunnelV20260717TestRouter()
	w2 := httptest.NewRecorder()
	r2.ServeHTTP(w2, httptest.NewRequest("GET", "/api/v20260717/tunnels", nil))
	require.Equal(t, 200, w2.Code)
	require.NotContains(t, w2.Body.String(), "tunnelToken", "无设备上下文时字段应 omitempty")
}
