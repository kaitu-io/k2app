package center

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wordgate/qtoolkit/appstore"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Apple 退款收回（spec 2026-10-08-apple-refund-design.md）。两品牌同一条规则：
// 收回被退交易自身尚未消耗的付费时长；不碰赠送；不追讨已消耗的旧期；退款被撤销就还回去。
// 开途特有的风险（邀请、钱包、分销）挂在开途独有的功能上，由 onPaidOrderRefundedInTx 处理，
// 这里不写品牌分支。
//
// 先后顺序按 Apple 给出的时刻判定（毫秒），不按到达顺序：webhook、对账、Apple 重投都可能乱序。

// appleRefundEvidence 一次退款 / 撤销退款证据。
type appleRefundEvidence struct {
	SignedAt             int64 // 毫秒：通知 signedDate，或对账时的 revocationDate；0 = 无，取处理时刻
	RevocationType       string
	RevocationPercentage int32
	Source               string // webhook | reconcile
}

const appleRevocationFamilyRevoke = "FAMILY_REVOKE"

// appleRefundOutcome 供调用方与测试观察。
type appleRefundOutcome struct {
	Adopted       bool
	ReRefund      bool
	Cut           int64
	MarkedRevoked bool
	Followup      *refundFollowup
	UserID        uint64
}

// applyAppleRefund 处理 REFUND / REVOKE（webhook）或对账发现的 Revoked。取代旧的
// revokeSubscription。见 spec §3.3。
func applyAppleRefund(ctx context.Context, otx string, txn *appstore.TransactionInfo, ev appleRefundEvidence) (*appleRefundOutcome, error) {
	if txn == nil || txn.TransactionId == "" {
		return nil, errors.New("apple refund without transaction")
	}
	var out *appleRefundOutcome
	var alerts []string
	err := withDeadlockRetry(ctx, 3, func(tx *gorm.DB) error {
		out, alerts = &appleRefundOutcome{}, nil
		now := time.Now().Unix()
		appleProvided := ev.SignedAt > 0
		signedAt := ev.SignedAt
		if !appleProvided {
			signedAt = time.Now().UnixMilli()
		}

		// 1. 锁序：订阅 → 用户 → 订单 → 邀请人 → 分销配置（与入账路径一致）。
		var sub Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&Subscription{Provider: SubscriptionProviderApple, ProviderSubscriptionID: otx}).First(&sub).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				log.Infof(ctx, "[AppleRefund] otx %s not bound, nothing to refund", otx)
				return nil
			}
			return err
		}
		out.UserID = sub.UserID

		// 2. 采纳判定。
		var r AppleRefund
		rErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("transaction_id = ?", txn.TransactionId).First(&r).Error
		exists := rErr == nil
		if rErr != nil && !errors.Is(rErr, gorm.ErrRecordNotFound) {
			return rErr
		}
		reRefund := false
		if exists {
			if r.Active {
				// 已在退款状态：不重复收回。只记下 Apple 给出的更晚的退款证据（供撤销探测冷却与
				// 撤销时间戳使用）；now 兜底不推后——否则每周对账会让冷却永不到期。
				if appleProvided && signedAt > r.RefundSignedAt {
					return tx.Model(&AppleRefund{}).Where("id = ?", r.ID).Update("refund_signed_at", signedAt).Error
				}
				return nil
			}
			if signedAt <= r.ReversedSignedAt {
				// 证据早于已采纳的撤销 → 迟到的旧退款。对账时 Apple 仍报 Revoked 而我们认为已撤销：
				// 可能是撤销探测读到了旧数据，告警一次，不自动处理。
				if ev.Source == "reconcile" && r.ConflictAlertedAt == 0 {
					if err := tx.Model(&AppleRefund{}).Where("id = ?", r.ID).Update("conflict_alerted_at", now).Error; err != nil {
						return err
					}
					alerts = append(alerts, fmt.Sprintf("[APPLE-REFUND-CONFLICT] txn %s（用户 %d）：本地已按撤销退款恢复，Apple 仍报已退款且证据早于撤销——请人工核对是否需要再次收回",
						txn.TransactionId, sub.UserID))
				}
				return nil
			}
			// 撤销之后 Apple 再次退款。上一轮确实收过也确实还过时，才按"最多收回还回去且未用掉的"封顶。
			reRefund = r.CutSeconds > 0 && r.RestoredAt > 0
		}
		out.Adopted, out.ReRefund = true, reRefund

		// 3. 锁用户。
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "expired_at", "brand").First(&user, sub.UserID).Error; err != nil {
			return fmt.Errorf("lock user %d: %w", sub.UserID, err)
		}

		// 4–5. 入账与订单。
		var credits int64
		if err := tx.Model(&SubscriptionCredit{}).
			Where(&SubscriptionCredit{Provider: SubscriptionProviderApple, TransactionID: txn.TransactionId}).
			Count(&credits).Error; err != nil {
			return err
		}
		credited := credits > 0
		ord, err := revokeIAPOrderCashbackInTx(ctx, tx, txn.TransactionId)
		if err != nil {
			return err
		}

		// 6. 付费时长。
		tExp := txn.ExpiresDate / 1000
		periodLen := tExp - txn.PurchaseDate/1000
		paid := subscriptionPaidThrough(&sub)
		newer := sub.CurrentPeriodEnd > tExp // 更新的一期已入账：提前扣续费 / 迟到处理 / 复活覆盖过 CPE
		if txn.ExpiresDate == 0 {
			alerts = append(alerts, fmt.Sprintf("[APPLE-REFUND] txn %s 缺少 expiresDate，按订阅周期末兜底", txn.TransactionId))
			tExp, newer, periodLen = sub.CurrentPeriodEnd, false, paid-now
		}
		base := credited && ev.RevocationType != appleRevocationFamilyRevoke &&
			txn.InAppOwnershipType != appstore.OwnershipType_FAMILY_SHARED && !ord.WalletRefunded
		remaining := max(user.ExpiredAt-now, 0)
		var cut int64
		var eligible bool
		if reRefund {
			// 撤销时还回的时长可能落在 tExp 之后，不能套 tExp>now 门。
			eligible = base
			if eligible {
				cut = min(max(r.CutSeconds-(now-r.RestoredAt), 0), remaining)
			}
		} else {
			eligible = base && tExp > now
			if eligible {
				if newer {
					// 账本里被退期之后还有新付的一期，paid−now 包含新一期，只收被退期日历上剩下的。
					// （此分支 periodLen 冗余：tExp−now ≤ periodLen 恒成立。）
					cut = max(min(tExp-now, remaining), 0)
				} else {
					cut = max(min(paid-now, remaining, periodLen), 0)
				}
			}
		}
		if cut > 0 {
			if err := tx.Model(&User{}).Where("id = ?", user.ID).
				Update("expired_at", gorm.Expr("expired_at - ?", cut)).Error; err != nil {
				return fmt.Errorf("claw back user %d: %w", user.ID, err)
			}
			newPaid := paid - cut
			if reRefund {
				newPaid = max(max(sub.PaidThrough, now)-cut, now)
			}
			if err := tx.Model(&Subscription{}).Where("id = ?", sub.ID).Update("paid_through", newPaid).Error; err != nil {
				return fmt.Errorf("update paid_through for sub %d: %w", sub.ID, err)
			}
			if err := tx.Create(&UserProHistory{
				UserID: user.ID, Type: VipRefund, ReferenceID: sub.ID, Days: -ceilDays(cut),
				Reason: fmt.Sprintf("apple 退款收回 - %s", txn.TransactionId),
			}).Error; err != nil {
				return err
			}
		}
		if eligible && !reRefund && ev.RevocationPercentage > 0 && periodLen > 0 {
			expected := (tExp - now) * 100000 / periodLen
			if diff := int64(ev.RevocationPercentage) - expected; diff > 10000 || diff < -10000 {
				alerts = append(alerts, fmt.Sprintf("[APPLE-REFUND-PCT] txn %s：Apple 退款比例 %.1f%% 与剩余时间比例 %.1f%% 相差超过 10 个百分点，请核对是否多收",
					txn.TransactionId, float64(ev.RevocationPercentage)/1000, float64(expected)/1000))
			}
		}

		// 6b. 邀请 / 分销后续（付费时长已写库，邀请扣减基于重读值）。
		keys := purchaseKeys{AppleTxnID: txn.TransactionId}
		if ord.Order != nil {
			keys.OrderID = ord.Order.ID
		}
		fu, err := onPaidOrderRefundedInTx(ctx, tx, sub.UserID, keys, ord.Order, false)
		if err != nil {
			return err
		}
		out.Followup = fu
		alerts = append(alerts, fu.Alerts...)

		// 8. 状态：被退的就是最新一期才置 revoked；不改 current_period_end。
		markRevoked := eligible && !newer
		if markRevoked {
			if err := tx.Model(&Subscription{}).Where("id = ?", sub.ID).Update("status", "revoked").Error; err != nil {
				return err
			}
		}
		out.Cut, out.MarkedRevoked = cut, markRevoked

		// 9. 记录。
		revocationDate := txn.RevocationDate
		if revocationDate == 0 {
			revocationDate = signedAt
		}
		row := AppleRefund{
			UserID: sub.UserID, SubscriptionID: sub.ID, OriginalTransactionID: otx, TransactionID: txn.TransactionId,
			RefundSignedAt: signedAt, ReversedSignedAt: r.ReversedSignedAt, RestoredAt: r.RestoredAt, Active: true,
			RevocationDate: revocationDate, RevocationReason: txn.RevocationReason,
			RevocationType: ev.RevocationType, RevocationPercentage: ev.RevocationPercentage,
			Credited: credited, CutSeconds: cut, MarkedRevoked: markRevoked, Source: ev.Source,
		}
		if exists {
			row.ID, row.CreatedAt = r.ID, r.CreatedAt
		}
		if err := tx.Save(&row).Error; err != nil {
			return fmt.Errorf("save apple refund %s: %w", txn.TransactionId, err)
		}

		var count int64
		tx.Model(&AppleRefund{}).Where("user_id = ?", sub.UserID).Count(&count)
		alerts = append(alerts, fmt.Sprintf("[APPLE-REFUND] 品牌 %s 用户 %d 交易 %s：收回 %d 天%s，邀请奖励%s，分销计数%s，该用户 Apple 退款累计 %d 次（来源 %s）",
			user.Brand, sub.UserID, txn.TransactionId, ceilDays(cut), map[bool]string{true: "（撤销后再次退款）", false: ""}[reRefund],
			describeInviteFollowup(fu), describeRetailerFollowup(fu), count, ev.Source))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sendBillingAlerts(ctx, alerts)
	if out.Adopted {
		alertKaituRefundRate(ctx)
	}
	return out, nil
}

func describeInviteFollowup(fu *refundFollowup) string {
	switch {
	case fu == nil:
		return "无"
	case fu.InviteReversed:
		return fmt.Sprintf("已撤回（被邀请人 −%d 天，邀请人 %d −%d 天）", ceilDays(fu.InviteeCut), fu.InviterUserID, ceilDays(fu.InviterCut))
	case fu.InviteReanchored:
		return "保留（仍持有其它合格购买，已改锚）"
	}
	return "无"
}

func describeRetailerFollowup(fu *refundFollowup) string {
	if fu == nil || fu.RetailerAdjusted == "" {
		return "无变化"
	}
	return fu.RetailerAdjusted
}

// alertKaituRefundRate 开途 30 天内 Apple 退款 ≥3 次时提示考虑提前上线 Consumption 应答（spec §5）。
func alertKaituRefundRate(ctx context.Context) {
	var n int64
	since := time.Now().AddDate(0, 0, -30)
	if err := getDB().Model(&AppleRefund{}).
		Joins("JOIN users ON users.id = apple_refunds.user_id").
		Where("users.brand = ? AND apple_refunds.created_at >= ?", string(BrandKaitu), since).
		Count(&n).Error; err != nil {
		log.Warnf(ctx, "[AppleRefund] refund-rate query failed: %v", err)
		return
	}
	if n >= 3 {
		alertBilling(ctx, fmt.Sprintf("[APPLE-REFUND-RATE] 开途 30 天内 Apple 退款 %d 次——考虑提前上线 Consumption 应答（spec 2026-10-08 §5）", n))
	}
}

// reverseAppleRefund 处理 REFUND_REVERSED（webhook）或对账探测到的撤销退款。见 spec §3.6。
func reverseAppleRefund(ctx context.Context, otx string, txn *appstore.TransactionInfo, signedAt int64, source string) error {
	if txn == nil || txn.TransactionId == "" {
		return errors.New("apple refund reversal without transaction")
	}
	var alerts []string
	err := withDeadlockRetry(ctx, 3, func(tx *gorm.DB) error {
		alerts = nil
		now := time.Now().Unix()
		var sub Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&Subscription{Provider: SubscriptionProviderApple, ProviderSubscriptionID: otx}).First(&sub).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var r AppleRefund
		rErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("transaction_id = ?", txn.TransactionId).First(&r).Error
		if errors.Is(rErr, gorm.ErrRecordNotFound) {
			// 撤销先于退款到达：建占位行，之后证据更早的退款会被判为迟到。
			return tx.Create(&AppleRefund{
				UserID: sub.UserID, SubscriptionID: sub.ID, OriginalTransactionID: otx, TransactionID: txn.TransactionId,
				ReversedSignedAt: signedAt, Active: false, Source: source, Note: "reversal before refund",
			}).Error
		}
		if rErr != nil {
			return rErr
		}
		if !r.Active {
			if signedAt > r.ReversedSignedAt {
				return tx.Model(&AppleRefund{}).Where("id = ?", r.ID).Update("reversed_signed_at", signedAt).Error
			}
			return nil
		}
		if signedAt <= r.RefundSignedAt {
			return nil // 撤销证据早于当前退款：迟到的旧撤销
		}

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&User{}, sub.UserID).Error; err != nil {
			return fmt.Errorf("lock user %d: %w", sub.UserID, err)
		}
		// 全额还：Apple 撤销退款即重新收了这笔钱，空窗不让用户承担。
		if r.CutSeconds > 0 {
			if err := tx.Model(&User{}).Where("id = ?", sub.UserID).
				Update("expired_at", gorm.Expr("GREATEST(expired_at, ?) + ?", now, r.CutSeconds)).Error; err != nil {
				return err
			}
			if err := tx.Model(&Subscription{}).Where("id = ?", sub.ID).
				Update("paid_through", gorm.Expr("GREATEST(paid_through, ?) + ?", now, r.CutSeconds)).Error; err != nil {
				return err
			}
			if err := tx.Create(&UserProHistory{
				UserID: sub.UserID, Type: VipAppleSub, ReferenceID: sub.ID, Days: int(r.CutSeconds / 86400),
				Reason: fmt.Sprintf("apple 撤销退款，恢复会员 - %s", txn.TransactionId),
			}).Error; err != nil {
				return err
			}
		}
		if sub.Status == "revoked" && r.MarkedRevoked {
			if err := tx.Model(&Subscription{}).Where("id = ?", sub.ID).
				Update("status", deriveActiveOrExpired(sub.CurrentPeriodEnd, now)).Error; err != nil {
				return err
			}
		}
		keys := purchaseKeys{AppleTxnID: txn.TransactionId}
		var order Order
		if err := tx.Select("id").Where(&Order{AppleTransactionID: txn.TransactionId}).First(&order).Error; err == nil {
			keys.OrderID = order.ID
		}
		if err := restoreInviteGrantForPurchaseInTx(tx, sub.UserID, keys, now); err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", sub.UserID).Update("is_first_order_done", true).Error; err != nil {
			return err
		}
		if err := tx.Model(&AppleRefund{}).Where("id = ?", r.ID).Updates(map[string]any{
			"active": false, "reversed_signed_at": signedAt, "restored_at": now,
		}).Error; err != nil {
			return err
		}
		alerts = append(alerts, fmt.Sprintf("[APPLE-REFUND-REVERSED] 用户 %d 交易 %s：Apple 撤销了退款，已自动恢复 %d 天会员与邀请奖励。需人工：①订单仍标记已退款（如需改回请同步处理）②分销返现未恢复（如手工补发，Apple 再次退款时需手工再撤）③分销付费人数未恢复（来源 %s）",
			sub.UserID, txn.TransactionId, r.CutSeconds/86400, source))
		return nil
	})
	if err != nil {
		return err
	}
	sendBillingAlerts(ctx, alerts)
	return nil
}

// restoreInviteGrantForPurchaseInTx 撤销退款时把这笔购买撤回的邀请奖励自动还给双方，并清掉
// Reversed——之后 Apple 若再次退款，onPaidOrderRefundedInTx 能再撤一次。
func restoreInviteGrantForPurchaseInTx(tx *gorm.DB, inviteeID uint64, keys purchaseKeys, now int64) error {
	var g InviteRewardGrant
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("invitee_user_id = ? AND reversed = ?", inviteeID, true).First(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !keys.matches(g.TriggerKind, g.TriggerRef) {
		return nil
	}
	for _, p := range []struct {
		userID uint64
		secs   int64
	}{{inviteeID, g.InviteeCutSeconds}, {g.InviterUserID, g.InviterCutSeconds}} {
		if p.secs <= 0 || p.userID == 0 {
			continue
		}
		if err := tx.Model(&User{}).Where("id = ?", p.userID).
			Update("expired_at", gorm.Expr("GREATEST(expired_at, ?) + ?", now, p.secs)).Error; err != nil {
			return err
		}
		if err := tx.Create(&UserProHistory{
			UserID: p.userID, Type: VipSystemGrant, ReferenceID: g.InviteCodeID, Days: int(p.secs / 86400),
			Reason: "Apple 撤销退款，邀请奖励恢复",
		}).Error; err != nil {
			return err
		}
	}
	return tx.Model(&InviteRewardGrant{}).Where("id = ?", g.ID).Updates(map[string]any{
		"reversed": false, "invitee_cut_seconds": 0, "inviter_cut_seconds": 0,
	}).Error
}

// decodeAppleRevocation 从通知里的 signedTransactionInfo 补读 qtoolkit 尚未建模的
// revocationType / revocationPercentage（2025-12 起 Apple 下发）。JWS 已由 webhook 校验过，
// 这里只解 payload；解不出返回零值（只用于审计与比例告警，不影响收回规则）。
func decodeAppleRevocation(signedTxn string) (revType string, pct int32) {
	parts := strings.Split(signedTxn, ".")
	if len(parts) != 3 {
		return "", 0
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", 0
	}
	var v struct {
		RevocationType       string `json:"revocationType"`
		RevocationPercentage int32  `json:"revocationPercentage"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return "", 0
	}
	return v.RevocationType, v.RevocationPercentage
}
