package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// stripeSubStatus 把 Stripe subscription.status 映射到本仓库 Subscription.Status 词表
// （active|grace|billing_retry|expired|revoked）。纯函数。
// 返回 "" 表示"不改"（未知/不适用状态）。绝不返回 "revoked"——revoked 专属退款语义，
// 由人工/后续 admin 流程落地，webhook 状态同步永不触碰（对标 deriveVerifiedStatus 的 terminal 规则）。
func stripeSubStatus(s stripe.SubscriptionStatus) string {
	switch s {
	case stripe.SubscriptionStatusActive, stripe.SubscriptionStatusTrialing:
		return "active"
	case stripe.SubscriptionStatusPastDue:
		return "billing_retry"
	case stripe.SubscriptionStatusCanceled, stripe.SubscriptionStatusUnpaid,
		stripe.SubscriptionStatusIncompleteExpired, stripe.SubscriptionStatusPaused:
		return "expired"
	default:
		return ""
	}
}

// errNotSubscriptionInvoice 标记"这张 invoice 本就不属订阅入账语义"（一次性账单等）——
// 这是 extractStripeInvoiceFacts 唯一可以安全忽略的失败。其余 extract 失败一概是**形态
// 解析失败**：钱已经收了，事实却读不出来。两者性质相反，调用方必须能区分：
// 前者 200 忽略，后者告警 + 500 让 Stripe 重投，绝不静默吞掉一笔已付款。
var errNotSubscriptionInvoice = errors.New("invoice has no parent subscription")

// alertStripeCredit 是 Stripe 入账侧非品牌类 fail-loud 哨兵的统一告警出口（形态解析失败、
// 实付 price 与 plan 不符）。与 alertPaymentBrandMismatch 同级同构：error 日志 + Slack
// "alert" 频道，best-effort 不阻断主流程，var 形态供测试替换。
var alertStripeCredit = func(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Errorf(ctx, "%s", msg)
	if err := slack.Send("alert", "[STRIPE-CREDIT] "+msg); err != nil {
		log.Errorf(ctx, "failed to send stripe credit alert: %v", err)
	}
}

// stripeInvoiceFacts 是入账所需字段的规范载体。extractStripeInvoiceFacts 是从
// stripe.Invoice 提取它的唯一适配点——隔离 SDK/API 版本形态差异（basil 把
// subscription 挪进 invoice.parent.subscription_details）。升级 stripe-go 只改这里。
type stripeInvoiceFacts struct {
	InvoiceID      string
	SubscriptionID string
	CustomerID     string
	UserUUID       string // subscription_data.metadata.user_uuid（checkout 创建时烘焙）
	PlanPID        string // subscription_data.metadata.plan_pid（订阅创建那一刻的**快照**，非实付）
	PriceID        string // 计费周期那条 line 实际收的 price id —— 实付真相，用于与 PlanPID 快照对账
	PeriodStart    int64  // unix 秒（invoice line period；basil 后订阅级 period 已删）
	PeriodEnd      int64  // unix 秒
	Livemode       bool
}

// stripeLinePriceID 取 line 的 price id（basil：line.pricing.price_details.price）。
// 取不到返回 ""——调用方（creditStripeInvoice 对账哨兵）把"读不出"与"对不上"同等拒绝，
// 绝不留"无法校验就放行"的口子。
func stripeLinePriceID(line *stripe.InvoiceLineItem) string {
	if line.Pricing == nil || line.Pricing.PriceDetails == nil {
		return ""
	}
	return line.Pricing.PriceDetails.Price
}

func extractStripeInvoiceFacts(inv *stripe.Invoice) (*stripeInvoiceFacts, error) {
	if inv == nil || inv.ID == "" {
		return nil, fmt.Errorf("nil or empty invoice")
	}
	f := &stripeInvoiceFacts{InvoiceID: inv.ID, Livemode: inv.Livemode}
	if inv.Customer != nil {
		f.CustomerID = inv.Customer.ID
	}
	if inv.Parent != nil && inv.Parent.SubscriptionDetails != nil {
		sd := inv.Parent.SubscriptionDetails
		if sd.Subscription != nil {
			f.SubscriptionID = sd.Subscription.ID
		}
		f.UserUUID = sd.Metadata["user_uuid"]
		f.PlanPID = sd.Metadata["plan_pid"]
	}
	if f.SubscriptionID == "" {
		// 唯一可忽略的情形：本就不是订阅 invoice。用 sentinel 包裹保留 invoice id。
		return nil, fmt.Errorf("invoice %s is not a subscription invoice: %w", inv.ID, errNotSubscriptionInvoice)
	}
	if inv.Lines != nil {
		for _, line := range inv.Lines.Data {
			if line == nil || line.Period == nil {
				continue
			}
			if line.Period.End > f.PeriodEnd {
				f.PeriodEnd = line.Period.End
				f.PeriodStart = line.Period.Start
				f.PriceID = stripeLinePriceID(line) // 必须与 period 取自同一条 line
			}
		}
	}
	if f.PeriodEnd == 0 {
		return nil, fmt.Errorf("invoice %s has no line period", inv.ID)
	}
	return f, nil
}

// creditStripeInvoice is the single Stripe→ledger entry point（对标 creditAppleTransaction）。
// 它 (1) 首张 invoice 靠 metadata.user_uuid 绑定订阅归属，此后订阅行 UserID 权威，
// 绝不 re-bind（INV9）；(2) 按 (provider, transaction_id=invoice.ID) 去重，每张 invoice
// 至多入账一次（INV1）；(3) 首购 applyGiftCredit / 续费 applyRenewalCredit 叠加入账，
// 礼赠时长永不被吸收（INV3）；(4) 刷新订阅行的 plan-state。必须在事务内调用；
// 锁订阅行 + 用户行。
func creditStripeInvoice(ctx context.Context, tx *gorm.DB, f *stripeInvoiceFacts) error {
	const provider = "stripe"

	// Load-or-create 订阅行（绑定键 = Stripe subscription id）。
	var sub Subscription
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(&Subscription{Provider: provider, ProviderSubscriptionID: f.SubscriptionID}).
		First(&sub).Error
	isFirst := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !isFirst {
		return err
	}

	// 归属解析：首张 invoice 靠 metadata.user_uuid（checkout 创建时烘焙，Stripe 原样回传）。
	var userID uint64
	if isFirst {
		if f.UserUUID == "" {
			return fmt.Errorf("invoice %s missing user_uuid metadata: refusing to bind", f.InvoiceID)
		}
		var u User
		if err := tx.Where(&User{UUID: f.UserUUID}).First(&u).Error; err != nil {
			return fmt.Errorf("resolve user %s for invoice %s: %w", f.UserUUID, f.InvoiceID, err)
		}
		userID = u.ID
	} else {
		userID = sub.UserID
		if f.UserUUID != "" {
			// 防御：metadata 与既有绑定冲突 → 拒绝（INV9，绝不 re-bind）。
			var u User
			if err := tx.Where(&User{UUID: f.UserUUID}).First(&u).Error; err == nil && u.ID != userID {
				return fmt.Errorf("subscription %s already bound to user %d, invoice %s metadata points to user %d",
					f.SubscriptionID, userID, f.InvoiceID, u.ID)
			}
		}
	}

	var user User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil {
		return fmt.Errorf("lock user %d: %w", userID, err)
	}

	// 品牌错配哨兵：stripe 是 overleap 专属支付渠道。线上命中即为 bug——告警并拒绝入账，
	// 绝不静默记账。返回 error → webhook 500 → Stripe 按重试策略重发，对同一张 invoice
	// 反复告警是 fail-loud 的设计取舍（同 wordgate/apple 哨兵），持续出现视为 page 级事件。
	if !Brand(user.Brand).Config().AllowsPayment(PayChannelStripe) {
		alertPaymentBrandMismatch(ctx, "brand-mismatch stripe credit: user %d brand %s does not allow stripe, invoice=%s",
			userID, user.Brand, f.InvoiceID)
		return fmt.Errorf("brand mismatch: user %d brand %s does not allow stripe channel", userID, user.Brand)
	}

	// plan：tier 与 sub.ProductID 的来源。品牌内查找、不过滤 is_active（下架不停续费入账）。
	plan := planByPIDForCredit(ctx, tx, f.PlanPID, Brand(user.Brand))
	if plan == nil {
		return fmt.Errorf("no plan %q for brand %s (invoice %s)", f.PlanPID, user.Brand, f.InvoiceID)
	}

	// 实付对账哨兵：f.PlanPID 来自 subscription_data.metadata —— 订阅创建那一刻的不可变
	// 快照。若只信快照，权益与这张 invoice 真正收的 price 之间零校验。触发路径：Stripe
	// Billing Portal 一旦启用 "Customers can switch plans"，用户自助换档后续费 invoice 仍带
	// 旧 plan_pid → 按旧档入账 + sub.ProductID 写旧 price + tier 反查报旧档，且静默绕过
	// validatePurchase（仓库硬规则：复购必须同档，变更档位走人工）。
	// 对不上就拒绝入账 + 告警（同品牌哨兵的 fail-loud）。首次绑定与续费同等适用。
	// plan.StripePriceID 为空 或 f.PriceID 读不出 → 一律视为对不上：无法校验绝不放行。
	if plan.StripePriceID == "" || f.PriceID != plan.StripePriceID {
		alertStripeCredit(ctx, "price mismatch: invoice %s charged price %q but plan %q (user %d) expects %q — refusing credit; check Billing Portal plan switching is disabled",
			f.InvoiceID, f.PriceID, plan.PID, userID, plan.StripePriceID)
		return fmt.Errorf("price mismatch on invoice %s: charged %q, plan %q expects %q",
			f.InvoiceID, f.PriceID, plan.PID, plan.StripePriceID)
	}

	now := time.Now().Unix()
	priorPeriodEnd := sub.CurrentPeriodEnd // 0 when first

	// Dedup（INV1）：每张 invoice 至多入账一次。
	var existing SubscriptionCredit
	dErr := tx.Where(&SubscriptionCredit{Provider: provider, TransactionID: f.InvoiceID}).First(&existing).Error
	alreadyCredited := dErr == nil
	if dErr != nil && !errors.Is(dErr, gorm.ErrRecordNotFound) {
		return dErr
	}

	// Upsert 订阅行 plan-state（状态推导复用 deriveVerifiedStatus——revoked 绝不复活）。
	sub.UserID = userID
	sub.Provider = provider
	sub.ProviderSubscriptionID = f.SubscriptionID
	sub.ProductID = plan.StripePriceID
	sub.ProviderCustomerID = f.CustomerID
	sub.ProviderLatestRef = f.InvoiceID
	if f.PeriodEnd > sub.CurrentPeriodEnd {
		sub.CurrentPeriodEnd = f.PeriodEnd
	}
	sub.AutoRenew = true
	if f.Livemode {
		sub.Environment = "production"
	} else {
		sub.Environment = "sandbox"
	}
	sub.Status = deriveVerifiedStatus(sub.CurrentPeriodEnd, sub.Status, now)
	if err := tx.Save(&sub).Error; err != nil {
		return err
	}
	if alreadyCredited {
		return nil // idempotent: plan-state 已刷新，不重复入账
	}

	// 叠加入账（INV3）。
	var creditSeconds int64
	var kind string
	if isFirst {
		creditSeconds = f.PeriodEnd - f.PeriodStart
		if creditSeconds < 0 {
			creditSeconds = 0
		}
		newExpiry := applyGiftCredit(user.ExpiredAt, creditSeconds, now)
		creditSeconds = newExpiry - max(user.ExpiredAt, now) // audited net add
		user.ExpiredAt = newExpiry
		kind = "purchase"
	} else {
		newExpiry := applyRenewalCredit(user.ExpiredAt, priorPeriodEnd, f.PeriodEnd)
		creditSeconds = newExpiry - user.ExpiredAt
		user.ExpiredAt = newExpiry
		kind = "renewal"
	}

	if user.IsActivated == nil || !*user.IsActivated {
		user.IsActivated = BoolPtr(true)
		user.ActivatedAt = now
	}
	if user.IsFirstOrderDone == nil || !*user.IsFirstOrderDone {
		if plan.Tier != "" {
			user.Tier = plan.Tier
		}
		user.IsFirstOrderDone = BoolPtr(true)
	}
	if err := tx.Save(&user).Error; err != nil {
		return fmt.Errorf("save user %d: %w", userID, err)
	}

	creditRow := &SubscriptionCredit{
		UserID:                userID,
		Provider:              provider,
		TransactionID:         f.InvoiceID,
		OriginalTransactionID: f.SubscriptionID,
		CreditedSeconds:       creditSeconds,
		Kind:                  kind,
	}
	if err := tx.Create(creditRow).Error; err != nil {
		return err
	}
	// Human audit（INV8）。Reason 用英文——overleap 用户会在 /api/user/pro-histories 看到它。
	if err := tx.Create(&UserProHistory{
		UserID:      userID,
		Type:        VipStripeSub,
		ReferenceID: creditRow.ID,
		Days:        int(creditSeconds / 86400),
		Reason:      fmt.Sprintf("stripe subscription credit (%s) - %s", kind, f.InvoiceID),
	}).Error; err != nil {
		return err
	}
	log.Infof(ctx, "[creditStripeInvoice] user %d credited +%dd (%s) invoice=%s expiry→%s",
		userID, int(creditSeconds/86400), kind, f.InvoiceID,
		time.Unix(user.ExpiredAt, 0).Format("2006-01-02"))
	return nil
}

// applyStripeSubscriptionUpdate 落地 customer.subscription.updated：同步 auto_renew
//（=!cancel_at_period_end）与 status。绝不 re-grant、绝不改用户权益到期（取消后用户
// 仍享有到周期结束——对标 applyRenewalInfo 的铁律）；revoked terminal 绝不复活。
// 未知订阅（绑定发生在 invoice.paid）→ 跳过。
func applyStripeSubscriptionUpdate(ctx context.Context, s *stripe.Subscription) error {
	var sub Subscription
	if err := getDB().Where(&Subscription{Provider: "stripe", ProviderSubscriptionID: s.ID}).First(&sub).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Infof(ctx, "[StripeWebhook] sub %s not yet bound (bind happens on invoice.paid), skipping update", s.ID)
			return nil
		}
		return err
	}
	if sub.Status == "revoked" {
		return nil // terminal：绝不复活
	}
	updates := map[string]any{"auto_renew": !s.CancelAtPeriodEnd}
	if st := stripeSubStatus(s.Status); st != "" {
		updates["status"] = st
	}
	if err := getDB().Model(&Subscription{}).Where("id = ?", sub.ID).Updates(updates).Error; err != nil {
		return err
	}
	log.Infof(ctx, "[StripeWebhook] sub %s autoRenew=%v status=%v", s.ID, updates["auto_renew"], updates["status"])
	return nil
}

// markStripeSubscriptionDeleted 落地 customer.subscription.deleted：标记 expired + 关
// auto_renew。权益不回收——expired_at 已等于最后周期末，自然过期（同 Apple EXPIRED 语义）。
func markStripeSubscriptionDeleted(ctx context.Context, s *stripe.Subscription) error {
	var sub Subscription
	if err := getDB().Where(&Subscription{Provider: "stripe", ProviderSubscriptionID: s.ID}).First(&sub).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Infof(ctx, "[StripeWebhook] deleted sub %s unknown, skipping", s.ID)
			return nil
		}
		return err
	}
	if sub.Status == "revoked" {
		return nil
	}
	return getDB().Model(&Subscription{}).Where("id = ?", sub.ID).
		Updates(map[string]any{"status": "expired", "auto_renew": false}).Error
}

// recordStripeRefundAlert 被动记账退款：Slack 告警 + 尽力归属本地用户。
// 不自动 clawback、不自动置 revoked——主动退款/权益回收走 admin 后续迭代
//（仓库既定「支付网关不退款」原则在 Stripe 侧的过渡形态）。
func recordStripeRefundAlert(ctx context.Context, raw []byte) error {
	var ch stripe.Charge
	if err := json.Unmarshal(raw, &ch); err != nil {
		return fmt.Errorf("parse charge: %w", err)
	}
	attribution := ""
	if ch.Customer != nil && ch.Customer.ID != "" {
		var sub Subscription
		if err := getDB().Where(&Subscription{Provider: "stripe", ProviderCustomerID: ch.Customer.ID}).
			Order("id DESC").First(&sub).Error; err == nil {
			attribution = fmt.Sprintf(" user_id=%d sub=%s", sub.UserID, sub.ProviderSubscriptionID)
		}
	}
	msg := fmt.Sprintf("[STRIPE-REFUND] charge=%s refunded=%d/%d %s customer=%s%s — passive record only, manual follow-up in Stripe Dashboard",
		ch.ID, ch.AmountRefunded, ch.Amount, string(ch.Currency), stripeCustomerID(ch.Customer), attribution)
	log.Errorf(ctx, "%s", msg)
	if err := slack.Send("alert", msg); err != nil {
		log.Errorf(ctx, "failed to send stripe refund alert: %v", err)
	}
	return nil
}

// recordStripeDisputeAlert 被动记账争议（chargeback）：Slack 告警。争议在 Stripe
// Dashboard 应诉（devices 使用日志可作"已交付服务"抗辩素材）。
func recordStripeDisputeAlert(ctx context.Context, raw []byte) error {
	var d stripe.Dispute
	if err := json.Unmarshal(raw, &d); err != nil {
		return fmt.Errorf("parse dispute: %w", err)
	}
	chargeID := ""
	if d.Charge != nil {
		chargeID = d.Charge.ID
	}
	msg := fmt.Sprintf("[STRIPE-DISPUTE] dispute=%s charge=%s amount=%d %s reason=%s — respond in Stripe Dashboard",
		d.ID, chargeID, d.Amount, string(d.Currency), string(d.Reason))
	log.Errorf(ctx, "%s", msg)
	if err := slack.Send("alert", msg); err != nil {
		log.Errorf(ctx, "failed to send stripe dispute alert: %v", err)
	}
	return nil
}

func stripeCustomerID(c *stripe.Customer) string {
	if c == nil {
		return ""
	}
	return c.ID
}
