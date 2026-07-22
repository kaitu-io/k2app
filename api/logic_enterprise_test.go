package center

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// seedEnterprise 建齐 customer/node×2/line×2/binding×2,返回 gateway device 与 tunnels 序
func seedEnterprise(t *testing.T) (devID uint64, tunnels []SlaveTunnel) {
	user := CreateTestUser(t)
	cust := &EnterpriseCustomer{Company: "S-" + time.Now().Format("150405.000000"), UserID: user.ID}
	require.NoError(t, db.Get().Create(cust).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(cust) })

	mk := func(ip, cc string, lineNo int) (*SlaveNode, *EnterpriseLine) {
		n := &SlaveNode{Ipv4: ip, SecretToken: "tok", Country: cc, Region: "r", Name: "n-" + ip, Class: NodeClassPrivate, PrivateOwnerUserID: &user.ID}
		require.NoError(t, db.Get().Create(n).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(n) })
		l := &EnterpriseLine{CustomerID: cust.ID, NodeID: n.ID, CountryCode: cc, LineNo: lineNo}
		require.NoError(t, db.Get().Create(l).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(l) })
		return n, l
	}
	n1, l1 := mk(uniqueTestIP(t), "ae", 1)
	n2, l2 := mk(uniqueTestIP(t), "gn", 1)

	dev := CreateTestDevice(t, user.ID, "ent-gw-"+time.Now().Format("150405.000000"))
	for _, b := range []*EnterpriseRouterBinding{
		{GatewayDeviceID: dev.ID, Slot: 1, LineID: l1.ID},
		{GatewayDeviceID: dev.ID, Slot: 5, LineID: l2.ID},
	} {
		require.NoError(t, db.Get().Create(b).Error)
		bb := b
		t.Cleanup(func() { db.Get().Unscoped().Delete(bb) })
	}
	// tunnels 序模拟 ResolveGatewayPrivateTunnels 输出:n2 在前、n1 在后(验证 index 不是槽序)
	tunnels = []SlaveTunnel{{NodeID: n2.ID, ServerURL: "k2v5://x@2.2.2.2:443"}, {NodeID: n1.ID, ServerURL: "k2v5://x@1.1.1.1:443"}}
	return dev.ID, tunnels
}

func TestResolveSlotBindings(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	devID, tunnels := seedEnterprise(t)
	got := resolveSlotBindings(context.Background(), devID, tunnels)
	require.Len(t, got, 2)
	require.Equal(t, SubsSlotBinding{Slot: 1, Country: "ae", Index: 1, TunnelIndex: 1}, got[0])
	require.Equal(t, SubsSlotBinding{Slot: 5, Country: "gn", Index: 1, TunnelIndex: 0}, got[1])
}

func TestResolveSlotBindings_NonEnterpriseNil(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	require.Nil(t, resolveSlotBindings(context.Background(), 999999999, nil))
}

func TestResolveSlotBindings_LineNodeNotServiceable(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	devID, tunnels := seedEnterprise(t)
	// 只给一个 node 的 tunnels → 另一条线路的槽位被省略(k2r 侧该槽 disabled = fail-closed 收敛)
	got := resolveSlotBindings(context.Background(), devID, tunnels[:1])
	require.Len(t, got, 1)
	require.Equal(t, 5, got[0].Slot)
}
