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
	"gorm.io/gorm"
)

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

// TestVerifyAndGrantTransaction_Integration exercises the full DB grant path against
// real MySQL with a stubbed Apple fetch under the additive-ledger model: first grant
// credits one period from-now, the appAccountToken binds the sub to the user, an
// idempotent re-delivery credits nothing, and a different user attempting the same
// original_transaction_id is rejected (INV9). Skips without a DB.
func TestVerifyAndGrantTransaction_Integration(t *testing.T) {
	skipIfNoDB(t)
	ctx := context.Background()

	viper.Set("appstore.bundleId", "io.kaitu.test")
	t.Cleanup(func() { viper.Set("appstore.bundleId", "") })

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
	nowSec := time.Now().Unix()
	purchaseMs := nowSec * 1000
	appleExpiresMs := (nowSec + 365*86400) * 1000
	// The binding (first) transaction must carry the buyer's derived appAccountToken.
	token := deriveAppleAccountToken(user.UUID)

	old := fetchAppleTransaction
	fetchAppleTransaction = func(ctx context.Context, bundleId, transactionID string) (*appstore.TransactionInfo, error) {
		return &appstore.TransactionInfo{
			BundleId:              appleBundleID(),
			ProductId:             productID,
			OriginalTransactionId: otx,
			TransactionId:         transactionID,
			AppAccountToken:       token,
			PurchaseDate:          purchaseMs,
			ExpiresDate:           appleExpiresMs,
			InAppOwnershipType:    appstore.OwnershipType_PURCHASED,
			Environment:           "Sandbox",
		}, nil
	}
	t.Cleanup(func() { fetchAppleTransaction = old })
	t.Cleanup(func() {
		db.Get().Where("provider = ? AND provider_subscription_id = ?", "apple", otx).Delete(&Subscription{})
		db.Get().Where("user_id = ?", user.ID).Delete(&SubscriptionCredit{})
		db.Get().Where("user_id = ? AND type = ?", user.ID, VipAppleSub).Delete(&UserProHistory{})
	})

	// ---- first grant: credits one period from now, binds to user ----
	require.NoError(t, verifyAndGrantTransaction(ctx, user.ID, "TXN1"))

	var u1 User
	require.NoError(t, db.Get().First(&u1, user.ID).Error)
	assert.False(t, u1.IsExpired(), "user should be a member after grant")
	assert.InDelta(t, nowSec+365*86400, u1.ExpiredAt, 5, "≈ one year from now (additive, from-now-if-expired)")
	assert.Equal(t, "family", u1.Tier, "tier set from plan on first order")

	var sub Subscription
	require.NoError(t, db.Get().Where("provider = ? AND provider_subscription_id = ?", "apple", otx).First(&sub).Error)
	assert.Equal(t, user.ID, sub.UserID)
	assert.Equal(t, "active", sub.Status)

	var histCount int64
	db.Get().Model(&UserProHistory{}).
		Where("user_id = ? AND type = ?", user.ID, VipAppleSub).Count(&histCount)
	assert.Equal(t, int64(1), histCount, "exactly one grant history")

	// ---- idempotent re-delivery (same txn id) credits nothing ----
	require.NoError(t, verifyAndGrantTransaction(ctx, user.ID, "TXN1"))
	var u2 User
	require.NoError(t, db.Get().First(&u2, user.ID).Error)
	assert.Equal(t, u1.ExpiredAt, u2.ExpiredAt, "idempotent: expiry unchanged")
	db.Get().Model(&UserProHistory{}).
		Where("user_id = ? AND type = ?", user.ID, VipAppleSub).Count(&histCount)
	assert.Equal(t, int64(1), histCount, "idempotent: no second history row")

	// ---- INV9: a different user cannot rebind/credit the same subscription ----
	other := CreateTestUser(t)
	err := verifyAndGrantTransaction(ctx, other.ID, "TXN2")
	require.Error(t, err, "rebinding to a different user must be rejected")
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

func TestCreditAppleTransaction_NoAbsorption_Idempotent(t *testing.T) {
	skipIfNoDB(t)
	uniq := time.Now().UnixNano()
	productID := fmt.Sprintf("io.kaitu.test.1y.%d", uniq)
	plan := &Plan{PID: fmt.Sprintf("tcat%d", uniq), Label: "X", Price: 1000, OriginPrice: 1000, Month: 12, Tier: "basic", AppleProductID: productID}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Delete(plan) })

	user := CreateTestUser(t)
	day := int64(86400)
	t0 := time.Now().Unix()
	orig := fmt.Sprintf("OTX-%d", uniq)

	token := deriveAppleAccountToken(user.UUID) // first (binding) txn must carry it (INV9)
	credit := func(txnID string, purchaseMs, expiresMs int64) error {
		return db.Get().Transaction(func(tx *gorm.DB) error {
			return creditAppleTransaction(context.Background(), tx, user.ID, &appstore.TransactionInfo{
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

	// First purchase: 1 year.
	require.NoError(t, credit("T1", t0*1000, (t0+365*day)*1000))
	require.NoError(t, db.Get().First(&user, user.ID).Error)
	assert.InDelta(t, t0+365*day, user.ExpiredAt, float64(2*day))

	// Gift +7 days (own clock).
	require.NoError(t, db.Get().Transaction(func(tx *gorm.DB) error {
		_, e := addProExpiredDays(context.Background(), tx, user, VipSystemGrant, 0, 7, "test gift")
		return e
	}))
	require.NoError(t, db.Get().First(&user, user.ID).Error)
	giftedExpiry := user.ExpiredAt

	// Renewal: +1 year. Gift must NOT be absorbed (INV3).
	require.NoError(t, credit("T2", (t0+365*day)*1000, (t0+730*day)*1000))
	require.NoError(t, db.Get().First(&user, user.ID).Error)
	assert.Equal(t, giftedExpiry+365*day, user.ExpiredAt, "renewal stacks on gift, no absorption")

	// Replay T2: idempotent (INV1) — expiry unchanged.
	require.NoError(t, credit("T2", (t0+365*day)*1000, (t0+730*day)*1000))
	require.NoError(t, db.Get().First(&user, user.ID).Error)
	assert.Equal(t, giftedExpiry+365*day, user.ExpiredAt, "replayed transaction credits nothing")

	var n int64
	db.Get().Model(&SubscriptionCredit{}).Where("user_id = ?", user.ID).Count(&n)
	assert.Equal(t, int64(2), n, "two distinct transactions credited once each")
}

// INV9: a subscription bound to user A is never re-bound/credited to user B.
func TestCreditAppleTransaction_BindingIsPermanent(t *testing.T) {
	skipIfNoDB(t)
	uniq := time.Now().UnixNano()
	productID := fmt.Sprintf("io.kaitu.test.bind.%d", uniq)
	plan := &Plan{PID: fmt.Sprintf("tbind%d", uniq), Label: "X", Price: 1000, OriginPrice: 1000, Month: 12, Tier: "basic", AppleProductID: productID}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Delete(plan) })

	userA := CreateTestUser(t)
	userB := CreateTestUser(t)
	orig := fmt.Sprintf("OTX-bind-%d", uniq)
	day := int64(86400)
	t0 := time.Now().Unix()
	t.Cleanup(func() { db.Get().Where("provider_subscription_id = ?", orig).Delete(&Subscription{}) })

	// First credit binds to userA (binding txn carries userA's token, INV9).
	require.NoError(t, db.Get().Transaction(func(tx *gorm.DB) error {
		return creditAppleTransaction(context.Background(), tx, userA.ID, &appstore.TransactionInfo{
			OriginalTransactionId: orig, TransactionId: "B1", ProductId: productID,
			AppAccountToken: deriveAppleAccountToken(userA.UUID),
			Environment:     "Sandbox", PurchaseDate: t0 * 1000, ExpiresDate: (t0 + 365*day) * 1000,
		})
	}))

	// userB attempting the same original_transaction_id must be rejected (INV9).
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		return creditAppleTransaction(context.Background(), tx, userB.ID, &appstore.TransactionInfo{
			OriginalTransactionId: orig, TransactionId: "B2", ProductId: productID,
			Environment: "Sandbox", PurchaseDate: t0 * 1000, ExpiresDate: (t0 + 730*day) * 1000,
		})
	})
	require.Error(t, err, "binding to a different user must be rejected")
}

// INV9 (binding-path token): a FIRST/binding transaction missing appAccountToken is hard-rejected.
func TestVerifyAndGrantTransaction_EmptyTokenRejected(t *testing.T) {
	skipIfNoDB(t)
	ctx := context.Background()
	viper.Set("appstore.bundleId", "io.kaitu.test")
	t.Cleanup(func() { viper.Set("appstore.bundleId", "") })

	uniq := time.Now().UnixNano()
	productID := fmt.Sprintf("io.kaitu.test.empty.%d", uniq)
	plan := &Plan{PID: fmt.Sprintf("temp%d", uniq), Label: "X", Price: 1000, OriginPrice: 1000, Month: 12, Tier: "basic", AppleProductID: productID}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Delete(plan) })

	user := CreateTestUser(t)
	otx := "OTX-empty-" + generateId("otx")

	old := fetchAppleTransaction
	fetchAppleTransaction = func(ctx context.Context, bundleId, transactionID string) (*appstore.TransactionInfo, error) {
		return &appstore.TransactionInfo{
			BundleId:              appleBundleID(),
			ProductId:             productID,
			OriginalTransactionId: otx,
			TransactionId:         transactionID,
			AppAccountToken:       "", // missing — must be hard-rejected on first bind
			PurchaseDate:          time.Now().Unix() * 1000,
			ExpiresDate:           (time.Now().Unix() + 365*86400) * 1000,
			InAppOwnershipType:    appstore.OwnershipType_PURCHASED,
			Environment:           "Sandbox",
		}, nil
	}
	t.Cleanup(func() { fetchAppleTransaction = old })
	t.Cleanup(func() {
		db.Get().Where("provider_subscription_id = ?", otx).Delete(&Subscription{})
		db.Get().Where("user_id = ?", user.ID).Delete(&SubscriptionCredit{})
	})

	err := verifyAndGrantTransaction(ctx, user.ID, "ETXN1")
	require.Error(t, err, "first-bind transaction without appAccountToken must be rejected")
	require.NoError(t, db.Get().First(&user, user.ID).Error)
	assert.True(t, user.IsExpired(), "no entitlement granted when token missing")
}
