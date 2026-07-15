package center

import (
	"encoding/json"
	"io"

	"github.com/gin-gonic/gin"
	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// api_stripe_webhook 处理 Stripe webhook 事件（overleap 官网购买的唯一入账通道）。
//
// 与 wordgate/appstore webhook 一致，使用 HTTP 状态码表达 S2S 重试语义（非 JSON code）：
//   - 200 = 已处理（或确定无需处理），勿重试
//   - 4xx = 坏请求/验签失败，停止重试
//   - 5xx = 临时失败（或 fail-loud 的持久拒绝，如品牌错配），请重试
//
// 幂等双保险：
//  1. 事件级：stripe_webhook_events 按 event id 去重（check → process → record）。
//  2. 金额级：SubscriptionCredit UNIQUE(provider, transaction_id=invoice id) + 行锁，
//     并发重复投递也不可能双入账。
func api_stripe_webhook(c *gin.Context) {
	cfg := configStripe(c)
	if !cfg.Ready() {
		// 渠道未配置却收到 webhook：部署配置缺失，503 让 Stripe 重试直到配置补齐。
		log.Errorf(c, "[StripeWebhook] received webhook but stripe.* config missing")
		c.AbortWithStatus(503)
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		log.Errorf(c, "[StripeWebhook] read body: %v", err)
		c.AbortWithStatus(400)
		return
	}

	// 验签（必须）。IgnoreAPIVersionMismatch：Stripe 账号 API 版本升级不应打断入账；
	// 形态差异由 extractStripeInvoiceFacts 单点吸收。
	event, err := webhook.ConstructEventWithOptions(body, c.GetHeader("Stripe-Signature"), cfg.WebhookSecret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
	if err != nil {
		log.Warnf(c, "[StripeWebhook] signature verification failed: %v", err)
		c.AbortWithStatus(400)
		return
	}

	// 事件级幂等：同一 event id 只处理一次（Stripe 偶发重投）。
	var seen StripeWebhookEvent
	if err := db.Get().Where(&StripeWebhookEvent{EventID: event.ID}).First(&seen).Error; err == nil {
		log.Infof(c, "[StripeWebhook] duplicate event %s (%s), already processed", event.ID, event.Type)
		c.Status(200)
		return
	}

	log.Infof(c, "[StripeWebhook] type=%s id=%s livemode=%v", event.Type, event.ID, event.Livemode)

	if err := handleStripeEvent(c, &event); err != nil {
		log.Errorf(c, "[StripeWebhook] handle %s (%s) failed: %v", event.Type, event.ID, err)
		c.AbortWithStatus(500)
		return
	}

	// 处理成功后才落 dedup 行（失败必须让 Stripe 重试进入处理）。落表失败不 500：
	// 处理已成功，重放由金额级幂等兜底。
	if err := db.Get().Create(&StripeWebhookEvent{EventID: event.ID, Type: string(event.Type)}).Error; err != nil {
		log.Warnf(c, "[StripeWebhook] record event %s failed: %v", event.ID, err)
	}
	c.Status(200)
}

// handleStripeEvent 事件分发。返回 error → 500 → Stripe 重试。
func handleStripeEvent(c *gin.Context, event *stripe.Event) error {
	switch event.Type {
	case "invoice.paid":
		var inv stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return err
		}
		f, err := extractStripeInvoiceFacts(&inv)
		if err != nil {
			// 无订阅 parent 的 invoice（一次性账单等）不属本渠道语义——忽略勿重试。
			log.Warnf(c, "[StripeWebhook] invoice.paid not creditable, ignoring: %v", err)
			return nil
		}
		return withDeadlockRetry(c, 3, func(tx *gorm.DB) error {
			return creditStripeInvoice(c, tx, f)
		})

	case "customer.subscription.updated":
		var s stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &s); err != nil {
			return err
		}
		return applyStripeSubscriptionUpdate(c, &s)

	case "customer.subscription.deleted":
		var s stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &s); err != nil {
			return err
		}
		return markStripeSubscriptionDeleted(c, &s)

	case "charge.refunded":
		return recordStripeRefundAlert(c, event.Data.Raw)

	case "charge.dispute.created":
		return recordStripeDisputeAlert(c, event.Data.Raw)

	case "checkout.session.completed":
		// 绑定与入账统一发生在 invoice.paid（订阅 metadata 自足），此处仅记录。
		log.Infof(c, "[StripeWebhook] checkout.session.completed received (credit happens on invoice.paid)")
		return nil

	default:
		log.Infof(c, "[StripeWebhook] unhandled type=%s", event.Type)
		return nil
	}
}
