package center

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestSubsCarriesIPType asserts that /api/subs propagates the node's IPType
// field onto every SubsTunnel in the response — both through the private
// (gateway) branch (buildPrivateSubsTunnels) and through the shared-pool
// branch (fetchK2V5Tunnels).
//
// This is an additive field: old daemons ignore unknown JSON keys, so no
// backward-compat concern. New daemon Pick logic can prefer residential IPs.
//
// Requires dev MySQL (guarded by skipIfNoConfig).
func TestSubsCarriesIPType(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")

	// ── Gateway (private-branch) path ────────────────────────────────────
	t.Run("private gateway branch emits ipType", func(t *testing.T) {
		// Owner with expired shared membership — gateway branch must run first.
		owner := User{UUID: "usr-ipt-gw-" + uniq, ExpiredAt: now - 99999}
		require.NoError(t, db.Get().Create(&owner).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&owner) })

		udid := "udid-ipt-gw-" + uniq
		device := Device{
			UDID: udid, UserID: owner.ID, Remark: "IPType GW Device",
			IsGateway: true, TokenIssueAt: now,
		}
		require.NoError(t, db.Get().Create(&device).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&device) })

		token := GenerateTestToken(owner.ID, udid, time.Hour)
		require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
			Update("token_issue_at", tokenIssueAtOf(t, token)).Error)

		// Private node with IPType=residential.
		node := SlaveNode{
			Ipv4: "10.88.1.1", SecretToken: "ipt-s1", Country: "US",
			Region: "us-east", Name: "ipt-priv-us-" + uniq,
			Class: NodeClassPrivate, PrivateOwnerUserID: &owner.ID,
			IPType: IPTypeResidential,
		}
		require.NoError(t, db.Get().Create(&node).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })

		tun := SlaveTunnel{
			Domain: "ipt-priv-us-" + uniq + ".example", SecretToken: "ipt-tt1",
			Name: "ipt-priv-us-tun-" + uniq, Protocol: TunnelProtocolK2V5,
			Port: 443, NodeID: node.ID, IsTest: BoolPtr(false),
			ServerURL: "k2v5://ipt-priv-us-" + uniq + ".example:443",
		}
		require.NoError(t, db.Get().Create(&tun).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

		sub := PrivateNodeSubscription{
			UserID: owner.ID, OrderID: owner.ID, Status: PNStatusActive,
			Region: "us-east", IPType: IPTypeResidential, SlaveNodeID: &node.ID,
			PurchasedAt: now, ExpiresAt: now + 86400,
		}
		require.NoError(t, db.Get().Create(&sub).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&sub) })

		r := gin.New()
		r.GET("/api/subs", api_subs)

		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(udid+":"+token))
		req, _ := http.NewRequest("GET", "/api/subs", nil)
		req.Header.Set("Authorization", authHeader)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var resp SubsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.NotEmpty(t, resp.Tunnels, "private gateway must return at least one tunnel")
		assert.Equal(t, IPTypeResidential, resp.Tunnels[0].IPType,
			"SubsTunnel must carry ipType from the private node")
	})

	// ── Shared-pool path ─────────────────────────────────────────────────
	t.Run("shared pool branch emits ipType", func(t *testing.T) {
		// Non-gateway user with valid shared membership.
		user := User{UUID: "usr-ipt-app-" + uniq, ExpiredAt: now + 86400}
		require.NoError(t, db.Get().Create(&user).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

		udid := "udid-ipt-app-" + uniq
		device := Device{
			UDID: udid, UserID: user.ID, Remark: "IPType App Device",
			IsGateway: false, TokenIssueAt: now,
		}
		require.NoError(t, db.Get().Create(&device).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&device) })

		token := GenerateTestToken(user.ID, udid, time.Hour)
		require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
			Update("token_issue_at", tokenIssueAtOf(t, token)).Error)

		// Shared node with IPType=residential.
		sharedDomain := "ipt-shared-us-" + uniq + ".example"
		node := SlaveNode{
			Ipv4: "10.88.2.1", SecretToken: "ipt-s2", Country: "US",
			Region: "us-east", Name: "ipt-shared-us-" + uniq,
			Class: NodeClassShared, IPType: IPTypeResidential,
		}
		require.NoError(t, db.Get().Create(&node).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })

		tun := SlaveTunnel{
			Domain: sharedDomain, SecretToken: "ipt-tt2",
			Name: "ipt-shared-us-tun-" + uniq, Protocol: TunnelProtocolK2V5,
			Port: 443, NodeID: node.ID, IsTest: BoolPtr(false),
			ServerURL: "k2v5://" + sharedDomain + ":443",
		}
		require.NoError(t, db.Get().Create(&tun).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

		r := gin.New()
		r.GET("/api/subs", api_subs)

		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(udid+":"+token))
		req, _ := http.NewRequest("GET", "/api/subs", nil)
		req.Header.Set("Authorization", authHeader)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var resp SubsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

		// Find the tunnel we inserted (there may be other tunnels from test fixtures).
		// URL contains the domain after cred injection: k2v5://udid:token@domain:port
		found := false
		for _, st := range resp.Tunnels {
			if strings.Contains(st.URL, sharedDomain) {
				assert.Equal(t, IPTypeResidential, st.IPType,
					"shared-pool SubsTunnel must carry ipType from its node")
				found = true
				break
			}
		}
		assert.True(t, found, "our shared-pool tunnel must appear in the response, tunnels=%+v", resp.Tunnels)
	})
}
