package center

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckDeviceLimit_BasicTierRoutersRejected verifies that a user on the
// basic tier (MaxRouterDevice == 0) is rejected with ErrorPlanNoRouter (402001)
// when attempting a gateway/router login.
func TestCheckDeviceLimit_BasicTierRoutersRejected(t *testing.T) {
	m := SetupMockDB(t)

	user := &User{ID: 1, Tier: TierBasic} // TierQuotas["basic"].MaxRouterDevice == 0
	err := checkDeviceLimitOrKick(context.Background(), m.DB, user, true)
	require.Error(t, err)
	var re rerr
	require.True(t, errors.As(err, &re), "expected rerr, got %T: %v", err, err)
	assert.Equal(t, ErrorPlanNoRouter, re.code)
}

// TestCheckDeviceLimit_RouterSlotFull verifies that a user on the family tier
// (MaxRouterDevice == 1) who already has one router device is rejected with
// ErrorRouterDeviceLimit (403001).
func TestCheckDeviceLimit_RouterSlotFull(t *testing.T) {
	m := SetupMockDB(t)

	user := &User{ID: 2, Tier: TierFamily} // TierQuotas["family"].MaxRouterDevice == 1
	// Mock count query: existing router count = 1 = limit reached.
	m.Mock.ExpectQuery(`SELECT count\(.+\) FROM .devices.+is_gateway = true`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	err := checkDeviceLimitOrKick(context.Background(), m.DB, user, true)
	require.Error(t, err)
	var re rerr
	require.True(t, errors.As(err, &re), "expected rerr, got %T: %v", err, err)
	assert.Equal(t, ErrorRouterDeviceLimit, re.code)
}
