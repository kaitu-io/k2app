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

// applyOrderToTargetUsers 订单生效：为指定用户延长授权时间（在给定事务中执行）
func applyOrderToTargetUsers(ctx context.Context, tx *gorm.DB, order *Order) error {
	log.Infof(ctx, "[applyOrderToTargetUsers] applying order %d to target users", order.ID)

	if order.IsPaid == nil || !*order.IsPaid {
		log.Warnf(ctx, "[ApplyOrderToTargetUsers] order %d is not paid, skipping", order.ID)
		return nil // 未支付的订单不处理
	}

	// 获取套餐信息
	plan, err := order.GetPlan()
	if err != nil {
		log.Errorf(ctx, "[ApplyOrderToTargetUsers] failed to get plan for order %d: %v", order.ID, err)
		return err
	}
	if plan == nil {
		log.Errorf(ctx, "[ApplyOrderToTargetUsers] plan is nil for order %d", order.ID)
		return fmt.Errorf("plan not found for order %d", order.ID)
	}

	// 使用月数计算实际天数（按自然月计算，正确处理闰年等情况）
	// 例如：2025-11-26 + 12个月 = 2026-11-26（365天或366天）
	now := time.Now()
	futureDate := now.AddDate(0, plan.Month, 0)
	days := int(futureDate.Sub(now).Hours() / 24)
	log.Debugf(ctx, "[ApplyOrderToTargetUsers] plan: PID=%s, Month=%d, Days=%d (calculated from %s to %s)",
		plan.PID, plan.Month, days, now.Format("2006-01-02"), futureDate.Format("2006-01-02"))

	// 获取目标用户列表
	forUserUUIDs := order.GetForUsers()
	forMyself := order.GetForMyself()
	log.Debugf(ctx, "[ApplyOrderToTargetUsers] forMyself=%v, forUserUUIDs=%v", forMyself, forUserUUIDs)

	// 收集所有需要增加 Pro 的用户（直接查询 User 对象，避免后续重复查询）
	var targetUsers []User

	// 1. 如果为自己购买，查询购买者
	if forMyself {
		var buyer User
		if err := tx.First(&buyer, order.UserID).Error; err != nil {
			log.Errorf(ctx, "[ApplyOrderToTargetUsers] failed to find order buyer %d: %v", order.UserID, err)
			return fmt.Errorf("购买者不存在: %v", err)
		}
		targetUsers = append(targetUsers, buyer)
		log.Debugf(ctx, "[ApplyOrderToTargetUsers] added order owner: UserID=%d", order.UserID)
	}

	// 2. 根据 UUID 查找其他用户
	if len(forUserUUIDs) > 0 {
		var users []User
		if err := tx.Where("uuid IN ?", forUserUUIDs).Find(&users).Error; err != nil {
			log.Errorf(ctx, "[ApplyOrderToTargetUsers] failed to find users by UUIDs: %v", err)
			return err
		}

		// 检查是否所有 UUID 都找到了（严格验证，避免部分用户未授权）
		if len(users) != len(forUserUUIDs) {
			log.Errorf(ctx, "[ApplyOrderToTargetUsers] some users not found: expected=%d, found=%d",
				len(forUserUUIDs), len(users))
			return fmt.Errorf("部分目标用户不存在: 期望 %d 个，实际找到 %d 个", len(forUserUUIDs), len(users))
		}

		for _, user := range users {
			targetUsers = append(targetUsers, user)
			log.Debugf(ctx, "[ApplyOrderToTargetUsers] added user: UUID=%s, UserID=%d", user.UUID, user.ID)
		}
	}

	if len(targetUsers) == 0 {
		log.Warnf(ctx, "[ApplyOrderToTargetUsers] no target users found for order %d", order.ID)
		return nil
	}

	log.Infof(ctx, "[ApplyOrderToTargetUsers] processing %d target users", len(targetUsers))

	// 3. 直接遍历 targetUsers，为每个用户增加授权（无需再次查询数据库）
	for i := range targetUsers {
		user := &targetUsers[i] // 获取指针以便 addProExpiredDays 修改用户数据

		// 使用 addProExpiredDays 统一处理（自动处理过期时间计算、首单标记、历史记录）
		reason := fmt.Sprintf("订单支付 - %s", order.UUID)
		_, err := addProExpiredDays(ctx, tx, user, VipPurchase, order.ID, days, reason)
		if err != nil {
			log.Errorf(ctx, "[ApplyOrderToTargetUsers] failed to add Pro to user %d: %v", user.ID, err)
			return err
		}

		log.Infof(ctx, "[ApplyOrderToTargetUsers] successfully added %d days to user %d", days, user.ID)
	}

	log.Infof(ctx, "[ApplyOrderToTargetUsers] successfully applied order %d to all target users", order.ID)
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
