package center

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// setTestAppleBundleID 配好 kaitu 品牌的 bundleId(响亮失败契约要求非空),测试自理清理。
func setTestAppleBundleID(t *testing.T) {
	t.Helper()
	viper.Set("appstore.bundleId", "io.kaitu.test")
	t.Cleanup(func() { viper.Set("appstore.bundleId", "") })
}

// 对账(spec 2026-08-22):webhook 全丢时,cron 拉 Apple 最新交易灌回现有入账路径,
// status/period/ExpiredAt 三者收敛。
func TestReconcileSubscription_Apple_StalePeriodRecovered(t *testing.T) {
	skipIfNoDB(t)
	setTestAppleBundleID(t)
	uniq := time.Now().UnixNano()
	productID := fmt.Sprintf("io.kaitu.test.rec.%d", uniq)
	plan := &Plan{PID: fmt.Sprintf("trec%d", uniq), Label: "X", Price: 1000, OriginPrice: 1000, Month: 1, Tier: "basic", AppleProductID: productID}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Delete(plan) })

	user := CreateTestUser(t)
	now := time.Now().Unix()
	day := int64(86400)
	orig := fmt.Sprintf("OTXR-%d", uniq)
	token := deriveAppleAccountToken(user.UUID)

	// 首购入账(建立绑定),然后把用户与订阅行都做旧:period 已过、权益已过期。
	require.NoError(t, db.Get().Transaction(func(tx *gorm.DB) error {
		return creditAppleTransaction(context.Background(), tx, user.ID, &appstore.TransactionInfo{
			OriginalTransactionId: orig, TransactionId: "TR1", ProductId: productID,
			AppAccountToken: token, Environment: "Sandbox",
			PurchaseDate: (now - 31*day) * 1000, ExpiresDate: (now - day) * 1000,
		})
	}))
	t.Cleanup(func() {
		db.Get().Where("user_id = ?", user.ID).Delete(&SubscriptionCredit{})
		db.Get().Where("user_id = ?", user.ID).Delete(&Subscription{})
	})

	// Apple 侧真相:TR2 已续订到 now+29d,但我们从没收到通知。
	newEnd := now + 29*day
	orig2 := orig
	old := fetchAppleSubStatus
	fetchAppleSubStatus = func(ctx context.Context, bundleID, originalTxnID string) (*appleSubStatus, error) {
		require.Equal(t, orig2, originalTxnID)
		return &appleSubStatus{
			status: appstore.SubscriptionStatus_Active,
			txn: &appstore.TransactionInfo{
				OriginalTransactionId: orig2, TransactionId: "TR2", ProductId: productID,
				AppAccountToken: token, Environment: "Sandbox",
				PurchaseDate: (now - day) * 1000, ExpiresDate: newEnd * 1000,
			},
		}, nil
	}
	t.Cleanup(func() { fetchAppleSubStatus = old })

	var sub Subscription
	require.NoError(t, db.Get().Where("provider = ? AND provider_subscription_id = ?", "apple", orig).First(&sub).Error)
	changed, err := reconcileSubscription(context.Background(), &sub, now)
	require.NoError(t, err)
	assert.True(t, changed)

	require.NoError(t, db.Get().First(user, user.ID).Error)
	assert.GreaterOrEqual(t, user.ExpiredAt, newEnd, "对账后权益覆盖到 Apple 真实周期末")
	require.NoError(t, db.Get().First(&sub, sub.ID).Error)
	assert.Equal(t, newEnd, sub.CurrentPeriodEnd)
}

// Apple 报已过期 → 落 expired;revoked 行绝不被对账复活或改动。
func TestReconcileSubscription_Apple_ExpiredAndRevokedTerminal(t *testing.T) {
	skipIfNoDB(t)
	setTestAppleBundleID(t)
	user := CreateTestUser(t)
	now := time.Now().Unix()
	old := fetchAppleSubStatus
	fetchAppleSubStatus = func(ctx context.Context, bundleID, originalTxnID string) (*appleSubStatus, error) {
		return &appleSubStatus{status: appstore.SubscriptionStatus_Expired}, nil
	}
	t.Cleanup(func() { fetchAppleSubStatus = old })

	sub := &Subscription{UserID: user.ID, Provider: "apple",
		ProviderSubscriptionID: fmt.Sprintf("OTXE-%d", time.Now().UnixNano()),
		Status:                 "active", CurrentPeriodEnd: now - 86400}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&Subscription{}) })

	changed, err := reconcileSubscription(context.Background(), sub, now)
	require.NoError(t, err)
	assert.True(t, changed)
	var got Subscription
	require.NoError(t, db.Get().First(&got, sub.ID).Error)
	assert.Equal(t, "expired", got.Status)

	// revoked:对账绝不触碰(clawback 归退款流所有)。
	revoked := &Subscription{UserID: user.ID, Provider: "apple",
		ProviderSubscriptionID: fmt.Sprintf("OTXV-%d", time.Now().UnixNano()),
		Status:                 "revoked", CurrentPeriodEnd: now - 86400}
	require.NoError(t, db.Get().Create(revoked).Error)
	changed, err = reconcileSubscription(context.Background(), revoked, now)
	require.NoError(t, err)
	assert.False(t, changed)
}

// 纯状态迁移(active→billing_retry,txn=nil、周期不变)必须被计入 changed——
// 这是"漏 webhook 导致扣费重试却没人知道"的典型场景,只看 CurrentPeriodEnd 会漏计。
func TestReconcileSubscription_Apple_BillingRetryTransitionCountsAsChanged(t *testing.T) {
	skipIfNoDB(t)
	setTestAppleBundleID(t)
	user := CreateTestUser(t)
	now := time.Now().Unix()
	orig := fmt.Sprintf("OTXB-%d", time.Now().UnixNano())
	periodEnd := now + 3*86400

	old := fetchAppleSubStatus
	fetchAppleSubStatus = func(ctx context.Context, bundleID, originalTxnID string) (*appleSubStatus, error) {
		return &appleSubStatus{
			status: appstore.SubscriptionStatus_Active,
			renewal: &appstore.RenewalInfo{
				OriginalTransactionId:  originalTxnID,
				AutoRenewStatus:        appstore.AutoRenewStatus_On,
				IsInBillingRetryPeriod: true,
			},
		}, nil
	}
	t.Cleanup(func() { fetchAppleSubStatus = old })

	sub := &Subscription{UserID: user.ID, Provider: "apple",
		ProviderSubscriptionID: orig,
		Status:                 "active", AutoRenew: true, CurrentPeriodEnd: periodEnd}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&Subscription{}) })

	changed, err := reconcileSubscription(context.Background(), sub, now)
	require.NoError(t, err)
	assert.True(t, changed, "纯状态迁移(周期未变)也必须计入 changed")

	var got Subscription
	require.NoError(t, db.Get().First(&got, sub.ID).Error)
	assert.Equal(t, "billing_retry", got.Status)
	assert.Equal(t, periodEnd, got.CurrentPeriodEnd, "billing_retry 不改周期,只改状态")
}
