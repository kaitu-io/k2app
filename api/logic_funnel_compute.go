package center

import (
	"slices"
	"sort"
	"strconv"
	"time"
)

// 漏斗的纯函数计算：输入一批记录（行为事件 + 事实）与匿名身份 → 用户的映射，输出各步人数。
// 不碰数据库、不读时钟；结果不依赖 map 迭代顺序。

// funnelGroupDims 是 computeFunnel 接受的 groupBy 取值；其它值一律按不分组处理。
var funnelGroupDims = []string{"source", "utm_campaign", "country", "os", "device", "app_version", "paywall_source", "plan", "channel"}

const (
	funnelMaxGroups     = 50
	funnelGroupDirect   = "direct"
	funnelGroupUnknown  = "unknown"
	funnelGroupOther    = "(other)"
	funnelDailyDateForm = "2006-01-02"
)

type FunnelStepResult struct {
	Label             string  `json:"label"`
	Count             int     `json:"count"`
	RateFromPrev      float64 `json:"rateFromPrev"`      // 第 1 步恒为 1；上一步人数为 0 时为 0
	RateFromFirst     float64 `json:"rateFromFirst"`     // 第 1 步人数为 0 时全部为 0（含第 1 步）
	MedianSecFromPrev *int64  `json:"medianSecFromPrev"` // 第 1 步与 Count==0 时 nil
}

type FunnelDay struct {
	Date      string `json:"date"` // YYYY-MM-DD（UTC）
	Entered   int    `json:"entered"`
	Completed int    `json:"completed"`
}

type FunnelGroup struct {
	Key   string `json:"key"`
	Steps []int  `json:"steps"`
}

type FunnelResult struct {
	Steps  []FunnelStepResult `json:"steps"`
	Daily  []FunnelDay        `json:"daily"`  // 按进入日（UTC），区间内每天都有一项
	Groups []FunnelGroup      `json:"groups"` // 不分组时为空数组；按第 1 步人数降序、key 升序取前 50 组，其余并进末尾的 "(other)"
}

// matches 判断一条记录是否满足该步。带 Surface 过滤的步骤不匹配事实（事实的 Surface 为 ""）。
func (s funnelStep) matches(r *funnelRecord) bool {
	if s.Surface != "" && r.Surface != s.Surface {
		return false
	}
	if s.UtmSource != "" && r.UtmSource != s.UtmSource {
		return false
	}
	return slices.Contains(s.Events, r.Event)
}

// funnelPersonKey 把记录归到"人"：已登录 → 用户；匿名身份已关联 → 用户；否则匿名身份本身。
// 两样都没有（GPC 下的匿名事件）返回 ok=false，该记录不参与计算。
func funnelPersonKey(r *funnelRecord, identities map[string]uint64) (string, bool) {
	if r.UserID != 0 {
		return "u:" + strconv.FormatUint(r.UserID, 10), true
	}
	if r.AnonID == "" {
		return "", false
	}
	anon := r.AnonKind + ":" + r.AnonID
	if uid, ok := identities[anon]; ok {
		return "u:" + strconv.FormatUint(uid, 10), true
	}
	return anon, true
}

func funnelOrUnknown(s string) string {
	if s == "" {
		return funnelGroupUnknown
	}
	return s
}

// funnelGroupKey 取一个人的分组键。entry = 进入记录，last = 该人到达的最后一步的记录。
func funnelGroupKey(groupBy string, entry, last *funnelRecord) string {
	switch groupBy {
	case "source":
		if entry.UtmSource != "" {
			return entry.UtmSource
		}
		if entry.RefHost != "" {
			return entry.RefHost
		}
		return funnelGroupDirect
	case "utm_campaign":
		return funnelOrUnknown(entry.UtmCampaign)
	case "country":
		return funnelOrUnknown(entry.Country)
	case "os":
		return funnelOrUnknown(entry.OS)
	case "device":
		return funnelOrUnknown(entry.Device)
	case "app_version":
		return funnelOrUnknown(entry.AppVersion)
	case "paywall_source":
		return funnelOrUnknown(entry.Source)
	case "plan":
		return funnelOrUnknown(last.Plan)
	case "channel":
		return funnelOrUnknown(last.Channel)
	}
	return funnelGroupUnknown
}

// funnelWalk 走一个人的漏斗。recs 已按 At 升序（稳定）。返回到达的每一步所用记录的下标
// （长度 = 到达的步数；0 = 没有进入）。
//
//   - 进入：最早一条满足第 1 步且 from <= At < to 的记录。
//   - 推进：第 i 步取最早一条满足该步、At >= 上一步 At、At <= 进入 At + Window 的记录；
//     同一条记录可以连续满足多步。
func funnelWalk(p funnelPath, recs []*funnelRecord, from, to time.Time) []int {
	if len(p.Steps) == 0 {
		return nil
	}
	entry := -1
	for i, r := range recs {
		if !r.At.Before(to) {
			break
		}
		if !r.At.Before(from) && p.Steps[0].matches(r) {
			entry = i
			break
		}
	}
	if entry < 0 {
		return nil
	}
	reached := make([]int, 1, len(p.Steps))
	reached[0] = entry
	deadline := recs[entry].At.Add(p.Window)
	prevAt := recs[entry].At
	lo := 0 // 第一条 At >= prevAt 的记录；prevAt 单调不减，所以 lo 只进不退
	for _, step := range p.Steps[1:] {
		for lo < len(recs) && recs[lo].At.Before(prevAt) {
			lo++
		}
		found := -1
		for i := lo; i < len(recs) && !recs[i].At.After(deadline); i++ {
			if step.matches(recs[i]) {
				found = i
				break
			}
		}
		if found < 0 {
			break
		}
		reached = append(reached, found)
		prevAt = recs[found].At
	}
	return reached
}

// funnelRate：分母为 0 时为 0（不产生 NaN / Inf）。
func funnelRate(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// funnelLowerMedian：偶数个取下中位。xs 会被就地排序。
func funnelLowerMedian(xs []int64) *int64 {
	if len(xs) == 0 {
		return nil
	}
	slices.Sort(xs)
	m := xs[(len(xs)-1)/2]
	return &m
}

func funnelUTCDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// computeFunnel 计算路径 p 在 [from, to) 内进入的人各走到了哪一步。
// recs 可以是任意顺序、不会被修改；identities 的键是 AnonKind+":"+AnonID。
// groupBy 不在 funnelGroupDims 里（含 ""）时不分组。
func computeFunnel(p funnelPath, recs []funnelRecord, identities map[string]uint64, from, to time.Time, groupBy string) FunnelResult {
	nSteps := len(p.Steps)
	if !slices.Contains(funnelGroupDims, groupBy) {
		groupBy = ""
	}

	// 1. 按人归集，人的顺序 = 输入里首次出现的顺序（只为确定性；计数本身与顺序无关）。
	byPerson := make(map[string][]*funnelRecord)
	var order []string
	for i := range recs {
		key, ok := funnelPersonKey(&recs[i], identities)
		if !ok {
			continue
		}
		if _, seen := byPerson[key]; !seen {
			order = append(order, key)
		}
		byPerson[key] = append(byPerson[key], &recs[i])
	}

	counts := make([]int, nSteps)
	gaps := make([][]int64, nSteps) // gaps[i] = 到达第 i 步的人从上一步到这一步的秒数
	entered := make(map[string]int)
	completed := make(map[string]int)
	groups := make(map[string][]int)

	for _, key := range order {
		mine := byPerson[key]
		// 2. 每人按时间升序；同一时刻保持输入顺序。
		sort.SliceStable(mine, func(i, j int) bool { return mine[i].At.Before(mine[j].At) })
		reached := funnelWalk(p, mine, from, to)
		if len(reached) == 0 {
			continue
		}
		for i, idx := range reached {
			counts[i]++
			if i > 0 {
				gaps[i] = append(gaps[i], int64(mine[idx].At.Sub(mine[reached[i-1]].At)/time.Second))
			}
		}
		entry := mine[reached[0]]
		day := entry.At.UTC().Format(funnelDailyDateForm)
		entered[day]++
		if len(reached) == nSteps {
			completed[day]++
		}
		if groupBy != "" {
			gk := funnelGroupKey(groupBy, entry, mine[reached[len(reached)-1]])
			g := groups[gk]
			if g == nil {
				g = make([]int, nSteps)
				groups[gk] = g
			}
			for i := range reached {
				g[i]++
			}
		}
	}

	res := FunnelResult{
		Steps:  make([]FunnelStepResult, nSteps),
		Daily:  []FunnelDay{},
		Groups: []FunnelGroup{},
	}
	for i, s := range p.Steps {
		sr := FunnelStepResult{Label: s.Label, Count: counts[i]}
		if i == 0 {
			sr.RateFromPrev = 1
		} else {
			sr.RateFromPrev = funnelRate(counts[i], counts[i-1])
			sr.MedianSecFromPrev = funnelLowerMedian(gaps[i])
		}
		sr.RateFromFirst = funnelRate(counts[i], counts[0])
		res.Steps[i] = sr
	}

	// 区间 [from, to) 触及的每个 UTC 日各一项（对齐到 UTC 零点时即 from <= d < to 的每一天）。
	if from.Before(to) {
		last := funnelUTCDay(to.Add(-time.Nanosecond))
		for d := funnelUTCDay(from); !d.After(last); d = d.AddDate(0, 0, 1) {
			date := d.Format(funnelDailyDateForm)
			res.Daily = append(res.Daily, FunnelDay{Date: date, Entered: entered[date], Completed: completed[date]})
		}
	}

	for key, steps := range groups {
		res.Groups = append(res.Groups, FunnelGroup{Key: key, Steps: steps})
	}
	sort.Slice(res.Groups, func(i, j int) bool {
		a, b := res.Groups[i], res.Groups[j]
		if a.Steps[0] != b.Steps[0] {
			return a.Steps[0] > b.Steps[0]
		}
		return a.Key < b.Key
	})
	// 第 51 名起并进最后一个 "(other)" 组，保证每一步各组相加 = 该步总数。
	if len(res.Groups) > funnelMaxGroups {
		other := FunnelGroup{Key: funnelGroupOther, Steps: make([]int, nSteps)}
		for _, g := range res.Groups[funnelMaxGroups:] {
			for i, n := range g.Steps {
				other.Steps[i] += n
			}
		}
		res.Groups = append(res.Groups[:funnelMaxGroups], other)
	}
	return res
}
