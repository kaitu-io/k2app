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

	// 自愈：清理上次中断运行残留的固定 IPv4 节点/隧道（idx_slave_nodes_ipv4 + slave_tunnels.domain 唯一索引）。
	for _, ip := range []string{"10.99.7.1", "10.99.7.2"} {
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
	}
	for _, d := range []string{"priv-jp.example", "priv-jp-susp.example"} {
		db.Get().Unscoped().Where("domain = ?", d).Delete(&SlaveTunnel{})
	}
	// 清理历史 order_id=0 的孤儿订阅：order_id 现为唯一索引，order_id=0 的残留会撞唯一键。
	db.Get().Unscoped().Where("order_id = 0").Delete(&PrivateNodeSubscription{})

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
		UserID: owner.ID, OrderID: owner.ID, Status: PNStatusActive, Region: "japan",
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

	// 状态门控：拥有专属节点+隧道，但唯一一条订阅不可服务（grace 已过期），应返回空。
	suspendedOwner := User{UUID: "usr-pn-susp-" + uniq, ExpiredAt: now + 86400}
	require.NoError(t, db.Get().Create(&suspendedOwner).Error)

	suspPriv := SlaveNode{
		Ipv4: "10.99.7.2", SecretToken: "s8", Country: "JP", Region: "japan",
		Name: "priv-jp-susp", Class: NodeClassPrivate, PrivateOwnerUserID: &suspendedOwner.ID,
	}
	require.NoError(t, db.Get().Create(&suspPriv).Error)

	suspTun := SlaveTunnel{
		Domain: "priv-jp-susp.example", SecretToken: "tt8", Name: "priv-jp-susp-tun",
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: suspPriv.ID,
		IsTest: BoolPtr(false), ServerURL: "k2v5://priv-jp-susp.example:443",
	}
	require.NoError(t, db.Get().Create(&suspTun).Error)

	// grace 状态但 GraceUntil 已过去 => IsServiceable(now) == false。
	suspSub := PrivateNodeSubscription{
		UserID: suspendedOwner.ID, OrderID: suspendedOwner.ID, Status: PNStatusGrace, Region: "japan",
		IPType: IPTypeNonResidential, SlaveNodeID: &suspPriv.ID,
		PurchasedAt: now - 172800, ExpiresAt: now - 86400, GraceUntil: now - 1,
	}
	require.NoError(t, db.Get().Create(&suspSub).Error)

	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&suspSub)
		db.Get().Unscoped().Delete(&suspTun)
		db.Get().Unscoped().Delete(&suspPriv)
		db.Get().Unscoped().Delete(&suspendedOwner)
	})

	gated, err := ResolveGatewayPrivateTunnels(context.Background(), suspendedOwner.ID, now)
	require.NoError(t, err)
	assert.Len(t, gated, 0, "唯一订阅不可服务（grace 过期）应返回空")
}

func TestBuildPrivateSubsTunnels(t *testing.T) {
	node := &SlaveNode{ID: 1, Ipv4: "10.99.7.1", Country: "JP", Class: NodeClassPrivate}
	tunnels := []SlaveTunnel{
		{ID: 1, NodeID: 1, Node: node, ServerURL: "k2v5://priv-jp.example:443"},
		{ID: 2, NodeID: 1, Node: node, ServerURL: ""},      // 空 URL 应跳过
		{ID: 3, NodeID: 1, Node: nil, ServerURL: "k2v5://x"}, // Node nil 应跳过
	}
	items := buildPrivateSubsTunnels(tunnels, "udid-x", "tok-y")
	require.Len(t, items, 1, "只有 1 条有效隧道")
	assert.Equal(t, 0.5, items[0].RecommendScore)
	assert.Equal(t, 50, items[0].Weight) // round(0.5 * 100)
	assert.Contains(t, items[0].URL, "udid-x:tok-y@")
}
