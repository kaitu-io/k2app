package center

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// gateway 用户只拿到自己的专属节点隧道；拿不到共享池隧道。
func TestResolveGatewayPrivateTunnels(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")
	owner := User{UUID: "usr-pn-owner-" + uniq, ExpiredAt: now - 99999} // 故意过期：专属节点不看共享会员
	require.NoError(t, db.Get().Create(&owner).Error)

	priv := SlaveNode{
		Ipv4: "10.99.7.1", SecretToken: "s7", Country: "JP", Region: "japan",
		Name: "priv-jp", Class: NodeClassPrivate, PrivateOwnerUserID: &owner.ID,
	}
	require.NoError(t, db.Get().Create(&priv).Error)

	tun := SlaveTunnel{
		Domain: "priv-jp.example", SecretToken: "tt7", Name: "priv-jp-tun",
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: priv.ID,
		IsTest: BoolPtr(false), ServerURL: "k2v5://priv-jp.example:443",
	}
	require.NoError(t, db.Get().Create(&tun).Error)

	sub := PrivateNodeSubscription{
		UserID: owner.ID, Status: PNStatusActive, Region: "japan",
		IPType: IPTypeNonResidential, SlaveNodeID: &priv.ID,
		PurchasedAt: now, ExpiresAt: now + 86400,
	}
	require.NoError(t, db.Get().Create(&sub).Error)

	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&sub)
		db.Get().Unscoped().Delete(&tun)
		db.Get().Unscoped().Delete(&priv)
		db.Get().Unscoped().Delete(&owner)
	})

	tunnels, err := ResolveGatewayPrivateTunnels(context.Background(), owner.ID, now)
	require.NoError(t, err)
	require.Len(t, tunnels, 1, "应只返回该用户的 1 条专属隧道")
	assert.Equal(t, tun.ID, tunnels[0].ID)
	require.NotNil(t, tunnels[0].Node)
	assert.Equal(t, NodeClassPrivate, tunnels[0].Node.Class)
	assert.Equal(t, "k2v5://priv-jp.example:443", tunnels[0].ServerURL)

	other := User{UUID: "usr-pn-other-" + uniq, ExpiredAt: now + 86400}
	require.NoError(t, db.Get().Create(&other).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&other) })

	empty, err := ResolveGatewayPrivateTunnels(context.Background(), other.ID, now)
	require.NoError(t, err)
	assert.Len(t, empty, 0, "无专属订阅应返回空")
}
