package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
