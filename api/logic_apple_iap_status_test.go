package center

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// TestIsSubscriptionLive pins the read-model predicate: a sub counts as "live"
// (occupies the user → show manage / don't double-sell) only when it genuinely
// covers the user right now. The production bug was an `active` row whose
// current_period_end was already in the past being reported as live.
func TestIsSubscriptionLive(t *testing.T) {
	const now int64 = 1_000_000

	cases := []struct {
		name       string
		status     string
		periodEnd  int64
		wantLive   bool
	}{
		{"active future period → live", "active", now + 86400, true},
		{"active past period → NOT live (the prod bug)", "active", now - 86400, false},
		{"active period exactly now → NOT live", "active", now, false},
		{"grace always live (apple still granting)", "grace", now - 86400, true},
		{"billing_retry always live (apple retrying)", "billing_retry", now - 999999, true},
		{"expired → never live", "expired", now + 86400, false},
		{"revoked → never live", "revoked", now + 86400, false},
		{"unknown status → never live", "weird", now + 86400, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Subscription{Status: c.status, CurrentPeriodEnd: c.periodEnd}
			assert.Equal(t, c.wantLive, isSubscriptionLive(s, now))
		})
	}
}

// TestDeriveVerifiedStatus pins the write-side status derivation after a
// successful Apple verify: status follows the (post-merge, max) period end —
// never born-stale "active" with a past period. A refunded sub is never revived.
func TestDeriveVerifiedStatus(t *testing.T) {
	const now int64 = 1_000_000

	cases := []struct {
		name              string
		effectivePeriod   int64
		existingStatus    string
		want              string
	}{
		{"new row, future period → active", now + 86400, "", "active"},
		{"new row, past period → expired (not born-stale)", now - 86400, "", "expired"},
		{"period exactly now → expired", now, "", "expired"},
		{"future period revives prior active", now + 86400, "active", "active"},
		{"future period revives prior expired (genuine resubscribe)", now + 86400, "expired", "active"},
		{"revoked stays revoked even with future period (no replay revival)", now + 86400, "revoked", "revoked"},
		{"past period on prior grace → expired", now - 1, "grace", "expired"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, deriveVerifiedStatus(c.effectivePeriod, c.existingStatus, now))
		})
	}
}

// Grace cover-through(spec 2026-08-22):进入宽限期时权益覆盖到 GracePeriodExpiresDate,
// Apple 重试扣费期间不断服务。
func TestApplyRenewalInfo_GraceCoversEntitlement(t *testing.T) {
	skipIfNoDB(t)
	user := CreateTestUser(t)
	now := time.Now().Unix()
	// 权益已过期 1 天,订阅进入宽限期(宽限到 +16 天)。
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", user.ID).
		Update("expired_at", now-86400).Error)
	sub := &Subscription{
		UserID: user.ID, Provider: "apple",
		ProviderSubscriptionID: fmt.Sprintf("OTXG-%d", time.Now().UnixNano()),
		Status:                 "active", CurrentPeriodEnd: now - 86400,
	}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&Subscription{}) })

	graceEndMs := (now + 16*86400) * 1000
	ri := &appstore.RenewalInfo{GracePeriodExpiresDate: graceEndMs, IsInBillingRetryPeriod: false}
	require.NoError(t, applyRenewalInfo(context.Background(), sub, ri, ""))

	require.NoError(t, db.Get().First(user, user.ID).Error)
	assert.GreaterOrEqual(t, user.ExpiredAt, graceEndMs/1000, "grace 期间权益必须覆盖到宽限期末")

	var got Subscription
	require.NoError(t, db.Get().First(&got, sub.ID).Error)
	assert.Equal(t, "grace", got.Status)
}

// TestApplyRenewalInfo_GraceThenRenew_NoDoubleCount is the fix-round-1 regression for
// review finding 1: grace cover-through must advance sub.CurrentPeriodEnd (not just
// user.ExpiredAt), otherwise a subsequent DID_RENEW's priorPeriodEnd is still the
// pre-grace period end and applyRenewalCredit's delta re-grants the whole grace
// window as if it were a gift, double-counting it (T+16d grace + 30d renewal would
// wrongly land on T+46d instead of T+30d).
func TestApplyRenewalInfo_GraceThenRenew_NoDoubleCount(t *testing.T) {
	skipIfNoDB(t)
	ctx := context.Background()
	day := int64(86400)
	uniq := time.Now().UnixNano()
	productID := fmt.Sprintf("io.kaitu.test.grace.%d", uniq)
	plan := &Plan{PID: fmt.Sprintf("tgr%d", uniq), Label: "X", Price: 1000, OriginPrice: 1000, Month: 1, Tier: "basic", AppleProductID: productID}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Delete(plan) })

	user := CreateTestUser(t)
	token := deriveAppleAccountToken(user.UUID)
	orig := fmt.Sprintf("OTXGR-%d", uniq)
	t0 := time.Now().Unix()
	periodT := t0 + 30*day // first period end (T)

	credit := func(txnID string, purchaseMs, expiresMs int64) error {
		return db.Get().Transaction(func(tx *gorm.DB) error {
			return creditAppleTransaction(ctx, tx, user.ID, &appstore.TransactionInfo{
				OriginalTransactionId: orig, TransactionId: txnID, ProductId: productID,
				AppAccountToken: token,
				Environment:     "Sandbox", PurchaseDate: purchaseMs, ExpiresDate: expiresMs,
			})
		})
	}
	t.Cleanup(func() {
		db.Get().Where("user_id = ?", user.ID).Delete(&SubscriptionCredit{})
		db.Get().Where("user_id = ?", user.ID).Delete(&Subscription{})
	})

	// 首购:period 到 T。
	require.NoError(t, credit("TXNG1", t0*1000, periodT*1000))

	var sub Subscription
	require.NoError(t, db.Get().Where("provider = ? AND provider_subscription_id = ?", "apple", orig).First(&sub).Error)
	require.Equal(t, periodT, sub.CurrentPeriodEnd)

	// 进入宽限期:graceEnd = T + 16d。
	graceEnd := periodT + 16*day
	ri := &appstore.RenewalInfo{GracePeriodExpiresDate: graceEnd * 1000, IsInBillingRetryPeriod: false}
	require.NoError(t, applyRenewalInfo(ctx, &sub, ri, ""))

	var uAfterGrace User
	require.NoError(t, db.Get().First(&uAfterGrace, user.ID).Error)
	assert.GreaterOrEqual(t, uAfterGrace.ExpiredAt, graceEnd, "grace 期间权益必须覆盖到宽限期末")

	var subAfterGrace Subscription
	require.NoError(t, db.Get().First(&subAfterGrace, sub.ID).Error)
	assert.GreaterOrEqual(t, subAfterGrace.CurrentPeriodEnd, graceEnd,
		"sub.current_period_end 也必须推进到宽限期末,否则后续续订会把宽限补时当礼物双算")

	// Apple 宽限期内扣费成功,DID_RENEW 落地:period T → T+30d。
	newPeriodEnd := periodT + 30*day
	require.NoError(t, credit("TXNG2", graceEnd*1000, newPeriodEnd*1000))

	var uAfterRenew User
	require.NoError(t, db.Get().First(&uAfterRenew, user.ID).Error)
	assert.InDelta(t, newPeriodEnd, uAfterRenew.ExpiredAt, 5,
		"续订入账后必须精确收敛到新周期末,绝不是 T+46d(双算宽限窗)")

	var cr SubscriptionCredit
	require.NoError(t, db.Get().Where("provider = ? AND transaction_id = ?", "apple", "TXNG2").First(&cr).Error)
	assert.InDelta(t, 14*day, cr.CreditedSeconds, 5, "delta 口径:newPeriodEnd - graceEnd ≈ 14d")
}
