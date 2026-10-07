package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/invoicepayment"
	"github.com/stripe/stripe-go/v82/subscription"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Stripe 收回：全额退款、拒付、撤回执行器的共同落点（spec 2026-10-07 A 期 §1–§2）。
//
// revoked 是终态：一旦置 revoked，creditStripeInvoice 不再加时长、对账 cover-through
// 不再延长、subscription.updated/deleted 不再改状态。收回原语自己锁订阅行再锁用户行，
// 与 creditStripeInvoice 同序，不复用 Apple 的 revokeSubscription（它不锁订阅行、
// 加锁顺序相反、原因文案写死中文——overleap 用户在 /api/user/pro-histories 看得到）。

// earlyEndToleranceSec：subscription.deleted 的 ended_at 比事件自身周期末早超过这么多，
// 才算"提前结束"（后台立即取消 / 期中 cancel_at）。期末自然结束时两者几乎相等。
const earlyEndToleranceSec = 3600

// SDK 测试替换点（同 stripeNewCheckoutSession 模式；key 逐调用传入）。
var (
	// stripeSubscriptionByPaymentIntent：PI → 所属订阅 id。不是订阅扣款返回 ("", nil)。
	stripeSubscriptionByPaymentIntent = func(key, pi string) (string, error) {
		params := &stripe.InvoicePaymentListParams{
			Payment: &stripe.InvoicePaymentListPaymentParams{
				Type:          stripe.String("payment_intent"),
				PaymentIntent: stripe.String(pi),
			},
			Status: stripe.String("paid"),
		}
		params.AddExpand("data.invoice")
		it := invoicepayment.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}.List(params)
		for it.Next() {
			ip := it.InvoicePayment()
			if ip.Invoice != nil && ip.Invoice.Parent != nil && ip.Invoice.Parent.SubscriptionDetails != nil &&
				ip.Invoice.Parent.SubscriptionDetails.Subscription != nil {
				return ip.Invoice.Parent.SubscriptionDetails.Subscription.ID, nil
			}
		}
		// 迭代器出错必须上抛：吞掉就会把"查询失败"误判成"不是订阅扣款"→ 200 → 收回静默丢失。
		if err := it.Err(); err != nil {
			return "", err
		}
		return "", nil
	}

	// stripeCancelSubscription：立即取消，不按比例退、不出结算单。
	stripeCancelSubscription = func(subID, comment string) error {
		key := stripeSecretKey()
		if key == "" {
			return fmt.Errorf("stripe secret key unavailable")
		}
		_, err := subscription.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}.Cancel(subID,
			&stripe.SubscriptionCancelParams{
				Prorate:             stripe.Bool(false),
				InvoiceNow:          stripe.Bool(false),
				CancellationDetails: &stripe.SubscriptionCancelCancellationDetailsParams{Comment: stripe.String(comment)},
			})
		return err
	}
)

// alertStripeRevoke 收回相关告警出口（var 供测试替换）。tag 形如 [STRIPE-REFUND]。
var alertStripeRevoke = func(ctx context.Context, tag, format string, args ...any) {
	msg := tag + " " + fmt.Sprintf(format, args...)
	log.Errorf(ctx, "%s", msg)
	if err := slack.Send("alert", msg); err != nil {
		log.Errorf(ctx, "failed to send stripe revoke alert: %v", err)
	}
}

func isStripeResourceMissing(err error) bool {
	var se *stripe.Error
	return errors.As(err, &se) && se.Code == stripe.ErrorCodeResourceMissing
}

// revokeStripeSubscriptionInTx 收回一条 Stripe 订阅撑着的会员并置 revoked。幂等。
// found=false：本地没有这条订阅；revokedNow=false 且 found=true：之前已 revoked。
// 收回规则同 Apple：用户到期落在 (now, sub.CurrentPeriodEnd] 才砍到 now——叠加的赠送
// 时长不误伤。reason 写进 UserProHistory，必须是英文。
func revokeStripeSubscriptionInTx(ctx context.Context, tx *gorm.DB, providerSubID, reason string) (found, revokedNow bool, err error) {
	var sub Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(&Subscription{Provider: SubscriptionProviderStripe, ProviderSubscriptionID: providerSubID}).
		First(&sub).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, false, nil
		}
		return false, false, err
	}
	if sub.Status == "revoked" {
		return true, false, nil
	}
	var user User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, sub.UserID).Error; err != nil {
		return true, false, fmt.Errorf("lock user %d: %w", sub.UserID, err)
	}
	now := time.Now().Unix()
	if user.ExpiredAt > now && user.ExpiredAt <= sub.CurrentPeriodEnd {
		cut := user.ExpiredAt - now
		if err := tx.Model(&User{}).Where("id = ?", user.ID).Update("expired_at", now).Error; err != nil {
			return true, false, err
		}
		if err := tx.Create(&UserProHistory{
			UserID:      user.ID,
			Type:        VipRefund,
			ReferenceID: sub.ID,
			Days:        -int(cut / 86400),
			Reason:      reason,
		}).Error; err != nil {
			return true, false, err
		}
		log.Infof(ctx, "[StripeRevoke] user %d clawed back -%ds (sub %s): %s", user.ID, cut, providerSubID, reason)
	} else {
		log.Infof(ctx, "[StripeRevoke] user %d no clawback (expiredAt=%d periodEnd=%d now=%d), sub %s marked revoked",
			user.ID, user.ExpiredAt, sub.CurrentPeriodEnd, now, providerSubID)
	}
	if err := tx.Model(&Subscription{}).Where("id = ?", sub.ID).
		Updates(map[string]any{"status": "revoked", "auto_renew": false}).Error; err != nil {
		return true, false, err
	}
	return true, true, nil
}

// revokeStripeSubscription 跑收回原语；本地没有订阅行（首张 invoice 入账曾失败）时
// 按远端订阅建一条 revoked 墓碑行——之后 Stripe 重投那张 invoice 会命中
// creditStripeInvoice 的 revoked 门，不会补发整期会员。
func revokeStripeSubscription(ctx context.Context, subID, reason string) (revokedNow bool, err error) {
	run := func() (bool, bool, error) {
		var found, now bool
		err := withDeadlockRetry(ctx, 3, func(tx *gorm.DB) error {
			var e error
			found, now, e = revokeStripeSubscriptionInTx(ctx, tx, subID, reason)
			return e
		})
		return found, now, err
	}
	found, revokedNow, err := run()
	if err != nil || found {
		return revokedNow, err
	}

	remote, err := stripeFetchSubscription(subID)
	if err != nil {
		if isStripeResourceMissing(err) {
			alertStripeRevoke(ctx, "[STRIPE-REVOKE]", "sub %s unknown locally and missing at Stripe — nothing to revoke (%s)", subID, reason)
			return false, nil
		}
		return false, fmt.Errorf("fetch remote sub %s for tombstone: %w", subID, err)
	}
	created, err := createStripeTombstone(ctx, remote)
	if err != nil {
		if util.DbIsDuplicatedErr(err) {
			// 并发的 invoice.paid 先建了行：对那一行正常收回。
			found, revokedNow, err = run()
			if err == nil && !found {
				err = fmt.Errorf("sub %s: tombstone collided but row not found", subID)
			}
			return revokedNow, err
		}
		return false, err
	}
	if created {
		log.Infof(ctx, "[StripeRevoke] tombstone revoked row created for sub %s", subID)
	}
	return false, nil
}

// createStripeTombstone 按远端订阅建一条 revoked 行。user_uuid 缺失或用户品牌不走 Stripe
// → 告警、不建（返回 false, nil）。
func createStripeTombstone(ctx context.Context, remote *stripe.Subscription) (bool, error) {
	userUUID := remote.Metadata["user_uuid"]
	if userUUID == "" {
		alertStripeRevoke(ctx, "[STRIPE-REVOKE]", "sub %s has no local row and no metadata.user_uuid — tombstone not created, manual check", remote.ID)
		return false, nil
	}
	var u User
	if err := getDB().Where(&User{UUID: userUUID}).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			alertStripeRevoke(ctx, "[STRIPE-REVOKE]", "sub %s metadata.user_uuid %s not found — tombstone not created", remote.ID, userUUID)
			return false, nil
		}
		return false, err
	}
	if !Brand(u.Brand).Config().AllowsPayment(PayChannelStripe) {
		alertStripeRevoke(ctx, "[STRIPE-REVOKE]", "sub %s belongs to user %d of brand %s (no stripe channel) — tombstone not created", remote.ID, u.ID, u.Brand)
		return false, nil
	}
	row := &Subscription{
		UserID:                 u.ID,
		Provider:               SubscriptionProviderStripe,
		ProviderSubscriptionID: remote.ID,
		Status:                 "revoked",
		AutoRenew:              false,
		Environment:            "sandbox",
	}
	if remote.Livemode {
		row.Environment = "production"
	}
	if remote.Customer != nil {
		row.ProviderCustomerID = remote.Customer.ID
	}
	if remote.Items != nil && len(remote.Items.Data) > 0 && remote.Items.Data[0] != nil {
		item := remote.Items.Data[0]
		row.CurrentPeriodEnd = item.CurrentPeriodEnd
		if item.Price != nil {
			row.ProductID = item.Price.ID
		}
	}
	if err := getDB().Create(row).Error; err != nil {
		return false, err
	}
	return true, nil
}

// cancelStripeSubscriptionIfLive 取消 Stripe 订阅（已取消 / 不存在视为完成）。
func cancelStripeSubscriptionIfLive(ctx context.Context, subID, reason string) error {
	remote, err := stripeFetchSubscription(subID)
	if err != nil {
		if isStripeResourceMissing(err) {
			return nil
		}
		return fmt.Errorf("fetch sub %s before cancel: %w", subID, err)
	}
	if stripeSubEnded(remote.Status) {
		return nil
	}
	if err := stripeCancelSubscription(subID, reason); err != nil {
		if isStripeResourceMissing(err) {
			alertStripeRevoke(ctx, "[STRIPE-REVOKE]", "cancel sub %s: resource_missing, treated as done", subID)
			return nil
		}
		// 并发取消的另一方可能先到：再看一眼。
		if again, gerr := stripeFetchSubscription(subID); gerr == nil && stripeSubEnded(again.Status) {
			return nil
		}
		return fmt.Errorf("cancel sub %s: %w", subID, err)
	}
	return nil
}

func stripeSubEnded(s stripe.SubscriptionStatus) bool {
	return s == stripe.SubscriptionStatusCanceled || s == stripe.SubscriptionStatusIncompleteExpired
}

// statutoryRefundByPI 找这笔付款上我们自己的有效撤回记录（作废的不算）。
func statutoryRefundByPI(pi string) (*StatutoryRefund, error) {
	if pi == "" {
		return nil, nil
	}
	var sr StatutoryRefund
	err := getDB().Where("payment_intent_id = ? AND status IN ?", pi, []string{"pending", "done"}).
		Order("id DESC").First(&sr).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sr, nil
}

// withdrawalRevokeReason 撤回请求的收回原因（英文，用户可见）。
func withdrawalRevokeReason(requestID string) string {
	var rows []StatutoryRefund
	getDB().Where("request_id = ?", requestID).Order("id ASC").Find(&rows)
	for _, r := range rows {
		if r.Kind == "primary" {
			if r.Mode == "termination" {
				return "Service terminated by Overleap - " + r.ProviderSubscriptionID
			}
			return "Withdrawal within 14 days - " + r.InvoiceID
		}
	}
	return "Withdrawal within 14 days - " + requestID
}

// revokeStripeForChargeLoss 全额退款 / 拒付的共同处理：收回 → 取消 → 告警。
// 返回 error → webhook 500 → Stripe 重投；收回幂等，重投只重试没完成的取消。
func revokeStripeForChargeLoss(ctx context.Context, pi, reason, tag, detail string) error {
	if pi == "" {
		alertStripeRevoke(ctx, tag, "%s — no payment_intent, cannot attribute; manual follow-up", detail)
		return nil
	}

	var subID string
	own, err := statutoryRefundByPI(pi)
	if err != nil {
		return err
	}
	if own != nil {
		// 我们自己的撤回退款（全额撤回时 webhook 可能先于执行器的收回到达）。
		subID = own.ProviderSubscriptionID
		reason = withdrawalRevokeReason(own.RequestID)
		tag = "[WITHDRAWAL]"
	} else {
		subID, err = stripeSubscriptionByPaymentIntent(stripeSecretKey(), pi)
		if err != nil {
			return fmt.Errorf("lookup subscription for pi %s: %w", pi, err)
		}
		if subID == "" {
			alertStripeRevoke(ctx, tag, "%s pi=%s — not a subscription charge; manual follow-up", detail, pi)
			return nil
		}
	}

	revokedNow, err := revokeStripeSubscription(ctx, subID, reason)
	if err != nil {
		return fmt.Errorf("revoke sub %s: %w", subID, err)
	}
	if err := cancelStripeSubscriptionIfLive(ctx, subID, reason); err != nil {
		alertStripeRevoke(ctx, tag, "%s sub=%s — membership revoked=%v but CANCEL FAILED (will retry): %v", detail, subID, revokedNow, err)
		return err
	}
	alertStripeRevoke(ctx, tag, "%s sub=%s — membership revoked (now=%v), subscription canceled", detail, subID, revokedNow)
	return nil
}

// handleStripeChargeRefunded：全额 → 收回 + 取消；部分 → 只告警。
func handleStripeChargeRefunded(ctx context.Context, raw []byte) error {
	var ch stripe.Charge
	if err := json.Unmarshal(raw, &ch); err != nil {
		return fmt.Errorf("parse charge: %w", err)
	}
	pi := ""
	if ch.PaymentIntent != nil {
		pi = ch.PaymentIntent.ID
	}
	detail := fmt.Sprintf("charge=%s refunded=%d/%d %s customer=%s", ch.ID, ch.AmountRefunded, ch.Amount,
		string(ch.Currency), stripeCustomerID(ch.Customer))
	full := ch.Refunded || (ch.Amount > 0 && ch.AmountRefunded >= ch.Amount)
	if !full {
		own, err := statutoryRefundByPI(pi)
		if err != nil {
			return err
		}
		if own != nil {
			alertStripeRevoke(ctx, "[WITHDRAWAL]", "%s — partial refund from withdrawal request %s", detail, own.RequestID)
			return nil
		}
		alertStripeRevoke(ctx, "[STRIPE-REFUND]", "%s — partial refund, membership kept", detail)
		return nil
	}
	return revokeStripeForChargeLoss(ctx, pi, "Stripe full refund - "+ch.ID, "[STRIPE-REFUND]", detail)
}

// handleStripeDisputeCreated：任何拒付（含询问）→ 收回 + 取消。
func handleStripeDisputeCreated(ctx context.Context, raw []byte) error {
	var d stripe.Dispute
	if err := json.Unmarshal(raw, &d); err != nil {
		return fmt.Errorf("parse dispute: %w", err)
	}
	pi, chargeID := "", ""
	if d.PaymentIntent != nil {
		pi = d.PaymentIntent.ID
	}
	if d.Charge != nil {
		chargeID = d.Charge.ID
	}
	detail := fmt.Sprintf("dispute=%s charge=%s amount=%d %s reason=%s status=%s",
		d.ID, chargeID, d.Amount, string(d.Currency), string(d.Reason), string(d.Status))
	return revokeStripeForChargeLoss(ctx, pi, "Stripe dispute - "+d.ID, "[STRIPE-DISPUTE]", detail)
}

// handleStripeDisputeClosed：只告警（胜诉后客服按条款 7.7 恢复，并人工补退被跳过的撤回款）。
func handleStripeDisputeClosed(ctx context.Context, raw []byte) error {
	var d stripe.Dispute
	if err := json.Unmarshal(raw, &d); err != nil {
		return fmt.Errorf("parse dispute: %w", err)
	}
	pi := ""
	if d.PaymentIntent != nil {
		pi = d.PaymentIntent.ID
	}
	linked := "none"
	if own, err := statutoryRefundByPI(pi); err != nil {
		return err
	} else if own != nil {
		linked = fmt.Sprintf("request=%s invoice=%s amount=%d refund=%q note=%q", own.RequestID, own.InvoiceID, own.Amount, own.StripeRefundID, own.RefundNote)
	}
	alertStripeRevoke(ctx, "[STRIPE-DISPUTE-CLOSED]", "dispute=%s status=%s amount=%d %s pi=%s withdrawal=%s — if won: restore access (Terms 7.7) and pay any skipped withdrawal refund",
		d.ID, string(d.Status), d.Amount, string(d.Currency), pi, linked)
	return nil
}
