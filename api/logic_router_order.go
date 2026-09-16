package center

import (
	"context"
	"fmt"
	"time"

	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// applyRouterOrder 路由器版订单入账（事务内）。
//   - 套餐含硬件，或用户名下没有可续的线路 → 新购：建内部专属线路（pending，post-commit 入开机队列）+
//     发货台账（paid）。
//   - 套餐不含硬件且用户已有 active/grace/suspended 线路 → 续费：延期该线路，不建新线、不建台账。
//
// 不碰 User.ExpiredAt（路由器版独立时钟）。
func applyRouterOrder(ctx context.Context, tx *gorm.DB, order *Order, plan *Plan, now int64, provisionSubIDs *[]uint64) error {
	if plan.HardwareSKU == "" {
		existing, err := findRenewablePrivateLine(tx, order.UserID)
		if err != nil {
			return err
		}
		if existing != nil {
			if err := extendPrivateLine(ctx, tx, existing, plan.Month, now); err != nil {
				return fmt.Errorf("extend private line %d for router order %d: %w", existing.ID, order.ID, err)
			}
			log.Infof(ctx, "router renewal: order %d extended line %d by %d months", order.ID, existing.ID, plan.Month)
			sendCloudSlackNotification(ctx, "Router Edition — Renewed",
				fmt.Sprintf("路由器版续费 user=%d order=%d line=%d 延期 %d 个月（若该线路已停机需人工开机）。", order.UserID, order.ID, existing.ID, plan.Month))
			return nil
		}
	}

	sub, err := createPrivateNodeSubscription(ctx, tx, order, plan, now)
	if err != nil {
		return fmt.Errorf("create internal line for router order %d: %w", order.ID, err)
	}
	if provisionSubIDs != nil {
		*provisionSubIDs = append(*provisionSubIDs, sub.ID)
	}
	f := &RouterFulfillment{OrderID: order.ID, UserID: order.UserID, SubID: sub.ID,
		HardwareSKU: plan.HardwareSKU, Stage: RouterStagePaid, UpdatedBy: "system:order"}
	if err := tx.Create(f).Error; err != nil {
		return fmt.Errorf("create router fulfillment for order %d: %w", order.ID, err)
	}
	log.Infof(ctx, "router order %d: line %d created (pending), fulfillment %d (sku=%q)", order.ID, sub.ID, f.ID, plan.HardwareSKU)
	return nil
}

// findRenewablePrivateLine 用户名下最新的可续线路（active/grace/suspended，按到期时间倒序）；无则 nil。
func findRenewablePrivateLine(tx *gorm.DB, userID uint64) (*PrivateNodeSubscription, error) {
	var sub PrivateNodeSubscription
	err := tx.Where("user_id = ? AND status IN ?", userID,
		[]string{PNStatusActive, PNStatusGrace, PNStatusSuspended}).
		Order("expires_at DESC").First(&sub).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// extendPrivateLine 延期：未到期从当前到期日叠加，已到期从 now 起算；grace/suspended 回 active 并
// 取消未结 stop/destroy（与 lifecycle sweep 的续费回收同语义，但立即生效不等 cron）。
func extendPrivateLine(ctx context.Context, tx *gorm.DB, sub *PrivateNodeSubscription, months int, now int64) error {
	base := sub.ExpiresAt
	if base < now {
		base = now
	}
	newExpiry := time.Unix(base, 0).AddDate(0, months, 0).Unix()
	updates := map[string]any{"expires_at": newExpiry}
	if sub.Status == PNStatusGrace || sub.Status == PNStatusSuspended {
		updates["status"] = PNStatusActive
		updates["grace_until"] = 0
		updates["suspend_until"] = 0
	}
	if err := tx.Model(&PrivateNodeSubscription{}).Where("id = ?", sub.ID).Updates(updates).Error; err != nil {
		return err
	}
	if sub.Status == PNStatusGrace || sub.Status == PNStatusSuspended {
		if err := cancelOpenNodeOperations(tx, []uint64{sub.ID}, []string{NodeOpStop, NodeOpDestroy}); err != nil {
			log.Errorf(ctx, "extend line %d: cancel open stop/destroy: %v", sub.ID, err)
		}
	}
	sub.ExpiresAt = newExpiry
	return nil
}
