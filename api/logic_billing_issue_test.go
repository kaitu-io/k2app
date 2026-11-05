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
	time.Sleep(time.Millisecond)
	stripeRetry := seedAppleSub(t, u.ID, "billing_retry", false)
	require.NoError(t, db.Get().Model(&Subscription{}).Where("id = ?", stripeRetry.ID).Update("provider", "stripe").Error)
	time.Sleep(time.Millisecond)
	appleExpired := seedAppleSub(t, u.ID, "expired", false)
	require.NoError(t, db.Get().Model(&Subscription{}).Where("id = ?", grace.ID).Update("billing_issue_since", 500).Error)

	now := time.Now().Unix()
	n, err := backfillBillingIssueSince(ctx, db.Get(), now)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, int64(1))
	assert.Equal(t, now, billingIssueSinceOf(t, retry.ID))
	assert.Equal(t, int64(500), billingIssueSinceOf(t, grace.ID))
	assert.Equal(t, int64(0), billingIssueSinceOf(t, active.ID))
	assert.Equal(t, int64(0), billingIssueSinceOf(t, stripeRetry.ID), "stripe billing_retry untouched")
	assert.Equal(t, int64(0), billingIssueSinceOf(t, appleExpired.ID), "apple expired untouched")

	n, err = backfillBillingIssueSince(ctx, db.Get(), now+10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "idempotent")
}

// ---- 第 0 / 3 / 7 天提醒 ----

func setBillingIssueSince(t *testing.T, id uint64, since int64) {
	t.Helper()
	require.NoError(t, db.Get().Model(&Subscription{}).Where("id = ?", id).Update("billing_issue_since", since).Error)
}

func candidateIDs(t *testing.T, day int, now time.Time) map[uint64]bool {
	t.Helper()
	subs, err := billingIssueCandidates(day, now)
	require.NoError(t, err)
	out := map[uint64]bool{}
	for _, s := range subs {
		out[s.ID] = true
	}
	return out
}

func TestBillingIssueCandidates(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	u, _, _ := seedPushUser(t, false, false)
	mk := func(offset time.Duration) uint64 {
		s := seedAppleSub(t, u.ID, "billing_retry", false)
		setBillingIssueSince(t, s.ID, now.Add(-offset).Unix())
		time.Sleep(time.Millisecond)
		return s.ID
	}
	day3 := mk(3*24*time.Hour + time.Hour)
	day2 := mk(3*24*time.Hour - time.Hour)
	day0 := mk(time.Hour)
	day7 := mk(7*24*time.Hour + 23*time.Hour)
	old := mk(8*24*time.Hour + time.Hour)

	c0, c3, c7 := candidateIDs(t, 0, now), candidateIDs(t, 3, now), candidateIDs(t, 7, now)
	assert.True(t, c0[day0])
	assert.False(t, c0[day2])
	assert.False(t, c0[day3])
	assert.True(t, c3[day3])
	assert.False(t, c3[day2])
	assert.False(t, c3[day0])
	assert.True(t, c7[day7])
	assert.False(t, c7[old])
	assert.False(t, c7[day3])
	for _, c := range []map[uint64]bool{c0, c3, c7} {
		assert.False(t, c[old], "过了 8 天的任何一天都不命中")
	}
}

func TestBillingIssueCandidates_Filters(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	since := now.Add(-3*24*time.Hour - time.Hour).Unix()
	u, _, _ := seedPushUser(t, false, false)
	mk := func(status string, mutate map[string]any) uint64 {
		s := seedAppleSub(t, u.ID, status, false)
		upd := map[string]any{"billing_issue_since": since}
		for k, v := range mutate {
			upd[k] = v
		}
		require.NoError(t, db.Get().Model(&Subscription{}).Where("id = ?", s.ID).Updates(upd).Error)
		time.Sleep(time.Millisecond)
		return s.ID
	}
	grace := mk("grace", nil)
	retry := mk("billing_retry", nil)
	active := mk("active", nil)
	expired := mk("expired", nil)
	revoked := mk("revoked", nil)
	sandbox := mk("billing_retry", map[string]any{"environment": "Sandbox"})
	stripe := mk("billing_retry", map[string]any{"provider": "stripe"})

	got := candidateIDs(t, 3, now)
	assert.True(t, got[grace])
	assert.True(t, got[retry])
	for name, id := range map[string]uint64{"active": active, "expired": expired, "revoked": revoked, "sandbox": sandbox, "stripe": stripe} {
		assert.False(t, got[id], name)
	}
}

func seedIssueSub(t *testing.T, userID uint64, now time.Time, age time.Duration) Subscription {
	t.Helper()
	s := seedAppleSub(t, userID, "billing_retry", false)
	setBillingIssueSince(t, s.ID, now.Add(-age).Unix())
	var fresh Subscription
	require.NoError(t, db.Get().First(&fresh, s.ID).Error)
	return fresh
}

func TestBillingIssueReminders_PushOnlyDays3And7(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	now := time.Now()

	t.Run("day0 sends no push", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true)
		cleanupPushLogs(t, u.ID)
		seedIssueSub(t, u.ID, now, time.Hour)
		processBillingIssueReminders(ctx, now)
		assert.Empty(t, pushLogsOf(t, u.ID))
		assert.Empty(t, h.enqueued)
	})

	t.Run("day3 pushes once, rerun deduped", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true)
		cleanupPushLogs(t, u.ID)
		sub := seedIssueSub(t, u.ID, now, 3*24*time.Hour+time.Hour)
		processBillingIssueReminders(ctx, now)
		logs := pushLogsOf(t, u.ID)
		require.Len(t, logs, 1)
		assert.Equal(t, fmt.Sprintf("billing_issue_3:%d:%d", sub.ID, sub.BillingIssueSince), logs[0].DedupKey)
		assert.Equal(t, "/purchase", logs[0].Route)
		assert.Equal(t, string(PushCategoryTransactional), logs[0].Category)
		assert.Contains(t, h.enqueued, logs[0].ID)
		n := len(h.enqueued)
		processBillingIssueReminders(ctx, now)
		assert.Len(t, pushLogsOf(t, u.ID), 1)
		assert.Len(t, h.enqueued, n)
	})

	t.Run("two day3 subs of one user push once", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true)
		cleanupPushLogs(t, u.ID)
		first := seedIssueSub(t, u.ID, now, 3*24*time.Hour+time.Hour)
		time.Sleep(time.Millisecond)
		seedIssueSub(t, u.ID, now, 3*24*time.Hour+2*time.Hour)
		processBillingIssueReminders(ctx, now)
		logs := pushLogsOf(t, u.ID)
		require.Len(t, logs, 1)
		assert.Equal(t, fmt.Sprintf("billing_issue_3:%d:%d", first.ID, first.BillingIssueSince), logs[0].DedupKey)
		assert.Equal(t, []uint64{logs[0].ID}, h.enqueued)
	})
}

func TestBillingIssueReminders_EmailMissingTemplateDoesNotBlockPush(t *testing.T) {
	skipIfNoConfig(t)
	require.False(t, templateSlugExists(billingIssueSlug), "测试前提：库里没有 billing-issue 模板")
	h := withPushHarness(t)
	u, _, _ := seedPushUser(t, false, true)
	cleanupPushLogs(t, u.ID)
	now := time.Now()
	seedIssueSub(t, u.ID, now, 7*24*time.Hour+time.Hour)
	var failed int
	assert.NotPanics(t, func() { _, _, failed = processBillingIssueReminders(context.Background(), now) })
	assert.Len(t, pushLogsOf(t, u.ID), 1)
	assert.NotEmpty(t, h.enqueued)
	assert.Equal(t, 0, failed)
}

func TestEdmOptOutExempt_BillingIssue(t *testing.T) {
	assert.True(t, edmOptOutExemptSlugs[billingIssueSlug])
	assert.Equal(t, []int{0, 3, 7}, billingIssueReminderDays)
}

func TestBillingIssueReminders_EmailSentToOptedOutUser(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	require.False(t, templateSlugExists(billingIssueSlug), "测试前提：库里没有 billing-issue 模板")
	withPushHarness(t)

	email := fmt.Sprintf("billing-issue-%d@example.com", time.Now().UnixNano())
	u := chatUserWithEmail(t, email)
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", u.ID).Update("edm_opt_out", true).Error)
	t.Cleanup(func() { db.Get().Where("user_id = ?", u.ID).Delete(&EmailSendLog{}) })

	// 模板语言取用户偏好语言：否则 SendTemplatedEmails 会走 AI 翻译分支（测试环境无 key）。
	var fresh User
	require.NoError(t, db.Get().Preload("LoginIdentifies").First(&fresh, u.ID).Error)
	lang := fresh.GetLanguagePreference()
	active := true
	tmpl := EmailMarketingTemplate{
		Name: "billing-issue test", Slug: billingIssueSlug, Language: lang,
		Subject: "s", Content: "<p>c</p>", IsActive: &active, Brand: string(BrandKaitu),
	}
	require.NoError(t, db.Get().Create(&tmpl).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&EmailMarketingTemplate{}, tmpl.ID) })

	now := time.Now()
	seedIssueSub(t, u.ID, now, 3*24*time.Hour+time.Hour)
	sent, _, _ := processBillingIssueReminders(ctx, now)
	assert.GreaterOrEqual(t, sent, 1)

	var logs []EmailSendLog
	batch := "billing-issue:3d:" + now.UTC().Format("2006-01-02")
	require.NoError(t, db.Get().Where("batch_id = ? AND user_id = ?", batch, u.ID).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, EmailSendLogStatusSent, logs[0].Status)
}
