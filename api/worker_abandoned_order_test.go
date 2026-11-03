package center

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAbandonedTriggerDelays_Config(t *testing.T) {
	assert.Equal(t, []int{1}, abandonedHourlyDelays)
	assert.Equal(t, []int{1, 3, 7}, abandonedDailyDelays, "14 / 30 天加码邮件已停发")
}

// 未付款召回不逐步加深：带优惠的阶段只有 3 天、7 天，且用同一个新客码。
func TestAbandonedOfferStages_NoEscalation(t *testing.T) {
	assert.Equal(t, map[int]bool{3: true, 7: true}, abandonedOfferStages)
	for _, d := range abandonedDailyDelays {
		if d < 3 {
			assert.False(t, abandonedOfferStages[d], "%d 天只提醒、不带折扣", d)
		}
	}
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
