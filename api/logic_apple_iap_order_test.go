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
		Month: 12, Tier: "basic", AppleProductID: f.productID,
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
