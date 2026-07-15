package center

import (
	"context"
	"fmt"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v82"
	db "github.com/wordgate/qtoolkit/db"
)

// setStripeTestConfig 设置 stripe viper 键并在测试结束时清空。
// 注意 viper 是全局单例——必须 Cleanup 归零，避免污染同包其它测试。
func setStripeTestConfig(t *testing.T, secretKey, webhookSecret string) {
	t.Helper()
	viper.Set("stripe.secret_key", secretKey)
	viper.Set("stripe.webhook_secret", webhookSecret)
	t.Cleanup(func() {
		viper.Set("stripe.secret_key", "")
		viper.Set("stripe.webhook_secret", "")
		viper.Set("stripe.success_url", "")
		viper.Set("stripe.cancel_url", "")
		viper.Set("stripe.portal_return_url", "")
	})
}

func TestConfigStripe(t *testing.T) {
	ctx := context.Background()

	t.Run("MissingConfig_NotReady", func(t *testing.T) {
		setStripeTestConfig(t, "", "")
		cfg := configStripe(ctx)
		assert.False(t, cfg.Ready())
	})

	t.Run("SecretOnly_NotReady", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_xxx", "")
		assert.False(t, configStripe(ctx).Ready())
	})

	t.Run("FullConfig_Ready_WithBrandURLDefaults", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_xxx", "whsec_xxx")
		cfg := configStripe(ctx)
		assert.True(t, cfg.Ready())
		// URL 缺省回退 overleap 品牌 BaseURL（viper 旧键恒 kaitu-only 的既定规则：
		// stripe 是 overleap 专属渠道，默认 URL 必须来自品牌注册表而非 viper 旧键）
		assert.Equal(t, "https://www.overleap.io/account?checkout=success", cfg.SuccessURL)
		assert.Equal(t, "https://www.overleap.io/pricing?checkout=cancelled", cfg.CancelURL)
		assert.Equal(t, "https://www.overleap.io/account", cfg.PortalReturnURL)
	})

	t.Run("ExplicitURLsWin", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_xxx", "whsec_xxx")
		viper.Set("stripe.success_url", "https://www.overleap.io/thanks")
		assert.Equal(t, "https://www.overleap.io/thanks", configStripe(ctx).SuccessURL)
	})
}

// 真 MySQL：验证 Phase 6 stripe 相关列/表在 AutoMigrate 后存在（对标 brand_schema_e2e_test.go）
func TestStripeSchemaMigration(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	m := db.Get().Migrator()
	assert.True(t, m.HasColumn(&Plan{}, "stripe_price_id"), "plans missing stripe_price_id")
	assert.True(t, m.HasColumn(&Subscription{}, "provider_customer_id"), "subscriptions missing provider_customer_id")
	assert.True(t, m.HasTable(&StripeWebhookEvent{}), "stripe_webhook_events table missing")
}

func TestStripeSubStatus(t *testing.T) {
	assert.Equal(t, "active", stripeSubStatus(stripe.SubscriptionStatusActive))
	assert.Equal(t, "active", stripeSubStatus(stripe.SubscriptionStatusTrialing))
	assert.Equal(t, "billing_retry", stripeSubStatus(stripe.SubscriptionStatusPastDue))
	assert.Equal(t, "expired", stripeSubStatus(stripe.SubscriptionStatusCanceled))
	assert.Equal(t, "expired", stripeSubStatus(stripe.SubscriptionStatusUnpaid))
	assert.Equal(t, "expired", stripeSubStatus(stripe.SubscriptionStatusIncompleteExpired))
	assert.Equal(t, "expired", stripeSubStatus(stripe.SubscriptionStatusPaused))
	// incomplete = 首扣未完成，我们的订阅行只在 invoice.paid 后诞生，此状态无行可改 → 不改
	assert.Equal(t, "", stripeSubStatus(stripe.SubscriptionStatusIncomplete))
	assert.Equal(t, "", stripeSubStatus(stripe.SubscriptionStatus("weird_future_status")))
}

func TestExtractStripeInvoiceFacts(t *testing.T) {
	mk := func() *stripe.Invoice {
		return &stripe.Invoice{
			ID:       "in_test_1",
			Livemode: false,
			Customer: &stripe.Customer{ID: "cus_test_1"},
			Parent: &stripe.InvoiceParent{
				SubscriptionDetails: &stripe.InvoiceParentSubscriptionDetails{
					Subscription: &stripe.Subscription{ID: "sub_test_1"},
					Metadata: map[string]string{
						"user_uuid": "user-abc", "plan_pid": "ol_pro_month", "brand": "overleap",
					},
				},
			},
			Lines: &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{
				{Period: &stripe.Period{Start: 1000, End: 2000}},
			}},
		}
	}

	t.Run("HappyPath", func(t *testing.T) {
		f, err := extractStripeInvoiceFacts(mk())
		require.NoError(t, err)
		assert.Equal(t, "in_test_1", f.InvoiceID)
		assert.Equal(t, "sub_test_1", f.SubscriptionID)
		assert.Equal(t, "cus_test_1", f.CustomerID)
		assert.Equal(t, "user-abc", f.UserUUID)
		assert.Equal(t, "ol_pro_month", f.PlanPID)
		assert.Equal(t, int64(1000), f.PeriodStart)
		assert.Equal(t, int64(2000), f.PeriodEnd)
		assert.False(t, f.Livemode)
	})

	t.Run("MultiLine_TakesLatestPeriod", func(t *testing.T) {
		inv := mk()
		inv.Lines.Data = append(inv.Lines.Data, &stripe.InvoiceLineItem{Period: &stripe.Period{Start: 2000, End: 3000}})
		f, err := extractStripeInvoiceFacts(inv)
		require.NoError(t, err)
		assert.Equal(t, int64(3000), f.PeriodEnd)
		assert.Equal(t, int64(2000), f.PeriodStart)
	})

	t.Run("NoSubscriptionParent_Error", func(t *testing.T) {
		inv := mk()
		inv.Parent = nil
		_, err := extractStripeInvoiceFacts(inv)
		assert.Error(t, err)
	})

	t.Run("NoLinePeriod_Error", func(t *testing.T) {
		inv := mk()
		inv.Lines = &stripe.InvoiceLineItemList{}
		_, err := extractStripeInvoiceFacts(inv)
		assert.Error(t, err)
	})
}

func TestAlertPaymentBrandMismatch_Replaceable(t *testing.T) {
	orig := alertPaymentBrandMismatch
	t.Cleanup(func() { alertPaymentBrandMismatch = orig })

	var captured string
	alertPaymentBrandMismatch = func(ctx context.Context, format string, args ...any) {
		captured = fmt.Sprintf(format, args...)
	}
	alertPaymentBrandMismatch(context.Background(), "user %d brand %s", uint64(7), "kaitu")
	assert.Equal(t, "user 7 brand kaitu", captured)
}
