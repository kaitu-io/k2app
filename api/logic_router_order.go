package center

import (
	"context"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// applyRouterOrder 路由器版订单入账（事务内）。
//   - 套餐含硬件，或用户名下没有可续的线路 → 新购：建内部专属线路（pending，post-commit 入开机队列）+
//     发货台账（paid）。
//   - 套餐不含硬件且用户已有 active/grace/suspended 线路 → 续费：延期该线路，不建新线、不建台账。
//
// 不碰 User.ExpiredAt（路由器版独立时钟）。
func applyRouterOrder(ctx context.Context, tx *gorm.DB, order *Order, plan *Plan, now int64, pc *OrderPostCommit) error {
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
			// Slack 通知推迟到事务提交后（onRouterLineRenewed）：MarkOrderAsPaid 后面还有
			// processOrderCashbackInTx 可能失败回滚整个事务，事务内发通知会对着一次根本没
			// 发生的变更误报（与 Bug #4 的 enqueue 同理，见 OrderPostCommit 注释）。
			if pc != nil {
				pc.RenewedRouterSubIDs = append(pc.RenewedRouterSubIDs, existing.ID)
			}
			return nil
		}
	}

	sub, err := createPrivateNodeSubscription(ctx, tx, order, plan, now)
	if err != nil {
		return fmt.Errorf("create internal line for router order %d: %w", order.ID, err)
	}
	if pc != nil {
		pc.ProvisionSubIDs = append(pc.ProvisionSubIDs, sub.ID)
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
// FOR UPDATE 行锁：两笔并发续费 webhook 都读到同一条线路时，第二个必须等第一个提交后
// 再读，否则两次 extendPrivateLine 都从同一个旧 ExpiresAt 起算叠加，后写覆盖前一次的延期
// （丢失更新）。锁随调用方事务释放（提交/回滚）。
func findRenewablePrivateLine(tx *gorm.DB, userID uint64) (*PrivateNodeSubscription, error) {
	var sub PrivateNodeSubscription
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND status IN ?", userID,
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

// onRouterLineRenewed 路由器版续费 post-commit 通知（best-effort，不返回错误）：加载已延期
// 的订阅，发 Slack 告知运营新到期日；若该线路此前已停机（suspended 被续费回收到 active）
// 需要人工重新开机，Slack 里提醒一句。事务已提交，sub.ExpiresAt 已是延期后的值。
func onRouterLineRenewed(ctx context.Context, subID uint64) {
	var sub PrivateNodeSubscription
	if err := db.Get().First(&sub, subID).Error; err != nil {
		log.Errorf(ctx, "router renewal notify: load sub %d: %v", subID, err)
		return
	}
	sendCloudSlackNotification(ctx, "Router Edition — Renewed",
		fmt.Sprintf("路由器版续费 user=%d line=%d 新到期日=%s（若该线路已停机需人工开机）。",
			sub.UserID, sub.ID, time.Unix(sub.ExpiresAt, 0).Format("2006-01-02")))
}
