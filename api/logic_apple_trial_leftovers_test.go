package center

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

// 试用遗留项（spec 2026-10-19 §3.3 / §3.7）：多条试用链时 in_trial 的清除条件；对账遇到等改绑的交易要告警。

// endTrialChain 把一条试用链推到"试用已结束、漏了 EXPIRED 通知"：周期末与试用末都落到过去，status 仍 active。
func endTrialChain(t *testing.T, otx string, at int64) {
	t.Helper()
	require.NoError(t, db.Get().Model(&Subscription{}).Where("provider = ? AND provider_subscription_id = ?", SubscriptionProviderApple, otx).
		Updates(map[string]any{"current_period_end": at, "trial_ends_at": at}).Error)
}

func appleSubOf(t *testing.T, otx string) Subscription {
	t.Helper()
	var s Subscription
	require.NoError(t, db.Get().Where(&Subscription{Provider: SubscriptionProviderApple, ProviderSubscriptionID: otx}).First(&s).Error)
	return s
}

// 同一用户两条 Apple 试用链（两个 Apple ID 各开一次试用）：链 1 到期不能清掉链 2 仍在用的试用标志；
// 链 2 也到期后才清。每周重扫会反复对账同一条已到期的链，重复跑也不能清。
func TestReconcile_DoesNotClearInTrialWhileOtherTrialChainLive(t *testing.T) {
	trialDB(t)
	setTestAppleBundleID(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	otx1, otx2 := f.origTxn, f.origTxn+"-2"

	trial1 := f.trialTxn("TR-ML-1", t0-7*aDay, t0+aDay)
	require.NoError(t, creditInfo(t, f.buyer.ID, trial1))
	trial2 := f.trialTxn("TR-ML-2", t0, t0+7*aDay)
	trial2.OriginalTransactionId = otx2
	require.NoError(t, creditInfo(t, f.buyer.ID, trial2))
	require.True(t, f.userNow(t, f.buyer.ID).InTrial)
	require.True(t, appleSubOf(t, otx2).IsTrial)

	ended := map[string]*appstore.TransactionInfo{}
	fakeAppleStatus(t, func(otx string) (*appleSubStatus, error) {
		x := *ended[otx]
		return &appleSubStatus{status: appstore.SubscriptionStatus_Expired, txn: &x}, nil
	})
	expire := func(otx string, txn *appstore.TransactionInfo) {
		endTrialChain(t, otx, t0-60)
		e := *txn
		e.ExpiresDate = (t0 - 60) * 1000
		ended[otx] = &e
	}

	// 链 1 到期：对账两轮（日常临期扫描 + 每周重扫），in_trial 保持。
	expire(otx1, trial1)
	for i := 0; i < 2; i++ {
		s := appleSubOf(t, otx1)
		_, err := reconcileAppleSubscription(context.Background(), &s, time.Now().Unix())
		require.NoError(t, err)
		assert.Equal(t, "expired", appleSubOf(t, otx1).Status)
		assert.True(t, f.userNow(t, f.buyer.ID).InTrial, "链 2 仍在试用中，第 %d 轮对账不清标志", i+1)
	}
	// webhook EXPIRED 也是同一个清除点。
	s1 := appleSubOf(t, otx1)
	require.NoError(t, applyAppleExpired(context.Background(), &s1))
	assert.True(t, f.userNow(t, f.buyer.ID).InTrial, "webhook EXPIRED 同样不清")

	// 链 2 也到期：没有其它活着的试用链了 → 清。
	expire(otx2, trial2)
	s2 := appleSubOf(t, otx2)
	_, err := reconcileAppleSubscription(context.Background(), &s2, time.Now().Unix())
	require.NoError(t, err)
	assert.False(t, f.userNow(t, f.buyer.ID).InTrial, "两条试用链都结束才清")
}

// 对账遇到等改绑的交易（errAppleTxnAwaitingRebind）：发 [APPLE-AWAITING-REBIND] 告警，含 otx、交易 id、
// 绑定用户 id；同一 otx 24 小时内只发一次，过了窗口再发。
func TestReconcile_AwaitingRebindAlerts(t *testing.T) {
	trialDB(t)
	setTestAppleBundleID(t)
	f := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()
	_, paid := setupRebindRace(t, f, t0, "TR-AR")
	fakeAppleStatus(t, func(otx string) (*appleSubStatus, error) {
		x := *paid
		return &appleSubStatus{status: appstore.SubscriptionStatus_Active, txn: &x}, nil
	})

	type alert struct{ tag, msg string }
	var mu sync.Mutex
	var got []alert
	old := alertPaymentAnomaly
	alertPaymentAnomaly = func(_ context.Context, tag, format string, args ...any) {
		mu.Lock()
		got = append(got, alert{tag, fmt.Sprintf(format, args...)})
		mu.Unlock()
	}
	t.Cleanup(func() { alertPaymentAnomaly = old })
	rebindAlerts := func() []alert {
		mu.Lock()
		defer mu.Unlock()
		var out []alert
		for _, a := range got {
			if a.tag == "APPLE-AWAITING-REBIND" {
				out = append(out, a)
			}
		}
		return out
	}

	run := func(now int64) {
		s := f.subNow(t)
		changed, err := reconcileAppleSubscription(context.Background(), &s, now)
		require.NoError(t, err, "仍是跳过，不是失败")
		assert.False(t, changed)
	}

	run(t0)
	require.Len(t, rebindAlerts(), 1)
	msg := rebindAlerts()[0].msg
	assert.Contains(t, msg, f.origTxn)
	assert.Contains(t, msg, paid.TransactionId)
	assert.Contains(t, msg, fmt.Sprintf("%d", f.buyer.ID))

	run(t0 + 3600)
	run(t0 + 23*3600)
	assert.Len(t, rebindAlerts(), 1, "24 小时内同一 otx 只告警一次")

	run(t0 + 25*3600)
	assert.Len(t, rebindAlerts(), 2, "过了窗口再告警")
	assert.Zero(t, creditCount(t, paid.TransactionId), "告警不改变跳过语义")
}
