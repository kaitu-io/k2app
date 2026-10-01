package center

import (
	"context"
	"slices"
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

// paidRetentionGrace：检查点 = 首付满 N 个月 + 这段宽限。订阅在周期结束那一刻续费，入账行要等
// webhook 处理完才写（晚几秒到几小时）；不留宽限的话，按时续费的人在整月检查点上都会读成流失。
const paidRetentionGrace = 7 * 24 * time.Hour

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

// paidCoverage 是一笔付款带来的付费覆盖：Months > 0 用日历月（订单的套餐快照），否则用 Seconds
// （订阅入账行的 credited_seconds）。两者都为 0（时长未知）或订单已退款 → 不提供覆盖。
type paidCoverage struct {
	At       time.Time
	Months   int
	Seconds  int64
	Refunded bool
}

// addMonthsClamped 加 n 个日历月，日子钳到目标月的最后一天（1 月 31 日 + 1 个月 = 2 月 28/29 日，
// 不会像 time.AddDate 那样溢出到 3 月）。按 UTC 计算，时分秒保留。
func addMonthsClamped(t time.Time, n int) time.Time {
	t = t.UTC()
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	if last := first.AddDate(0, 1, -1).Day(); d > last {
		d = last
	}
	h, mi, sec := t.Clock()
	return time.Date(first.Year(), first.Month(), d, h, mi, sec, t.Nanosecond(), time.UTC)
}

// paidCoveredUntil 按时间顺序走完 At <= checkpoint 的付款，返回覆盖到期时刻（零值 = 从未被覆盖）。
// 每笔付款从 max(上一段到期, 付款时刻) 起算：提前续费是往后叠，断档后回来是从回来那刻重新起算。
// pays 必须已按 At 升序。
func paidCoveredUntil(pays []paidCoverage, checkpoint time.Time) time.Time {
	var until time.Time
	for _, p := range pays {
		if p.At.After(checkpoint) {
			break
		}
		if p.Refunded || (p.Months <= 0 && p.Seconds <= 0) {
			continue
		}
		start := p.At
		if until.After(start) {
			start = until
		}
		if p.Months > 0 {
			until = addMonthsClamped(start, p.Months)
		} else {
			until = start.Add(time.Duration(p.Seconds) * time.Second)
		}
	}
	return until
}

// computePaidCohorts：群组 = 首笔付款的月份（UTC），按 cohort 升序。
// 留存按**付款记录**推算，不看 users.expired_at（那个字段还会被赠送 / 试用 / 邀请奖励 / 人工发放推动，
// 而且只反映当下）：检查点 = 该用户首付时间 + N 个月（钳到月末）+ paidRetentionGrace；
// 留存 ⇔ 走完检查点之前的付款后，覆盖到期时刻**严格晚于**检查点。
// 分母恒为群组人数。群组里只要还有人的检查点没到，该检查点就是 nil——不拿半组人算一个偏低的比例。
// refundedUsers 只用于 Refunded 计数（信息性）；退款订单不留存是因为它不提供覆盖。
func computePaidCohorts(payments map[uint64][]paidCoverage, refundedUsers map[uint64]bool, now time.Time) []PaidCohortRow {
	type acc struct {
		size, refunded int
		retained       []int
		pending        []bool
	}
	cohorts := make(map[string]*acc)
	for uid, input := range payments {
		if len(input) == 0 {
			continue
		}
		pays := slices.Clone(input)
		sort.SliceStable(pays, func(i, j int) bool { return pays[i].At.Before(pays[j].At) })
		first := pays[0].At.UTC()
		key := first.Format(paidCohortMonthForm)
		a := cohorts[key]
		if a == nil {
			a = &acc{retained: make([]int, len(paidCohortCheckpoints)), pending: make([]bool, len(paidCohortCheckpoints))}
			cohorts[key] = a
		}
		a.size++
		if refundedUsers[uid] {
			a.refunded++
		}
		for i, cp := range paidCohortCheckpoints {
			at := addMonthsClamped(first, cp.Months).Add(paidRetentionGrace)
			if at.After(now) {
				a.pending[i] = true
				continue
			}
			if paidCoveredUntil(pays, at).After(at) {
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

// funnelOrderMonths 取订单套餐快照里的月数；快照缺失 / 解析失败 → 0（时长未知，不提供覆盖）。
func funnelOrderMonths(o *Order) int {
	plan, err := o.GetPlan()
	if err != nil || plan == nil {
		return 0
	}
	return plan.Month
}

// loadPaidCohortInputs 装载 computePaidCohorts 的输入：群组成员 = **全时段首付**落在 [from, to) 内、
// 且符合品牌过滤的用户；每个成员带上截至 now 的**完整**付款历史（不只是群组月份里的那几笔）。
// 付款口径与漏斗事实一致（funnelPaidOrderScope ∪ funnelPaymentCreditScope）。
func loadPaidCohortInputs(ctx context.Context, brand Brand, hasBrand bool, from, to, now time.Time) (payments map[uint64][]paidCoverage, refundedUsers map[uint64]bool, err error) {
	pays, err := loadPayments(ctx, brand, hasBrand, from, to)
	if err != nil {
		return nil, nil, err
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
		return nil, nil, err
	}
	var userIDs []uint64
	for _, uid := range candidates {
		if at, ok := first[uid]; ok && !at.Before(from) && at.Before(to) {
			userIDs = append(userIDs, uid)
		}
	}

	payments = make(map[uint64][]paidCoverage, len(userIDs))
	refundedUsers = make(map[uint64]bool)
	for start := 0; start < len(userIDs); start += funnelLoadBatch {
		batch := userIDs[start:min(start+funnelLoadBatch, len(userIDs))]

		var orders []Order
		if err := db.Get().WithContext(ctx).Model(&Order{}).
			Select("id", "user_id", "paid_at", "is_refunded", "meta").
			Scopes(funnelPaidOrderScope).
			Where("user_id IN ? AND paid_at <= ?", batch, now).
			Order("paid_at, id").Find(&orders).Error; err != nil {
			return nil, nil, err
		}
		for i := range orders {
			o := &orders[i]
			if o.PaidAt == nil {
				continue
			}
			payments[o.UserID] = append(payments[o.UserID], paidCoverage{
				At: *o.PaidAt, Months: funnelOrderMonths(o), Refunded: o.IsRefunded != nil && *o.IsRefunded,
			})
		}

		var credits []SubscriptionCredit
		if err := db.Get().WithContext(ctx).Model(&SubscriptionCredit{}).
			Select("id", "user_id", "created_at", "credited_seconds", "provider", "transaction_id").
			Scopes(funnelPaymentCreditScope).
			Where("user_id IN ? AND created_at <= ?", batch, now).
			Order("created_at, id").Find(&credits).Error; err != nil {
			return nil, nil, err
		}
		// 应用商店订阅的退款：入账行本身没有退款标记，但同一笔交易的订单有——
		// 订单的 apple_transaction_id 与入账行的 transaction_id 都是该交易的 transactionId
		// （createAppleIAPOrderInTx / creditAppleTransaction），退款时订单被标 is_refunded
		// （revokeIAPOrderCashbackInTx）。没有建单的交易（沙盒、建单功能上线前）对不上，照常计覆盖。
		var refundedTxns []string
		if err := db.Get().WithContext(ctx).Model(&Order{}).
			Where("is_refunded = ? AND channel = ? AND user_id IN ?", true, OrderChannelAppleIAP, batch).
			Where("apple_transaction_id <> ?", "").
			Pluck("apple_transaction_id", &refundedTxns).Error; err != nil {
			return nil, nil, err
		}
		refundedTxn := make(map[string]bool, len(refundedTxns))
		for _, id := range refundedTxns {
			refundedTxn[id] = true
		}
		for i := range credits {
			c := &credits[i]
			payments[c.UserID] = append(payments[c.UserID], paidCoverage{
				At: c.CreatedAt, Seconds: c.CreditedSeconds,
				Refunded: c.Provider == SubscriptionProviderApple && refundedTxn[c.TransactionID],
			})
		}

		// 任何一张退款订单都算（含不计入付款口径的渠道）：这一列只是信息性的。
		var refunded []uint64
		if err := db.Get().WithContext(ctx).Model(&Order{}).
			Where("is_refunded = ? AND user_id IN ?", true, batch).
			Distinct().Pluck("user_id", &refunded).Error; err != nil {
			return nil, nil, err
		}
		for _, uid := range refunded {
			refundedUsers[uid] = true
		}
	}
	return payments, refundedUsers, nil
}

type activeOpenRow struct {
	DeviceHash string
	At         time.Time
}

// loadActiveOpens 返回首次出现时间 >= since 的设备 → 它的全部打开时间（reported_at，服务端权威）。
// 带品牌过滤时打开记录只看该品牌的行（brand 列上线前的历史行品牌为空，不归属任何品牌）；
// 但"是否老设备"的判断把无品牌的历史行也算上。
// 表本身由保留期任务限制在 120 天内，所以"首次出现"取的是保留期内最早的一行。
func loadActiveOpens(ctx context.Context, brand Brand, hasBrand bool, since time.Time) (map[string][]time.Time, error) {
	scope := func() *gorm.DB {
		q := db.Get().WithContext(ctx).Model(&StatAppOpen{})
		if hasBrand {
			q = q.Scopes(ScopeBrand(brand))
		}
		return q
	}
	inRange := func() *gorm.DB { return scope().Where("reported_at >= ?", since) }

	// 规模闸门：区间内的打开记录数超过上限就不装载。
	var n int64
	if err := inRange().Count(&n).Error; err != nil {
		return nil, err
	}
	if n > funnelActiveMaxOpens {
		return nil, errFunnelRangeTooLarge
	}

	// 区间之前就出现过的设备不属于这些群组。带品牌过滤时，无品牌的历史行（brand 列上线前）也算
	// "出现过"——否则上线那天整个存量装机都会被当成新设备。
	// 只问「区间内出现的设备里哪些更早就有记录」：结果集以区间内的设备数为界（已被上面的闸门限住），
	// 而不是保留期内全部老设备。
	seenBefore := db.Get().WithContext(ctx).Model(&StatAppOpen{}).
		Where("reported_at < ?", since).
		Where("device_hash IN (?)", inRange().Select("device_hash"))
	if hasBrand {
		seenBefore = seenBefore.Where("brand IN ?", []string{string(brand), ""})
	}
	var old []string
	if err := seenBefore.Distinct().Pluck("device_hash", &old).Error; err != nil {
		return nil, err
	}
	exclude := make(map[string]bool, len(old))
	for _, h := range old {
		exclude[h] = true
	}

	// 流式装载；闸门之后新写入的行也守住上限。
	q := inRange().Select("device_hash, reported_at as at")
	rows, err := q.Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	opens := make(map[string][]time.Time)
	var loaded int64
	for rows.Next() {
		if loaded++; loaded > funnelActiveMaxOpens {
			return nil, errFunnelRangeTooLarge
		}
		var row activeOpenRow
		if err := q.ScanRows(rows, &row); err != nil {
			return nil, err
		}
		if exclude[row.DeviceHash] {
			continue
		}
		opens[row.DeviceHash] = append(opens[row.DeviceHash], row.At)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return opens, nil
}
