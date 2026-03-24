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

func TestGetCampaignMatcherWithDB_FirstOrder(t *testing.T) {
	matcher := getCampaignMatcherWithDB(nil, "first_order", "")
	assert.NotNil(t, matcher)

	done := true
	notDone := false

	assert.True(t, matcher(context.Background(), &User{IsFirstOrderDone: &done}, nil))
	assert.False(t, matcher(context.Background(), &User{IsFirstOrderDone: &notDone}, nil))
	assert.False(t, matcher(context.Background(), &User{IsFirstOrderDone: nil}, nil))
}
