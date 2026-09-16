package center

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func seedPaidRouterOrder(t *testing.T, user *User, plan *Plan, shipping string) *Order {
	t.Helper()
	isPaid, paidAt := true, time.Now()
	o := &Order{UUID: generateId("ord-rt"), Title: plan.Label, UserID: user.ID, PayAmount: plan.Price,
		IsPaid: &isPaid, PaidAt: &paidAt, Meta: "{}", PrivateNodeRegion: "japan"}
	if shipping != "" {
		o.RouterShipping = &shipping
	}
	require.NoError(t, o.SetPlan(plan))
	require.NoError(t, db.Get().Create(o).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("order_id = ?", o.ID).Delete(&RouterFulfillment{})
		db.Get().Unscoped().Where("order_id = ?", o.ID).Delete(&PrivateNodeSubscription{})
		db.Get().Unscoped().Delete(o)
	})
	return o
}

func TestApplyOrderToBuyer_RouterHardware_CreatesLineAndFulfillment(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	user := CreateTestUser(t)
	before := user.ExpiredAt
	plan := seedRouterPlan(t, "router-apply-hw", "redmi-ax6s", 39900)
	order := seedPaidRouterOrder(t, user, plan, `{"name":"张三","phone":"138","address":"上海"}`)

	var ids []uint64
	require.NoError(t, applyOrderToBuyer(ctx, db.Get(), order, &ids))

	var sub PrivateNodeSubscription
	require.NoError(t, db.Get().Where("order_id = ?", order.ID).First(&sub).Error)
	assert.Equal(t, PNStatusPending, sub.Status)
	assert.Equal(t, "japan", sub.Region)
	assert.Equal(t, []uint64{sub.ID}, ids, "must be queued for provisioning post-commit")

	var f RouterFulfillment
	require.NoError(t, db.Get().Where("order_id = ?", order.ID).First(&f).Error)
	assert.Equal(t, RouterStagePaid, f.Stage)
	assert.Equal(t, "redmi-ax6s", f.HardwareSKU)
	assert.Equal(t, sub.ID, f.SubID)
	assert.Equal(t, user.ID, f.UserID)

	var reloaded User
	require.NoError(t, db.Get().First(&reloaded, user.ID).Error)
	assert.Equal(t, before, reloaded.ExpiredAt, "router order must not touch the shared membership clock")
	assert.True(t, reloaded.IsFirstOrderDone != nil && *reloaded.IsFirstOrderDone)
}

func TestApplyOrderToBuyer_RouterService_NoLine_IsBYOPurchase(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	plan := seedRouterPlan(t, "router-apply-byo", "", 29900)
	order := seedPaidRouterOrder(t, user, plan, "")

	var ids []uint64
	require.NoError(t, applyOrderToBuyer(context.Background(), db.Get(), order, &ids))
	require.Len(t, ids, 1)
	var f RouterFulfillment
	require.NoError(t, db.Get().Where("order_id = ?", order.ID).First(&f).Error)
	assert.True(t, f.IsBYO())
	assert.Equal(t, RouterStagePaid, f.Stage)
}

func TestApplyOrderToBuyer_RouterService_WithLine_ExtendsExpiry(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	plan := seedRouterPlan(t, "router-apply-renew", "", 29900)
	now := time.Now().Unix()
	// 现有线路：宽限期中（已过期 3 天）
	existing := &PrivateNodeSubscription{UserID: user.ID, PlanID: plan.ID, OrderID: 0,
		Region: "japan", IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40,
		Status: PNStatusGrace, PurchasedAt: now - 400*86400, ExpiresAt: now - 3*86400, GraceUntil: now + 4*86400}
	require.NoError(t, db.Get().Create(existing).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(existing) })
	order := seedPaidRouterOrder(t, user, plan, "")

	var ids []uint64
	require.NoError(t, applyOrderToBuyer(context.Background(), db.Get(), order, &ids))
	assert.Empty(t, ids, "renewal must not provision a new line")

	var subs []PrivateNodeSubscription
	require.NoError(t, db.Get().Where("user_id = ?", user.ID).Find(&subs).Error)
	require.Len(t, subs, 1, "renewal must not create a second line")
	got := subs[0]
	// 已过期：从 now 起算 12 个月
	wantMin := time.Unix(now, 0).AddDate(0, 12, 0).Unix() - 5
	assert.GreaterOrEqual(t, got.ExpiresAt, wantMin)
	assert.Equal(t, PNStatusActive, got.Status)
	assert.EqualValues(t, 0, got.GraceUntil)

	var cnt int64
	db.Get().Model(&RouterFulfillment{}).Where("order_id = ?", order.ID).Count(&cnt)
	assert.EqualValues(t, 0, cnt, "renewal creates no fulfillment row")
}

func TestExtendPrivateLine_FutureExpiryStacks(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	now := time.Now().Unix()
	future := now + 100*86400
	sub := &PrivateNodeSubscription{UserID: user.ID, PlanID: 1, OrderID: 0, Region: "japan",
		IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40, Status: PNStatusActive,
		PurchasedAt: now, ExpiresAt: future}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub) })

	require.NoError(t, extendPrivateLine(context.Background(), db.Get(), sub, 12, now))
	var got PrivateNodeSubscription
	require.NoError(t, db.Get().First(&got, sub.ID).Error)
	assert.Equal(t, time.Unix(future, 0).AddDate(0, 12, 0).Unix(), got.ExpiresAt, "unexpired line stacks from its current expiry")
}
