package center

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAbandonedTriggerDelays_Config(t *testing.T) {
	assert.Equal(t, []int{1}, abandonedHourlyDelays)
	assert.Equal(t, []int{1, 3, 7, 14, 30}, abandonedDailyDelays)
}

func TestAbandonedCampaigns_Config(t *testing.T) {
	_, ok := abandonedCampaigns[1]
	assert.False(t, ok, "1-day abandoned should have no campaign")

	c3, ok := abandonedCampaigns[3]
	require.True(t, ok, "3-day abandoned campaign must exist")
	assert.Equal(t, "READY4U", c3.code)
	assert.Equal(t, 95, c3.discountPct)

	c7, ok := abandonedCampaigns[7]
	require.True(t, ok, "7-day abandoned campaign must exist")
	assert.Equal(t, "STAYFREE", c7.code)
	assert.Equal(t, 90, c7.discountPct)

	c14, ok := abandonedCampaigns[14]
	require.True(t, ok, "14-day abandoned campaign must exist")
	assert.Equal(t, "SMOOTHDAY", c14.code)
	assert.Equal(t, 90, c14.discountPct)

	c30, ok := abandonedCampaigns[30]
	require.True(t, ok, "30-day abandoned campaign must exist")
	assert.Equal(t, "KEEPGOING", c30.code)
	assert.Equal(t, 85, c30.discountPct)
}

func TestAbandonedBatchIDFormat(t *testing.T) {
	now := time.Date(2026, 4, 8, 3, 0, 0, 0, time.UTC)
	nowStr := now.Format("2006-01-02")
	hourStr := now.Format("2006-01-02-15")

	batchID := fmt.Sprintf("abandoned:%dh:%s", 1, hourStr)
	assert.Equal(t, "abandoned:1h:2026-04-08-03", batchID)

	batchID = fmt.Sprintf("abandoned:%dd:%s", 3, nowStr)
	assert.Equal(t, "abandoned:3d:2026-04-08", batchID)
}

func TestAbandonedHourlyWindow(t *testing.T) {
	now := time.Date(2026, 4, 8, 3, 0, 0, 0, time.UTC)
	windowEnd := now.Add(-30 * time.Minute)
	windowStart := now.Add(-90 * time.Minute)

	assert.Equal(t, time.Date(2026, 4, 8, 1, 30, 0, 0, time.UTC), windowStart)
	assert.Equal(t, time.Date(2026, 4, 8, 2, 30, 0, 0, time.UTC), windowEnd)
	assert.Equal(t, time.Hour, windowEnd.Sub(windowStart))
}

func TestAbandonedDailyWindow(t *testing.T) {
	now := time.Date(2026, 4, 8, 2, 30, 0, 0, time.UTC)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	targetDate := today.AddDate(0, 0, -3)
	targetDateEnd := targetDate.AddDate(0, 0, 1)

	assert.Equal(t, time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC), targetDate)
	assert.Equal(t, time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC), targetDateEnd)
	assert.Equal(t, 24*time.Hour, targetDateEnd.Sub(targetDate))
}

func TestAbandonedSavingsCalculation(t *testing.T) {
	tests := []struct {
		name        string
		minPrice    uint64
		maxPrice    uint64
		discountPct int
		expectMin   string
		expectMax   string
	}{
		{"READY4U_95pct", 3900, 14900, 95, "$1.95", "$7.45"},
		{"STAYFREE_90pct", 3900, 14900, 90, "$3.9", "$14.9"},
		{"KEEPGOING_85pct", 3900, 14900, 85, "$5.85", "$22.35"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minSave := tt.minPrice * uint64(100-tt.discountPct) / 100
			maxSave := tt.maxPrice * uint64(100-tt.discountPct) / 100
			assert.Equal(t, tt.expectMin, formatCents(minSave))
			assert.Equal(t, tt.expectMax, formatCents(maxSave))
		})
	}
}
