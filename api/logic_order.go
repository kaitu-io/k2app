package center

import (
	"context"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// getDB returns the global database instance.
// Overrideable in tests via package-level variable.
var getDB = func() *gorm.DB { return db.Get() }

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
	// 奖励失败不阻断支付到账——支付是最高优先级
	// 使用 SAVEPOINT 确保奖励要么全部成功要么全部回滚（避免部分状态）
	if err := tx.SavePoint("invite_reward").Error; err == nil {
		if err := handleInvitePurchaseRewardInTx(ctx, tx, order.UserID, order.ID); err != nil {
			tx.RollbackTo("invite_reward")
			log.Errorf(ctx, "[MarkOrderAsPaid] invite reward failed (non-fatal, rolled back), order %d: %v", order.ID, err)
		}
	}

	// 第三步：为购买用户增加 Pro 授权
	if err := applyOrderToBuyer(ctx, tx, order); err != nil {
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
// 完整的退款流程：
//  1. 加行锁加载订单 + 预校验
//  2. 撤销授权（扣 ExpiredAt、写反向 UserProHistory、必要时翻回 IsFirstOrderDone）
//  3. 撤销分销商返现（refundCashbackInTx）
//  4. 给用户钱包打款（order_refund 记录）
//  5. 更新订单状态（IsRefunded/RefundedAt/RefundAmount/RefundReason）
//
// operatorID 必传：落到 wallet_changes.operator_id，用于审计追溯
func ProcessOrderRefund(ctx context.Context, orderID uint64, refundReason string, operatorID uint64) error {
	return getDB().Transaction(func(tx *gorm.DB) error {
		// ---------- 1. 加行锁加载订单 + 预校验 ----------
		var order Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Preload("User").First(&order, orderID).Error; err != nil {
			return fmt.Errorf("查询订单失败: %v", err)
		}
		if order.IsPaid == nil || !*order.IsPaid {
			return fmt.Errorf("订单未支付，无法退款")
		}
		if order.IsRefunded != nil && *order.IsRefunded {
			return fmt.Errorf("订单已退款")
		}

		// ---------- 2. 撤销授权 ----------
		// SUM 订单直接关联的 VipPurchase 天数（不含邀请奖励）
		var purchaseDays int64
		if err := tx.Model(&UserProHistory{}).
			Select("COALESCE(SUM(days), 0)").
			Where("user_id = ? AND reference_id = ? AND type = ? AND days > ?",
				order.UserID, orderID, VipPurchase, 0).
			Scan(&purchaseDays).Error; err != nil {
			return fmt.Errorf("查询授权天数失败: %v", err)
		}

		if order.User == nil {
			return fmt.Errorf("订单关联用户为空")
		}
		user := *order.User

		if purchaseDays > 0 {
			// 扣 ExpiredAt（不强制置 0，小于 now 自然过期即可）
			user.ExpiredAt -= purchaseDays * 86400

			// 写反向 UserProHistory
			reverseHistory := UserProHistory{
				UserID:      user.ID,
				Type:        VipRefund,
				ReferenceID: orderID,
				Days:        int(-purchaseDays),
				Reason:      fmt.Sprintf("订单退款撤销授权 - 订单 %s，原因：%s", order.UUID, refundReason),
			}
			if err := tx.Create(&reverseHistory).Error; err != nil {
				return fmt.Errorf("写反向授权记录失败: %v", err)
			}
		} else {
			log.Warnf(ctx, "订单 %d 未找到关联的 VipPurchase 记录，跳过扣天数（脏数据）", orderID)
		}

		// 若是唯一有效付费订单，翻回 IsFirstOrderDone
		if user.IsFirstOrderDone != nil && *user.IsFirstOrderDone {
			var otherPaidCount int64
			if err := tx.Model(&Order{}).
				Where("user_id = ? AND is_paid = ? AND (is_refunded IS NULL OR is_refunded = ?) AND id != ?",
					user.ID, true, false, orderID).
				Count(&otherPaidCount).Error; err != nil {
				return fmt.Errorf("查询其它付费订单失败: %v", err)
			}
			if otherPaidCount == 0 {
				user.IsFirstOrderDone = BoolPtr(false)
			}
		}

		if err := tx.Model(&user).
			Select("ExpiredAt", "IsFirstOrderDone").
			Updates(&user).Error; err != nil {
			return fmt.Errorf("更新用户授权失败: %v", err)
		}

		// ---------- 3. 撤销分销商返现 ----------
		if err := refundCashbackInTx(ctx, tx, orderID); err != nil {
			// 没有 income 记录 → warning，不 rollback（沿用现有行为）
			log.Warnf(ctx, "退回返现失败（可能没有返现记录）: %v", err)
		}

		// ---------- 4. 给用户钱包打款 ----------
		wallet, err := getOrCreateWalletInTx(ctx, tx, user.ID)
		if err != nil {
			return fmt.Errorf("查询/创建用户钱包失败: %v", err)
		}

		balanceBefore := wallet.Balance
		orderRefundChange := WalletChange{
			WalletID:      wallet.ID,
			Type:          WalletChangeTypeOrderRefund,
			Amount:        int64(order.PayAmount),
			BalanceBefore: balanceBefore,
			BalanceAfter:  balanceBefore + int64(order.PayAmount),
			FrozenUntil:   nil, // E1: 不冻结
			OrderID:       &orderID,
			OperatorID:    &operatorID,
			Remark:        refundReason,
		}
		if err := tx.Create(&orderRefundChange).Error; err != nil {
			if util.DbIsDuplicatedErr(err) {
				return fmt.Errorf("订单已退款（wallet_changes 唯一索引冲突）")
			}
			return fmt.Errorf("记录钱包退款变动失败: %v", err)
		}

		if err := tx.Model(wallet).
			Update("balance", gorm.Expr("balance + ?", order.PayAmount)).
			Update("total_income", gorm.Expr("total_income + ?", order.PayAmount)).
			Error; err != nil {
			return fmt.Errorf("更新钱包余额失败: %v", err)
		}

		// ---------- 5. 更新订单状态 ----------
		nowTime := time.Now()
		order.IsRefunded = BoolPtr(true)
		order.RefundedAt = &nowTime
		order.RefundAmount = order.PayAmount
		order.RefundReason = refundReason
		if err := tx.Model(&order).
			Select("IsRefunded", "RefundedAt", "RefundAmount", "RefundReason").
			Updates(&order).Error; err != nil {
			return fmt.Errorf("更新订单退款状态失败: %v", err)
		}

		log.Infof(ctx, "order refunded: uuid=%s user=%d amount=%d reason=%s operator=%d",
			order.UUID, order.UserID, order.PayAmount, refundReason, operatorID)

		return nil
	})
}

// getOrCreateWalletInTx 在给定事务中查找或创建钱包
// 现有 GetOrCreateWallet 只用 db.Get()，不能用于事务内；此处提供事务版
func getOrCreateWalletInTx(ctx context.Context, tx *gorm.DB, userID uint64) (*Wallet, error) {
	var wallet Wallet
	err := tx.Where(&Wallet{UserID: userID}).First(&wallet).Error
	if err == gorm.ErrRecordNotFound {
		wallet = Wallet{UserID: userID}
		if err := tx.Create(&wallet).Error; err != nil {
			return nil, err
		}
		log.Infof(ctx, "在事务中创建钱包: user_id=%d, wallet_id=%d", userID, wallet.ID)
		return &wallet, nil
	}
	if err != nil {
		return nil, err
	}
	return &wallet, nil
}

