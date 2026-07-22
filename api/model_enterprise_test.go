package center

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// uniqueTestIP generates a unique 10.x.x.x address for SlaveNode.Ipv4's
// uniqueIndex, keyed off the current time so parallel/repeated test runs
// don't collide.
func uniqueTestIP(t *testing.T) string {
	t.Helper()
	now := time.Now()
	return fmt.Sprintf("10.250.%d.%d", (now.Unix()/60)%256, now.UnixNano()%256)
}

func TestEnterpriseModels_Constraints(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	user := CreateTestUser(t)
	cust := &EnterpriseCustomer{Company: "Test Studio " + time.Now().Format("20060102150405.000000"), Contact: "a@b.c", UserID: user.ID}
	require.NoError(t, db.Get().Create(cust).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(cust) })
	require.Equal(t, "active", cust.Status) // default

	node := &SlaveNode{Ipv4: uniqueTestIP(t), SecretToken: "tok", Country: "ae", Region: "dubai", Name: "ent-node", Class: NodeClassPrivate, PrivateOwnerUserID: &user.ID}
	require.NoError(t, db.Get().Create(node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(node) })

	line := &EnterpriseLine{CustomerID: cust.ID, NodeID: node.ID, CountryCode: "ae", LineNo: 1}
	require.NoError(t, db.Get().Create(line).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(line) })

	// 同节点第二条线路 → 唯一约束拒绝
	dupNode := &EnterpriseLine{CustomerID: cust.ID, NodeID: node.ID, CountryCode: "ae", LineNo: 2}
	require.Error(t, db.Get().Create(dupNode).Error)

	dev := CreateTestDevice(t, user.ID, "ent-gw-"+time.Now().Format("150405.000000"))
	b := &EnterpriseRouterBinding{GatewayDeviceID: dev.ID, Slot: 1, LineID: line.ID}
	require.NoError(t, db.Get().Create(b).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(b) })

	// 同设备同槽第二绑定 → 拒绝
	require.Error(t, db.Get().Create(&EnterpriseRouterBinding{GatewayDeviceID: dev.ID, Slot: 1, LineID: line.ID}).Error)
	// 同线路绑到别的槽 → 拒绝(line_id unique)
	require.Error(t, db.Get().Create(&EnterpriseRouterBinding{GatewayDeviceID: dev.ID, Slot: 2, LineID: line.ID}).Error)
}
