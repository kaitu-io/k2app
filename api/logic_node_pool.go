package center

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
)

// nodeAllowedForUser 是体验线路隔离（spec §3.7）的唯一判定：试用用户只能
// 拿到、只能连 pool=trial 的节点；其他人（含付费用户）不受池限制。
//
// 「试用用户」= InTrial 且从未付过费。IsFirstOrderDone=true 的人即使 InTrial
// 被置上（理论上入账时就不会置，见 §3.7），也绝不降级到体验线路——付过费的人
// 换个 Apple ID 开试用不能变成二等用户。nil user 放行：调用方各自已有鉴权，
// 这里只做池过滤，不替它们判断"没登录"。
func nodeAllowedForUser(node *SlaveNode, user *User) bool {
	if user == nil || !user.InTrial || user.IsVip() {
		return true
	}
	return node.Pool == NodePoolTrial
}

// LockedTunnelGroup 是试用用户看到的「锁定的会员线路」摘要：只有国家和台数，
// 绝不携带域名 / IP / serverUrl（会员线路地址不下发给试用用户）。
type LockedTunnelGroup struct {
	Country string `json:"country"`
	Count   int    `json:"count"`
}

// lockedSummaryCounter 按国家统计被池规则挡掉的节点（按节点去重：一台节点
// 多条隧道只算一台）。
type lockedSummaryCounter struct {
	seen    map[uint64]bool
	byCount map[string]int
}

func newLockedSummaryCounter() *lockedSummaryCounter {
	return &lockedSummaryCounter{seen: map[uint64]bool{}, byCount: map[string]int{}}
}

func (l *lockedSummaryCounter) add(node *SlaveNode) {
	if l.seen[node.ID] {
		return
	}
	l.seen[node.ID] = true
	l.byCount[node.Country]++
}

// groups 返回按国家排序的摘要；没有被挡的节点时返回 nil（配合 omitempty，
// 非试用用户的响应里不出现该字段）。
func (l *lockedSummaryCounter) groups() []LockedTunnelGroup {
	if len(l.byCount) == 0 {
		return nil
	}
	out := make([]LockedTunnelGroup, 0, len(l.byCount))
	for country, n := range l.byCount {
		out = append(out, LockedTunnelGroup{Country: country, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Country < out[j].Country })
	return out
}

// ---------- 体验线路为空告警 ----------

// trialPoolEmptyAlertWindow 同一品牌「试用用户拿到空列表」告警的最短间隔。
const trialPoolEmptyAlertWindow = time.Hour

// alertTrialPoolEmpty 发 Slack 告警。var 形态供测试替换（同 alertPaymentAnomaly）。
var alertTrialPoolEmpty = func(ctx context.Context, brand Brand, lockedNodes int) {
	msg := fmt.Sprintf("[TRIAL-POOL-EMPTY] 品牌 %s：试用用户的线路列表为空（另有 %d 台会员节点被体验线路规则挡掉）——"+
		"检查体验线路节点是否在线、.env 是否写了 K2_NODE_POOL=trial（节点重注册以它为准）", brand, lockedNodes)
	log.Errorf(ctx, "%s", msg)
	if err := slack.Send("alert", msg); err != nil {
		log.Errorf(ctx, "failed to send slack alert [TRIAL-POOL-EMPTY]: %v", err)
	}
}

// trialPoolEmptyAlerts 进程内按品牌去重（多实例各自至多每小时一条，可接受）。
var trialPoolEmptyAlerts = struct {
	mu   sync.Mutex
	last map[Brand]time.Time
}{last: map[Brand]time.Time{}}

// shouldAlertTrialPoolEmpty 纯判定：这次响应是否属于「被池规则限制的试用用户拿到了空列表」。
// 非试用用户（含付过费又开试用的人）列表为空是别的问题，不走这条告警。
func shouldAlertTrialPoolEmpty(user *User, items int) bool {
	return items == 0 && user != nil && !nodeAllowedForUser(&SlaveNode{Pool: ""}, user)
}

// maybeAlertTrialPoolEmpty 在 /api/v20260717/tunnels 末尾调用：命中判定且该品牌一小时内没告过警时
// 异步发一条（不拖慢列表响应）。返回是否发出，供测试断言。
func maybeAlertTrialPoolEmpty(user *User, items, lockedNodes int, now time.Time) bool {
	if !shouldAlertTrialPoolEmpty(user, items) {
		return false
	}
	brand := Brand(user.Brand)
	trialPoolEmptyAlerts.mu.Lock()
	if last, ok := trialPoolEmptyAlerts.last[brand]; ok && now.Sub(last) < trialPoolEmptyAlertWindow {
		trialPoolEmptyAlerts.mu.Unlock()
		return false
	}
	trialPoolEmptyAlerts.last[brand] = now
	trialPoolEmptyAlerts.mu.Unlock()
	// 不传请求的 gin.Context：gin 会回收复用它，handler 返回后在 goroutine 里读就是数据竞争。
	go alertTrialPoolEmpty(context.Background(), brand, lockedNodes)
	return true
}
