package center

import (
	"context"
	"sort"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// 留存群组。两个 compute* 是纯函数（不碰数据库、不读时钟）；装载在 load* 里。
//   - 付费留存：群组 = 首付月份（UTC），看首付后第 N 个月权益是否仍有效。
//   - 活跃留存：群组 = 设备首次出现日（UTC），看第 N 天是否有打开。

const (
	paidCohortMonthForm    = "2006-01"
	activeCohortDays       = 60 // 活跃留存回看的首次出现日天数
	retentionMonthsMin     = 1
	retentionMonthsMax     = 24
	retentionMonthsDefault = 12
)

// paidCohortCheckpoints 的顺序即输出键的固定集合。
var paidCohortCheckpoints = []struct {
	Key    string
	Months int
}{{"m1", 1}, {"m3", 3}, {"m6", 6}, {"m12", 12}}

type PaidCohortRow struct {
	Cohort   string              `json:"cohort"` // "2026-09"
	Size     int                 `json:"size"`
	Retained map[string]*float64 `json:"retained"` // 键 m1/m3/m6/m12；检查点在未来 → nil
	Refunded int                 `json:"refunded"`
}

type ActiveCohortRow struct {
	Cohort string   `json:"cohort"` // "2026-09-30"
	Size   int      `json:"size"`
	D1     *float64 `json:"d1"`
	D7     *float64 `json:"d7"`
	D30    *float64 `json:"d30"`
}

// computePaidCohorts：群组 = 首付月份（UTC），按 cohort 升序。
// 检查点 = 该用户首付时间 + N 个月；留存 = 检查点 <= now 且 expiredAt >= 检查点 且未退款。
// 分母恒为群组人数（含退款用户）。群组里只要还有人的检查点没到，该检查点就是 nil——
// 不拿半组人算一个偏低的比例。
func computePaidCohorts(payFirst map[uint64]time.Time, expiredAt map[uint64]int64, refundedUsers map[uint64]bool, now time.Time) []PaidCohortRow {
	type acc struct {
		size, refunded int
		retained       []int
		pending        []bool
	}
	cohorts := make(map[string]*acc)
	for uid, first := range payFirst {
		first = first.UTC()
		key := first.Format(paidCohortMonthForm)
		a := cohorts[key]
		if a == nil {
			a = &acc{retained: make([]int, len(paidCohortCheckpoints)), pending: make([]bool, len(paidCohortCheckpoints))}
			cohorts[key] = a
		}
		a.size++
		refunded := refundedUsers[uid]
		if refunded {
			a.refunded++
		}
		exp, hasExp := expiredAt[uid]
		for i, cp := range paidCohortCheckpoints {
			at := first.AddDate(0, cp.Months, 0)
			if at.After(now) {
				a.pending[i] = true
				continue
			}
			if !refunded && hasExp && exp >= at.Unix() {
				a.retained[i]++
			}
		}
	}

	rows := make([]PaidCohortRow, 0, len(cohorts))
	for key, a := range cohorts {
		row := PaidCohortRow{Cohort: key, Size: a.size, Refunded: a.refunded, Retained: make(map[string]*float64, len(paidCohortCheckpoints))}
		for i, cp := range paidCohortCheckpoints {
			if a.pending[i] {
				row.Retained[cp.Key] = nil
				continue
			}
			f := funnelRate(a.retained[i], a.size)
			row.Retained[cp.Key] = &f
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Cohort < rows[j].Cohort })
	return rows
}

// computeActiveCohorts：群组 = 设备首次出现日（UTC），按 cohort 升序。
// Dn = 首日 + n 天当天有打开的设备占比；该日（对群组里所有设备是同一天）尚未结束 → nil。
func computeActiveCohorts(opens map[string][]time.Time, now time.Time) []ActiveCohortRow {
	offsets := [3]int{1, 7, 30}
	type acc struct {
		day  time.Time
		size int
		hits [3]int
	}
	cohorts := make(map[string]*acc)
	for _, times := range opens {
		if len(times) == 0 {
			continue
		}
		days := make(map[time.Time]bool, len(times))
		first := funnelUTCDay(times[0])
		for _, at := range times {
			d := funnelUTCDay(at)
			days[d] = true
			if d.Before(first) {
				first = d
			}
		}
		key := first.Format(funnelDailyDateForm)
		a := cohorts[key]
		if a == nil {
			a = &acc{day: first}
			cohorts[key] = a
		}
		a.size++
		for i, n := range offsets {
			if days[first.AddDate(0, 0, n)] {
				a.hits[i]++
			}
		}
	}

	rows := make([]ActiveCohortRow, 0, len(cohorts))
	for key, a := range cohorts {
		row := ActiveCohortRow{Cohort: key, Size: a.size}
		out := [3]**float64{&row.D1, &row.D7, &row.D30}
		for i, n := range offsets {
			if a.day.AddDate(0, 0, n+1).After(now) { // 目标日的结束时刻还没到
				continue
			}
			f := funnelRate(a.hits[i], a.size)
			*out[i] = &f
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Cohort < rows[j].Cohort })
	return rows
}

// paidCohortRange 返回最近 months 个首付月（含当月）的区间 [from, to)，UTC 月初对齐。
func paidCohortRange(now time.Time, months int) (from, to time.Time) {
	y, m, _ := now.UTC().Date()
	thisMonth := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	return thisMonth.AddDate(0, -(months - 1), 0), thisMonth.AddDate(0, 1, 0)
}

// loadPaidCohortInputs 装载 computePaidCohorts 的三个输入：只收**全时段首付**落在 [from, to) 内、
// 且符合品牌过滤的用户。退款目前只来自订单（is_refunded）。
func loadPaidCohortInputs(ctx context.Context, brand Brand, hasBrand bool, from, to time.Time) (payFirst map[uint64]time.Time, expiredAt map[uint64]int64, refundedUsers map[uint64]bool, err error) {
	pays, err := loadPayments(ctx, brand, hasBrand, from, to)
	if err != nil {
		return nil, nil, nil, err
	}
	seen := make(map[uint64]bool, len(pays))
	var candidates []uint64
	for _, p := range pays {
		if !seen[p.UserID] {
			seen[p.UserID] = true
			candidates = append(candidates, p.UserID)
		}
	}
	first, err := firstPaymentAt(ctx, candidates)
	if err != nil {
		return nil, nil, nil, err
	}
	payFirst = make(map[uint64]time.Time)
	var userIDs []uint64
	for _, uid := range candidates {
		if at, ok := first[uid]; ok && !at.Before(from) && at.Before(to) {
			payFirst[uid] = at
			userIDs = append(userIDs, uid)
		}
	}

	expiredAt = make(map[uint64]int64, len(userIDs))
	refundedUsers = make(map[uint64]bool)
	for start := 0; start < len(userIDs); start += funnelLoadBatch {
		batch := userIDs[start:min(start+funnelLoadBatch, len(userIDs))]
		var users []User
		if err := db.Get().WithContext(ctx).Unscoped().Model(&User{}).
			Select("id", "expired_at").Where("id IN ?", batch).Find(&users).Error; err != nil {
			return nil, nil, nil, err
		}
		for i := range users {
			expiredAt[users[i].ID] = users[i].ExpiredAt
		}
		var refunded []uint64
		if err := db.Get().WithContext(ctx).Model(&Order{}).
			Where("is_refunded = ? AND user_id IN ?", true, batch).
			Distinct().Pluck("user_id", &refunded).Error; err != nil {
			return nil, nil, nil, err
		}
		for _, uid := range refunded {
			refundedUsers[uid] = true
		}
	}
	return payFirst, expiredAt, refundedUsers, nil
}

type activeOpenRow struct {
	DeviceHash string
	At         time.Time
}

// loadActiveOpens 返回首次出现时间 >= since 的设备 → 它的全部打开时间（reported_at，服务端权威）。
// 带品牌过滤时只看该品牌的行：brand 列上线前的历史行品牌为空，不计入。
// 表本身由保留期任务限制在 120 天内，所以"首次出现"取的是保留期内最早的一行。
func loadActiveOpens(ctx context.Context, brand Brand, hasBrand bool, since time.Time) (map[string][]time.Time, error) {
	scope := func() *gorm.DB {
		q := db.Get().WithContext(ctx).Model(&StatAppOpen{})
		if hasBrand {
			q = q.Scopes(ScopeBrand(brand))
		}
		return q
	}
	// 区间之前就出现过的设备不属于这些群组。
	var old []string
	if err := scope().Where("reported_at < ?", since).Distinct().Pluck("device_hash", &old).Error; err != nil {
		return nil, err
	}
	exclude := make(map[string]bool, len(old))
	for _, h := range old {
		exclude[h] = true
	}
	var rows []activeOpenRow
	if err := scope().Select("device_hash, reported_at as at").
		Where("reported_at >= ?", since).Scan(&rows).Error; err != nil {
		return nil, err
	}
	opens := make(map[string][]time.Time)
	for i := range rows {
		if exclude[rows[i].DeviceHash] {
			continue
		}
		opens[rows[i].DeviceHash] = append(opens[rows[i].DeviceHash], rows[i].At)
	}
	return opens, nil
}
