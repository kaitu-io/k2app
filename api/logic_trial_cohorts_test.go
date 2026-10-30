package center

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeTrialCohorts_StatesAndRate(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	d28 := time.Date(2026, 10, 28, 7, 0, 0, 0, time.UTC)
	d29 := time.Date(2026, 10, 29, 23, 30, 0, 0, time.UTC)
	ended := d28.Add(7 * 24 * time.Hour).Unix() // 11-04 07:00，已过
	open := d29.Add(7 * 24 * time.Hour).Unix()  // 11-05 23:30，未到

	chains := []trialChain{
		// 10-28 批：全部已到期
		{StartedAt: d28, TrialEndsAt: ended, IsTrial: false, AutoRenew: true, Status: "active", PaidThrough: ended + 365*86400}, // 转付费
		{StartedAt: d28, TrialEndsAt: ended, IsTrial: true, AutoRenew: false, Status: "expired"},                                // 关了续费，到期
		{StartedAt: d28, TrialEndsAt: ended, IsTrial: true, AutoRenew: true, Status: "billing_retry"},                           // 扣费重试
		{StartedAt: d28, TrialEndsAt: ended, IsTrial: true, AutoRenew: true, Status: "active"},                                  // webhook 还没到：按时间算已结束
		{StartedAt: d28, TrialEndsAt: ended, IsTrial: false, AutoRenew: false, Status: "revoked", PaidThrough: ended},           // 付了又退
		// 10-29 批：都还在试用期
		{StartedAt: d29, TrialEndsAt: open, IsTrial: true, AutoRenew: true, Status: "active"},
		{StartedAt: d29, TrialEndsAt: open, IsTrial: true, AutoRenew: false, Status: "active"},
	}
	res := computeTrialCohorts(chains, now)

	require.Len(t, res.Rows, 2)
	r28, r29 := res.Rows[0], res.Rows[1]
	assert.Equal(t, "2026-10-28", r28.Cohort)
	assert.Equal(t, 5, r28.Started)
	assert.Equal(t, 1, r28.Converted)
	assert.Equal(t, 2, r28.EndedUnpaid)
	assert.Equal(t, 1, r28.BillingRetry)
	assert.Equal(t, 1, r28.Revoked)
	assert.Equal(t, 0, r28.InTrialRenew+r28.InTrialOff)
	assert.Equal(t, 5, r28.Matured)
	require.NotNil(t, r28.ConvRate)
	assert.InDelta(t, 0.2, *r28.ConvRate, 1e-9)

	// 23:30Z 落在 UTC 当天
	assert.Equal(t, "2026-10-29", r29.Cohort)
	assert.Equal(t, 1, r29.InTrialRenew)
	assert.Equal(t, 1, r29.InTrialOff)
	assert.Equal(t, 0, r29.Matured)
	assert.Nil(t, r29.ConvRate, "no matured chains → no rate")

	assert.Equal(t, 7, res.Totals.Started)
	assert.Equal(t, 5, res.Totals.Matured)
	require.NotNil(t, res.Totals.ConvRate)
	assert.InDelta(t, 0.2, *res.Totals.ConvRate, 1e-9)

	// 各状态互斥且加总 = Started
	for _, r := range append(res.Rows, res.Totals) {
		sum := r.InTrialRenew + r.InTrialOff + r.BillingRetry + r.Converted + r.EndedUnpaid + r.Revoked
		assert.Equal(t, r.Started, sum, r.Cohort)
	}
}

func TestComputeTrialCohorts_EmptyIsNotNull(t *testing.T) {
	res := computeTrialCohorts(nil, time.Now())
	assert.NotNil(t, res.Rows)
	assert.Empty(t, res.Rows)
	assert.Nil(t, res.Totals.ConvRate)
}
