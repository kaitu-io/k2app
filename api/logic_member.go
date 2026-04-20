package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"context"
	"fmt"
	"time"

	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// addProExpiredDays 添加Pro过期天数
func addProExpiredDays(ctx context.Context, tx *gorm.DB, user *User, vipType VipChangeType, referenceID uint64, days int, reason string) (*UserProHistory, error) {
	log.Infof(ctx, "[addProExpiredDays] adding %d days to user %d (type: %s)", days, user.ID, vipType)
	log.Debugf(ctx, "[addProExpiredDays] reference_id=%d, reason=%s", referenceID, reason)
	log.Debugf(ctx, "[addProExpiredDays] user current ExpiredAt: %d (%s)", user.ExpiredAt, time.Unix(user.ExpiredAt, 0).Format("2006-01-02 15:04:05"))

	// 创建历史记录
	history := &UserProHistory{
		UserID:      user.ID,
		Type:        vipType,
		Days:        days,
		Reason:      reason,
		ReferenceID: referenceID,
	}

	if err := tx.Create(history).Error; err != nil {
		log.Errorf(ctx, "[addProExpiredDays] failed to create pro history for user %d: %v", user.ID, err)
		return nil, err
	}
	log.Debugf(ctx, "[addProExpiredDays] created pro history record %d", history.ID)

	// 计算新的过期时间
	now := time.Now()

	// 如果用户已过期，从当前时间开始计算
	if user.ExpiredAt < now.Unix() {
		user.ExpiredAt = now.AddDate(0, 0, days).Unix()
		log.Debugf(ctx, "[addProExpiredDays] user %d was expired, new expiry from current time: %s",
			user.ID, time.Unix(user.ExpiredAt, 0).Format("2006-01-02 15:04:05"))
	} else {
		// 如果用户未过期，在现有时间基础上增加天数
		user.ExpiredAt = time.Unix(user.ExpiredAt, 0).AddDate(0, 0, days).Unix()
		log.Debugf(ctx, "[addProExpiredDays] user %d extended from existing time: %s",
			user.ID, time.Unix(user.ExpiredAt, 0).Format("2006-01-02 15:04:05"))
	}

	// 如果这是用户的第一笔订单（只有真正的订单支付才标记，奖励不标记）
	if vipType == VipPurchase {
		if user.IsFirstOrderDone == nil || !*user.IsFirstOrderDone {
			user.IsFirstOrderDone = BoolPtr(true)
			log.Infof(ctx, "[addProExpiredDays] marking user %d first order as completed", user.ID)
		}
	}

	// 付费时自动激活用户账号(避免出现已付费但未激活的状态)
	if user.IsActivated == nil || !*user.IsActivated {
		user.IsActivated = BoolPtr(true)
		user.ActivatedAt = time.Now().Unix()
		log.Infof(ctx, "[addProExpiredDays] auto-activating user %d at payment (activated_at=%d)", user.ID, user.ActivatedAt)
	}

	// 保存用户更新
	if err := tx.Save(user).Error; err != nil {
		log.Errorf(ctx, "[addProExpiredDays] failed to save user %d: %v", user.ID, err)
		return history, err
	}

	log.Infof(ctx, "[addProExpiredDays] user %d updated: ExpiredAt=%s, IsFirstOrderDone=%v, IsActivated=%v",
		user.ID, time.Unix(user.ExpiredAt, 0).Format("2006-01-02 15:04:05"), user.IsFirstOrderDone, user.IsActivated)

	return history, nil
}

// applyOrderToBuyer 订单生效：为购买者本人延长授权时间
// 注：旧版本支持代付（forUsers），2026-04-20 简化为只处理 buyer
func applyOrderToBuyer(ctx context.Context, tx *gorm.DB, order *Order) error {
	log.Infof(ctx, "[applyOrderToBuyer] applying order %d to buyer %d", order.ID, order.UserID)

	if order.IsPaid == nil || !*order.IsPaid {
		log.Warnf(ctx, "[applyOrderToBuyer] order %d not paid, skipping", order.ID)
		return nil
	}

	plan, err := order.GetPlan()
	if err != nil || plan == nil {
		return fmt.Errorf("plan not found for order %d", order.ID)
	}

	var buyer User
	if err := tx.First(&buyer, order.UserID).Error; err != nil {
		return fmt.Errorf("buyer not found: %v", err)
	}

	// Tier 处理：首次购买写入；续费保持（API 层已校验匹配）
	if buyer.IsFirstOrderDone == nil || !*buyer.IsFirstOrderDone {
		tier := plan.Tier
		if tier == "" {
			tier = TierBasic // legacy plans without Tier field default to basic (the "pro" rename target)
		}
		buyer.Tier = tier
		log.Infof(ctx, "[applyOrderToBuyer] first-time purchase, set buyer.Tier=%s", buyer.Tier)
	}
	// 注：MaxDevice/MaxRouterDevice/MaxLanClient 字段已删除，不再写

	// 计算实际天数（按自然月）
	now := time.Now()
	days := int(now.AddDate(0, plan.Month, 0).Sub(now).Hours() / 24)

	reason := fmt.Sprintf("订单支付 - %s", order.UUID)
	_, err = addProExpiredDays(ctx, tx, &buyer, VipPurchase, order.ID, days, reason)
	if err != nil {
		return err
	}

	log.Infof(ctx, "[applyOrderToBuyer] success: %d days added to buyer %d, tier=%s", days, buyer.ID, buyer.Tier)
	return nil
}

// CanPayForUsers 检查用户是否可以为指定用户付费
func CanPayForUsers(user *User, targetUserIDs []uint64) bool {
	if len(targetUserIDs) == 0 {
		return true // 如果没有指定其他用户，允许（可能只是为自己购买）
	}

	// 查找目标用户
	var targetUsers []User
	if err := db.Get().Where("id IN ?", targetUserIDs).Find(&targetUsers).Error; err != nil {
		return false // 数据库错误，拒绝
	}

	// 检查是否所有目标用户都存在
	if len(targetUsers) != len(targetUserIDs) {
		return false // 有些用户不存在
	}

	// 检查每个目标用户：要么是自己，要么委托给了自己
	for _, targetUser := range targetUsers {
		if targetUser.ID == user.ID {
			continue // 自己总是可以的
		}

		// 检查是否委托给了自己
		if targetUser.DelegateID == nil || *targetUser.DelegateID != user.ID {
			return false
		}
	}

	return true
}
