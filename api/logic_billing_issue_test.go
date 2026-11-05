package center

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

func TestIsBillingIssueStatus(t *testing.T) {
	cases := map[string]bool{
		"grace": true, "billing_retry": true,
		"active": false, "expired": false, "revoked": false, "": false,
	}
	for status, want := range cases {
		assert.Equal(t, want, isBillingIssueStatus(status), "status=%q", status)
	}
}

func billingIssueSinceOf(t *testing.T, id uint64) int64 {
	t.Helper()
	var s Subscription
	require.NoError(t, db.Get().First(&s, id).Error)
	return s.BillingIssueSince
}

func TestApplyRenewalInfo_BillingIssueSince(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()

	t.Run("entering stamps now", func(t *testing.T) {
		withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true)
		cleanupPushLogs(t, u.ID)
		sub := seedAppleSub(t, u.ID, "active", false)
		before := time.Now().Unix()
		require.NoError(t, applyRenewalInfo(ctx, &sub, billingRetryInfo(time.Now().UnixMilli()), ""))
		after := time.Now().Unix()
		since := billingIssueSinceOf(t, sub.ID)
		assert.GreaterOrEqual(t, since, before)
		assert.LessOrEqual(t, since, after)
	})

	t.Run("replay keeps original", func(t *testing.T) {
		withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true)
		cleanupPushLogs(t, u.ID)
		sub := seedAppleSub(t, u.ID, "billing_retry", false)
		require.NoError(t, db.Get().Model(&Subscription{}).Where("id = ?", sub.ID).Update("billing_issue_since", 1000).Error)
		var fresh Subscription
		require.NoError(t, db.Get().First(&fresh, sub.ID).Error)
		require.NoError(t, applyRenewalInfo(ctx, &fresh, billingRetryInfo(time.Now().UnixMilli()), ""))
		assert.Equal(t, int64(1000), billingIssueSinceOf(t, sub.ID))
	})

	t.Run("re-entry after recovery restamps", func(t *testing.T) {
		withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true)
		cleanupPushLogs(t, u.ID)
		sub := seedAppleSub(t, u.ID, "active", false)
		require.NoError(t, db.Get().Model(&Subscription{}).Where("id = ?", sub.ID).Update("billing_issue_since", 1000).Error)
		var fresh Subscription
		require.NoError(t, db.Get().First(&fresh, sub.ID).Error)
		require.NoError(t, applyRenewalInfo(ctx, &fresh, billingRetryInfo(time.Now().UnixMilli()), ""))
		assert.Greater(t, billingIssueSinceOf(t, sub.ID), int64(1000))
	})

	t.Run("revoked untouched", func(t *testing.T) {
		withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true)
		cleanupPushLogs(t, u.ID)
		sub := seedAppleSub(t, u.ID, "revoked", false)
		require.NoError(t, applyRenewalInfo(ctx, &sub, billingRetryInfo(time.Now().UnixMilli()), ""))
		assert.Equal(t, int64(0), billingIssueSinceOf(t, sub.ID))
	})

	t.Run("auto-renew toggle does not stamp", func(t *testing.T) {
		withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true)
		sub := seedAppleSub(t, u.ID, "active", false)
		off := &appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_Off, SignedDate: time.Now().UnixMilli()}
		require.NoError(t, applyRenewalInfo(ctx, &sub, off, appstore.Subtype_AUTO_RENEW_DISABLED))
		assert.Equal(t, int64(0), billingIssueSinceOf(t, sub.ID))
	})
}

func TestGetActiveSubscriptions_BillingIssue(t *testing.T) {
	skipIfNoConfig(t)
	u, _, _ := seedPushUser(t, false, false)
	// DataSubscription 不带 ProviderSubscriptionID：用互异的 CurrentPeriodEnd 做对应键。
	want := map[int64]bool{}
	for i, st := range []string{"active", "grace", "billing_retry"} {
		s := seedAppleSub(t, u.ID, st, false)
		end := time.Now().Add(time.Duration(i+1) * time.Hour).Unix()
		require.NoError(t, db.Get().Model(&Subscription{}).Where("id = ?", s.ID).Update("current_period_end", end).Error)
		want[end] = st != "active"
		time.Sleep(time.Millisecond) // ProviderSubscriptionID 取 UnixNano，保证互异
	}
	got := GetActiveSubscriptions(u.ID)
	require.Len(t, got, 3)
	for _, g := range got {
		wantIssue, ok := want[g.CurrentPeriodEnd]
		require.True(t, ok, "unexpected period end %d", g.CurrentPeriodEnd)
		assert.Equal(t, wantIssue, g.BillingIssue, "periodEnd=%d", g.CurrentPeriodEnd)
	}
}

func TestBackfillBillingIssueSince(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	u, _, _ := seedPushUser(t, false, false)
	retry := seedAppleSub(t, u.ID, "billing_retry", false)
	time.Sleep(time.Millisecond)
	grace := seedAppleSub(t, u.ID, "grace", false)
	time.Sleep(time.Millisecond)
	active := seedAppleSub(t, u.ID, "active", false)
	require.NoError(t, db.Get().Model(&Subscription{}).Where("id = ?", grace.ID).Update("billing_issue_since", 500).Error)

	now := time.Now().Unix()
	n, err := backfillBillingIssueSince(ctx, db.Get(), now)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, int64(1))
	assert.Equal(t, now, billingIssueSinceOf(t, retry.ID))
	assert.Equal(t, int64(500), billingIssueSinceOf(t, grace.ID))
	assert.Equal(t, int64(0), billingIssueSinceOf(t, active.ID))

	n, err = backfillBillingIssueSince(ctx, db.Get(), now+10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "idempotent")
}
