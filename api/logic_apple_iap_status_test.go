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
