package center

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// iapOrderFixture 搭建 分销商 + 邀请码 + 被邀请买家 + IAP plan 的完整链路，
// 用于验证 Apple IAP 入账时的建单 + 分销商返现。
type iapOrderFixture struct {
	retailer  User
	buyer     User
	code      InviteCode
	config    RetailerConfig
	plan      *Plan
	productID string
	origTxn   string
	token     string
	// env 是 credit() 送进 TransactionInfo 的 Apple 环境。默认 Production——建单/返现是
	// **只在生产交易上发生**的行为，用 Sandbox 建 fixture 等于全套测试都在验证一条被
	// creditAppleTransaction 的沙盒门提前截断的路径（这个 fixture 最初正是硬编码 "Sandbox"，
	// 于是没有任何测试发现沙盒交易会建出真订单）。沙盒行为由专门的用例翻转此字段来验。
	env string
}

// setupIAPOrderFixture 建全套记录并注册硬删除清理。
// firstPct/renewalPct 直接落到 RetailerConfig，避免依赖等级默认值。
func setupIAPOrderFixture(t *testing.T, firstPct, renewalPct int) *iapOrderFixture {
	t.Helper()
	uniq := time.Now().UnixNano()
	now := time.Now()
	f := &iapOrderFixture{
		productID: fmt.Sprintf("io.kaitu.test.iaporder.%d", uniq),
		origTxn:   fmt.Sprintf("OTX-iaporder-%d", uniq),
		env:       appstore.Environment_Production,
	}

	// 分销商（IsRetailer=true 是 processRetailerCashbackInTx 的准入条件）
	f.retailer = User{UUID: fmt.Sprintf("usr-rtl-%d", uniq), ExpiredAt: now.Unix(), IsRetailer: BoolPtr(true)}
	require.NoError(t, db.Get().Create(&f.retailer).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&f.retailer) })

	f.code = InviteCode{UserID: f.retailer.ID}
	require.NoError(t, db.Get().Create(&f.code).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&f.code) })

	f.config = RetailerConfig{
		UserID:            f.retailer.ID,
		Level:             2,
		FirstOrderPercent: firstPct,
		RenewalPercent:    renewalPct,
	}
	require.NoError(t, db.Get().Create(&f.config).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&f.config) })

	// 买家：经分销商邀请码注册
	f.buyer = User{UUID: fmt.Sprintf("usr-buyer-%d", uniq), ExpiredAt: now.Unix(), InvitedByCodeID: f.code.ID}
	require.NoError(t, db.Get().Create(&f.buyer).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&f.buyer) })

	// plan.Price = 4900 美分（$49），即分佣基数
	f.plan = &Plan{
		PID: fmt.Sprintf("tiap%d", uniq), Label: "IAP 年付", Price: 4900, OriginPrice: 4900,
		Month: 12, Tier: "basic", AppleProductID: f.productID, Brand: string(BrandKaitu),
	}
	require.NoError(t, db.Get().Create(f.plan).Error)
	t.Cleanup(func() { db.Get().Delete(f.plan) })

	f.token = deriveAppleAccountToken(f.buyer.UUID)

	t.Cleanup(func() {
		ids := []uint64{f.retailer.ID, f.buyer.ID}
		db.Get().Unscoped().Where("user_id IN ?", ids).Delete(&SubscriptionCredit{})
		db.Get().Unscoped().Where("user_id IN ?", ids).Delete(&Subscription{})
		db.Get().Unscoped().Where("user_id IN ?", ids).Delete(&UserProHistory{})
		db.Get().Unscoped().Where("user_id IN ?", ids).Delete(&Order{})
		// WalletChange 挂在 wallet_id 上（无 user_id 列），先摘钱包 id 再删变动流水。
		var walletIDs []uint64
		db.Get().Model(&Wallet{}).Where("user_id IN ?", ids).Pluck("id", &walletIDs)
		if len(walletIDs) > 0 {
			db.Get().Unscoped().Where("wallet_id IN ?", walletIDs).Delete(&WalletChange{})
		}
		db.Get().Unscoped().Where("user_id IN ?", ids).Delete(&Wallet{})
	})
	return f
}

// credit 触发一次 Apple 入账（首购或续订由 txnID/周期决定）。
func (f *iapOrderFixture) credit(t *testing.T, txnID string, purchaseSec, expiresSec int64) error {
	t.Helper()
	return db.Get().Transaction(func(tx *gorm.DB) error {
		return creditAppleTransaction(context.Background(), tx, f.buyer.ID, &appstore.TransactionInfo{
			OriginalTransactionId: f.origTxn,
			TransactionId:         txnID,
			ProductId:             f.productID,
			AppAccountToken:       f.token,
			Environment:           f.env,
			PurchaseDate:          purchaseSec * 1000,
			ExpiresDate:           expiresSec * 1000,
		})
	})
}

func (f *iapOrderFixture) orders(t *testing.T) []Order {
	t.Helper()
	var out []Order
	require.NoError(t, db.Get().Where("user_id = ?", f.buyer.ID).Order("id ASC").Find(&out).Error)
	return out
}

// retailerBalance 返回分销商钱包总余额。注意只能读 Balance——AvailableBalance/FrozenBalance
// 是 `gorm:"-"` 的实时计算字段，从 DB 读出来恒为 0。
func (f *iapOrderFixture) retailerBalance(t *testing.T) int64 {
	t.Helper()
	var w Wallet
	err := db.Get().Where(&Wallet{UserID: f.retailer.ID}).First(&w).Error
	if err == gorm.ErrRecordNotFound {
		return 0
	}
	require.NoError(t, err)
	return w.Balance
}

// 首购：建单 + 首单比例返现，且分佣基数是 plan.Price 而非 Apple 实付。
func TestCreditAppleTransaction_FirstPurchase_CreatesOrderAndCashback(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	day := int64(86400)
	t0 := time.Now().Unix()

	require.NoError(t, f.credit(t, "IAPO-T1", t0, t0+365*day))

	orders := f.orders(t)
	require.Len(t, orders, 1, "首购必须建一条订单")
	o := orders[0]

	assert.Equal(t, OrderChannelAppleIAP, o.Channel, "渠道标记")
	assert.Equal(t, uint64(4900), o.PayAmount, "PayAmount 必须是 plan 标价，不是 Apple 实付")
	assert.Equal(t, uint64(4900), o.OriginAmount)
	assert.Equal(t, f.plan.Label, o.Title)
	require.NotNil(t, o.IsPaid)
	assert.True(t, *o.IsPaid, "IAP 订单落库即已付")
	assert.NotEmpty(t, o.UUID)

	// Meta 必须带 plan，供后续 GetPlan / 邀请奖励逻辑读取
	gotPlan, err := o.GetPlan()
	require.NoError(t, err)
	require.NotNil(t, gotPlan)
	assert.Equal(t, 12, gotPlan.Month)

	// 分销商返现 = plan.Price × 首单比例 = 4900 × 30% = 1470
	assert.Equal(t, int64(1470), f.retailerBalance(t), "首单返现按 plan 价 × FirstOrderPercent")
}

// 续订：同样建单，但走续费比例（决策①：IAP 续订也给分销商分成）。
func TestCreditAppleTransaction_Renewal_CreatesOrderWithRenewalPercent(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	day := int64(86400)
	t0 := time.Now().Unix()

	require.NoError(t, f.credit(t, "IAPO-R1", t0, t0+365*day))
	require.Equal(t, int64(1470), f.retailerBalance(t), "前置：首单返现已发")

	// 一年后续订
	require.NoError(t, f.credit(t, "IAPO-R2", t0+365*day, t0+730*day))

	orders := f.orders(t)
	require.Len(t, orders, 2, "续订必须另建一条订单")
	assert.Equal(t, OrderChannelAppleIAP, orders[1].Channel)
	assert.Equal(t, uint64(4900), orders[1].PayAmount)

	// 续费返现 = 4900 × 10% = 490，累计 1470 + 490 = 1960
	assert.Equal(t, int64(1960), f.retailerBalance(t), "续订按 RenewalPercent 追加返现")
}

// 幂等：重放同一 transactionId 不得重复建单 / 重复返现。
func TestCreditAppleTransaction_Replay_NoDuplicateOrderOrCashback(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	day := int64(86400)
	t0 := time.Now().Unix()

	require.NoError(t, f.credit(t, "IAPO-D1", t0, t0+365*day))
	require.Len(t, f.orders(t), 1)
	require.Equal(t, int64(1470), f.retailerBalance(t))

	// webhook / StoreKit 重投同一笔
	require.NoError(t, f.credit(t, "IAPO-D1", t0, t0+365*day))

	assert.Len(t, f.orders(t), 1, "重放不得重复建单")
	assert.Equal(t, int64(1470), f.retailerBalance(t), "重放不得重复返现")
}

// 退款：撤销分销商返现 + 订单标记已退款。
func TestRevokeSubscription_RefundsCashbackAndMarksOrder(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	day := int64(86400)
	t0 := time.Now().Unix()

	require.NoError(t, f.credit(t, "IAPO-RF1", t0, t0+365*day))
	require.Equal(t, int64(1470), f.retailerBalance(t), "前置：返现已发")

	var sub Subscription
	require.NoError(t, db.Get().Where(&Subscription{
		Provider: "apple", ProviderSubscriptionID: f.origTxn,
	}).First(&sub).Error)

	require.NoError(t, revokeSubscription(context.Background(), &sub, "IAPO-RF1"))

	assert.Equal(t, int64(0), f.retailerBalance(t), "退款后返现必须被撤回")

	orders := f.orders(t)
	require.Len(t, orders, 1)
	require.NotNil(t, orders[0].IsRefunded)
	assert.True(t, *orders[0].IsRefunded, "订单必须标记已退款")
	assert.Equal(t, uint64(4900), orders[0].RefundAmount)
}

// 退款重投：Apple 重送 REFUND 通知不得把返现重复扣两次。
func TestRevokeSubscription_ReplayIsIdempotent(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	day := int64(86400)
	t0 := time.Now().Unix()

	require.NoError(t, f.credit(t, "IAPO-RF2", t0, t0+365*day))
	var sub Subscription
	require.NoError(t, db.Get().Where(&Subscription{
		Provider: "apple", ProviderSubscriptionID: f.origTxn,
	}).First(&sub).Error)

	require.NoError(t, revokeSubscription(context.Background(), &sub, "IAPO-RF2"))
	require.Equal(t, int64(0), f.retailerBalance(t))

	// Apple 重投同一通知
	require.NoError(t, revokeSubscription(context.Background(), &sub, "IAPO-RF2"))
	assert.Equal(t, int64(0), f.retailerBalance(t), "重投不得二次扣款（余额不得变负）")
}

// 空 productId 必须硬失败。GORM 结构体条件丢弃零值，不拦就退化成 `SELECT * FROM plans LIMIT 1`，
// 静默返回任意 plan，其 Price 会成为订单金额和分佣基数。
func TestPlanByAppleProductID_EmptyProductIDRejected(t *testing.T) {
	skipIfNoDB(t)
	plan, err := planByAppleProductID(context.Background(), db.Get(), "", BrandKaitu)
	require.Error(t, err, "空 productId 必须拒绝，不得回退成任意 plan")
	assert.Nil(t, plan)
	assert.Contains(t, err.Error(), "empty apple product id")
}

// 同一 productId 挂到多个 plan 时必须拒绝，不能按主键序默默取旧的低价行。
// 真实触发方式：改价时"插新行、留旧行"。
func TestPlanByAppleProductID_AmbiguousMappingRejected(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)

	dup := &Plan{
		PID: f.plan.PID + "dup", Label: "IAP 年付（改价）", Price: 5999, OriginPrice: 5999,
		Month: 12, Tier: "basic", AppleProductID: f.productID, Brand: string(BrandKaitu),
	}
	require.NoError(t, db.Get().Create(dup).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(dup) })

	plan, err := planByAppleProductID(context.Background(), db.Get(), f.productID, BrandKaitu)
	require.Error(t, err, "一对多映射必须拒绝，不得猜测用哪个价")
	assert.Nil(t, plan)
	assert.Contains(t, err.Error(), "multiple plans")
}

// 建单是非致命的：product 没映射到 plan 时，用户权益必须照常到账，只是不建单/不返现。
// 这条钉住的是设计取舍——Apple 已扣款，"付了钱没权益"比"内部账务缺一笔"严重得多。
func TestCreditAppleTransaction_UnmappedProduct_StillGrantsEntitlement(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	day := int64(86400)
	t0 := time.Now().Unix()

	// 把 plan 的 apple_product_id 摘掉，制造"交易的 productId 查不到 plan"
	require.NoError(t, db.Get().Model(&Plan{}).Where("id = ?", f.plan.ID).
		Update("apple_product_id", "").Error)

	require.NoError(t, f.credit(t, "IAPO-UNMAP1", t0, t0+365*day), "建单失败不得让入账失败")

	// 权益到账
	var u User
	require.NoError(t, db.Get().First(&u, f.buyer.ID).Error)
	assert.Greater(t, u.ExpiredAt, t0, "权益必须照常到账")

	// 去重账本写了（幂等仍然成立）
	var creditCount int64
	require.NoError(t, db.Get().Model(&SubscriptionCredit{}).
		Where("user_id = ? AND transaction_id = ?", f.buyer.ID, "IAPO-UNMAP1").
		Count(&creditCount).Error)
	assert.Equal(t, int64(1), creditCount)

	// 但没有订单、没有返现
	assert.Empty(t, f.orders(t), "查不到 plan 时不建单")
	assert.Equal(t, int64(0), f.retailerBalance(t), "不建单则无返现")
}

// 后台退款支持 IAP 订单：钱包打款、按 SubscriptionCredit 精确撤销权益、冲销分销返现。
//
// 关键在权益那一步——IAP 入账写的是 UserProHistory{type=apple_sub, reference_id=credit 行 id}，
// 按网页订单的 (type=purchase, reference_id=orderID) 口径反算恒为 0，会出现"钱退了权益没扣"。
// 本用例断言扣掉的秒数与 SubscriptionCredit.CreditedSeconds 完全相等，若 orderEntitlementSecondsInTx
// 退回按 VipPurchase 查，expiredAt 一秒都不会少，用例立刻红。
func TestProcessOrderRefund_AppleIAPOrder_RefundsWalletAndRevokesEntitlement(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	day := int64(86400)
	t0 := time.Now().Unix()

	require.NoError(t, f.credit(t, "IAPO-ARF1", t0, t0+365*day))
	orders := f.orders(t)
	require.Len(t, orders, 1)
	iapOrder := orders[0]
	require.Equal(t, OrderChannelAppleIAP, iapOrder.Channel)
	require.Equal(t, "IAPO-ARF1", iapOrder.AppleTransactionID)

	// 入账后的基准：权益到期时刻 + 该交易实际入账的秒数
	var beforeUser User
	require.NoError(t, db.Get().First(&beforeUser, f.buyer.ID).Error)
	var credit SubscriptionCredit
	require.NoError(t, db.Get().Where(&SubscriptionCredit{TransactionID: "IAPO-ARF1"}).First(&credit).Error)
	require.Greater(t, credit.CreditedSeconds, int64(0), "fixture 必须真的发了权益，否则本用例测不到东西")

	require.NoError(t, ProcessOrderRefund(context.Background(), iapOrder.ID, "客服补偿", 1))

	// 1. 权益按入账秒数精确回退
	var afterUser User
	require.NoError(t, db.Get().First(&afterUser, f.buyer.ID).Error)
	assert.Equal(t, beforeUser.ExpiredAt-credit.CreditedSeconds, afterUser.ExpiredAt,
		"必须按 SubscriptionCredit.CreditedSeconds 扣回，不是按 VipPurchase（那会一秒不扣）")

	// 2. 钱包收到 PayAmount
	var buyerWallet Wallet
	require.NoError(t, db.Get().Where(&Wallet{UserID: f.buyer.ID}).First(&buyerWallet).Error)
	assert.Equal(t, int64(iapOrder.PayAmount), buyerWallet.Balance)

	// 3. 分销商返现被冲销
	assert.Equal(t, int64(0), f.retailerBalance(t), "退款必须冲销分销商返现")

	// 4. 订单状态与反向审计记录
	after := f.orders(t)
	require.Len(t, after, 1)
	require.NotNil(t, after[0].IsRefunded)
	assert.True(t, *after[0].IsRefunded)
	assert.Equal(t, iapOrder.PayAmount, after[0].RefundAmount)

	var reverse UserProHistory
	require.NoError(t, db.Get().Where("user_id = ? AND type = ? AND reference_id = ?",
		f.buyer.ID, VipRefund, iapOrder.ID).First(&reverse).Error)
	assert.Equal(t, -int(credit.CreditedSeconds/86400), reverse.Days)
}

// 缺 apple_transaction_id 的 IAP 订单必须拒绝退款，而不是"查到 0 秒→只打钱不扣权益"。
func TestProcessOrderRefund_AppleIAPOrder_RejectsMissingTransactionID(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	day := int64(86400)
	t0 := time.Now().Unix()

	require.NoError(t, f.credit(t, "IAPO-ARF2", t0, t0+365*day))
	orders := f.orders(t)
	require.Len(t, orders, 1)
	iapOrder := orders[0]

	// 模拟被手工改过 / 早期数据形态的脏订单
	require.NoError(t, db.Get().Model(&Order{}).Where("id = ?", iapOrder.ID).
		Update("apple_transaction_id", "").Error)

	err := ProcessOrderRefund(context.Background(), iapOrder.ID, "客服补偿", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "缺少 apple_transaction_id")

	// 事务整体回滚：钱一分没打，订单没被标记
	var buyerWallet Wallet
	werr := db.Get().Where(&Wallet{UserID: f.buyer.ID}).First(&buyerWallet).Error
	if werr == nil {
		assert.Equal(t, int64(0), buyerWallet.Balance, "拒绝后不得打款")
	} else {
		assert.ErrorIs(t, werr, gorm.ErrRecordNotFound)
	}
	after := f.orders(t)
	require.Len(t, after, 1)
	if after[0].IsRefunded != nil {
		assert.False(t, *after[0].IsRefunded)
	}
}

// 沙盒交易：权益照发，但绝不建订单、绝不发返现。
//
// 生产事故（2026-08-10）就是这条路径漏了门——沙盒交易建出一笔 pay_amount=4900 的订单，
// 客服在后台点退款，差一步把 $49 可提现余额打给一个从没付过钱的账号。
func TestCreditAppleTransaction_SandboxCreditsEntitlementButCreatesNoOrder(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.env = appstore.Environment_Sandbox
	day := int64(86400)
	t0 := time.Now().Unix()

	var before User
	require.NoError(t, db.Get().First(&before, f.buyer.ID).Error)

	require.NoError(t, f.credit(t, "IAPO-SBX1", t0, t0+365*day), "沙盒交易的权益入账不得失败")

	// 权益照发——iOS 用沙盒账号做端到端测试时必须能看到 Pro 生效
	var after User
	require.NoError(t, db.Get().First(&after, f.buyer.ID).Error)
	assert.Greater(t, after.ExpiredAt, before.ExpiredAt, "沙盒交易的权益必须照常到账")

	// 去重账本照写（幂等仍然成立，重投的沙盒交易不会重复发权益）
	var creditCount int64
	require.NoError(t, db.Get().Model(&SubscriptionCredit{}).
		Where(&SubscriptionCredit{TransactionID: "IAPO-SBX1"}).Count(&creditCount).Error)
	assert.Equal(t, int64(1), creditCount)

	// 但订单与返现一个都不许有——订单是财务实体，沙盒交易用户实付为 0
	assert.Empty(t, f.orders(t), "沙盒交易不得建订单")
	assert.Equal(t, int64(0), f.retailerBalance(t), "沙盒交易不得发返现")
}

// Apple 退款后，若无其它有效付费订单，IsFirstOrderDone 必须翻回 false，
// 否则退款用户仍被 first_order 活动码当成老客拒绝。
func TestRevokeSubscription_ResetsIsFirstOrderDone(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	day := int64(86400)
	t0 := time.Now().Unix()

	require.NoError(t, f.credit(t, "IAPO-FOD1", t0, t0+365*day))

	var beforeUser User
	require.NoError(t, db.Get().First(&beforeUser, f.buyer.ID).Error)
	require.NotNil(t, beforeUser.IsFirstOrderDone)
	require.True(t, *beforeUser.IsFirstOrderDone, "前置：入账后应标记已完成首单")

	var sub Subscription
	require.NoError(t, db.Get().Where(&Subscription{
		Provider: "apple", ProviderSubscriptionID: f.origTxn,
	}).First(&sub).Error)
	require.NoError(t, revokeSubscription(context.Background(), &sub, "IAPO-FOD1"))

	var afterUser User
	require.NoError(t, db.Get().First(&afterUser, f.buyer.ID).Error)
	require.NotNil(t, afterUser.IsFirstOrderDone)
	assert.False(t, *afterUser.IsFirstOrderDone, "唯一付费订单退款后必须翻回新客")
}

// 无分销商的买家：照常建单，只是不产生返现。
func TestCreditAppleTransaction_NoRetailer_StillCreatesOrder(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	// 摘掉邀请关系
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.buyer.ID).
		Update("invited_by_code_id", 0).Error)

	day := int64(86400)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "IAPO-N1", t0, t0+365*day))

	require.Len(t, f.orders(t), 1, "无分销商也要建单（财务口径需要）")
	assert.Equal(t, int64(0), f.retailerBalance(t), "无邀请关系不返现")
}
