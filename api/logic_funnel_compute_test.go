package center

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 全部纯内存。t0 是区间起点（UTC 零点）。
var funnelT0 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

type fnRecOpt func(*funnelRecord)

func fnWithSurface(s string) fnRecOpt     { return func(r *funnelRecord) { r.Surface = s } }
func fnWithPlan(s string) fnRecOpt        { return func(r *funnelRecord) { r.Plan = s } }
func fnWithChannel(s string) fnRecOpt     { return func(r *funnelRecord) { r.Channel = s } }
func fnWithSource(s string) fnRecOpt      { return func(r *funnelRecord) { r.Source = s } }
func fnWithUtmSource(s string) fnRecOpt   { return func(r *funnelRecord) { r.UtmSource = s } }
func fnWithRefHost(s string) fnRecOpt     { return func(r *funnelRecord) { r.RefHost = s } }
func fnWithUtmCampaign(s string) fnRecOpt { return func(r *funnelRecord) { r.UtmCampaign = s } }
func fnWithCountry(s string) fnRecOpt     { return func(r *funnelRecord) { r.Country = s } }
func fnWithAt(at time.Time) fnRecOpt      { return func(r *funnelRecord) { r.At = at } }

// fnRec 构造一条记录。person："sid:A" / "did:D"（匿名）、"u:7"（已登录）、""（无任何身份）。
// 面的默认值：事实 = ""；单面事件 = 那个面；双面事件 = did → app，否则 web。
func fnRec(person, event string, offsetMin int, opts ...fnRecOpt) funnelRecord {
	r := funnelRecord{At: funnelT0.Add(time.Duration(offsetMin) * time.Minute), Event: event}
	kind, id, _ := strings.Cut(person, ":")
	switch kind {
	case "u":
		n, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			panic(err)
		}
		r.UserID = n
	case "sid", "did":
		r.AnonKind, r.AnonID = kind, id
	case "":
	default:
		panic("bad person " + person)
	}
	def, ok := funnelEventDefByName(event)
	if !ok {
		panic("unregistered event " + event)
	}
	switch {
	case def.Kind == FunnelKindFact:
	case len(def.Surfaces) == 1:
		r.Surface = def.Surfaces[0]
	case kind == "did":
		r.Surface = FunnelSurfaceApp
	default:
		r.Surface = FunnelSurfaceWeb
	}
	for _, o := range opts {
		o(&r)
	}
	return r
}

func fnMustPath(t *testing.T, key string) funnelPath {
	t.Helper()
	p, ok := funnelPathByKey(key)
	require.True(t, ok, key)
	return p
}

func fnStepCounts(res FunnelResult) []int {
	out := make([]int, len(res.Steps))
	for i, s := range res.Steps {
		out[i] = s.Count
	}
	return out
}

// 默认区间：[t0, t0+30d)。
func fnCompute30d(t *testing.T, key string, recs []funnelRecord, ids map[string]uint64, groupBy string) FunnelResult {
	t.Helper()
	return computeFunnel(fnMustPath(t, key), recs, ids, funnelT0, funnelT0.Add(30*24*time.Hour), groupBy)
}

const fnMinPerDay = 24 * 60

func TestCompute_AnonThenLoggedInIsOnePerson(t *testing.T) {
	recs := []funnelRecord{
		fnRec("sid:A", "pricing_view", 0),
		fnRec("sid:A", "checkout_start", 1),
		fnRec("u:7", "purchase", 3),
	}
	res := fnCompute30d(t, "web_purchase", recs, map[string]uint64{"sid:A": 7}, "")
	assert.Equal(t, []int{1, 1, 1, 1}, fnStepCounts(res))

	// 对照：没有身份关联时是两个人，匿名的止步于发起支付，登录的那个从没进入。
	res = fnCompute30d(t, "web_purchase", recs, nil, "")
	assert.Equal(t, []int{1, 1, 1, 0}, fnStepCounts(res))
}

func TestCompute_UserIDWinsOverIdentityMap(t *testing.T) {
	// 记录自带 UserID 时不看 identities（即使匿名身份映射到别人）。
	r := fnRec("sid:A", "pricing_view", 0)
	r.UserID = 9
	recs := []funnelRecord{r, fnRec("u:9", "checkout_start", 1, fnWithSurface("web")), fnRec("u:7", "checkout_start", 1, fnWithSurface("web"))}
	res := fnCompute30d(t, "web_purchase", recs, map[string]uint64{"sid:A": 7}, "")
	assert.Equal(t, []int{1, 1, 1, 0}, fnStepCounts(res))
}

func TestCompute_SidAndDidWithSameIDAreDifferentPeople(t *testing.T) {
	recs := []funnelRecord{
		fnRec("sid:X", "auth_code_sent", 0),
		fnRec("did:X", "auth_done", 1, fnWithSurface("web")),
	}
	res := fnCompute30d(t, "web_checkout_auth", recs, nil, "")
	assert.Equal(t, []int{1, 0, 0}, fnStepCounts(res))
}

func TestCompute_LandingOnPricingCountsBothSteps(t *testing.T) {
	res := fnCompute30d(t, "web_purchase", []funnelRecord{fnRec("sid:A", "pricing_view", 0)}, nil, "")
	assert.Equal(t, []int{1, 1, 0, 0}, fnStepCounts(res))
	require.NotNil(t, res.Steps[1].MedianSecFromPrev)
	assert.Equal(t, int64(0), *res.Steps[1].MedianSecFromPrev)
}

func TestCompute_OutOfOrderNotCounted(t *testing.T) {
	recs := []funnelRecord{
		fnRec("sid:A", "page_view", 0),
		fnRec("sid:A", "checkout_start", 1),
		fnRec("sid:A", "pricing_view", 2),
	}
	res := fnCompute30d(t, "web_purchase", recs, nil, "")
	assert.Equal(t, []int{1, 1, 0, 0}, fnStepCounts(res))
}

func TestCompute_OutsideWindowNotCounted(t *testing.T) {
	build := func(purchaseAt time.Time) []funnelRecord {
		return []funnelRecord{
			fnRec("u:7", "pricing_view", 0),
			fnRec("u:7", "checkout_start", 2),
			fnRec("u:7", "purchase", 0, fnWithAt(purchaseAt)),
		}
	}
	window := 14 * 24 * time.Hour
	cases := []struct {
		name string
		at   time.Time
		want int
	}{
		{"15 days after entry", funnelT0.Add(15 * 24 * time.Hour), 0},
		{"13 days after entry", funnelT0.Add(13 * 24 * time.Hour), 1},
		{"exactly at entry+window (inclusive)", funnelT0.Add(window), 1},
		{"one second past the window", funnelT0.Add(window + time.Second), 0},
	}
	for _, c := range cases {
		res := fnCompute30d(t, "web_purchase", build(c.at), nil, "")
		assert.Equal(t, []int{1, 1, 1, c.want}, fnStepCounts(res), c.name)
	}
}

// 窗口从进入时刻起算，不是从上一步起算。
func TestCompute_WindowAnchoredAtEntry(t *testing.T) {
	recs := []funnelRecord{
		fnRec("sid:A", "auth_code_sent", 0),
		fnRec("sid:A", "auth_done", 23*60),
		fnRec("sid:A", "checkout_start", 25*60), // 距上一步 2h，但距进入 25h > 24h
	}
	res := fnCompute30d(t, "web_checkout_auth", recs, nil, "")
	assert.Equal(t, []int{1, 1, 0}, fnStepCounts(res))
}

func TestCompute_EntryMustBeInRange(t *testing.T) {
	recs := []funnelRecord{
		fnRec("sid:A", "page_view", -10),
		fnRec("sid:A", "pricing_view", 5, fnWithSurface("app")), // 区间内，但不满足第 1 步的面过滤
	}
	res := fnCompute30d(t, "web_purchase", recs, nil, "")
	assert.Equal(t, []int{0, 0, 0, 0}, fnStepCounts(res))

	// to 是开区间：恰在 to 的记录不算进入；恰在 from 的算。
	to := funnelT0.Add(30 * 24 * time.Hour)
	res = fnCompute30d(t, "web_purchase", []funnelRecord{fnRec("sid:A", "page_view", 0, fnWithAt(to))}, nil, "")
	assert.Equal(t, 0, res.Steps[0].Count)
	res = fnCompute30d(t, "web_purchase", []funnelRecord{fnRec("sid:A", "page_view", 0)}, nil, "")
	assert.Equal(t, 1, res.Steps[0].Count)

	// 区间外有更早的第 1 步记录不妨碍区间内的那条成为进入。
	recs = []funnelRecord{fnRec("sid:A", "page_view", -10), fnRec("sid:A", "page_view", 10), fnRec("sid:A", "pricing_view", 20)}
	res = fnCompute30d(t, "web_purchase", recs, nil, "")
	assert.Equal(t, []int{1, 1, 0, 0}, fnStepCounts(res))
	assert.Equal(t, int64(600), *res.Steps[1].MedianSecFromPrev)
}

// 回访者：第 0 天只看了一眼，第 20 天回来走完全程 → 以第 20 天那次进入计（窗口 14 天从它起算）。
func TestCompute_ReturningVisitorConvertsFromLaterEntry(t *testing.T) {
	d20 := 20 * fnMinPerDay
	recs := []funnelRecord{
		fnRec("u:7", "page_view", 0, fnWithUtmSource("early")),
		fnRec("u:7", "page_view", d20, fnWithUtmSource("late")),
		fnRec("u:7", "pricing_view", d20+1),
		fnRec("u:7", "checkout_start", d20+6),
		fnRec("u:7", "purchase", d20+10),
	}
	res := fnCompute30d(t, "web_purchase", recs, nil, "source")
	require.Equal(t, []int{1, 1, 1, 1}, fnStepCounts(res))
	for _, d := range res.Daily {
		if d.Date == "2026-09-21" {
			assert.Equal(t, FunnelDay{Date: "2026-09-21", Entered: 1, Completed: 1}, d)
		} else {
			assert.Equal(t, FunnelDay{Date: d.Date}, d)
		}
	}
	// 耗时从第 20 天那次走起：60s / 300s / 240s，不是 20 天。
	for i, want := range []int64{60, 300, 240} {
		require.NotNil(t, res.Steps[i+1].MedianSecFromPrev)
		assert.Equal(t, want, *res.Steps[i+1].MedianSecFromPrev, "step %d", i+1)
	}
	assert.Equal(t, []FunnelGroup{{Key: "late", Steps: []int{1, 1, 1, 1}}}, res.Groups)
}

// 多个候选进入：走得最深的那次胜出；一样深取最早的。
func TestCompute_DeepestEntryWinsTiesGoToEarliest(t *testing.T) {
	d20 := 20 * fnMinPerDay
	daysWithEntry := func(res FunnelResult) []string {
		var out []string
		for _, d := range res.Daily {
			if d.Entered > 0 {
				out = append(out, d.Date)
			}
		}
		return out
	}
	// 早的走到第 3 步，晚的只到第 2 步 → 用早的。
	res := fnCompute30d(t, "web_purchase", []funnelRecord{
		fnRec("sid:A", "page_view", 0, fnWithUtmSource("early")),
		fnRec("sid:A", "pricing_view", 1),
		fnRec("sid:A", "checkout_start", 2),
		fnRec("sid:A", "page_view", d20, fnWithUtmSource("late")),
		fnRec("sid:A", "pricing_view", d20+5),
	}, nil, "source")
	assert.Equal(t, []FunnelGroup{{Key: "early", Steps: []int{1, 1, 1, 0}}}, res.Groups)
	assert.Equal(t, []string{"2026-09-01"}, daysWithEntry(res))
	assert.Equal(t, int64(60), *res.Steps[1].MedianSecFromPrev)

	// 一样深（都到第 2 步）→ 最早的候选。
	res = fnCompute30d(t, "web_purchase", []funnelRecord{
		fnRec("sid:A", "page_view", 0, fnWithUtmSource("early")),
		fnRec("sid:A", "pricing_view", 1),
		fnRec("sid:A", "page_view", d20, fnWithUtmSource("late")),
		fnRec("sid:A", "pricing_view", d20+5),
	}, nil, "source")
	assert.Equal(t, []FunnelGroup{{Key: "early", Steps: []int{1, 1, 0, 0}}}, res.Groups)
	assert.Equal(t, []string{"2026-09-01"}, daysWithEntry(res))
	assert.Equal(t, int64(60), *res.Steps[1].MedianSecFromPrev)

	// 晚的更深（没走完）→ 用晚的；人仍然只计一次。
	res = fnCompute30d(t, "web_purchase", []funnelRecord{
		fnRec("sid:A", "page_view", 0, fnWithUtmSource("early")),
		fnRec("sid:A", "pricing_view", 1),
		fnRec("sid:A", "page_view", d20, fnWithUtmSource("late")),
		fnRec("sid:A", "pricing_view", d20+5),
		fnRec("sid:A", "checkout_start", d20+6),
	}, nil, "source")
	assert.Equal(t, []FunnelGroup{{Key: "late", Steps: []int{1, 1, 1, 0}}}, res.Groups)
	assert.Equal(t, []string{"2026-09-21"}, daysWithEntry(res))
	assert.Equal(t, int64(300), *res.Steps[1].MedianSecFromPrev)

	// 区间外的候选不参与，即使它能走得更深。
	res = fnCompute30d(t, "web_purchase", []funnelRecord{
		fnRec("sid:A", "page_view", -5),
		fnRec("sid:A", "pricing_view", -4),
		fnRec("sid:A", "checkout_start", -3),
		fnRec("sid:A", "page_view", 10),
	}, nil, "")
	assert.Equal(t, []int{1, 0, 0, 0}, fnStepCounts(res))
}

// 进入前发生的后续步骤事件不算（At >= 上一步）。
func TestCompute_EventsBeforeEntryNotCounted(t *testing.T) {
	recs := []funnelRecord{
		fnRec("sid:A", "auth_done", -5),
		fnRec("sid:A", "auth_code_sent", 0),
	}
	res := fnCompute30d(t, "web_checkout_auth", recs, nil, "")
	assert.Equal(t, []int{1, 0, 0}, fnStepCounts(res))
}

func TestCompute_RepeatEventsCountOnce(t *testing.T) {
	var recs []funnelRecord
	for i := 0; i < 5; i++ {
		recs = append(recs, fnRec("sid:A", "page_view", i))
	}
	res := fnCompute30d(t, "web_purchase", recs, nil, "")
	assert.Equal(t, []int{1, 0, 0, 0}, fnStepCounts(res))
	entered := 0
	for _, d := range res.Daily {
		entered += d.Entered
	}
	assert.Equal(t, 1, entered)
}

func TestCompute_SurfaceFilter(t *testing.T) {
	res := fnCompute30d(t, "web_checkout_auth", []funnelRecord{
		fnRec("did:D", "auth_code_sent", 0), // app 面
		fnRec("did:D", "auth_done", 1),
		fnRec("did:D", "checkout_start", 2),
	}, nil, "")
	assert.Equal(t, []int{0, 0, 0}, fnStepCounts(res))

	res = fnCompute30d(t, "web_checkout_auth", []funnelRecord{
		fnRec("u:7", "auth_code_sent", 0),
		fnRec("u:7", "auth_done", 1, fnWithSurface("app")), // 面不对 → 不满足第 2 步
		fnRec("u:7", "auth_done", 2),
		fnRec("u:7", "checkout_start", 3),
	}, nil, "")
	assert.Equal(t, []int{1, 1, 1}, fnStepCounts(res))
	assert.Equal(t, int64(120), *res.Steps[1].MedianSecFromPrev)
}

// web_checkout_auth 诊断的是网站内联登录：第 3 步只认 web 面的 checkout_start。
func TestCompute_WebCheckoutAuthNeedsWebCheckout(t *testing.T) {
	auth := []funnelRecord{fnRec("u:7", "auth_code_sent", 0), fnRec("u:7", "auth_done", 1)}
	res := fnCompute30d(t, "web_checkout_auth", append(slices.Clone(auth), fnRec("u:7", "checkout_start", 2, fnWithSurface("app"))), nil, "")
	assert.Equal(t, []int{1, 1, 0}, fnStepCounts(res))
	res = fnCompute30d(t, "web_checkout_auth", append(slices.Clone(auth), fnRec("u:7", "checkout_start", 2, fnWithSurface("web"))), nil, "")
	assert.Equal(t, []int{1, 1, 1}, fnStepCounts(res))
}

// 购买路径的 checkout_start 不限面：一个面上看、另一个面上付是真实转化。
func TestCompute_PurchasePathsAcceptCheckoutOnEitherSurface(t *testing.T) {
	res := fnCompute30d(t, "web_purchase", []funnelRecord{
		fnRec("u:7", "pricing_view", 0), fnRec("u:7", "checkout_start", 2, fnWithSurface("app")),
	}, nil, "")
	assert.Equal(t, []int{1, 1, 1, 0}, fnStepCounts(res))
	res = fnCompute30d(t, "app_purchase", []funnelRecord{
		fnRec("u:7", "paywall_view", 0), fnRec("u:7", "checkout_start", 2, fnWithSurface("web")),
	}, nil, "")
	assert.Equal(t, []int{1, 1, 0}, fnStepCounts(res))
}

// fnGroupStepSums 把各组逐步相加。
func fnGroupStepSums(res FunnelResult) []int {
	sum := make([]int, len(res.Steps))
	for _, g := range res.Groups {
		for i, c := range g.Steps {
			sum[i] += c
		}
	}
	return sum
}

// 带面过滤的步骤不匹配事实（Surface==""）；不带面过滤的匹配。
func TestCompute_SurfaceFilterNeverMatchesFacts(t *testing.T) {
	filtered := funnelPath{Key: "t", Window: time.Hour, Steps: []funnelStep{
		{Label: "a", Events: []string{"purchase"}, Surface: FunnelSurfaceWeb},
		{Label: "b", Events: []string{"first_connect_ok"}},
	}}
	recs := []funnelRecord{fnRec("u:7", "purchase", 0), fnRec("u:7", "first_connect_ok", 1)}
	res := computeFunnel(filtered, recs, nil, funnelT0, funnelT0.Add(time.Hour), "")
	assert.Equal(t, []int{0, 0}, fnStepCounts(res))

	res = fnCompute30d(t, "post_purchase_activation", recs, nil, "")
	assert.Equal(t, []int{1, 1}, fnStepCounts(res))
}

func TestCompute_UtmSourceFilter(t *testing.T) {
	p := funnelPath{Key: "t", Window: time.Hour, Steps: []funnelStep{
		{Label: "a", Events: []string{"page_view"}, UtmSource: "tw"},
		{Label: "b", Events: []string{"pricing_view"}},
	}}
	recs := []funnelRecord{
		fnRec("sid:A", "page_view", 0, fnWithUtmSource("tw")),
		fnRec("sid:A", "pricing_view", 1),
		fnRec("sid:B", "page_view", 0, fnWithUtmSource("fb")),
		fnRec("sid:B", "pricing_view", 1),
		fnRec("sid:C", "page_view", 0),
	}
	res := computeFunnel(p, recs, nil, funnelT0, funnelT0.Add(time.Hour), "")
	assert.Equal(t, []int{1, 1}, fnStepCounts(res))
}

func TestCompute_NoIdentityNoAnonSkipped(t *testing.T) {
	recs := []funnelRecord{
		fnRec("", "page_view", 0),
		fnRec("", "pricing_view", 1),
		{At: funnelT0, Event: "page_view", Surface: "web", AnonKind: "sid", AnonID: ""}, // 有 kind 没 id
	}
	// 即使 identities 里有个空键也不能把这些记录归给谁。
	res := fnCompute30d(t, "web_purchase", recs, map[string]uint64{":": 7, "sid:": 8}, "source")
	assert.Equal(t, []int{0, 0, 0, 0}, fnStepCounts(res))
	assert.Empty(t, res.Groups)
}

func TestCompute_RatesAndMedian(t *testing.T) {
	recs := []funnelRecord{
		fnRec("sid:A", "page_view", 0), fnRec("sid:A", "pricing_view", 1), // 60s
		fnRec("sid:B", "page_view", 0), fnRec("sid:B", "pricing_view", 3), // 180s
		fnRec("sid:C", "page_view", 0),
		fnRec("sid:A", "checkout_start", 5),
	}
	res := fnCompute30d(t, "web_purchase", recs, nil, "")
	require.Equal(t, []int{3, 2, 1, 0}, fnStepCounts(res))

	assert.Equal(t, 1.0, res.Steps[0].RateFromPrev)
	assert.Equal(t, 1.0, res.Steps[0].RateFromFirst)
	assert.Nil(t, res.Steps[0].MedianSecFromPrev)

	assert.InDelta(t, 0.6667, res.Steps[1].RateFromPrev, 1e-4)
	assert.InDelta(t, 0.6667, res.Steps[1].RateFromFirst, 1e-4)
	require.NotNil(t, res.Steps[1].MedianSecFromPrev)
	assert.Equal(t, int64(60), *res.Steps[1].MedianSecFromPrev) // 偶数个取下中位

	assert.InDelta(t, 0.5, res.Steps[2].RateFromPrev, 1e-9)
	assert.InDelta(t, 0.3333, res.Steps[2].RateFromFirst, 1e-4)
	assert.Equal(t, int64(240), *res.Steps[2].MedianSecFromPrev)

	// 没人到达 → 0，不是 NaN；Count==0 → 中位数 nil。（上一步人数为 0 的情形见 TestCompute_EmptyInput。）
	for _, i := range []int{3} {
		assert.Equal(t, 0.0, res.Steps[i].RateFromPrev)
		assert.Equal(t, 0.0, res.Steps[i].RateFromFirst)
		assert.Nil(t, res.Steps[i].MedianSecFromPrev)
	}
}

func TestCompute_MedianOddAndEven(t *testing.T) {
	build := func(mins ...int) []funnelRecord {
		var recs []funnelRecord
		for i, m := range mins {
			person := fmt.Sprintf("sid:P%d", i)
			recs = append(recs, fnRec(person, "auth_code_sent", 0), fnRec(person, "auth_done", m))
		}
		return recs
	}
	res := fnCompute30d(t, "web_checkout_auth", build(9, 1, 5), nil, "")
	assert.Equal(t, int64(300), *res.Steps[1].MedianSecFromPrev)
	res = fnCompute30d(t, "web_checkout_auth", build(9, 1, 5, 7), nil, "")
	assert.Equal(t, int64(300), *res.Steps[1].MedianSecFromPrev) // 1,5,7,9 → 下中位 5
	res = fnCompute30d(t, "web_checkout_auth", build(4), nil, "")
	assert.Equal(t, int64(240), *res.Steps[1].MedianSecFromPrev)
}

func TestCompute_GroupBySource(t *testing.T) {
	recs := []funnelRecord{
		fnRec("sid:A", "page_view", 0, fnWithUtmSource("tw"), fnWithRefHost("t.co")),
		fnRec("sid:A", "pricing_view", 1, fnWithUtmSource("other")), // 分组键只看进入记录
		fnRec("sid:B", "page_view", 0, fnWithUtmSource("tw")),
		fnRec("sid:C", "page_view", 0, fnWithRefHost("google.com")),
		fnRec("sid:D", "page_view", 0),
	}
	res := fnCompute30d(t, "web_purchase", recs, nil, "source")
	assert.Equal(t, []FunnelGroup{
		{Key: "tw", Steps: []int{2, 1, 0, 0}},
		{Key: "direct", Steps: []int{1, 0, 0, 0}}, // 并列按 key 升序
		{Key: "google.com", Steps: []int{1, 0, 0, 0}},
	}, res.Groups)
	assert.Equal(t, fnStepCounts(res), fnGroupStepSums(res))
}

func TestCompute_GroupByPlanUsesLastStep(t *testing.T) {
	recs := []funnelRecord{
		fnRec("sid:A", "pricing_view", 0),
		fnRec("sid:A", "checkout_start", 1, fnWithPlan("p1"), fnWithChannel("stripe")),
		// B 止步于第 2 步，最后一步记录（pricing_view）没有 plan。
		fnRec("sid:B", "pricing_view", 0),
	}
	res := fnCompute30d(t, "web_purchase", recs, nil, "plan")
	assert.Equal(t, []FunnelGroup{
		{Key: "p1", Steps: []int{1, 1, 1, 0}},
		{Key: "unknown", Steps: []int{1, 1, 0, 0}},
	}, res.Groups)

	res = fnCompute30d(t, "web_purchase", recs, nil, "channel")
	assert.Equal(t, "stripe", res.Groups[0].Key)
}

func TestCompute_GroupByEntryDims(t *testing.T) {
	entry := fnRec("did:D", "paywall_view", 0, fnWithSource("quota_wall"), fnWithUtmCampaign("c1"), fnWithCountry("JP"))
	entry.OS, entry.Device, entry.AppVersion = "ios", "mobile", "0.4.10"
	later := fnRec("did:D", "checkout_start", 1, fnWithSource("other"), fnWithCountry("US"))
	later.OS = "android"
	bare := fnRec("did:E", "paywall_view", 0)
	recs := []funnelRecord{entry, later, bare}
	want := map[string][2]string{
		"utm_campaign":   {"c1", "unknown"},
		"country":        {"JP", "unknown"},
		"os":             {"ios", "unknown"},
		"device":         {"mobile", "unknown"},
		"app_version":    {"0.4.10", "unknown"},
		"paywall_source": {"quota_wall", "unknown"},
	}
	for dim, keys := range want {
		res := fnCompute30d(t, "app_purchase", recs, nil, dim)
		assert.Equal(t, []FunnelGroup{
			{Key: keys[0], Steps: []int{1, 1, 0}},
			{Key: keys[1], Steps: []int{1, 0, 0}},
		}, res.Groups, dim)
	}
	// 每个声明的维度都被实现（未实现的会退化成"不分组"）。
	for _, dim := range funnelGroupDims {
		res := fnCompute30d(t, "app_purchase", recs, nil, dim)
		assert.NotEmpty(t, res.Groups, "dimension %q is declared but yields no groups", dim)
	}
}

func TestCompute_UnknownGroupByIsNoGrouping(t *testing.T) {
	recs := []funnelRecord{fnRec("sid:A", "page_view", 0)}
	for _, g := range []string{"", "nope", "Source", "path"} {
		res := fnCompute30d(t, "web_purchase", recs, nil, g)
		assert.NotNil(t, res.Groups, g)
		assert.Empty(t, res.Groups, g)
		assert.Equal(t, 1, res.Steps[0].Count, g)
	}
}

// 超过 50 组：第 51 名起并进最后一个 "(other)" 组，各组逐步相加仍等于总数。
func TestCompute_GroupsBeyond50FoldIntoOther(t *testing.T) {
	var recs []funnelRecord
	// 53 个来源。前 50 个各 2 人（其中 1 人走到第 2 步）；后 3 个各 1 人，其中 s51 的走到第 3 步。
	for i := 0; i < 53; i++ {
		src := fnWithUtmSource(fmt.Sprintf("s%02d", i))
		a := fmt.Sprintf("sid:S%dA", i)
		recs = append(recs, fnRec(a, "page_view", 0, src), fnRec(a, "pricing_view", 1))
		if i < 50 {
			recs = append(recs, fnRec(fmt.Sprintf("sid:S%dB", i), "page_view", 0, src))
		}
		if i == 51 {
			recs = append(recs, fnRec(a, "checkout_start", 2))
		}
	}
	res := fnCompute30d(t, "web_purchase", recs, nil, "source")
	require.Equal(t, []int{103, 53, 1, 0}, fnStepCounts(res))
	require.Len(t, res.Groups, 51)
	assert.Equal(t, FunnelGroup{Key: "s00", Steps: []int{2, 1, 0, 0}}, res.Groups[0])
	assert.Equal(t, FunnelGroup{Key: "s49", Steps: []int{2, 1, 0, 0}}, res.Groups[49])
	assert.Equal(t, FunnelGroup{Key: "(other)", Steps: []int{3, 3, 1, 0}}, res.Groups[50])
	assert.Equal(t, fnStepCounts(res), fnGroupStepSums(res))

	// 正好 50 组：不产生 "(other)"。
	var fifty []funnelRecord
	for i := 0; i < 50; i++ {
		fifty = append(fifty, fnRec(fmt.Sprintf("sid:P%d", i), "page_view", 0, fnWithUtmSource(fmt.Sprintf("s%02d", i))))
	}
	res = fnCompute30d(t, "web_purchase", fifty, nil, "source")
	require.Len(t, res.Groups, 50)
	assert.Equal(t, "s49", res.Groups[49].Key)
}

// 真实取值恰好是 "(other)"：有折叠桶时并进去（不出现两个同名组）；没有折叠桶时是普通的一组。
func TestCompute_RealOtherValueMergesIntoFoldBucket(t *testing.T) {
	build := func(nKeys int) []funnelRecord {
		var recs []funnelRecord
		// 真实的 "(other)" 人数最多（3 人，1 人到第 2 步），按人数本该排第一。
		for j := 0; j < 3; j++ {
			recs = append(recs, fnRec(fmt.Sprintf("sid:O%d", j), "page_view", 0, fnWithUtmSource("(other)")))
		}
		recs = append(recs, fnRec("sid:O0", "pricing_view", 1))
		for i := 0; i < nKeys-1; i++ {
			recs = append(recs, fnRec(fmt.Sprintf("sid:P%d", i), "page_view", 0, fnWithUtmSource(fmt.Sprintf("s%02d", i))))
		}
		return recs
	}
	countOther := func(res FunnelResult) (n int) {
		for _, g := range res.Groups {
			if g.Key == "(other)" {
				n++
			}
		}
		return n
	}

	// 53 个取值（含真实 "(other)"）：前 50 个普通组 + 一个 "(other)" = 真实的 3 人 + 被折叠的 s50、s51。
	res := fnCompute30d(t, "web_purchase", build(53), nil, "source")
	require.Len(t, res.Groups, 51)
	assert.Equal(t, 1, countOther(res))
	assert.Equal(t, "s00", res.Groups[0].Key)
	assert.Equal(t, "s49", res.Groups[49].Key)
	assert.Equal(t, FunnelGroup{Key: "(other)", Steps: []int{5, 1, 0, 0}}, res.Groups[50])
	assert.Equal(t, fnStepCounts(res), fnGroupStepSums(res))

	// 51 个取值（含真实 "(other)"）：50 个普通组 + "(other)" 只含真实的那 3 人。
	res = fnCompute30d(t, "web_purchase", build(51), nil, "source")
	require.Len(t, res.Groups, 51)
	assert.Equal(t, 1, countOther(res))
	assert.Equal(t, FunnelGroup{Key: "(other)", Steps: []int{3, 1, 0, 0}}, res.Groups[50])
	assert.Equal(t, fnStepCounts(res), fnGroupStepSums(res))

	// 50 个取值：没有折叠桶，真实 "(other)" 是普通组，按人数排第一。
	res = fnCompute30d(t, "web_purchase", build(50), nil, "source")
	require.Len(t, res.Groups, 50)
	assert.Equal(t, FunnelGroup{Key: "(other)", Steps: []int{3, 1, 0, 0}}, res.Groups[0])
	assert.Equal(t, fnStepCounts(res), fnGroupStepSums(res))
}

func TestCompute_GroupsCappedAt50(t *testing.T) {
	var recs []funnelRecord
	// 60 个来源；来源 i 有 (i%3)+1 个人。
	for i := 0; i < 60; i++ {
		for j := 0; j <= i%3; j++ {
			recs = append(recs, fnRec(fmt.Sprintf("sid:S%dP%d", i, j), "page_view", 0, fnWithUtmSource(fmt.Sprintf("s%02d", i))))
		}
	}
	res := fnCompute30d(t, "web_purchase", recs, nil, "source")
	require.Len(t, res.Groups, 51)
	assert.Equal(t, 120, res.Steps[0].Count)
	assert.Equal(t, fnStepCounts(res), fnGroupStepSums(res))
	// "(other)" 排最后，不参与排序（它的人数比前面的组多）。
	assert.Equal(t, FunnelGroup{Key: "(other)", Steps: []int{10, 0, 0, 0}}, res.Groups[50])
	for i := 1; i < 50; i++ {
		a, b := res.Groups[i-1], res.Groups[i]
		assert.True(t, a.Steps[0] > b.Steps[0] || (a.Steps[0] == b.Steps[0] && a.Key < b.Key), "order at %d", i)
	}
	assert.Equal(t, FunnelGroup{Key: "s02", Steps: []int{3, 0, 0, 0}}, res.Groups[0])
	// 20 组 3 人 + 20 组 2 人 + 10 组 1 人（key 最小的 10 个）。
	assert.Equal(t, 1, res.Groups[49].Steps[0])
	assert.Equal(t, "s27", res.Groups[49].Key)
}

func TestCompute_DailyCoversEveryDay(t *testing.T) {
	recs := []funnelRecord{
		fnRec("sid:A", "auth_code_sent", fnMinPerDay+10),
		fnRec("sid:A", "auth_done", fnMinPerDay+11),
		fnRec("sid:A", "checkout_start", fnMinPerDay+12),
		fnRec("sid:B", "auth_code_sent", fnMinPerDay+20),
	}
	res := computeFunnel(fnMustPath(t, "web_checkout_auth"), recs, nil, funnelT0, funnelT0.Add(3*24*time.Hour), "")
	assert.Equal(t, []FunnelDay{
		{Date: "2026-09-01"},
		{Date: "2026-09-02", Entered: 2, Completed: 1},
		{Date: "2026-09-03"},
	}, res.Daily)
}

// Completed 记在进入日，不是完成日；日期按 UTC 切。
func TestCompute_DailyByEntryDayUTC(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	// 北京时间 9/2 07:30 = UTC 9/1 23:30 → 记在 9/1。
	entryAt := time.Date(2026, 9, 2, 7, 30, 0, 0, cst)
	recs := []funnelRecord{
		fnRec("sid:A", "auth_code_sent", 0, fnWithAt(entryAt)),
		fnRec("sid:A", "auth_done", 0, fnWithAt(entryAt.Add(time.Hour))),
		fnRec("sid:A", "checkout_start", 0, fnWithAt(entryAt.Add(2*time.Hour))), // UTC 9/2
	}
	res := computeFunnel(fnMustPath(t, "web_checkout_auth"), recs, nil, funnelT0, funnelT0.Add(2*24*time.Hour), "")
	assert.Equal(t, []FunnelDay{
		{Date: "2026-09-01", Entered: 1, Completed: 1},
		{Date: "2026-09-02"},
	}, res.Daily)

	// from/to 带非 UTC 时区也按 UTC 日列出。
	res = computeFunnel(fnMustPath(t, "web_checkout_auth"), recs, nil, funnelT0.In(cst), funnelT0.Add(2*24*time.Hour).In(cst), "")
	require.Len(t, res.Daily, 2)
	assert.Equal(t, "2026-09-01", res.Daily[0].Date)
	assert.Equal(t, 1, res.Daily[0].Entered)
}

// 区间不对齐到 UTC 零点时，每个进入者仍然落在某一天里。
func TestCompute_DailyUnalignedRange(t *testing.T) {
	from := funnelT0.Add(10 * time.Hour)
	to := funnelT0.Add(34 * time.Hour) // 9/2 10:00
	recs := []funnelRecord{
		fnRec("sid:A", "page_view", 11*60),
		fnRec("sid:B", "page_view", 25*60),
	}
	res := computeFunnel(fnMustPath(t, "web_purchase"), recs, nil, from, to, "")
	assert.Equal(t, []FunnelDay{
		{Date: "2026-09-01", Entered: 1},
		{Date: "2026-09-02", Entered: 1},
	}, res.Daily)
}

func TestCompute_EmptyInput(t *testing.T) {
	for _, p := range funnelPathRegistry {
		res := computeFunnel(p, nil, nil, funnelT0, funnelT0.Add(2*24*time.Hour), "source")
		require.Len(t, res.Steps, len(p.Steps), p.Key)
		for i, s := range res.Steps {
			assert.Equal(t, p.Steps[i].Label, s.Label)
			assert.Equal(t, 0, s.Count)
			assert.Nil(t, s.MedianSecFromPrev)
			assert.Equal(t, 0.0, s.RateFromFirst) // 第 1 步人数为 0 → 全部 0（含第 1 步）
			if i == 0 {
				assert.Equal(t, 1.0, s.RateFromPrev)
			} else {
				assert.Equal(t, 0.0, s.RateFromPrev)
			}
		}
		assert.Len(t, res.Daily, 2)
		assert.NotNil(t, res.Groups)
		assert.Empty(t, res.Groups)
	}
	// 空区间 / 反向区间：不 panic，daily 为空数组。
	p := fnMustPath(t, "web_purchase")
	for _, to := range []time.Time{funnelT0, funnelT0.Add(-time.Hour)} {
		res := computeFunnel(p, []funnelRecord{fnRec("sid:A", "page_view", 0)}, nil, funnelT0, to, "")
		assert.NotNil(t, res.Daily)
		assert.Empty(t, res.Daily)
		assert.Equal(t, 0, res.Steps[0].Count)
	}
}

// JSON 字段名是与看板的契约。
func TestCompute_JSONShape(t *testing.T) {
	recs := []funnelRecord{fnRec("sid:A", "auth_code_sent", 0), fnRec("sid:A", "auth_done", 2)}
	p := fnMustPath(t, "web_checkout_auth")

	raw, err := json.Marshal(computeFunnel(p, recs, nil, funnelT0, funnelT0.Add(24*time.Hour), ""))
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"steps": [
			{"label":"发验证码","count":1,"rateFromPrev":1,"rateFromFirst":1,"medianSecFromPrev":null},
			{"label":"登录成功","count":1,"rateFromPrev":1,"rateFromFirst":1,"medianSecFromPrev":120},
			{"label":"发起支付","count":0,"rateFromPrev":0,"rateFromFirst":0,"medianSecFromPrev":null}
		],
		"daily": [{"date":"2026-09-01","entered":1,"completed":0}],
		"groups": []
	}`, string(raw))

	raw, err = json.Marshal(computeFunnel(p, recs, nil, funnelT0, funnelT0.Add(24*time.Hour), "source"))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, []any{map[string]any{"key": "direct", "steps": []any{1.0, 1.0, 0.0}}}, got["groups"])

	// 空输入、空区间：三个数组都是 [] 而不是 null。
	raw, err = json.Marshal(computeFunnel(p, nil, nil, funnelT0, funnelT0, ""))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"daily":[]`)
	assert.Contains(t, string(raw), `"groups":[]`)
	assert.Contains(t, string(raw), `"steps":[{`)
}

func TestCompute_DoesNotMutateInput(t *testing.T) {
	recs := []funnelRecord{
		fnRec("sid:A", "pricing_view", 5),
		fnRec("sid:A", "page_view", 0),
		fnRec("sid:B", "page_view", 3),
	}
	before := slices.Clone(recs)
	ids := map[string]uint64{"sid:A": 7}
	fnCompute30d(t, "web_purchase", recs, ids, "source")
	assert.Equal(t, before, recs)
	assert.Equal(t, map[string]uint64{"sid:A": 7}, ids)
}

// 同一时刻的记录：按输入顺序稳定。两条同刻的 checkout_start，第一条决定 plan 分组键。
func TestCompute_EqualTimestampsUseInputOrder(t *testing.T) {
	recs := []funnelRecord{
		fnRec("sid:A", "pricing_view", 0),
		fnRec("sid:A", "checkout_start", 1, fnWithPlan("first")),
		fnRec("sid:A", "checkout_start", 1, fnWithPlan("second")),
	}
	res := fnCompute30d(t, "web_purchase", recs, nil, "plan")
	assert.Equal(t, "first", res.Groups[0].Key)

	// 记录多到排序算法不再天然稳定时也一样（乱序输入 + 大量同刻记录）。
	recs = []funnelRecord{fnRec("sid:A", "pricing_view", 0)}
	for i := 0; i < 200; i++ {
		recs = append(recs, fnRec("sid:A", "checkout_start", 2-i%2, fnWithPlan(fmt.Sprintf("p%03d", i))))
	}
	res = fnCompute30d(t, "web_purchase", recs, nil, "plan")
	assert.Equal(t, "p001", res.Groups[0].Key) // 第 1 分钟的记录里输入最靠前的那条

	// 同刻但在输入里排在上一步记录之前的记录也满足 At >= 上一步。
	recs = []funnelRecord{
		fnRec("sid:A", "checkout_start", 0),
		fnRec("sid:A", "pricing_view", 0),
	}
	res = fnCompute30d(t, "web_purchase", recs, nil, "")
	assert.Equal(t, []int{1, 1, 1, 0}, fnStepCounts(res))
}

// ---------- 性质测试（固定种子的随机数据，全部路径 × 全部分组维度） ----------

type funnelFixture struct {
	recs []funnelRecord
	ids  map[string]uint64
	from time.Time
	to   time.Time
}

// genFunnelFixture 生成一批随机记录。时间戳两两不同（分钟级排列），所以结果与输入顺序无关。
func genFunnelFixture(seed int64, p funnelPath) funnelFixture {
	rng := rand.New(rand.NewSource(seed))
	var events []string
	for _, s := range p.Steps {
		for _, e := range s.Events {
			if !slices.Contains(events, e) {
				events = append(events, e)
			}
		}
	}
	events = append(events, "refund_click") // 与路径无关的噪声事件
	pick := func(xs ...string) string { return xs[rng.Intn(len(xs))] }

	nPersons := 5 + rng.Intn(25)
	fx := funnelFixture{ids: map[string]uint64{}, from: funnelT0, to: funnelT0.Add(10 * 24 * time.Hour)}
	type ident struct{ anon, user string }
	persons := make([]ident, nPersons)
	for i := range persons {
		kind := pick("sid", "did")
		persons[i].anon = fmt.Sprintf("%s:a%d", kind, i)
		switch rng.Intn(3) {
		case 0: // 纯匿名
		case 1: // 匿名 + 已关联用户
			persons[i].user = fmt.Sprintf("u:%d", 1000+i)
			fx.ids[persons[i].anon] = uint64(1000 + i)
		case 2: // 只有登录态
			persons[i].user = fmt.Sprintf("u:%d", 1000+i)
			persons[i].anon = ""
		}
	}
	n := 50 + rng.Intn(400)
	// 时间跨度随路径的窗口缩放（窗口短的路径要更密才走得深），最多 31 天；起点在 from 之前，
	// 所以总有区间外的记录。分钟不重复。
	span := min(31*fnMinPerDay, 6*int(p.Window/time.Minute))
	lead := span / 10
	offsets := rng.Perm(span)[:n]
	for _, off := range offsets {
		pr := persons[rng.Intn(nPersons)]
		who := pr.anon
		if who == "" || (pr.user != "" && rng.Intn(2) == 0) {
			who = pr.user
		}
		ev := events[rng.Intn(len(events))]
		r := fnRec(who, ev, off-lead,
			fnWithUtmSource(pick("", "", "tw", "fb")), fnWithRefHost(pick("", "google.com", "t.co")),
			fnWithUtmCampaign(pick("", "c1", "c2")), fnWithCountry(pick("", "JP", "US", "DE")),
			fnWithPlan(pick("", "p1", "p2")), fnWithChannel(pick("", "stripe", "apple")), fnWithSource(pick("", "quota", "banner")))
		r.OS, r.Device, r.AppVersion = pick("", "ios", "macos"), pick("", "mobile", "desktop"), pick("", "0.4.9", "0.4.10")
		if funnelEventKind(ev) != FunnelKindFact && rng.Intn(4) == 0 {
			r.Surface = pick(FunnelSurfaceWeb, FunnelSurfaceApp)
		}
		if funnelEventKind(ev) == FunnelKindFact && r.UserID == 0 {
			continue // 事实总是带 UserID
		}
		fx.recs = append(fx.recs, r)
	}
	// 少量没有任何身份的记录（GPC）。
	for i := 0; i < 5; i++ {
		fx.recs = append(fx.recs, fnRec("", events[0], 31*fnMinPerDay+i))
	}
	return fx
}

// funnelPersonOf 测试侧的"这条记录属于谁"，只用于把输入按人拆开（不参与任何计数）。
func funnelPersonOf(r funnelRecord, ids map[string]uint64) string {
	if r.UserID != 0 {
		return fmt.Sprintf("u:%d", r.UserID)
	}
	if r.AnonID == "" {
		return ""
	}
	if uid, ok := ids[r.AnonKind+":"+r.AnonID]; ok {
		return fmt.Sprintf("u:%d", uid)
	}
	return r.AnonKind + ":" + r.AnonID
}

func forEachFunnelFixture(t *testing.T, fn func(t *testing.T, p funnelPath, fx funnelFixture)) {
	for _, p := range funnelPathRegistry {
		for seed := int64(1); seed <= 25; seed++ {
			t.Run(fmt.Sprintf("%s/seed%d", p.Key, seed), func(t *testing.T) {
				fn(t, p, genFunnelFixture(seed, p))
			})
		}
	}
}

// 夹具必须真的把漏斗走深，否则下面的性质都是空话。
func TestComputeProperty_FixturesReachDeepSteps(t *testing.T) {
	for _, p := range funnelPathRegistry {
		maxLast, total := 0, 0
		for seed := int64(1); seed <= 25; seed++ {
			fx := genFunnelFixture(seed, p)
			res := computeFunnel(p, fx.recs, fx.ids, fx.from, fx.to, "")
			maxLast = max(maxLast, res.Steps[len(res.Steps)-1].Count)
			total += res.Steps[0].Count
		}
		assert.Greater(t, maxLast, 0, "%s: no fixture ever completes the funnel", p.Key)
		assert.Greater(t, total, 100, "%s: fixtures barely enter the funnel", p.Key)
	}
}

func TestComputeProperty_StepCountsNonIncreasing(t *testing.T) {
	forEachFunnelFixture(t, func(t *testing.T, p funnelPath, fx funnelFixture) {
		res := computeFunnel(p, fx.recs, fx.ids, fx.from, fx.to, "")
		counts := fnStepCounts(res)
		for i := 1; i < len(counts); i++ {
			assert.LessOrEqual(t, counts[i], counts[i-1], "step %d", i)
		}
		for i, s := range res.Steps {
			assert.GreaterOrEqual(t, s.RateFromPrev, 0.0)
			assert.LessOrEqual(t, s.RateFromPrev, 1.0)
			assert.LessOrEqual(t, s.RateFromFirst, 1.0)
			if i > 0 && s.Count > 0 {
				require.NotNil(t, s.MedianSecFromPrev)
				assert.GreaterOrEqual(t, *s.MedianSecFromPrev, int64(0))
				assert.LessOrEqual(t, *s.MedianSecFromPrev, int64(p.Window/time.Second))
			} else {
				assert.Nil(t, s.MedianSecFromPrev)
			}
		}
	})
}

// 单独一个人的结果必须形如 1…1 0…0（到了第 k 步就到过所有更早的步），
// 且全体结果 = 各人单独结果之和（别人的记录不影响某个人的结果）。
func TestComputeProperty_PerPersonPrefixAndAdditivity(t *testing.T) {
	forEachFunnelFixture(t, func(t *testing.T, p funnelPath, fx funnelFixture) {
		byPerson := map[string][]funnelRecord{}
		var order []string
		for _, r := range fx.recs {
			who := funnelPersonOf(r, fx.ids)
			if _, ok := byPerson[who]; !ok {
				order = append(order, who)
			}
			byPerson[who] = append(byPerson[who], r)
		}
		sum := make([]int, len(p.Steps))
		daily := map[string][2]int{}
		for _, who := range order {
			res := computeFunnel(p, byPerson[who], fx.ids, fx.from, fx.to, "")
			counts := fnStepCounts(res)
			for i, c := range counts {
				require.Contains(t, []int{0, 1}, c, "person %q step %d", who, i)
				if i > 0 {
					require.LessOrEqual(t, c, counts[i-1], "person %q reached step %d without step %d", who, i, i-1)
				}
				sum[i] += c
			}
			if who == "" {
				require.Equal(t, 0, counts[0], "records without identity must not form a person")
			}
			for _, d := range res.Daily {
				cur := daily[d.Date]
				daily[d.Date] = [2]int{cur[0] + d.Entered, cur[1] + d.Completed}
			}
		}
		all := computeFunnel(p, fx.recs, fx.ids, fx.from, fx.to, "")
		assert.Equal(t, sum, fnStepCounts(all))
		for _, d := range all.Daily {
			assert.Equal(t, daily[d.Date], [2]int{d.Entered, d.Completed}, d.Date)
		}
	})
}

func TestComputeProperty_ShuffleInvariant(t *testing.T) {
	forEachFunnelFixture(t, func(t *testing.T, p funnelPath, fx funnelFixture) {
		for _, groupBy := range append([]string{""}, funnelGroupDims...) {
			want := computeFunnel(p, fx.recs, fx.ids, fx.from, fx.to, groupBy)
			rng := rand.New(rand.NewSource(99))
			for round := 0; round < 3; round++ {
				shuffled := slices.Clone(fx.recs)
				rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
				got := computeFunnel(p, shuffled, fx.ids, fx.from, fx.to, groupBy)
				require.Equal(t, want, got, "groupBy=%q", groupBy)
			}
			// 同一输入重复计算结果相同（不依赖 map 迭代顺序）。
			require.Equal(t, want, computeFunnel(p, fx.recs, fx.ids, fx.from, fx.to, groupBy), "groupBy=%q", groupBy)
		}
	})
}

// 分组与按日是总数的划分：各组逐步相加 = 总数；Daily 的 Entered / Completed 之和 = 首 / 末步人数。
func TestComputeProperty_GroupsAndDailyPartitionTotals(t *testing.T) {
	forEachFunnelFixture(t, func(t *testing.T, p funnelPath, fx funnelFixture) {
		for _, groupBy := range funnelGroupDims {
			res := computeFunnel(p, fx.recs, fx.ids, fx.from, fx.to, groupBy)
			require.Less(t, len(res.Groups), 50) // 夹具的取值域很小，不会触发截断
			sum := make([]int, len(p.Steps))
			for i, g := range res.Groups {
				require.Len(t, g.Steps, len(p.Steps))
				require.NotEmpty(t, g.Key)
				for j, c := range g.Steps {
					sum[j] += c
				}
				if i > 0 {
					prev := res.Groups[i-1]
					assert.True(t, prev.Steps[0] > g.Steps[0] || (prev.Steps[0] == g.Steps[0] && prev.Key < g.Key))
				}
			}
			assert.Equal(t, fnStepCounts(res), sum, "groupBy=%q", groupBy)

			entered, completed := 0, 0
			for _, d := range res.Daily {
				entered += d.Entered
				completed += d.Completed
				assert.LessOrEqual(t, d.Completed, d.Entered)
			}
			assert.Len(t, res.Daily, 10)
			assert.Equal(t, res.Steps[0].Count, entered)
			assert.Equal(t, res.Steps[len(res.Steps)-1].Count, completed)
		}
	})
}

// ---------- 独立参照实现（oracle） ----------
//
// 故意写得很笨：按规则文字直译的嵌套循环，不调用生产代码的任何 helper
// （自己归人、自己判断命中、自己取分组键、自己算中位数），只共享类型与路径注册表这些数据。

type fnOracleResult struct {
	counts  []int
	medians []*int64
	daily   map[string][2]int // date → entered, completed
	groups  map[string][]int
}

func fnOracleMatches(st funnelStep, r funnelRecord) bool {
	hit := false
	for _, e := range st.Events {
		if e == r.Event {
			hit = true
		}
	}
	if !hit {
		return false
	}
	if st.Surface != "" && st.Surface != r.Surface {
		return false
	}
	if st.UtmSource != "" && st.UtmSource != r.UtmSource {
		return false
	}
	return true
}

func fnOracleGroupKey(groupBy string, entry, last funnelRecord) string {
	v := ""
	switch groupBy {
	case "source":
		v = entry.UtmSource
		if v == "" {
			v = entry.RefHost
		}
		if v == "" {
			return "direct"
		}
		return v
	case "utm_campaign":
		v = entry.UtmCampaign
	case "country":
		v = entry.Country
	case "os":
		v = entry.OS
	case "device":
		v = entry.Device
	case "app_version":
		v = entry.AppVersion
	case "paywall_source":
		v = entry.Source
	case "plan":
		v = last.Plan
	case "channel":
		v = last.Channel
	}
	if v == "" {
		return "unknown"
	}
	return v
}

func fnOracle(p funnelPath, recs []funnelRecord, ids map[string]uint64, from, to time.Time, groupBy string) fnOracleResult {
	n := len(p.Steps)
	out := fnOracleResult{counts: make([]int, n), medians: make([]*int64, n), daily: map[string][2]int{}, groups: map[string][]int{}}
	gaps := make([][]int64, n)

	// 归人：记录在输入里的下标按人分开。
	people := map[string][]int{}
	for i, r := range recs {
		who := ""
		switch {
		case r.UserID != 0:
			who = fmt.Sprintf("u:%d", r.UserID)
		case r.AnonID == "":
			continue
		default:
			who = r.AnonKind + ":" + r.AnonID
			if uid, ok := ids[who]; ok {
				who = fmt.Sprintf("u:%d", uid)
			}
		}
		people[who] = append(people[who], i)
	}
	// "更早" = 时间更早；同刻则输入更靠前。
	earlier := func(a, b int) bool {
		if !recs[a].At.Equal(recs[b].At) {
			return recs[a].At.Before(recs[b].At)
		}
		return a < b
	}

	for _, mine := range people {
		var best []int
		bestEntry := -1
		for _, cand := range mine {
			c := recs[cand]
			if !fnOracleMatches(p.Steps[0], c) || c.At.Before(from) || !c.At.Before(to) {
				continue
			}
			walk := []int{cand}
			prev := cand
			for s := 1; s < n; s++ {
				next := -1
				for _, i := range mine {
					r := recs[i]
					if !fnOracleMatches(p.Steps[s], r) || r.At.Before(recs[prev].At) || r.At.After(c.At.Add(p.Window)) {
						continue
					}
					if next == -1 || earlier(i, next) {
						next = i
					}
				}
				if next == -1 {
					break
				}
				walk = append(walk, next)
				prev = next
			}
			if len(walk) > len(best) || (len(walk) == len(best) && earlier(cand, bestEntry)) {
				best, bestEntry = walk, cand
			}
		}
		if best == nil {
			continue
		}
		for s := range best {
			out.counts[s]++
			if s > 0 {
				gaps[s] = append(gaps[s], int64(recs[best[s]].At.Sub(recs[best[s-1]].At)/time.Second))
			}
		}
		day := recs[bestEntry].At.UTC().Format("2006-01-02")
		d := out.daily[day]
		d[0]++
		if len(best) == n {
			d[1]++
		}
		out.daily[day] = d
		if groupBy != "" {
			key := fnOracleGroupKey(groupBy, recs[bestEntry], recs[best[len(best)-1]])
			if out.groups[key] == nil {
				out.groups[key] = make([]int, n)
			}
			for s := range best {
				out.groups[key][s]++
			}
		}
	}
	for s := 1; s < n; s++ {
		if g := gaps[s]; len(g) > 0 {
			slices.Sort(g)
			m := g[(len(g)-1)/2]
			out.medians[s] = &m
		}
	}
	return out
}

func fnAssertMatchesOracle(t *testing.T, p funnelPath, fx funnelFixture, groupBy string) {
	t.Helper()
	got := computeFunnel(p, fx.recs, fx.ids, fx.from, fx.to, groupBy)
	want := fnOracle(p, fx.recs, fx.ids, fx.from, fx.to, groupBy)

	require.Equal(t, want.counts, fnStepCounts(got), "step counts, groupBy=%q", groupBy)
	for i, s := range got.Steps {
		require.Equal(t, want.medians[i], s.MedianSecFromPrev, "median of step %d", i)
	}
	gotDaily := map[string][2]int{}
	for _, d := range got.Daily {
		if d.Entered != 0 || d.Completed != 0 {
			gotDaily[d.Date] = [2]int{d.Entered, d.Completed}
		}
	}
	require.Equal(t, want.daily, gotDaily, "daily, groupBy=%q", groupBy)
	gotGroups := map[string][]int{}
	for _, g := range got.Groups {
		require.NotContains(t, gotGroups, g.Key)
		gotGroups[g.Key] = g.Steps
	}
	require.Equal(t, want.groups, gotGroups, "groups, groupBy=%q", groupBy)
}

func TestComputeOracle_RandomFixtures(t *testing.T) {
	multiEntry := 0
	for _, p := range funnelPathRegistry {
		for seed := int64(1); seed <= 60; seed++ {
			fx := genFunnelFixture(seed, p)
			// 变体：把时间戳压到 6 小时的格子上，制造大量同刻记录（同刻按输入顺序）。
			coarse := fx
			coarse.recs = slices.Clone(fx.recs)
			for i := range coarse.recs {
				coarse.recs[i].At = coarse.recs[i].At.Truncate(6 * time.Hour)
			}
			// 变体：只取区间的中间一段，区间两侧都有记录。
			narrow := fx
			narrow.from, narrow.to = fx.from.Add(36*time.Hour), fx.from.Add(5*24*time.Hour+7*time.Hour)
			for name, f := range map[string]funnelFixture{"exact": fx, "coarse": coarse, "narrow": narrow} {
				t.Run(fmt.Sprintf("%s/seed%d/%s", p.Key, seed, name), func(t *testing.T) {
					for _, groupBy := range append([]string{""}, funnelGroupDims...) {
						fnAssertMatchesOracle(t, p, f, groupBy)
					}
				})
			}
			// 夹具里确实有"最早的候选不是最好的候选"的人，否则 oracle 分辨不出进入规则。
			o := fnOracle(p, fx.recs, fx.ids, fx.from, fx.to, "")
			e := fnOracle(funnelPath{Key: p.Key, Steps: p.Steps[:1], Window: p.Window}, fx.recs, fx.ids, fx.from, fx.to, "")
			if fmt.Sprint(o.daily) != fmt.Sprint(e.daily) {
				multiEntry++
			}
		}
	}
	assert.Greater(t, multiEntry, 20, "fixtures rarely exercise the best-entry rule")
}

// 口径裁定：付款后激活的第 2 步认任何一次连上（connect_ok），不只是首次连上；
// 购买路径末步认续费；记录里有没有 plan_select 都不影响各步人数。
func TestCompute_RulingsOnStepEvents(t *testing.T) {
	res := fnCompute30d(t, "post_purchase_activation", []funnelRecord{
		fnRec("u:7", "purchase", 0), fnRec("u:7", "connect_ok", 5),
		fnRec("u:8", "purchase", 0), fnRec("u:8", "first_connect_ok", 5),
		fnRec("u:9", "purchase", 0),
		fnRec("u:10", "renewal", 0), fnRec("u:10", "connect_ok", 5), // 第 1 步只认首购
	}, nil, "")
	assert.Equal(t, []int{3, 2}, fnStepCounts(res))

	for _, withPlanSelect := range []bool{false, true} {
		web := []funnelRecord{fnRec("u:7", "pricing_view", 0), fnRec("u:7", "checkout_start", 2), fnRec("u:7", "renewal", 3)}
		app := []funnelRecord{fnRec("did:D", "paywall_view", 0), fnRec("did:D", "checkout_start", 2), fnRec("u:7", "renewal", 3)}
		if withPlanSelect {
			web = append(web, fnRec("u:7", "plan_select", 1))
			app = append(app, fnRec("did:D", "plan_select", 1))
		}
		assert.Equal(t, []int{1, 1, 1, 1}, fnStepCounts(fnCompute30d(t, "web_purchase", web, nil, "")), "plan_select=%v", withPlanSelect)
		assert.Equal(t, []int{1, 1, 1}, fnStepCounts(fnCompute30d(t, "app_purchase", app, map[string]uint64{"did:D": 7}, "")), "plan_select=%v", withPlanSelect)
	}
}
