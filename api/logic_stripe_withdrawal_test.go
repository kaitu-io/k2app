package center

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v82"
	db "github.com/wordgate/qtoolkit/db"
)

const tDay = int64(86400)

// ---------------- 纯函数 ----------------

// #16 quote 表驱动
func TestQuoteStripeWithdrawal(t *testing.T) {
	// 付款 2026-01-10 08:00 UTC；年付周期 365 天
	paid := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC).Unix()
	annual := withdrawInvoice{InvoiceID: "in_a", PaidAmount: 36500, PaidAt: paid, PeriodStart: paid, PeriodEnd: paid + 365*tDay,
		BillingReason: string(stripe.InvoiceBillingReasonSubscriptionCreate)}
	renewalAnnual := annual
	renewalAnnual.BillingReason = string(stripe.InvoiceBillingReasonSubscriptionCycle)
	monthlyFirst := withdrawInvoice{InvoiceID: "in_m", PaidAmount: 3000, PaidAt: paid, PeriodStart: paid, PeriodEnd: paid + 30*tDay,
		BillingReason: string(stripe.InvoiceBillingReasonSubscriptionCreate)}
	monthlyRenewal := monthlyFirst
	monthlyRenewal.BillingReason = string(stripe.InvoiceBillingReasonSubscriptionCycle)
	windowEnd := time.Date(2026, 1, 26, 0, 0, 0, 0, time.UTC).Unix() // D+16 00:00

	cases := []struct {
		name       string
		inv        withdrawInvoice
		consent    bool
		notice     int64
		mode       string
		eligible   bool
		used, tot  int
		amount     int64
		fullRefund bool
	}{
		{"annual first, payment day", annual, true, paid + 60, withdrawModeWithdrawal, true, 1, 365, 36400, false},
		{"annual first, day 13 notice", annual, true, paid + 12*tDay + 3600, withdrawModeWithdrawal, true, 13, 365, 36500 * 352 / 365, false},
		{"annual renewal eligible", renewalAnnual, true, paid + 5*tDay, withdrawModeWithdrawal, true, 6, 365, 36500 * 359 / 365, false},
		{"monthly first eligible", monthlyFirst, true, paid + 2*tDay, withdrawModeWithdrawal, true, 3, 30, 2700, false},
		{"monthly renewal not covered", monthlyRenewal, true, paid + tDay, withdrawModeWithdrawal, false, 0, 0, 0, false},
		{"last second of window", annual, true, windowEnd - 1, withdrawModeWithdrawal, true, 16, 365, 36500 * 349 / 365, false},
		{"window closed", annual, true, windowEnd, withdrawModeWithdrawal, false, 0, 0, 0, false},
		{"nothing paid", withdrawInvoice{PaidAt: paid, PeriodStart: paid, PeriodEnd: paid + 30*tDay, BillingReason: "subscription_create"}, true, paid + tDay, withdrawModeWithdrawal, false, 0, 0, 0, false},
		{"no consent → full refund", annual, false, paid + 5*tDay, withdrawModeWithdrawal, true, 6, 365, 36500, true},
		{"termination ignores consent and eligibility", monthlyRenewal, false, paid + 10*tDay, withdrawModeTermination, true, 11, 30, 3000 * 19 / 30, false},
		{"termination after period end → 0", monthlyRenewal, true, paid + 40*tDay, withdrawModeTermination, true, 30, 30, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := quoteStripeWithdrawal(c.inv, c.consent, c.notice, c.mode)
			require.Equal(t, c.eligible, q.Eligible, q.Reason)
			if !c.eligible {
				return
			}
			assert.Equal(t, c.used, q.UsedDays)
			assert.Equal(t, c.tot, q.TotalDays)
			assert.Equal(t, c.amount, q.RefundAmount)
			assert.Equal(t, c.fullRefund, q.FullRefund)
		})
	}

	t.Run("late payment counts from paid_at", func(t *testing.T) {
		// 周期 1 月 1 日开始，扣款重试到 1 月 6 日才成功：前 5 天不算用户用掉的
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
		inv := withdrawInvoice{PaidAmount: 36000, PaidAt: start + 5*tDay, PeriodStart: start, PeriodEnd: start + 365*tDay,
			BillingReason: string(stripe.InvoiceBillingReasonSubscriptionCycle)}
		q := quoteStripeWithdrawal(inv, true, start+5*tDay+60, withdrawModeWithdrawal)
		require.True(t, q.Eligible)
		assert.Equal(t, 360, q.TotalDays)
		assert.Equal(t, 1, q.UsedDays)
		assert.Equal(t, int64(36000*359/360), q.RefundAmount)
	})
}

func TestValidateWithdrawalRequest(t *testing.T) {
	now := time.Now().Unix()
	assert.Error(t, validateWithdrawalRequest(&withdrawalRequest{Mode: withdrawModeWithdrawal}, now), "notice_at required")
	assert.Error(t, validateWithdrawalRequest(&withdrawalRequest{Mode: withdrawModeWithdrawal, NoticeAt: now + 60}, now), "future")
	assert.Error(t, validateWithdrawalRequest(&withdrawalRequest{Mode: "x", NoticeAt: now}, now), "mode")
	assert.Error(t, validateWithdrawalRequest(&withdrawalRequest{Mode: withdrawModeTermination, NoticeAt: now - 3*tDay}, now), "termination backdated")
	assert.NoError(t, validateWithdrawalRequest(&withdrawalRequest{Mode: withdrawModeTermination, NoticeAt: now - 3600}, now))
	assert.NoError(t, validateWithdrawalRequest(&withdrawalRequest{Mode: withdrawModeWithdrawal, NoticeAt: now - 10*tDay}, now))
}

// #17 invoice 解析：先失败后成功的两条 payment，取 paid 那条
func TestParseWithdrawInvoice(t *testing.T) {
	inv := &stripe.Invoice{
		ID: "in_1", Currency: "gbp", BillingReason: stripe.InvoiceBillingReasonSubscriptionCreate,
		StatusTransitions: &stripe.InvoiceStatusTransitions{PaidAt: 1000},
		Payments: &stripe.InvoicePaymentList{Data: []*stripe.InvoicePayment{
			{Status: "canceled", AmountPaid: 0, Payment: &stripe.InvoicePaymentPayment{PaymentIntent: &stripe.PaymentIntent{ID: "pi_failed"}}},
			{Status: "paid", AmountPaid: 7900, Payment: &stripe.InvoicePaymentPayment{PaymentIntent: &stripe.PaymentIntent{ID: "pi_ok"}}},
		}},
		Lines: &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{
			{Period: &stripe.Period{Start: 1000, End: 2000}},
		}},
	}
	wi, ok := parseWithdrawInvoice(inv)
	require.True(t, ok)
	assert.Equal(t, "pi_ok", wi.PaymentIntentID)
	assert.Equal(t, int64(7900), wi.PaidAmount)
	assert.Equal(t, int64(1000), wi.PaidAt)
	assert.Equal(t, int64(2000), wi.PeriodEnd)

	inv.Payments.Data = inv.Payments.Data[:1]
	_, ok = parseWithdrawInvoice(inv)
	assert.False(t, ok, "no paid payment → not usable")
}

// ---------------- 执行器（DB） ----------------

type fakeRefund struct {
	id, invoiceID string
	amount        int64
}

type withdrawFakes struct {
	mu          sync.Mutex
	invoices    map[string][]withdrawInvoice // subID → paid invoices
	refunds     map[string][]fakeRefund      // pi → refunds
	charge      map[string]stripeChargeState // pi → state（缺省：未退、未拒付、剩余足够）
	findErr     error
	createErr   error // 一次性
	createSaved bool  // 与 createErr 同用：退款在 Stripe 已成功，但调用方拿到错误（id 未落库）
	creates     []fakeRefund
	onCreate    func(pi string)
}

func installWithdrawFakes(t *testing.T) *withdrawFakes {
	t.Helper()
	w := &withdrawFakes{invoices: map[string][]withdrawInvoice{}, refunds: map[string][]fakeRefund{}, charge: map[string]stripeChargeState{}}
	o1, o2, o3, o4 := stripePaidInvoicesForSub, stripeFindWithdrawalRefund, stripeChargeStateForPI, stripeCreateWithdrawalRefund
	t.Cleanup(func() {
		stripePaidInvoicesForSub, stripeFindWithdrawalRefund, stripeChargeStateForPI, stripeCreateWithdrawalRefund = o1, o2, o3, o4
	})
	stripePaidInvoicesForSub = func(key, subID string) ([]withdrawInvoice, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		return append([]withdrawInvoice(nil), w.invoices[subID]...), nil
	}
	stripeFindWithdrawalRefund = func(key, pi, invoiceID string) (string, int64, bool, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.findErr != nil {
			return "", 0, false, w.findErr
		}
		for _, r := range w.refunds[pi] {
			if r.invoiceID == invoiceID {
				return r.id, r.amount, true, nil
			}
		}
		return "", 0, false, nil
	}
	stripeChargeStateForPI = func(key, pi string) (stripeChargeState, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		if st, ok := w.charge[pi]; ok {
			return st, nil
		}
		return stripeChargeState{Remaining: 1 << 40}, nil
	}
	stripeCreateWithdrawalRefund = func(key, pi string, amount int64, invoiceID, requestID string) (string, int64, error) {
		w.mu.Lock()
		r := fakeRefund{id: "re_" + stripeUniq(), invoiceID: invoiceID, amount: amount}
		err := w.createErr
		w.createErr = nil
		if err == nil || w.createSaved {
			w.refunds[pi] = append(w.refunds[pi], r)
			w.creates = append(w.creates, r)
		}
		hook := w.onCreate
		w.mu.Unlock()
		if hook != nil {
			hook(pi)
		}
		if err != nil {
			return "", 0, err
		}
		return r.id, r.amount, nil
	}
	return w
}

func (w *withdrawFakes) createCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.creates)
}

// withdrawFixture 一个 overleap 用户 + 本地 Stripe 订阅 + Stripe 侧已付 invoice。
type withdrawFixture struct {
	u     *User
	subID string
	inv   withdrawInvoice
}

func seedWithdrawFixture(t *testing.T, f *stripeFakes, w *withdrawFakes, periodStart, periodDays int64, paid int64, reason stripe.InvoiceBillingReason, consent bool) *withdrawFixture {
	t.Helper()
	u := createStripeTestUser(t, BrandOverleap)
	subID := "sub_" + stripeUniq()
	periodEnd := periodStart + periodDays*tDay
	seedStripeSub(t, u, subID, periodEnd, periodEnd)
	f.remote[subID] = liveRemoteSub(subID, u.UUID, "price_x", periodEnd)
	inv := withdrawInvoice{InvoiceID: "in_" + stripeUniq(), PaymentIntentID: "pi_" + stripeUniq(), Currency: "gbp",
		PaidAmount: paid, PaidAt: periodStart, PeriodStart: periodStart, PeriodEnd: periodEnd, BillingReason: string(reason)}
	w.invoices[subID] = []withdrawInvoice{inv}
	t.Cleanup(func() {
		db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&StatutoryRefund{})
		db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&SubscriptionConsent{})
	})
	if consent {
		require.NoError(t, db.Get().Create(&SubscriptionConsent{UserID: u.ID, CheckoutSessionID: "cs_" + stripeUniq(),
			ProviderSubscriptionID: subID, TextVersion: consentTextVersion, AcceptedAt: periodStart}).Error)
	}
	return &withdrawFixture{u: u, subID: subID, inv: inv}
}

func withdrawRows(t *testing.T, userID uint64) []StatutoryRefund {
	t.Helper()
	var rows []StatutoryRefund
	require.NoError(t, db.Get().Where("user_id = ?", userID).Order("id ASC").Find(&rows).Error)
	return rows
}

func TestStripeWithdrawalExecutor(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	setStripeTestConfig(t, "sk_test_x", stripeTestWebhookSecret)
	ctx := context.Background()

	// #18 正常路径：年付首付第 3 天撤回 → 折算退款、收回、取消、done
	t.Run("HappyPath", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay-3600, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)

		res, err := executeStripeWithdrawal(ctx, &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 60, Mode: withdrawModeWithdrawal, OperatorID: 1, Source: "admin"})
		require.NoError(t, err)

		require.Len(t, res.Rows, 1)
		row := res.Rows[0]
		assert.Equal(t, withdrawStatusDone, row.Status)
		assert.Equal(t, 3, row.UsedDays)
		assert.Equal(t, int64(7900*362/365), row.Amount)
		assert.Equal(t, row.Amount, row.RefundedAmount)
		require.Len(t, w.creates, 1)
		assert.Equal(t, fx.inv.InvoiceID, w.creates[0].invoiceID)
		assert.InDelta(t, time.Now().Unix(), reloadUser(t, fx.u.ID).ExpiredAt, 5)
		assert.Equal(t, "revoked", reloadStripeSub(t, fx.subID).Status)
		hs := refundHistories(t, fx.u.ID)
		require.Len(t, hs, 1)
		assert.Equal(t, "Withdrawal within 14 days - "+fx.inv.InvoiceID, hs[0].Reason)
		assert.Equal(t, []string{fx.subID}, f.cancels())
	})

	// #19 续跑：取消失败 → 报错；资格已变（续跑不重判）→ 重提完成，不重复退款
	t.Run("ResumeAfterCancelFailure_NoRequote", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)
		f.cancelErr = errors.New("stripe down")
		f.fetchErr = nil
		req := &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 60, Mode: withdrawModeWithdrawal, OperatorID: 1, Source: "admin"}
		_, err := executeStripeWithdrawal(ctx, req)
		// cancel 失败后会再 Get 一次；远端仍 active → 报错
		require.Error(t, err)
		assert.Equal(t, withdrawStatusPending, withdrawRows(t, fx.u.ID)[0].Status)

		// 让资格判定变成"不合格"：续跑不能重判
		w.invoices[fx.subID][0].BillingReason = string(stripe.InvoiceBillingReasonSubscriptionCycle)
		res, err := executeStripeWithdrawal(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, withdrawStatusDone, res.Rows[0].Status)
		assert.Equal(t, 1, w.createCount())
		assert.Len(t, refundHistories(t, fx.u.ID), 1)
	})

	// #20 退款已在 Stripe、id 未落库（崩溃 / 超 24 小时）→ 续跑按 metadata 找回，不新建
	t.Run("ResumeFindsUnsavedRefund", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, false) // 无同意 → 全额
		w.createErr, w.createSaved = errors.New("connection reset"), true
		req := &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 60, Mode: withdrawModeWithdrawal, OperatorID: 1, Source: "admin"}
		_, err := executeStripeWithdrawal(ctx, req)
		require.Error(t, err)
		// 全额退款已发生：charge 显示已全额退款——查重必须先于 charge 检查，否则记成"跳过"
		w.charge[fx.inv.PaymentIntentID] = stripeChargeState{Refunded: true}

		res, err := executeStripeWithdrawal(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, 1, w.createCount())
		assert.NotEmpty(t, res.Rows[0].StripeRefundID)
		assert.Empty(t, res.Rows[0].RefundNote)
		assert.Equal(t, int64(7900), res.Rows[0].RefundedAmount)
	})

	t.Run("FindErrorAborts_NoCreate", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)
		w.findErr = errors.New("list refunds failed")
		_, err := executeStripeWithdrawal(ctx, &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 60, Mode: withdrawModeWithdrawal})
		require.Error(t, err)
		assert.Equal(t, 0, w.createCount())
	})

	// #21 取消触发的 subscription.deleted 在执行中途到达 → revoked 短路，历史只有撤回原因
	t.Run("DeletedEventMidExecution", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)
		inner := stripeCancelSubscription
		stripeCancelSubscription = func(subID, comment string) error {
			err := inner(subID, comment)
			ended := time.Now().Unix()
			require.NoError(t, markStripeSubscriptionDeleted(ctx, &stripe.Subscription{ID: subID, EndedAt: ended,
				Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{{CurrentPeriodEnd: fx.inv.PeriodEnd}}}}))
			return err
		}
		_, err := executeStripeWithdrawal(ctx, &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 60, Mode: withdrawModeWithdrawal})
		require.NoError(t, err)
		hs := refundHistories(t, fx.u.ID)
		require.Len(t, hs, 1)
		assert.True(t, strings.HasPrefix(hs[0].Reason, "Withdrawal within 14 days"), hs[0].Reason)
	})

	// #22 未完成请求与新请求不一致 → 报错，不续跑旧的
	t.Run("MismatchedRequestRejected", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)
		f.cancelErr = errors.New("stripe down")
		_, err := executeStripeWithdrawal(ctx, &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 60, Mode: withdrawModeWithdrawal})
		require.Error(t, err)
		before := w.createCount()

		_, err = executeStripeWithdrawal(ctx, &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 120, Mode: withdrawModeWithdrawal})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unfinished withdrawal request")
		assert.Equal(t, before, w.createCount())
		assert.Equal(t, withdrawStatusPending, withdrawRows(t, fx.u.ID)[0].Status)
	})

	// #23 作废：退款已发但 id 未落库 → 作废仍找回并收回、取消；之后同一 invoice 可重新发起且不重复退
	t.Run("AbandonThenRedo", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)
		w.createErr, w.createSaved = errors.New("timeout"), true
		_, err := executeStripeWithdrawal(ctx, &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 60, Mode: withdrawModeWithdrawal})
		require.Error(t, err)
		reqID := withdrawRows(t, fx.u.ID)[0].RequestID

		rep, err := abandonStripeWithdrawal(ctx, reqID, 1, "stuck")
		require.NoError(t, err)
		assert.True(t, rep.Revoked)
		assert.True(t, rep.Canceled)
		assert.Equal(t, "revoked", reloadStripeSub(t, fx.subID).Status)
		rows := withdrawRows(t, fx.u.ID)
		require.Len(t, rows, 1)
		assert.Equal(t, withdrawStatusAbandoned, rows[0].Status)
		assert.Nil(t, rows[0].ActiveInvoiceID)
		assert.NotEmpty(t, rows[0].StripeRefundID)

		// 重新发起：订阅已 revoked，需显式 subscription_id
		res, err := executeStripeWithdrawal(ctx, &withdrawalRequest{UserID: fx.u.ID, SubscriptionID: fx.subID, NoticeAt: now - 60, Mode: withdrawModeWithdrawal})
		require.NoError(t, err)
		assert.Equal(t, 1, w.createCount(), "redo reuses the refund found by metadata")
		assert.Equal(t, rows[0].StripeRefundID, res.Rows[0].StripeRefundID)
	})

	// #24 通知后被扣的续费全额退；计划建好后、取消前又扣一笔 → 追加并全额退
	t.Run("PostNoticeChargesRefundedInFull", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		start := now - 31*tDay
		fx := seedWithdrawFixture(t, f, w, start, 30, 999, stripe.InvoiceBillingReasonSubscriptionCreate, true)
		renewal := withdrawInvoice{InvoiceID: "in_" + stripeUniq(), PaymentIntentID: "pi_" + stripeUniq(), Currency: "gbp",
			PaidAmount: 999, PaidAt: start + 30*tDay, PeriodStart: start + 30*tDay, PeriodEnd: start + 60*tDay,
			BillingReason: string(stripe.InvoiceBillingReasonSubscriptionCycle)}
		w.invoices[fx.subID] = append(w.invoices[fx.subID], renewal) // 该续费本地未入账也一样被枚举到
		late := withdrawInvoice{InvoiceID: "in_" + stripeUniq(), PaymentIntentID: "pi_" + stripeUniq(), Currency: "gbp",
			PaidAmount: 999, PaidAt: now - 10, PeriodStart: now, PeriodEnd: now + 30*tDay,
			BillingReason: string(stripe.InvoiceBillingReasonSubscriptionCycle)}
		inner := stripeCancelSubscription
		stripeCancelSubscription = func(subID, comment string) error {
			w.mu.Lock()
			w.invoices[subID] = append(w.invoices[subID], late)
			w.mu.Unlock()
			return inner(subID, comment)
		}

		notice := start + 12*tDay + 3600 // 第 13 天
		res, err := executeStripeWithdrawal(ctx, &withdrawalRequest{UserID: fx.u.ID, NoticeAt: notice, Mode: withdrawModeWithdrawal})
		require.NoError(t, err)
		require.Len(t, res.Rows, 3)
		byInv := map[string]StatutoryRefund{}
		for _, r := range res.Rows {
			byInv[r.InvoiceID] = r
			assert.Equal(t, withdrawStatusDone, r.Status)
		}
		assert.Equal(t, withdrawKindPrimary, byInv[fx.inv.InvoiceID].Kind)
		assert.Equal(t, 13, byInv[fx.inv.InvoiceID].UsedDays)
		assert.Equal(t, int64(999*17/30), byInv[fx.inv.InvoiceID].RefundedAmount)
		assert.Equal(t, int64(999), byInv[renewal.InvoiceID].RefundedAmount)
		assert.Equal(t, int64(999), byInv[late.InvoiceID].RefundedAmount)
	})

	// #25 charge 已退 / 拒付中 → 不退款但收回、取消；部分已退 → 封顶
	t.Run("ChargeStateChecks", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			st    stripeChargeState
			want  int64
			note  string
			calls int
		}{
			{"disputed", stripeChargeState{Disputed: true, Remaining: 7900}, 0, "disputed=true", 0},
			{"refunded", stripeChargeState{Refunded: true}, 0, "refunded=true", 0},
			// 运营此前已手工退 7400 → 只再退 计划额 − 7400（总额不超过计划额），want 在下面按计划额算
			{"partly refunded → reduced", stripeChargeState{AmountRefunded: 7400, Remaining: 500}, -7400, "reduced", 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f, w := installStripeFakes(t), installWithdrawFakes(t)
				now := time.Now().Unix()
				fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)
				w.charge[fx.inv.PaymentIntentID] = tc.st
				res, err := executeStripeWithdrawal(ctx, &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 60, Mode: withdrawModeWithdrawal})
				require.NoError(t, err)
				assert.Equal(t, tc.calls, w.createCount())
				want := tc.want
				if want < 0 {
					want += res.Rows[0].Amount
				}
				assert.Equal(t, want, res.Rows[0].RefundedAmount)
				assert.Contains(t, res.Rows[0].RefundNote, tc.note)
				assert.Equal(t, "revoked", reloadStripeSub(t, fx.subID).Status)
				assert.Equal(t, []string{fx.subID}, f.cancels())
			})
		}
	})

	// #26 全额撤回时 charge.refunded webhook 先于执行器的收回到达 → 历史是撤回原因、告警标 [WITHDRAWAL]
	t.Run("FullRefundWebhookFirst", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, false)
		w.onCreate = func(pi string) {
			raw := []byte(fmt.Sprintf(`{"id":"ch_w","object":"charge","amount":7900,"amount_refunded":7900,"refunded":true,"currency":"gbp","payment_intent":%q}`, pi))
			require.NoError(t, handleStripeChargeRefunded(ctx, raw))
		}
		_, err := executeStripeWithdrawal(ctx, &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 60, Mode: withdrawModeWithdrawal})
		require.NoError(t, err)
		hs := refundHistories(t, fx.u.ID)
		require.Len(t, hs, 1)
		assert.Equal(t, "Withdrawal within 14 days - "+fx.inv.InvoiceID, hs[0].Reason)
		assert.Contains(t, f.alertText(), "[WITHDRAWAL]")
		assert.NotContains(t, f.alertText(), "[STRIPE-REFUND]")
	})

	// 同一请求被并发续跑：占位锁被占 → 第二路报错，不碰 Stripe
	t.Run("ConcurrentResumeLocked", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)
		f.cancelErr = errors.New("stripe down")
		req := &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 60, Mode: withdrawModeWithdrawal}
		_, err := executeStripeWithdrawal(ctx, req)
		require.Error(t, err)
		reqID := withdrawRows(t, fx.u.ID)[0].RequestID
		release, err := lockWithdrawalRequest(reqID) // 另一位操作员正在执行
		require.NoError(t, err)
		_, err = executeStripeWithdrawal(ctx, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "being processed")
		_, err = abandonStripeWithdrawal(ctx, reqID, 1, "x")
		require.Error(t, err)
		release()
		_, err = executeStripeWithdrawal(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, 1, w.createCount())
	})

	// #27 并发取消：Cancel 报非 404 错误，再 Get 已 canceled → 成功
	t.Run("ConcurrentCancel", func(t *testing.T) {
		f := installStripeFakes(t)
		subID := "sub_" + stripeUniq()
		f.remote[subID] = liveRemoteSub(subID, "u", "p", time.Now().Unix()+tDay)
		inner := stripeCancelSubscription
		stripeCancelSubscription = func(id, comment string) error {
			_ = inner(id, comment) // 另一方先取消成功
			return &stripe.Error{Code: "subscription_canceled", HTTPStatusCode: 400}
		}
		require.NoError(t, cancelStripeSubscriptionIfLive(ctx, subID, "x"))
	})

	// #28 termination：不看合格性与同意，照常折算
	t.Run("Termination", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-20*tDay, 30, 3000, stripe.InvoiceBillingReasonSubscriptionCycle, false)
		res, err := executeStripeWithdrawal(ctx, &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 60, Mode: withdrawModeTermination})
		require.NoError(t, err)
		assert.False(t, res.Rows[0].FullRefund)
		assert.Equal(t, int64(3000*10/30), res.Rows[0].RefundedAmount) // 周期已过 19 天多，含当天算用了 20 天
		hs := refundHistories(t, fx.u.ID)
		require.Len(t, hs, 1)
		assert.Equal(t, "Service terminated by Overleap - "+fx.subID, hs[0].Reason)
	})

	t.Run("IneligibleMonthlyRenewal_NoRows", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 30, 999, stripe.InvoiceBillingReasonSubscriptionCycle, true)
		_, err := executeStripeWithdrawal(ctx, &withdrawalRequest{UserID: fx.u.ID, NoticeAt: now - 60, Mode: withdrawModeWithdrawal})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "monthly renewal")
		assert.Empty(t, withdrawRows(t, fx.u.ID))
		assert.Equal(t, "active", reloadStripeSub(t, fx.subID).Status)
	})

	// 作废行不被 webhook 当作自家撤回
	t.Run("AbandonedRowIgnoredByWebhookLookup", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)
		active := fx.inv.InvoiceID
		require.NoError(t, db.Get().Create(&StatutoryRefund{RequestID: "wdr_x", UserID: fx.u.ID, ProviderSubscriptionID: fx.subID,
			InvoiceID: fx.inv.InvoiceID, ActiveInvoiceID: &active, PaymentIntentID: fx.inv.PaymentIntentID, Kind: withdrawKindPrimary,
			NoticeAt: now, Mode: withdrawModeWithdrawal, Status: withdrawStatusAbandoned}).Error)
		raw := []byte(fmt.Sprintf(`{"id":"ch_p","object":"charge","amount":7900,"amount_refunded":100,"refunded":false,"currency":"gbp","payment_intent":%q}`, fx.inv.PaymentIntentID))
		require.NoError(t, handleStripeChargeRefunded(ctx, raw))
		assert.Contains(t, f.alertText(), "partial refund, membership kept")
	})
}
