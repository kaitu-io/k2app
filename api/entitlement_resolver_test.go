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

	// grace 状态但已超 ExpiresAt+7d 宽限期 => IsServiceable(now) == false（时间戳权威）。
	suspSub := PrivateNodeSubscription{
		UserID: suspendedOwner.ID, OrderID: suspendedOwner.ID, Status: PNStatusGrace, Region: "japan",
		IPType: IPTypeNonResidential, SlaveNodeID: &suspPriv.ID,
		PurchasedAt: now - 10*86400, ExpiresAt: now - 8*86400,
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

func TestHasActivePrivateLines(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")
	db.Get().Unscoped().Where("order_id = 0").Delete(&PrivateNodeSubscription{})

	// 无任何订阅 → false
	noneOwner := User{UUID: "usr-hapl-none-" + uniq}
	require.NoError(t, db.Get().Create(&noneOwner).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&noneOwner) })
	has, err := HasActivePrivateLines(context.Background(), db.Get(), noneOwner.ID, now)
	require.NoError(t, err)
	assert.False(t, has, "无订阅应为 false")

	// active 未过期 → true
	activeOwner := User{UUID: "usr-hapl-active-" + uniq}
	require.NoError(t, db.Get().Create(&activeOwner).Error)
	activeSub := PrivateNodeSubscription{
		UserID: activeOwner.ID, OrderID: activeOwner.ID, Status: PNStatusActive,
		Region: "japan", IPType: IPTypeNonResidential,
		PurchasedAt: now, ExpiresAt: now + 86400,
	}
	require.NoError(t, db.Get().Create(&activeSub).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&activeSub)
		db.Get().Unscoped().Delete(&activeOwner)
	})
	has, err = HasActivePrivateLines(context.Background(), db.Get(), activeOwner.ID, now)
	require.NoError(t, err)
	assert.True(t, has, "active 未过期应为 true")

	// grace 在 7d 宽限内 → true
	graceOwner := User{UUID: "usr-hapl-grace-" + uniq}
	require.NoError(t, db.Get().Create(&graceOwner).Error)
	graceSub := PrivateNodeSubscription{
		UserID: graceOwner.ID, OrderID: graceOwner.ID, Status: PNStatusGrace,
		Region: "japan", IPType: IPTypeNonResidential,
		PurchasedAt: now - 10*86400, ExpiresAt: now - 86400,
	}
	require.NoError(t, db.Get().Create(&graceSub).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&graceSub)
		db.Get().Unscoped().Delete(&graceOwner)
	})
	has, err = HasActivePrivateLines(context.Background(), db.Get(), graceOwner.ID, now)
	require.NoError(t, err)
	assert.True(t, has, "grace 宽限内应为 true")

	// grace 超过 7d 宽限 → false
	expiredOwner := User{UUID: "usr-hapl-exp-" + uniq}
	require.NoError(t, db.Get().Create(&expiredOwner).Error)
	expiredSub := PrivateNodeSubscription{
		UserID: expiredOwner.ID, OrderID: expiredOwner.ID, Status: PNStatusGrace,
		Region: "japan", IPType: IPTypeNonResidential,
		PurchasedAt: now - 20*86400, ExpiresAt: now - 8*86400,
	}
	require.NoError(t, db.Get().Create(&expiredSub).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&expiredSub)
		db.Get().Unscoped().Delete(&expiredOwner)
	})
	has, err = HasActivePrivateLines(context.Background(), db.Get(), expiredOwner.ID, now)
	require.NoError(t, err)
	assert.False(t, has, "超宽限应为 false")
}

// TestResolveGateway_ExhaustedDroppedViaUsage pins that the resolver now drops a
// private line whose NodeUsage is over the unified 500MB cutoff reserve — sourced
// from NodeUsage-by-NodeID, no CloudInstance. The healthy line stays so the router
// has a target. (G3: offline is NOT exercised here; offline is alarm-only, never
// hidden in the resolver.)
func TestResolveGateway_ExhaustedDroppedViaUsage(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")
	for _, ip := range []string{"10.99.8.1", "10.99.8.2"} {
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
	}
	for _, d := range []string{"priv-oq-exhausted.example", "priv-oq-healthy.example"} {
		db.Get().Unscoped().Where("domain = ?", d).Delete(&SlaveTunnel{})
	}
	db.Get().Unscoped().Where("order_id = 0").Delete(&PrivateNodeSubscription{})

	owner := User{UUID: "usr-oq-owner-" + uniq}
	require.NoError(t, db.Get().Create(&owner).Error)

	exhaustedNode := SlaveNode{
		Ipv4: "10.99.8.1", SecretToken: "soq1", Country: "JP", Region: "japan",
		Name: "priv-oq-exhausted", Class: NodeClassPrivate, PrivateOwnerUserID: &owner.ID,
	}
	require.NoError(t, db.Get().Create(&exhaustedNode).Error)
	healthyNode := SlaveNode{
		Ipv4: "10.99.8.2", SecretToken: "soq2", Country: "JP", Region: "japan",
		Name: "priv-oq-healthy", Class: NodeClassPrivate, PrivateOwnerUserID: &owner.ID,
	}
	require.NoError(t, db.Get().Create(&healthyNode).Error)

	// NodeUsage keyed by ipv4: exhausted node is over the 500MB reserve
	// (2T - 100MB used > 2T - 500MB cutoff), healthy node well under.
	for _, ip := range []string{exhaustedNode.Ipv4, healthyNode.Ipv4} {
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&NodeUsage{})
	}
	exhaustedUsage := NodeUsage{
		NodeID: exhaustedNode.ID, Ipv4: exhaustedNode.Ipv4, QuotaTotalBytes: 2 << 40,
		UsedBytes: (2 << 40) - (100 << 20), Epoch: now + 15*86400, LastReportAt: now,
	}
	require.NoError(t, db.Get().Create(&exhaustedUsage).Error)
	healthyUsage := NodeUsage{
		NodeID: healthyNode.ID, Ipv4: healthyNode.Ipv4, QuotaTotalBytes: 2 << 40,
		UsedBytes: 1 << 30, Epoch: now + 15*86400, LastReportAt: now,
	}
	require.NoError(t, db.Get().Create(&healthyUsage).Error)

	exhaustedTun := SlaveTunnel{
		Domain: "priv-oq-exhausted.example", SecretToken: "ttoq1", Name: "priv-oq-exhausted-tun",
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: exhaustedNode.ID,
		IsTest: BoolPtr(false), ServerURL: "k2v5://priv-oq-exhausted.example:443",
	}
	require.NoError(t, db.Get().Create(&exhaustedTun).Error)
	healthyTun := SlaveTunnel{
		Domain: "priv-oq-healthy.example", SecretToken: "ttoq2", Name: "priv-oq-healthy-tun",
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: healthyNode.ID,
		IsTest: BoolPtr(false), ServerURL: "k2v5://priv-oq-healthy.example:443",
	}
	require.NoError(t, db.Get().Create(&healthyTun).Error)

	exhaustedSub := PrivateNodeSubscription{
		UserID: owner.ID, OrderID: owner.ID*100 + 1, Status: PNStatusActive, Region: "japan",
		IPType: IPTypeNonResidential, SlaveNodeID: &exhaustedNode.ID, BoundIpv4: exhaustedNode.Ipv4,
		PurchasedAt: now, ExpiresAt: now + 86400,
	}
	require.NoError(t, db.Get().Create(&exhaustedSub).Error)
	healthySub := PrivateNodeSubscription{
		UserID: owner.ID, OrderID: owner.ID*100 + 2, Status: PNStatusActive, Region: "japan",
		IPType: IPTypeNonResidential, SlaveNodeID: &healthyNode.ID, BoundIpv4: healthyNode.Ipv4,
		PurchasedAt: now, ExpiresAt: now + 86400,
	}
	require.NoError(t, db.Get().Create(&healthySub).Error)

	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&exhaustedSub)
		db.Get().Unscoped().Delete(&healthySub)
		db.Get().Unscoped().Delete(&exhaustedTun)
		db.Get().Unscoped().Delete(&healthyTun)
		db.Get().Unscoped().Delete(&exhaustedUsage)
		db.Get().Unscoped().Delete(&healthyUsage)
		db.Get().Unscoped().Delete(&exhaustedNode)
		db.Get().Unscoped().Delete(&healthyNode)
		db.Get().Unscoped().Delete(&owner)
	})

	tunnels, err := ResolveGatewayPrivateTunnels(context.Background(), owner.ID, now)
	require.NoError(t, err)
	require.Len(t, tunnels, 1, "耗尽线应被剔除，只返回健康线")
	assert.Equal(t, healthyTun.ID, tunnels[0].ID)
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
