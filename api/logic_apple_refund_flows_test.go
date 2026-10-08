package center

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// 退款后续（邀请 / 分销，两条退款路径共用）、对账巡检与撤销探测、webhook 端到端
// （spec 2026-10-08-apple-refund-design.md §7）。

// ---------- 邀请奖励 ----------

// webInviteFixture 网页订单的被邀请首购：订单已付、奖励已发（grant 锚在订单上）。
func webInviteFixture(t *testing.T) *inviteRewardFixture {
	t.Helper()
	f := setupInviteRewardFixture(t, 12)
	for _, id := range []uint64{f.inviter.ID, f.invitee.ID} {
		require.NoError(t, db.Get().Model(&User{}).Where("id = ?", id).
			Updates(map[string]any{"brand": string(BrandKaitu), "expired_at": time.Now().Unix()}).Error)
	}
	require.NoError(t, db.Get().Model(&Order{}).Where("id = ?", f.order.ID).
		Updates(map[string]any{"is_paid": true, "paid_at": time.Now()}).Error)
	f.runReward(t)
	t.Cleanup(func() {
		var walletIDs []uint64
		db.Get().Model(&Wallet{}).Where("user_id IN ?", []uint64{f.invitee.ID, f.inviter.ID}).Pluck("id", &walletIDs)
		if len(walletIDs) > 0 {
			db.Get().Unscoped().Where("wallet_id IN ?", walletIDs).Delete(&WalletChange{})
		}
		db.Get().Unscoped().Where("user_id IN ?", []uint64{f.invitee.ID, f.inviter.ID}).Delete(&Wallet{})
		db.Get().Unscoped().Where("user_id = ?", f.invitee.ID).Delete(&Order{})
	})
	return f
}

func (f *inviteRewardFixture) grant(t *testing.T) InviteRewardGrant {
	t.Helper()
	var g InviteRewardGrant
	require.NoError(t, db.Get().Where("invitee_user_id = ?", f.invitee.ID).First(&g).Error)
	return g
}

func (f *inviteRewardFixture) addPaidOrder(t *testing.T, months int, pay uint64, mutate func(*Order)) Order {
	t.Helper()
	o := Order{UUID: fmt.Sprintf("ord-inv2-%d", time.Now().UnixNano()), Title: "second", UserID: f.invitee.ID,
		PayAmount: pay, IsPaid: BoolPtr(true), Meta: "{}"}
	require.NoError(t, o.SetPlan(&Plan{Month: months}))
	if mutate != nil {
		mutate(&o)
	}
	require.NoError(t, db.Get().Create(&o).Error)
	return o
}

func eaOf(t *testing.T, id uint64) int64 {
	t.Helper()
	var u User
	require.NoError(t, db.Get().Select("expired_at").First(&u, id).Error)
	return u.ExpiredAt
}

// 18. 网页后台退款也撤回双方邀请奖励；keepInviteRewards 经审批参数豁免。
func TestWebRefund_ReversesInviteGrant(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := webInviteFixture(t)
	g := f.grant(t)
	require.Equal(t, InviteTriggerOrder, g.TriggerKind)
	require.Equal(t, strconv.FormatUint(f.order.ID, 10), g.TriggerRef)
	require.Greater(t, eaOf(t, f.inviter.ID), time.Now().Unix()+86400)

	require.NoError(t, ProcessOrderRefund(context.Background(), f.order.ID, "test refund", 1))
	assert.True(t, f.grant(t).Reversed)
	assert.LessOrEqual(t, eaOf(t, f.invitee.ID), time.Now().Unix()+120)
	assert.LessOrEqual(t, eaOf(t, f.inviter.ID), time.Now().Unix()+120)
}

func TestWebRefund_KeepInviteRewardsViaApproval(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := webInviteFixture(t)
	inviterEA := eaOf(t, f.inviter.ID)
	params, _ := json.Marshal(orderRefundApprovalParams{OrderID: f.order.ID, Reason: "service outage", OperatorID: 1, KeepInviteRewards: true})
	require.NoError(t, executeApprovalOrderRefund(context.Background(), params))
	assert.False(t, f.grant(t).Reversed)
	assert.Equal(t, inviterEA, eaOf(t, f.inviter.ID))
}

// 19. 改锚：被邀请人仍持有另一笔合格购买 → 不撤回；目标是 IAP 单时锚到交易号；实付 0 的单不算。
func TestInviteRefund_ReanchorToQualifyingPurchase(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := webInviteFixture(t)
	f.addPaidOrder(t, 12, 0, nil) // 实付 0：不算
	iap := f.addPaidOrder(t, 12, 4900, func(o *Order) {
		o.Channel, o.AppleTransactionID = OrderChannelAppleIAP, fmt.Sprintf("TXN-RA-%d", time.Now().UnixNano())
	})
	inviterEA := eaOf(t, f.inviter.ID)

	require.NoError(t, ProcessOrderRefund(context.Background(), f.order.ID, "test refund", 1))
	g := f.grant(t)
	assert.False(t, g.Reversed)
	assert.Equal(t, InviteTriggerAppleTxn, g.TriggerKind)
	assert.Equal(t, iap.AppleTransactionID, g.TriggerRef)
	assert.Equal(t, inviterEA, eaOf(t, f.inviter.ID))

	// 之后那笔 IAP 被 Apple 退款：按交易号仍能找到 grant 并撤回（没有订阅行时直接走 helper）
	require.NoError(t, db.Get().Transaction(func(tx *gorm.DB) error {
		_, err := onPaidOrderRefundedInTx(context.Background(), tx, f.invitee.ID, purchaseKeys{AppleTxnID: iap.AppleTransactionID}, nil, false)
		return err
	}))
	assert.True(t, f.grant(t).Reversed)
}

func TestInviteRefund_ZeroPayOrderDoesNotQualify(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := webInviteFixture(t)
	f.addPaidOrder(t, 12, 0, nil)
	require.NoError(t, ProcessOrderRefund(context.Background(), f.order.ID, "test refund", 1))
	assert.True(t, f.grant(t).Reversed)
}

// 20. 一生一次：退款后重买不再发；存量 invited_reward 历史阻止发放；沙盒不发、不消耗首单资格。
func TestInviteReward_OncePerLifetime(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := webInviteFixture(t)
	require.NoError(t, ProcessOrderRefund(context.Background(), f.order.ID, "test refund", 1))
	inviterEA := eaOf(t, f.inviter.ID)

	again := f.addPaidOrder(t, 12, 1000, nil)
	require.NoError(t, db.Get().Transaction(func(tx *gorm.DB) error {
		return handleInvitePurchaseRewardInTx(context.Background(), tx, &again)
	}))
	assert.Equal(t, inviterEA, eaOf(t, f.inviter.ID), "重买不再给邀请人发奖励")
	var n int64
	db.Get().Model(&InviteRewardGrant{}).Where("invitee_user_id = ?", f.invitee.ID).Count(&n)
	assert.Equal(t, int64(1), n)
}

func TestInviteReward_LegacyHistoryBlocks(t *testing.T) {
	skipIfNoDB(t)
	f := setupInviteRewardFixture(t, 12)
	require.NoError(t, db.Get().Create(&UserProHistory{UserID: f.invitee.ID, Type: VipInvitedReward, Days: 30, Reason: "legacy"}).Error)
	f.runReward(t)
	_, inviterHist := f.rewardHistories(t)
	assert.Empty(t, inviterHist)
}

func TestAppleIAP_SandboxNoInviteRewardNoFirstOrder(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.env = appstore.Environment_Sandbox
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "SBX-INV1", t0, t0+365*aDay))
	var n int64
	db.Get().Model(&InviteRewardGrant{}).Where("invitee_user_id = ?", f.buyer.ID).Count(&n)
	assert.Zero(t, n)
	var u User
	require.NoError(t, db.Get().First(&u, f.buyer.ID).Error)
	assert.False(t, u.IsFirstOrderDone != nil && *u.IsFirstOrderDone, "沙盒不消耗首单资格")
	assertNear(t, t0+365*aDay, u.ExpiredAt, 120, "权益照发")

	// 沙盒退款再被撤销：同样不得占用首单资格
	f.refund(t, "SBX-INV1", t0, t0+365*aDay)
	f.reverse(t, "SBX-INV1", t0, t0+365*aDay, time.Now().UnixMilli()+1000)
	require.False(t, f.refundRow(t, "SBX-INV1").Active, "撤销已生效")
	require.NoError(t, db.Get().First(&u, f.buyer.ID).Error)
	assert.False(t, u.IsFirstOrderDone != nil && *u.IsFirstOrderDone, "撤销沙盒退款也不消耗首单资格")
}

// 21. 扣减顺序：邀请扣减在付费扣减之后、基于重读值，合计不使到期早于 now。
func TestInviteRefund_NeverBelowNow(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := webInviteFixture(t)
	// 被邀请人已把奖励用掉大半：只剩 5 天
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.invitee.ID).Update("expired_at", time.Now().Unix()+5*aDay).Error)
	require.NoError(t, ProcessOrderRefund(context.Background(), f.order.ID, "test refund", 1))
	assert.GreaterOrEqual(t, eaOf(t, f.invitee.ID), time.Now().Unix()-5)
	assertNear(t, 5*aDay, f.grant(t).InviteeCutSeconds, 120, "实扣 = 剩余")
}

// ---------- 分销 ----------

// 22. 计数标记在分成比例为 0 的早退之前写；IAP 返现冻结 90 天。
func TestRetailer_CountMarkAndIAPFreeze(t *testing.T) {
	skipIfNoDB(t)
	f0 := setupIAPOrderFixture(t, 0, 0)
	t0 := time.Now().Unix()
	require.NoError(t, f0.credit(t, "RT-Z1", t0, t0+365*aDay))
	assert.Equal(t, f0.config.ID, f0.orders(t)[0].RetailerCountedID, "比例为 0 也记计数标记")

	f := setupIAPOrderFixture(t, 30, 10)
	require.NoError(t, f.credit(t, "RT-F1", t0, t0+365*aDay))
	var income WalletChange
	require.NoError(t, db.Get().Where("type = ? AND order_id = ?", WalletChangeTypeIncome, f.orders(t)[0].ID).First(&income).Error)
	require.NotNil(t, income.FrozenUntil)
	assert.InDelta(t, float64(time.Now().Add(90*24*time.Hour).Unix()), float64(income.FrozenUntil.Unix()), 3600, "IAP 返现冻结 90 天")
}

// 22b. 有其它有效付费单时转移计数标记；最后一笔退款才扣；L2 自动升级后跌破门槛告警。
func TestRetailer_CountTransferThenDecrementWithAlert(t *testing.T) {
	skipIfNoDB(t)
	alerts := captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	required := RetailerLevelConfig[RetailerLevelRetailer].RequiredUsers
	require.NoError(t, db.Get().Model(&RetailerConfig{}).Where("id = ?", f.config.ID).
		Updates(map[string]any{"level": RetailerLevelRetailer, "paid_user_count": required}).Error)
	h := RetailerLevelHistory{RetailerConfigID: f.config.ID, OldLevel: 1, NewLevel: 2, Reason: "auto_upgrade"}
	require.NoError(t, db.Get().Create(&h).Error)
	t.Cleanup(func() { db.Get().Delete(&h) })

	mk := func(counted uint64) Order {
		o := Order{UUID: fmt.Sprintf("ord-rt-%d", time.Now().UnixNano()), Title: "web", UserID: f.buyer.ID,
			PayAmount: 1000, IsPaid: BoolPtr(true), Meta: "{}", RetailerCountedID: counted}
		require.NoError(t, db.Get().Create(&o).Error)
		return o
	}
	a, b := mk(f.config.ID), mk(0)

	require.NoError(t, ProcessOrderRefund(context.Background(), a.ID, "test", 1))
	var cfg RetailerConfig
	require.NoError(t, db.Get().First(&cfg, f.config.ID).Error)
	assert.Equal(t, required, cfg.PaidUserCount, "买家仍有有效付费单：不扣，转移标记")
	var bNow Order
	require.NoError(t, db.Get().First(&bNow, b.ID).Error)
	assert.Equal(t, f.config.ID, bNow.RetailerCountedID)

	require.NoError(t, ProcessOrderRefund(context.Background(), b.ID, "test", 1))
	require.NoError(t, db.Get().First(&cfg, f.config.ID).Error)
	assert.Equal(t, required-1, cfg.PaidUserCount)
	assert.True(t, alerts.has("[RETAILER-COUNT-DROP]"))
}

// 22c. 返现被提走后才退款 → 余额为负并告警。
func TestRetailer_NegativeBalanceAlert(t *testing.T) {
	skipIfNoDB(t)
	alerts := captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10) // 保留邀请关系：返现靠它产生
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "RT-NB1", t0, t0+365*aDay))
	require.Equal(t, int64(1470), f.retailerBalance(t), "前置：返现已发")
	require.NoError(t, db.Get().Model(&Wallet{}).Where("user_id = ?", f.retailer.ID).Update("balance", 0).Error)
	f.refundEv(t, "RT-NB1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	assert.True(t, alerts.has("[CASHBACK-NEGATIVE]"))
}

// 17. 后台预校验拒绝 IAP 订单，不建审批单。
func TestAdminRefund_RejectsAppleIAPOrder(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "ADM-IAP1", t0, t0+365*aDay))
	var before int64
	db.Get().Model(&AdminApproval{}).Count(&before)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/app/orders/:uuid/refund", api_admin_refund_order)
	body, _ := json.Marshal(map[string]any{"reason": "valid test reason"})
	req := httptest.NewRequest(http.MethodPost, "/app/orders/"+f.orders(t)[0].UUID+"/refund", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, int(ErrorNotSupported), respCode(t, w), w.Body.String())
	var after int64
	db.Get().Model(&AdminApproval{}).Count(&after)
	assert.Equal(t, before, after)
}

// ---------- 对账 ----------

func fakeAppleStatus(t *testing.T, fn func(otx string) (*appleSubStatus, error)) {
	t.Helper()
	old := fetchAppleSubStatus
	fetchAppleSubStatus = func(_ context.Context, _, otx string) (*appleSubStatus, error) { return fn(otx) }
	t.Cleanup(func() { fetchAppleSubStatus = old })
}

func fakeAppleTxn(t *testing.T, fn func(id string) (*appstore.TransactionInfo, error)) {
	t.Helper()
	old := fetchAppleTransaction
	fetchAppleTransaction = func(_ context.Context, _, id string) (*appstore.TransactionInfo, error) { return fn(id) }
	t.Cleanup(func() { fetchAppleTransaction = old })
}

// 23. 巡检桶：有效、近 120 天过期、带生效退款记录的 Apple 生产订阅按桶每周覆盖；沙盒与远期过期不扫。
func TestSubscriptionsToReconcile_AppleWeeklyBuckets(t *testing.T) {
	skipIfNoDB(t)
	user := CreateTestUser(t)
	now := time.Now().Unix()
	bucket := (now / 86400) % appleReconcileBuckets
	type c struct {
		status, env string
		cpe         int64
		refund      bool
		wantInBkt   bool
	}
	cases := []c{
		{"active", "Production", now + 200*aDay, false, true},
		{"expired", "Production", now - 30*aDay, false, true},
		{"expired", "Production", now - 200*aDay, false, false},
		{"revoked", "Production", now + 200*aDay, true, true},
		{"revoked", "Production", now + 200*aDay, false, false},
		{"active", "Sandbox", now + 200*aDay, false, false},
	}
	var subs []Subscription
	t.Cleanup(func() {
		db.Get().Unscoped().Where("user_id = ?", user.ID).Delete(&Subscription{})
		db.Get().Unscoped().Where("user_id = ?", user.ID).Delete(&AppleRefund{})
	})
	// 每个场景建 7 行，保证每个桶都覆盖到
	for i, cs := range cases {
		for k := 0; k < int(appleReconcileBuckets); k++ {
			s := Subscription{UserID: user.ID, Provider: "apple", ProviderSubscriptionID: fmt.Sprintf("BKT-%d-%d-%d", now, i, k),
				Status: cs.status, Environment: cs.env, CurrentPeriodEnd: cs.cpe}
			require.NoError(t, db.Get().Create(&s).Error)
			if cs.refund {
				require.NoError(t, db.Get().Create(&AppleRefund{UserID: user.ID, SubscriptionID: s.ID, OriginalTransactionID: s.ProviderSubscriptionID,
					TransactionID: s.ProviderSubscriptionID + "-T", Active: true, RevocationDate: (now - 10*aDay) * 1000}).Error)
			}
			subs = append(subs, s)
		}
	}
	got, err := subscriptionsToReconcile(now)
	require.NoError(t, err)
	in := map[uint64]bool{}
	for _, s := range got {
		in[s.ID] = true
	}
	for i, s := range subs {
		cs := cases[i/int(appleReconcileBuckets)]
		want := cs.wantInBkt && int64(s.ID)%appleReconcileBuckets == bucket
		assert.Equal(t, want, in[s.ID], "case %d sub %d", i/int(appleReconcileBuckets), s.ID)
	}
}

// 24. 对账发现 Apple Revoked → 收回，且不执行宽限延长。
func TestReconcile_AppleRevokedAppliesRefundWithoutGrace(t *testing.T) {
	skipIfNoDB(t)
	setTestAppleBundleID(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "RC-RV1", t0, t0+365*aDay))
	tx := f.txn("RC-RV1", t0, t0+365*aDay)
	fakeAppleStatus(t, func(string) (*appleSubStatus, error) {
		return &appleSubStatus{status: appstore.SubscriptionStatus_Revoked, txn: tx,
			renewal: &appstore.RenewalInfo{GracePeriodExpiresDate: (t0 + 16*aDay) * 1000}}, nil
	})
	sub := f.subNow(t)
	changed, err := reconcileSubscription(context.Background(), &sub, time.Now().Unix())
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "revoked", f.subNow(t).Status)
	assert.LessOrEqual(t, f.userNow(t, f.buyer.ID).ExpiredAt, time.Now().Unix()+120, "收回且无宽限延长")
	r := f.refundRow(t, "RC-RV1")
	assert.Equal(t, "reconcile", r.Source)
	assert.Equal(t, tx.RevocationDate, r.RefundSignedAt, "证据时刻取 revocationDate")
}

// 25. 撤销探测：冷却期、交易号、环境、仍有 revocationDate、请求失败都不恢复；满足条件才恢复，
// 证据时刻 = 最新退款证据 + 1ms。
func TestReconcile_ReversalProbe(t *testing.T) {
	skipIfNoDB(t)
	setTestAppleBundleID(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "RC-PB1", t0, t0+365*aDay))
	f.refundEv(t, "RC-PB1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	fakeAppleStatus(t, func(string) (*appleSubStatus, error) {
		return &appleSubStatus{status: appstore.SubscriptionStatus_Active}, nil
	})
	clean := f.txn("RC-PB1", t0, t0+365*aDay)
	clean.RevocationDate = 0
	resp := clean
	var respErr error
	fakeAppleTxn(t, func(string) (*appstore.TransactionInfo, error) { return resp, respErr })
	run := func() {
		sub := f.subNow(t)
		_, err := reconcileSubscription(context.Background(), &sub, time.Now().Unix())
		require.NoError(t, err)
	}

	run() // 冷却期内
	assert.True(t, f.refundRow(t, "RC-PB1").Active)

	old := (time.Now().Unix() - 3*aDay) * 1000
	require.NoError(t, db.Get().Model(&AppleRefund{}).Where("transaction_id = ?", "RC-PB1").Update("refund_signed_at", old).Error)
	defer func() {
		var stray int64
		db.Get().Model(&AppleRefund{}).Where("transaction_id = ?", "OTHER").Count(&stray)
		assert.Zero(t, stray, "交易号不符的应答不得被当作撤销处理")
	}()
	for _, bad := range []func(){
		func() { respErr = fmt.Errorf("apple down") },
		func() { respErr = nil; x := *clean; x.TransactionId = "OTHER"; resp = &x },
		func() { x := *clean; x.Environment = appstore.Environment_Sandbox; resp = &x },
		func() { x := *clean; x.RevocationDate = time.Now().UnixMilli(); resp = &x },
	} {
		bad()
		run()
		assert.True(t, f.refundRow(t, "RC-PB1").Active)
	}

	resp, respErr = clean, nil
	run()
	r := f.refundRow(t, "RC-PB1")
	assert.False(t, r.Active)
	assert.Equal(t, old+1, r.ReversedSignedAt)
	assertNear(t, time.Now().Unix()+365*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 180, "恢复")
	assert.Equal(t, "active", f.subNow(t).Status)
}

// 25b. F4：退款 → 漏收撤销 → 再退款（行仍 Active，只记下更晚证据）→ 探测读到旧数据误恢复 →
// 再来的对账证据早于撤销时间戳 → 冲突告警一次，数据不动。
func TestReconcile_ConflictAlertOnce(t *testing.T) {
	skipIfNoDB(t)
	setTestAppleBundleID(t)
	alerts := captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "RC-CF1", t0, t0+365*aDay))
	tx := f.txn("RC-CF1", t0, t0+365*aDay)
	s := time.Now().UnixMilli()
	f.refundEv(t, "RC-CF1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: s})
	f.reverse(t, "RC-CF1", t0, t0+365*aDay, s+5000)
	ea := f.userNow(t, f.buyer.ID).ExpiredAt
	tx.RevocationDate = s + 1000 // 早于撤销证据
	fakeAppleStatus(t, func(string) (*appleSubStatus, error) {
		return &appleSubStatus{status: appstore.SubscriptionStatus_Revoked, txn: tx}, nil
	})
	for i := 0; i < 2; i++ {
		sub := f.subNow(t)
		_, err := reconcileSubscription(context.Background(), &sub, time.Now().Unix())
		require.NoError(t, err)
	}
	assert.Equal(t, ea, f.userNow(t, f.buyer.ID).ExpiredAt)
	n := 0
	for _, m := range alerts.all() {
		if len(m) > 23 && m[:23] == "[APPLE-REFUND-CONFLICT]" {
			n++
		}
	}
	assert.Equal(t, 1, n, "每条记录只告警一次")
}

// 26. revoked 行：对账拿到退款后新付款 → 复活；Apple 报 Expired → 保持 revoked。
func TestReconcile_RevokedRowRevivalAndExpiredGuard(t *testing.T) {
	skipIfNoDB(t)
	setTestAppleBundleID(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "RC-RS1", t0, t0+365*aDay))
	f.refundEv(t, "RC-RS1", t0, t0+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})

	fakeAppleStatus(t, func(string) (*appleSubStatus, error) {
		return &appleSubStatus{status: appstore.SubscriptionStatus_Expired}, nil
	})
	// 周期末已过：Expired 分支的原子 UPDATE 条件全部满足，只有"revoked 行跳过"这道门挡得住
	require.NoError(t, db.Get().Model(&Subscription{}).Where("provider_subscription_id = ?", f.origTxn).
		Update("current_period_end", t0-aDay).Error)
	sub := f.subNow(t)
	_, err := reconcileSubscription(context.Background(), &sub, time.Now().Unix())
	require.NoError(t, err)
	assert.Equal(t, "revoked", f.subNow(t).Status)

	next := f.txn("RC-RS2", t0+10*aDay, t0+375*aDay)
	next.RevocationDate = 0
	fakeAppleStatus(t, func(string) (*appleSubStatus, error) {
		return &appleSubStatus{status: appstore.SubscriptionStatus_Active, txn: next}, nil
	})
	sub = f.subNow(t)
	_, err = reconcileSubscription(context.Background(), &sub, time.Now().Unix())
	require.NoError(t, err)
	assert.Equal(t, "active", f.subNow(t).Status)
}

// ---------- webhook 端到端 ----------

// 28. REFUND（带 revocationType/Percentage）与 REFUND_REVERSED 字面量经 webhook 端到端；
// 别的 app 的通知（bundle 不符）拒收；撤销退款须经 Apple 复核确认。
func TestAppleWebhook_RefundAndReversedEndToEnd(t *testing.T) {
	skipIfNoDB(t)
	setTestAppleBundleID(t)
	captureBillingAlerts(t)
	rootPEM, leafKey, leafDER, intDER := makeTestChain(t)
	old := appleRootCAPEM
	appleRootCAPEM = rootPEM
	t.Cleanup(func() { appleRootCAPEM = old })

	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "WH-RF1", t0, t0+365*aDay))

	postB := func(nType, bundle, txnBundle string, signedAt int64, revoked bool) int {
		claims := map[string]any{
			"bundleId": txnBundle, "originalTransactionId": f.origTxn, "transactionId": "WH-RF1", "productId": f.productID,
			"inAppOwnershipType": "PURCHASED", "environment": "Production",
			"purchaseDate": t0 * 1000, "expiresDate": (t0 + 365*aDay) * 1000, "signedDate": signedAt,
		}
		if revoked {
			claims["revocationDate"] = signedAt
			claims["revocationType"] = "REFUND_PRORATED"
			claims["revocationPercentage"] = 99700
		}
		tj, _ := json.Marshal(claims)
		inner := signJWS(t, leafKey, leafDER, intDER, string(tj))
		outer, _ := json.Marshal(map[string]any{
			"notificationType": nType, "notificationUUID": fmt.Sprintf("uuid-%s-%s-%d", nType, bundle, signedAt),
			"version": "2.0", "signedDate": signedAt,
			"data": map[string]any{"bundleId": bundle, "environment": "Production", "signedTransactionInfo": inner},
		})
		body, _ := json.Marshal(map[string]string{"signedPayload": signJWS(t, leafKey, leafDER, intDER, string(outer))})
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.POST("/webhook/apple", api_apple_webhook)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/webhook/apple", bytes.NewReader(body)))
		return w.Code
	}
	post := func(nType, bundle string, signedAt int64, revoked bool) int {
		return postB(nType, bundle, bundle, signedAt, revoked)
	}

	s := time.Now().UnixMilli()
	assert.Equal(t, 400, postB("REFUND", "com.other.app", "io.kaitu.test", s, true), "通知 bundle 不符拒收")
	assert.Equal(t, 400, postB("REFUND", "io.kaitu.test", "com.other.app", s, true), "交易 bundle 不符拒收")
	var n int64
	db.Get().Model(&AppleRefund{}).Where("transaction_id = ?", "WH-RF1").Count(&n)
	require.Zero(t, n)

	require.Equal(t, 200, post("REFUND", "io.kaitu.test", s, true))
	r := f.refundRow(t, "WH-RF1")
	assert.True(t, r.Active)
	assert.Equal(t, "REFUND_PRORATED", r.RevocationType)
	assert.Equal(t, int32(99700), r.RevocationPercentage)
	assert.Equal(t, s, r.RefundSignedAt)
	assert.Equal(t, "revoked", f.subNow(t).Status)

	// Apple 复核仍说已退款 → 不恢复
	appleSays := f.txn("WH-RF1", t0, t0+365*aDay)
	appleSays.BundleId = "io.kaitu.test"
	fakeAppleTxn(t, func(string) (*appstore.TransactionInfo, error) { x := *appleSays; return &x, nil })
	require.Equal(t, 200, post("REFUND_REVERSED", "io.kaitu.test", s+1000, false))
	assert.True(t, f.refundRow(t, "WH-RF1").Active)

	// Apple 复核确认已撤销 → 恢复
	appleSays.RevocationDate = 0
	require.Equal(t, 200, post("REFUND_REVERSED", "io.kaitu.test", s+2000, false))
	assert.False(t, f.refundRow(t, "WH-RF1").Active)
	assert.Equal(t, "active", f.subNow(t).Status)
}

// 复活入账按 Apple 周期对齐：退款后的新付款很久之后才入账，不发超过 Apple 覆盖的时长。
func TestAppleRefund_LateRevivalAlignedToApple(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-LR1", t0, t0+365*aDay))
	refunded := f.txn("AR-LR1", t0, t0+365*aDay)
	refunded.RevocationDate = (t0 - 300*aDay) * 1000 // Apple 300 天前就退了
	_, err := applyAppleRefund(context.Background(), f.origTxn, refunded, appleRefundEvidence{SignedAt: refunded.RevocationDate, Source: "webhook"})
	require.NoError(t, err)
	// 账本早已过期（等价于当年就处理了退款）
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.buyer.ID).Update("expired_at", t0-250*aDay).Error)
	// 退款后 200 天前重订阅，到现在才入账：Apple 覆盖到 +165 天
	require.NoError(t, f.credit(t, "AR-LR2", t0-200*aDay, t0+165*aDay))
	assert.Equal(t, "active", f.subNow(t).Status)
	assertNear(t, t0+165*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 120, "与 Apple 覆盖对齐，不从 now 起叠一整期")
}

// 19b. 退一笔与奖励无关的订单 → 奖励不动。
func TestInviteRefund_UnrelatedOrderKeepsGrant(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := webInviteFixture(t)
	other := f.addPaidOrder(t, 1, 500, nil)
	require.NoError(t, ProcessOrderRefund(context.Background(), other.ID, "test refund", 1))
	g := f.grant(t)
	assert.False(t, g.Reversed)
	assert.Equal(t, strconv.FormatUint(f.order.ID, 10), g.TriggerRef)
}

// 11b. verify 端点：被退交易返回明确的"已退款"错误。
func TestAppleIAPVerify_RefundedTransaction(t *testing.T) {
	skipIfNoDB(t)
	setTestAppleBundleID(t)
	user := CreateTestUser(t)
	plan := createApplePlan(t, 12)
	fakeAppleTxn(t, func(id string) (*appstore.TransactionInfo, error) {
		return &appstore.TransactionInfo{BundleId: "io.kaitu.test", TransactionId: id, OriginalTransactionId: "OTX-VR-" + id,
			ProductId: plan.AppleProductID, InAppOwnershipType: appstore.OwnershipType_PURCHASED, Environment: "Production",
			PurchaseDate: time.Now().UnixMilli(), ExpiresDate: time.Now().Add(365 * 24 * time.Hour).UnixMilli(),
			RevocationDate: time.Now().UnixMilli(), AppAccountToken: deriveAppleAccountToken(user.UUID)}, nil
	})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: user.ID, User: user})
		c.Next()
	})
	r.POST("/api/iap/apple/verify", api_apple_iap_verify)
	body, _ := json.Marshal(map[string]string{"transactionId": fmt.Sprintf("VR-%d", time.Now().UnixNano())})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/iap/apple/verify", bytes.NewReader(body)))
	assert.Equal(t, int(ErrorInvalidOperation), respCode(t, w))
	assert.Contains(t, w.Body.String(), "refunded")
}

// 19c. 奖励锚在别的购买上、且被邀请人已无其它合格购买时，退一笔无关订单也不得撤回奖励
// （锚点匹配是唯一挡板，改锚兜不住）。
func TestInviteRefund_UnrelatedOrderNoFallbackAnchor(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := webInviteFixture(t)
	require.NoError(t, db.Get().Model(&InviteRewardGrant{}).Where("invitee_user_id = ?", f.invitee.ID).
		Updates(map[string]any{"trigger_kind": InviteTriggerAppleTxn, "trigger_ref": "TXN-ELSEWHERE"}).Error)
	require.NoError(t, db.Get().Model(&Order{}).Where("id = ?", f.order.ID).Update("is_refunded", true).Error)
	other := f.addPaidOrder(t, 1, 500, nil)
	inviterEA := eaOf(t, f.inviter.ID)
	require.NoError(t, ProcessOrderRefund(context.Background(), other.ID, "test refund", 1))
	assert.False(t, f.grant(t).Reversed)
	assert.Equal(t, inviterEA, eaOf(t, f.inviter.ID))
}


// 复活入账：账本过期时刻介于购买与现在之间（迟到入账）→ 只发 Apple 剩余覆盖期。
func TestAppleRefund_LateRevivalLedgerBetweenPurchaseAndNow(t *testing.T) {
	skipIfNoDB(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "AR-LB1", t0, t0+365*aDay))
	refunded := f.txn("AR-LB1", t0, t0+365*aDay)
	refunded.RevocationDate = (t0 - 300*aDay) * 1000
	_, err := applyAppleRefund(context.Background(), f.origTxn, refunded, appleRefundEvidence{SignedAt: refunded.RevocationDate, Source: "webhook"})
	require.NoError(t, err)
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.buyer.ID).Update("expired_at", t0-100*aDay).Error)
	require.NoError(t, f.credit(t, "AR-LB2", t0-200*aDay, t0+165*aDay))
	assertNear(t, t0+165*aDay, f.userNow(t, f.buyer.ID).ExpiredAt, 120, "不因账本过期时刻晚于购买而多发")
}
