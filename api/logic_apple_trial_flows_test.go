package center

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

// 试用链端到端流程（spec 2026-10-19 §3.3 / §3.5）：改绑竞态、webhook 转付费、退款、overleap、对账到期。

// trialWebhook 是签名 JWS 的 Apple webhook 测试台：byID 同时是 webhook 内层交易与 fetchAppleTransaction 的真相源。
type trialWebhook struct {
	t    *testing.T
	byID map[string]*appstore.TransactionInfo
	post func(nType, subtype, uuid, txnID string) int
}

func newTrialWebhook(t *testing.T) *trialWebhook {
	t.Helper()
	setTestAppleBundleID(t)
	rootPEM, leafKey, leafDER, intDER := makeTestChain(t)
	old := appleRootCAPEM
	appleRootCAPEM = rootPEM
	t.Cleanup(func() { appleRootCAPEM = old })
	h := &trialWebhook{t: t, byID: map[string]*appstore.TransactionInfo{}}
	fakeAppleTxn(t, func(id string) (*appstore.TransactionInfo, error) {
		info, ok := h.byID[id]
		if !ok {
			return nil, fmt.Errorf("unknown txn %s", id)
		}
		x := *info
		return &x, nil
	})
	h.post = func(nType, subtype, uuid, txnID string) int {
		tj, err := json.Marshal(h.byID[txnID])
		require.NoError(t, err)
		inner := signJWS(t, leafKey, leafDER, intDER, string(tj))
		outer, _ := json.Marshal(map[string]any{
			"notificationType": nType, "subtype": subtype,
			"notificationUUID": uuid, "version": "2.0", "signedDate": time.Now().UnixMilli(),
			"data": map[string]any{"bundleId": "io.kaitu.test", "environment": "Production", "signedTransactionInfo": inner},
		})
		body, _ := json.Marshal(map[string]string{"signedPayload": signJWS(t, leafKey, leafDER, intDER, string(outer))})
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.POST("/webhook/apple", api_apple_webhook)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/webhook/apple", bytes.NewReader(body)))
		return w.Code
	}
	return h
}

// rebindUserB 建第二个账号 B（不经邀请码）并注册它名下入账产物的清理。
func rebindUserB(t *testing.T) *User {
	t.Helper()
	b := CreateTestUser(t)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("user_id = ?", b.ID).Delete(&SubscriptionCredit{})
		db.Get().Unscoped().Where("user_id = ?", b.ID).Delete(&Subscription{})
		db.Get().Unscoped().Where("user_id = ?", b.ID).Delete(&UserProHistory{})
		db.Get().Unscoped().Where("user_id = ?", b.ID).Delete(&Order{})
	})
	return b
}

func creditCount(t *testing.T, txnID string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Get().Model(&SubscriptionCredit{}).Where("transaction_id = ?", txnID).Count(&n).Error)
	return n
}

// A 的试用链从没付过费、已过期；B 在同一 Apple ID 上重订阅，交易带 B 的 token。返回 B 与那笔付费交易。
func setupRebindRace(t *testing.T, f *iapOrderFixture, t0 int64, prefix string) (*User, *appstore.TransactionInfo) {
	t.Helper()
	require.NoError(t, creditInfo(t, f.buyer.ID, f.trialTxn(prefix+"-T", t0-14*aDay, t0-7*aDay)))
	require.True(t, f.userNow(t, f.buyer.ID).InTrial)
	b := rebindUserB(t)
	paid := f.paidTxn(prefix+"-P", t0, t0+365*aDay)
	paid.AppAccountToken = deriveAppleAccountToken(b.UUID)
	return b, paid
}

// assertRebindToB B 成为订阅链主人并拿到首购；A 什么都没拿到、试用标志清掉。
func assertRebindToB(t *testing.T, f *iapOrderFixture, b *User, paidID string, t0, aExpiredBefore int64) {
	t.Helper()
	s := f.subNow(t)
	assert.Equal(t, b.ID, s.UserID, "订阅链改绑到 B")
	assert.False(t, s.IsTrial)
	c := creditRowOf(t, paidID)
	assert.Equal(t, b.ID, c.UserID)
	assert.Equal(t, "purchase", c.Kind)
	assert.Equal(t, int64(1), creditCount(t, paidID), "只入账一次")
	assert.Equal(t, int64(1), ordersForTxn(t, paidID))
	var o Order
	require.NoError(t, db.Get().Where("apple_transaction_id = ?", paidID).First(&o).Error)
	assert.Equal(t, b.ID, o.UserID, "订单属于 B")
	bu := f.userNow(t, b.ID)
	assert.True(t, isTrue(bu.IsFirstOrderDone))
	assert.InDelta(t, t0+365*aDay, bu.ExpiredAt, 60)
	a := f.userNow(t, f.buyer.ID)
	assert.Equal(t, aExpiredBefore, a.ExpiredAt, "A 没拿到时长")
	assert.False(t, isTrue(a.IsFirstOrderDone), "A 没消耗首单")
	assert.False(t, a.InTrial, "A 的试用标志清掉")
	assert.Empty(t, f.orders(t), "A 名下无订单")
	assert.Zero(t, f.retailerBalance(t), "A 的分销商不拿返现")
}

// Finding 1：webhook 先到——以 A 入账会抢走 B 的首购。webhook 必须 5xx 且什么都不写；B verify 改绑；Apple 重投命中去重。
func TestTrial_RebindRace_WebhookFirst(t *testing.T) {
	trialDB(t)
	h := newTrialWebhook(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()
	b, paid := setupRebindRace(t, f, t0, "TR-RW")
	h.byID[paid.TransactionId] = paid
	aBefore := f.userNow(t, f.buyer.ID)
	subBefore := f.subNow(t)

	require.Equal(t, 503, h.post("SUBSCRIBED", "RESUBSCRIBE", "uuid-rw-1", paid.TransactionId), "等改绑：让 Apple 重投")
	assert.Zero(t, creditCount(t, paid.TransactionId), "什么都没入账")
	s := f.subNow(t)
	assert.Equal(t, f.buyer.ID, s.UserID)
	assert.True(t, s.IsTrial, "链仍是从没付过费的试用链")
	assert.Equal(t, subBefore.ProviderLatestRef, s.ProviderLatestRef, "订阅行未被改写")
	assert.Empty(t, s.LastEventID, "不记通知 UUID，重投才不会被去重门挡掉")
	assert.Equal(t, aBefore.ExpiredAt, f.userNow(t, f.buyer.ID).ExpiredAt)
	assert.True(t, f.userNow(t, f.buyer.ID).InTrial, "没改写任何用户状态")

	require.NoError(t, verifyAndGrantTransaction(context.Background(), b.ID, paid.TransactionId))
	require.Equal(t, 200, h.post("SUBSCRIBED", "RESUBSCRIBE", "uuid-rw-1", paid.TransactionId), "改绑后重投被接受")
	assertRebindToB(t, f, b, paid.TransactionId, t0, aBefore.ExpiredAt)
}

// Finding 1：对账 worker 先拉到这笔交易——记日志跳过（不报错），B verify 改绑，再对账无害。
func TestTrial_RebindRace_ReconcileFirst(t *testing.T) {
	trialDB(t)
	setTestAppleBundleID(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()
	b, paid := setupRebindRace(t, f, t0, "TR-RR")
	fakeAppleTxn(t, func(id string) (*appstore.TransactionInfo, error) {
		require.Equal(t, paid.TransactionId, id)
		x := *paid
		return &x, nil
	})
	fakeAppleStatus(t, func(otx string) (*appleSubStatus, error) {
		x := *paid
		return &appleSubStatus{status: appstore.SubscriptionStatus_Active, txn: &x}, nil
	})
	aBefore := f.userNow(t, f.buyer.ID)

	sub := f.subNow(t)
	changed, err := reconcileAppleSubscription(context.Background(), &sub, time.Now().Unix())
	require.NoError(t, err, "跳过，不是失败（不进告警/重试循环）")
	assert.False(t, changed)
	assert.Zero(t, creditCount(t, paid.TransactionId))
	assert.Equal(t, f.buyer.ID, f.subNow(t).UserID)
	assert.True(t, f.subNow(t).IsTrial)
	assert.Equal(t, aBefore.ExpiredAt, f.userNow(t, f.buyer.ID).ExpiredAt)

	require.NoError(t, verifyAndGrantTransaction(context.Background(), b.ID, paid.TransactionId))
	sub = f.subNow(t)
	_, err = reconcileAppleSubscription(context.Background(), &sub, time.Now().Unix())
	require.NoError(t, err, "改绑后再对账命中去重")
	assertRebindToB(t, f, b, paid.TransactionId, t0, aBefore.ExpiredAt)
}

// Finding 1 + 2(b)：同 token（A 自己的）付费交易经签名 webhook DID_RENEW 到达——照常以 A 入账，按首购转付费。
func TestTrial_WebhookDidRenewConvertsTrial(t *testing.T) {
	trialDB(t)
	h := newTrialWebhook(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()
	trialEnd := t0 + 7*aDay
	rewardDays := int64(configInvite(context.Background()).PurchaseRewardDays)

	h.byID["TR-WR1"] = f.trialTxn("TR-WR1", t0, trialEnd)
	require.NoError(t, verifyAndGrantTransaction(context.Background(), f.buyer.ID, "TR-WR1"))
	require.True(t, f.userNow(t, f.buyer.ID).InTrial)

	h.byID["TR-WR2"] = f.paidTxn("TR-WR2", trialEnd, trialEnd+365*aDay)
	require.Equal(t, 200, h.post("DID_RENEW", "", "uuid-wr-2", "TR-WR2"))

	assert.Equal(t, "purchase", creditRowOf(t, "TR-WR2").Kind, "试用后首笔付费记 purchase")
	assert.Equal(t, f.buyer.ID, creditRowOf(t, "TR-WR2").UserID)
	assert.Equal(t, int64(1), ordersForTxn(t, "TR-WR2"))
	assert.Equal(t, int64(1470), f.retailerBalance(t), "首单比例返佣")
	assert.Equal(t, int64(1), inviteGrants(t, f.buyer.ID))
	u := f.userNow(t, f.buyer.ID)
	assert.True(t, isTrue(u.IsFirstOrderDone))
	assert.False(t, u.InTrial)
	s := f.subNow(t)
	assert.False(t, s.IsTrial)
	assert.Positive(t, s.PaidThrough)
	assert.Equal(t, "uuid-wr-2", s.LastEventID)
	assert.InDelta(t, trialEnd+365*aDay+rewardDays*aDay, u.ExpiredAt, 60)

	// 同一通知重投：幂等。
	require.Equal(t, 200, h.post("DID_RENEW", "", "uuid-wr-2", "TR-WR2"))
	assert.Equal(t, int64(1), creditCount(t, "TR-WR2"))
	assert.Equal(t, int64(1), ordersForTxn(t, "TR-WR2"))

	// 续订交易省略 appAccountToken 也照常入账给 A（不触发改绑门）。
	h.byID["TR-WR3"] = f.paidTxn("TR-WR3", trialEnd+365*aDay, trialEnd+730*aDay)
	h.byID["TR-WR3"].AppAccountToken = ""
	require.Equal(t, 200, h.post("DID_RENEW", "", "uuid-wr-3", "TR-WR3"))
	assert.Equal(t, "renewal", creditRowOf(t, "TR-WR3").Kind)
}

// 2(a)：试用 → 转付费 → Apple 退款那笔付费：首单资格回退、返现撤回、只收回那一笔入账的时长（试用剩余保留）。
func TestTrial_ConvertedThenPaidTxnRefunded(t *testing.T) {
	trialDB(t)
	alerts := captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()
	trialEnd := t0 + 7*aDay

	require.NoError(t, creditInfo(t, f.buyer.ID, f.trialTxn("TR-RF1", t0, trialEnd)))
	paid := f.paidTxn("TR-RF2", trialEnd, trialEnd+365*aDay)
	require.NoError(t, creditInfo(t, f.buyer.ID, paid))
	require.True(t, isTrue(f.userNow(t, f.buyer.ID).IsFirstOrderDone))
	require.Equal(t, int64(1470), f.retailerBalance(t))
	paidCredit := creditRowOf(t, "TR-RF2").CreditedSeconds
	assert.InDelta(t, 365*aDay, paidCredit, 60, "前置：付费那笔入账一整期")

	out := f.refundEv(t, "TR-RF2", trialEnd, trialEnd+365*aDay, appleRefundEvidence{SignedAt: time.Now().UnixMilli()})
	require.True(t, out.Adopted)
	assert.InDelta(t, paidCredit, out.Cut, 120, "收回量 = 那笔付费入账")
	assert.LessOrEqual(t, out.Cut, paidCredit, "绝不多收（试用段不是付费段）")
	assert.True(t, out.MarkedRevoked)

	u := f.userNow(t, f.buyer.ID)
	assert.False(t, isTrue(u.IsFirstOrderDone), "唯一付费单退了：首单资格回退")
	assert.Zero(t, f.retailerBalance(t), "返现撤回")
	assert.InDelta(t, trialEnd, u.ExpiredAt, 180, "试用剩余保留，邀请奖励撤回")
	assert.False(t, u.InTrial, "已转付费的链退款不恢复试用隔离")
	orders := f.orders(t)
	require.Len(t, orders, 1)
	assert.True(t, isTrue(orders[0].IsRefunded))
	assert.Equal(t, "TR-RF2", orders[0].AppleTransactionID)
	assert.True(t, alerts.has("[APPLE-REFUND]"))
}

// 2(c)：overleap 品牌（apple_iap 在其支付渠道里）的免费试用与 kaitu 同语义：不建单、不返佣、设 in_trial。
func TestTrial_OverleapBrandTrial(t *testing.T) {
	trialDB(t)
	require.True(t, BrandOverleap.Config().AllowsPayment(PayChannelAppleIAP), "前置：overleap 允许 apple_iap")
	viper.Set("appstore.bundleIds.overleap", "io.kaitu.test")
	t.Cleanup(func() { viper.Set("appstore.bundleIds.overleap", "") })
	f := setupIAPOrderFixture(t, 30, 10)
	for _, id := range []uint64{f.buyer.ID, f.retailer.ID} {
		require.NoError(t, db.Get().Model(&User{}).Where("id = ?", id).Update("brand", string(BrandOverleap)).Error)
	}
	require.NoError(t, db.Get().Model(&Plan{}).Where("id = ?", f.plan.ID).Update("brand", string(BrandOverleap)).Error)
	t0 := time.Now().Unix()
	fakeAppleTxn(t, func(id string) (*appstore.TransactionInfo, error) { return f.trialTxn(id, t0, t0+7*aDay), nil })

	require.NoError(t, verifyAndGrantTransaction(context.Background(), f.buyer.ID, "TR-OL1"))

	u := f.userNow(t, f.buyer.ID)
	assert.Equal(t, string(BrandOverleap), u.Brand)
	assert.True(t, u.InTrial)
	assert.False(t, isTrue(u.IsFirstOrderDone))
	assert.Equal(t, "basic", u.Tier)
	assert.InDelta(t, t0+7*aDay, u.ExpiredAt, 60)
	assert.Equal(t, "trial", creditRowOf(t, "TR-OL1").Kind)
	assert.Zero(t, ordersForTxn(t, "TR-OL1"), "不建单")
	assert.Zero(t, f.retailerBalance(t), "不返佣")
	assert.Zero(t, inviteGrants(t, f.buyer.ID))
	s := f.subNow(t)
	assert.True(t, s.IsTrial)
	assert.Zero(t, s.PaidThrough)
}

// 2(d)：漏收 EXPIRED 时由对账 worker 清 in_trial。两条路径：
//   - 重放最新交易时 creditAppleTransaction 已把 status 写成 expired（试用到期后首次对账的常态）；
//   - 状态由 Expired 分支的条件 UPDATE 迁移（Apple 没回交易）。
func TestTrial_ReconcileExpiryClearsInTrial(t *testing.T) {
	trialDB(t)
	setTestAppleBundleID(t)
	for _, withTxn := range []bool{true, false} {
		t.Run(fmt.Sprintf("withTxn=%v", withTxn), func(t *testing.T) {
			f := setupIAPOrderFixture(t, 30, 10)
			f.noInvite(t)
			t0 := time.Now().Unix()
			trial := f.trialTxn(fmt.Sprintf("TR-RX-%v", withTxn), t0-7*aDay, t0+aDay)
			require.NoError(t, creditInfo(t, f.buyer.ID, trial))
			require.True(t, f.userNow(t, f.buyer.ID).InTrial)
			// 时间推进到试用结束之后：周期末落到过去，status 仍是 active（漏了 EXPIRED 通知）。
			require.NoError(t, db.Get().Model(&Subscription{}).Where("provider_subscription_id = ?", f.origTxn).
				Update("current_period_end", t0-60).Error)
			ended := *trial
			ended.ExpiresDate = (t0 - 60) * 1000
			fakeAppleStatus(t, func(otx string) (*appleSubStatus, error) {
				st := &appleSubStatus{status: appstore.SubscriptionStatus_Expired}
				if withTxn {
					x := ended
					st.txn = &x
				}
				return st, nil
			})
			expiredBefore := f.userNow(t, f.buyer.ID).ExpiredAt

			sub := f.subNow(t)
			require.Equal(t, "active", sub.Status)
			_, err := reconcileAppleSubscription(context.Background(), &sub, time.Now().Unix())
			require.NoError(t, err)

			u := f.userNow(t, f.buyer.ID)
			assert.False(t, u.InTrial, "对账到期清试用标志")
			assert.Equal(t, expiredBefore, u.ExpiredAt, "不动 expired_at")
			assert.Equal(t, "expired", f.subNow(t).Status)
		})
	}
}
