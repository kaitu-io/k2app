package center

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanMarshalJSON_InjectsLegacyQuotaFields(t *testing.T) {
	plan := Plan{
		ID:    1,
		Tier:  TierFamily,
		Label: "Family Plan",
	}
	data, err := json.Marshal(plan)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))

	assert.Equal(t, float64(8), out["maxDevice"])
	assert.Equal(t, float64(1), out["maxRouterDevice"])
	assert.Equal(t, float64(20), out["maxLanClient"])
	assert.Equal(t, TierFamily, out["tier"])
}

func TestUserMarshalJSON_InjectsLegacyQuotaFields_Active(t *testing.T) {
	user := User{
		ID:        100,
		Tier:      TierBasic,
		ExpiredAt: time.Now().Add(24 * time.Hour).Unix(),
	}
	data, err := json.Marshal(user)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))

	assert.Equal(t, float64(5), out["maxDevice"])
	assert.Equal(t, float64(0), out["maxRouterDevice"])
	assert.Equal(t, float64(0), out["maxLanClient"])
}

func TestUserMarshalJSON_ExpiredUserGetsZeroQuota(t *testing.T) {
	user := User{
		ID:        100,
		Tier:      TierFamily,
		ExpiredAt: time.Now().Add(-24 * time.Hour).Unix(),
	}
	data, err := json.Marshal(user)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))

	assert.Equal(t, float64(0), out["maxDevice"], "expired user must show zero quota")
}
