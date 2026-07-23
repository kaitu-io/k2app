package center

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTrafficDate_AsiaShanghai(t *testing.T) {
	// 2026-07-21 17:30 UTC == 2026-07-22 01:30 +08 → 应归 07-22
	utc := time.Date(2026, 7, 21, 17, 30, 0, 0, time.UTC)
	assert.Equal(t, "2026-07-22", trafficDate(utc))
	// 2026-07-21 15:59 UTC == 2026-07-21 23:59 +08 → 仍是 07-21
	utc2 := time.Date(2026, 7, 21, 15, 59, 0, 0, time.UTC)
	assert.Equal(t, "2026-07-21", trafficDate(utc2))
}

func TestTrafficMonthRange(t *testing.T) {
	start, end, err := trafficMonthRange("2026-07")
	assert.NoError(t, err)
	assert.Equal(t, "2026-07-01", start)
	assert.Equal(t, "2026-07-31", end)
	_, _, err = trafficMonthRange("garbage")
	assert.Error(t, err)
}
