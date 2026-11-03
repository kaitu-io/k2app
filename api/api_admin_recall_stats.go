package center

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// api_admin_recall_stats GET /app/recall-stats?days=14&since=2026-10-01
// 召回效果：未付款 / 过期 / 试用三条召回链路，召回组 vs 对照组在 days 天内的付款率（logic_recall_stats.go）。
// days 默认 14（1~60）；since 默认 90 天前（YYYY-MM-DD，北京时间零点）。
func api_admin_recall_stats(c *gin.Context) {
	days := 14
	if v := c.Query("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 60 {
			Error(c, ErrorInvalidArgument, "days must be 1..60")
			return
		}
		days = n
	}
	since := time.Now().AddDate(0, 0, -90)
	if v := c.Query("since"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, trialRecallLocation())
		if err != nil {
			Error(c, ErrorInvalidArgument, "since must be YYYY-MM-DD")
			return
		}
		since = t
	}
	stats, err := loadRecallStats(c, since, days)
	if err != nil {
		log.Errorf(c, "recall stats failed: %v", err)
		Error(c, ErrorSystemError, "failed to load recall stats")
		return
	}
	Success(c, &stats)
}
