package center

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTierConstants(t *testing.T) {
	assert.Equal(t, "lite", TierLite)
	assert.Equal(t, "basic", TierBasic)
	assert.Equal(t, "family", TierFamily)
	assert.Equal(t, "business", TierBusiness)
}

func TestTierQuotas_AllFourTiers(t *testing.T) {
	require.Len(t, TierQuotas, 4)

	assert.Equal(t, TierQuota{MaxDevice: 1, MaxRouterDevice: 0, MaxLanClient: 0}, TierQuotas[TierLite].TierQuota)
	assert.Equal(t, TierQuota{MaxDevice: 5, MaxRouterDevice: 0, MaxLanClient: 0}, TierQuotas[TierBasic].TierQuota)
	assert.Equal(t, TierQuota{MaxDevice: 8, MaxRouterDevice: 1, MaxLanClient: 20}, TierQuotas[TierFamily].TierQuota)
	assert.Equal(t, TierQuota{MaxDevice: 20, MaxRouterDevice: 3, MaxLanClient: -1}, TierQuotas[TierBusiness].TierQuota)
}

func TestTierQuotas_RankStrictlyIncreasing(t *testing.T) {
	assert.Equal(t, 1, TierQuotas[TierLite].Rank)
	assert.Equal(t, 2, TierQuotas[TierBasic].Rank)
	assert.Equal(t, 3, TierQuotas[TierFamily].Rank)
	assert.Equal(t, 4, TierQuotas[TierBusiness].Rank)
}

func TestAllTiers_SortedByRank(t *testing.T) {
	all := AllTiers()
	require.Len(t, all, 4)
	for i := 1; i < len(all); i++ {
		assert.Less(t, all[i-1].Rank, all[i].Rank, "tiers must be in ascending rank order")
	}
	assert.Equal(t, TierLite, all[0].Name)
	assert.Equal(t, TierBusiness, all[3].Name)
}

func TestIsValidTier(t *testing.T) {
	assert.True(t, IsValidTier(TierLite))
	assert.True(t, IsValidTier(TierBasic))
	assert.True(t, IsValidTier(TierFamily))
	assert.True(t, IsValidTier(TierBusiness))
	assert.False(t, IsValidTier("pro"))
	assert.False(t, IsValidTier(""))
	assert.False(t, IsValidTier("free"))
	assert.False(t, IsValidTier("BASIC"))
}

func TestZeroQuota(t *testing.T) {
	assert.Equal(t, TierQuota{}, ZeroQuota)
	assert.Equal(t, 0, ZeroQuota.MaxDevice)
}

func TestUserQuota_ActiveUser(t *testing.T) {
	user := &User{Tier: TierFamily, ExpiredAt: time.Now().Add(24 * time.Hour).Unix()}
	q := user.Quota()
	assert.Equal(t, 8, q.MaxDevice)
	assert.Equal(t, 1, q.MaxRouterDevice)
	assert.Equal(t, 20, q.MaxLanClient)
}

func TestUserQuota_ExpiredUserKeepsTierQuota(t *testing.T) {
	// Expiry must NOT zero out the quota. checkDeviceLimitOrKick (logic_auth.go)
	// uses quota.MaxDevice on every login; returning ZeroQuota for expired users
	// makes appDeviceCount >= 0 always true and kicks the oldest device (or fails
	// login with ErrRecordNotFound when there are zero devices). Legacy behavior
	// kept persisted user.MaxDevice=5 after expiry so users could still log in
	// and renew.
	user := &User{Tier: TierFamily, ExpiredAt: time.Now().Add(-24 * time.Hour).Unix()}
	q := user.Quota()
	assert.Equal(t, 8, q.MaxDevice, "expired family user still exposes family-tier MaxDevice")
	assert.Equal(t, 1, q.MaxRouterDevice)
	assert.Equal(t, 20, q.MaxLanClient)
}

func TestUserQuota_UnpaidUserDefaultsToBasic(t *testing.T) {
	// New signups have ExpiredAt=0 (never paid), but DB column default sets
	// tier='basic'. Quota must return basic-tier values so they can register
	// up to 5 app devices before a purchase — matching pre-change column default.
	user := &User{Tier: TierBasic, ExpiredAt: 0}
	q := user.Quota()
	assert.Equal(t, 5, q.MaxDevice)
	assert.Equal(t, 0, q.MaxRouterDevice)
	assert.Equal(t, 0, q.MaxLanClient)
}

func TestUserQuota_InvalidTierFallsBackToBasic(t *testing.T) {
	user := &User{Tier: "garbage_tier_value", ExpiredAt: time.Now().Add(24 * time.Hour).Unix()}
	q := user.Quota()
	assert.Equal(t, 5, q.MaxDevice, "invalid tier should fall back to basic quota")
}

func TestPlanQuota_ValidTier(t *testing.T) {
	plan := &Plan{Tier: TierBusiness}
	q := plan.Quota()
	assert.Equal(t, 20, q.MaxDevice)
	assert.Equal(t, 3, q.MaxRouterDevice)
	assert.Equal(t, -1, q.MaxLanClient)
}

func TestPlanQuota_InvalidTierFallsBackToBasic(t *testing.T) {
	plan := &Plan{Tier: ""}
	assert.Equal(t, 5, plan.Quota().MaxDevice)
}
