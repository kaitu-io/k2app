package center

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestAddMonthsClamped(t *testing.T) {
	at := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 13, 14, 15, 0, time.UTC) }
	cases := []struct {
		name string
		from time.Time
		n    int
		want time.Time
	}{
		{"plain", at(2026, 1, 15), 1, at(2026, 2, 15)},
		{"31st clamps to Feb 28", at(2026, 1, 31), 1, at(2026, 2, 28)},
		{"31st clamps to Feb 29 in a leap year", at(2028, 1, 31), 1, at(2028, 2, 29)},
		{"31st into a 30-day month", at(2026, 3, 31), 1, at(2026, 4, 30)},
		{"31st into a 31-day month is untouched", at(2026, 1, 31), 2, at(2026, 3, 31)},
		{"December into January", at(2026, 12, 31), 1, at(2027, 1, 31)},
		{"November 30 + 3 crosses the year, clamps to Feb 28", at(2026, 11, 30), 3, at(2027, 2, 28)},
		{"twelve months from Feb 29", at(2028, 2, 29), 12, at(2029, 2, 28)},
		{"zero months", at(2026, 5, 31), 0, at(2026, 5, 31)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := addMonthsClamped(tc.from, tc.n)
			assert.True(t, got.Equal(tc.want), "got %v want %v", got, tc.want)
		})
	}
	// 非 UTC 输入按 UTC 日历算。
	got := addMonthsClamped(time.Date(2026, 1, 31, 23, 0, 0, 0, time.FixedZone("x", -2*3600)), 1) // = 02-01 01:00Z
	assert.True(t, got.Equal(time.Date(2026, 3, 1, 1, 0, 0, 0, time.UTC)), "%v", got)
}

// paidOne 跑单用户群组，返回那一行。
func paidOne(t *testing.T, now time.Time, pays ...paidCoverage) PaidCohortRow {
	t.Helper()
	rows := computePaidCohorts(map[uint64][]paidCoverage{1: pays}, nil, now)
	require.Len(t, rows, 1)
	return rows[0]
}

func TestPaidCohorts(t *testing.T) {
	first := retUTC(2026, 1, 15, 12)
	now := retUTC(2027, 6, 1, 0) // 2026-01 群组的四个检查点全部已过
	month := func(at time.Time, n int) paidCoverage { return paidCoverage{At: at, Months: n} }

	t.Run("1-month plan never renewed: not retained at m1", func(t *testing.T) {
		row := paidOne(t, now, month(first, 1))
		assert.Equal(t, "2026-01", row.Cohort)
		assert.Equal(t, 1, row.Size)
		assert.Equal(t, 0, row.Refunded)
		for _, k := range []string{"m1", "m3", "m6", "m12"} {
			retFrac(t, row.Retained[k], 0, k)
		}
	})

	t.Run("checkpoint exactly equal to coverage end is not retained", func(t *testing.T) {
		// m1 检查点 = 02-15 12:00 + 7 天 = 02-22 12:00 = 首付 + 38 天。覆盖恰好到这一刻 → 严格大于才算。
		row := paidOne(t, now, paidCoverage{At: first, Seconds: int64(38 * 24 * 3600)})
		retFrac(t, row.Retained["m1"], 0, "m1 with coverage ending exactly at the checkpoint")
		// 多一秒覆盖就算。
		row = paidOne(t, now, paidCoverage{At: first, Seconds: int64(38*24*3600) + 1})
		retFrac(t, row.Retained["m1"], 1, "m1")
	})

	t.Run("1-month plan renewed monthly: retained while renewing", func(t *testing.T) {
		// 每月提前一天续费，共 4 笔（覆盖到 05-15）；之后不再续。
		var pays []paidCoverage
		for i := range 4 {
			pays = append(pays, month(addMonthsClamped(first, i).Add(-time.Duration(min(i, 1))*24*time.Hour), 1))
		}
		row := paidOne(t, now, pays...)
		retFrac(t, row.Retained["m1"], 1, "m1")
		retFrac(t, row.Retained["m3"], 1, "m3")
		retFrac(t, row.Retained["m6"], 0, "m6")
		retFrac(t, row.Retained["m12"], 0, "m12")
	})

	t.Run("12-month plan: m1/m3/m6 retained, m12 only with a renewal", func(t *testing.T) {
		row := paidOne(t, now, month(first, 12))
		retFrac(t, row.Retained["m1"], 1, "m1")
		retFrac(t, row.Retained["m3"], 1, "m3")
		retFrac(t, row.Retained["m6"], 1, "m6")
		retFrac(t, row.Retained["m12"], 0, "m12 without renewal")

		row = paidOne(t, now, month(first, 12), month(retUTC(2027, 1, 10, 0), 12))
		retFrac(t, row.Retained["m12"], 1, "m12 with renewal")

		// 满一年后 7 天内续上（01-20）：算留存。
		row = paidOne(t, now, month(first, 12), month(retUTC(2027, 1, 20, 0), 12))
		retFrac(t, row.Retained["m12"], 1, "m12 renewed within the grace period")

		// 续费发生在 m12 检查点（01-22 12:00）之后：检查点那一刻并没有覆盖。
		row = paidOne(t, now, month(first, 12), month(retUTC(2027, 1, 23, 0), 12))
		retFrac(t, row.Retained["m12"], 0, "renewal after the checkpoint")
	})

	t.Run("grace: on-time and slightly late renewals count, 8 days late does not", func(t *testing.T) {
		end := addMonthsClamped(first, 1) // 首个周期结束：02-15 12:00；m1 检查点 02-22 12:00
		retFrac(t, paidOne(t, now, month(first, 1), month(end, 1)).Retained["m1"], 1, "renewed exactly at period end")
		// 订阅入账行晚几秒 / 几小时才写：没有宽限时这两个都会读成流失。
		retFrac(t, paidOne(t, now, month(first, 1), month(end.Add(5*time.Second), 1)).Retained["m1"], 1, "seconds late")
		retFrac(t, paidOne(t, now, month(first, 1), month(end.Add(3*time.Hour), 1)).Retained["m1"], 1, "hours late")
		// 晚 6 天：覆盖从这笔晚到的付款重新起算（到 03-21 12:00）。
		row := paidOne(t, now, month(first, 1), month(end.Add(6*24*time.Hour), 1))
		retFrac(t, row.Retained["m1"], 1, "6 days late")
		// 晚 8 天（02-23 12:00）买 3 个月：m1 检查点时没有覆盖，m3（04-22 12:00）时有（到 05-23）。
		row = paidOne(t, now, month(first, 1), month(end.Add(8*24*time.Hour), 3))
		retFrac(t, row.Retained["m1"], 0, "8 days late")
		retFrac(t, row.Retained["m3"], 1, "m3 after an 8-day-late return")
		retFrac(t, row.Retained["m6"], 0, "m6")
	})

	t.Run("grace: now inside the grace period of the latest member is null", func(t *testing.T) {
		pays := map[uint64][]paidCoverage{1: {month(retUTC(2026, 8, 1, 0), 12)}, 2: {month(retUTC(2026, 8, 15, 0), 12)}}
		// 最晚成员的 m1 检查点 = 09-15 + 7 天 = 09-22 00:00。
		rows := computePaidCohorts(pays, nil, retUTC(2026, 9, 18, 0))
		assert.Nil(t, rows[0].Retained["m1"], "past the month mark but still inside the grace period")
		rows = computePaidCohorts(pays, nil, retUTC(2026, 9, 21, 23))
		assert.Nil(t, rows[0].Retained["m1"])
		rows = computePaidCohorts(pays, nil, retUTC(2026, 9, 22, 0))
		retFrac(t, rows[0].Retained["m1"], 1, "m1 once the grace period has passed")
	})

	t.Run("lapse and return: gap at m3, back by m6", func(t *testing.T) {
		row := paidOne(t, now, month(first, 1), month(retUTC(2026, 6, 20, 0), 3)) // 覆盖 06-20 → 09-20
		retFrac(t, row.Retained["m1"], 0, "m1")
		retFrac(t, row.Retained["m3"], 0, "m3")
		retFrac(t, row.Retained["m6"], 1, "m6 (07-15)")
		retFrac(t, row.Retained["m12"], 0, "m12")
	})

	t.Run("returning after a gap restarts from the payment, not from the old expiry", func(t *testing.T) {
		// 首付 1 个月（到 02-15）；07-14 再买 1 个月 → 覆盖 07-14..08-14，m6（07-15）有覆盖。
		row := paidOne(t, now, month(first, 1), month(retUTC(2026, 7, 14, 0), 1))
		retFrac(t, row.Retained["m6"], 1, "m6")
	})

	t.Run("early renewal stacks from the previous expiry, not from the payment time", func(t *testing.T) {
		// 首付 3 个月（到 04-15 12:00）；02-01 提前再买 3 个月 → 叠到 07-15 12:00；
		// 02-02 再叠 7 天 + 1 秒 → 07-22 12:00:01，刚好越过 m6 检查点（07-22 12:00）。
		// 若错误地从付款时刻起算，最后只覆盖到 02-09，m3 / m6 都会变。
		row := paidOne(t, now, month(first, 3), month(retUTC(2026, 2, 1, 0), 3),
			paidCoverage{At: retUTC(2026, 2, 2, 0), Seconds: 7*24*3600 + 1})
		retFrac(t, row.Retained["m3"], 1, "m3")
		retFrac(t, row.Retained["m6"], 1, "m6: 07-22 12:00:01 > 07-22 12:00")
		retFrac(t, row.Retained["m12"], 0, "m12")
	})

	t.Run("refunded first order: retained nowhere, refunded = 1", func(t *testing.T) {
		rows := computePaidCohorts(map[uint64][]paidCoverage{
			7: {{At: first, Months: 12, Refunded: true}},
		}, map[uint64]bool{7: true}, now)
		require.Len(t, rows, 1)
		assert.Equal(t, 1, rows[0].Size)
		assert.Equal(t, 1, rows[0].Refunded)
		for _, k := range []string{"m1", "m3", "m6", "m12"} {
			retFrac(t, rows[0].Retained[k], 0, k)
		}
	})

	t.Run("refunded flag is informational: a later valid payment still counts", func(t *testing.T) {
		rows := computePaidCohorts(map[uint64][]paidCoverage{
			7: {{At: first, Months: 1, Refunded: true}, {At: first.Add(time.Hour), Months: 12}},
		}, map[uint64]bool{7: true}, now)
		assert.Equal(t, 1, rows[0].Refunded)
		retFrac(t, rows[0].Retained["m6"], 1, "m6")
	})

	t.Run("unknown duration gives no coverage but still defines the cohort", func(t *testing.T) {
		row := paidOne(t, now, paidCoverage{At: first})
		assert.Equal(t, 1, row.Size)
		retFrac(t, row.Retained["m1"], 0, "m1")
	})

	t.Run("subscription credits use seconds", func(t *testing.T) {
		day := int64(24 * 3600)
		row := paidOne(t, now,
			paidCoverage{At: first, Seconds: 31 * day},                       // 到 02-15 12:00
			paidCoverage{At: retUTC(2026, 2, 15, 11), Seconds: 28 * day},     // 提前 1 小时续 → 到 03-15 12:00
			paidCoverage{At: retUTC(2026, 3, 15, 13), Seconds: 31 * day * 2}, // 晚 1 小时续 → 到 05-15 13:00
		)
		retFrac(t, row.Retained["m1"], 1, "m1")
		retFrac(t, row.Retained["m3"], 1, "m3 (04-15)")
		retFrac(t, row.Retained["m6"], 0, "m6")
	})

	t.Run("mixed cohort fractions, unsorted input", func(t *testing.T) {
		rows := computePaidCohorts(map[uint64][]paidCoverage{
			1: {month(first, 12)},
			2: {month(retUTC(2026, 1, 20, 0), 1)},
			3: {month(retUTC(2026, 2, 20, 0), 3), month(retUTC(2026, 1, 3, 0), 3)}, // 乱序：首付是 01-03
			4: {{At: retUTC(2026, 1, 9, 0), Months: 12, Refunded: true}},
		}, map[uint64]bool{4: true}, now)
		require.Len(t, rows, 1)
		assert.Equal(t, 4, rows[0].Size)
		assert.Equal(t, 1, rows[0].Refunded)
		retFrac(t, rows[0].Retained["m1"], 0.5, "m1: users 1,3")
		retFrac(t, rows[0].Retained["m3"], 0.5, "m3: users 1,3 (3 covered to 07-03)")
		retFrac(t, rows[0].Retained["m6"], 0.25, "m6: user 1")
		retFrac(t, rows[0].Retained["m12"], 0, "m12")
	})

	t.Run("future checkpoint is null", func(t *testing.T) {
		row := paidOne(t, retUTC(2026, 10, 1, 0), month(first, 12))
		retFrac(t, row.Retained["m6"], 1, "m6")
		m12, has := row.Retained["m12"]
		assert.True(t, has, "m12 key must be present")
		assert.Nil(t, m12)
	})

	t.Run("a checkpoint is null until every member of the cohort has reached it", func(t *testing.T) {
		// now = 09-20：08-25 首付的 m1 检查点（09-25）还没到 → 整组 m1 为 null。
		rows := computePaidCohorts(map[uint64][]paidCoverage{
			1: {month(retUTC(2026, 8, 10, 0), 12)}, 2: {month(retUTC(2026, 8, 25, 0), 12)},
		}, nil, retUTC(2026, 9, 20, 0))
		require.Len(t, rows, 1)
		assert.Nil(t, rows[0].Retained["m1"])
	})

	t.Run("checkpoints clamp to month end", func(t *testing.T) {
		// 01-31 首付 1 个月 → 覆盖到 02-28；m1 检查点 = 02-28 + 7 天 = 03-07（不是 03-03 + 7 天 = 03-10）。
		// 02-27 续 1 个月 → 叠到 03-28，m1 留存。
		jan31 := retUTC(2026, 1, 31, 0)
		retFrac(t, paidOne(t, now, month(jan31, 1)).Retained["m1"], 0, "m1")
		retFrac(t, paidOne(t, now, month(jan31, 1), month(retUTC(2026, 2, 27, 0), 1)).Retained["m1"], 1, "m1 renewed")
		// 若检查点溢出到 03-10，下面这个 03-08 到期的用户会被误判为不留存。
		row := paidOne(t, now, paidCoverage{At: jan31, Seconds: 36 * 24 * 3600}) // 到 03-08
		retFrac(t, row.Retained["m1"], 1, "m1 at 03-07")
	})

	t.Run("cohort by UTC month of the first payment, ascending", func(t *testing.T) {
		rows := computePaidCohorts(map[uint64][]paidCoverage{
			1: {month(retUTC(2026, 3, 2, 0), 1)},
			2: {month(retUTC(2025, 12, 31, 23), 1)},
			// 2026-01-31 23:30 UTC-2 == 2026-02-01 01:30 UTC → 二月群组
			3: {month(time.Date(2026, 1, 31, 23, 30, 0, 0, time.FixedZone("x", -2*3600)), 1)},
			4: {},
		}, nil, now)
		require.Len(t, rows, 3)
		assert.Equal(t, []string{"2025-12", "2026-02", "2026-03"}, []string{rows[0].Cohort, rows[1].Cohort, rows[2].Cohort})
	})

	t.Run("empty input marshals rows as []", func(t *testing.T) {
		b, err := json.Marshal(computePaidCohorts(nil, nil, now))
		require.NoError(t, err)
		assert.Equal(t, "[]", string(b))
	})

	t.Run("json shape", func(t *testing.T) {
		b, err := json.Marshal(paidOne(t, retUTC(2026, 10, 1, 0), month(first, 12)))
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

// 付费群组的装载：成员 = 首付落在区间内且符合品牌的用户；每人带上截至 now 的完整付款历史
// （含区间之后的续费），订单时长取套餐快照的月数，入账行取 credited_seconds，退款订单带标记。
func TestLoadPaidCohortInputs(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	from, to := factsWindow(t)
	now := to.AddDate(1, 0, 0)
	withMonths := func(n int) factsOrderOpt {
		return func(o *Order) { o.Meta = fmt.Sprintf(`{"plan":{"pid":"facts-plan","month":%d}}`, n) }
	}

	in := factsUser(t, BrandKaitu, from.Add(-time.Hour))
	factsOrder(t, in.ID, OrderChannelNextpay, factsPaid(from.Add(time.Hour)), factsRefunded(from.Add(2*time.Hour)), withMonths(12))
	factsOrder(t, in.ID, OrderChannelNextpay, factsPaid(to.Add(240*time.Hour)), withMonths(3))  // 区间之后的续费
	factsOrder(t, in.ID, OrderChannelNextpay, factsPaid(now.Add(time.Hour)), withMonths(1))     // now 之后：不装载
	factsOrder(t, in.ID, OrderChannelNextpay, withMonths(1))                                    // 未付：不是付款
	factsOrder(t, in.ID, OrderChannelAppleIAP, factsPaid(from.Add(3*time.Hour)), withMonths(1)) // 由入账行计，订单侧排除
	factsCredit(t, in.ID, SubscriptionProviderApple, "renewal", "", from.Add(5*time.Hour))      // CreditedSeconds = 3600
	factsCredit(t, in.ID, SubscriptionProviderApple, "grace", "", from.Add(6*time.Hour))        // 不是付款

	early := factsUser(t, BrandKaitu, from.Add(-48*time.Hour)) // 首付在区间之前 → 不属于这些群组
	factsOrder(t, early.ID, OrderChannelNextpay, factsPaid(from.Add(-time.Hour)), withMonths(1))
	factsOrder(t, early.ID, OrderChannelNextpay, factsPaid(from.Add(time.Hour)), withMonths(1))

	other := factsUser(t, BrandOverleap, from.Add(-time.Hour))
	factsCredit(t, other.ID, SubscriptionProviderStripe, "purchase", "", from.Add(3*time.Hour))
	noSnapshot := factsUser(t, BrandOverleap, from.Add(-time.Hour))
	factsOrder(t, noSnapshot.ID, OrderChannelNextpay, factsPaid(from.Add(time.Hour))) // 快照里没有月数

	// 应用商店订阅退款：退款订单的 apple_transaction_id == 入账行的 transaction_id → 那笔入账不提供覆盖。
	apple := factsUser(t, BrandKaitu, from.Add(-time.Hour))
	refundedCredit := factsCredit(t, apple.ID, SubscriptionProviderApple, "purchase", "", from.Add(time.Hour))
	factsCredit(t, apple.ID, SubscriptionProviderApple, "renewal", "", from.Add(2*time.Hour)) // 没退的那笔不受影响
	factsOrder(t, apple.ID, OrderChannelAppleIAP, factsPaid(from.Add(time.Hour)), factsRefunded(from.Add(90*time.Minute)),
		func(o *Order) { o.AppleTransactionID = refundedCredit.TransactionID })
	// 同一个交易号挂在别的渠道的退款订单上不算（只认 apple_iap 订单）。
	stripeUser := factsUser(t, BrandKaitu, from.Add(-time.Hour))
	stripeCredit := factsCredit(t, stripeUser.ID, SubscriptionProviderStripe, "purchase", "", from.Add(time.Hour))
	factsOrder(t, stripeUser.ID, OrderChannelAppleIAP, factsPaid(from.Add(time.Hour)), factsRefunded(from.Add(90*time.Minute)),
		func(o *Order) { o.AppleTransactionID = stripeCredit.TransactionID })

	payments, refunded, err := loadPaidCohortInputs(ctx, BrandKaitu, false, from, to, now)
	require.NoError(t, err)
	got := payments[in.ID]
	require.Len(t, got, 3, "%+v", got)
	assert.True(t, got[0].At.Equal(from.Add(time.Hour)))
	assert.Equal(t, 12, got[0].Months)
	assert.True(t, got[0].Refunded)
	assert.True(t, got[1].At.Equal(to.Add(240*time.Hour)))
	assert.Equal(t, 3, got[1].Months)
	assert.False(t, got[1].Refunded)
	assert.True(t, got[2].At.Equal(from.Add(5*time.Hour)))
	assert.Equal(t, 0, got[2].Months)
	assert.Equal(t, int64(3600), got[2].Seconds)
	assert.True(t, refunded[in.ID])
	assert.NotContains(t, payments, early.ID)

	require.Len(t, payments[apple.ID], 2, "%+v", payments[apple.ID])
	assert.Equal(t, paidCoverage{At: payments[apple.ID][0].At, Seconds: 3600, Refunded: true}, payments[apple.ID][0])
	assert.Equal(t, paidCoverage{At: payments[apple.ID][1].At, Seconds: 3600}, payments[apple.ID][1])
	assert.True(t, refunded[apple.ID])
	// 纯函数接上：退款那笔不给覆盖，用户计入 refunded。
	rows := computePaidCohorts(map[uint64][]paidCoverage{apple.ID: payments[apple.ID][:1]}, refunded, now.AddDate(5, 0, 0))
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Refunded)
	retFrac(t, rows[0].Retained["m1"], 0, "m1")
	require.Len(t, payments[stripeUser.ID], 1)
	assert.False(t, payments[stripeUser.ID][0].Refunded, "only apple credits are matched against apple orders")

	require.Len(t, payments[other.ID], 1)
	assert.Equal(t, int64(3600), payments[other.ID][0].Seconds)
	assert.False(t, refunded[other.ID])
	require.Len(t, payments[noSnapshot.ID], 1)
	assert.Equal(t, paidCoverage{At: payments[noSnapshot.ID][0].At}, payments[noSnapshot.ID][0], "no duration, not refunded")

	payments, _, err = loadPaidCohortInputs(ctx, BrandOverleap, true, from, to, now)
	require.NoError(t, err)
	assert.NotContains(t, payments, in.ID)
	assert.Contains(t, payments, other.ID)
}

// 活跃群组的装载：首次出现早于区间的设备整个排除；带品牌过滤时无品牌的历史行不计入。
func TestLoadActiveOpens(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	m := generateId("ret-open")
	hNew, hOld, hLegacy, hExisting, hCross := m+"-new", m+"-old", m+"-legacy", m+"-existing", m+"-cross"
	t.Cleanup(func() {
		factsCleanup(t, db.Get().Where("device_hash IN ?", []string{hNew, hOld, hLegacy, hExisting, hCross}).Delete(&StatAppOpen{}).Error)
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
	// 存量设备：brand 列上线前就有（无品牌）记录，之后才出现带品牌的行。
	open(hExisting, "", since.Add(-48*time.Hour))
	open(hExisting, "overleap", since.Add(48*time.Hour))
	// 区间之前只在另一个品牌出现过：对 overleap 来说是新设备。
	open(hCross, "kaitu", since.Add(-48*time.Hour))
	open(hCross, "overleap", since.Add(48*time.Hour))

	opens, err := loadActiveOpens(ctx, BrandKaitu, false, since)
	require.NoError(t, err)
	assert.Len(t, opens[hNew], 2)
	assert.NotContains(t, opens, hOld, "first seen before the window")
	assert.Len(t, opens[hLegacy], 1)
	assert.NotContains(t, opens, hExisting)
	assert.NotContains(t, opens, hCross, "without a brand filter any earlier row excludes")

	opens, err = loadActiveOpens(ctx, BrandOverleap, true, since)
	require.NoError(t, err)
	assert.Len(t, opens[hNew], 2)
	assert.NotContains(t, opens, hLegacy, "rows without a brand are excluded under a brand filter")
	assert.NotContains(t, opens, hExisting, "a brandless row before the window marks the device as existing")
	assert.Len(t, opens[hCross], 1, "another brand's earlier row does not make the device existing for this brand")

	opens, err = loadActiveOpens(ctx, BrandKaitu, true, since)
	require.NoError(t, err)
	assert.NotContains(t, opens, hNew)
}
