package center

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v82/webhook"
	db "github.com/wordgate/qtoolkit/db"
)

const stripeTestWebhookSecret = "whsec_test_secret_for_unit_tests"

// stripeSigHeader 用官方 ComputeSignature 生成合法 Stripe-Signature 头。
func stripeSigHeader(payload []byte) string {
	ts := time.Now()
	sig := webhook.ComputeSignature(ts, payload, stripeTestWebhookSecret)
	return fmt.Sprintf("t=%d,v1=%s", ts.Unix(), hex.EncodeToString(sig))
}

// stripeWebhookRouter 只挂 webhook 路由（对标 brand_isolation 的手工路由模式）。
func stripeWebhookRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/webhook/stripe", BrandResolver(), api_stripe_webhook)
	return r
}

func postStripeWebhook(t *testing.T, r *gin.Engine, payload []byte, sig string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", strings.NewReader(string(payload)))
	req.Header.Set("Stripe-Signature", sig)
	r.ServeHTTP(w, req)
	return w
}

// invoicePaidPayload 构造 basil 形态的 invoice.paid 事件 JSON。
func invoicePaidPayload(eventID, invoiceID, subID, userUUID, planPID string, start, end int64) []byte {
	return []byte(fmt.Sprintf(`{
		"id": %q, "object": "event", "type": "invoice.paid", "livemode": false,
		"data": {"object": {
			"id": %q, "object": "invoice", "livemode": false,
			"customer": "cus_%s",
			"parent": {"subscription_details": {"subscription": %q,
				"metadata": {"user_uuid": %q, "plan_pid": %q, "brand": "overleap"}}},
			"lines": {"object": "list", "data": [
				{"id": "il_1", "object": "line_item", "period": {"start": %d, "end": %d}}
			]}
		}}
	}`, eventID, invoiceID, subID, subID, userUUID, planPID, start, end))
}

// invoicePaidNoSubParentPayload 构造真·一次性账单（无 parent.subscription_details）。
func invoicePaidNoSubParentPayload(eventID, invoiceID string, start, end int64) []byte {
	return []byte(fmt.Sprintf(`{
		"id": %q, "object": "event", "type": "invoice.paid", "livemode": false,
		"data": {"object": {
			"id": %q, "object": "invoice", "livemode": false,
			"customer": "cus_oneoff",
			"lines": {"object": "list", "data": [
				{"id": "il_1", "object": "line_item", "period": {"start": %d, "end": %d}}
			]}
		}}
	}`, eventID, invoiceID, start, end))
}

// invoicePaidBrokenShapePayload 构造形态损坏的订阅 invoice：有 subscription parent
// （= 真订阅账单，钱已收），但 line 缺 period → 事实读不出。模拟 API 版本漂移。
func invoicePaidBrokenShapePayload(eventID, invoiceID, subID, userUUID, planPID string) []byte {
	return []byte(fmt.Sprintf(`{
		"id": %q, "object": "event", "type": "invoice.paid", "livemode": false,
		"data": {"object": {
			"id": %q, "object": "invoice", "livemode": false,
			"customer": "cus_%s",
			"parent": {"subscription_details": {"subscription": %q,
				"metadata": {"user_uuid": %q, "plan_pid": %q, "brand": "overleap"}}},
			"lines": {"object": "list", "data": []}
		}}
	}`, eventID, invoiceID, subID, subID, userUUID, planPID))
}

func subscriptionEventPayload(eventID, eventType, subID string, cancelAtPeriodEnd bool, status string) []byte {
	return []byte(fmt.Sprintf(`{
		"id": %q, "object": "event", "type": %q, "livemode": false,
		"data": {"object": {
			"id": %q, "object": "subscription",
			"cancel_at_period_end": %t, "status": %q
		}}
	}`, eventID, eventType, subID, cancelAtPeriodEnd, status))
}

func cleanupStripeEvents(t *testing.T, eventIDs ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, id := range eventIDs {
			db.Get().Unscoped().Where("event_id = ?", id).Delete(&StripeWebhookEvent{})
		}
	})
}

func TestStripeWebhook(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	setStripeTestConfig(t, "sk_test_x", stripeTestWebhookSecret)
	r := stripeWebhookRouter()

	now := time.Now().Unix()
	month := int64(31 * 86400)

	t.Run("BadSignature_400", func(t *testing.T) {
		payload := invoicePaidPayload("evt_bad_"+stripeUniq(), "in_x", "sub_x", "user-x", "plan-x", now, now+month)
		w := postStripeWebhook(t, r, payload, "t=1,v1=deadbeef")
		assert.Equal(t, 400, w.Code)
	})

	t.Run("MissingConfig_503", func(t *testing.T) {
		viper.Set("stripe.webhook_secret", "")
		t.Cleanup(func() { viper.Set("stripe.webhook_secret", stripeTestWebhookSecret) })
		payload := invoicePaidPayload("evt_nc_"+stripeUniq(), "in_x", "sub_x", "user-x", "plan-x", now, now+month)
		w := postStripeWebhook(t, r, payload, stripeSigHeader(payload))
		assert.Equal(t, 503, w.Code)
	})

	t.Run("InvoicePaid_FullCreditFlow", func(t *testing.T) {
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		evtID := "evt_" + stripeUniq()
		cleanupStripeEvents(t, evtID)
		payload := invoicePaidPayload(evtID, "in_"+stripeUniq(), "sub_"+stripeUniq(), u.UUID, p.PID, now, now+month)

		w := postStripeWebhook(t, r, payload, stripeSigHeader(payload))
		require.Equal(t, 200, w.Code)

		var got User
		require.NoError(t, db.Get().First(&got, u.ID).Error)
		assert.InDelta(t, now+month, got.ExpiredAt, 5)

		var evt StripeWebhookEvent
		assert.NoError(t, db.Get().Where("event_id = ?", evtID).First(&evt).Error)
	})

	t.Run("DuplicateEventID_SkippedIdempotent", func(t *testing.T) {
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		evtID := "evt_" + stripeUniq()
		cleanupStripeEvents(t, evtID)
		payload := invoicePaidPayload(evtID, "in_"+stripeUniq(), "sub_"+stripeUniq(), u.UUID, p.PID, now, now+month)

		require.Equal(t, 200, postStripeWebhook(t, r, payload, stripeSigHeader(payload)).Code)
		var before User
		require.NoError(t, db.Get().First(&before, u.ID).Error)

		require.Equal(t, 200, postStripeWebhook(t, r, payload, stripeSigHeader(payload)).Code)
		var after User
		require.NoError(t, db.Get().First(&after, u.ID).Error)
		assert.Equal(t, before.ExpiredAt, after.ExpiredAt)
	})

	t.Run("KaituUserMetadata_BrandMismatch_500_Alerted", func(t *testing.T) {
		orig := alertPaymentBrandMismatch
		t.Cleanup(func() { alertPaymentBrandMismatch = orig })
		var alerted string
		alertPaymentBrandMismatch = func(ctx context.Context, format string, args ...any) {
			alerted = fmt.Sprintf(format, args...)
		}
		u := createStripeTestUser(t, BrandKaitu)
		p := createStripeTestPlan(t)
		evtID := "evt_" + stripeUniq()
		cleanupStripeEvents(t, evtID)
		payload := invoicePaidPayload(evtID, "in_"+stripeUniq(), "sub_"+stripeUniq(), u.UUID, p.PID, now, now+month)

		w := postStripeWebhook(t, r, payload, stripeSigHeader(payload))
		assert.Equal(t, 500, w.Code) // fail-loud：Stripe 会重试 → 重复告警是设计取舍
		assert.Contains(t, alerted, "stripe")
		// 事件不落 dedup 表（未处理成功），重试仍会进入处理
		var count int64
		db.Get().Model(&StripeWebhookEvent{}).Where("event_id = ?", evtID).Count(&count)
		assert.Equal(t, int64(0), count)
	})

	// 形态解析失败 ≠ 可忽略：钱已收却读不出事实 → 必须告警 + 500 重投，
	// 且绝不可落 dedup 行（落了就永不重投 = 静默吞钱）。
	t.Run("InvoicePaid_BrokenShape_500_Alerted_NoDedupRow", func(t *testing.T) {
		orig := alertStripeCredit
		t.Cleanup(func() { alertStripeCredit = orig })
		var alerted string
		alertStripeCredit = func(ctx context.Context, format string, args ...any) {
			alerted = fmt.Sprintf(format, args...)
		}
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		evtID := "evt_" + stripeUniq()
		cleanupStripeEvents(t, evtID)
		payload := invoicePaidBrokenShapePayload(evtID, "in_"+stripeUniq(), "sub_"+stripeUniq(), u.UUID, p.PID)

		w := postStripeWebhook(t, r, payload, stripeSigHeader(payload))
		assert.Equal(t, 500, w.Code) // fail-loud：Stripe 重投 → 修 extract 后可补账
		assert.Contains(t, alerted, "NOT credited")

		// 事件不落 dedup 表 —— 重投仍会进入处理（这才是"钱没丢"的保证）
		var count int64
		db.Get().Model(&StripeWebhookEvent{}).Where("event_id = ?", evtID).Count(&count)
		assert.Equal(t, int64(0), count)
	})

	// 真·一次性账单：不属订阅入账语义 → 200 忽略勿重试（唯一可忽略的 extract 失败）。
	t.Run("InvoicePaid_NotSubscriptionInvoice_200Ignored", func(t *testing.T) {
		orig := alertStripeCredit
		t.Cleanup(func() { alertStripeCredit = orig })
		alerted := false
		alertStripeCredit = func(ctx context.Context, format string, args ...any) { alerted = true }
		evtID := "evt_" + stripeUniq()
		cleanupStripeEvents(t, evtID)
		payload := invoicePaidNoSubParentPayload(evtID, "in_"+stripeUniq(), now, now+month)

		w := postStripeWebhook(t, r, payload, stripeSigHeader(payload))
		assert.Equal(t, 200, w.Code)
		assert.False(t, alerted, "一次性账单是正常业务，不该告警")
	})

	t.Run("SubscriptionUpdated_CancelAtPeriodEnd_SyncsAutoRenew", func(t *testing.T) {
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		subID := "sub_" + stripeUniq()
		// 先入账绑定
		evt1 := "evt_" + stripeUniq()
		cleanupStripeEvents(t, evt1)
		payload := invoicePaidPayload(evt1, "in_"+stripeUniq(), subID, u.UUID, p.PID, now, now+month)
		require.Equal(t, 200, postStripeWebhook(t, r, payload, stripeSigHeader(payload)).Code)

		// 用户在 portal 取消自动续订
		evt2 := "evt_" + stripeUniq()
		cleanupStripeEvents(t, evt2)
		upd := subscriptionEventPayload(evt2, "customer.subscription.updated", subID, true, "active")
		require.Equal(t, 200, postStripeWebhook(t, r, upd, stripeSigHeader(upd)).Code)

		var sub Subscription
		require.NoError(t, db.Get().Where(&Subscription{Provider: "stripe", ProviderSubscriptionID: subID}).First(&sub).Error)
		assert.False(t, sub.AutoRenew)
		assert.Equal(t, "active", sub.Status) // 权益不缩短：取消后仍享有到周期结束
	})

	t.Run("SubscriptionUpdated_UnknownSub_200Skip", func(t *testing.T) {
		evtID := "evt_" + stripeUniq()
		cleanupStripeEvents(t, evtID)
		upd := subscriptionEventPayload(evtID, "customer.subscription.updated", "sub_unknown_"+stripeUniq(), true, "active")
		assert.Equal(t, 200, postStripeWebhook(t, r, upd, stripeSigHeader(upd)).Code)
	})

	t.Run("SubscriptionDeleted_MarksExpired", func(t *testing.T) {
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		subID := "sub_" + stripeUniq()
		evt1 := "evt_" + stripeUniq()
		cleanupStripeEvents(t, evt1)
		payload := invoicePaidPayload(evt1, "in_"+stripeUniq(), subID, u.UUID, p.PID, now, now+month)
		require.Equal(t, 200, postStripeWebhook(t, r, payload, stripeSigHeader(payload)).Code)

		evt2 := "evt_" + stripeUniq()
		cleanupStripeEvents(t, evt2)
		del := subscriptionEventPayload(evt2, "customer.subscription.deleted", subID, true, "canceled")
		require.Equal(t, 200, postStripeWebhook(t, r, del, stripeSigHeader(del)).Code)

		var sub Subscription
		require.NoError(t, db.Get().Where(&Subscription{Provider: "stripe", ProviderSubscriptionID: subID}).First(&sub).Error)
		assert.Equal(t, "expired", sub.Status)
		assert.False(t, sub.AutoRenew)
		// 权益不回收：expired_at 保持到周期结束（自然过期）
		var got User
		require.NoError(t, db.Get().First(&got, u.ID).Error)
		assert.InDelta(t, now+month, got.ExpiredAt, 5)
	})

	t.Run("ChargeRefunded_PassiveAlert_200", func(t *testing.T) {
		evtID := "evt_" + stripeUniq()
		cleanupStripeEvents(t, evtID)
		refund := []byte(fmt.Sprintf(`{
			"id": %q, "object": "event", "type": "charge.refunded", "livemode": false,
			"data": {"object": {"id": "ch_test_1", "object": "charge",
				"amount": 999, "amount_refunded": 999, "currency": "usd", "customer": "cus_test_ref"}}
		}`, evtID))
		assert.Equal(t, 200, postStripeWebhook(t, r, refund, stripeSigHeader(refund)).Code)
	})

	t.Run("UnhandledEventType_200", func(t *testing.T) {
		evtID := "evt_" + stripeUniq()
		cleanupStripeEvents(t, evtID)
		payload := []byte(fmt.Sprintf(`{"id": %q, "object": "event", "type": "customer.created", "livemode": false,
			"data": {"object": {"id": "cus_x", "object": "customer"}}}`, evtID))
		assert.Equal(t, 200, postStripeWebhook(t, r, payload, stripeSigHeader(payload)).Code)
	})
}
