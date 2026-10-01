package center

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// adminFunnelRouter 镜像 route.go 的 /app opsAdmin 组（BrandResolver + StaffAuthRequired），
// 路由用生产同一个注册函数挂上去，所以 RoleRequired 是真的那一道。
func adminFunnelRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	app := r.Group("/app", BrandResolver(), StaffAuthRequired())
	registerAdminFunnelRoutes(app)
	return r
}

func afGet(t *testing.T, r *gin.Engine, key, path string) *TestResponse {
	t.Helper()
	w := NewTestRequest("GET", path).WithHeader("X-Access-Key", key).Execute(r)
	resp, err := ParseResponse(w)
	require.NoError(t, err, "body: %s", w.Body.String())
	return resp
}

func afMarketingKey(t *testing.T) string {
	t.Helper()
	return createAccessKeyUserForTest(t, BrandKaitu, RoleUser|RoleMarketing)
}

// afDay 给每个测试一个历史 UTC 日（2011–2018；funnel_events 与订单在这段时间没有真实数据）。
// 随机日只是降低干扰；断言本身按唯一标记分组（afMarkerSteps），不依赖"这一天只有我"。
func afDay(t *testing.T) time.Time {
	t.Helper()
	off := int(time.Now().UnixNano() % 2900)
	return time.Date(2011, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, off)
}

// afSeedWebPurchase：一个匿名访客（sid）在 day 当天走完 web_purchase 全程：
// pricing_view → plan_select → checkout_start，登录关联到用户，用户有一张已付订单。
// plan_select 照常记录但不是路径的一步（默认选中套餐的人不发它）。
func afSeedWebPurchase(t *testing.T, day time.Time) (marker string) {
	t.Helper()
	marker = generateId("af")
	sid := "sid-" + marker
	user := factsUser(t, BrandKaitu, day.Add(-time.Hour))
	t.Cleanup(func() {
		factsCleanup(t, db.Get().Where("plan = ?", marker).Delete(&FunnelEvent{}).Error)
		factsCleanup(t, db.Get().Where("anon_id = ?", sid).Delete(&FunnelIdentity{}).Error)
	})
	for i, ev := range []string{"pricing_view", "plan_select", "checkout_start"} {
		require.NoError(t, db.Get().Create(&FunnelEvent{
			OccurredAt: day.Add(time.Duration(i+1) * time.Hour), Brand: string(BrandKaitu),
			Surface: FunnelSurfaceWeb, Event: ev, AnonID: sid, Plan: marker, UtmCampaign: marker,
		}).Error)
	}
	require.NoError(t, db.Get().Create(&FunnelIdentity{
		Kind: funnelAnonKindSid, AnonID: sid, UserID: user.ID, Brand: string(BrandKaitu),
	}).Error)
	factsOrder(t, user.ID, OrderChannelNextpay, factsPaid(day.Add(5*time.Hour)))
	return marker
}

func afCounts(t *testing.T, resp *TestResponse) []int {
	t.Helper()
	require.Equal(t, int(ErrorNone), resp.Code, resp.Message)
	var res FunnelResult
	require.NoError(t, json.Unmarshal(resp.Data, &res))
	counts := make([]int, len(res.Steps))
	for i, s := range res.Steps {
		counts[i] = s.Count
	}
	return counts
}

// afMarkerSteps 用 groupBy=utm_campaign 取出唯一标记那一组的各步人数（进入事件的 utm_campaign = 标记）；
// 该组不存在（没有人进入）返回 nil。共享库里别的行落在别的组，不影响断言。
func afMarkerSteps(t *testing.T, r *gin.Engine, key string, day time.Time, marker, extra string) []int {
	t.Helper()
	resp := afGet(t, r, key, afPath("web_purchase", day, "&groupBy=utm_campaign"+extra))
	require.Equal(t, int(ErrorNone), resp.Code, resp.Message)
	var res FunnelResult
	require.NoError(t, json.Unmarshal(resp.Data, &res))
	for _, g := range res.Groups {
		if g.Key == marker {
			return g.Steps
		}
	}
	return nil
}

func afPath(key string, day time.Time, extra string) string {
	d := day.Format("2006-01-02")
	return fmt.Sprintf("/app/stats/funnels/%s?from=%s&to=%s%s", key, d, d, extra)
}

func TestAdminFunnels_List(t *testing.T) {
	skipIfNoConfig(t)
	resp := afGet(t, adminFunnelRouter(), afMarketingKey(t), "/app/stats/funnels")
	require.Equal(t, int(ErrorNone), resp.Code, resp.Message)
	var data struct {
		Paths []struct {
			Key, Title, Question string
			Steps                []string
			WindowHours          *float64
		}
		GroupDims []string
	}
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	require.Len(t, data.Paths, 6)
	var keys, want []string
	for _, p := range data.Paths {
		keys = append(keys, p.Key)
		assert.NotEmpty(t, p.Title, p.Key)
		assert.NotEmpty(t, p.Question, p.Key)
	}
	for _, p := range funnelPathRegistry {
		want = append(want, p.Key)
	}
	assert.Equal(t, want, keys)
	assert.Equal(t, funnelGroupDims, data.GroupDims)

	first := data.Paths[0]
	require.Equal(t, "web_purchase", first.Key)
	assert.Equal(t, []string{"访问", "看定价", "发起支付", "付款"}, first.Steps)
	require.NotNil(t, first.WindowHours)
	assert.InDelta(t, 336, *first.WindowHours, 1e-9)

	// 线上形状：键名是 dashboard / MCP 工具已经在读的那几个。
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resp.Data, &raw))
	assert.Contains(t, raw, "paths")
	assert.Contains(t, raw, "groupDims")
	assert.Contains(t, string(raw["paths"]), `"windowHours":336`)
}

func TestAdminFunnel_EndToEnd(t *testing.T) {
	skipIfNoConfig(t)
	day := afDay(t)
	marker := afSeedWebPurchase(t, day)
	r, key := adminFunnelRouter(), afMarketingKey(t)
	all := []int{1, 1, 1, 1}

	assert.Equal(t, all, afMarkerSteps(t, r, key, day, marker, ""))

	// 不分组的总数：这一天只有本测试的行时恰好是四个 1。
	resp := afGet(t, r, key, afPath("web_purchase", day, ""))
	assert.Equal(t, all, afCounts(t, resp))
	var res FunnelResult
	require.NoError(t, json.Unmarshal(resp.Data, &res))
	require.Len(t, res.Daily, 1, "`to` is inclusive: from == to is exactly one day")
	assert.Equal(t, FunnelDay{Date: day.Format("2006-01-02"), Entered: 1, Completed: 1}, res.Daily[0])
	assert.Empty(t, res.Groups)

	// 进入区间是 [from, to+1 天)：前一天、后一天都不含这批进入事件。
	assert.Nil(t, afMarkerSteps(t, r, key, day.AddDate(0, 0, -1), marker, ""))
	assert.Nil(t, afMarkerSteps(t, r, key, day.AddDate(0, 0, 1), marker, ""))

	resp = afGet(t, r, key, afPath("web_purchase", day, "&groupBy=channel"))
	require.NoError(t, json.Unmarshal(resp.Data, &res))
	assert.Contains(t, res.Groups, FunnelGroup{Key: OrderChannelNextpay, Steps: all})
}

// 回头客：这一天的付款是续费（renewal）而不是首购，末步照样算到达；
// 而且他根本没发过 plan_select（默认选中的套餐）。
func TestAdminFunnel_RenewalCompletesPurchasePath(t *testing.T) {
	skipIfNoConfig(t)
	day := afDay(t)
	marker := generateId("af")
	user := factsUser(t, BrandKaitu, day.AddDate(0, -2, 0))
	t.Cleanup(func() { factsCleanup(t, db.Get().Where("plan = ?", marker).Delete(&FunnelEvent{}).Error) })
	for i, ev := range []string{"pricing_view", "checkout_start"} {
		require.NoError(t, db.Get().Create(&FunnelEvent{
			OccurredAt: day.Add(time.Duration(i+1) * time.Hour), Brand: string(BrandKaitu),
			Surface: FunnelSurfaceWeb, Event: ev, UserID: user.ID, Plan: marker, UtmCampaign: marker,
		}).Error)
	}
	factsOrder(t, user.ID, OrderChannelNextpay, factsPaid(day.AddDate(0, -1, 0))) // 首购，早于区间
	factsOrder(t, user.ID, OrderChannelNextpay, factsPaid(day.Add(5*time.Hour)))  // 续费

	assert.Equal(t, []int{1, 1, 1, 1}, afMarkerSteps(t, adminFunnelRouter(), afMarketingKey(t), day, marker, ""))
}

// 后续步骤的界是"进入时刻 + 时间窗"，不是 `to`：付款落在 `to` 之后几天也要算进来，
// 所以装载区间必须是 [from, to + Window)。
func TestAdminFunnel_LaterStepsLoadedPastTo(t *testing.T) {
	skipIfNoConfig(t)
	day := afDay(t)
	marker := generateId("af")
	user := factsUser(t, BrandKaitu, day.Add(-time.Hour))
	t.Cleanup(func() { factsCleanup(t, db.Get().Where("plan = ?", marker).Delete(&FunnelEvent{}).Error) })
	for i, ev := range []string{"pricing_view", "plan_select", "checkout_start"} {
		require.NoError(t, db.Get().Create(&FunnelEvent{
			OccurredAt: day.Add(time.Hour).AddDate(0, 0, i*2), Brand: string(BrandKaitu),
			Surface: FunnelSurfaceWeb, Event: ev, UserID: user.ID, Plan: marker, UtmCampaign: marker,
		}).Error)
	}
	factsOrder(t, user.ID, OrderChannelNextpay, factsPaid(day.AddDate(0, 0, 10)))

	assert.Equal(t, []int{1, 1, 1, 1}, afMarkerSteps(t, adminFunnelRouter(), afMarketingKey(t), day, marker, ""))
}

func TestAdminFunnel_BrandFilter(t *testing.T) {
	skipIfNoConfig(t)
	day := afDay(t)
	marker := afSeedWebPurchase(t, day)
	r, key := adminFunnelRouter(), afMarketingKey(t)

	assert.Nil(t, afMarkerSteps(t, r, key, day, marker, "&brand=overleap"))
	assert.Equal(t, []int{1, 1, 1, 1}, afMarkerSteps(t, r, key, day, marker, "&brand=kaitu"))
	assert.Equal(t, []int{0, 0, 0, 0}, afCounts(t, afGet(t, r, key, afPath("web_purchase", day, "&brand=overleap"))),
		"nothing of the other brand exists on this historical day")
}

func TestAdminFunnel_BadInput(t *testing.T) {
	skipIfNoConfig(t)
	r, key := adminFunnelRouter(), afMarketingKey(t)
	cases := []struct {
		name, path string
		code       ErrorCode
	}{
		{"unknown key", "/app/stats/funnels/no_such_path", ErrorNotFound},
		{"91 days", "/app/stats/funnels/web_purchase?from=2015-01-01&to=2015-04-01", ErrorInvalidArgument},
		{"from after to", "/app/stats/funnels/web_purchase?from=2015-02-02&to=2015-02-01", ErrorInvalidArgument},
		{"bad from", "/app/stats/funnels/web_purchase?from=2015-2-2&to=2015-02-03", ErrorInvalidArgument},
		{"bad to", "/app/stats/funnels/web_purchase?from=2015-02-02&to=tomorrow", ErrorInvalidArgument},
		{"datetime not date", "/app/stats/funnels/web_purchase?from=2015-02-02T00:00:00Z&to=2015-02-03", ErrorInvalidArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, int(tc.code), afGet(t, r, key, tc.path).Code)
		})
	}

	// 含首尾 90 天（dashboard 的"90 天"选项：from = today − 89, to = today）必须放行。
	resp := afGet(t, r, key, "/app/stats/funnels/web_purchase?from=2015-01-01&to=2015-03-31")
	require.Equal(t, int(ErrorNone), resp.Code, resp.Message)
	var res FunnelResult
	require.NoError(t, json.Unmarshal(resp.Data, &res))
	assert.Len(t, res.Daily, 90)
}

func TestAdminFunnel_DefaultRangeIsLast30Days(t *testing.T) {
	skipIfNoConfig(t)
	resp := afGet(t, adminFunnelRouter(), afMarketingKey(t), "/app/stats/funnels/web_install")
	require.Equal(t, int(ErrorNone), resp.Code, resp.Message)
	var res FunnelResult
	require.NoError(t, json.Unmarshal(resp.Data, &res))
	require.Len(t, res.Daily, 30)
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	assert.Contains(t, []string{today, yesterday}, res.Daily[29].Date) // 跨 UTC 零点的那一瞬间容一天
}

func TestAdminFunnel_RangeTooLarge(t *testing.T) {
	skipIfNoConfig(t)
	day := afDay(t)
	afSeedWebPurchase(t, day)
	// 上限按这次查询实际会数到的行数来设（装载区间 = 当天 + 时间窗），不假设区间里只有本测试的行。
	path, ok := funnelPathByKey("web_purchase")
	require.True(t, ok)
	behaviors, _ := path.eventNames()
	n, err := countFunnelEvents(context.Background(), BrandKaitu, false, day, day.AddDate(0, 0, 1).Add(path.Window), behaviors)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(2))
	old := funnelQueryMaxEvents
	t.Cleanup(func() { funnelQueryMaxEvents = old })
	r, key := adminFunnelRouter(), afMarketingKey(t)

	funnelQueryMaxEvents = n - 1
	resp := afGet(t, r, key, afPath("web_purchase", day, ""))
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Equal(t, funnelRangeTooLargeMsg, resp.Message)
	assert.Contains(t, resp.Message, "range too large")
	assert.Contains(t, resp.Message, "shorten the date range", "the message must tell the operator what to do")

	funnelQueryMaxEvents = n // 恰好等于上限不算超
	resp = afGet(t, r, key, afPath("web_purchase", day, ""))
	assert.Equal(t, int(ErrorNone), resp.Code, resp.Message)
}

func TestAdminFunnel_MaxEventsDefault(t *testing.T) {
	assert.EqualValues(t, 500_000, funnelQueryMaxEvents)
	assert.EqualValues(t, 2_000_000, funnelActiveMaxOpens)
}

// 装载器自己也守上限（count 与 load 之间新写入的行不能把内存顶穿）：流式读到超过上限就停。
func TestLoadFunnelEvents_StopsPastCap(t *testing.T) {
	skipIfNoConfig(t)
	day := afDay(t)
	afSeedWebPurchase(t, day) // 3 行行为事件
	names := []string{"pricing_view", "plan_select", "checkout_start"}
	ctx := context.Background()
	n, err := countFunnelEvents(ctx, BrandKaitu, false, day, day.AddDate(0, 0, 1), names)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(3))
	old := funnelQueryMaxEvents
	t.Cleanup(func() { funnelQueryMaxEvents = old })

	funnelQueryMaxEvents = n - 1
	recs, err := loadFunnelEvents(ctx, BrandKaitu, false, day, day.AddDate(0, 0, 1), names)
	assert.ErrorIs(t, err, errFunnelRangeTooLarge)
	assert.Nil(t, recs)

	funnelQueryMaxEvents = n
	recs, err = loadFunnelEvents(ctx, BrandKaitu, false, day, day.AddDate(0, 0, 1), names)
	require.NoError(t, err)
	assert.Len(t, recs, int(n))
}

// 活跃留存：区间内的打开记录数超过上限 → 同一个 "range too large" 错误，不装载。
func TestAdminRetention_ActiveOpensCapped(t *testing.T) {
	skipIfNoConfig(t)
	hash := generateId("af-cap")
	t.Cleanup(func() { factsCleanup(t, db.Get().Where("device_hash = ?", hash).Delete(&StatAppOpen{}).Error) })
	at := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Get().Create(&StatAppOpen{
			DeviceHash: hash, OS: "test", AppVersion: "0", Brand: "overleap", CreatedAt: at, ReportedAt: at,
		}).Error)
	}
	old := funnelActiveMaxOpens
	t.Cleanup(func() { funnelActiveMaxOpens = old })
	r, key := adminFunnelRouter(), afMarketingKey(t)

	funnelActiveMaxOpens = 1
	resp := afGet(t, r, key, "/app/stats/retention?metric=active&brand=overleap")
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Equal(t, funnelRangeTooLargeMsg, resp.Message)

	since := funnelUTCDay(time.Now()).AddDate(0, 0, -(activeCohortDays - 1))
	_, err := loadActiveOpens(context.Background(), BrandOverleap, true, since)
	assert.ErrorIs(t, err, errFunnelRangeTooLarge)

	funnelActiveMaxOpens = old
	resp = afGet(t, r, key, "/app/stats/retention?metric=active&brand=overleap")
	assert.Equal(t, int(ErrorNone), resp.Code, resp.Message)
}

func TestAdminFunnel_RequiresMarketingRole(t *testing.T) {
	skipIfNoConfig(t)
	r := adminFunnelRouter()
	plain := createAccessKeyUserForTest(t, BrandKaitu, RoleUser)
	support := createAccessKeyUserForTest(t, BrandKaitu, RoleUser|RoleSupport)
	marketing := afMarketingKey(t)
	paths := []string{
		"/app/stats/funnels",
		"/app/stats/funnels/web_purchase?from=2015-02-02&to=2015-02-02",
		"/app/stats/retention?metric=active",
	}
	for _, p := range paths {
		assert.Equal(t, int(ErrorForbidden), afGet(t, r, plain, p).Code, "plain user %s", p)
		assert.Equal(t, int(ErrorForbidden), afGet(t, r, support, p).Code, "other staff role %s", p)
		assert.Equal(t, int(ErrorNone), afGet(t, r, marketing, p).Code, "marketing %s", p)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		anon, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, int(ErrorNotLogin), anon.Code, "anonymous %s", p)
	}
}

// 接线守卫：生产路由把这组路由挂在 opsAdmin 组上（测试路由器用的是同一个注册函数）。
func TestAdminFunnel_RoutesWiredIntoOpsAdmin(t *testing.T) {
	src, err := os.ReadFile("route.go")
	require.NoError(t, err)
	assert.Contains(t, string(src), "registerAdminFunnelRoutes(opsAdmin)")
}

func TestAdminRetention_Paid(t *testing.T) {
	skipIfNoConfig(t)
	// 首付就在此刻的一个用户。查最近 2 个月：即使测试与 handler 之间跨了 UTC 月界，这个群组也在范围内。
	paidAt := time.Now().UTC().Truncate(time.Second)
	user := factsUser(t, BrandOverleap, paidAt.Add(-time.Hour))
	c := &SubscriptionCredit{
		CreatedAt: paidAt, UserID: user.ID, Provider: SubscriptionProviderStripe, Kind: "purchase",
		TransactionID: uuid.NewString(), CreditedSeconds: 3600,
	}
	require.NoError(t, db.Get().Create(c).Error)
	t.Cleanup(func() { factsCleanup(t, db.Get().Unscoped().Delete(c).Error) })

	resp := afGet(t, adminFunnelRouter(), afMarketingKey(t), "/app/stats/retention?metric=paid&months=2&brand=overleap")
	require.Equal(t, int(ErrorNone), resp.Code, resp.Message)
	var data struct {
		Rows []PaidCohortRow `json:"rows"`
		Note string          `json:"note"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	var row *PaidCohortRow
	for i := range data.Rows {
		if data.Rows[i].Cohort == paidAt.Format("2006-01") {
			row = &data.Rows[i]
		}
	}
	require.NotNil(t, row, "cohort %s missing in %+v", paidAt.Format("2006-01"), data.Rows)
	assert.GreaterOrEqual(t, row.Size, 1)
	for _, k := range []string{"m1", "m3", "m6", "m12"} {
		v, ok := row.Retained[k]
		assert.True(t, ok, k)
		assert.Nil(t, v, "%s checkpoint of a payment made just now is in the future", k)
	}
	assert.Contains(t, data.Note, "付款记录")
	assert.Contains(t, data.Note, "7 天宽限")
	assert.Contains(t, data.Note, "退款")
	assert.Contains(t, data.Note, "订阅")
}

func TestAdminRetention_Active(t *testing.T) {
	skipIfNoConfig(t)
	hash := generateId("af-open")
	t.Cleanup(func() { factsCleanup(t, db.Get().Where("device_hash = ?", hash).Delete(&StatAppOpen{}).Error) })
	first := funnelUTCDay(time.Now()).AddDate(0, 0, -5).Add(3 * time.Hour)
	for _, at := range []time.Time{first, first.AddDate(0, 0, 1)} {
		require.NoError(t, db.Get().Create(&StatAppOpen{
			DeviceHash: hash, OS: "test", AppVersion: "0", Brand: "overleap", CreatedAt: at, ReportedAt: at,
		}).Error)
	}
	r, key := adminFunnelRouter(), afMarketingKey(t)

	type activeData struct {
		Rows []ActiveCohortRow `json:"rows"`
		Note *string           `json:"note"`
	}
	find := func(path string) (*ActiveCohortRow, activeData) {
		t.Helper()
		resp := afGet(t, r, key, path)
		require.Equal(t, int(ErrorNone), resp.Code, resp.Message)
		assert.NotContains(t, string(resp.Data), `"rows":null`)
		var data activeData
		require.NoError(t, json.Unmarshal(resp.Data, &data))
		for i := range data.Rows {
			if data.Rows[i].Cohort == first.Format("2006-01-02") {
				return &data.Rows[i], data
			}
		}
		return nil, data
	}

	row, data := find("/app/stats/retention?metric=active&brand=overleap")
	require.NotNil(t, row)
	assert.GreaterOrEqual(t, row.Size, 1)
	require.NotNil(t, row.D1)
	assert.Greater(t, *row.D1, 0.0)
	assert.Nil(t, row.D7, "D+7 is in the future")
	require.NotNil(t, data.Note)
	assert.Contains(t, *data.Note, "品牌")
	assert.Contains(t, *data.Note, "老设备")

	_, data = find("/app/stats/retention?metric=active")
	assert.Nil(t, data.Note, "no brand filter → no note key")
}

func TestAdminRetention_BadInput(t *testing.T) {
	skipIfNoConfig(t)
	r, key := adminFunnelRouter(), afMarketingKey(t)
	for _, q := range []string{"metric=weekly", "metric=paid&months=0", "metric=paid&months=25", "metric=paid&months=abc", ""} {
		assert.Equal(t, int(ErrorInvalidArgument), afGet(t, r, key, "/app/stats/retention?"+q).Code, q)
	}
}

// 说明文字里不出现任何品牌词。
func TestAdminRetention_NotesCarryNoBrandWord(t *testing.T) {
	for _, note := range []string{retentionNotePaid, retentionNoteActiveBrand} {
		assert.NotEmpty(t, note)
		assert.NotRegexp(t, `(?i)kaitu|overleap|开途|stripe`, note)
	}
}
