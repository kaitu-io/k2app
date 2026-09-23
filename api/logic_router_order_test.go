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

	var pc OrderPostCommit
	require.NoError(t, applyOrderToBuyer(ctx, db.Get(), order, &pc))

	var sub PrivateNodeSubscription
	require.NoError(t, db.Get().Where("order_id = ?", order.ID).First(&sub).Error)
	assert.Equal(t, PNStatusPending, sub.Status)
	assert.Equal(t, "japan", sub.Region)
	assert.Equal(t, []uint64{sub.ID}, pc.ProvisionSubIDs, "must be queued for provisioning post-commit")
	assert.Empty(t, pc.RenewedRouterSubIDs, "new purchase must not be treated as a renewal")

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

	var pc OrderPostCommit
	require.NoError(t, applyOrderToBuyer(context.Background(), db.Get(), order, &pc))
	require.Len(t, pc.ProvisionSubIDs, 1)
	var f RouterFulfillment
	require.NoError(t, db.Get().Where("order_id = ?", order.ID).First(&f).Error)
	assert.True(t, f.IsBYO())
	assert.Equal(t, RouterStagePaid, f.Stage)
	assert.Equal(t, f.SubID, pc.ProvisionSubIDs[0], "queued sub id must be the one backing this fulfillment")
}

func TestApplyOrderToBuyer_RouterService_WithLine_ExtendsExpiry(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	plan := seedRouterPlan(t, "router-apply-renew", "", 29900)
	now := time.Now().Unix()
	// 现有线路：宽限期中（已过期 3 天）。必须连带台账 —— 台账是"这条线路属于路由器版"的唯一判据
	// （routerLineIDs）；只建线路等于造了一条定制线路，续费不会也不该认它。
	existing := &PrivateNodeSubscription{UserID: user.ID, PlanID: plan.ID, OrderID: 730000 + user.ID,
		Region: "japan", IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40,
		Status: PNStatusGrace, PurchasedAt: now - 400*86400, ExpiresAt: now - 3*86400, GraceUntil: now + 4*86400}
	require.NoError(t, db.Get().Create(existing).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(existing) })
	existingFul := &RouterFulfillment{OrderID: existing.OrderID, UserID: user.ID, SubID: existing.ID,
		HardwareSKU: "redmi-ax6s", Stage: RouterStageShipped, ShippedAt: now - 390*86400}
	require.NoError(t, db.Get().Create(existingFul).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(existingFul) })
	order := seedPaidRouterOrder(t, user, plan, "")

	var pc OrderPostCommit
	require.NoError(t, applyOrderToBuyer(context.Background(), db.Get(), order, &pc))
	assert.Empty(t, pc.ProvisionSubIDs, "renewal must not provision a new line")
	assert.Equal(t, []uint64{existing.ID}, pc.RenewedRouterSubIDs, "renewal must be collected for post-commit notify")

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

// 续费分支必须在同一事务里同步挂在该线路上的台账：expired + grace 线路 → 续费后台账不再是 expired。
func TestApplyOrderToBuyer_RouterRenewal_SyncsFulfillment(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	plan := seedRouterPlan(t, "router-apply-renew-sync", "", 29900)
	now := time.Now().Unix()
	existing := &PrivateNodeSubscription{UserID: user.ID, PlanID: plan.ID, OrderID: 0,
		Region: "japan", IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40,
		Status: PNStatusGrace, PurchasedAt: now - 400*86400, ExpiresAt: now - 3*86400, GraceUntil: now + 4*86400}
	require.NoError(t, db.Get().Create(existing).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(existing) })
	f := &RouterFulfillment{OrderID: 720000 + user.ID, UserID: user.ID, SubID: existing.ID,
		HardwareSKU: "redmi-ax6s", Stage: RouterStageExpired, ShippedAt: now - 300*86400}
	require.NoError(t, db.Get().Create(f).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(f) })
	order := seedPaidRouterOrder(t, user, plan, "")

	var pc OrderPostCommit
	require.NoError(t, applyOrderToBuyer(context.Background(), db.Get(), order, &pc))
	require.Equal(t, []uint64{existing.ID}, pc.RenewedRouterSubIDs)

	var got RouterFulfillment
	require.NoError(t, db.Get().First(&got, f.ID).Error)
	assert.Equal(t, RouterStageShipped, got.Stage, "renewal must re-sync the ledger (shipped hardware returns to shipped)")
}

// 线路已 deprovisioned 的成品客户买服务套餐：走新建线路，新台账必须继承旧硬件台账（SKU/发货/凭证），
// 线路激活后直接 shipped，设备有晚于 max(铸造, 发货) 的活动则 online。
func TestApplyOrderToBuyer_RouterService_InheritsHardwareFulfillment(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	user := CreateTestUser(t)
	plan := seedRouterPlan(t, "router-apply-inherit", "", 29900)
	now := time.Now().Unix()

	oldSub := &PrivateNodeSubscription{UserID: user.ID, PlanID: plan.ID, OrderID: 730000 + user.ID,
		Region: "japan", IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40,
		Status: PNStatusDeprovisioned, PurchasedAt: now - 500*86400, ExpiresAt: now - 30*86400}
	require.NoError(t, db.Get().Create(oldSub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(oldSub) })
	dev := &Device{UDID: newRouterUDID(), UserID: user.ID, IsGateway: true, AppPlatform: "router",
		TokenIssueAt: now - 499*86400, TokenLastUsedAt: now - 40*86400, TunnelIssueAt: now - 499*86400}
	require.NoError(t, db.Get().Create(dev).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(dev) })
	mintAt, shippedAt := now-499*86400, now-495*86400
	old := &RouterFulfillment{OrderID: oldSub.OrderID, UserID: user.ID, SubID: oldSub.ID,
		HardwareSKU: "redmi-ax6s", Stage: RouterStageExpired, ShippedAt: shippedAt, TrackingNo: "SF-OLD",
		GatewayDeviceID: &dev.ID, CredentialMintedAt: mintAt}
	require.NoError(t, db.Get().Create(old).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(old) })

	order := seedPaidRouterOrder(t, user, plan, "")
	var pc OrderPostCommit
	require.NoError(t, applyOrderToBuyer(ctx, db.Get(), order, &pc))
	require.Len(t, pc.ProvisionSubIDs, 1, "deprovisioned line is not renewable: a new line is provisioned")

	var f RouterFulfillment
	require.NoError(t, db.Get().Where("order_id = ?", order.ID).First(&f).Error)
	assert.Equal(t, "redmi-ax6s", f.HardwareSKU)
	assert.Equal(t, shippedAt, f.ShippedAt)
	require.NotNil(t, f.GatewayDeviceID)
	assert.Equal(t, dev.ID, *f.GatewayDeviceID)
	assert.Equal(t, mintAt, f.CredentialMintedAt)
	assert.Equal(t, RouterStagePaid, f.Stage)
	assert.Equal(t, pc.ProvisionSubIDs[0], f.SubID)

	// 线路开通（active）。先让设备活动早于发货时刻 → 停在 shipped；再让活动晚于门槛 → online。
	require.NoError(t, db.Get().Model(&PrivateNodeSubscription{}).Where("id = ?", f.SubID).Update("status", PNStatusActive).Error)
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", dev.ID).Update("token_last_used_at", shippedAt-1).Error)
	require.NoError(t, syncRouterFulfillment(ctx, db.Get(), &f, now))
	assert.Equal(t, RouterStageShipped, f.Stage, "hardware already with the customer: skip ready, no manual re-ship")

	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", dev.ID).Update("token_last_used_at", now-60).Error)
	require.NoError(t, syncRouterFulfillment(ctx, db.Get(), &f, now))
	assert.Equal(t, RouterStageOnline, f.Stage)
	var persisted RouterFulfillment
	require.NoError(t, db.Get().First(&persisted, f.ID).Error)
	assert.Equal(t, RouterStageOnline, persisted.Stage)
}

// 纯自备历史（无硬件台账）的服务套餐新建：新台账保持自备形态，不继承任何字段。
func TestApplyOrderToBuyer_RouterService_BYOHistoryDoesNotInherit(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	plan := seedRouterPlan(t, "router-apply-byo-hist", "", 29900)
	now := time.Now().Unix()
	oldSub := &PrivateNodeSubscription{UserID: user.ID, PlanID: plan.ID, OrderID: 740000 + user.ID,
		Region: "japan", IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40,
		Status: PNStatusDeprovisioned, PurchasedAt: now - 500*86400, ExpiresAt: now - 30*86400}
	require.NoError(t, db.Get().Create(oldSub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(oldSub) })
	devID := uint64(515151)
	old := &RouterFulfillment{OrderID: oldSub.OrderID, UserID: user.ID, SubID: oldSub.ID,
		Stage: RouterStageExpired, GatewayDeviceID: &devID, CredentialMintedAt: now - 400*86400}
	require.NoError(t, db.Get().Create(old).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(old) })

	order := seedPaidRouterOrder(t, user, plan, "")
	var pc OrderPostCommit
	require.NoError(t, applyOrderToBuyer(context.Background(), db.Get(), order, &pc))
	var f RouterFulfillment
	require.NoError(t, db.Get().Where("order_id = ?", order.ID).First(&f).Error)
	assert.True(t, f.IsBYO())
	assert.Nil(t, f.GatewayDeviceID)
	assert.Zero(t, f.CredentialMintedAt)
	assert.Zero(t, f.ShippedAt)
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

// TestOnRouterLineRenewed_BestEffort 覆盖 post-commit 续费通知：加载已存在的 sub 时不 panic
// （正常发 Slack），加载不存在的 sub 时同样不 panic（只 log 后返回）——它是 best-effort 副作用，
// 绝不能向上抛错误或让调用方（webhook post-commit 循环）中断。
func TestOnRouterLineRenewed_BestEffort(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	sub := seedTestPrivateSub(t)

	assert.NotPanics(t, func() { onRouterLineRenewed(ctx, sub.ID) }, "must notify without panicking for an existing sub")
	assert.NotPanics(t, func() { onRouterLineRenewed(ctx, sub.ID+999999) }, "must log-and-return without panicking when the sub is missing")
}
