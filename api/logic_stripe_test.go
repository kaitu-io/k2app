package center

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v82"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
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

	// 非订阅 invoice = 唯一可安全忽略的失败 → 必须带 errNotSubscriptionInvoice sentinel。
	t.Run("NoSubscriptionParent_SentinelError", func(t *testing.T) {
		inv := mk()
		inv.Parent = nil
		_, err := extractStripeInvoiceFacts(inv)
		require.Error(t, err)
		assert.True(t, errors.Is(err, errNotSubscriptionInvoice))
		assert.Contains(t, err.Error(), "in_test_1") // 包裹保留 invoice id
	})

	// 以下都是"形态解析失败"：钱已收、事实读不出 → 绝不可被当成"忽略勿重试"。
	t.Run("NoLinePeriod_NotSentinel", func(t *testing.T) {
		inv := mk()
		inv.Lines = &stripe.InvoiceLineItemList{}
		_, err := extractStripeInvoiceFacts(inv)
		require.Error(t, err)
		assert.False(t, errors.Is(err, errNotSubscriptionInvoice))
	})

	t.Run("NilInvoice_NotSentinel", func(t *testing.T) {
		_, err := extractStripeInvoiceFacts(nil)
		require.Error(t, err)
		assert.False(t, errors.Is(err, errNotSubscriptionInvoice))
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

// ===================== creditStripeInvoice e2e（真 MySQL） =====================

// stripeUniq 本文件测试数据唯一后缀。
func stripeUniq() string { return strconv.FormatInt(time.Now().UnixNano(), 36) }

// createStripeTestUser 造一个指定品牌的测试用户，t.Cleanup 硬删（含关联账目行）。
func createStripeTestUser(t *testing.T, brand Brand) *User {
	t.Helper()
	u := &User{UUID: generateId("stripetest-user"), Brand: string(brand)}
	require.NoError(t, db.Get().Create(u).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&Subscription{})
		db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&SubscriptionCredit{})
		db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&UserProHistory{})
		db.Get().Unscoped().Delete(u)
	})
	return u
}

// createStripeTestPlan 造一个 overleap + stripe price 的测试套餐。
func createStripeTestPlan(t *testing.T) *Plan {
	t.Helper()
	p := &Plan{
		PID: "olst_" + stripeUniq(), Label: "Overleap Pro Monthly (test)",
		Price: 999, OriginPrice: 999, Month: 1, Tier: TierBasic,
		IsActive: BoolPtr(true), Brand: string(BrandOverleap),
		StripePriceID: "price_test_" + stripeUniq(),
	}
	require.NoError(t, db.Get().Create(p).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(p) })
	return p
}

// mkStripeFacts 构造入账事实。
func mkStripeFacts(u *User, p *Plan, invoiceID, subID string, start, end int64) *stripeInvoiceFacts {
	return &stripeInvoiceFacts{
		InvoiceID: invoiceID, SubscriptionID: subID, CustomerID: "cus_" + subID,
		UserUUID: u.UUID, PlanPID: p.PID, PeriodStart: start, PeriodEnd: end, Livemode: false,
	}
}

func creditInTx(f *stripeInvoiceFacts) error {
	return db.Get().Transaction(func(tx *gorm.DB) error {
		return creditStripeInvoice(context.Background(), tx, f)
	})
}

func TestCreditStripeInvoice(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	now := time.Now().Unix()
	month := int64(31 * 86400)

	t.Run("FirstInvoice_BindsAndCredits", func(t *testing.T) {
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		subID := "sub_" + stripeUniq()

		require.NoError(t, creditInTx(mkStripeFacts(u, p, "in_"+stripeUniq(), subID, now, now+month)))

		var got User
		require.NoError(t, db.Get().First(&got, u.ID).Error)
		assert.InDelta(t, now+month, got.ExpiredAt, 5) // 首购从 now 起一个周期
		assert.True(t, *got.IsFirstOrderDone)
		assert.Equal(t, TierBasic, got.Tier)

		var sub Subscription
		require.NoError(t, db.Get().Where(&Subscription{Provider: "stripe", ProviderSubscriptionID: subID}).First(&sub).Error)
		assert.Equal(t, u.ID, sub.UserID)
		assert.Equal(t, p.StripePriceID, sub.ProductID)
		assert.Equal(t, "cus_"+subID, sub.ProviderCustomerID)
		assert.Equal(t, "active", sub.Status)
		assert.Equal(t, now+month, sub.CurrentPeriodEnd)
		assert.Equal(t, "sandbox", sub.Environment)
	})

	t.Run("SameInvoiceReplay_NoDoubleCredit", func(t *testing.T) {
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		f := mkStripeFacts(u, p, "in_"+stripeUniq(), "sub_"+stripeUniq(), now, now+month)

		require.NoError(t, creditInTx(f))
		var before User
		require.NoError(t, db.Get().First(&before, u.ID).Error)

		require.NoError(t, creditInTx(f)) // 重放：幂等成功，不加时长
		var after User
		require.NoError(t, db.Get().First(&after, u.ID).Error)
		assert.Equal(t, before.ExpiredAt, after.ExpiredAt)

		var creditCount int64
		db.Get().Model(&SubscriptionCredit{}).Where("user_id = ?", u.ID).Count(&creditCount)
		assert.Equal(t, int64(1), creditCount)
	})

	t.Run("RenewalInvoice_AdditiveCredit", func(t *testing.T) {
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		subID := "sub_" + stripeUniq()

		require.NoError(t, creditInTx(mkStripeFacts(u, p, "in_"+stripeUniq(), subID, now, now+month)))
		require.NoError(t, creditInTx(mkStripeFacts(u, p, "in_"+stripeUniq(), subID, now+month, now+2*month)))

		var got User
		require.NoError(t, db.Get().First(&got, u.ID).Error)
		assert.InDelta(t, now+2*month, got.ExpiredAt, 5)

		var sub Subscription
		require.NoError(t, db.Get().Where(&Subscription{Provider: "stripe", ProviderSubscriptionID: subID}).First(&sub).Error)
		assert.Equal(t, now+2*month, sub.CurrentPeriodEnd)

		var hist []UserProHistory
		db.Get().Where("user_id = ? AND type = ?", u.ID, VipStripeSub).Find(&hist)
		assert.Len(t, hist, 2)
	})

	t.Run("MissingUserUUID_OnFirstBind_Refused", func(t *testing.T) {
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		f := mkStripeFacts(u, p, "in_"+stripeUniq(), "sub_"+stripeUniq(), now, now+month)
		f.UserUUID = ""
		assert.Error(t, creditInTx(f))
	})

	t.Run("MetadataPointsToDifferentUser_AfterBind_Refused", func(t *testing.T) {
		u1 := createStripeTestUser(t, BrandOverleap)
		u2 := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		subID := "sub_" + stripeUniq()
		require.NoError(t, creditInTx(mkStripeFacts(u1, p, "in_"+stripeUniq(), subID, now, now+month)))

		f2 := mkStripeFacts(u2, p, "in_"+stripeUniq(), subID, now+month, now+2*month)
		assert.Error(t, creditInTx(f2)) // 绝不 re-bind
	})

	t.Run("KaituUser_BrandSentinel_RefusesAndAlerts", func(t *testing.T) {
		orig := alertPaymentBrandMismatch
		t.Cleanup(func() { alertPaymentBrandMismatch = orig })
		var alerted string
		alertPaymentBrandMismatch = func(ctx context.Context, format string, args ...any) {
			alerted = fmt.Sprintf(format, args...)
		}

		u := createStripeTestUser(t, BrandKaitu)
		p := createStripeTestPlan(t)
		err := creditInTx(mkStripeFacts(u, p, "in_"+stripeUniq(), "sub_"+stripeUniq(), now, now+month))
		assert.Error(t, err)
		assert.Contains(t, alerted, "stripe")

		var got User
		require.NoError(t, db.Get().First(&got, u.ID).Error)
		assert.Equal(t, int64(0), got.ExpiredAt) // 绝不静默入账
	})

	t.Run("UnknownPlanPID_Refused", func(t *testing.T) {
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		f := mkStripeFacts(u, p, "in_"+stripeUniq(), "sub_"+stripeUniq(), now, now+month)
		f.PlanPID = "nonexistent_" + stripeUniq()
		assert.Error(t, creditInTx(f))
	})

	t.Run("DeactivatedPlan_RenewalStillCredits", func(t *testing.T) {
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		subID := "sub_" + stripeUniq()
		require.NoError(t, creditInTx(mkStripeFacts(u, p, "in_"+stripeUniq(), subID, now, now+month)))
		// ops 下架 plan 后续费仍须入账（planByPIDForCredit 不看 is_active）
		require.NoError(t, db.Get().Model(p).Update("is_active", false).Error)
		require.NoError(t, creditInTx(mkStripeFacts(u, p, "in_"+stripeUniq(), subID, now+month, now+2*month)))
	})
}

func TestGetActiveSubscriptions_StripeProvider(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	u := createStripeTestUser(t, BrandOverleap)
	p := createStripeTestPlan(t)
	sub := &Subscription{UserID: u.ID, Provider: "stripe",
		ProviderSubscriptionID: "sub_gas_" + stripeUniq(), ProviderCustomerID: "cus_gas_" + stripeUniq(),
		ProductID: p.StripePriceID, CurrentPeriodEnd: time.Now().Unix() + 86400,
		AutoRenew: true, Status: "active"}
	require.NoError(t, db.Get().Create(sub).Error)

	subs := GetActiveSubscriptions(u.ID)
	require.Len(t, subs, 1)
	assert.Equal(t, "stripe", subs[0].Provider)
	assert.Equal(t, p.Tier, subs[0].Tier) // stripe 行走 planByStripePriceID，不再误走 apple 查找
	assert.Equal(t, "stripe_portal", subs[0].Manage.Kind)
	assert.True(t, subs[0].AutoRenew)
}

func TestUsersWithLiveAutoRenew(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	uAuto := createStripeTestUser(t, BrandOverleap)  // 活跃自动续订 → 应跳过提醒
	uNoSub := createStripeTestUser(t, BrandOverleap) // 无订阅 → 照发
	uStale := createStripeTestUser(t, BrandOverleap) // 陈旧 active 行(period 已过) → 照发

	mkSub := func(u *User, periodEnd int64, autoRenew bool) {
		s := &Subscription{UserID: u.ID, Provider: "stripe",
			ProviderSubscriptionID: "sub_ar_" + stripeUniq(), ProductID: "price_x",
			CurrentPeriodEnd: periodEnd, AutoRenew: autoRenew, Status: "active"}
		require.NoError(t, db.Get().Create(s).Error)
	}
	mkSub(uAuto, time.Now().Unix()+30*86400, true)
	mkSub(uStale, time.Now().Unix()-86400, true)

	got := usersWithLiveAutoRenew([]uint64{uAuto.ID, uNoSub.ID, uStale.ID})
	assert.True(t, got[uAuto.ID])
	assert.False(t, got[uNoSub.ID])
	assert.False(t, got[uStale.ID]) // isSubscriptionLive 拒绝陈旧 active 行
}
