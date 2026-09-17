package center

import (
	"context"
	"errors"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// applyRouterOrder 路由器版订单入账（事务内）。
//   - 套餐含硬件，或用户名下没有可续的线路 → 新购：建内部专属线路（pending，post-commit 入开机队列）+
//     发货台账（paid）。服务套餐新建时若用户有旧硬件台账，新台账继承其 SKU/发货/凭证。
//   - 套餐不含硬件且用户已有 active/grace/suspended 线路 → 续费：延期该线路，不建新线、不建台账，
//     同事务同步挂在该线路上的台账。
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
			// 同一事务里把挂在这条线路上的台账按续费后的线路状态同步：否则存储的 expired 会一直陈旧到
			// 下次读，期间用户直接重铸凭证会挂不上台账（见 mintGatewayCredential）。
			var rows []RouterFulfillment
			if err := tx.Where("sub_id = ?", existing.ID).Find(&rows).Error; err != nil {
				return fmt.Errorf("load fulfillments of line %d for router order %d: %w", existing.ID, order.ID, err)
			}
			for i := range rows {
				if err := syncRouterFulfillment(ctx, tx, &rows[i], now); err != nil {
					return fmt.Errorf("sync fulfillment %d after renewal (order %d): %w", rows[i].ID, order.ID, err)
				}
			}
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
	if plan.HardwareSKU == "" {
		// 服务套餐走到新建线路 = 旧线路已 deprovisioned（无可续线路）。若用户以前买过成品路由器，
		// 硬件还在客户手里：新台账继承硬件台账的 SKU / 发货 / 凭证，而不是变成一条自备样子的台账
		// （否则账户页让硬件客户"自己装 k2r"，且 online 门槛丢了发货时刻）。
		var hw RouterFulfillment
		err := tx.Where("user_id = ? AND hardware_sku <> ?", order.UserID, "").Order("id DESC").First(&hw).Error
		switch {
		case err == nil:
			f.HardwareSKU = hw.HardwareSKU
			f.ShippedAt = hw.ShippedAt
			f.GatewayDeviceID = hw.GatewayDeviceID
			f.CredentialMintedAt = hw.CredentialMintedAt
			log.Infof(ctx, "router order %d: service plan inherits hardware fulfillment %d (sku=%q)", order.ID, hw.ID, hw.HardwareSKU)
		case errors.Is(err, gorm.ErrRecordNotFound):
		default:
			return fmt.Errorf("load hardware fulfillment for router order %d: %w", order.ID, err)
		}
	}
	if err := tx.Create(f).Error; err != nil {
		return fmt.Errorf("create router fulfillment for order %d: %w", order.ID, err)
	}
	log.Infof(ctx, "router order %d: line %d created (pending), fulfillment %d (sku=%q)", order.ID, sub.ID, f.ID, f.HardwareSKU)
	return nil
}

// renewableLineStatuses 可续线路的状态集合（findRenewablePrivateLine 与下单门共用）。
var renewableLineStatuses = []string{PNStatusActive, PNStatusGrace, PNStatusSuspended}

// userHasRouter 下单门（一户一台）：用户已有 stage≠expired 的路由器版台账，或有可续线路
// （active/grace/suspended）即为 true。只读、不加锁。存储的 stage 可能陈旧（线路已停服但台账没被读过），
// 先对非 expired 台账同步一次再判断，避免把线路早已 deprovisioned 的老用户挡在门外。
func userHasRouter(ctx context.Context, tx *gorm.DB, userID uint64, now int64) (bool, error) {
	var rows []RouterFulfillment
	if err := tx.Where("user_id = ? AND stage <> ?", userID, RouterStageExpired).Find(&rows).Error; err != nil {
		return false, err
	}
	for i := range rows {
		if err := syncRouterFulfillment(ctx, tx, &rows[i], now); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, err
		}
		if rows[i].Stage != RouterStageExpired {
			return true, nil
		}
	}
	var n int64
	if err := tx.Model(&PrivateNodeSubscription{}).
		Where("user_id = ? AND status IN ?", userID, renewableLineStatuses).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// userCanBuyRouterService 服务套餐下单门：服务套餐只作续费，不对没有路由器的用户新售（自备新购已下线）。
// 放行条件：userHasRouter 为真（未过期台账或可续线路），或用户有任意一条成品台账（hardware_sku 非空，
// 任何 stage）——后者对应线路已回收的成品客户，applyRouterOrder 会让新台账继承其硬件字段。
// 纯自备历史（台账全 expired 且无可续线路）不放行。只读、不加锁。
func userCanBuyRouterService(ctx context.Context, tx *gorm.DB, userID uint64, now int64) (bool, error) {
	has, err := userHasRouter(ctx, tx, userID, now)
	if err != nil || has {
		return has, err
	}
	var n int64
	if err := tx.Model(&RouterFulfillment{}).
		Where("user_id = ? AND hardware_sku <> ?", userID, "").Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// findRenewablePrivateLine 用户名下最新的可续线路（active/grace/suspended，按到期时间倒序）；无则 nil。
// FOR UPDATE 行锁：两笔并发续费 webhook 都读到同一条线路时，第二个必须等第一个提交后
// 再读，否则两次 extendPrivateLine 都从同一个旧 ExpiresAt 起算叠加，后写覆盖前一次的延期
// （丢失更新）。锁随调用方事务释放（提交/回滚）。
func findRenewablePrivateLine(tx *gorm.DB, userID uint64) (*PrivateNodeSubscription, error) {
	var sub PrivateNodeSubscription
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND status IN ?", userID, renewableLineStatuses).
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
