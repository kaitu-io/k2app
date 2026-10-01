package center

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// 转化漏斗 / 留存的管理端只读接口。品牌只是 ?brand= 查询参数（空 / 非法 = 全部品牌）。

const (
	funnelQueryDateForm    = "2006-01-02"
	funnelQueryDefaultDays = 30
	funnelQueryMaxDays     = 90 // 含首尾
)

// funnelQueryMaxEvents：一次查询最多装载的行为事件数（装载前先 count）。变量仅为测试调低。
var funnelQueryMaxEvents int64 = 2_000_000

const (
	retentionNotePaid        = "留存 = 到检查点时仍在付费覆盖期内，按付款记录推算（赠送、试用的时长不计）；检查点为首次付款满 N 个月后再加 7 天宽限；已退款的订单不提供覆盖；银行卡自动续费订阅渠道的退款暂未反映。"
	retentionNoteActiveBrand = "按品牌筛选时：品牌列上线之前就有记录的设备按老设备识别（不算新设备）；那之前的打开记录不归属任何品牌，不计入。"
)

// registerAdminFunnelRoutes 挂到 /app opsAdmin 组（StaffAuthRequired 之后）。
func registerAdminFunnelRoutes(g *gin.RouterGroup) {
	g.GET("/stats/funnels", RoleRequired(RoleMarketing), api_admin_list_funnels)
	g.GET("/stats/funnels/:key", RoleRequired(RoleMarketing), api_admin_get_funnel)
	g.GET("/stats/retention", RoleRequired(RoleMarketing), api_admin_get_retention)
}

type AdminFunnelPath struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Question    string   `json:"question"`
	Steps       []string `json:"steps"`
	WindowHours float64  `json:"windowHours"`
}

type AdminFunnelListResponse struct {
	Paths     []AdminFunnelPath `json:"paths"`
	GroupDims []string          `json:"groupDims"`
}

type AdminRetentionResponse struct {
	Rows any    `json:"rows"` // []PaidCohortRow 或 []ActiveCohortRow，永不为 null
	Note string `json:"note,omitempty"`
}

// api_admin_list_funnels GET /app/stats/funnels
func api_admin_list_funnels(c *gin.Context) {
	resp := AdminFunnelListResponse{Paths: make([]AdminFunnelPath, 0, len(funnelPathRegistry)), GroupDims: funnelGroupDims}
	for _, p := range funnelPathRegistry {
		labels := make([]string, 0, len(p.Steps))
		for _, s := range p.Steps {
			labels = append(labels, s.Label)
		}
		resp.Paths = append(resp.Paths, AdminFunnelPath{
			Key: p.Key, Title: p.Title, Question: p.Question, Steps: labels, WindowHours: p.Window.Hours(),
		})
	}
	Success(c, &resp)
}

// parseFunnelDateRange 解析 from / to（UTC 日，to 含当日），返回进入区间 [from, to)。
// 缺省 = 截至今天（含）的最近 30 天；只给一端时另一端按 30 天补齐。
func parseFunnelDateRange(rawFrom, rawTo string, now time.Time) (from, to time.Time, ok bool) {
	parse := func(raw string) (time.Time, bool) {
		t, err := time.ParseInLocation(funnelQueryDateForm, raw, time.UTC)
		return t, err == nil
	}
	var lastDay time.Time
	if rawTo == "" {
		lastDay = funnelUTCDay(now)
	} else if lastDay, ok = parse(rawTo); !ok {
		return from, to, false
	}
	if rawFrom == "" {
		from = lastDay.AddDate(0, 0, -(funnelQueryDefaultDays - 1))
		if rawTo == "" {
			return from, lastDay.AddDate(0, 0, 1), true
		}
	} else if from, ok = parse(rawFrom); !ok {
		return from, to, false
	}
	if rawFrom != "" && rawTo == "" {
		// 只给了 from：to = from 起 30 天与今天之间较早的那个，保证区间合法。
		if end := from.AddDate(0, 0, funnelQueryDefaultDays-1); end.Before(lastDay) {
			lastDay = end
		}
	}
	if from.After(lastDay) {
		return from, to, false
	}
	to = lastDay.AddDate(0, 0, 1)
	if to.Sub(from) > funnelQueryMaxDays*funnelDay {
		return from, to, false
	}
	return from, to, true
}

// api_admin_get_funnel GET /app/stats/funnels/:key?brand=&from=&to=&groupBy=
func api_admin_get_funnel(c *gin.Context) {
	path, ok := funnelPathByKey(c.Param("key"))
	if !ok {
		Error(c, ErrorNotFound, "funnel path not found")
		return
	}
	from, to, ok := parseFunnelDateRange(c.Query("from"), c.Query("to"), time.Now())
	if !ok {
		Error(c, ErrorInvalidArgument, "invalid date range")
		return
	}
	brand, hasBrand := parseBrandFilter(c.Query("brand"))

	// 后续步骤的界是「进入时刻 + 时间窗」而不是 to：装载区间要多带一个时间窗。
	loadTo := to.Add(path.Window)
	behaviors, facts := path.eventNames()

	n, err := countFunnelEvents(c, brand, hasBrand, from, loadTo, behaviors)
	if err != nil {
		log.Errorf(c, "failed to count funnel events for %s: %v", path.Key, err)
		Error(c, ErrorSystemError, "failed to compute funnel")
		return
	}
	if n > funnelQueryMaxEvents {
		log.Warnf(c, "funnel %s range too large: %d events", path.Key, n)
		Error(c, ErrorInvalidArgument, "range too large")
		return
	}
	recs, err := loadFunnelEvents(c, brand, hasBrand, from, loadTo, behaviors)
	if err != nil {
		log.Errorf(c, "failed to load funnel events for %s: %v", path.Key, err)
		Error(c, ErrorSystemError, "failed to compute funnel")
		return
	}
	// 身份映射只需要行为事件里的匿名身份（事实没有匿名身份）。
	identities, err := loadFunnelIdentities(c, recs)
	if err != nil {
		log.Errorf(c, "failed to load funnel identities for %s: %v", path.Key, err)
		Error(c, ErrorSystemError, "failed to compute funnel")
		return
	}
	factRecs, err := loadFunnelFacts(c, brand, hasBrand, from, loadTo, facts)
	if err != nil {
		log.Errorf(c, "failed to load funnel facts for %s: %v", path.Key, err)
		Error(c, ErrorSystemError, "failed to compute funnel")
		return
	}
	// recs 覆盖的是装载区间 [from, to+Window)，比进入区间多一个时间窗；进入区间由 computeFunnel 的 from/to 限定。
	recs = append(recs, factRecs...)

	res := computeFunnel(path, recs, identities, from, to, c.Query("groupBy"))
	Success(c, &res)
}

// api_admin_get_retention GET /app/stats/retention?brand=&metric=paid|active&months=
func api_admin_get_retention(c *gin.Context) {
	brand, hasBrand := parseBrandFilter(c.Query("brand"))
	now := time.Now()

	switch c.Query("metric") {
	case "paid":
		months := retentionMonthsDefault
		if raw := c.Query("months"); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < retentionMonthsMin || v > retentionMonthsMax {
				Error(c, ErrorInvalidArgument, "months must be 1-24")
				return
			}
			months = v
		}
		from, to := paidCohortRange(now, months)
		payments, refunded, err := loadPaidCohortInputs(c, brand, hasBrand, from, to, now)
		if err != nil {
			log.Errorf(c, "failed to load paid cohorts: %v", err)
			Error(c, ErrorSystemError, "failed to compute retention")
			return
		}
		Success(c, &AdminRetentionResponse{
			Rows: computePaidCohorts(payments, refunded, now),
			Note: retentionNotePaid,
		})
	case "active":
		since := funnelUTCDay(now).AddDate(0, 0, -(activeCohortDays - 1))
		opens, err := loadActiveOpens(c, brand, hasBrand, since)
		if err != nil {
			log.Errorf(c, "failed to load active cohorts: %v", err)
			Error(c, ErrorSystemError, "failed to compute retention")
			return
		}
		resp := AdminRetentionResponse{Rows: computeActiveCohorts(opens, now)}
		if hasBrand {
			resp.Note = retentionNoteActiveBrand
		}
		Success(c, &resp)
	default:
		Error(c, ErrorInvalidArgument, "metric must be paid or active")
	}
}
