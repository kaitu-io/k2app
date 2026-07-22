package center

import (
	"testing"

	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func TestAdminEnterprise_LineValidation(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	user := CreateTestUser(t)
	cust := &EnterpriseCustomer{Company: "V", UserID: user.ID}
	require.NoError(t, db.Get().Create(cust).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(cust) })

	// 节点必须 Class=private 且归属客户账号
	sharedNode := &SlaveNode{Ipv4: uniqueTestIP(t), SecretToken: "t", Country: "ae", Region: "r", Name: "shared"}
	require.NoError(t, db.Get().Create(sharedNode).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sharedNode) })

	err := adminCreateEnterpriseLine(cust.ID, sharedNode.ID, "ae", 1)
	require.Error(t, err, "shared node must be rejected")

	otherUser := CreateTestUser(t)
	foreignNode := &SlaveNode{Ipv4: uniqueTestIP(t), SecretToken: "t", Country: "ae", Region: "r", Name: "foreign", Class: NodeClassPrivate, PrivateOwnerUserID: &otherUser.ID}
	require.NoError(t, db.Get().Create(foreignNode).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(foreignNode) })
	require.Error(t, adminCreateEnterpriseLine(cust.ID, foreignNode.ID, "ae", 1), "foreign-owned node must be rejected")

	// country 非法
	ownNode := &SlaveNode{Ipv4: uniqueTestIP(t), SecretToken: "t", Country: "ae", Region: "r", Name: "own", Class: NodeClassPrivate, PrivateOwnerUserID: &user.ID}
	require.NoError(t, db.Get().Create(ownNode).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(ownNode) })
	require.Error(t, adminCreateEnterpriseLine(cust.ID, ownNode.ID, "AE", 1), "uppercase country must be rejected")
	require.Error(t, adminCreateEnterpriseLine(cust.ID, ownNode.ID, "xyz", 1), "3-letter country must be rejected")

	// 合法创建
	line, err := adminCreateEnterpriseLineFull(cust.ID, ownNode.ID, "ae", 1)
	require.NoError(t, err)
	t.Cleanup(func() { db.Get().Unscoped().Delete(line) })
}

func TestAdminEnterprise_BindingUpsertAndSlotRange(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	devID, _ := seedEnterprise(t) // Task 2 的 fixture(已绑 slot 1、5)
	// slot 越界
	require.Error(t, adminUpsertBinding(devID, 0, 1))
	require.Error(t, adminUpsertBinding(devID, 9, 1))
	// 删除未绑定线路的保护:绑定中的 line 不可删
	var b EnterpriseRouterBinding
	require.NoError(t, db.Get().Where("gateway_device_id = ? AND slot = 1", devID).First(&b).Error)
	require.Error(t, adminDeleteEnterpriseLine(b.LineID), "bound line must not be deletable")
}
