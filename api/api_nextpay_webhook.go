package center

import (
	"context"
	"fmt"
	"io"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/nextpay"
	"github.com/wordgate/qtoolkit/slack"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// alertNextpayPaymentAnomaly 是 NextPay 入账异常（金额错配 / 双付）的统一告警出口：
// error 日志 + Slack "alert" 频道，消息以 "[<tag>]" 开头。var 形态供测试替换。
var alertNextpayPaymentAnomaly = func(ctx context.Context, tag, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Errorf(ctx, "[%s] %s", tag, msg)
	if err := slack.Send("alert", "["+tag+"] "+msg); err != nil {
		log.Errorf(ctx, "failed to send slack alert [%s]: %v", tag, err)
	}
}

// api_nextpay_webhook 处理 NextPay 出站 webhook（kaitu 一次性购买入账通道）。
// NOTE: 与 api_wordgate_webhook 一样，这里用 HTTP 状态码而非 JSON code：
//   - 200 = 已处理/无需处理，别重投
//   - 400 = 签名/格式错，别重投
//   - 5xx = 暂时失败，NextPay 会重投（outbox 15 次 + cron 补投）
//
// 持久错配（品牌 / 金额）故意返回 5xx 让它反复告警——fail-loud 设计取舍，同 wordgate 哨兵。
func api_nextpay_webhook(c *gin.Context) {
	cfg := configNextpay(c)
	if !cfg.Ready() {
		log.Errorf(c, "[Webhook] nextpay webhook received but channel not configured")
		c.AbortWithStatus(503)
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Errorf(c, "[Webhook] failed to read nextpay body: %v", err)
		c.AbortWithStatus(400)
		return
	}
	evt, err := nextpay.ParseWebhook(body, c.GetHeader("X-NextPay-Signature"), cfg.WebhookSecret)
	if err != nil {
		log.Warnf(c, "[Webhook] nextpay signature/envelope rejected: %v", err)
		c.AbortWithStatus(400)
		return
	}
	log.Infof(c, "[Webhook] nextpay event: id=%s type=%s", evt.ID, evt.Type)

	switch evt.Type {
	case nextpay.WebhookOrderPaid:
		if err := handleNextpayOrderPaid(c, evt); err != nil {
			log.Errorf(c, "[Webhook] nextpay order.paid %s failed: %v", evt.ID, err)
			c.AbortWithStatus(500)
			return
		}
	case nextpay.WebhookOrderExpired, nextpay.WebhookOrderFailed:
		if d, err := evt.OrderData(); err == nil {
			log.Infof(c, "[Webhook] nextpay %s: order=%s nextpay=%s", evt.Type, d.ObjectID, d.OrderID)
		}
	default:
		log.Warnf(c, "[Webhook] nextpay event type %s ignored", evt.Type)
	}
	SuccessEmpty(c)
}

// handleNextpayOrderPaid 以 ObjectID(=orders.uuid) 关联本地订单并经 MarkOrderAsPaid 入账。
// 幂等：FOR UPDATE 序列化重投；已付订单直接 ack。
func handleNextpayOrderPaid(c *gin.Context, evt *nextpay.WebhookEvent) error {
	d, err := evt.OrderData()
	if err != nil {
		return err
	}
	log.Infof(c, "[Webhook] nextpay order paid: order=%s nextpay=%s amount=%d %s", d.ObjectID, d.OrderID, d.Amount, d.Currency)

	var provisionSubIDs []uint64
	err = withDeadlockRetry(c, 3, func(tx *gorm.DB) error {
		provisionSubIDs = nil // 死锁重试会重跑闭包，旧 ID 必须丢弃
		var order Order
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("User").Where(&Order{UUID: d.ObjectID}).First(&order).Error
		if err == gorm.ErrRecordNotFound {
			// 永久异常：ack，避免 outbox 15 次重投打空。
			log.Errorf(c, "[Webhook] nextpay order.paid for unknown order %s (nextpay=%s); acking", d.ObjectID, d.OrderID)
			return nil
		}
		if err != nil {
			return err
		}
		if order.User == nil {
			return fmt.Errorf("order %d has no associated user", order.ID)
		}
		if !Brand(order.User.Brand).Config().AllowsPayment(PayChannelNextpay) {
			alertPaymentBrandMismatch(c, "brand-mismatch webhook: order %d user brand %s does not allow nextpay", order.ID, order.User.Brand)
			return fmt.Errorf("brand mismatch: order %d user brand %s does not allow nextpay channel", order.ID, order.User.Brand)
		}
		if d.Amount != order.PayAmount || d.Currency != "usd" {
			alertNextpayPaymentAnomaly(c, "PAYMENT-AMOUNT-MISMATCH",
				"order %s expected %d usd, nextpay %s paid %d %s", order.UUID, order.PayAmount, d.OrderID, d.Amount, d.Currency)
			return fmt.Errorf("amount mismatch for order %s", order.UUID)
		}

		if order.IsPaid != nil && *order.IsPaid {
			if order.NextpayOrderID != d.OrderID {
				// 同一 Center 订单的两个 Stripe session 都被付了（耐久链接重建窗口）：人工退一笔。
				alertNextpayPaymentAnomaly(c, "DOUBLE-PAY",
					"order %s already paid via nextpay %s, now paid again via nextpay %s (%d usd) — refund one manually",
					order.UUID, order.NextpayOrderID, d.OrderID, d.Amount)
			} else {
				log.Debugf(c, "[Webhook] order %s already paid, ack redelivery", order.UUID)
			}
			return nil
		}

		if err := MarkOrderAsPaid(c, tx, &order, &provisionSubIDs); err != nil {
			return fmt.Errorf("mark order paid: %w", err)
		}
		if order.NextpayOrderID != d.OrderID {
			if err := tx.Model(&order).Update("nextpay_order_id", d.OrderID).Error; err != nil {
				return err
			}
		}
		log.Infof(c, "[Webhook] nextpay payment credited: order=%s nextpay=%s", order.UUID, d.OrderID)
		return nil
	})
	if err != nil {
		return err
	}
	// 事务已提交：现在才入队专属节点开通（照抄 wordgate handler 的 Bug #4 约束）。
	for _, subID := range provisionSubIDs {
		if err := enqueueProvision(c, subID); err != nil {
			log.Errorf(c, "[Webhook] failed to enqueue provision for sub %d (order committed; needs manual retry): %v", subID, err)
		}
		onPrivateNodeOrderOnboarding(c, subID)
	}
	return nil
}
