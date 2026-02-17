package center

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/wordgate-sdk"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// withDeadlockRetry 带死锁重试的事务执行
// MySQL 检测到死锁时会立即返回 Error 1213，此函数自动重试
func withDeadlockRetry(ctx context.Context, maxRetries int, fn func(tx *gorm.DB) error) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		err := db.Get().Transaction(fn)
		if err == nil {
			return nil
		}

		// 检查是否是死锁错误 (MySQL error 1213)
		if isDeadlockError(err) {
			log.Warnf(ctx, "[Transaction] deadlock detected, retry %d/%d: %v", i+1, maxRetries, err)
			lastErr = err
			time.Sleep(time.Duration(10*(i+1)) * time.Millisecond) // 递增延迟
			continue
		}

		// 非死锁错误，直接返回
		return err
	}
	return fmt.Errorf("transaction failed after %d retries due to deadlock: %v", maxRetries, lastErr)
}

// isDeadlockError 检查是否是死锁错误
func isDeadlockError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "1213") ||
		strings.Contains(errStr, "Deadlock") ||
		strings.Contains(errStr, "deadlock")
}

// api_wordgate_webhook handles Wordgate payment webhook events.
// NOTE: This handler intentionally uses HTTP status codes (c.AbortWithStatus) instead of
// the standard JSON error response (Error(c, code, msg)). Payment webhooks are server-to-server:
// - HTTP 200 = processed successfully, don't retry
// - HTTP 4xx = bad request, stop retrying
// - HTTP 5xx = temporary failure, please retry
// Returning HTTP 200 with JSON error code would cause the payment provider to stop retrying
// on transient failures, potentially losing payments.
func api_wordgate_webhook(c *gin.Context) {
	// 读取原始请求体用于签名验证
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Errorf(c, "[Webhook] failed to read request body: %v", err)
		c.AbortWithStatus(int(ErrorInvalidArgument))
		return
	}

	// 验证 webhook 签名
	signature := c.GetHeader("X-Webhook-Signature")
	if signature == "" {
		log.Warnf(c, "[Webhook] missing webhook signature")
		c.AbortWithStatus(int(ErrorInvalidArgument))
		return
	}

	// 获取 webhook secret 进行签名验证
	cfg := configWordgate(c)
	if err := wordgate.VerifySignature(signature, body, cfg.WebhookSecret, 300); err != nil {
		log.Errorf(c, "[Webhook] webhook signature verification failed: %v", err)
		c.AbortWithStatus(int(ErrorInvalidArgument))
		return
	}

	// 解析 webhook 事件
	var webhookEvent wordgate.WebhookEventData
	if err := json.Unmarshal(body, &webhookEvent); err != nil {
		log.Errorf(c, "[Webhook] failed to parse webhook event: %v", err)
		c.AbortWithStatus(int(ErrorInvalidArgument))
		return
	}

	log.Infof(c, "[Webhook] received wordgate webhook event: type=%s, app_id=%d, timestamp=%d",
		webhookEvent.EventType, webhookEvent.AppID, webhookEvent.Timestamp)

	// 调用通用的 wordgate webhook 处理器
	if err := handleWordgateWebhookEvent(c, &webhookEvent); err != nil {
		log.Errorf(c, "[Webhook] failed to handle wordgate webhook event: %v", err)
		c.AbortWithStatus(int(ErrorSystemError))
		return
	}

	log.Infof(c, "[Webhook] wordgate webhook event processed successfully")
	SuccessEmpty(c)
}

// handleWordgateWebhookEvent 通用 wordgate webhook 事件处理器
func handleWordgateWebhookEvent(c *gin.Context, webhookEvent *wordgate.WebhookEventData) error {
	// 处理不同类型的事件
	switch webhookEvent.EventType {
	case wordgate.WebhookEventOrderPaid:
		return handleWordgateOrderPaidEvent(c, webhookEvent)
	case wordgate.WebhookEventOrderCancelled:
		return handleWordgateOrderCancelledEvent(c, webhookEvent)
	default:
		log.Warnf(c, "[Webhook] unsupported wordgate webhook event type: %s", webhookEvent.EventType)
		// 对于不支持的事件类型，返回 nil 以避免重复发送
		return nil
	}
}

// handleWordgateOrderPaidEvent 处理 wordgate 订单支付成功事件
func handleWordgateOrderPaidEvent(c *gin.Context, webhookEvent *wordgate.WebhookEventData) error {
	var orderData wordgate.WebhookOrderPaidData
	if err := webhookEvent.Parse(&orderData); err != nil {
		return err
	}

	log.Infof(c, "[Webhook] processing wordgate order paid event: order_no=%s, amount=%d, currency=%s",
		orderData.WordgateOrderNo, orderData.Amount, orderData.Currency)

	// 使用带死锁重试的事务处理整个流程
	// 死锁可能发生在：两个并发订单涉及相同的邀请人时
	return withDeadlockRetry(c, 3, func(tx *gorm.DB) error {
		// 根据 wordgate 订单号查找本地订单，使用结构体字段确保编译时类型安全
		var order Order
		err := tx.Preload("User").Where(&Order{WordgateOrderNo: orderData.WordgateOrderNo}).First(&order).Error
		if err != nil {
			log.Errorf(c, "[Webhook] failed to get order by wordgate_order_no %s: %v", orderData.WordgateOrderNo, err.Error())
			return err
		}

		log.Debugf(c, "[Webhook] found local order: id=%d, uuid=%s, is_paid=%v", order.ID, order.UUID, order.IsPaid)

		localIsPaid := order.IsPaid != nil && *order.IsPaid
		// 如果本地订单未标记为已支付，则处理支付
		if !localIsPaid && orderData.IsPaid {
			log.Infof(c, "[Webhook] processing payment for order %s", order.UUID)

			// 调用 MarkOrderAsPaid 处理完整的支付流程（授权 + 返现 + 邀请奖励）
			if err := MarkOrderAsPaid(c, tx, &order); err != nil {
				return fmt.Errorf("处理订单支付失败: %v", err)
			}

			log.Infof(c, "[Webhook] successfully processed payment for order %s", order.UUID)
		} else if localIsPaid {
			log.Debugf(c, "[Webhook] order %s already marked as paid", order.UUID)
		} else {
			log.Debugf(c, "[Webhook] order %s payment status mismatch: webhook_paid=%v, local_paid=%v",
				order.UUID, orderData.IsPaid, localIsPaid)
		}

		return nil
	})
}

// handleWordgateOrderCancelledEvent 处理 wordgate 订单取消事件
func handleWordgateOrderCancelledEvent(c *gin.Context, webhookEvent *wordgate.WebhookEventData) error {
	var cancelData wordgate.WebhookOrderCancelledData
	if err := webhookEvent.Parse(&cancelData); err != nil {
		return err
	}

	log.Infof(c, "[Webhook] processing wordgate order cancelled event: order_no=%s, reason=%s",
		cancelData.WordgateOrderNo, cancelData.Reason)

	// 使用事务处理整个流程
	return db.Get().Transaction(func(tx *gorm.DB) error {
		// 根据 wordgate 订单号查找本地订单
		var order Order
		err := tx.Where(&Order{WordgateOrderNo: cancelData.WordgateOrderNo}).First(&order).Error
		if err != nil {
			log.Errorf(c, "[Webhook] failed to get order by wordgate_order_no %s: %v", cancelData.WordgateOrderNo, err.Error())
			return err
		}

		log.Debugf(c, "[Webhook] found local order: id=%d, uuid=%s, is_paid=%v", order.ID, order.UUID, order.IsPaid)

		// 如果订单已支付但被取消，记录日志但不做特殊处理
		// 实际业务逻辑可能需要根据具体需求来处理订单取消
		if order.IsPaid != nil && *order.IsPaid {
			log.Warnf(c, "[Webhook] paid order %s was cancelled: %s", order.UUID, cancelData.Reason)
		}

		// 这里可以添加具体的订单取消处理逻辑
		// 例如：更新订单状态、回滚用户权益等

		return nil
	})
}
