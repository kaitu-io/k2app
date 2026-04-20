package center

// Note: db.Get() is a sync.Once package global with no Set() — handler-level
// integration tests require qtoolkit upstream support. See api_stats_test.go
// for precedent (handlers that early-return before DB use can still be tested
// at the router level).

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildTierInfos_ReturnsAll4TiersInRankOrder verifies the pure assembly
// helper without touching the database. Asserts the contract exposed via
// GET /api/tiers: all 4 tiers in ascending rank order with correct quotas.
func TestBuildTierInfos_ReturnsAll4TiersInRankOrder(t *testing.T) {
	tiers := buildTierInfos()
	require.Len(t, tiers, 4, "must return exactly 4 tiers")

	// rank ascending
	assert.Equal(t, "lite", tiers[0].Name)
	assert.Equal(t, 1, tiers[0].Rank)
	assert.Equal(t, "basic", tiers[1].Name)
	assert.Equal(t, 2, tiers[1].Rank)
	assert.Equal(t, "family", tiers[2].Name)
	assert.Equal(t, 3, tiers[2].Rank)
	assert.Equal(t, "business", tiers[3].Name)
	assert.Equal(t, 4, tiers[3].Rank)

	// quota sanity
	assert.Equal(t, 1, tiers[0].MaxDevice, "lite max device")
	assert.Equal(t, 5, tiers[1].MaxDevice, "basic max device")
	assert.Equal(t, 8, tiers[2].MaxDevice, "family max device")
	assert.Equal(t, 1, tiers[2].MaxRouterDevice, "family router device")
	assert.Equal(t, 20, tiers[2].MaxLanClient, "family lan client")
	assert.Equal(t, 20, tiers[3].MaxDevice, "business max device")
	assert.Equal(t, -1, tiers[3].MaxLanClient, "business unlimited lan")

	// plans must be empty before DB lookup
	for _, ti := range tiers {
		assert.Nil(t, ti.Plans, "buildTierInfos must not populate Plans")
	}
}

// TestGetAdminTiers_HandlerExists is a compile-time smoke check that the
// admin variant of the tiers endpoint is wired. The handler's
// includeInactive=true behavior is exercised by integration tests against a
// real DB — the qtoolkit/db global cannot be mocked here (see file header
// note + api_stats_test.go precedent).
func TestGetAdminTiers_HandlerExists(t *testing.T) {
	var _ gin.HandlerFunc = GetAdminTiers
	var _ gin.HandlerFunc = GetTiers
}
