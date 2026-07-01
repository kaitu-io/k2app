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

// TestSlavePrivateExclusion_AccelerateTunnels — CAPABILITY MATRIX (mesh→private ❌)
//
// /slave/accelerate-tunnels feeds the slave-to-slave relay mesh. A private node
// (single-owner dedicated VPS) must never appear here, or its IP leaks into the
// shared mesh and reaches every relay/App. Mirrors api_relay.go / api_subs.go.
// Needs dev MySQL.
func TestSlavePrivateExclusion_AccelerateTunnels(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")

	owner := User{UUID: "usr-accel-" + uniq, ExpiredAt: now + 86400}
	require.NoError(t, db.Get().Create(&owner).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&owner) })

	sharedIP := "10.98.11.1"
	privIP := "10.98.11.2"
	sharedDomain := "accel-shared-" + uniq + ".example"
	privDomain := "accel-priv-" + uniq + ".example"

	// Pre-purge fixed IPv4s + tunnel domains (Unscoped, in case a prior run leaked).
	db.Get().Unscoped().Where("ipv4 IN ?", []string{sharedIP, privIP}).Delete(&SlaveNode{})
	db.Get().Unscoped().Where("domain IN ?", []string{sharedDomain, privDomain}).Delete(&SlaveTunnel{})

	shared := SlaveNode{
		Ipv4: sharedIP, SecretToken: "accel-shared-s1", Country: "JP", Region: "japan",
		Name: "accel-shared-jp-" + uniq, Class: NodeClassShared,
	}
	require.NoError(t, db.Get().Create(&shared).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&shared) })

	priv := SlaveNode{
		Ipv4: privIP, SecretToken: "accel-priv-s1", Country: "JP", Region: "japan",
		Name: "accel-priv-jp-" + uniq, Class: NodeClassPrivate, PrivateOwnerUserID: &owner.ID,
	}
	require.NoError(t, db.Get().Create(&priv).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&priv) })

	sharedTun := SlaveTunnel{
		Domain: sharedDomain, SecretToken: "accel-shared-tt1",
		Name: "accel-shared-tun-" + uniq, Protocol: TunnelProtocolK2V5, Port: 443,
		NodeID: shared.ID, IsTest: BoolPtr(false),
		ServerURL: "k2v5://" + sharedDomain + ":443",
	}
	require.NoError(t, db.Get().Create(&sharedTun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&sharedTun) })

	privTun := SlaveTunnel{
		Domain: privDomain, SecretToken: "accel-priv-tt1",
		Name: "accel-priv-tun-" + uniq, Protocol: TunnelProtocolK2V5, Port: 443,
		NodeID: priv.ID, IsTest: BoolPtr(false),
		ServerURL: "k2v5://" + privDomain + ":443",
	}
	require.NoError(t, db.Get().Create(&privTun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&privTun) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/slave/accelerate-tunnels", nil)

	api_slave_accelerate_tunnels(c)

	resp, err := ParseResponseData[ListResult[AcceleratePath]](w)
	require.NoError(t, err)

	var sawShared bool
	for _, p := range resp.Items {
		assert.NotEqual(t, privIP, p.Ip, "shared mesh must NOT leak the private node IP (mesh→private ❌)")
		if p.Ip == sharedIP {
			sawShared = true
		}
	}
	assert.True(t, sawShared, "shared node IP must still be present in the accelerate-tunnels response")
}

// TestSlavePrivateExclusion_ResolveDomain — CAPABILITY MATRIX (mesh→private ❌)
//
// /slave/resolve-domain resolves a domain to a node IP for the mesh. A private
// node must never be resolvable here, or its IP leaks. Needs dev MySQL.
func TestSlavePrivateExclusion_ResolveDomain(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")

	owner := User{UUID: "usr-resolve-" + uniq, ExpiredAt: now + 86400}
	require.NoError(t, db.Get().Create(&owner).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&owner) })

	sharedIP := "10.97.11.1"
	privIP := "10.97.11.2"
	sharedDomain := "resolve-shared-" + uniq + ".example"
	privDomain := "resolve-priv-" + uniq + ".example"

	db.Get().Unscoped().Where("ipv4 IN ?", []string{sharedIP, privIP}).Delete(&SlaveNode{})
	db.Get().Unscoped().Where("domain IN ?", []string{sharedDomain, privDomain}).Delete(&SlaveTunnel{})

	shared := SlaveNode{
		Ipv4: sharedIP, SecretToken: "resolve-shared-s1", Country: "JP", Region: "japan",
		Name: "resolve-shared-jp-" + uniq, Class: NodeClassShared,
	}
	require.NoError(t, db.Get().Create(&shared).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&shared) })

	priv := SlaveNode{
		Ipv4: privIP, SecretToken: "resolve-priv-s1", Country: "JP", Region: "japan",
		Name: "resolve-priv-jp-" + uniq, Class: NodeClassPrivate, PrivateOwnerUserID: &owner.ID,
	}
	require.NoError(t, db.Get().Create(&priv).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&priv) })

	sharedTun := SlaveTunnel{
		Domain: sharedDomain, SecretToken: "resolve-shared-tt1",
		Name: "resolve-shared-tun-" + uniq, Protocol: TunnelProtocolK2V5, Port: 443,
		NodeID: shared.ID, IsTest: BoolPtr(false),
		ServerURL: "k2v5://" + sharedDomain + ":443",
	}
	require.NoError(t, db.Get().Create(&sharedTun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&sharedTun) })

	privTun := SlaveTunnel{
		Domain: privDomain, SecretToken: "resolve-priv-tt1",
		Name: "resolve-priv-tun-" + uniq, Protocol: TunnelProtocolK2V5, Port: 443,
		NodeID: priv.ID, IsTest: BoolPtr(false),
		ServerURL: "k2v5://" + privDomain + ":443",
	}
	require.NoError(t, db.Get().Create(&privTun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&privTun) })

	// Shared domain still resolves.
	{
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/slave/resolve-domain?domain="+sharedDomain, nil)
		api_slave_resolve_domain(c)

		resp, err := ParseResponseData[ResolveDomainResponse](w)
		require.NoError(t, err)
		assert.True(t, resp.Found, "shared domain must still resolve")
		assert.Equal(t, sharedIP, resp.Ip, "shared domain must resolve to the shared node IP")
	}

	// Private domain must NOT resolve (no IP leak).
	{
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/slave/resolve-domain?domain="+privDomain, nil)
		api_slave_resolve_domain(c)

		resp, err := ParseResponseData[ResolveDomainResponse](w)
		require.NoError(t, err)
		assert.NotEqual(t, privIP, resp.Ip, "resolve-domain must NOT leak the private node IP (mesh→private ❌)")
		assert.False(t, resp.Found, "private domain must not be resolvable in the shared mesh")
	}
}
