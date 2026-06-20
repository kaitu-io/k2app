package center

import (
	"context"

	db "github.com/wordgate/qtoolkit/db"
	log "github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// HasActivePrivateLines 报告该用户是否拥有至少一条当前可服务的专属线路
// （active/grace 且在宽限窗口内）。这是路由器（网关）准入的**单一**信号 ——
// 取代旧的 App tier MaxRouterDevice 闸门。语义与 ResolveGatewayPrivateTunnels
// 一致（active+grace 经 IsServiceable 过滤）。now 为 Unix 秒。tx 透传调用方事务。
func HasActivePrivateLines(ctx context.Context, tx *gorm.DB, userID uint64, now int64) (bool, error) {
	var subs []PrivateNodeSubscription
	if err := tx.WithContext(ctx).
		Where("user_id = ? AND status IN ?", userID, []string{PNStatusActive, PNStatusGrace}).
		Find(&subs).Error; err != nil {
		return false, err
	}
	for i := range subs {
		if subs[i].IsServiceable(now) {
			return true, nil
		}
	}
	return false, nil
}

// ResolveGatewayPrivateTunnels 返回 gateway（路由器）用户**可服务**的专属节点隧道。
// 能力矩阵访问侧：路由器只能访问自己拥有、且专属订阅处于可服务态（active/宽限期内）
// 的专属节点。共享池隧道永不出现在此结果中。返回值与共享池 fetchK2V5Tunnels 同构
// （[]SlaveTunnel，Preload Node，仅 k2v5），便于复用 injectSubsCreds。now 为 Unix 秒。
func ResolveGatewayPrivateTunnels(ctx context.Context, userID uint64, now int64) ([]SlaveTunnel, error) {
	var subs []PrivateNodeSubscription
	if err := db.Get().WithContext(ctx).
		Where("user_id = ? AND status IN ?", userID, []string{PNStatusActive, PNStatusGrace}).
		Find(&subs).Error; err != nil {
		return nil, err
	}

	// Collect serviceable, node-bound subs. Tunnel fetch keys by SlaveNodeID
	// (the FK on SlaveTunnel); usage/exhaustion keys by BoundIpv4 (durable —
	// SlaveNode.ID churns on re-registration, see NodeUsage ipv4 re-key).
	nodeIDs := make([]uint64, 0, len(subs))
	idToIP := make(map[uint64]string, len(subs))
	nodeIPs := make([]string, 0, len(subs))
	for i := range subs {
		if subs[i].IsServiceable(now) && subs[i].SlaveNodeID != nil {
			nodeIDs = append(nodeIDs, *subs[i].SlaveNodeID)
			idToIP[*subs[i].SlaveNodeID] = subs[i].BoundIpv4
			if subs[i].BoundIpv4 != "" {
				nodeIPs = append(nodeIPs, subs[i].BoundIpv4)
			}
		}
	}
	if len(nodeIDs) == 0 {
		return []SlaveTunnel{}, nil
	}
	usageMap := getNodeUsagesByIPs(nodeIPs)
	healthyNodeIDs := make([]uint64, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if isNodeOverQuota(usageMap[idToIP[id]]) {
			continue // exhausted → drop from pool, triggers router switch
		}
		healthyNodeIDs = append(healthyNodeIDs, id)
	}
	if len(healthyNodeIDs) == 0 {
		return []SlaveTunnel{}, nil
	}
	nodeIDs = healthyNodeIDs

	// 两段查询（不用 Joins("Node")）：与 fetchK2V5Tunnels 一致，规避 commit 3e20b8e 的
	// Join 别名陷阱（曾 500 每个 /api/subs?country=XX）。私有节点数量极小，两段查询无开销。
	var tunnels []SlaveTunnel
	if err := db.Get().WithContext(ctx).
		Model(&SlaveTunnel{}).
		Preload("Node").
		Where("node_id IN ?", nodeIDs).
		Where("protocol IN ?", tunnelProtocolsForQuery(TunnelProtocolK2V5)).
		Where(&SlaveTunnel{IsTest: BoolPtr(false)}).
		Find(&tunnels).Error; err != nil {
		return nil, err
	}

	// 防御：只保留 private-class 节点的隧道（防数据漂移）。
	out := make([]SlaveTunnel, 0, len(tunnels))
	for _, t := range tunnels {
		if t.Node != nil && t.Node.Class == NodeClassPrivate {
			out = append(out, t)
			continue
		}
		// 正常数据下不可达：subs 已门控 private 节点。若命中说明 node class 数据漂移
		// （节点被改回 shared 但订阅仍指向它），记录以便排查"我的专属节点不见了"。
		nodeClass := ""
		if t.Node != nil {
			nodeClass = t.Node.Class
		}
		log.Warnf(ctx, "private tunnel dropped: node not private (tunnel_id=%d node_id=%d class=%q)", t.ID, t.NodeID, nodeClass)
	}
	return out, nil
}
