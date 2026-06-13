package center

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckDeviceLimit_NoLineRejected verifies that a user with no active
// private line is rejected with ErrorPlanNoRouter (402001) on a gateway/router
// login — regardless of app tier (router access is line-gated, not tier-gated).
func TestCheckDeviceLimit_NoLineRejected(t *testing.T) {
	m := SetupMockDB(t)

	user := &User{ID: 1, Tier: TierBasic} // tier irrelevant; absence of a private line is what rejects
	// HasActivePrivateLines issues a SELECT on private_node_subscriptions; return zero rows.
	m.Mock.ExpectQuery(`SELECT.+FROM .private_node_subscriptions`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "expires_at"}))

	err := checkDeviceLimitOrKick(context.Background(), m.DB, user, true)
	require.Error(t, err)
	var re rerr
	require.True(t, errors.As(err, &re), "expected rerr, got %T: %v", err, err)
	assert.Equal(t, ErrorPlanNoRouter, re.code)
}

// TestCheckDeviceLimit_RouterSlotFull verifies that a user who owns an active
// private line but already has one router device is rejected with
// ErrorRouterDeviceLimit (one router per account, cap=1).
func TestCheckDeviceLimit_RouterSlotFull(t *testing.T) {
	m := SetupMockDB(t)
	now := time.Now().Unix()

	user := &User{ID: 2, Tier: TierFamily}
	// HasActivePrivateLines: one serviceable active line (future expiry).
	m.Mock.ExpectQuery(`SELECT.+FROM .private_node_subscriptions`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "expires_at"}).
			AddRow(1, PNStatusActive, now+86400))
	// Existing router count = 1 = cap reached.
	m.Mock.ExpectQuery(`SELECT count\(.+\) FROM .devices.+is_gateway = true`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	err := checkDeviceLimitOrKick(context.Background(), m.DB, user, true)
	require.Error(t, err)
	var re rerr
	require.True(t, errors.As(err, &re), "expected rerr, got %T: %v", err, err)
	assert.Equal(t, ErrorRouterDeviceLimit, re.code)
}
