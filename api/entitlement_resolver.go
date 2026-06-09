package center

import (
	"context"

	db "github.com/wordgate/qtoolkit/db"
)

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

	nodeIDs := make([]uint64, 0, len(subs))
	for i := range subs {
		if subs[i].IsServiceable(now) && subs[i].SlaveNodeID != nil {
			nodeIDs = append(nodeIDs, *subs[i].SlaveNodeID)
		}
	}
	if len(nodeIDs) == 0 {
		return []SlaveTunnel{}, nil
	}

	var tunnels []SlaveTunnel
	if err := db.Get().WithContext(ctx).
		Model(&SlaveTunnel{}).
		Preload("Node").
		Where("node_id IN ?", nodeIDs).
		Where("protocol IN ?", tunnelProtocolsForQuery(TunnelProtocolK2V5)).
		Find(&tunnels).Error; err != nil {
		return nil, err
	}

	// 防御：只保留 private-class 节点的隧道（防数据漂移）。
	out := make([]SlaveTunnel, 0, len(tunnels))
	for _, t := range tunnels {
		if t.Node != nil && t.Node.Class == NodeClassPrivate {
			out = append(out, t)
		}
	}
	return out, nil
}
