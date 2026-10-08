package center

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// Apple 退款收回（spec 2026-10-08-apple-refund-design.md §7）。fixture 的买家经分销商邀请码注册，
// 年付首购会触发被邀请首购奖励（测试配置 30+30 天）；不需要邀请的用例先 noInvite()。

const aDay = int64(86400)

type billingAlerts struct {
	mu   sync.Mutex
	msgs []string
}

func (b *billingAlerts) all() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.msgs...)
}

func (b *billingAlerts) has(tag string) bool {
	for _, m := range b.all() {
		if len(m) >= len(tag) && m[:len(tag)] == tag {
			return true
		}
	}
	return false
}

func captureBillingAlerts(t *testing.T) *billingAlerts {
	t.Helper()
	b := &billingAlerts{}
	old := alertBilling
	alertBilling = func(_ context.Context, msg string) {
		b.mu.Lock()
		b.msgs = append(b.msgs, msg)
		b.mu.Unlock()
	}
	t.Cleanup(func() { alertBilling = old })
	return b
}

func (f *iapOrderFixture) noInvite(t *testing.T) {
	t.Helper()
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.buyer.ID).Update("invited_by_code_id", 0).Error)
}

func (f *iapOrderFixture) userNow(t *testing.T, id uint64) User {
	t.Helper()
	var u User
	require.NoError(t, db.Get().First(&u, id).Error)
	return u
}

func (f *iapOrderFixture) subNow(t *testing.T) Subscription {
	t.Helper()
	var s Subscription
	require.NoError(t, db.Get().Where(&Subscription{Provider: "apple", ProviderSubscriptionID: f.origTxn}).First(&s).Error)
	return s
}

func (f *iapOrderFixture) addGift(t *testing.T, seconds int64) {
	t.Helper()
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.buyer.ID).
		Update("expired_at", gorm.Expr("GREATEST(expired_at, ?) + ?", time.Now().Unix(), seconds)).Error)
}

func (f *iapOrderFixture) txn(id string, p, e int64) *appstore.TransactionInfo {
	return &appstore.TransactionInfo{
		OriginalTransactionId: f.origTxn, TransactionId: id, ProductId: f.productID, AppAccountToken: f.token,
		InAppOwnershipType: appstore.OwnershipType_PURCHASED, Environment: f.env,
		PurchaseDate: p * 1000, ExpiresDate: e * 1000, RevocationDate: time.Now().UnixMilli(),
	}
}

func (f *iapOrderFixture) refundEv(t *testing.T, id string, p, e int64, ev appleRefundEvidence) *appleRefundOutcome {
	t.Helper()
	if ev.Source == "" {
		ev.Source = "webhook"
	}
	out, err := applyAppleRefund(context.Background(), f.origTxn, f.txn(id, p, e), ev)
	require.NoError(t, err)
	return out
}

func (f *iapOrderFixture) reverse(t *testing.T, id string, p, e, signedAt int64) {
	t.Helper()
	tx := f.txn(id, p, e)
	tx.RevocationDate = 0
	require.NoError(t, reverseAppleRefund(context.Background(), f.origTxn, tx, signedAt, "webhook"))
}

func (f *iapOrderFixture) refundRow(t *testing.T, id string) AppleRefund {
	t.Helper()
	var r AppleRefund
	require.NoError(t, db.Get().Where("transaction_id = ?", id).First(&r).Error)
	return r
}

func assertNear(t *testing.T, want, got, tol int64, msg string) {
	t.Helper()
	assert.InDelta(t, float64(want), float64(got), float64(tol), msg)
}

// 1. 赠送在购买前叠加（被邀请首购：奖励先于购买入账，另有 100 天赠送）→ 收回整期剩余付费段、
// 撤回被邀请奖励，原有赠送保留；邀请人奖励撤回；返现撤回；分销计数扣回。
func TestAppleRefund_GiftBeforePurchase(t *testing.T) {
	skipIfNoDB(t)
	alerts := captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	cfg := configInvite(context.Background())
	require.Greater(t, cfg.PurchaseRewardDays, 0)
	t0 := time.Now().Unix()
	f.addGift(t, 100*aDay)

	require.NoError(t, f.credit(t, "AR-GB1", t0, t0+365*aDay))
	reward := int64(cfg.PurchaseRewardDays) * aDay
	assertNear(t, t0+100*aDay+reward+365*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 120, "前置：赠送+奖励+整期")
	assertNear(t, t0+365*aDay, f.subNow(t).PaidThrough, 120, "PaidThrough 只记付费段")
	var cfgBefore RetailerConfig
	require.NoError(t, db.Get().First(&cfgBefore, f.config.ID).Error)
	require.Equal(t, 1, cfgBefore.PaidUserCount, "前置：首单计入分销人数")
	require.Equal(t, f.config.ID, f.orders(t)[0].RetailerCountedID)

	out := f.refundEv(t, "AR-GB1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	require.True(t, out.Adopted)
	assertNear(t, 365*aDay, out.Cut, 120, "收回整期剩余付费段")
	assert.True(t, out.MarkedRevoked)
	require.NotNil(t, out.Followup)
	assert.True(t, out.Followup.InviteReversed)

	assertNear(t, t0+100*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 180, "原有 100 天赠送保留，被邀请奖励撤回")
	assert.LessOrEqual(t, f.userNow(t, f.retailer.ID).ExpiredAt, time.Now().Unix()+120, "邀请人奖励撤回")
	sub := f.subNow(t)
	assert.Equal(t, "revoked", sub.Status)
	assert.Equal(t, t0+365*aDay, sub.CurrentPeriodEnd, "退款不改周期末")
	assert.LessOrEqual(t, sub.PaidThrough, time.Now().Unix()+120)
	assert.Equal(t, int64(0), f.retailerBalance(t), "返现撤回")
	var cfgAfter RetailerConfig
	require.NoError(t, db.Get().First(&cfgAfter, f.config.ID).Error)
	assert.Equal(t, 0, cfgAfter.PaidUserCount, "分销计数扣回")
	assert.Zero(t, f.orders(t)[0].RetailerCountedID)
	r := f.refundRow(t, "AR-GB1")
	assert.True(t, r.Active)
	assert.True(t, r.Credited)
	var g InviteRewardGrant
	require.NoError(t, db.Get().Where("invitee_user_id = ?", f.buyer.ID).First(&g).Error)
	assert.True(t, g.Reversed)
	assert.True(t, alerts.has("[APPLE-REFUND]"))
}

// 1b. 赠送在购买后叠加 → 赠送保留。
func TestAppleRefund_GiftAfterPurchase(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-GA1", t0, t0+365*aDay))
	f.addGift(t, 50*aDay)

	f.refundEv(t, "AR-GA1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	assertNear(t, t0+50*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 120, "购买后叠加的赠送保留")
}

// 2. 入账迟到（购买 10 天后才入账，付费段整体后移）→ 按 PaidThrough 收，不按 tExp 少收 10 天。
func TestAppleRefund_LateCreditCutsWholePaidSegment(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-LC1", t0-10*aDay, t0+355*aDay))
	assertNear(t, t0+365*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 120, "前置：首购从 now 起整期")

	out := f.refundEv(t, "AR-LC1", t0-10*aDay, t0+355*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	assertNear(t, 365*aDay, out.Cut, 120, "整段付费时长都收回")
	assert.LessOrEqual(t, f.userNow(t, f.buyer.ID).ExpiredAt, time.Now().Unix()+120)
}

// 3. 退已过期的旧期（有更新一期在跑）→ 不收时长、不改状态，订单与返现照撤。
func TestAppleRefund_ConsumedOldPeriod(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-OP1", t0-400*aDay, t0-35*aDay))
	require.NoError(t, f.credit(t, "AR-OP2", t0-35*aDay, t0+330*aDay))
	before := f.userNow(t, f.buyer.ID).ExpiredAt

	out := f.refundEv(t, "AR-OP1", t0-400*aDay, t0-35*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	assert.True(t, out.Adopted)
	assert.Zero(t, out.Cut)
	assert.False(t, out.MarkedRevoked)
	assert.Equal(t, before, f.userNow(t, f.buyer.ID).ExpiredAt)
	assert.Equal(t, "active", f.subNow(t).Status)
	orders := f.orders(t)
	require.Len(t, orders, 2)
	require.NotNil(t, orders[0].IsRefunded)
	assert.True(t, *orders[0].IsRefunded, "旧期订单照撤")
}

// 4. 新一期已入账后退上一期（Apple 最多提前 24h 扣续费 / 退款迟到处理）→ 只收上一期日历剩余，
// 新一期完整、不置 revoked。本例账本是"首期迟到入账"形态（首期从 now 起整期）；账本与 Apple 对齐的
// 形态见 TestAppleRefund_NewerPeriodAlignedLedger。
func TestAppleRefund_NewerPeriodAlreadyCredited(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-NP1", t0-364*aDay, t0+aDay))
	require.NoError(t, f.credit(t, "AR-NP2", t0+aDay, t0+366*aDay))
	beforeEA, beforePT := f.userNow(t, f.buyer.ID).ExpiredAt, f.subNow(t).PaidThrough

	out := f.refundEv(t, "AR-NP1", t0-364*aDay, t0+aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	assertNear(t, aDay, out.Cut, 120, "只收被退期剩下的约 1 天")
	assert.False(t, out.MarkedRevoked)
	assertNear(t, beforeEA-aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 120, "新一期整年保留")
	assertNear(t, beforePT-aDay, f.subNow(t).PaidThrough, 120, "")
	assert.Equal(t, "active", f.subNow(t).Status)
}

// 5. 未入账交易 / 家庭共享撤销 → 不收、不改状态。
func TestAppleRefund_UncreditedAndFamilyRevoke(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-FU1", t0, t0+365*aDay))
	before := f.userNow(t, f.buyer.ID).ExpiredAt

	out := f.refundEv(t, "AR-FU-NEVER", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	assert.Zero(t, out.Cut)
	assert.False(t, f.refundRow(t, "AR-FU-NEVER").Credited)

	out = f.refundEv(t, "AR-FU1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli(), RevocationType: "FAMILY_REVOKE"})
	assert.Zero(t, out.Cut)
	assert.False(t, out.MarkedRevoked)
	assert.Equal(t, before, f.userNow(t, f.buyer.ID).ExpiredAt)
	assert.Equal(t, "active", f.subNow(t).Status)
}

// 8. REFUND 与 REVOKE 并发 / 重投 → 只处理一次。
func TestAppleRefund_ConcurrentOnce(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-CC1", t0, t0+365*aDay))

	signed := time.Now().UnixMilli()
	var wg sync.WaitGroup
	results := make([]*appleRefundOutcome, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := applyAppleRefund(context.Background(), f.origTxn, f.txn("AR-CC1", t0, t0+365*aDay),
				appleRefundEvidence{SignedAt: signed + int64(i), Source: "webhook"})
			assert.NoError(t, err)
			results[i] = out
		}(i)
	}
	wg.Wait()
	adopted := 0
	for _, r := range results {
		if r != nil && r.Adopted {
			adopted++
		}
	}
	assert.Equal(t, 1, adopted)
	assert.LessOrEqual(t, f.userNow(t, f.buyer.ID).ExpiredAt, time.Now().Unix()+120)
	assert.Equal(t, int64(0), f.retailerBalance(t), "返现只撤一次，余额不为负")
	var n int64
	db.Get().Model(&AppleRefund{}).Where("transaction_id = ?", "AR-CC1").Count(&n)
	assert.Equal(t, int64(1), n)
	assert.GreaterOrEqual(t, f.refundRow(t, "AR-CC1").RefundSignedAt, signed, "重投只记下最新证据")
}

// 9a. 撤销先到（占位行）→ 证据更早的退款判为迟到；证据更晚的真实退款按首次退款公式收回。
func TestAppleRefund_ReversalArrivesFirst(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-RF1", t0, t0+365*aDay))
	before := f.userNow(t, f.buyer.ID).ExpiredAt
	s := time.Now().UnixMilli()

	f.reverse(t, "AR-RF1", t0, t0+365*aDay, s)
	out := f.refundEv(t, "AR-RF1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s - 1000})
	assert.False(t, out.Adopted, "早于撤销的退款是迟到的旧证据")
	assert.Equal(t, before, f.userNow(t, f.buyer.ID).ExpiredAt)

	out = f.refundEv(t, "AR-RF1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s})
	assert.False(t, out.Adopted, "同毫秒不采纳")

	out = f.refundEv(t, "AR-RF1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s + 1000})
	assert.True(t, out.Adopted)
	assert.False(t, out.ReRefund, "占位行没收过也没还过，走首次退款公式")
	assertNear(t, 365*aDay, out.Cut, 120, "")
}

// 16/17. 退款 → 撤销退款（全额还、邀请奖励还、首单标记 true、状态复活）→ 再次退款（封顶、邀请再撤、首单回 false）。
func TestAppleRefund_RefundReverseRefund(t *testing.T) {
	skipIfNoDB(t)
	alerts := captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	cfg := configInvite(context.Background())
	reward := int64(cfg.PurchaseRewardDays) * aDay
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-RR1", t0, t0+365*aDay))
	s := time.Now().UnixMilli()

	f.refundEv(t, "AR-RR1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s})
	require.LessOrEqual(t, f.userNow(t, f.buyer.ID).ExpiredAt, time.Now().Unix()+120)
	require.False(t, *f.userNow(t, f.buyer.ID).IsFirstOrderDone)

	f.reverse(t, "AR-RR1", t0, t0+365*aDay, s+1000)
	u := f.userNow(t, f.buyer.ID)
	assertNear(t, time.Now().Unix()+365*aDay+reward, u.ExpiredAt, 180, "付费时长与被邀请奖励都还回")
	assert.True(t, *u.IsFirstOrderDone)
	assert.Equal(t, "active", f.subNow(t).Status, "经 deriveActiveOrExpired 离开 revoked")
	assertNear(t, time.Now().Unix()+365*aDay, f.subNow(t).PaidThrough, 180, "")
	var g InviteRewardGrant
	require.NoError(t, db.Get().Where("invitee_user_id = ?", f.buyer.ID).First(&g).Error)
	assert.False(t, g.Reversed)
	assertNear(t, time.Now().Unix()+int64(cfg.InviterPurchaseRewardDays)*aDay, f.userNow(t, f.retailer.ID).ExpiredAt, 180, "邀请人奖励还回")
	r := f.refundRow(t, "AR-RR1")
	assert.False(t, r.Active)
	assert.NotZero(t, r.RestoredAt)
	assert.True(t, alerts.has("[APPLE-REFUND-REVERSED]"))

	// 重复投递撤销：幂等
	eaBefore := f.userNow(t, f.buyer.ID).ExpiredAt
	f.reverse(t, "AR-RR1", t0, t0+365*aDay, s+1000)
	assert.Equal(t, eaBefore, f.userNow(t, f.buyer.ID).ExpiredAt)

	// Apple 再次退款
	out := f.refundEv(t, "AR-RR1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s + 2000})
	assert.True(t, out.ReRefund)
	assertNear(t, 365*aDay, out.Cut, 180, "封顶 = 还回去且未用掉的")
	assert.True(t, out.Followup.InviteReversed, "邀请奖励再撤一次")
	u = f.userNow(t, f.buyer.ID)
	assert.LessOrEqual(t, u.ExpiredAt, time.Now().Unix()+180)
	assert.False(t, *u.IsFirstOrderDone, "已退款短路下也翻回 false")
	assert.Equal(t, "revoked", f.subNow(t).Status)
}

// 17. 再次退款封顶按实际恢复时刻（RestoredAt）计：恢复后已用 20 天，再退只收回剩下的。
func TestAppleRefund_ReRefundCapUsesRestoredAt(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-CAP1", t0, t0+365*aDay))
	s := time.Now().UnixMilli()
	f.refundEv(t, "AR-CAP1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s})
	f.reverse(t, "AR-CAP1", t0, t0+365*aDay, s+1000)
	require.NoError(t, db.Get().Model(&AppleRefund{}).Where("transaction_id = ?", "AR-CAP1").
		Update("restored_at", time.Now().Unix()-20*aDay).Error)

	out := f.refundEv(t, "AR-CAP1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s + 2000})
	assert.True(t, out.ReRefund)
	assertNear(t, 345*aDay, out.Cut, 180, "365 天还回后已用 20 天")
	assertNear(t, time.Now().Unix()+20*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 180, "")
}

// 17b. 再次退款不受 tExp 门限制：被退期早已过期，撤销时还回的时长仍能再收回。
func TestAppleRefund_ReRefundAfterPeriodEnd(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-PE1", t0, t0+365*aDay))
	s := time.Now().UnixMilli()
	f.refundEv(t, "AR-PE1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s})
	f.reverse(t, "AR-PE1", t0, t0+365*aDay, s+1000)
	// 再次退款时把交易的周期挪到过去（等价于 tExp 之后才撤销、之后再退）
	out := f.refundEv(t, "AR-PE1", t0-400*aDay, t0-35*aDay, appleRefundEvidence{SignedAt: s + 2000})
	assert.True(t, out.ReRefund)
	assertNear(t, 365*aDay, out.Cut, 180, "还回的时长被收回")
}

// 11. 撤销退款时订阅已因重订阅复活 → 不动状态，两笔付款的时长都在。
func TestAppleRefund_ReversalAfterRevival(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-RV1", t0, t0+365*aDay))
	s := time.Now().UnixMilli()
	f.refundEv(t, "AR-RV1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s})
	require.NoError(t, f.credit(t, "AR-RV2", t0+30*aDay, t0+395*aDay))
	require.Equal(t, "active", f.subNow(t).Status)
	ea := f.userNow(t, f.buyer.ID).ExpiredAt

	f.reverse(t, "AR-RV1", t0, t0+365*aDay, s+1000)
	assert.Equal(t, "active", f.subNow(t).Status)
	assertNear(t, ea+365*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 180, "两笔付款的时长都保留")
	assert.Equal(t, t0+395*aDay, f.subNow(t).CurrentPeriodEnd)
}

// 12. 退款 → 隔 30 天同链重订阅（账户有 100 天赠送）→ 按首购入账整期、赠送保留、状态复活；
// 被退交易重放不复活。
func TestAppleRefund_RevivalCreditsWholePeriod(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-RS1", t0, t0+365*aDay))
	f.refundEv(t, "AR-RS1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})

	// 被退交易重放（verify / 对账）：已入账 → 只刷新 plan-state，停在 revoked
	require.NoError(t, f.credit(t, "AR-RS1", t0, t0+365*aDay))
	require.Equal(t, "revoked", f.subNow(t).Status)

	f.addGift(t, 100*aDay)
	require.NoError(t, f.credit(t, "AR-RS2", t0+30*aDay, t0+395*aDay))
	var credit SubscriptionCredit
	require.NoError(t, db.Get().Where(&SubscriptionCredit{TransactionID: "AR-RS2"}).First(&credit).Error)
	assertNear(t, 365*aDay, credit.CreditedSeconds, 120, "按首购口径入一整期，不是 395 天")
	assert.Equal(t, "purchase", credit.Kind)
	assertNear(t, t0+100*aDay+365*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 180, "叠在赠送之上")
	sub := f.subNow(t)
	assert.Equal(t, "active", sub.Status)
	assert.Equal(t, t0+395*aDay, sub.CurrentPeriodEnd)
	assert.True(t, sub.AutoRenew)
}

// 13. 退款后 Apple 立即自动续订（purchaseDate = 旧 tExp）→ 复活并入整期。
func TestAppleRefund_AutoRenewAfterRefundRevives(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-AR1", t0, t0+365*aDay))
	f.refundEv(t, "AR-AR1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	require.NoError(t, f.credit(t, "AR-AR2", t0+365*aDay, t0+730*aDay))
	assert.Equal(t, "active", f.subNow(t).Status)
	var credit SubscriptionCredit
	require.NoError(t, db.Get().Where(&SubscriptionCredit{TransactionID: "AR-AR2"}).First(&credit).Error)
	assertNear(t, 365*aDay, credit.CreditedSeconds, 120, "")
}

// 14. 退年付 → 同链改订月付 → 两次月续订，有赠送：每次都入整月，赠送不被吞。
func TestAppleRefund_CrossgradeAfterRefund(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-XG1", t0, t0+365*aDay))
	f.refundEv(t, "AR-XG1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	f.addGift(t, 100*aDay)

	require.NoError(t, f.credit(t, "AR-XG2", t0+20*aDay, t0+50*aDay))
	require.NoError(t, f.credit(t, "AR-XG3", t0+50*aDay, t0+80*aDay))
	require.NoError(t, f.credit(t, "AR-XG4", t0+80*aDay, t0+110*aDay))
	for _, id := range []string{"AR-XG2", "AR-XG3", "AR-XG4"} {
		var c SubscriptionCredit
		require.NoError(t, db.Get().Where(&SubscriptionCredit{TransactionID: id}).First(&c).Error)
		assertNear(t, 30*aDay, c.CreditedSeconds, 120, id+" 入整月")
	}
	assertNear(t, t0+100*aDay+90*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 180, "赠送 + 三个月")
}

// 7. 被退交易从未入账 → 不入账、不建订阅行；webhook 路径视为已处理。
func TestCreditAppleTransaction_RefundedTxnNeverCredited(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()
	before := f.userNow(t, f.buyer.ID).ExpiredAt
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		return creditAppleTransaction(context.Background(), tx, f.buyer.ID, f.txn("AR-NC1", t0, t0+365*aDay))
	})
	require.ErrorIs(t, err, errAppleTxnRevoked)
	var n int64
	db.Get().Model(&Subscription{}).Where("provider_subscription_id = ?", f.origTxn).Count(&n)
	assert.Zero(t, n, "不建订阅行")
	assert.Equal(t, before, f.userNow(t, f.buyer.ID).ExpiredAt)
}

// 10. 终态守卫：revoked 不被 EXPIRED / 宽限事件覆盖，宽限期不再延长权益。
func TestAppleRefund_TerminalGuards(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-TG1", t0, t0+365*aDay))
	f.refundEv(t, "AR-TG1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	sub := f.subNow(t)
	stale := sub
	stale.Status = "active" // 事务外读到的旧快照

	require.NoError(t, setSubStatus(context.Background(), sub.ID, "expired"))
	assert.Equal(t, "revoked", f.subNow(t).Status)

	ea := f.userNow(t, f.buyer.ID).ExpiredAt
	require.NoError(t, applyRenewalInfo(context.Background(), &stale, &appstore.RenewalInfo{
		AutoRenewStatus: appstore.AutoRenewStatus_On, GracePeriodExpiresDate: (time.Now().Unix() + 16*aDay) * 1000,
	}, ""))
	assert.Equal(t, "revoked", f.subNow(t).Status)
	assert.Equal(t, ea, f.userNow(t, f.buyer.ID).ExpiredAt, "不做宽限延长")
}

// 27. 存量已退到钱包的 IAP 单再遇 Apple 退款 → 不再扣时长、不置 revoked；邀请照撤。
func TestAppleRefund_LegacyWalletRefundedOrder(t *testing.T) {
	skipIfNoDB(t)
	alerts := captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-WR1", t0, t0+365*aDay))
	o := f.orders(t)[0]
	wallet, err := getOrCreateWalletInTx(context.Background(), db.Get(), f.buyer.ID)
	require.NoError(t, err)
	require.NoError(t, db.Get().Create(&WalletChange{WalletID: wallet.ID, Type: WalletChangeTypeOrderRefund,
		Amount: int64(o.PayAmount), OrderID: &o.ID}).Error)
	require.NoError(t, db.Get().Model(&Order{}).Where("id = ?", o.ID).Update("is_refunded", true).Error)
	before := f.userNow(t, f.buyer.ID).ExpiredAt

	out := f.refundEv(t, "AR-WR1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	assert.Zero(t, out.Cut, "后台退款已扣过权益")
	assert.False(t, out.MarkedRevoked)
	assert.True(t, out.Followup.InviteReversed)
	assert.True(t, alerts.has("[DOUBLE-REFUND]"), "双退告警在提交后经 alertBilling 发出")
	cfg := configInvite(context.Background())
	assertNear(t, before-int64(cfg.PurchaseRewardDays)*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 120, "只撤被邀请奖励")
}

// 2b. 两期都迟到入账（付费段 = 两期），退最新一期 → 只收一期（periodLen 上界）。
func TestAppleRefund_TwoLatePeriodsCutsOnlyOne(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-2L1", t0-400*aDay, t0-35*aDay))
	require.NoError(t, f.credit(t, "AR-2L2", t0-35*aDay, t0+330*aDay))
	before := f.userNow(t, f.buyer.ID).ExpiredAt
	out := f.refundEv(t, "AR-2L2", t0-35*aDay, t0+330*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	assertNear(t, 365*aDay, out.Cut, 120, "只收被退的这一期")
	assertNear(t, before-365*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 120, "")
}

// 3b. 订阅已过期后才退款 → 不收、不改成 revoked。
func TestAppleRefund_ExpiredSubscriptionNotRevoked(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-EX1", t0-400*aDay, t0-35*aDay))
	require.NoError(t, db.Get().Model(&Subscription{}).Where("provider_subscription_id = ?", f.origTxn).Update("status", "expired").Error)
	out := f.refundEv(t, "AR-EX1", t0-400*aDay, t0-35*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	assert.Zero(t, out.Cut)
	assert.False(t, out.MarkedRevoked)
	assert.Equal(t, "expired", f.subNow(t).Status)
}

// 9b. 迟到的旧撤销（证据早于当前退款）→ 不恢复。
func TestAppleRefund_StaleReversalIgnored(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-SR1", t0, t0+365*aDay))
	s := time.Now().UnixMilli()
	f.refundEv(t, "AR-SR1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s})
	ea := f.userNow(t, f.buyer.ID).ExpiredAt
	f.reverse(t, "AR-SR1", t0, t0+365*aDay, s-1000)
	assert.True(t, f.refundRow(t, "AR-SR1").Active)
	assert.Equal(t, ea, f.userNow(t, f.buyer.ID).ExpiredAt)
}

// 9c. 已在退款状态时，对账的 now 兜底证据不推后 RefundSignedAt（否则撤销探测冷却永不到期）；
// Apple 给出的更晚证据才推后。
func TestAppleRefund_NowFallbackDoesNotBumpEvidence(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-NF1", t0, t0+365*aDay))
	s := time.Now().UnixMilli() - 3*aDay*1000
	f.refundEv(t, "AR-NF1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s})
	f.refundEv(t, "AR-NF1", t0, t0+365*aDay, appleRefundEvidence{Source: "reconcile"}) // 无 revocationDate
	assert.Equal(t, s, f.refundRow(t, "AR-NF1").RefundSignedAt)
	f.refundEv(t, "AR-NF1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s + 5000})
	assert.Equal(t, s+5000, f.refundRow(t, "AR-NF1").RefundSignedAt)
}

// 12b. 退款之前就发生、却晚到才入账的旧交易 → 不复活。
func TestAppleRefund_PreRefundTxnDoesNotRevive(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-PR1", t0, t0+365*aDay))
	f.refundEv(t, "AR-PR1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	require.NoError(t, f.credit(t, "AR-PR0", t0-365*aDay, t0))
	assert.Equal(t, "revoked", f.subNow(t).Status)
}

// 6b. Apple 退款比例与剩余时间比例偏差 > 10 个百分点 → 告警。
func TestAppleRefund_PercentageMismatchAlert(t *testing.T) {
	skipIfNoDB(t)
	alerts := captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-PC1", t0, t0+365*aDay))
	f.refundEv(t, "AR-PC1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli(), RevocationType: "REFUND_PRORATED", RevocationPercentage: 99900})
	assert.False(t, alerts.has("[APPLE-REFUND-PCT]"), "比例吻合不告警")
}

func TestAppleRefund_PercentageMismatchAlertFires(t *testing.T) {
	skipIfNoDB(t)
	alerts := captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-PF1", t0, t0+365*aDay))
	f.refundEv(t, "AR-PF1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli(), RevocationType: "REFUND_PRORATED", RevocationPercentage: 50000})
	assert.True(t, alerts.has("[APPLE-REFUND-PCT]"))
}


// 4b. 同上，账本与 Apple 对齐（首期按时入账，续订提前一天扣款）。
func TestAppleRefund_NewerPeriodAlignedLedger(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-NA1", t0, t0+365*aDay))
	// 一年后的视角不好造，改为把账本平移成"首期还剩 1 天"：到期与 PaidThrough 都挪到 t0+1 天
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.buyer.ID).Update("expired_at", t0+aDay).Error)
	require.NoError(t, db.Get().Model(&Subscription{}).Where("provider_subscription_id = ?", f.origTxn).
		Updates(map[string]any{"paid_through": t0 + aDay, "current_period_end": t0 + aDay}).Error)
	require.NoError(t, f.credit(t, "AR-NA2", t0+aDay, t0+366*aDay))
	assertNear(t, t0+366*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 120, "前置：续订一整年")

	out := f.refundEv(t, "AR-NA1", t0-364*aDay, t0+aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	assertNear(t, aDay, out.Cut, 120, "")
	assertNear(t, t0+365*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 120, "新一期整年保留")
	assert.Equal(t, "active", f.subNow(t).Status)
}

// 2c. 两个不同订阅同时首次退款（不同交易号落在同一索引间隙）→ 都成功、各收各的。
func TestAppleRefund_ConcurrentDifferentSubscriptions(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	t0 := time.Now().Unix()
	fs := []*iapOrderFixture{setupIAPOrderFixture(t, 30, 10), setupIAPOrderFixture(t, 30, 10), setupIAPOrderFixture(t, 30, 10)}
	for i, f := range fs {
		f.noInvite(t)
		require.NoError(t, f.credit(t, fmt.Sprintf("AR-CD%d-%d", i, t0), t0, t0+365*aDay))
	}
	var wg sync.WaitGroup
	for i, f := range fs {
		wg.Add(1)
		go func(i int, f *iapOrderFixture) {
			defer wg.Done()
			_, err := applyAppleRefund(context.Background(), f.origTxn, f.txn(fmt.Sprintf("AR-CD%d-%d", i, t0), t0, t0+365*aDay),
				appleRefundEvidence{SignedAt: time.Now().UnixMilli(), Source: "webhook"})
			assert.NoError(t, err)
		}(i, f)
	}
	wg.Wait()
	for _, f := range fs {
		assert.LessOrEqual(t, f.userNow(t, f.buyer.ID).ExpiredAt, time.Now().Unix()+120)
		assert.Equal(t, "revoked", f.subNow(t).Status)
	}
}
