package center

import (
	"context"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
