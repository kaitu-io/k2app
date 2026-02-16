package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"context"
	"fmt"
	"time"

	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// ==================== 订单支付处理 ====================

// MarkOrderAsPaid 标记订单为已支付，并处理所有支付后的业务逻辑
// 这是订单支付的主调度函数，协调以下流程：
// 1. 更新订单状态为已支付
// 2. 处理邀请购买奖励（必须在 ApplyOrderToTargetUsers 之前，因为后者会设置 IsFirstOrderDone=true）
// 3. 调用 ApplyOrderToTargetUsers 给购买用户增加 Pro 授权
// 4. 调用 processOrderCashbackInTx 处理分销商返现
//
// 原子性保证：所有步骤必须全部成功，任何一步失败都会导致整个事务回滚
// 这确保用户不会出现"付了款但没拿到应得奖励"的情况
func MarkOrderAsPaid(ctx context.Context, tx *gorm.DB, order *Order) error {
	log.Infof(ctx, "[MarkOrderAsPaid] processing payment for order %d", order.ID)

	// 如果订单已经标记为已支付，直接返回
	if order.IsPaid != nil && *order.IsPaid {
		log.Debugf(ctx, "[MarkOrderAsPaid] order %d is already marked as paid, skipping", order.ID)
		return nil
	}

	log.Debugf(ctx, "[MarkOrderAsPaid] order details: UUID=%s, UserID=%d, PayAmount=%d",
		order.UUID, order.UserID, order.PayAmount)

	// 第一步：更新订单状态为已支付
	now := time.Now()
	order.IsPaid = BoolPtr(true)
	order.PaidAt = &now

	if err := tx.Model(order).
		Select("IsPaid", "PaidAt").
		Updates(order).Error; err != nil {
		log.Errorf(ctx, "[MarkOrderAsPaid] failed to update order %d payment status: %v", order.ID, err)
		return fmt.Errorf("更新订单支付状态失败: %v", err)
	}
	log.Infof(ctx, "[MarkOrderAsPaid] order %d marked as paid at %s", order.ID, now.Format("2006-01-02 15:04:05"))

	// 第二步：处理邀请购买奖励（必须在 ApplyOrderToTargetUsers 之前执行）
	// 因为 ApplyOrderToTargetUsers 会设置 IsFirstOrderDone=true，必须先处理邀请奖励
	if err := handleInvitePurchaseRewardInTx(ctx, tx, order.UserID, order.ID); err != nil {
		log.Errorf(ctx, "[MarkOrderAsPaid] failed to handle invite purchase reward: %v", err)
		return fmt.Errorf("处理邀请购买奖励失败: %v", err)
	}

	// 第三步：为购买用户增加 Pro 授权
	if err := applyOrderToTargetUsers(ctx, tx, order); err != nil {
		log.Errorf(ctx, "[MarkOrderAsPaid] failed to apply order to target users: %v", err)
		return fmt.Errorf("给用户增加授权失败: %v", err)
	}

	// 第四步：处理分销商返现
	if err := processOrderCashbackInTx(ctx, tx, order.ID); err != nil {
		log.Errorf(ctx, "[MarkOrderAsPaid] failed to process order cashback: %v", err)
		return fmt.Errorf("处理分销商返现失败: %v", err)
	}

	log.Infof(ctx, "[MarkOrderAsPaid] payment processing completed successfully for order %d", order.ID)
	return nil
}

// ==================== 订单返现处理 ====================

// ProcessOrderCashback 处理订单返现
// 当订单支付完成后调用，为分销商发放返现
// 注意：此函数应在事务中调用，传入 tx 参数以保证原子性
func ProcessOrderCashback(ctx context.Context, orderID uint64) error {
	return processOrderCashbackInTx(ctx, db.Get(), orderID)
}

// processOrderCashbackInTx 在给定事务中处理订单分销商返现
// 此函数只负责分销商返现逻辑，用户授权由 ApplyOrderToTargetUsers 处理
// 详细的分成计算逻辑在 logic_retailer.go 中的 ProcessRetailerCashback
func processOrderCashbackInTx(ctx context.Context, tx *gorm.DB, orderID uint64) error {
	// 委托给 retailer 模块处理，保持订单逻辑简洁
	return processRetailerCashbackInTx(ctx, tx, orderID)
}

// ==================== 订单退款处理 ====================

// ProcessOrderRefund 处理订单退款
// 完整的退款流程：退款订单、退回返现、更新相关状态
func ProcessOrderRefund(ctx context.Context, orderID uint64, refundReason string) error {
	return db.Get().Transaction(func(tx *gorm.DB) error {
		// 1. 查询订单
		var order Order
		if err := tx.Preload("User").First(&order, orderID).Error; err != nil {
			return fmt.Errorf("查询订单失败: %v", err)
		}

		// 检查订单是否已支付
		if order.IsPaid == nil || !*order.IsPaid {
			return fmt.Errorf("订单未支付，无法退款")
		}

		// 2. 处理返现退回（如果有返现记录）
		if err := refundCashbackInTx(ctx, tx, orderID); err != nil {
			// 如果没有返现记录，仅记录警告，不影响退款流程
			log.Warnf(ctx, "退回返现失败（可能没有返现记录）: %v", err)
		}

		// 3. 更新订单状态为已退款
		order.IsPaid = BoolPtr(false)
		if err := tx.Model(&order).
			Select("IsPaid").
			Updates(&order).Error; err != nil {
			return fmt.Errorf("更新订单状态失败: %v", err)
		}
		// TODO: 如果有 is_refunded, refunded_at, refund_reason 字段，应该一起更新

		// 4. 处理用户套餐权限（如果需要）
		// TODO: 根据业务需求，可能需要撤销用户的 Pro 权限
		// 这部分逻辑需要根据实际业务场景实现

		log.Infof(ctx, "订单退款处理完成: order_id=%d, user_id=%d, amount=%d, reason=%s",
			orderID, order.UserID, order.PayAmount, refundReason)

		return nil
	})
}

