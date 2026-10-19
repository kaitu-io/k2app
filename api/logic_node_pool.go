package center

import "sort"

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
