package center

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// NodeDistributionItem is one node with its live placement state. Load fields
// are nil when the node has no fresh trustworthy sample (old sidecar or silent
// node); nil must never be read as zero.
type NodeDistributionItem struct {
	AdminNodeItem
	Status         string   `json:"status"`
	ActiveDevices  int      `json:"activeDevices"`
	ActiveSessions int      `json:"activeSessions"`
	PeakDevices24h int      `json:"peakDevices24h"`
	CPUPercent     *float64 `json:"cpuPercent"`
	StealPercent   *float64 `json:"stealPercent"`
	CPUCores       int      `json:"cpuCores"`
	MemoryPercent  *float64 `json:"memoryPercent"`
	BandwidthMbps  *float64 `json:"bandwidthMbps"`
	CapacityMbps   *float64 `json:"capacityMbps"`
	PressureRatio  *float64 `json:"pressureRatio"`
	MetricsVersion int      `json:"metricsVersion"`
	MetricsAt      int64    `json:"metricsAt"`
	UsageReportAt  int64    `json:"usageReportAt"`
	UsedBytes      int64    `json:"usedBytes"`
	QuotaBytes     int64    `json:"quotaBytes"`
}

// NodeDistributionSummary is the balance of one scope ("all" or a country).
type NodeDistributionSummary struct {
	Scope           string  `json:"scope"`
	Nodes           int     `json:"nodes"`
	EligibleNodes   int     `json:"eligibleNodes"`
	IdleNodes       int     `json:"idleNodes"`
	OverloadedNodes int     `json:"overloadedNodes"`
	ActiveDevices   int     `json:"activeDevices"`
	MaxDevices      int     `json:"maxDevices"`
	MaxNode         string  `json:"maxNode"`
	MedianDevices   float64 `json:"medianDevices"`
	TopShare        float64 `json:"topShare"`
	Verdict         string  `json:"verdict"`
}

// NodeDistributionResponse is GET /app/nodes/distribution.
type NodeDistributionResponse struct {
	GeneratedAt   int64                     `json:"generatedAt"`
	WindowSeconds int                       `json:"windowSeconds"`
	Summaries     []NodeDistributionSummary `json:"summaries"`
	Items         []NodeDistributionItem    `json:"items"`
}

// NodeDistributionTrendPoint is one completed 5-minute bucket.
type NodeDistributionTrendPoint struct {
	Bucket        int64   `json:"bucket"`
	ActiveDevices int     `json:"activeDevices"`
	ActiveNodes   int     `json:"activeNodes"`
	NodesInScope  int     `json:"nodesInScope"`
	MaxDevices    int     `json:"maxDevices"`
	MaxNode       string  `json:"maxNode"`
	TopShare      float64 `json:"topShare"`
}

// NodeDistributionTrendResponse is GET /app/nodes/distribution/trend.
type NodeDistributionTrendResponse struct {
	Hours  int                          `json:"hours"`
	Points []NodeDistributionTrendPoint `json:"points"`
}

func float64Ptr(v float64) *float64 { return &v }

func loadDistributionNodes(c *gin.Context) ([]SlaveNode, bool) {
	var nodes []SlaveNode
	if err := db.Get().Preload("Tunnels").Order("name ASC").Find(&nodes).Error; err != nil {
		log.Errorf(c, "failed to load nodes: %v", err)
		Error(c, ErrorSystemError, "failed to load nodes")
		return nil, false
	}
	brand, hasBrand := parseBrandFilter(c.Query("brand"))
	if !hasBrand {
		return nodes, true
	}
	out := make([]SlaveNode, 0, len(nodes))
	for _, n := range nodes {
		if n.VisibleTo(brand) {
			out = append(out, n)
		}
	}
	return out, true
}

func buildNodeDistributionItem(n SlaveNode, u *NodeUsage, load *SlaveNodeLoad, act nodeActivity, peak int, now int64) NodeDistributionItem {
	item := NodeDistributionItem{
		AdminNodeItem:  buildAdminNodeItem(n),
		ActiveDevices:  act.Devices,
		ActiveSessions: act.Sessions,
		PeakDevices24h: peak,
	}
	in := nodeStatusInput{
		Private:       n.Class == "private",
		Visible:       n.VisibleTo(BrandKaitu) || n.VisibleTo(BrandOverleap),
		Offline:       isNodeOffline(u, now),
		OverQuota:     isNodeOverQuota(u),
		ActiveDevices: act.Devices,
	}
	if u != nil {
		item.UsageReportAt = u.LastReportAt
		item.UsedBytes = u.UsedBytes
		item.QuotaBytes = u.QuotaTotalBytes
		if u.CapacityMbps > 0 {
			item.CapacityMbps = float64Ptr(u.CapacityMbps)
		}
		if u.PressureAt > 0 && now-u.PressureAt <= pressureStaleSeconds {
			item.PressureRatio = float64Ptr(u.PressureRatio)
			in.PressureFresh = true
			in.Pressure = u.PressureRatio
		}
	}
	if load != nil {
		item.MetricsVersion = load.MetricsVersion
		item.MetricsAt = load.CreatedAt.Unix()
		item.MemoryPercent = float64Ptr(load.MemoryUsagePercent)
		if load.MetricsVersion >= 1 {
			item.CPUPercent = float64Ptr(load.CPUPercent)
			item.StealPercent = float64Ptr(load.StealPercent)
			item.CPUCores = load.CPUCores
			item.BandwidthMbps = float64Ptr(load.BandwidthUpMbps + load.BandwidthDownMbps)
		}
	}
	item.Status = classifyNodeStatus(in)
	return item
}

// api_admin_node_distribution returns every node with live device counts, load
// and balance verdicts. Not paginated: balance is a whole-fleet question.
func api_admin_node_distribution(c *gin.Context) {
	nodes, ok := loadDistributionNodes(c)
	if !ok {
		return
	}
	now := time.Now().Unix()
	ips := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ips = append(ips, n.Ipv4)
	}

	activity, err := loadNodeActivity(now)
	if err != nil {
		log.Errorf(c, "failed to load node activity: %v", err)
		Error(c, ErrorSystemError, "failed to load node activity")
		return
	}
	peaks, err := loadNodePeaks(now)
	if err != nil {
		log.Errorf(c, "failed to load node peaks: %v", err)
		Error(c, ErrorSystemError, "failed to load node activity")
		return
	}
	metrics, err := loadLatestNodeMetrics(ips, now)
	if err != nil {
		log.Errorf(c, "failed to load node metrics: %v", err)
		Error(c, ErrorSystemError, "failed to load node metrics")
		return
	}
	usages := getNodeUsagesByIPs(ips)

	items := make([]NodeDistributionItem, 0, len(nodes))
	for _, n := range nodes {
		var load *SlaveNodeLoad
		if l, ok := metrics[n.Ipv4]; ok {
			load = &l
		}
		items = append(items, buildNodeDistributionItem(n, usages[n.Ipv4], load, activity[n.Ipv4], peaks[n.Ipv4], now))
	}
	Success(c, &NodeDistributionResponse{
		GeneratedAt:   now,
		WindowSeconds: nodePresenceWindowSeconds,
		Summaries:     summarizeNodeDistribution(items),
		Items:         items,
	})
}

// api_admin_node_distribution_trend returns balance over time so a client
// strategy can be compared before and after release. ?hours= 1-168, default 24.
// Scope is the shared nodes matching ?brand= and ?country=, as of now.
func api_admin_node_distribution_trend(c *gin.Context) {
	hours, err := strconv.Atoi(c.DefaultQuery("hours", "24"))
	if err != nil || hours < 1 {
		hours = 24
	}
	if hours > 168 {
		hours = 168
	}
	nodes, ok := loadDistributionNodes(c)
	if !ok {
		return
	}
	country := c.Query("country")
	names := make(map[string]string, len(nodes))
	for _, n := range nodes {
		if n.Class == "private" || (country != "" && n.Country != country) {
			continue
		}
		names[n.Ipv4] = n.Name
	}
	now := time.Now().Unix()
	key := fmt.Sprintf("node_dist:trend:%d:%s:%s:%d", hours, c.Query("brand"), country, nodePresenceBucket(now))
	points, err := cachedForBucket(key, func() ([]NodeDistributionTrendPoint, error) {
		rows, err := loadNodeBucketCounts(now-int64(hours)*3600, now)
		if err != nil {
			return nil, err
		}
		return buildNodeTrend(rows, names), nil
	})
	if err != nil {
		log.Errorf(c, "failed to load node trend: %v", err)
		Error(c, ErrorSystemError, "failed to load node trend")
		return
	}
	if points == nil {
		points = []NodeDistributionTrendPoint{}
	}
	Success(c, &NodeDistributionTrendResponse{Hours: hours, Points: points})
}
