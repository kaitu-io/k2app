package center

import (
	"context"
	"encoding/json"
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

// stripeFakes 替换收回路径用到的全部 Stripe seam 与告警出口。
type stripeFakes struct {
	mu          sync.Mutex
	subByPI     map[string]string
	subByPIErr  error
	remote      map[string]*stripe.Subscription
	fetchErr    error
	cancelErr   error // 一次性：用过即清空
	cancelCalls []string
	alerts      []string
}

func (f *stripeFakes) alertText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.alerts, "\n")
}

func (f *stripeFakes) cancels() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cancelCalls...)
}

func installStripeFakes(t *testing.T) *stripeFakes {
	t.Helper()
	f := &stripeFakes{subByPI: map[string]string{}, remote: map[string]*stripe.Subscription{}}
	o1, o2, o3, o4, o5 := stripeSubscriptionByPaymentIntent, stripeFetchSubscription, stripeCancelSubscription, alertStripeRevoke, alertStripeCredit
	t.Cleanup(func() {
		stripeSubscriptionByPaymentIntent, stripeFetchSubscription, stripeCancelSubscription, alertStripeRevoke, alertStripeCredit = o1, o2, o3, o4, o5
	})
	stripeSubscriptionByPaymentIntent = func(key, pi string) (string, error) {
		if f.subByPIErr != nil {
			return "", f.subByPIErr
		}
		return f.subByPI[pi], nil
	}
	stripeFetchSubscription = func(subID string) (*stripe.Subscription, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fetchErr != nil {
			return nil, f.fetchErr
		}
		if s, ok := f.remote[subID]; ok {
			cp := *s
			return &cp, nil
		}
		return nil, &stripe.Error{Code: stripe.ErrorCodeResourceMissing, HTTPStatusCode: 404}
	}
	stripeCancelSubscription = func(subID, comment string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.cancelCalls = append(f.cancelCalls, subID)
		if err := f.cancelErr; err != nil {
			f.cancelErr = nil
			return err
		}
		if s, ok := f.remote[subID]; ok {
			s.Status = stripe.SubscriptionStatusCanceled
		}
		return nil
	}
	alertStripeRevoke = func(ctx context.Context, tag, format string, args ...any) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.alerts = append(f.alerts, tag+" "+fmt.Sprintf(format, args...))
	}
	alertStripeCredit = func(ctx context.Context, format string, args ...any) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.alerts = append(f.alerts, "[STRIPE-CREDIT] "+fmt.Sprintf(format, args...))
	}
	return f
}

// liveRemoteSub 远端活跃订阅（metadata 带 user_uuid，供墓碑用）。
func liveRemoteSub(subID, userUUID, priceID string, periodEnd int64) *stripe.Subscription {
	return &stripe.Subscription{
		ID:       subID,
		Status:   stripe.SubscriptionStatusActive,
		Customer: &stripe.Customer{ID: "cus_" + subID},
		Metadata: map[string]string{"user_uuid": userUUID},
		Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{
			{CurrentPeriodEnd: periodEnd, Price: &stripe.Price{ID: priceID}},
		}},
	}
}

// seedStripeSub 建一条本地 Stripe 订阅（已付费覆盖到 periodEnd）并把用户到期设为 userExpiry。
func seedStripeSub(t *testing.T, u *User, subID string, periodEnd, userExpiry int64) *Subscription {
	t.Helper()
	return seedStripeSubPaid(t, u, subID, periodEnd, periodEnd, userExpiry)
}

// seedStripeSubPaid 同上，但已付费覆盖点 paidThrough 与本地周期末 periodEnd 可以不同
// （对账会把 periodEnd 推到未付的下一期）。
func seedStripeSubPaid(t *testing.T, u *User, subID string, periodEnd, paidThrough, userExpiry int64) *Subscription {
	t.Helper()
	sub := &Subscription{
		UserID: u.ID, Provider: SubscriptionProviderStripe, ProviderSubscriptionID: subID,
		ProductID: "price_x", CurrentPeriodEnd: periodEnd, PaidThrough: paidThrough, AutoRenew: true, Status: "active", Environment: "sandbox",
	}
	require.NoError(t, db.Get().Create(sub).Error)
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", u.ID).Update("expired_at", userExpiry).Error)
	return sub
}

func reloadUser(t *testing.T, id uint64) User {
	t.Helper()
	var u User
	require.NoError(t, db.Get().First(&u, id).Error)
	return u
}

func reloadStripeSub(t *testing.T, subID string) Subscription {
	t.Helper()
	var s Subscription
	require.NoError(t, db.Get().Where(&Subscription{Provider: SubscriptionProviderStripe, ProviderSubscriptionID: subID}).First(&s).Error)
	return s
}

func refundHistories(t *testing.T, userID uint64) []UserProHistory {
	t.Helper()
	var hs []UserProHistory
	require.NoError(t, db.Get().Where("user_id = ? AND type = ?", userID, VipRefund).Find(&hs).Error)
	return hs
}

func chargeRefundedPayload(evtID, chargeID, pi string, amount, refunded int64, full bool) []byte {
	piField := "null"
	if pi != "" {
		piField = fmt.Sprintf("%q", pi)
	}
	return []byte(fmt.Sprintf(`{
		"id": %q, "object": "event", "type": "charge.refunded", "livemode": false,
		"data": {"object": {"id": %q, "object": "charge", "amount": %d, "amount_refunded": %d,
			"refunded": %t, "currency": "gbp", "customer": "cus_x", "payment_intent": %s}}
	}`, evtID, chargeID, amount, refunded, full, piField))
}

func disputePayload(evtID, typ, disputeID, pi, status string) []byte {
	return []byte(fmt.Sprintf(`{
		"id": %q, "object": "event", "type": %q, "livemode": false,
		"data": {"object": {"id": %q, "object": "dispute", "charge": "ch_d", "payment_intent": %q,
			"amount": 7900, "currency": "gbp", "reason": "fraudulent", "status": %q}}
	}`, evtID, typ, disputeID, pi, status))
}

func subDeletedPayload(evtID, subID string, endedAt, periodEnd int64) []byte {
	return []byte(fmt.Sprintf(`{
		"id": %q, "object": "event", "type": "customer.subscription.deleted", "livemode": false,
		"data": {"object": {"id": %q, "object": "subscription", "status": "canceled",
			"cancel_at_period_end": false, "ended_at": %d,
			"items": {"object": "list", "data": [{"id": "si_x", "object": "subscription_item", "current_period_end": %d}]}}}
	}`, evtID, subID, endedAt, periodEnd))
}

func TestStripeRevoke(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	setStripeTestConfig(t, "sk_test_x", stripeTestWebhookSecret)
	r := stripeWebhookRouter()
	day := int64(86400)

	post := func(t *testing.T, payload []byte) int {
		t.Helper()
		var m struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(payload, &m)
		cleanupStripeEvents(t, m.ID)
		return postStripeWebhook(t, r, payload, stripeSigHeader(payload)).Code
	}

	// #1 全额退款 → 收回 + 取消；历史原因是英文
	t.Run("FullRefund_RevokesAndCancels", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		seedStripeSub(t, u, subID, now+300*day, now+300*day)
		f.subByPI[pi] = subID
		f.remote[subID] = liveRemoteSub(subID, u.UUID, "price_x", now+300*day)

		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_1", pi, 7900, 7900, true)))

		assert.InDelta(t, time.Now().Unix(), reloadUser(t, u.ID).ExpiredAt, 5)
		s := reloadStripeSub(t, subID)
		assert.Equal(t, "revoked", s.Status)
		assert.False(t, s.AutoRenew)
		hs := refundHistories(t, u.ID)
		require.Len(t, hs, 1)
		assert.Equal(t, "Stripe full refund - ch_1", hs[0].Reason)
		assert.Equal(t, []string{subID}, f.cancels())
		assert.Contains(t, f.alertText(), "[STRIPE-REFUND]")
	})

	// #2 部分退款 → 不动
	t.Run("PartialRefund_NoChange", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		seedStripeSub(t, u, subID, now+300*day, now+300*day)
		f.subByPI[pi] = subID

		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_2", pi, 7900, 1000, false)))

		assert.InDelta(t, now+300*day, reloadUser(t, u.ID).ExpiredAt, 1)
		assert.Equal(t, "active", reloadStripeSub(t, subID).Status)
		assert.Empty(t, f.cancels())
		assert.Contains(t, f.alertText(), "partial refund, membership kept")
	})

	// #3 赠送时长保护：先有 30 天赠送再买（叠加），到期 = 周期末 + 30 天
	// → 只扣掉付费那段，剩 30 天；订阅 revoked。（旧规则"到期超出周期末就不动"= 白拿整期）
	t.Run("GiftTimePreserved_PaidPartClawed", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		seedStripeSub(t, u, subID, now+365*day, now+395*day)
		f.subByPI[pi] = subID
		f.remote[subID] = liveRemoteSub(subID, u.UUID, "price_x", now+365*day)

		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_3", pi, 7900, 7900, true)))

		assert.InDelta(t, now+30*day, reloadUser(t, u.ID).ExpiredAt, 5)
		assert.Equal(t, "revoked", reloadStripeSub(t, subID).Status)
	})

	// #4 取消失败 → 500；重投：不重复写历史，只重试取消
	t.Run("CancelFails_RetryOnlyCancels", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		seedStripeSub(t, u, subID, now+300*day, now+300*day)
		f.subByPI[pi] = subID
		f.remote[subID] = liveRemoteSub(subID, u.UUID, "price_x", now+300*day)
		f.cancelErr = errors.New("stripe 500")

		payload := chargeRefundedPayload("evt_"+stripeUniq(), "ch_4", pi, 7900, 7900, true)
		require.Equal(t, 500, post(t, payload))
		require.Equal(t, 200, postStripeWebhook(t, r, payload, stripeSigHeader(payload)).Code)

		assert.Len(t, refundHistories(t, u.ID), 1)
		assert.Equal(t, []string{subID, subID}, f.cancels())
		assert.Contains(t, f.alertText(), "membership revoked (now=false)") // 重投时收回原语短路
		assert.Equal(t, stripe.SubscriptionStatusCanceled, f.remote[subID].Status)
	})

	// #5 远端已取消 → 不调 cancel
	t.Run("RemoteAlreadyCanceled_NoCancelCall", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		seedStripeSub(t, u, subID, now+300*day, now+300*day)
		f.subByPI[pi] = subID
		rs := liveRemoteSub(subID, u.UUID, "price_x", now+300*day)
		rs.Status = stripe.SubscriptionStatusCanceled
		f.remote[subID] = rs

		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_5", pi, 7900, 7900, true)))
		assert.Empty(t, f.cancels())
		assert.Equal(t, "revoked", reloadStripeSub(t, subID).Status)
	})

	// #6 cancel 返回 resource_missing → 视为完成
	t.Run("CancelResourceMissing_200", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		seedStripeSub(t, u, subID, now+300*day, now+300*day)
		f.subByPI[pi] = subID
		f.remote[subID] = liveRemoteSub(subID, u.UUID, "price_x", now+300*day)
		f.cancelErr = &stripe.Error{Code: stripe.ErrorCodeResourceMissing, HTTPStatusCode: 404}

		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_6", pi, 7900, 7900, true)))
	})

	// #7 PI → 订阅查询出错 → 500
	t.Run("LookupError_500", func(t *testing.T) {
		f := installStripeFakes(t)
		f.subByPIErr = errors.New("iterator failed")
		require.Equal(t, 500, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_7", "pi_"+stripeUniq(), 7900, 7900, true)))
	})

	// #8 无 PI / 非订阅扣款 → 200 无写入
	t.Run("NoPIOrNotSubscription_200", func(t *testing.T) {
		f := installStripeFakes(t)
		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_8a", "", 999, 999, true)))
		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_8b", "pi_"+stripeUniq(), 999, 999, true)))
		assert.Empty(t, f.cancels())
		assert.Contains(t, f.alertText(), "no payment_intent")
		assert.NotContains(t, f.alertText(), "not a subscription charge", "shared account: foreign one-off charges are not alerted")
	})

	// 共用 Stripe 账户：别的业务的订阅退款 / 拒付 → 不建墓碑、绝不取消
	t.Run("ForeignSubscription_NeverCanceled", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		kaituUser := createStripeTestUser(t, BrandKaitu)
		for i, uuid := range []string{"", "no-such-user-" + stripeUniq(), kaituUser.UUID} {
			subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
			f.subByPI[pi] = subID
			f.remote[subID] = liveRemoteSub(subID, uuid, "price_foreign", now+30*day)
			require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), fmt.Sprintf("ch_f%d", i), pi, 4900, 4900, true)))
			require.Equal(t, 200, post(t, disputePayload("evt_"+stripeUniq(), "charge.dispute.created", fmt.Sprintf("dp_f%d", i), pi, "needs_response")))
			assert.Equal(t, stripe.SubscriptionStatusActive, f.remote[subID].Status)
			var n int64
			getDB().Model(&Subscription{}).Where("provider_subscription_id = ?", subID).Count(&n)
			assert.Zero(t, n, "no tombstone for foreign sub")
		}
		assert.Empty(t, f.cancels())
	})

	// #9 本地无订阅行 → 墓碑行；cancel 照调
	// #11 墓碑后迟到的 invoice.paid 不加时长
	t.Run("Tombstone_ThenLateInvoiceNotCredited", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		f.subByPI[pi] = subID
		f.remote[subID] = liveRemoteSub(subID, u.UUID, p.StripePriceID, now+30*day)

		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_9", pi, 999, 999, true)))

		s := reloadStripeSub(t, subID)
		assert.Equal(t, "revoked", s.Status)
		assert.Equal(t, u.ID, s.UserID)
		assert.Equal(t, p.StripePriceID, s.ProductID)
		assert.Equal(t, now+30*day, s.CurrentPeriodEnd)
		assert.Equal(t, []string{subID}, f.cancels())

		before := reloadUser(t, u.ID).ExpiredAt
		inv := invoicePaidPayload("evt_"+stripeUniq(), "in_"+stripeUniq(), subID, u.UUID, p.PID, p.StripePriceID, now, now+30*day)
		require.Equal(t, 200, post(t, inv))
		assert.Equal(t, before, reloadUser(t, u.ID).ExpiredAt)
		assert.Equal(t, "revoked", reloadStripeSub(t, subID).Status)
		assert.Contains(t, f.alertText(), "on revoked sub")
	})

	// 对账把本地周期末推到未付的下一期（+60d），已付只到 +30d；用户 = 已付 + 20 天赠送
	// → 只扣付费的 30 天，赠送 20 天保留。
	t.Run("Revoke_InflatedPeriodEnd_GiftKept", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		seedStripeSubPaid(t, u, subID, now+60*day, now+30*day, now+50*day)
		f.subByPI[pi] = subID
		f.remote[subID] = liveRemoteSub(subID, u.UUID, "price_x", now+60*day)
		require.Equal(t, 200, post(t, disputePayload("evt_"+stripeUniq(), "charge.dispute.created", "dp_inf", pi, "needs_response")))
		assert.InDelta(t, now+20*day, reloadUser(t, u.ID).ExpiredAt, 5)
	})

	// 扣款重试失败后被取消：那一期没付过钱（已付只到 5 天前），用户还有赠送时长 → 不扣
	t.Run("Deleted_UnpaidPeriod_GiftKept", func(t *testing.T) {
		installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID := "sub_" + stripeUniq()
		seedStripeSubPaid(t, u, subID, now+25*day, now-5*day, now+40*day)
		require.Equal(t, 200, post(t, subDeletedPayload("evt_"+stripeUniq(), subID, now, now+25*day)))
		assert.Equal(t, now+40*day, reloadUser(t, u.ID).ExpiredAt)
		assert.Empty(t, refundHistories(t, u.ID))
	})

	// 迁移前的老行（PaidThrough=0）续费失败被取消：周期末可能已被对账推到未付期，
	// 不能按它截 → 只告警不截，叠加的赠送时长保留
	t.Run("Deleted_LegacyZeroPaidThrough_NotClipped", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID := "sub_" + stripeUniq()
		seedStripeSubPaid(t, u, subID, now+25*day, 0, now+40*day)
		require.Equal(t, 200, post(t, subDeletedPayload("evt_"+stripeUniq(), subID, now, now+25*day)))
		assert.Equal(t, now+40*day, reloadUser(t, u.ID).ExpiredAt)
		assert.Contains(t, f.alertText(), "no paid_through")
	})

	// 先后台立即取消（截断一次），再全额退款 → 不重复扣，赠送时长保留
	t.Run("DeletedThenFullRefund_NoDoubleCut", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		seedStripeSubPaid(t, u, subID, now+30*day, now+30*day, now+50*day)
		require.Equal(t, 200, post(t, subDeletedPayload("evt_"+stripeUniq(), subID, now, now+30*day)))
		assert.Equal(t, now+20*day, reloadUser(t, u.ID).ExpiredAt)

		f.subByPI[pi] = subID
		rs := liveRemoteSub(subID, u.UUID, "price_x", now+30*day)
		rs.Status = stripe.SubscriptionStatusCanceled
		f.remote[subID] = rs
		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_dd", pi, 999, 999, true)))
		assert.InDelta(t, now+20*day, reloadUser(t, u.ID).ExpiredAt, 2)
		assert.Equal(t, "revoked", reloadStripeSub(t, subID).Status)
		assert.Len(t, refundHistories(t, u.ID), 1)
	})

	// #10 墓碑撞键：取远端订阅的瞬间，并发的 invoice.paid 建了本地行 → 重跑收回，最终 revoked
	t.Run("TombstoneCollision_RerunsRevoke", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID := "sub_" + stripeUniq()
		f.remote[subID] = liveRemoteSub(subID, u.UUID, "price_x", now+30*day)
		inner := stripeFetchSubscription
		stripeFetchSubscription = func(id string) (*stripe.Subscription, error) {
			if _, err := reloadStripeSubMaybe(id); err != nil {
				seedStripeSub(t, u, id, now+30*day, now+30*day)
			}
			return inner(id)
		}
		_, _, err := revokeStripeSubscription(context.Background(), subID, "Stripe full refund - ch_10")
		require.NoError(t, err)
		assert.Equal(t, "revoked", reloadStripeSub(t, subID).Status)
		assert.InDelta(t, time.Now().Unix(), reloadUser(t, u.ID).ExpiredAt, 5)
	})

	// #11b 收回后迟到的续费 invoice 不加时长
	t.Run("RenewalAfterRevoke_NotCredited", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		subID := "sub_" + stripeUniq()
		require.Equal(t, 200, post(t, invoicePaidPayload("evt_"+stripeUniq(), "in_"+stripeUniq(), subID, u.UUID, p.PID, p.StripePriceID, now, now+30*day)))
		pi := "pi_" + stripeUniq()
		f.subByPI[pi] = subID
		f.remote[subID] = liveRemoteSub(subID, u.UUID, p.StripePriceID, now+30*day)
		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_11", pi, 999, 999, true)))
		cut := reloadUser(t, u.ID).ExpiredAt
		// 经真实入账流程付费的订阅，全额退款后会员确实被收回（入账必须记 PaidThrough）
		assert.InDelta(t, time.Now().Unix(), cut, 5)

		require.Equal(t, 200, post(t, invoicePaidPayload("evt_"+stripeUniq(), "in_"+stripeUniq(), subID, "", p.PID, p.StripePriceID, now+30*day, now+60*day)))
		assert.Equal(t, cut, reloadUser(t, u.ID).ExpiredAt)
		assert.Contains(t, f.alertText(), "on revoked sub")
	})

	// 老行 PaidThrough=0 → 按 CurrentPeriodEnd 扣（fail-closed）
	t.Run("LegacyZeroPaidThrough_FailClosed", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		seedStripeSubPaid(t, u, subID, now+30*day, 0, now+30*day)
		f.subByPI[pi] = subID
		f.remote[subID] = liveRemoteSub(subID, u.UUID, "price_x", now+30*day)
		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_lg", pi, 999, 999, true)))
		assert.InDelta(t, time.Now().Unix(), reloadUser(t, u.ID).ExpiredAt, 5)
	})

	// 对账补出来的会员（入账失败、Stripe 显示已付）之后全额退款 → 补出来的这段也收回
	t.Run("ReconcileCoverThroughThenRefund_Revoked", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		sub := seedStripeSubPaid(t, u, subID, now+2*day, now+2*day, now+2*day)
		f.remote[subID] = liveRemoteSub(subID, u.UUID, "price_x", now+32*day)
		_, err := reconcileStripeSubscription(context.Background(), sub, now)
		require.NoError(t, err)
		require.Equal(t, now+32*day, reloadUser(t, u.ID).ExpiredAt)

		f.subByPI[pi] = subID
		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_rc", pi, 999, 999, true)))
		assert.InDelta(t, time.Now().Unix(), reloadUser(t, u.ID).ExpiredAt, 5)
	})

	// 入账晚到：周期 10 天前就开始，invoice.paid 今天才入账 → 从今天起发了整期 30 天；
	// 此时全额退款必须扣掉这 30 天（按周期末算只扣 20 天，白拿 10 天）
	t.Run("LateCreditThenRefund_FullyRevoked", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		require.Equal(t, 200, post(t, invoicePaidPayload("evt_"+stripeUniq(), "in_"+stripeUniq(), subID, u.UUID, p.PID, p.StripePriceID, now-10*day, now+20*day)))
		require.InDelta(t, now+30*day, reloadUser(t, u.ID).ExpiredAt, 5)
		f.subByPI[pi] = subID
		f.remote[subID] = liveRemoteSub(subID, u.UUID, p.StripePriceID, now+20*day)
		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_late", pi, 999, 999, true)))
		assert.InDelta(t, time.Now().Unix(), reloadUser(t, u.ID).ExpiredAt, 5)
	})

	// subscription.updated 不能把 revoked 写回 active
	t.Run("UpdateNeverResurrectsRevoked", func(t *testing.T) {
		installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID := "sub_" + stripeUniq()
		sub := seedStripeSub(t, u, subID, now+30*day, now)
		require.NoError(t, db.Get().Model(&Subscription{}).Where("id = ?", sub.ID).
			Updates(map[string]any{"status": "revoked", "auto_renew": false}).Error)
		require.Equal(t, 200, post(t, subscriptionEventPayload("evt_"+stripeUniq(), "customer.subscription.updated", subID, false, "active")))
		s := reloadStripeSub(t, subID)
		assert.Equal(t, "revoked", s.Status)
		assert.False(t, s.AutoRenew)
	})

	// #12 对账 cover-through 遇 revoked → 到期不变
	t.Run("ReconcileSkipsRevoked", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID := "sub_" + stripeUniq()
		sub := seedStripeSub(t, u, subID, now+30*day, now)
		f.remote[subID] = liveRemoteSub(subID, u.UUID, "price_x", now+30*day)
		stale := *sub // 对账先读到 active，随后被并发收回
		require.NoError(t, db.Get().Model(&Subscription{}).Where("id = ?", sub.ID).Update("status", "revoked").Error)

		_, err := reconcileStripeSubscription(context.Background(), &stale, now)
		require.NoError(t, err)
		assert.Equal(t, now, reloadUser(t, u.ID).ExpiredAt)
	})

	// #13 先全额退款后拒付 → 第二次不改数据、不调 cancel
	t.Run("RefundThenDispute_Idempotent", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		seedStripeSub(t, u, subID, now+300*day, now+300*day)
		f.subByPI[pi] = subID
		f.remote[subID] = liveRemoteSub(subID, u.UUID, "price_x", now+300*day)

		require.Equal(t, 200, post(t, chargeRefundedPayload("evt_"+stripeUniq(), "ch_13", pi, 7900, 7900, true)))
		exp := reloadUser(t, u.ID).ExpiredAt
		require.Equal(t, 200, post(t, disputePayload("evt_"+stripeUniq(), "charge.dispute.created", "dp_13", pi, "needs_response")))

		assert.Equal(t, exp, reloadUser(t, u.ID).ExpiredAt)
		assert.Len(t, refundHistories(t, u.ID), 1)
		assert.Equal(t, []string{subID}, f.cancels())
	})

	// #14 拒付 → 收回 + 取消
	t.Run("Dispute_RevokesAndCancels", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		seedStripeSub(t, u, subID, now+300*day, now+300*day)
		f.subByPI[pi] = subID
		f.remote[subID] = liveRemoteSub(subID, u.UUID, "price_x", now+300*day)

		require.Equal(t, 200, post(t, disputePayload("evt_"+stripeUniq(), "charge.dispute.created", "dp_14", pi, "warning_needs_response")))

		assert.InDelta(t, time.Now().Unix(), reloadUser(t, u.ID).ExpiredAt, 5)
		assert.Equal(t, "revoked", reloadStripeSub(t, subID).Status)
		hs := refundHistories(t, u.ID)
		require.Len(t, hs, 1)
		assert.Equal(t, "Stripe dispute - dp_14", hs[0].Reason)
		assert.Equal(t, []string{subID}, f.cancels())
		assert.Contains(t, f.alertText(), "[STRIPE-DISPUTE]")
	})

	// #15 deleted 截断：提前结束截到 ended_at；期末自然结束不截；本地周期被推远时以事件周期为准；不延长；赠送时长不截
	t.Run("DeletedEarly_Clips", func(t *testing.T) {
		installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID := "sub_" + stripeUniq()
		// 本地周期被对账推到了未付的下一期（+60d），事件自身周期末是 +30d
		seedStripeSub(t, u, subID, now+60*day, now+30*day)
		ended := now - 10

		require.Equal(t, 200, post(t, subDeletedPayload("evt_"+stripeUniq(), subID, ended, now+30*day)))
		assert.Equal(t, ended, reloadUser(t, u.ID).ExpiredAt)
		s := reloadStripeSub(t, subID)
		assert.Equal(t, "expired", s.Status)
		hs := refundHistories(t, u.ID)
		require.Len(t, hs, 1)
		assert.Equal(t, "Stripe subscription ended early - "+subID, hs[0].Reason)
	})

	t.Run("DeletedAtPeriodEnd_NoClip", func(t *testing.T) {
		installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID := "sub_" + stripeUniq()
		seedStripeSub(t, u, subID, now, now)
		require.Equal(t, 200, post(t, subDeletedPayload("evt_"+stripeUniq(), subID, now, now)))
		assert.Equal(t, now, reloadUser(t, u.ID).ExpiredAt)
		assert.Empty(t, refundHistories(t, u.ID))
	})

	t.Run("DeletedEarly_GiftTimeKept", func(t *testing.T) {
		installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID := "sub_" + stripeUniq()
		// 本地周期被推到 +120d（未付），事件周期 +30d，用户 +90d（含 60 天赠送）：只扣没给到的 30 天
		seedStripeSub(t, u, subID, now+120*day, now+90*day)
		require.Equal(t, 200, post(t, subDeletedPayload("evt_"+stripeUniq(), subID, now, now+30*day)))
		assert.Equal(t, now+60*day, reloadUser(t, u.ID).ExpiredAt)
	})

	// #29a dispute.closed → 只告警
	t.Run("DisputeClosed_AlertOnly", func(t *testing.T) {
		f := installStripeFakes(t)
		now := time.Now().Unix()
		u := createStripeTestUser(t, BrandOverleap)
		subID, pi := "sub_"+stripeUniq(), "pi_"+stripeUniq()
		seedStripeSub(t, u, subID, now+300*day, now+300*day)
		require.Equal(t, 200, post(t, disputePayload("evt_"+stripeUniq(), "charge.dispute.closed", "dp_29", pi, "won")))
		assert.Contains(t, f.alertText(), "[STRIPE-DISPUTE-CLOSED] dispute=dp_29 status=won")
		assert.Equal(t, "active", reloadStripeSub(t, subID).Status)
		assert.InDelta(t, now+300*day, reloadUser(t, u.ID).ExpiredAt, 1)
	})
}

func reloadStripeSubMaybe(subID string) (*Subscription, error) {
	var s Subscription
	err := db.Get().Where(&Subscription{Provider: SubscriptionProviderStripe, ProviderSubscriptionID: subID}).First(&s).Error
	return &s, err
}
