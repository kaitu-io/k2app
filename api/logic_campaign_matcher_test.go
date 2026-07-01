package center

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetCampaignMatcherWithDB_PaidBefore_NilDB(t *testing.T) {
	// When DB is nil and matcherType is "paid_before", matcher should return false
	// (can't query orders without DB)
	cutoff := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	params := fmt.Sprintf(`{"beforeDate": %d}`, cutoff)

	matcher := getCampaignMatcherWithDB(nil, "paid_before", params)
	assert.NotNil(t, matcher, "matcher function should not be nil")

	user := &User{ID: 1}
	// With nil DB, querying orders is impossible — matcher should return false safely
	result := matcher(context.Background(), user, nil)
	assert.False(t, result, "should return false when DB is nil")
}

func TestGetCampaignMatcherWithDB_PaidBefore_ZeroDate(t *testing.T) {
	// When beforeDate is 0 (invalid), matcher should always return false
	matcher := getCampaignMatcherWithDB(nil, "paid_before", `{"beforeDate": 0}`)
	assert.NotNil(t, matcher)

	result := matcher(context.Background(), &User{ID: 1}, nil)
	assert.False(t, result, "zero beforeDate should always return false")
}

func TestGetCampaignMatcherWithDB_PaidBeforeActive_ZeroDate(t *testing.T) {
	matcher := getCampaignMatcherWithDB(nil, "paid_before_active", `{"beforeDate": 0}`)
	assert.NotNil(t, matcher)

	result := matcher(context.Background(), &User{ID: 1}, nil)
	assert.False(t, result, "zero beforeDate should always return false")
}

func TestGetCampaignMatcherWithDB_UnknownType(t *testing.T) {
	matcher := getCampaignMatcherWithDB(nil, "unknown_type", "")
	assert.Nil(t, matcher, "unknown matcher type should return nil")
}

func TestGetCampaignMatcherWithDB_All(t *testing.T) {
	matcher := getCampaignMatcherWithDB(nil, "all", "")
	assert.NotNil(t, matcher)
	assert.True(t, matcher(context.Background(), &User{ID: 1}, nil), "'all' matcher should always return true")
}

// first_order matches NEW customers — users who have not yet completed a first
// (paid) order. This is the meaning the name and the admin label ("新客") imply.
// The already-paid case is served by the "vip" matcher (see mirror test below).
func TestGetCampaignMatcherWithDB_FirstOrder(t *testing.T) {
	matcher := getCampaignMatcherWithDB(nil, "first_order", "")
	assert.NotNil(t, matcher)

	done := true
	notDone := false

	// New customer (first order not done, or never recorded) → matches.
	assert.True(t, matcher(context.Background(), &User{IsFirstOrderDone: &notDone}, nil),
		"first_order should match a user who has not completed their first order")
	assert.True(t, matcher(context.Background(), &User{IsFirstOrderDone: nil}, nil),
		"first_order should treat a nil flag as a new customer")
	// Already-paid customer → does NOT match (that's what "vip" is for).
	assert.False(t, matcher(context.Background(), &User{IsFirstOrderDone: &done}, nil),
		"first_order should not match a user who already completed a first order")
}

// vip is the mirror of first_order: it matches already-paid customers and is used
// for renewal / win-back campaigns. Kept as a regression guard so the two matchers
// can never silently collapse into the same meaning again.
func TestGetCampaignMatcherWithDB_Vip(t *testing.T) {
	matcher := getCampaignMatcherWithDB(nil, "vip", "")
	assert.NotNil(t, matcher)

	done := true
	notDone := false

	assert.True(t, matcher(context.Background(), &User{IsFirstOrderDone: &done}, nil),
		"vip should match a user who already completed a first order")
	assert.False(t, matcher(context.Background(), &User{IsFirstOrderDone: &notDone}, nil),
		"vip should not match a new customer")
	assert.False(t, matcher(context.Background(), &User{IsFirstOrderDone: nil}, nil),
		"vip should not match a user with a nil flag")
}
