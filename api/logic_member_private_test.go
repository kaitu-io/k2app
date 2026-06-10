package center

import (
	"context"
	"testing"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyOrderToBuyer_PrivateNode 验证付费订单按 Plan.Kind 分流：
// private_node 套餐建 pending 订阅 + 异步开通，且不碰 User.ExpiredAt（独立时钟）。
func TestApplyOrderToBuyer_PrivateNode(t *testing.T) {
	skipIfNoConfig(t)

	ctx := context.Background()
	now := time.Now()
	stamp := now.Format("20060102150405")

	// 1. owner
	owner := User{
		UUID:      "usr-pn-" + stamp,
		ExpiredAt: now.Add(30 * 24 * time.Hour).Unix(),
	}
	require.NoError(t, db.Get().Create(&owner).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&owner) })
	expiredAtBefore := owner.ExpiredAt

	// 2. private_node Plan + spec
	plan := Plan{
		PID:   "pn-test-" + stamp,
		Label: "Test Private Node",
		Price: 9900,
		Month: 12,
		Kind:  PlanKindPrivateNode,
		Tier:  TierBasic,
	}
	require.NoError(t, db.Get().Create(&plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&plan) })

	spec := PrivateNodePlanSpec{
		PlanID:            plan.ID,
		Provider:          "aws_lightsail",
		IPType:            IPTypeNonResidential,
		AllowedRegions:    `["japan"]`,
		ImageID:           "img-test",
		BundleID:          "nano_2_0",
		TrafficTotalBytes: 2 * 1024 * 1024 * 1024 * 1024, // 2TB
	}
	require.NoError(t, db.Get().Create(&spec).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&spec) })

	// 3. paid order with the plan embedded into Meta (valid JSON via SetPlan)
	isPaid := true
	paidAt := now
	order := Order{
		UUID:      "ord-pn-" + stamp,
		Title:     "Test Private Node Order",
		UserID:    owner.ID,
		PayAmount: 9900,
		IsPaid:    &isPaid,
		PaidAt:    &paidAt,
		Meta:      "{}",
	}
	require.NoError(t, order.SetPlan(&plan))
	require.NoError(t, db.Get().Create(&order).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&order) })
	t.Cleanup(func() {
		db.Get().Unscoped().Where("order_id = ?", order.ID).Delete(&PrivateNodeSubscription{})
	})

	// 4. apply order
	require.NoError(t, applyOrderToBuyer(ctx, db.Get(), &order))

	// (a) exactly one pending sub with a claim token
	var subs []PrivateNodeSubscription
	require.NoError(t, db.Get().Where("order_id = ?", order.ID).Find(&subs).Error)
	require.Len(t, subs, 1)
	assert.Equal(t, PNStatusPending, subs[0].Status)
	assert.NotEmpty(t, subs[0].ProvisionClaimToken)
	assert.Equal(t, owner.ID, subs[0].UserID)
	assert.Equal(t, plan.ID, subs[0].PlanID)
	assert.Equal(t, IPTypeNonResidential, subs[0].IPType)
	assert.Equal(t, "japan", subs[0].Region)

	// (b) owner reloaded → ExpiredAt UNCHANGED (private must not touch shared clock)
	var reloaded User
	require.NoError(t, db.Get().First(&reloaded, owner.ID).Error)
	assert.Equal(t, expiredAtBefore, reloaded.ExpiredAt)

	// (c) idempotency: a second apply with the SAME order leaves still exactly 1 sub
	//     (OrderID uniqueIndex enforces it — second Create errors on the duplicate key).
	err := applyOrderToBuyer(ctx, db.Get(), &order)
	assert.Error(t, err)
	var count int64
	require.NoError(t, db.Get().Model(&PrivateNodeSubscription{}).Where("order_id = ?", order.ID).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
