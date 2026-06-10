package center

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestApiK2Relays_ExcludesPrivateNodes — CAPABILITY MATRIX (App→private ❌)
//
// /api/relays returns node IPv4/IPv6 to App users. A private node (single-owner
// dedicated VPS) must never appear here, or its IP/country leaks to every App
// user. Reaches the handler with no auth context (ReqUser→nil → isAdmin=false),
// which is exactly the non-admin App path. Needs dev MySQL.
func TestApiK2Relays_ExcludesPrivateNodes(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")

	owner := User{UUID: "usr-relay-" + uniq, ExpiredAt: now + 86400}
	require.NoError(t, db.Get().Create(&owner).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&owner) })

	privIP := "10.99.11.1"
	priv := SlaveNode{
		Ipv4: privIP, SecretToken: "relay-priv-s1", Country: "JP", Region: "japan",
		Name: "relay-priv-jp-" + uniq, Class: NodeClassPrivate, PrivateOwnerUserID: &owner.ID,
	}
	require.NoError(t, db.Get().Create(&priv).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&priv) })

	// A private node with a relay-capable k2v5 tunnel — the worst-case leak.
	tun := SlaveTunnel{
		Domain: "relay-priv-jp-" + uniq + ".example", SecretToken: "relay-priv-tt1",
		Name: "relay-priv-jp-tun-" + uniq, Protocol: TunnelProtocolK2V5, Port: 443,
		NodeID: priv.ID, IsTest: BoolPtr(false), HasRelay: BoolPtr(true),
		ServerURL: "k2v5://relay-priv-jp.example:443",
	}
	require.NoError(t, db.Get().Create(&tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/relays", nil)

	api_k2_relays(c)

	resp, err := ParseResponseData[DataRelayListResponse](w)
	require.NoError(t, err)
	for _, r := range resp.Relays {
		assert.NotEqual(t, privIP, r.Ipv4, "shared relay list must NOT leak the private node IP (App→private ❌)")
	}
}
