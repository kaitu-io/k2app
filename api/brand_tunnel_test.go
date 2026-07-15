package center

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 纯内存单测：品牌可见性过滤谓词
func TestTunnelBrandVisibilityPredicate(t *testing.T) {
	nodeKaituOnly := &SlaveNode{}                                                    // 零值 = 默认：kaitu 可见
	nodeBoth := &SlaveNode{VisibleKaitu: BoolPtr(true), VisibleOverleap: BoolPtr(true)}
	nodeOverleapOnly := &SlaveNode{VisibleKaitu: BoolPtr(false), VisibleOverleap: BoolPtr(true)}

	assert.True(t, nodeKaituOnly.VisibleTo(BrandKaitu))
	assert.False(t, nodeKaituOnly.VisibleTo(BrandOverleap))
	assert.True(t, nodeBoth.VisibleTo(BrandKaitu))
	assert.True(t, nodeBoth.VisibleTo(BrandOverleap))
	assert.False(t, nodeOverleapOnly.VisibleTo(BrandKaitu))
	assert.True(t, nodeOverleapOnly.VisibleTo(BrandOverleap))
}

// TestApiSubs_SharedPool_FiltersByUserBrand is the integration counterpart:
// a default (kaitu-visible-only, zero-value VisibleKaitu/VisibleOverleap)
// node+tunnel must be invisible to an overleap user's /api/subs response but
// still visible to a kaitu user — proving existing/default nodes remain
// zero-impact for kaitu (today's behavior) while overleap correctly gets
// nothing by default. Mirrors the setup in TestApiSubs_SharedPool_ExcludesPrivateNodes.
// Needs dev MySQL.
func TestApiSubs_SharedPool_FiltersByUserBrand(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")

	// Default-visibility node: zero-value VisibleKaitu/VisibleOverleap →
	// kaitu-visible, overleap-invisible (Task 3 semantics).
	domain := "brand-filter-" + uniq + ".example"
	node := SlaveNode{
		Ipv4: "10.99.8.1", SecretToken: "brand-s1", Country: "JP", Region: "japan",
		Name: "brand-filter-jp-" + uniq, Class: NodeClassShared,
	}
	require.NoError(t, db.Get().Create(&node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })

	tun := SlaveTunnel{
		Domain: domain, SecretToken: "brand-t1", Name: "brand-filter-jp-tun-" + uniq,
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: node.ID,
		IsTest: BoolPtr(false), ServerURL: "k2v5://" + domain + ":443",
	}
	require.NoError(t, db.Get().Create(&tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

	subsForBrand := func(brand Brand) *httptest.ResponseRecorder {
		user := User{UUID: "usr-brandsubs-" + string(brand) + "-" + uniq, ExpiredAt: now + 86400, Brand: string(brand)}
		require.NoError(t, db.Get().Create(&user).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

		udid := "udid-brandsubs-" + string(brand) + "-" + uniq
		device := Device{UDID: udid, UserID: user.ID, Remark: "brand-test", IsGateway: false, TokenIssueAt: now}
		require.NoError(t, db.Get().Create(&device).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&device) })

		token := GenerateTestToken(user.ID, udid, time.Hour)
		require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
			Update("token_issue_at", tokenIssueAtOf(t, token)).Error)

		authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(udid+":"+token))
		r := gin.New()
		r.GET("/api/subs", api_subs)

		req, _ := http.NewRequest("GET", "/api/subs", nil)
		req.Header.Set("Authorization", authHeader)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	overleapResp := subsForBrand(BrandOverleap)
	require.Equal(t, http.StatusOK, overleapResp.Code,
		"overleap user must still get 200 (empty tunnel list, not an error), body=%s", overleapResp.Body.String())
	assert.NotContains(t, overleapResp.Body.String(), domain,
		"default node (kaitu-visible only) must NOT leak to an overleap user")

	kaituResp := subsForBrand(BrandKaitu)
	require.Equal(t, http.StatusOK, kaituResp.Code, "kaitu user must get 200, body=%s", kaituResp.Body.String())
	assert.Contains(t, kaituResp.Body.String(), domain,
		"default node must remain visible to a kaitu user — zero regression for existing/default nodes")
}

// TestApiK2Relays_FiltersByUserBrand — /api/relays returns node IPv4/IPv6 to App
// users, so a kaitu-only (default-visibility) relay node must be invisible to an
// overleap user, visible to a kaitu user, and visible to an admin regardless of
// the admin's own brand (admin bypass, same convention as api_tunnel.go).
// Mirrors TestApiK2Relays_ExcludesPrivateNodes' direct-handler setup, but with
// an injected authContext since api_k2_relays reads ReqUser(c). Needs dev MySQL.
func TestApiK2Relays_FiltersByUserBrand(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	uniq := time.Now().Format("20060102150405.000000")

	// Default-visibility relay node: zero-value VisibleKaitu/VisibleOverleap →
	// kaitu-visible, overleap-invisible.
	relayIP := "10.99.12.1"
	node := SlaveNode{
		Ipv4: relayIP, SecretToken: "relay-brand-s1", Country: "JP", Region: "japan",
		Name: "relay-brand-jp-" + uniq, Class: NodeClassShared,
	}
	require.NoError(t, db.Get().Create(&node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })

	tun := SlaveTunnel{
		Domain: "relay-brand-jp-" + uniq + ".example", SecretToken: "relay-brand-tt1",
		Name: "relay-brand-jp-tun-" + uniq, Protocol: TunnelProtocolK2V5, Port: 443,
		NodeID: node.ID, IsTest: BoolPtr(false), HasRelay: BoolPtr(true),
		ServerURL: "k2v5://relay-brand-jp.example:443",
	}
	require.NoError(t, db.Get().Create(&tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

	relaysFor := func(user *User) []DataRelay {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/relays", nil)
		c.Set("authContext", &authContext{UserID: user.ID, User: user})

		api_k2_relays(c)

		resp, err := ParseResponseData[DataRelayListResponse](w)
		require.NoError(t, err)
		return resp.Relays
	}
	hasIP := func(relays []DataRelay, ip string) bool {
		for _, r := range relays {
			if r.Ipv4 == ip {
				return true
			}
		}
		return false
	}

	overleapUser := &User{ID: 999901, Brand: string(BrandOverleap)}
	assert.False(t, hasIP(relaysFor(overleapUser), relayIP),
		"default (kaitu-only) relay node IP must NOT leak to an overleap user")

	kaituUser := &User{ID: 999902, Brand: string(BrandKaitu)}
	assert.True(t, hasIP(relaysFor(kaituUser), relayIP),
		"default relay node must remain visible to a kaitu user — zero regression")

	// Admin bypass: an overleap-brand ADMIN still sees the kaitu-only node
	// (triage visibility, mirrors the isTest/quota-hide admin bypass).
	adminOverleap := &User{ID: 999903, Brand: string(BrandOverleap), IsAdmin: BoolPtr(true)}
	assert.True(t, hasIP(relaysFor(adminOverleap), relayIP),
		"admin must see brand-hidden nodes (admin bypass) regardless of own brand")
}
