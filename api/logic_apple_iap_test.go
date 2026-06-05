package center

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

// TestComputeRecurringEntitlement covers the load-bearing entitlement math: Apple's
// absolute expiresDate only ever raises the user's expiry (never shortens), and
// re-delivery of the same expiry is a no-op (idempotent).
func TestComputeRecurringEntitlement(t *testing.T) {
	const day = int64(86400)
	now := int64(1_700_000_000)

	cases := []struct {
		name         string
		current      int64
		appleExpires int64
		wantExpiry   int64
		wantDays     int
		wantAdvanced bool
	}{
		{
			name:         "expired user, fresh 1y from now",
			current:      now - 10*day,
			appleExpires: now + 365*day,
			wantExpiry:   now + 365*day,
			wantDays:     365,
			wantAdvanced: true,
		},
		{
			name:         "active user extended from existing expiry",
			current:      now + 30*day,
			appleExpires: now + 395*day,
			wantExpiry:   now + 395*day,
			wantDays:     365, // delta over the existing 30d, not from now
			wantAdvanced: true,
		},
		{
			name:         "apple expiry equals current -> idempotent no-op",
			current:      now + 100*day,
			appleExpires: now + 100*day,
			wantExpiry:   now + 100*day,
			wantDays:     0,
			wantAdvanced: false,
		},
		{
			name:         "apple expiry behind current -> never shorten",
			current:      now + 200*day,
			appleExpires: now + 100*day,
			wantExpiry:   now + 200*day,
			wantDays:     0,
			wantAdvanced: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotExpiry, gotDays, gotAdvanced := computeRecurringEntitlement(tc.current, tc.appleExpires, now)
			assert.Equal(t, tc.wantExpiry, gotExpiry, "expiry")
			assert.Equal(t, tc.wantDays, gotDays, "days")
			assert.Equal(t, tc.wantAdvanced, gotAdvanced, "advanced")
		})
	}
}

// TestComputeRenewalState covers the cancellation / billing-status decision used by
// the DID_CHANGE_RENEWAL_STATUS + DID_FAIL_TO_RENEW webhook paths. The load-bearing
// invariant: a user cancelling auto-renew must flip auto_renew → false (the old code
// re-granted and hardcoded true, silently losing the cancellation), while never
// resurrecting a terminal (expired/revoked) subscription.
func TestComputeRenewalState(t *testing.T) {
	const day = int64(86400)
	now := int64(1_700_000_000)
	on := &appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_On}
	off := &appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_Off}

	bptr := func(b bool) *bool { return &b }

	cases := []struct {
		name          string
		curStatus     string
		ri            *appstore.RenewalInfo
		subtype       string
		wantAutoRenew *bool
		wantStatus    string
	}{
		{
			name: "user cancels auto-renew -> auto_renew false, still active",
			curStatus: "active", ri: off, subtype: appstore.Subtype_AUTO_RENEW_DISABLED,
			wantAutoRenew: bptr(false), wantStatus: "active",
		},
		{
			name: "user re-enables auto-renew -> auto_renew true",
			curStatus: "active", ri: on, subtype: appstore.Subtype_AUTO_RENEW_ENABLED,
			wantAutoRenew: bptr(true), wantStatus: "active",
		},
		{
			name: "billing retry period -> status billing_retry",
			curStatus: "active",
			ri:        &appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_On, IsInBillingRetryPeriod: true},
			wantAutoRenew: bptr(true), wantStatus: "billing_retry",
		},
		{
			name: "grace period -> status grace, cancelled",
			curStatus: "active",
			ri:        &appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_Off, GracePeriodExpiresDate: (now + 5*day) * 1000},
			wantAutoRenew: bptr(false), wantStatus: "grace",
		},
		{
			name: "terminal revoked -> never resurrect (status unchanged)",
			curStatus: "revoked", ri: off, subtype: appstore.Subtype_AUTO_RENEW_DISABLED,
			wantAutoRenew: bptr(false), wantStatus: "",
		},
		{
			name: "terminal expired -> never resurrect",
			curStatus: "expired", ri: on, subtype: "",
			wantAutoRenew: bptr(true), wantStatus: "",
		},
		{
			name: "nil RenewalInfo falls back to subtype disabled",
			curStatus: "active", ri: nil, subtype: appstore.Subtype_AUTO_RENEW_DISABLED,
			wantAutoRenew: bptr(false), wantStatus: "active",
		},
		{
			name: "nil RenewalInfo + no informative subtype -> no auto_renew change",
			curStatus: "active", ri: nil, subtype: appstore.Subtype_BILLING_RETRY,
			wantAutoRenew: nil, wantStatus: "active",
		},
		{
			name: "expired grace date in the past -> active (grace already over)",
			curStatus: "active",
			ri:        &appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_On, GracePeriodExpiresDate: (now - day) * 1000},
			wantAutoRenew: bptr(true), wantStatus: "active",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotAR, gotStatus := computeRenewalState(tc.curStatus, tc.ri, tc.subtype, now)
			if tc.wantAutoRenew == nil {
				assert.Nil(t, gotAR, "auto_renew should be nil (no change)")
			} else {
				require.NotNil(t, gotAR, "auto_renew should be set")
				assert.Equal(t, *tc.wantAutoRenew, *gotAR, "auto_renew")
			}
			assert.Equal(t, tc.wantStatus, gotStatus, "status")
		})
	}
}

// TestDeriveAppleAccountToken: the appAccountToken must be a deterministic,
// valid RFC-4122 UUID per user (the raw Center UUID "user-<xid>" is not a UUID),
// so StoreKit accepts it and Center can recompute it for the anti-claim check.
func TestDeriveAppleAccountToken(t *testing.T) {
	a := deriveAppleAccountToken("user-cv0abc123def456gh")
	a2 := deriveAppleAccountToken("user-cv0abc123def456gh")
	b := deriveAppleAccountToken("user-zz9zzz999zzz999zz")

	assert.Equal(t, a, a2, "must be deterministic for the same user")
	assert.NotEqual(t, a, b, "different users must derive different tokens")

	parsed, err := uuid.Parse(a)
	require.NoError(t, err, "derived token must be a valid RFC-4122 UUID")
	assert.Equal(t, a, parsed.String(), "canonical lowercase form")
	// v5 (name-based SHA-1) → version nibble is 5.
	assert.Equal(t, uuid.Version(5), parsed.Version())
}

// TestVerifyAndGrantTransaction_Integration exercises the full DB grant path
// against real MySQL with a stubbed Apple fetch: first grant, idempotent
// re-delivery, and first-write-wins user binding. Skips without a DB.
func TestVerifyAndGrantTransaction_Integration(t *testing.T) {
	skipIfNoDB(t)
	ctx := context.Background()

	viper.Set("appstore.bundleId", "io.kaitu.test")
	t.Cleanup(func() { viper.Set("appstore.bundleId", "") })

	// Seed a plan mapped to an Apple product id. PID is varchar(30) — keep it short.
	uniq := time.Now().UnixNano()
	productID := fmt.Sprintf("io.kaitu.test.family.1y.%d", uniq)
	plan := &Plan{
		PID:            fmt.Sprintf("tiap%d", uniq), // <= 30 chars
		Label:          "Test Family 1Y",
		Price:          1000,
		OriginPrice:    1000,
		Month:          12,
		Tier:           "family",
		AppleProductID: productID,
	}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Delete(plan) })

	user := CreateTestUser(t)
	require.True(t, user.IsExpired(), "fresh test user should start expired")

	otx := "OTX-" + generateId("otx")
	appleExpiresMs := (time.Now().Unix() + 365*86400) * 1000

	old := fetchAppleTransaction
	fetchAppleTransaction = func(ctx context.Context, bundleId, transactionID string) (*appstore.TransactionInfo, error) {
		return &appstore.TransactionInfo{
			BundleId:              appleBundleID(),
			ProductId:             productID,
			OriginalTransactionId: otx,
			TransactionId:         transactionID,
			ExpiresDate:           appleExpiresMs,
			InAppOwnershipType:    appstore.OwnershipType_PURCHASED,
			Environment:           "Sandbox",
		}, nil
	}
	t.Cleanup(func() { fetchAppleTransaction = old })
	t.Cleanup(func() {
		db.Get().Where("provider = ? AND provider_subscription_id = ?", "apple", otx).Delete(&Subscription{})
		db.Get().Where("user_id = ? AND type = ?", user.ID, VipAppleSub).Delete(&UserProHistory{})
	})

	// ---- first grant ----
	require.NoError(t, verifyAndGrantTransaction(ctx, user.ID, "TXN1"))

	var u1 User
	require.NoError(t, db.Get().First(&u1, user.ID).Error)
	assert.False(t, u1.IsExpired(), "user should be a member after grant")
	assert.InDelta(t, appleExpiresMs/1000, u1.ExpiredAt, 2, "expiry should equal apple expiresDate (sec)")
	assert.Equal(t, "family", u1.Tier, "tier set from plan on first order")

	var sub Subscription
	require.NoError(t, db.Get().Where("provider = ? AND provider_subscription_id = ?", "apple", otx).First(&sub).Error)
	assert.Equal(t, user.ID, sub.UserID)
	assert.Equal(t, "active", sub.Status)

	var histCount int64
	db.Get().Model(&UserProHistory{}).
		Where("user_id = ? AND type = ?", user.ID, VipAppleSub).Count(&histCount)
	assert.Equal(t, int64(1), histCount, "exactly one grant history")

	// ---- idempotent re-delivery (same txn/expiry) ----
	require.NoError(t, verifyAndGrantTransaction(ctx, user.ID, "TXN1"))
	var u2 User
	require.NoError(t, db.Get().First(&u2, user.ID).Error)
	assert.Equal(t, u1.ExpiredAt, u2.ExpiredAt, "idempotent: expiry unchanged")
	db.Get().Model(&UserProHistory{}).
		Where("user_id = ? AND type = ?", user.ID, VipAppleSub).Count(&histCount)
	assert.Equal(t, int64(1), histCount, "idempotent: no second history row")

	// ---- first-write-wins: a different user cannot rebind the same subscription ----
	other := CreateTestUser(t)
	require.NoError(t, verifyAndGrantTransaction(ctx, other.ID, "TXN1"))
	var subAfter Subscription
	require.NoError(t, db.Get().Where("provider = ? AND provider_subscription_id = ?", "apple", otx).First(&subAfter).Error)
	assert.Equal(t, user.ID, subAfter.UserID, "subscription stays bound to the first user")
	var otherReloaded User
	require.NoError(t, db.Get().First(&otherReloaded, other.ID).Error)
	assert.True(t, otherReloaded.IsExpired(), "the second user must not receive entitlement")
}

// TestApplyRenewalInfo_Integration proves the cancellation actually persists:
// auto_renew=false must hit the DB (the GORM struct-zero-value trap would silently
// drop it — applyRenewalInfo uses a map update precisely to avoid that), and a
// billing-retry RenewalInfo must flip status without touching the user's expiry.
func TestApplyRenewalInfo_Integration(t *testing.T) {
	skipIfNoDB(t)
	ctx := context.Background()
	uniq := time.Now().UnixNano()

	user := CreateTestUser(t)
	sub := &Subscription{
		UserID: user.ID, Provider: "apple",
		ProviderSubscriptionID: fmt.Sprintf("OTX-RN-%d", uniq),
		ProductID:              "io.kaitu.test.basic.1y",
		CurrentPeriodEnd:       time.Now().Unix() + 200*86400,
		AutoRenew:              true, Environment: "Sandbox", Status: "active",
	}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&Subscription{}) })

	// ---- user cancels auto-renew ----
	require.NoError(t, applyRenewalInfo(ctx, sub,
		&appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_Off},
		appstore.Subtype_AUTO_RENEW_DISABLED))

	var afterCancel Subscription
	require.NoError(t, db.Get().First(&afterCancel, sub.ID).Error)
	assert.False(t, afterCancel.AutoRenew, "cancellation must persist auto_renew=false")
	assert.Equal(t, "active", afterCancel.Status, "still active until period end")

	// entitlement untouched by cancellation
	var u User
	require.NoError(t, db.Get().First(&u, user.ID).Error)
	assert.Equal(t, sub.CurrentPeriodEnd, afterCancel.CurrentPeriodEnd, "period end unchanged")

	// ---- billing retry ----
	afterCancel.Status = "active" // re-read baseline for the helper's terminal guard
	require.NoError(t, applyRenewalInfo(ctx, &afterCancel,
		&appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_On, IsInBillingRetryPeriod: true},
		""))
	var afterRetry Subscription
	require.NoError(t, db.Get().First(&afterRetry, sub.ID).Error)
	assert.Equal(t, "billing_retry", afterRetry.Status)
	assert.True(t, afterRetry.AutoRenew)
}

// TestGetActiveSubscriptions_Integration verifies the read model returns the
// active subscription with the Apple manage surface, and excludes expired ones.
func TestGetActiveSubscriptions_Integration(t *testing.T) {
	skipIfNoDB(t)

	uniq := time.Now().UnixNano()
	productID := fmt.Sprintf("io.kaitu.test.basic.1y.%d", uniq)
	plan := &Plan{
		PID: fmt.Sprintf("tgas%d", uniq), Label: "GAS", Price: 1000, OriginPrice: 1000,
		Month: 12, Tier: "basic", AppleProductID: productID,
	}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Delete(plan) })

	user := CreateTestUser(t)
	active := &Subscription{
		UserID: user.ID, Provider: "apple",
		ProviderSubscriptionID: fmt.Sprintf("OTX-A-%d", uniq),
		ProductID:              productID,
		CurrentPeriodEnd:       time.Now().Unix() + 200*86400,
		AutoRenew:              true, Environment: "Sandbox", Status: "active",
	}
	expired := &Subscription{
		UserID: user.ID, Provider: "apple",
		ProviderSubscriptionID: fmt.Sprintf("OTX-E-%d", uniq),
		ProductID:              productID,
		CurrentPeriodEnd:       time.Now().Unix() - 10*86400,
		Status:                 "expired",
	}
	require.NoError(t, db.Get().Create(active).Error)
	require.NoError(t, db.Get().Create(expired).Error)
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&Subscription{}) })

	got := GetActiveSubscriptions(user.ID)
	require.Len(t, got, 1, "only the active sub is returned")
	assert.Equal(t, "apple", got[0].Provider)
	assert.Equal(t, "basic", got[0].Tier)
	assert.True(t, got[0].AutoRenew)
	assert.Equal(t, "apple_settings", got[0].Manage.Kind)
}
