package center

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/invoice"
	"github.com/stripe/stripe-go/v82/paymentintent"
	"github.com/stripe/stripe-go/v82/refund"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// 14 天撤回执行器（spec 2026-10-07 A 期 §3）。A 期只由后台 / MCP 触发；B 期用户按钮复用。
//
// 一次请求 = 一个计划：primary（NoticeAt 之前最近一笔付款，按天折算）+ post_notice（通知后
// 才扣的付款，全额）。每条一行 StatutoryRefund，共享 RequestID。顺序：退款 → 收回 → 取消 →
// 完成。任何一步失败都可重提同一请求续跑：续跑锚点是 pending 行（不重新判定资格），退款先
// 按 metadata 查自家退款再新建（Stripe 幂等键只留 24 小时，查重才是主防线）。

const (
	withdrawModeWithdrawal  = "withdrawal"
	withdrawModeTermination = "termination"

	withdrawKindPrimary    = "primary"
	withdrawKindPostNotice = "post_notice"

	withdrawStatusPending   = "pending"
	withdrawStatusDone      = "done"
	withdrawStatusAbandoned = "abandoned"

	withdrawWindowDays    = 14
	terminationNoticeSlop = 48 * 3600
	annualPeriodMinDays   = 360
	withdrawalLockSec     = 600
)

// withdrawInvoice 一张已付 invoice 中撤回需要的事实（从 Stripe 读，适配点在 parseWithdrawInvoice）。
type withdrawInvoice struct {
	InvoiceID       string
	PaymentIntentID string
	Currency        string
	PaidAmount      int64 // status=paid 那条 invoice payment 的 amount_paid（不是 invoice.amount_paid）
	PaidAt          int64
	PeriodStart     int64
	PeriodEnd       int64
	BillingReason   string // subscription_create = 新订阅首付；subscription_cycle = 续费
}

// withdrawQuote primary 条目的资格与金额（纯函数结果）。
type withdrawQuote struct {
	Eligible     bool   `json:"eligible"`
	Reason       string `json:"reason,omitempty"`
	UsedDays     int    `json:"usedDays"`
	TotalDays    int    `json:"totalDays"`
	RefundAmount int64  `json:"refundAmount"`
	FullRefund   bool   `json:"fullRefund"`
	WindowEndsAt int64  `json:"windowEndsAt"`
}

// stripeChargeState PI 最新一笔 charge 的退款 / 拒付状态。
type stripeChargeState struct {
	Refunded       bool
	Disputed       bool
	AmountRefunded int64 // 这笔 charge 上已有的退款（不含我们按 metadata 找到的自家撤回退款——那种情况在查重时已返回）
	Remaining      int64 // amount - amount_refunded
}

// SDK 测试替换点。
var (
	stripePaidInvoicesForSub = func(key, subID string) ([]withdrawInvoice, error) {
		params := &stripe.InvoiceListParams{Subscription: stripe.String(subID), Status: stripe.String("paid")}
		params.AddExpand("data.payments")
		it := invoice.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}.List(params)
		var out []withdrawInvoice
		for it.Next() {
			if wi, ok := parseWithdrawInvoice(it.Invoice()); ok {
				out = append(out, wi)
			}
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
		return out, nil
	}

	stripeFindWithdrawalRefund = func(key, pi, invoiceID string) (id string, amount int64, found bool, err error) {
		it := refund.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}.List(&stripe.RefundListParams{PaymentIntent: stripe.String(pi)})
		for it.Next() {
			r := it.Refund()
			if r.Metadata["center_withdrawal"] == invoiceID && r.Status != stripe.RefundStatusFailed && r.Status != stripe.RefundStatusCanceled {
				return r.ID, r.Amount, true, nil
			}
		}
		// 出错必须中止：当成"没找到"会在幂等键过期后建出第二笔退款。
		if err := it.Err(); err != nil {
			return "", 0, false, err
		}
		return "", 0, false, nil
	}

	stripeChargeStateForPI = func(key, pi string) (stripeChargeState, error) {
		params := &stripe.PaymentIntentParams{}
		params.AddExpand("latest_charge")
		p, err := paymentintent.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}.Get(pi, params)
		if err != nil {
			return stripeChargeState{}, err
		}
		if p.LatestCharge == nil {
			return stripeChargeState{}, fmt.Errorf("payment intent %s has no charge", pi)
		}
		ch := p.LatestCharge
		return stripeChargeState{Refunded: ch.Refunded, Disputed: ch.Disputed, AmountRefunded: ch.AmountRefunded,
			Remaining: ch.Amount - ch.AmountRefunded}, nil
	}

	stripeCreateWithdrawalRefund = func(key, pi string, amount int64, invoiceID, requestID string) (string, int64, error) {
		params := &stripe.RefundParams{
			PaymentIntent: stripe.String(pi),
			Amount:        stripe.Int64(amount),
			Metadata:      map[string]string{"center_withdrawal": invoiceID, "center_request": requestID},
		}
		// 金额进幂等键：作废后按新金额重新发起时，不会撞上 24 小时内同键不同参数的报错。
		params.SetIdempotencyKey(fmt.Sprintf("withdraw-%s-%d", invoiceID, amount))
		r, err := refund.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}.New(params)
		if err != nil {
			return "", 0, err
		}
		// 幂等窗口内可能拿回一笔之前失败 / 取消的退款：不能当成功记账。
		if r.Status == stripe.RefundStatusFailed || r.Status == stripe.RefundStatusCanceled {
			return "", 0, fmt.Errorf("refund %s status %s", r.ID, r.Status)
		}
		return r.ID, r.Amount, nil
	}
)

// parseWithdrawInvoice 从 basil 形态的 invoice 取撤回事实。没有已付 payment → ok=false。
func parseWithdrawInvoice(inv *stripe.Invoice) (withdrawInvoice, bool) {
	if inv == nil {
		return withdrawInvoice{}, false
	}
	wi := withdrawInvoice{InvoiceID: inv.ID, Currency: string(inv.Currency), BillingReason: string(inv.BillingReason)}
	if inv.StatusTransitions != nil {
		wi.PaidAt = inv.StatusTransitions.PaidAt
	}
	if inv.Payments != nil {
		for _, p := range inv.Payments.Data {
			if p != nil && p.Status == "paid" && p.Payment != nil && p.Payment.PaymentIntent != nil {
				wi.PaymentIntentID = p.Payment.PaymentIntent.ID
				wi.PaidAmount = p.AmountPaid
				break
			}
		}
	}
	if inv.Lines != nil {
		for _, line := range inv.Lines.Data {
			if line != nil && line.Period != nil && line.Period.End > wi.PeriodEnd {
				wi.PeriodEnd, wi.PeriodStart = line.Period.End, line.Period.Start
			}
		}
	}
	if wi.PaymentIntentID == "" || wi.PaidAt == 0 {
		return withdrawInvoice{}, false
	}
	return wi, true
}

// utcDayStart 返回 ts 所在 UTC 日的 00:00。
func utcDayStart(ts int64) int64 {
	t := time.Unix(ts, 0).UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Unix()
}

// quoteStripeWithdrawal primary 条目的资格与金额（纯函数）。
//   - 合格（仅 withdrawal）：新订阅首付，或周期 ≥360 天的续费（年付续费）；月付续费不合格。
//     窗口：付款日 D（UTC）起，noticeAt < D+16 日 00:00（第 14 天结束再留 1 天余量）。
//   - termination（条款 8.3）：跳过合格性，照常折算。
//   - 已用天数：起点 u = max(周期起点, 付款时刻)；含付款当天，按 24 小时计。
//   - 无同意记录：仅 withdrawal 全额退（没有折算依据）。
func quoteStripeWithdrawal(inv withdrawInvoice, hasConsent bool, noticeAt int64, mode string) withdrawQuote {
	q := withdrawQuote{WindowEndsAt: utcDayStart(inv.PaidAt) + (withdrawWindowDays+2)*86400}
	if mode == withdrawModeWithdrawal {
		periodDays := (inv.PeriodEnd - inv.PeriodStart) / 86400
		switch {
		case inv.BillingReason == string(stripe.InvoiceBillingReasonSubscriptionCreate):
		case inv.BillingReason == string(stripe.InvoiceBillingReasonSubscriptionCycle) && periodDays >= annualPeriodMinDays:
		default:
			q.Reason = "payment is a monthly renewal (or not a first/annual payment): not covered"
			return q
		}
		if noticeAt >= q.WindowEndsAt {
			q.Reason = "notice is outside the 14-day window"
			return q
		}
		if inv.PaidAmount <= 0 {
			q.Reason = "nothing was paid for this period"
			return q
		}
	}

	u := max(inv.PeriodStart, inv.PaidAt)
	q.TotalDays = max(1, int(math.Round(float64(inv.PeriodEnd-u)/86400)))
	q.UsedDays = min(max(int((noticeAt-u)/86400)+1, 1), q.TotalDays)
	if noticeAt >= inv.PeriodEnd {
		q.UsedDays = q.TotalDays
	}
	q.Eligible = true
	if mode == withdrawModeWithdrawal && !hasConsent {
		q.FullRefund = true
		q.RefundAmount = inv.PaidAmount
		return q
	}
	q.RefundAmount = inv.PaidAmount * int64(q.TotalDays-q.UsedDays) / int64(q.TotalDays)
	return q
}

// withdrawalRequest 一次撤回请求（后台 / MCP；B 期用户按钮）。
type withdrawalRequest struct {
	UserID         uint64 `json:"userId"`
	SubscriptionID string `json:"subscriptionId,omitempty"` // 缺省：该用户最新一条非 revoked 的 Stripe 订阅
	NoticeAt       int64  `json:"noticeAt"`
	Mode           string `json:"mode"`
	RequestID      string `json:"requestId,omitempty"` // 显式续跑
	OperatorID     uint64 `json:"operatorId"`
	Source         string `json:"source"`
	Reason         string `json:"reason,omitempty"`
}

// withdrawalPlanItem 计划条目（预览与执行共用）。
type withdrawalPlanItem struct {
	Kind            string `json:"kind"`
	InvoiceID       string `json:"invoiceId"`
	PaymentIntentID string `json:"paymentIntentId"`
	Currency        string `json:"currency"`
	PaidAmount      int64  `json:"paidAmount"`
	PaidAt          int64  `json:"paidAt"`
	Amount          int64  `json:"amount"`
	UsedDays        int    `json:"usedDays"`
	TotalDays       int    `json:"totalDays"`
	FullRefund      bool   `json:"fullRefund"`
}

// withdrawalPlan 预览结果。
type withdrawalPlan struct {
	SubscriptionID string               `json:"subscriptionId"`
	NoticeAt       int64                `json:"noticeAt"`
	Mode           string               `json:"mode"`
	HasConsent     bool                 `json:"hasConsent"`
	Quote          withdrawQuote        `json:"quote"`
	Items          []withdrawalPlanItem `json:"items"`
}

func validateWithdrawalRequest(req *withdrawalRequest, now int64) error {
	switch req.Mode {
	case withdrawModeWithdrawal, withdrawModeTermination:
	default:
		return fmt.Errorf("mode must be withdrawal or termination")
	}
	if req.NoticeAt <= 0 {
		return fmt.Errorf("notice_at is required (the time the user told us)")
	}
	if req.NoticeAt > now {
		return fmt.Errorf("notice_at is in the future")
	}
	if req.Mode == withdrawModeTermination && req.NoticeAt < now-terminationNoticeSlop {
		return fmt.Errorf("termination notice_at must be within 48h before now — run the executor when you terminate")
	}
	return nil
}

// resolveWithdrawalSubscription 确定请求针对的 Stripe 订阅。
func resolveWithdrawalSubscription(userID uint64, subID string) (string, error) {
	if subID != "" {
		var sub Subscription
		if err := getDB().Where(&Subscription{UserID: userID, Provider: SubscriptionProviderStripe, ProviderSubscriptionID: subID}).
			First(&sub).Error; err != nil {
			return "", fmt.Errorf("subscription %s not found for this user: %w", subID, err)
		}
		return subID, nil
	}
	var sub Subscription
	if err := getDB().Where("user_id = ? AND provider = ? AND status <> ?", userID, SubscriptionProviderStripe, "revoked").
		Order("id DESC").First(&sub).Error; err != nil {
		return "", fmt.Errorf("no active Stripe subscription for this user: %w", err)
	}
	return sub.ProviderSubscriptionID, nil
}

// buildWithdrawalPlan 生成计划（不写库）。primary 不合格 → 返回带原因的错误（withdrawal 模式）。
func buildWithdrawalPlan(ctx context.Context, req *withdrawalRequest) (*withdrawalPlan, error) {
	subID, err := resolveWithdrawalSubscription(req.UserID, req.SubscriptionID)
	if err != nil {
		return nil, err
	}
	invoices, err := stripePaidInvoicesForSub(stripeSecretKey(), subID)
	if err != nil {
		return nil, fmt.Errorf("list paid invoices for %s: %w", subID, err)
	}
	var primary *withdrawInvoice
	for i := range invoices {
		inv := &invoices[i]
		if inv.PaidAt <= req.NoticeAt && (primary == nil || inv.PaidAt > primary.PaidAt) {
			primary = inv
		}
	}
	if primary == nil {
		return nil, fmt.Errorf("no payment on %s before notice_at — nothing to withdraw from", subID)
	}
	var consents int64
	if err := getDB().Model(&SubscriptionConsent{}).Where("provider_subscription_id = ?", subID).Count(&consents).Error; err != nil {
		return nil, err
	}
	plan := &withdrawalPlan{SubscriptionID: subID, NoticeAt: req.NoticeAt, Mode: req.Mode, HasConsent: consents > 0}
	plan.Quote = quoteStripeWithdrawal(*primary, plan.HasConsent, req.NoticeAt, req.Mode)
	if !plan.Quote.Eligible {
		return plan, fmt.Errorf("not eligible: %s", plan.Quote.Reason)
	}
	plan.Items = append(plan.Items, withdrawalPlanItem{
		Kind: withdrawKindPrimary, InvoiceID: primary.InvoiceID, PaymentIntentID: primary.PaymentIntentID,
		Currency: primary.Currency, PaidAmount: primary.PaidAmount, PaidAt: primary.PaidAt,
		Amount: plan.Quote.RefundAmount, UsedDays: plan.Quote.UsedDays, TotalDays: plan.Quote.TotalDays, FullRefund: plan.Quote.FullRefund,
	})
	for _, inv := range invoices {
		if inv.PaidAt > req.NoticeAt {
			plan.Items = append(plan.Items, postNoticeItem(inv))
		}
	}
	return plan, nil
}

func postNoticeItem(inv withdrawInvoice) withdrawalPlanItem {
	return withdrawalPlanItem{
		Kind: withdrawKindPostNotice, InvoiceID: inv.InvoiceID, PaymentIntentID: inv.PaymentIntentID,
		Currency: inv.Currency, PaidAmount: inv.PaidAmount, PaidAt: inv.PaidAt, Amount: inv.PaidAmount, FullRefund: true,
	}
}

func newStatutoryRefundRow(req *withdrawalRequest, requestID, subID string, it withdrawalPlanItem) StatutoryRefund {
	active := it.InvoiceID
	return StatutoryRefund{
		RequestID: requestID, UserID: req.UserID, ProviderSubscriptionID: subID,
		InvoiceID: it.InvoiceID, ActiveInvoiceID: &active, PaymentIntentID: it.PaymentIntentID, Kind: it.Kind,
		Amount: it.Amount, Currency: it.Currency, UsedDays: it.UsedDays, TotalDays: it.TotalDays, FullRefund: it.FullRefund,
		NoticeAt: req.NoticeAt, Mode: req.Mode, Status: withdrawStatusPending,
		OperatorID: req.OperatorID, Source: req.Source, Reason: req.Reason,
	}
}

// appendPostNoticeRows 从 Stripe 重新枚举通知后的已付 invoice，缺的追加到同一请求。
func appendPostNoticeRows(ctx context.Context, rows []StatutoryRefund) ([]StatutoryRefund, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	head := rows[0]
	invoices, err := stripePaidInvoicesForSub(stripeSecretKey(), head.ProviderSubscriptionID)
	if err != nil {
		return rows, fmt.Errorf("re-enumerate paid invoices: %w", err)
	}
	have := map[string]bool{}
	for _, r := range rows {
		have[r.InvoiceID] = true
	}
	req := &withdrawalRequest{UserID: head.UserID, NoticeAt: head.NoticeAt, Mode: head.Mode,
		OperatorID: head.OperatorID, Source: head.Source, Reason: head.Reason}
	for _, inv := range invoices {
		if inv.PaidAt <= head.NoticeAt || have[inv.InvoiceID] {
			continue
		}
		row := newStatutoryRefundRow(req, head.RequestID, head.ProviderSubscriptionID, postNoticeItem(inv))
		if err := getDB().Create(&row).Error; err != nil {
			return rows, fmt.Errorf("append post-notice row for %s: %w", inv.InvoiceID, err)
		}
		log.Infof(ctx, "[Withdrawal] request %s: appended post-notice invoice %s (%d %s)", head.RequestID, inv.InvoiceID, inv.PaidAmount, inv.Currency)
		rows = append(rows, row)
	}
	return rows, nil
}

func loadWithdrawalRows(requestID string) ([]StatutoryRefund, error) {
	var rows []StatutoryRefund
	err := getDB().Where("request_id = ?", requestID).Order("id ASC").Find(&rows).Error
	return rows, err
}

// withdrawalResult 执行结果（返回给后台 / MCP）。
type withdrawalResult struct {
	RequestID string            `json:"requestId"`
	Rows      []StatutoryRefund `json:"rows"`
}

// executeStripeWithdrawal 执行（或续跑）一次撤回请求。
func executeStripeWithdrawal(ctx context.Context, req *withdrawalRequest) (*withdrawalResult, error) {
	rows, err := openOrCreateWithdrawal(ctx, req)
	if err != nil {
		return nil, err
	}
	requestID := rows[0].RequestID
	subID := rows[0].ProviderSubscriptionID
	key := stripeSecretKey()
	release, err := lockWithdrawalRequest(requestID)
	if err != nil {
		return nil, err
	}
	defer release()

	if rows, err = appendPostNoticeRows(ctx, rows); err != nil {
		return nil, err
	}
	for pass := 0; ; pass++ {
		for i := range rows {
			if err := refundWithdrawalRow(ctx, key, &rows[i]); err != nil {
				return nil, fmt.Errorf("request %s invoice %s: %w", requestID, rows[i].InvoiceID, err)
			}
		}
		if pass == 0 {
			reason := withdrawalRevokeReason(requestID)
			if _, err := revokeStripeSubscription(ctx, subID, reason); err != nil {
				return nil, fmt.Errorf("request %s revoke: %w", requestID, err)
			}
			if err := cancelStripeSubscriptionIfLive(ctx, subID, reason); err != nil {
				return nil, fmt.Errorf("request %s cancel: %w", requestID, err)
			}
		}
		// 取消之后再枚举一次：计划建好后、取消前又扣的款也要全额退。
		before := len(rows)
		if rows, err = appendPostNoticeRows(ctx, rows); err != nil {
			return nil, err
		}
		if len(rows) == before || pass >= 2 {
			break
		}
	}

	if err := getDB().Model(&StatutoryRefund{}).Where("request_id = ? AND status = ?", requestID, withdrawStatusPending).
		Update("status", withdrawStatusDone).Error; err != nil {
		return nil, err
	}
	final, _ := loadWithdrawalRows(requestID)
	var total int64
	for _, r := range final {
		total += r.RefundedAmount
	}
	alertStripeRevoke(ctx, "[WITHDRAWAL]", "request %s done: user %d sub %s mode %s refunded %d %s across %d payment(s)",
		requestID, req.UserID, subID, rows[0].Mode, total, rows[0].Currency, len(final))
	return &withdrawalResult{RequestID: requestID, Rows: final}, nil
}

// openOrCreateWithdrawal 续跑锚点：显式 request_id → 续跑；否则该用户的 pending 请求与本次
// 一致 → 续跑，不一致 → 报错；没有 → 按计划新建 pending 行。
func openOrCreateWithdrawal(ctx context.Context, req *withdrawalRequest) ([]StatutoryRefund, error) {
	if req.RequestID != "" {
		rows, err := loadWithdrawalRows(req.RequestID)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 || rows[0].UserID != req.UserID {
			return nil, fmt.Errorf("request %s not found for this user", req.RequestID)
		}
		if !hasPending(rows) {
			return nil, fmt.Errorf("request %s has no pending rows (already %s)", req.RequestID, rows[0].Status)
		}
		return rows, nil
	}

	var open StatutoryRefund
	err := getDB().Where("user_id = ? AND status = ?", req.UserID, withdrawStatusPending).Order("id ASC").First(&open).Error
	if err == nil {
		rows, lerr := loadWithdrawalRows(open.RequestID)
		if lerr != nil {
			return nil, lerr
		}
		sameSub := req.SubscriptionID == "" || req.SubscriptionID == open.ProviderSubscriptionID
		if sameSub && req.NoticeAt == open.NoticeAt && req.Mode == open.Mode {
			return rows, nil
		}
		return nil, fmt.Errorf("user has an unfinished withdrawal request %s (invoice %s, notice_at %d, mode %s): resume it with request_id or abandon it first",
			open.RequestID, open.InvoiceID, open.NoticeAt, open.Mode)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if err := validateWithdrawalRequest(req, time.Now().Unix()); err != nil {
		return nil, err
	}
	plan, err := buildWithdrawalPlan(ctx, req)
	if err != nil {
		return nil, err
	}
	requestID := generateId("wdr")
	rows := make([]StatutoryRefund, 0, len(plan.Items))
	err = getDB().Transaction(func(tx *gorm.DB) error {
		for _, it := range plan.Items {
			row := newStatutoryRefundRow(req, requestID, plan.SubscriptionID, it)
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create withdrawal rows (a payment may already be under another request): %w", err)
	}
	if !plan.HasConsent && req.Mode == withdrawModeWithdrawal {
		alertStripeRevoke(ctx, "[WITHDRAWAL]", "request %s: no checkout consent record for sub %s — refunding in full", requestID, plan.SubscriptionID)
	}
	return rows, nil
}

// lockWithdrawalRequest 抢占请求的占位锁（primary 行的 LockedUntil，条件 UPDATE）。
// 已被占用 → 报错，让操作员稍后重试。release 清锁；进程崩溃时锁在 withdrawalLockSec 后自然过期。
func lockWithdrawalRequest(requestID string) (release func(), err error) {
	now := time.Now().Unix()
	res := getDB().Model(&StatutoryRefund{}).
		Where("request_id = ? AND kind = ? AND locked_until < ?", requestID, withdrawKindPrimary, now).
		Update("locked_until", now+withdrawalLockSec)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected != 1 {
		return nil, fmt.Errorf("withdrawal request %s is being processed by another operator — retry in a few minutes", requestID)
	}
	return func() {
		getDB().Model(&StatutoryRefund{}).Where("request_id = ? AND kind = ?", requestID, withdrawKindPrimary).Update("locked_until", 0)
	}, nil
}

func hasPending(rows []StatutoryRefund) bool {
	for _, r := range rows {
		if r.Status == withdrawStatusPending {
			return true
		}
	}
	return false
}

// refundWithdrawalRow 单条退款：先查自家退款 → 再查 charge 状态并封顶 → 新建 → 立即落库。
func refundWithdrawalRow(ctx context.Context, key string, row *StatutoryRefund) error {
	if row.Status != withdrawStatusPending || row.Amount <= 0 || row.StripeRefundID != "" || row.RefundNote != "" {
		return nil
	}
	save := func(updates map[string]any) error {
		return getDB().Model(&StatutoryRefund{}).Where("id = ?", row.ID).Updates(updates).Error
	}

	id, amount, found, err := stripeFindWithdrawalRefund(key, row.PaymentIntentID, row.InvoiceID)
	if err != nil {
		return fmt.Errorf("look up existing refund: %w", err)
	}
	if found {
		row.StripeRefundID, row.RefundedAmount = id, amount
		return save(map[string]any{"stripe_refund_id": id, "refunded_amount": amount})
	}

	st, err := stripeChargeStateForPI(key, row.PaymentIntentID)
	if err != nil {
		return fmt.Errorf("charge state: %w", err)
	}
	if st.Refunded || st.Disputed {
		row.RefundNote = fmt.Sprintf("skipped: charge refunded=%v disputed=%v", st.Refunded, st.Disputed)
		return save(map[string]any{"refund_note": row.RefundNote})
	}
	// 这笔 charge 上已有别的退款（如运营手工补偿）：应退总额不超过计划额，先扣掉已退的；
	// 再封顶到 charge 剩余可退额。
	amount = min(row.Amount-st.AmountRefunded, st.Remaining)
	note := ""
	if amount < row.Amount {
		note = fmt.Sprintf("reduced from %d to %d (charge already refunded %d)", row.Amount, max(amount, 0), st.AmountRefunded)
	}
	if amount <= 0 {
		row.RefundNote = "skipped: " + note
		return save(map[string]any{"refund_note": row.RefundNote})
	}

	id, amount, err = stripeCreateWithdrawalRefund(key, row.PaymentIntentID, amount, row.InvoiceID, row.RequestID)
	if err != nil {
		return fmt.Errorf("create refund: %w", err)
	}
	row.StripeRefundID, row.RefundedAmount = id, amount
	updates := map[string]any{"stripe_refund_id": id, "refunded_amount": amount}
	if note != "" {
		updates["refund_note"] = note
	}
	return save(updates)
}

// withdrawalAbandonReport 作废报告。
type withdrawalAbandonReport struct {
	RequestID string            `json:"requestId"`
	Revoked   bool              `json:"revoked"`
	Canceled  bool              `json:"canceled"`
	Rows      []StatutoryRefund `json:"rows"`
}

// abandonStripeWithdrawal 作废卡死的请求：先按 metadata 找回未落库的退款；有任何退款 → 补做
// 收回与取消（避免钱退了会员还在）；然后 pending 行置 abandoned 并释放 ActiveInvoiceID。
func abandonStripeWithdrawal(ctx context.Context, requestID string, operatorID uint64, reason string) (*withdrawalAbandonReport, error) {
	rows, err := loadWithdrawalRows(requestID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || !hasPending(rows) {
		return nil, fmt.Errorf("request %s has no pending rows", requestID)
	}
	release, err := lockWithdrawalRequest(requestID)
	if err != nil {
		return nil, err
	}
	defer release()
	key := stripeSecretKey()
	refunded := false
	for i := range rows {
		r := &rows[i]
		if r.Status == withdrawStatusPending && r.StripeRefundID == "" && r.PaymentIntentID != "" {
			id, amount, found, err := stripeFindWithdrawalRefund(key, r.PaymentIntentID, r.InvoiceID)
			if err != nil {
				return nil, fmt.Errorf("look up refund for %s: %w", r.InvoiceID, err)
			}
			if found {
				r.StripeRefundID, r.RefundedAmount = id, amount
				if err := getDB().Model(&StatutoryRefund{}).Where("id = ?", r.ID).
					Updates(map[string]any{"stripe_refund_id": id, "refunded_amount": amount}).Error; err != nil {
					return nil, err
				}
			}
		}
		if r.StripeRefundID != "" {
			refunded = true
		}
	}
	rep := &withdrawalAbandonReport{RequestID: requestID}
	if refunded {
		subID := rows[0].ProviderSubscriptionID
		reasonText := withdrawalRevokeReason(requestID)
		if _, err := revokeStripeSubscription(ctx, subID, reasonText); err != nil {
			return nil, fmt.Errorf("revoke before abandon: %w", err)
		}
		rep.Revoked = true
		if err := cancelStripeSubscriptionIfLive(ctx, subID, reasonText); err != nil {
			return nil, fmt.Errorf("cancel before abandon: %w", err)
		}
		rep.Canceled = true
	}
	if err := getDB().Model(&StatutoryRefund{}).Where("request_id = ? AND status = ?", requestID, withdrawStatusPending).
		Updates(map[string]any{"status": withdrawStatusAbandoned, "active_invoice_id": nil}).Error; err != nil {
		return nil, err
	}
	rep.Rows, _ = loadWithdrawalRows(requestID)
	alertStripeRevoke(ctx, "[WITHDRAWAL]", "request %s ABANDONED by operator %d (%s): refunded=%v revoked=%v canceled=%v",
		requestID, operatorID, reason, refunded, rep.Revoked, rep.Canceled)
	return rep, nil
}
