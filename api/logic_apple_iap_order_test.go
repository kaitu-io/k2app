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
			Environment:           "Sandbox",
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

// 后台退款必须拒绝 IAP 订单：Apple 已原路退款，再走 ProcessOrderRefund 会往用户钱包
// 二次打款（可提现，真实资损），且授权天数按 VipPurchase 反算恒为 0，权益也扣不掉。
func TestProcessOrderRefund_RejectsAppleIAPOrder(t *testing.T) {
	skipIfNoDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	day := int64(86400)
	t0 := time.Now().Unix()

	require.NoError(t, f.credit(t, "IAPO-ARJ1", t0, t0+365*day))
	orders := f.orders(t)
	require.Len(t, orders, 1)
	iapOrder := orders[0]
	require.Equal(t, OrderChannelAppleIAP, iapOrder.Channel)

	err := ProcessOrderRefund(context.Background(), iapOrder.ID, "管理员误操作", 1)
	require.Error(t, err, "IAP 订单必须被拒绝")
	assert.Contains(t, err.Error(), "Apple 内购订单不支持后台退款")

	// 事务整体回滚：订单未被标记退款
	after := f.orders(t)
	require.Len(t, after, 1)
	if after[0].IsRefunded != nil {
		assert.False(t, *after[0].IsRefunded, "拒绝后订单不得被标记为已退款")
	}

	// 关键：买家钱包不得凭空多出 PayAmount
	var buyerWallet Wallet
	werr := db.Get().Where(&Wallet{UserID: f.buyer.ID}).First(&buyerWallet).Error
	if werr == nil {
		assert.Equal(t, int64(0), buyerWallet.Balance, "买家钱包不得被二次打款")
	} else {
		assert.ErrorIs(t, werr, gorm.ErrRecordNotFound, "要么没钱包，要么余额为 0")
	}
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
