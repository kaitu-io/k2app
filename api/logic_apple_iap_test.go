package center

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

// TestComputeAppleEntitlement covers the load-bearing entitlement math: Apple's
// absolute expiresDate only ever raises the user's expiry (never shortens), and
// re-delivery of the same expiry is a no-op (idempotent).
func TestComputeAppleEntitlement(t *testing.T) {
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
			gotExpiry, gotDays, gotAdvanced := computeAppleEntitlement(tc.current, tc.appleExpires, now)
			assert.Equal(t, tc.wantExpiry, gotExpiry, "expiry")
			assert.Equal(t, tc.wantDays, gotDays, "days")
			assert.Equal(t, tc.wantAdvanced, gotAdvanced, "advanced")
		})
	}
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
		db.Get().Where("original_transaction_id = ?", otx).Delete(&AppleSubscription{})
		db.Get().Where("user_id = ? AND type = ?", user.ID, VipAppleSub).Delete(&UserProHistory{})
	})

	// ---- first grant ----
	require.NoError(t, verifyAndGrantTransaction(ctx, user.ID, "TXN1"))

	var u1 User
	require.NoError(t, db.Get().First(&u1, user.ID).Error)
	assert.False(t, u1.IsExpired(), "user should be a member after grant")
	assert.InDelta(t, appleExpiresMs/1000, u1.ExpiredAt, 2, "expiry should equal apple expiresDate (sec)")
	assert.Equal(t, "family", u1.Tier, "tier set from plan on first order")

	var sub AppleSubscription
	require.NoError(t, db.Get().Where("original_transaction_id = ?", otx).First(&sub).Error)
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
	var subAfter AppleSubscription
	require.NoError(t, db.Get().Where("original_transaction_id = ?", otx).First(&subAfter).Error)
	assert.Equal(t, user.ID, subAfter.UserID, "subscription stays bound to the first user")
	var otherReloaded User
	require.NoError(t, db.Get().First(&otherReloaded, other.ID).Error)
	assert.True(t, otherReloaded.IsExpired(), "the second user must not receive entitlement")
}
