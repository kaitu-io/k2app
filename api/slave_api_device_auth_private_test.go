package center

import (
	"testing"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/stretchr/testify/require"
)

// 验证：专属节点只放行主人。非主人即便会员有效也拒绝；逻辑层校验。
func TestHandleSlaveJWTAuth_PrivateNode_OwnerOnly(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()

	owner := User{ExpiredAt: now + 86400}
	require.NoError(t, db.Get().Create(&owner).Error)

	node := SlaveNode{
		Ipv4:               "10.99.0.1",
		SecretToken:        "secret-private-1",
		Country:            "HK",
		Region:             "hongkong",
		Name:               "private-hk-1",
		Class:              NodeClassPrivate,
		PrivateOwnerUserID: &owner.ID,
	}
	require.NoError(t, db.Get().Create(&node).Error)

	sub := PrivateNodeSubscription{
		UserID: owner.ID, Status: PNStatusActive, Region: "hongkong",
		IPType: IPTypeNonResidential, SlaveNodeID: &node.ID,
		PurchasedAt: now, ExpiresAt: now + 86400,
	}
	require.NoError(t, db.Get().Create(&sub).Error)
	node.PrivateSubID = &sub.ID
	require.NoError(t, db.Get().Save(&node).Error)

	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&sub)
		db.Get().Unscoped().Delete(&node)
		db.Get().Unscoped().Delete(&owner)
	})

	require.True(t, AuthorizeNodeAccess(&owner, &node, &sub, now), "owner 应放行")

	stranger := User{ID: owner.ID + 100000, ExpiredAt: now + 86400}
	require.False(t, AuthorizeNodeAccess(&stranger, &node, &sub, now), "非主人应拒绝")
}
