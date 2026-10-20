package center

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

// 立即转正（spec 2026-10-19 §3.9）：试用中买同组更高等级的 B → Apple 立即扣费、A 终止。
// B 交易沿用 A 的 otx，按"试用链首笔付费"入账；被升级掉的 A 交易（isUpgraded，可能带 revocationDate）不是退款。

// appleMemberProductID 商品 B：等级高于 A、价格相同、不挂任何首次优惠（spec §3.9）。
const appleMemberProductID = "io.kaitu.sub.member.1y"

// upgradePlanB 建 B 的开途套餐行。隐藏（is_active=false，不在前台套餐列表里），
// 顺带锁住 planByAppleProductID 不按可见性过滤——隐藏套餐照样能入账、建单、返佣。
func upgradePlanB(t *testing.T) *Plan {
	t.Helper()
	// 固定商品 id：先清掉中断运行遗留的同 id 行，否则 planByAppleProductID 判歧义。
	require.NoError(t, db.Get().Where("apple_product_id = ?", appleMemberProductID).Delete(&Plan{}).Error)
	p := &Plan{
		PID: "tup" + time.Now().Format("150405.000000"), Label: "正式会员 年付", Price: 4900, OriginPrice: 4900,
		Month: 12, Tier: "basic", AppleProductID: appleMemberProductID, Brand: string(BrandKaitu), IsActive: BoolPtr(false),
	}
	require.NoError(t, db.Get().Create(p).Error)
	require.NoError(t, db.Get().Model(&Plan{}).Where("id = ?", p.ID).Update("is_active", false).Error) // default:true 会吞掉零值
	t.Cleanup(func() { db.Get().Delete(p) })
	return p
}

// upgradeTxnB 商品 B 的付费交易：同一 otx，purchaseDate=now，一年期。
func (f *iapOrderFixture) upgradeTxnB(id string, now int64) *appstore.TransactionInfo {
	b := f.paidTxn(id, now, now+365*aDay)
	b.ProductId = appleMemberProductID
	return b
}

// upgradedTxnA A 被升级掉之后 Apple 给出的样子：isUpgraded=true，带 revocationDate。
func upgradedTxnA(a *appstore.TransactionInfo, now int64) *appstore.TransactionInfo {
	x := *a
	x.IsUpgraded = true
	x.RevocationDate = now * 1000
	return &x
}

func appleRefundRows(t *testing.T, otx string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Get().Model(&AppleRefund{}).Where("original_transaction_id = ?", otx).Count(&n).Error)
	return n
}

// assertUpgraded 转正后的统一断言：建单一次、purchase、首购、in_trial 清、到期 = 试用到期 + 邀请奖励 + B 一期。
func assertUpgraded(t *testing.T, f *iapOrderFixture, bID string, trialEnd, now int64) {
	t.Helper()
	rewardDays := int64(configInvite(context.Background()).PurchaseRewardDays)
	c := creditRowOf(t, bID)
	assert.Equal(t, "purchase", c.Kind, "B 按试用链首笔付费记 purchase")
	assert.Equal(t, int64(1), creditCount(t, bID))
	assert.Equal(t, int64(1), ordersForTxn(t, bID), "建单一次")
	require.Len(t, f.orders(t), 1, "该用户只有 B 这一张单")
	assert.Equal(t, int64(1470), f.retailerBalance(t), "首单比例返佣（隐藏套餐照样作分佣基数）")
	u := f.userNow(t, f.buyer.ID)
	assert.False(t, u.InTrial, "转正清试用线路隔离")
	assert.True(t, isTrue(u.IsFirstOrderDone))
	// 剩余试用天数叠在年费之后（spec §3.9 第 2 条：接受）。
	assert.InDelta(t, trialEnd+rewardDays*aDay+(now+365*aDay-now), u.ExpiredAt, 60)
	s := f.subNow(t)
	assert.False(t, s.IsTrial)
	assert.Equal(t, appleMemberProductID, s.ProductID)
	assert.Equal(t, bID, s.ProviderLatestRef)
	assert.Equal(t, "active", s.Status)
}

// verify 先到：客户端买 B 后立刻上报 → 立即转正；随后 A 带 isUpgraded 重放不改写订阅行。
func TestUpgrade_TrialToMember_VerifyFirst(t *testing.T) {
	trialDB(t)
	h := newTrialWebhook(t)
	f := setupIAPOrderFixture(t, 30, 10)
	upgradePlanB(t)
	t0 := time.Now().Unix()
	trialEnd := t0 + 7*aDay

	h.byID["UP-V-A"] = f.trialTxn("UP-V-A", t0, trialEnd)
	require.NoError(t, verifyAndGrantTransaction(context.Background(), f.buyer.ID, "UP-V-A"))
	require.Equal(t, "trial", creditRowOf(t, "UP-V-A").Kind)
	require.True(t, f.userNow(t, f.buyer.ID).InTrial)

	now := time.Now().Unix()
	h.byID["UP-V-B"] = f.upgradeTxnB("UP-V-B", now)
	require.NoError(t, verifyAndGrantTransaction(context.Background(), f.buyer.ID, "UP-V-B"))
	assertUpgraded(t, f, "UP-V-B", trialEnd, now)

	// webhook 随后到达：命中去重，仍一张单。
	require.Equal(t, 200, h.post(appstore.NotificationType_DID_CHANGE_RENEWAL_PREF, appstore.Subtype_UPGRADE, "uuid-up-v-1", "UP-V-B"))
	assertUpgraded(t, f, "UP-V-B", trialEnd, now)

	// A 被升级掉后重放（StoreKit 更新 / restore）：被取代，不把订阅行改回 A。
	h.byID["UP-V-A"] = upgradedTxnA(h.byID["UP-V-A"], now)
	err := verifyAndGrantTransaction(context.Background(), f.buyer.ID, "UP-V-A")
	require.ErrorIs(t, err, errAppleTxnUpgraded, "可区分的哨兵（verify 端点映射成成功）")
	assert.NotErrorIs(t, err, errAppleTxnRevoked)
	assertUpgraded(t, f, "UP-V-B", trialEnd, now)
}

// webhook 先到：DID_CHANGE_RENEWAL_PREF/UPGRADE 必须入账（兜底路径），随后 verify 重放结果相同。
func TestUpgrade_TrialToMember_WebhookFirst(t *testing.T) {
	trialDB(t)
	h := newTrialWebhook(t)
	f := setupIAPOrderFixture(t, 30, 10)
	upgradePlanB(t)
	t0 := time.Now().Unix()
	trialEnd := t0 + 7*aDay

	h.byID["UP-W-A"] = f.trialTxn("UP-W-A", t0, trialEnd)
	require.NoError(t, verifyAndGrantTransaction(context.Background(), f.buyer.ID, "UP-W-A"))

	now := time.Now().Unix()
	h.byID["UP-W-B"] = f.upgradeTxnB("UP-W-B", now)
	require.Equal(t, 200, h.post(appstore.NotificationType_DID_CHANGE_RENEWAL_PREF, appstore.Subtype_UPGRADE, "uuid-up-w-1", "UP-W-B"))
	assertUpgraded(t, f, "UP-W-B", trialEnd, now)
	assert.Equal(t, "uuid-up-w-1", f.subNow(t).LastEventID)

	require.NoError(t, verifyAndGrantTransaction(context.Background(), f.buyer.ID, "UP-W-B"))
	assertUpgraded(t, f, "UP-W-B", trialEnd, now)
}

// 被升级掉的 A（isUpgraded + revocationDate）经入账门 / 对账 Revoked / 退款收回 / verify 端点：
// 不收回时长、不 revoked、不记 AppleRefund、不发退款告警。
func TestUpgrade_UpgradedTxnNotRefund(t *testing.T) {
	trialDB(t)
	h := newTrialWebhook(t)
	alerts := captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	upgradePlanB(t)
	t0 := time.Now().Unix()
	trialEnd := t0 + 7*aDay

	trialA := f.trialTxn("UP-R-A", t0, trialEnd)
	require.NoError(t, creditInfo(t, f.buyer.ID, trialA))
	now := time.Now().Unix()
	require.NoError(t, creditInfo(t, f.buyer.ID, f.upgradeTxnB("UP-R-B", now)))
	before := f.userNow(t, f.buyer.ID)
	subBefore := f.subNow(t)
	a := upgradedTxnA(trialA, now)

	// (a) 入账门：可区分的哨兵，什么都不写。
	err := creditInfo(t, f.buyer.ID, a)
	require.ErrorIs(t, err, errAppleTxnUpgraded)
	assert.NotErrorIs(t, err, errAppleTxnRevoked, "不是退款")
	s := f.subNow(t)
	assert.Equal(t, appleMemberProductID, s.ProductID, "订阅行不被改回 A")
	assert.Equal(t, subBefore.ProviderLatestRef, s.ProviderLatestRef)

	// (b) 对账：Apple 对这条链报 Revoked 且附带的是被升级掉的 A。
	fakeAppleStatus(t, func(otx string) (*appleSubStatus, error) {
		x := *a
		return &appleSubStatus{status: appstore.SubscriptionStatus_Revoked, txn: &x}, nil
	})
	sub := f.subNow(t)
	_, err = reconcileAppleSubscription(context.Background(), &sub, time.Now().Unix())
	require.NoError(t, err)

	// (b') 对账：Active 状态下附带的交易是 A（升级通知尚未传播）——视为已处理，不报错。
	fakeAppleStatus(t, func(otx string) (*appleSubStatus, error) {
		x := *a
		return &appleSubStatus{status: appstore.SubscriptionStatus_Active, txn: &x}, nil
	})
	sub = f.subNow(t)
	_, err = reconcileAppleSubscription(context.Background(), &sub, time.Now().Unix())
	require.NoError(t, err)

	// (c) 退款收回直调（REFUND / REVOKE 通知也走这里）。
	out, err := applyAppleRefund(context.Background(), f.origTxn, a, appleRefundEvidence{SignedAt: a.RevocationDate, Source: "webhook"})
	require.NoError(t, err)
	assert.False(t, out != nil && out.Adopted, "不采纳为退款")

	// (d) verify 端点：升级掉的交易返回成功（不是"已退款"，也不是校验失败）。
	h.byID["UP-R-A"] = a
	gin.SetMode(gin.TestMode)
	r := gin.New()
	buyer := f.userNow(t, f.buyer.ID)
	r.Use(func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: buyer.ID, User: &buyer})
		c.Next()
	})
	r.POST("/api/iap/apple/verify", api_apple_iap_verify)
	body, _ := json.Marshal(map[string]string{"transactionId": "UP-R-A"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/iap/apple/verify", bytes.NewReader(body)))
	assert.Equal(t, 0, respCode(t, w), w.Body.String())

	u := f.userNow(t, f.buyer.ID)
	assert.Equal(t, before.ExpiredAt, u.ExpiredAt, "时长不减")
	assert.True(t, isTrue(u.IsFirstOrderDone), "首单资格不回退")
	assert.Equal(t, int64(1470), f.retailerBalance(t), "返佣不撤")
	s = f.subNow(t)
	assert.NotEqual(t, "revoked", s.Status)
	assert.Equal(t, subBefore.PaidThrough, s.PaidThrough)
	assert.Zero(t, appleRefundRows(t, f.origTxn), "无 AppleRefund 行")
	assert.False(t, alerts.has("[APPLE-REFUND]"), "不告警为退款")
}

// DID_CHANGE_RENEWAL_PREF 无 subtype（同级换档）/ DOWNGRADE（下个周期生效）：只是续订偏好，不入账。
func TestUpgrade_RenewalPrefNoSubtypeOrDowngrade_NoCredit(t *testing.T) {
	trialDB(t)
	h := newTrialWebhook(t)
	f := setupIAPOrderFixture(t, 30, 10)
	upgradePlanB(t)
	t0 := time.Now().Unix()
	h.byID["UP-N-A"] = f.trialTxn("UP-N-A", t0, t0+7*aDay)
	require.NoError(t, verifyAndGrantTransaction(context.Background(), f.buyer.ID, "UP-N-A"))

	for i, subtype := range []string{"", appstore.Subtype_DOWNGRADE} {
		id := "UP-N-B" + subtype
		h.byID[id] = f.upgradeTxnB(id, time.Now().Unix())
		require.Equal(t, 200, h.post(appstore.NotificationType_DID_CHANGE_RENEWAL_PREF, subtype, "uuid-up-n-"+string(rune('0'+i)), id))
		assert.Zero(t, creditCount(t, id), "subtype=%q 不入账", subtype)
	}
	var n int64
	require.NoError(t, db.Get().Model(&SubscriptionCredit{}).Where("original_transaction_id = ?", f.origTxn).Count(&n).Error)
	assert.Equal(t, int64(1), n, "只有试用那一行")
	assert.True(t, f.userNow(t, f.buyer.ID).InTrial)
	assert.True(t, f.subNow(t).IsTrial)
}
