package center

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

func TestClassifyAppleTxn(t *testing.T) {
	cases := []struct {
		name      string
		offerType int32
		price     int32
		want      appleTxnClass
	}{
		{"paid", 0, 4990, appleTxnPaid},
		{"paid, price field missing", 0, 0, appleTxnPaid},
		{"intro free trial", 1, 0, appleTxnFreeTrial},
		{"intro paid offer", 1, 990, appleTxnOtherOffer},
		{"promotional offer", 2, 0, appleTxnOtherOffer},
		{"offer code", 3, 0, appleTxnOtherOffer},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyAppleTxn(&appstore.TransactionInfo{OfferType: c.offerType, Price: c.price})
			if got != c.want {
				t.Fatalf("classifyAppleTxn(offerType=%d, price=%d) = %d, want %d", c.offerType, c.price, got, c.want)
			}
		})
	}
}

// ---------- creditAppleTransaction 试用分流（spec 2026-10-19 §3.3）----------

// trialInfo 构造一笔 Apple 交易；offerType/price 决定分类（1/0 = 免费试用，0/4900 = 付费）。
func (f *iapOrderFixture) offerTxn(id string, p, e int64, offerType, price int32) *appstore.TransactionInfo {
	return &appstore.TransactionInfo{
		BundleId:              "io.kaitu.test",
		OriginalTransactionId: f.origTxn, TransactionId: id, ProductId: f.productID, AppAccountToken: f.token,
		InAppOwnershipType: appstore.OwnershipType_PURCHASED, Environment: f.env,
		PurchaseDate: p * 1000, ExpiresDate: e * 1000, OfferType: offerType, Price: price,
	}
}

func (f *iapOrderFixture) trialTxn(id string, p, e int64) *appstore.TransactionInfo {
	return f.offerTxn(id, p, e, appleOfferTypeIntroductory, 0)
}

func (f *iapOrderFixture) paidTxn(id string, p, e int64) *appstore.TransactionInfo {
	return f.offerTxn(id, p, e, 0, 4900)
}

func creditInfo(t *testing.T, userID uint64, info *appstore.TransactionInfo) error {
	t.Helper()
	return db.Get().Transaction(func(tx *gorm.DB) error {
		return creditAppleTransaction(context.Background(), tx, userID, info)
	})
}

func creditRowOf(t *testing.T, txnID string) SubscriptionCredit {
	t.Helper()
	var c SubscriptionCredit
	require.NoError(t, db.Get().Where("provider = ? AND transaction_id = ?", SubscriptionProviderApple, txnID).First(&c).Error)
	return c
}

func ordersForTxn(t *testing.T, txnID string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Get().Model(&Order{}).Where("apple_transaction_id = ?", txnID).Count(&n).Error)
	return n
}

func inviteGrants(t *testing.T, inviteeID uint64) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Get().Model(&InviteRewardGrant{}).Where("invitee_user_id = ?", inviteeID).Count(&n).Error)
	return n
}

// trialDB 跳过无库环境，并在本测试二进制里迁移一次（本任务新增的列在旧库上还不存在）。
var trialMigrateOnce sync.Once

func trialDB(t *testing.T) {
	t.Helper()
	skipIfNoDB(t)
	var err error
	trialMigrateOnce.Do(func() { err = Migrate() })
	require.NoError(t, err)
}

func isTrue(b *bool) bool { return b != nil && *b }

// 免费试用：给到 expiresDate，但不建单、不返佣、不发邀请奖励、不消耗首单、不推进 paid_through。
func TestTrial_StartGrantsAccessWithoutPayment(t *testing.T) {
	trialDB(t)
	f := setupIAPOrderFixture(t, 30, 10) // buyer 带分销商邀请码
	t0 := time.Now().Unix()

	require.NoError(t, creditInfo(t, f.buyer.ID, f.trialTxn("TR-S1", t0, t0+7*aDay)))

	u := f.userNow(t, f.buyer.ID)
	assert.InDelta(t, t0+7*aDay, u.ExpiredAt, float64(60), "试用照常给到 expiresDate")
	assert.Equal(t, "trial", creditRowOf(t, "TR-S1").Kind)
	assert.Zero(t, ordersForTxn(t, "TR-S1"), "试用不建单")
	assert.Zero(t, f.retailerBalance(t), "试用不返佣")
	assert.False(t, isTrue(u.IsFirstOrderDone), "试用不消耗首单资格")
	assert.Equal(t, "basic", u.Tier, "试用期设备上限按套餐 Tier 生效")
	assert.True(t, u.InTrial)
	assert.Zero(t, inviteGrants(t, f.buyer.ID), "试用不发邀请奖励")
	s := f.subNow(t)
	assert.Zero(t, s.PaidThrough, "试用不推进 paid_through")
	assert.True(t, s.IsTrial)
	assert.Equal(t, t0+7*aDay, s.TrialEndsAt)
}

// 试用后第一笔付费 = 首购：建单 + 首单比例返佣 + 邀请奖励一次 + 消耗首单 + 清试用标志。
func TestTrial_ConvertsToFirstPurchase(t *testing.T) {
	trialDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()
	trialEnd := t0 + 7*aDay
	rewardDays := int64(configInvite(context.Background()).PurchaseRewardDays)

	require.NoError(t, creditInfo(t, f.buyer.ID, f.trialTxn("TR-C1", t0, trialEnd)))
	require.NoError(t, creditInfo(t, f.buyer.ID, f.paidTxn("TR-C2", trialEnd, trialEnd+365*aDay)))

	assert.Equal(t, "purchase", creditRowOf(t, "TR-C2").Kind, "试用后首笔付费记 purchase，不是 renewal")
	assert.Equal(t, int64(1), ordersForTxn(t, "TR-C2"))
	assert.Len(t, f.orders(t), 1, "整条链只有付费那笔建单")
	assert.Equal(t, int64(1470), f.retailerBalance(t), "按 FirstOrderPercent 返佣：4900×30%")
	assert.Equal(t, int64(1), inviteGrants(t, f.buyer.ID), "邀请奖励在转付费时发一次")
	u := f.userNow(t, f.buyer.ID)
	assert.True(t, isTrue(u.IsFirstOrderDone))
	assert.False(t, u.InTrial)
	s := f.subNow(t)
	assert.False(t, s.IsTrial)
	assert.Positive(t, s.PaidThrough)
	assert.InDelta(t, trialEnd+365*aDay+rewardDays*aDay, u.ExpiredAt, float64(60), "试用结束 + 一年 (+邀请奖励)")
}

// 试用失效一个月后再订：只给一个周期，不给"一年 + 整个空档"。
func TestTrial_LapseThenResubscribeGetsOnePeriod(t *testing.T) {
	trialDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()

	require.NoError(t, creditInfo(t, f.buyer.ID, f.trialTxn("TR-L1", t0-37*aDay, t0-30*aDay)))
	// 模拟试用早已结束：权益停在 30 天前。
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.buyer.ID).Update("expired_at", t0-30*aDay).Error)

	require.NoError(t, creditInfo(t, f.buyer.ID, f.paidTxn("TR-L2", t0, t0+365*aDay)))

	u := f.userNow(t, f.buyer.ID)
	assert.InDelta(t, t0+365*aDay, u.ExpiredAt, float64(60), "只给一个周期")
	assert.Equal(t, "purchase", creditRowOf(t, "TR-L2").Kind)
	assert.True(t, isTrue(u.IsFirstOrderDone))
}

// 从没付过费的试用链：另一个账号（带自己的 token）付费时改绑过去，按首购处理。
func TestTrial_RebindNeverPaidChain(t *testing.T) {
	trialDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	userB := CreateTestUser(t)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("user_id = ?", userB.ID).Delete(&SubscriptionCredit{})
		db.Get().Unscoped().Where("user_id = ?", userB.ID).Delete(&Subscription{})
		db.Get().Unscoped().Where("user_id = ?", userB.ID).Delete(&UserProHistory{})
		db.Get().Unscoped().Where("user_id = ?", userB.ID).Delete(&Order{})
	})

	require.NoError(t, creditInfo(t, f.buyer.ID, f.trialTxn("TR-RB1", t0-14*aDay, t0-7*aDay)))
	require.True(t, f.userNow(t, f.buyer.ID).InTrial)

	paid := f.paidTxn("TR-RB2", t0, t0+365*aDay)
	// 没带 B 的 token（为空/是 A 的）→ 仍按 INV9 拒绝。
	paid.AppAccountToken = ""
	require.ErrorContains(t, creditInfo(t, userB.ID, paid), "already bound")
	paid.AppAccountToken = f.token
	require.ErrorContains(t, creditInfo(t, userB.ID, paid), "already bound")

	paid.AppAccountToken = deriveAppleAccountToken(userB.UUID)
	require.NoError(t, creditInfo(t, userB.ID, paid))

	s := f.subNow(t)
	assert.Equal(t, userB.ID, s.UserID, "订阅链改绑到 B")
	assert.False(t, s.IsTrial)
	assert.Equal(t, "purchase", creditRowOf(t, "TR-RB2").Kind)
	assert.Equal(t, userB.ID, creditRowOf(t, "TR-RB2").UserID)
	assert.Equal(t, int64(1), ordersForTxn(t, "TR-RB2"))
	b := f.userNow(t, userB.ID)
	assert.True(t, isTrue(b.IsFirstOrderDone))
	assert.InDelta(t, t0+365*aDay, b.ExpiredAt, float64(60))
	assert.False(t, f.userNow(t, f.buyer.ID).InTrial, "原账号试用标志清掉")
}

// 有过付费的链：换账号仍拒绝（INV9）。
func TestTrial_RebindRejectedForPaidChain(t *testing.T) {
	trialDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	userB := CreateTestUser(t)

	require.NoError(t, creditInfo(t, f.buyer.ID, f.paidTxn("TR-RP1", t0, t0+365*aDay)))
	paid := f.paidTxn("TR-RP2", t0+365*aDay, t0+730*aDay)
	paid.AppAccountToken = deriveAppleAccountToken(userB.UUID)
	require.ErrorContains(t, creditInfo(t, userB.ID, paid), "already bound")
	assert.Equal(t, f.buyer.ID, f.subNow(t).UserID)

	// 试用链已转付费后同样拒绝。
	f2 := setupIAPOrderFixture(t, 30, 10)
	f2.noInvite(t)
	require.NoError(t, creditInfo(t, f2.buyer.ID, f2.trialTxn("TR-RP3", t0, t0+7*aDay)))
	require.NoError(t, creditInfo(t, f2.buyer.ID, f2.paidTxn("TR-RP4", t0+7*aDay, t0+372*aDay)))
	p := f2.paidTxn("TR-RP5", t0+372*aDay, t0+737*aDay)
	p.AppAccountToken = deriveAppleAccountToken(userB.UUID)
	require.ErrorContains(t, creditInfo(t, userB.ID, p), "already bound")
}

// trialSnapshot 是一次试用入账后可比较的 DB 状态（时间相关字段取相对 t0 的值）。
type trialSnapshot struct {
	ExpiredAt   int64 // 绝对时刻：两次运行间有秒差，用 assertSameState 按容差比较
	PaidThrough int64 // 同上
	InTrial     bool
	FirstDone   bool
	IsTrial     bool
	TrialEnds   int64
	Credits     int64
	Kind        string
	Orders      int64
	Balance     int64
}

// assertSameState 时间字段按 60s 容差比较，其余字段严格相等。
func assertSameState(t *testing.T, want, got trialSnapshot, msg string) {
	t.Helper()
	assert.InDelta(t, want.ExpiredAt, got.ExpiredAt, 60, msg+" (expired_at)")
	assert.InDelta(t, want.PaidThrough, got.PaidThrough, 60, msg+" (paid_through)")
	assert.Equal(t, want.PaidThrough == 0, got.PaidThrough == 0, msg+" (paid_through set)")
	want.ExpiredAt, got.ExpiredAt, want.PaidThrough, got.PaidThrough = 0, 0, 0, 0
	assert.Equal(t, want, got, msg)
}

func (f *iapOrderFixture) snapshot(t *testing.T, t0 int64, txnID string) trialSnapshot {
	t.Helper()
	u := f.userNow(t, f.buyer.ID)
	s := f.subNow(t)
	var n int64
	require.NoError(t, db.Get().Model(&SubscriptionCredit{}).Where("original_transaction_id = ?", f.origTxn).Count(&n).Error)
	return trialSnapshot{
		ExpiredAt: u.ExpiredAt, PaidThrough: s.PaidThrough,
		InTrial: u.InTrial, FirstDone: isTrue(u.IsFirstOrderDone),
		IsTrial: s.IsTrial, TrialEnds: s.TrialEndsAt - t0,
		Credits: n, Kind: creditRowOf(t, txnID).Kind, Orders: int64(len(f.orders(t))), Balance: f.retailerBalance(t),
	}
}

// Review Focus 1：试用的 SUBSCRIBED webhook 先于 verify 到达（未绑定 → 跳过）再 verify，
// 结果与 verify 先到、webhook 后到完全相同。
func TestTrial_WebhookBeforeVerify(t *testing.T) {
	trialDB(t)
	setTestAppleBundleID(t)
	rootPEM, leafKey, leafDER, intDER := makeTestChain(t)
	old := appleRootCAPEM
	appleRootCAPEM = rootPEM
	t.Cleanup(func() { appleRootCAPEM = old })
	t0 := time.Now().Unix()

	byID := map[string]*appstore.TransactionInfo{}
	fakeAppleTxn(t, func(id string) (*appstore.TransactionInfo, error) {
		x := *byID[id]
		return &x, nil
	})
	postSubscribed := func(f *iapOrderFixture, txnID string) int {
		tj, _ := json.Marshal(byID[txnID])
		inner := signJWS(t, leafKey, leafDER, intDER, string(tj))
		outer, _ := json.Marshal(map[string]any{
			"notificationType": "SUBSCRIBED", "subtype": "INITIAL_BUY",
			"notificationUUID": fmt.Sprintf("uuid-sub-%s", txnID), "version": "2.0", "signedDate": time.Now().UnixMilli(),
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

	// A：webhook 先到（被跳过），再 verify。
	fa := setupIAPOrderFixture(t, 30, 10)
	byID["TR-WA"] = fa.trialTxn("TR-WA", t0, t0+7*aDay)
	require.Equal(t, 200, postSubscribed(fa, "TR-WA"))
	var n int64
	db.Get().Model(&Subscription{}).Where("provider_subscription_id = ?", fa.origTxn).Count(&n)
	require.Zero(t, n, "未绑定的 webhook 什么都不写")
	require.NoError(t, verifyAndGrantTransaction(context.Background(), fa.buyer.ID, "TR-WA"))

	// B：verify 先到，webhook 后到（重放同一笔）。
	fb := setupIAPOrderFixture(t, 30, 10)
	byID["TR-WB"] = fb.trialTxn("TR-WB", t0, t0+7*aDay)
	require.NoError(t, verifyAndGrantTransaction(context.Background(), fb.buyer.ID, "TR-WB"))
	require.Equal(t, 200, postSubscribed(fb, "TR-WB"))

	a, b := fa.snapshot(t, t0, "TR-WA"), fb.snapshot(t, t0, "TR-WB")
	assertSameState(t, a, b, "两种到达顺序结果相同")
	assert.True(t, a.IsTrial)
	assert.True(t, a.InTrial)
	assert.Equal(t, "trial", a.Kind)
	assert.Zero(t, a.Orders)
}

// Review Focus 2：同一试用交易重复投递，不重复给时长、不翻转标志；转付费后再重放试用交易也不把标志翻回来。
func TestTrial_Idempotent(t *testing.T) {
	trialDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()

	trial := f.trialTxn("TR-I1", t0, t0+7*aDay)
	require.NoError(t, creditInfo(t, f.buyer.ID, trial))
	before := f.snapshot(t, t0, "TR-I1")
	require.NoError(t, creditInfo(t, f.buyer.ID, trial))
	assertSameState(t, before, f.snapshot(t, t0, "TR-I1"), "重放试用交易无任何变化")
	assert.Equal(t, int64(1), before.Credits)

	require.NoError(t, creditInfo(t, f.buyer.ID, f.paidTxn("TR-I2", t0+7*aDay, t0+372*aDay)))
	afterPaid := f.snapshot(t, t0, "TR-I2")
	require.NoError(t, creditInfo(t, f.buyer.ID, trial)) // 对账 worker 迟到补投
	assertSameState(t, afterPaid, f.snapshot(t, t0, "TR-I2"), "转付费后重放试用交易不翻回 is_trial / in_trial")
	assert.False(t, afterPaid.IsTrial)
	assert.False(t, afterPaid.InTrial)
}

// Review Focus 3：已付过费的用户换 Apple ID 开试用——in_trial 保持 false，但这笔 $0 仍不建单不返佣。
func TestTrial_PaidUserStartsAppleTrial(t *testing.T) {
	trialDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.buyer.ID).Update("is_first_order_done", true).Error)
	t0 := time.Now().Unix()

	require.NoError(t, creditInfo(t, f.buyer.ID, f.trialTxn("TR-P1", t0, t0+7*aDay)))

	u := f.userNow(t, f.buyer.ID)
	assert.False(t, u.InTrial, "付过费的用户不进试用线路隔离")
	assert.True(t, isTrue(u.IsFirstOrderDone))
	assert.Zero(t, ordersForTxn(t, "TR-P1"))
	assert.Zero(t, f.retailerBalance(t))
	assert.Equal(t, "trial", creditRowOf(t, "TR-P1").Kind)
	assert.True(t, f.subNow(t).IsTrial, "订阅链本身仍是试用链")
}

// Review Focus 4：TestFlight 沙盒试用——不建单；试用标志照常设置。
func TestTrial_Sandbox(t *testing.T) {
	trialDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.env = appstore.Environment_Sandbox
	t0 := time.Now().Unix()

	require.NoError(t, creditInfo(t, f.buyer.ID, f.trialTxn("TR-SB1", t0, t0+7*aDay)))
	u := f.userNow(t, f.buyer.ID)
	assert.True(t, u.InTrial)
	assert.True(t, f.subNow(t).IsTrial)
	assert.Equal(t, "trial", creditRowOf(t, "TR-SB1").Kind)
	assert.Empty(t, f.orders(t))

	// 沙盒转付费：仍不建单、不消耗首单，但试用标志清掉。
	require.NoError(t, creditInfo(t, f.buyer.ID, f.paidTxn("TR-SB2", t0+7*aDay, t0+372*aDay)))
	u = f.userNow(t, f.buyer.ID)
	assert.False(t, u.InTrial)
	assert.False(t, f.subNow(t).IsTrial)
	assert.Equal(t, "purchase", creditRowOf(t, "TR-SB2").Kind)
	assert.Empty(t, f.orders(t))
	assert.False(t, isTrue(u.IsFirstOrderDone))
}

// 未支持的优惠（promotional 等）：按现状入账（与普通付费一致）+ 告警。
func TestTrial_OtherOfferProcessedAsBefore(t *testing.T) {
	trialDB(t)
	var alerts []string
	oldAlert := alertPaymentAnomaly
	alertPaymentAnomaly = func(_ context.Context, tag, format string, args ...any) {
		alerts = append(alerts, tag+" "+fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { alertPaymentAnomaly = oldAlert })
	t0 := time.Now().Unix()

	fo := setupIAPOrderFixture(t, 30, 10)
	require.NoError(t, creditInfo(t, fo.buyer.ID, fo.offerTxn("TR-O1", t0, t0+365*aDay, 2, 0)))
	fp := setupIAPOrderFixture(t, 30, 10)
	require.NoError(t, creditInfo(t, fp.buyer.ID, fp.paidTxn("TR-O2", t0, t0+365*aDay)))

	o, p := fo.snapshot(t, t0, "TR-O1"), fp.snapshot(t, t0, "TR-O2")
	assertSameState(t, p, o, "OtherOffer 入账结果与普通付费一致")
	assert.Equal(t, "purchase", o.Kind)
	assert.Equal(t, int64(1), o.Orders)
	assert.Equal(t, int64(1470), o.Balance)
	require.Len(t, alerts, 1, "只有 OtherOffer 告警")
	assert.Contains(t, alerts[0], "unsupported apple offer")
	assert.Contains(t, alerts[0], "TR-O1")
}

// storefront 写进 credit 行（试用与付费都写）。
func TestTrial_StorefrontRecorded(t *testing.T) {
	trialDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()
	trial := f.trialTxn("TR-SF1", t0, t0+7*aDay)
	trial.StoreFront = "CHN"
	require.NoError(t, creditInfo(t, f.buyer.ID, trial))
	paid := f.paidTxn("TR-SF2", t0+7*aDay, t0+372*aDay)
	paid.StoreFront = "USA"
	require.NoError(t, creditInfo(t, f.buyer.ID, paid))
	assert.Equal(t, "CHN", creditRowOf(t, "TR-SF1").Storefront)
	assert.Equal(t, "USA", creditRowOf(t, "TR-SF2").Storefront)
}
