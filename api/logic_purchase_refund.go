package center

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 一笔付费购买被退款后的后续动作（spec 2026-10-08 Apple 退款 §3.5）。Apple 退款（applyAppleRefund）
// 与后台网页退款（ProcessOrderRefund）共用，保证两条路径同一语义：
//   - 被邀请首购奖励：这笔购买触发的奖励，若被邀请人已不再持有合格购买，则双方撤回；
//   - 分销商付费人数：这笔单当初计入的人数，买家已无其它有效付费单时扣回。
// 返现撤回不在这里（refundCashbackInTx），付费时长扣减也不在这里（各路径自己算）。

// purchaseKeys 一笔购买的全部锚点。Apple 单两者都可能有（订单建成时）；网页单只有 OrderID。
type purchaseKeys struct {
	OrderID    uint64
	AppleTxnID string
}

func (k purchaseKeys) matches(kind, ref string) bool {
	switch kind {
	case InviteTriggerOrder:
		return k.OrderID != 0 && ref == strconv.FormatUint(k.OrderID, 10)
	case InviteTriggerAppleTxn:
		return k.AppleTxnID != "" && ref == k.AppleTxnID
	}
	return false
}

// refundFollowup 后续动作的结果；Alerts 由调用方在事务提交后发出（事务内发了再回滚会误报）。
type refundFollowup struct {
	InviteReversed   bool
	InviteReanchored bool
	InviteeCut       int64
	InviterCut       int64
	InviterUserID    uint64
	RetailerAdjusted string // "" | decremented | transferred
	Alerts           []string
}

// alertBilling 计费类告警出口（var 供测试替换）。
var alertBilling = func(ctx context.Context, msg string) {
	log.Errorf(ctx, "%s", msg)
	if err := slack.Send("alert", msg); err != nil {
		log.Errorf(ctx, "failed to send billing alert: %v", err)
	}
}

func sendBillingAlerts(ctx context.Context, msgs []string) {
	for _, m := range msgs {
		alertBilling(ctx, m)
	}
}

// lockExistingRow 找到满足条件的行并按主键加行锁，找不到返回 (false, nil)。
// 不直接对"可能不存在"的唯一键做 FOR UPDATE：那会在 InnoDB 里加间隙锁，两个首次插入的事务
// 互等成死锁。调用方必须已持有串行化这条记录的上层锁（订阅行 / 用户行），普通读的快照在上层锁
// 之后建立；按主键加锁的重读是当前读，拿到最新值。
func lockExistingRow[T any](tx *gorm.DB, dest *T, query string, args ...any) (bool, error) {
	var ids []uint64
	if err := tx.Model(dest).Where(query, args...).Order("id ASC").Limit(1).Pluck("id", &ids).Error; err != nil {
		return false, err
	}
	if len(ids) == 0 {
		return false, nil
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(dest, ids[0]).Error; err != nil {
		return false, err
	}
	return true, nil
}

// onPaidOrderRefundedInTx 见文件头。调用前提：买家用户行已在本事务内加锁、付费时长已扣完写库
// （邀请扣减基于重新读取的 expired_at）。order 可为 nil（Apple 建单失败 / 沙盒）。
// 幂等：grant 的 Reversed 与订单的 RetailerCountedID 都是一次性标记，重复调用不重复扣。
func onPaidOrderRefundedInTx(ctx context.Context, tx *gorm.DB, buyerID uint64, keys purchaseKeys, order *Order, keepInviteRewards bool) (*refundFollowup, error) {
	out := &refundFollowup{}
	if !keepInviteRewards {
		if err := reverseInviteGrantForPurchaseInTx(ctx, tx, buyerID, keys, out); err != nil {
			return nil, err
		}
	}
	if order != nil {
		if err := adjustRetailerCountOnRefundInTx(ctx, tx, order, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// reverseInviteGrantForPurchaseInTx 撤回这笔购买触发的被邀请首购奖励。被邀请人若还持有另一笔
// 合格购买，改锚到那笔、不撤回（条款：奖励以保留一笔合格购买为前提）。
func reverseInviteGrantForPurchaseInTx(ctx context.Context, tx *gorm.DB, buyerID uint64, keys purchaseKeys, out *refundFollowup) error {
	var grant InviteRewardGrant
	found, err := lockExistingRow(tx, &grant, "invitee_user_id = ? AND reversed = ?", buyerID, false)
	if err != nil {
		return fmt.Errorf("load invite grant for user %d: %w", buyerID, err)
	}
	if !found || grant.Reversed {
		return nil
	}
	if !keys.matches(grant.TriggerKind, grant.TriggerRef) {
		return nil // 奖励锚在别的购买上（或已改锚），这笔退款不影响它
	}

	if kind, ref, ok, err := qualifyingPurchaseAnchorInTx(ctx, tx, buyerID, keys); err != nil {
		return err
	} else if ok {
		if err := tx.Model(&InviteRewardGrant{}).Where("id = ?", grant.ID).
			Updates(map[string]any{"trigger_kind": kind, "trigger_ref": ref}).Error; err != nil {
			return fmt.Errorf("re-anchor invite grant %d: %w", grant.ID, err)
		}
		out.InviteReanchored = true
		log.Infof(ctx, "[InviteRefund] grant %d re-anchored to %s/%s (invitee %d still holds a qualifying purchase)",
			grant.ID, kind, ref, buyerID)
		return nil
	}

	now := time.Now().Unix()
	// 锁序：被邀请人（调用方已锁）→ 邀请人。
	var inviter User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&inviter, grant.InviterUserID).Error; err != nil &&
		!errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("lock inviter %d: %w", grant.InviterUserID, err)
	}
	inviteeCut, err := clawBackGiftInTx(tx, buyerID, grant.InviteeSeconds, now,
		"被邀请人退款，被邀请购买奖励收回", grant.InviteCodeID)
	if err != nil {
		return err
	}
	var inviterCut int64
	if inviter.ID != 0 {
		if inviterCut, err = clawBackGiftInTx(tx, inviter.ID, grant.InviterSeconds, now,
			fmt.Sprintf("被邀请用户 %d 退款，邀请奖励收回", buyerID), grant.InviteCodeID); err != nil {
			return err
		}
	}
	if err := tx.Model(&InviteRewardGrant{}).Where("id = ?", grant.ID).Updates(map[string]any{
		"reversed": true, "invitee_cut_seconds": inviteeCut, "inviter_cut_seconds": inviterCut,
	}).Error; err != nil {
		return fmt.Errorf("mark invite grant %d reversed: %w", grant.ID, err)
	}
	out.InviteReversed, out.InviteeCut, out.InviterCut, out.InviterUserID = true, inviteeCut, inviterCut, grant.InviterUserID
	log.Infof(ctx, "[InviteRefund] grant %d reversed: invitee %d −%ds, inviter %d −%ds", grant.ID, buyerID, inviteeCut, grant.InviterUserID, inviterCut)
	return nil
}

// clawBackGiftInTx 从用户剩余时长里扣回至多 seconds（不扣到 now 以下），写一条 refund 历史。
// 返回实扣秒数。基于库里当前的 expired_at，不信任任何内存快照。
func clawBackGiftInTx(tx *gorm.DB, userID uint64, seconds, now int64, reason string, refID uint64) (int64, error) {
	if seconds <= 0 {
		return 0, nil
	}
	// 当前读（加锁）：可重复读下普通读用的是事务早先的快照，邀请人行可能在那之后被别的事务改过，
	// 按旧值算出的实扣会偏小，撤销退款时也只还回偏小的量。
	var u User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "expired_at").First(&u, userID).Error; err != nil {
		return 0, fmt.Errorf("load user %d: %w", userID, err)
	}
	cut := min(seconds, max(u.ExpiredAt-now, 0))
	if cut <= 0 {
		return 0, nil
	}
	if err := tx.Model(&User{}).Where("id = ?", userID).
		Update("expired_at", gorm.Expr("GREATEST(expired_at - ?, ?)", cut, now)).Error; err != nil {
		return 0, fmt.Errorf("claw back %ds from user %d: %w", cut, userID, err)
	}
	if err := tx.Create(&UserProHistory{
		UserID: userID, Type: VipRefund, ReferenceID: refID, Days: -ceilDays(cut), Reason: reason,
	}).Error; err != nil {
		return 0, fmt.Errorf("write refund history for user %d: %w", userID, err)
	}
	return cut, nil
}

// ceilDays 向上取整的天数：扣减展示用，避免不足一天的扣减显示为"-0 天"。
func ceilDays(seconds int64) int {
	return int((seconds + 86399) / 86400)
}

// qualifyingPurchaseAnchorInTx 被邀请人仍持有的最早一笔合格购买：已付、未退款、实付 > 0、
// 套餐月数达到邀请门槛，且不是正在退的这笔（按订单 id 与 Apple 交易号双重排除：Apple 建单
// 失败时没有订单 id，调用时机也不保证这笔已被标记退款）。IAP 单锚到其交易号，网页单锚到订单 id。
func qualifyingPurchaseAnchorInTx(ctx context.Context, tx *gorm.DB, buyerID uint64, exclude purchaseKeys) (kind, ref string, ok bool, err error) {
	var orders []Order
	q := tx.Where("user_id = ? AND is_paid = ? AND (is_refunded IS NULL OR is_refunded = ?) AND pay_amount > 0 AND id <> ?",
		buyerID, true, false, exclude.OrderID)
	if exclude.AppleTxnID != "" {
		q = q.Where("(apple_transaction_id IS NULL OR apple_transaction_id <> ?)", exclude.AppleTxnID)
	}
	if err = q.Order("id ASC").Find(&orders).Error; err != nil {
		return "", "", false, fmt.Errorf("list qualifying orders for user %d: %w", buyerID, err)
	}
	minMonths := configInvite(ctx).MinRewardMonths
	for i := range orders {
		o := &orders[i]
		plan, perr := o.GetPlan()
		if perr != nil || plan == nil || plan.Month < minMonths {
			continue
		}
		if o.Channel == OrderChannelAppleIAP && o.AppleTransactionID != "" {
			return InviteTriggerAppleTxn, o.AppleTransactionID, true, nil
		}
		return InviteTriggerOrder, strconv.FormatUint(o.ID, 10), true, nil
	}
	return "", "", false, nil
}

// adjustRetailerCountOnRefundInTx 扣回这笔单当初计入的分销商付费人数。买家还有其它有效付费单
// 时把计数标记转移给其中最早的一笔（买家仍在付费，人数不变）。等级不自动降，跌破自动升级门槛时告警。
func adjustRetailerCountOnRefundInTx(ctx context.Context, tx *gorm.DB, order *Order, out *refundFollowup) error {
	if order.RetailerCountedID == 0 {
		return nil
	}
	configID := order.RetailerCountedID
	if err := tx.Model(&Order{}).Where("id = ?", order.ID).Update("retailer_counted_id", 0).Error; err != nil {
		return fmt.Errorf("clear retailer count mark on order %d: %w", order.ID, err)
	}

	var other Order
	err := tx.Where("user_id = ? AND is_paid = ? AND (is_refunded IS NULL OR is_refunded = ?) AND id <> ?",
		order.UserID, true, false, order.ID).Order("id ASC").First(&other).Error
	if err == nil {
		if other.RetailerCountedID == 0 {
			if err := tx.Model(&Order{}).Where("id = ?", other.ID).Update("retailer_counted_id", configID).Error; err != nil {
				return fmt.Errorf("transfer retailer count mark to order %d: %w", other.ID, err)
			}
		}
		out.RetailerAdjusted = "transferred"
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("find other paid order for user %d: %w", order.UserID, err)
	}

	if err := tx.Model(&RetailerConfig{}).Where("id = ?", configID).
		Update("paid_user_count", gorm.Expr("GREATEST(paid_user_count - 1, 0)")).Error; err != nil {
		return fmt.Errorf("decrement paid_user_count for retailer config %d: %w", configID, err)
	}
	out.RetailerAdjusted = "decremented"

	var cfg RetailerConfig
	if err := tx.First(&cfg, configID).Error; err != nil {
		return fmt.Errorf("reload retailer config %d: %w", configID, err)
	}
	if cfg.Level == RetailerLevelRetailer && cfg.PaidUserCount < RetailerLevelConfig[RetailerLevelRetailer].RequiredUsers {
		var last RetailerLevelHistory
		if err := tx.Where("retailer_config_id = ?", configID).Order("id DESC").First(&last).Error; err == nil &&
			last.Reason == "auto_upgrade" {
			out.Alerts = append(out.Alerts, fmt.Sprintf(
				"[RETAILER-COUNT-DROP] 分销商配置 %d（用户 %d）自动升级到 L2 后，付费人数因退款跌到 %d（门槛 %d）——是否降级由运营决定（订单 %s）",
				configID, cfg.UserID, cfg.PaidUserCount, RetailerLevelConfig[RetailerLevelRetailer].RequiredUsers, order.UUID))
		}
	}
	return nil
}
