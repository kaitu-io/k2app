package center

import (
	"testing"

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
