package center

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func retUTC(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, time.UTC)
}

func retFrac(t *testing.T, got *float64, want float64, name string) {
	t.Helper()
	require.NotNil(t, got, name)
	assert.InDelta(t, want, *got, 1e-9, name)
}

func TestPaidCohorts(t *testing.T) {
	now := retUTC(2026, 10, 1, 0)
	first := retUTC(2026, 1, 15, 12)

	t.Run("single retained user, future checkpoint is nil", func(t *testing.T) {
		rows := computePaidCohorts(
			map[uint64]time.Time{1: first},
			map[uint64]int64{1: retUTC(2027, 1, 15, 12).Unix()},
			nil, now)
		require.Len(t, rows, 1)
		assert.Equal(t, "2026-01", rows[0].Cohort)
		assert.Equal(t, 1, rows[0].Size)
		assert.Equal(t, 0, rows[0].Refunded)
		retFrac(t, rows[0].Retained["m1"], 1, "m1")
		retFrac(t, rows[0].Retained["m3"], 1, "m3")
		retFrac(t, rows[0].Retained["m6"], 1, "m6")
		m12, has := rows[0].Retained["m12"]
		assert.True(t, has, "m12 key must be present")
		assert.Nil(t, m12, "m12 checkpoint (2027-01-15) is in the future")
	})

	t.Run("expired before checkpoint is not retained", func(t *testing.T) {
		rows := computePaidCohorts(
			map[uint64]time.Time{1: first, 2: retUTC(2026, 1, 15, 13)},
			map[uint64]int64{1: retUTC(2027, 1, 15, 12).Unix(), 2: retUTC(2026, 2, 10, 0).Unix()},
			nil, now)
		require.Len(t, rows, 1)
		assert.Equal(t, 2, rows[0].Size)
		retFrac(t, rows[0].Retained["m1"], 0.5, "m1")
		retFrac(t, rows[0].Retained["m6"], 0.5, "m6")
		assert.Nil(t, rows[0].Retained["m12"])
	})

	t.Run("refunded user counts in size and refunded, never retained", func(t *testing.T) {
		rows := computePaidCohorts(
			map[uint64]time.Time{1: first, 3: first},
			map[uint64]int64{1: retUTC(2027, 1, 15, 12).Unix(), 3: retUTC(2030, 1, 1, 0).Unix()},
			map[uint64]bool{3: true}, now)
		require.Len(t, rows, 1)
		assert.Equal(t, 2, rows[0].Size)
		assert.Equal(t, 1, rows[0].Refunded)
		retFrac(t, rows[0].Retained["m1"], 0.5, "m1")
		retFrac(t, rows[0].Retained["m3"], 0.5, "m3")
	})

	t.Run("expiry exactly at the checkpoint is retained", func(t *testing.T) {
		rows := computePaidCohorts(
			map[uint64]time.Time{1: first},
			map[uint64]int64{1: retUTC(2026, 2, 15, 12).Unix()}, nil, now)
		retFrac(t, rows[0].Retained["m1"], 1, "m1")
		retFrac(t, rows[0].Retained["m3"], 0, "m3")
	})

	t.Run("cohort by UTC month, ascending; missing expiry = not retained", func(t *testing.T) {
		rows := computePaidCohorts(
			map[uint64]time.Time{
				1: retUTC(2026, 3, 2, 0),
				2: retUTC(2025, 12, 31, 23),
				// 2026-01-31 23:30 UTC-2 == 2026-02-01 01:30 UTC → 二月群组
				3: time.Date(2026, 1, 31, 23, 30, 0, 0, time.FixedZone("x", -2*3600)),
			},
			map[uint64]int64{}, nil, now)
		require.Len(t, rows, 3)
		assert.Equal(t, []string{"2025-12", "2026-02", "2026-03"}, []string{rows[0].Cohort, rows[1].Cohort, rows[2].Cohort})
		retFrac(t, rows[0].Retained["m1"], 0, "m1")
	})

	t.Run("a checkpoint is nil until every member of the cohort has reached it", func(t *testing.T) {
		// now = 09-20：09-05 首付的 m1 检查点还没到 → 整组 m1 为 nil，不拿半组人算比例。
		rows := computePaidCohorts(
			map[uint64]time.Time{1: retUTC(2026, 8, 10, 0), 2: retUTC(2026, 8, 25, 0)},
			map[uint64]int64{1: retUTC(2027, 1, 1, 0).Unix(), 2: retUTC(2027, 1, 1, 0).Unix()},
			nil, retUTC(2026, 9, 20, 0))
		require.Len(t, rows, 1)
		assert.Nil(t, rows[0].Retained["m1"])
	})

	t.Run("empty input marshals rows as []", func(t *testing.T) {
		rows := computePaidCohorts(nil, nil, nil, now)
		b, err := json.Marshal(rows)
		require.NoError(t, err)
		assert.Equal(t, "[]", string(b))
	})

	t.Run("json shape", func(t *testing.T) {
		rows := computePaidCohorts(map[uint64]time.Time{1: first}, map[uint64]int64{1: retUTC(2027, 1, 15, 12).Unix()}, nil, now)
		b, err := json.Marshal(rows[0])
		require.NoError(t, err)
		assert.JSONEq(t, `{"cohort":"2026-01","size":1,"retained":{"m1":1,"m3":1,"m6":1,"m12":null},"refunded":0}`, string(b))
	})
}

func TestActiveCohorts(t *testing.T) {
	d := retUTC(2026, 9, 1, 0)

	t.Run("d1 hit, d7 miss, d30 not yet over", func(t *testing.T) {
		rows := computeActiveCohorts(map[string][]time.Time{
			"dev-a": {d.Add(26 * time.Hour), d.Add(9 * time.Hour), d.Add(10 * time.Hour)}, // 无序输入
		}, d.AddDate(0, 0, 10))
		require.Len(t, rows, 1)
		assert.Equal(t, "2026-09-01", rows[0].Cohort)
		assert.Equal(t, 1, rows[0].Size)
		retFrac(t, rows[0].D1, 1, "d1")
		retFrac(t, rows[0].D7, 0, "d7")
		assert.Nil(t, rows[0].D30)
	})

	t.Run("fractions over the cohort, cohorts ascending", func(t *testing.T) {
		rows := computeActiveCohorts(map[string][]time.Time{
			"a": {d.Add(time.Hour), d.AddDate(0, 0, 7).Add(23 * time.Hour)},
			"b": {d.Add(2 * time.Hour), d.AddDate(0, 0, 1)},
			"c": {d.AddDate(0, 0, -1).Add(23 * time.Hour)}, // 前一天的群组
			"d": {},
		}, d.AddDate(0, 0, 40))
		require.Len(t, rows, 2)
		assert.Equal(t, "2026-08-31", rows[0].Cohort)
		assert.Equal(t, 1, rows[0].Size)
		retFrac(t, rows[0].D1, 0, "c d1")
		assert.Equal(t, "2026-09-01", rows[1].Cohort)
		assert.Equal(t, 2, rows[1].Size)
		retFrac(t, rows[1].D1, 0.5, "d1")
		retFrac(t, rows[1].D7, 0.5, "d7")
		retFrac(t, rows[1].D30, 0, "d30")
	})

	t.Run("target day still running is nil, the moment it ends it counts", func(t *testing.T) {
		opens := map[string][]time.Time{"a": {d.Add(time.Hour), d.AddDate(0, 0, 1).Add(time.Hour)}}
		rows := computeActiveCohorts(opens, d.AddDate(0, 0, 1).Add(23*time.Hour))
		assert.Nil(t, rows[0].D1, "D+1 has not ended")
		rows = computeActiveCohorts(opens, d.AddDate(0, 0, 2))
		retFrac(t, rows[0].D1, 1, "d1")
	})

	t.Run("empty input and json shape", func(t *testing.T) {
		b, err := json.Marshal(computeActiveCohorts(nil, d))
		require.NoError(t, err)
		assert.Equal(t, "[]", string(b))
		rows := computeActiveCohorts(map[string][]time.Time{"a": {d}}, d.AddDate(0, 0, 3))
		b, err = json.Marshal(rows[0])
		require.NoError(t, err)
		assert.JSONEq(t, `{"cohort":"2026-09-01","size":1,"d1":0,"d7":null,"d30":null}`, string(b))
	})
}

func TestPaidCohortMonthRange(t *testing.T) {
	from, to := paidCohortRange(retUTC(2026, 10, 1, 5), 12)
	assert.True(t, from.Equal(retUTC(2025, 11, 1, 0)), "%v", from)
	assert.True(t, to.Equal(retUTC(2026, 11, 1, 0)), "%v", to)
	from, _ = paidCohortRange(retUTC(2026, 3, 31, 5), 1)
	assert.True(t, from.Equal(retUTC(2026, 3, 1, 0)), "%v", from)
}

// 付费群组的装载：只收首付落在区间内、且符合品牌的用户；到期时间与退款标记来自权威表。
func TestLoadPaidCohortInputs(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	from, to := factsWindow(t)

	in := factsUser(t, BrandKaitu, from.Add(-time.Hour))
	require.NoError(t, db.Get().Model(in).Update("expired_at", int64(1234567)).Error)
	factsOrder(t, in.ID, OrderChannelNextpay, factsPaid(from.Add(time.Hour)), factsRefunded(from.Add(2*time.Hour)))

	early := factsUser(t, BrandKaitu, from.Add(-48*time.Hour)) // 首付在区间之前 → 不属于这些群组
	factsOrder(t, early.ID, OrderChannelNextpay, factsPaid(from.Add(-time.Hour)))
	factsOrder(t, early.ID, OrderChannelNextpay, factsPaid(from.Add(time.Hour)))

	other := factsUser(t, BrandOverleap, from.Add(-time.Hour))
	factsCredit(t, other.ID, SubscriptionProviderStripe, "purchase", "", from.Add(3*time.Hour))

	payFirst, expiredAt, refunded, err := loadPaidCohortInputs(ctx, BrandKaitu, false, from, to)
	require.NoError(t, err)
	require.Contains(t, payFirst, in.ID)
	assert.True(t, payFirst[in.ID].Equal(from.Add(time.Hour)))
	assert.Equal(t, int64(1234567), expiredAt[in.ID])
	assert.True(t, refunded[in.ID])
	assert.NotContains(t, payFirst, early.ID)
	require.Contains(t, payFirst, other.ID)
	assert.False(t, refunded[other.ID])

	payFirst, _, _, err = loadPaidCohortInputs(ctx, BrandOverleap, true, from, to)
	require.NoError(t, err)
	assert.NotContains(t, payFirst, in.ID)
	assert.Contains(t, payFirst, other.ID)
}

// 活跃群组的装载：首次出现早于区间的设备整个排除；带品牌过滤时无品牌的历史行不计入。
func TestLoadActiveOpens(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	m := generateId("ret-open")
	hNew, hOld, hLegacy := m+"-new", m+"-old", m+"-legacy"
	t.Cleanup(func() {
		factsCleanup(t, db.Get().Where("device_hash IN ?", []string{hNew, hOld, hLegacy}).Delete(&StatAppOpen{}).Error)
	})
	now := time.Now().UTC()
	since := funnelUTCDay(now).AddDate(0, 0, -10)
	open := func(hash, brand string, at time.Time) {
		t.Helper()
		require.NoError(t, db.Get().Create(&StatAppOpen{
			DeviceHash: hash, OS: "test", AppVersion: "0", Brand: brand, CreatedAt: at, ReportedAt: at,
		}).Error)
	}
	open(hNew, "overleap", since.Add(30*time.Hour))
	open(hNew, "overleap", since.Add(60*time.Hour))
	open(hOld, "overleap", since.Add(-24*time.Hour))
	open(hOld, "overleap", since.Add(24*time.Hour))
	open(hLegacy, "", since.Add(24*time.Hour))

	opens, err := loadActiveOpens(ctx, BrandKaitu, false, since)
	require.NoError(t, err)
	assert.Len(t, opens[hNew], 2)
	assert.NotContains(t, opens, hOld, "first seen before the window")
	assert.Len(t, opens[hLegacy], 1)

	opens, err = loadActiveOpens(ctx, BrandOverleap, true, since)
	require.NoError(t, err)
	assert.Len(t, opens[hNew], 2)
	assert.NotContains(t, opens, hLegacy, "rows without a brand are excluded under a brand filter")

	opens, err = loadActiveOpens(ctx, BrandKaitu, true, since)
	require.NoError(t, err)
	assert.NotContains(t, opens, hNew)
}
