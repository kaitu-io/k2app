package center

import (
	"context"
	"fmt"
	"math"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
)

// 节点负载 Redis 缓存配置
//
// 缓存保留的原因（即使有数据库索引）：
// 1. 负载计算是CPU密集型操作（流量惩罚曲线、网络惩罚等复杂计算）
// 2. 客户端可能频繁请求隧道列表（每几秒一次）
// 3. 节点数据每60秒才上报一次，数据变化频率低
// 4. 30秒TTL可以在同一上报周期内避免重复计算
// 5. 使用Redis支持多实例部署，避免重复计算
const (
	nodeLoadCachePrefix = "node_load:"      // Redis key 前缀
	nodeLoadCacheTTL    = 30 * time.Second  // 缓存有效期：30秒
)

// getNodeLoadCacheKey 生成节点负载缓存的 Redis key
func getNodeLoadCacheKey(nodeID uint64) string {
	return fmt.Sprintf("%s%d", nodeLoadCachePrefix, nodeID)
}

// getNodeLoadFromCache 从 Redis 缓存获取节点负载分数
func getNodeLoadFromCache(ctx context.Context, nodeID uint64) (int, bool) {
	cacheKey := getNodeLoadCacheKey(nodeID)
	var score int
	exists, err := redis.CacheGet(cacheKey, &score)
	if err != nil {
		log.Warnf(ctx, "failed to get node load from Redis for node %d: %v", nodeID, err)
		return 0, false
	}
	return score, exists
}

// setNodeLoadToCache 将节点负载分数存入 Redis 缓存
func setNodeLoadToCache(ctx context.Context, nodeID uint64, score int) {
	cacheKey := getNodeLoadCacheKey(nodeID)
	ttlSeconds := int(nodeLoadCacheTTL.Seconds())
	if err := redis.CacheSet(cacheKey, score, ttlSeconds); err != nil {
		log.Warnf(ctx, "failed to cache node load in Redis for node %d: %v", nodeID, err)
	}
}

// invalidateNodeLoadCache 使指定节点的负载缓存失效
func invalidateNodeLoadCache(ctx context.Context, nodeID uint64) {
	cacheKey := getNodeLoadCacheKey(nodeID)
	if err := redis.CacheDel(cacheKey); err != nil {
		log.Warnf(ctx, "failed to invalidate node load cache for node %d: %v", nodeID, err)
	}
}

// CalculateNodeLoad 计算节点综合负载分数 (0-100)
// 综合考虑：基础负载(40%) + 网络质量(30%) + 流量使用率(30%)
func CalculateNodeLoad(load *SlaveNodeLoad) int {
	if load == nil {
		return 100 // 无数据时返回满载
	}

	// 1. 基础负载 (40%权重): CPU(25%) + 内存(15%)
	baseLoad := float64(load.Load)*0.25 + load.MemoryUsagePercent*0.15

	// 2. 网络质量惩罚 (30%权重): 延迟(15%) + 丢包(15%)
	latencyPenalty := calculateLatencyPenalty(load.NetworkLatencyMs)
	packetLossPenalty := calculatePacketLossPenalty(load.PacketLossPercent)
	networkPenalty := (latencyPenalty + packetLossPenalty) / 2 * 100 // 转换为0-100分数

	// 3. 流量使用惩罚 (30%权重)
	trafficPenalty := calculateTrafficPenalty(
		load.UsedTrafficBytes,
		load.MonthlyTrafficLimitBytes,
	)

	// 综合得分 = 各部分加权求和
	score := baseLoad*0.4 + networkPenalty*0.3 + trafficPenalty*0.3

	// 确保分数在 0-100 范围内
	return int(math.Max(0, math.Min(100, score)))
}

// calculateLatencyPenalty 计算延迟惩罚 (0-1)
// <50ms: 0分惩罚
// 50-100ms: 线性增长到0.3
// 100-200ms: 线性增长到0.7
// >200ms: 1.0满惩罚
func calculateLatencyPenalty(latencyMs float64) float64 {
	switch {
	case latencyMs < 50:
		return 0
	case latencyMs < 100:
		return (latencyMs - 50) / 50 * 0.3
	case latencyMs < 200:
		return 0.3 + (latencyMs-100)/100*0.4
	default:
		return 1.0
	}
}

// calculatePacketLossPenalty 计算丢包惩罚 (0-1)
// <0.1%: 0分惩罚
// 0.1-1%: 线性增长到0.5
// 1-5%: 线性增长到0.9
// >5%: 1.0满惩罚
func calculatePacketLossPenalty(lossPercent float64) float64 {
	switch {
	case lossPercent < 0.1:
		return 0
	case lossPercent < 1.0:
		return (lossPercent - 0.1) / 0.9 * 0.5
	case lossPercent < 5.0:
		return 0.5 + (lossPercent-1.0)/4.0*0.4
	default:
		return 1.0
	}
}

// calculateTrafficPenalty 计算流量使用惩罚分数 (0-100)
// 使用指数曲线，在高使用率时快速增长
// <50%: 0分惩罚
// 50-80%: 线性增长到30分
// 80-95%: 快速增长到70分
// 95-100%: 快速增长到95分
// >100%: 100分满惩罚
func calculateTrafficPenalty(usedBytes, limitBytes int64) float64 {
	if limitBytes == 0 {
		return 0 // 无限流量，无惩罚
	}

	usage := float64(usedBytes) / float64(limitBytes)

	switch {
	case usage < 0.5:
		return 0
	case usage < 0.8:
		// 50-80%: 线性增长到30分
		return (usage - 0.5) / 0.3 * 30
	case usage < 0.95:
		// 80-95%: 快速增长到70分
		return 30 + (usage-0.8)/0.15*40
	case usage < 1.0:
		// 95-100%: 快速增长到95分
		return 70 + (usage-0.95)/0.05*25
	default:
		// 超过限制，返回满惩罚
		return 100
	}
}

// GetNodeLoad 获取节点负载（带 Redis 缓存）
// 如果缓存命中且未过期，直接返回缓存值
// 否则从数据库查询最新 Load 记录并计算
//
// 注意：此方法适用于单个节点查询，批量查询请使用 GetNodeLoads()
func GetNodeLoad(ctx context.Context, nodeID uint64) int {
	// 先查 Redis 缓存
	if score, hit := getNodeLoadFromCache(ctx, nodeID); hit {
		return score
	}

	// 缓存未命中，从数据库查询最新 Load 记录（使用 max(id) 优化）
	var load SlaveNodeLoad
	if err := db.Get().Where("id = (?)",
		db.Get().Model(&SlaveNodeLoad{}).
			Select("MAX(id)").
			Where("node_id = ?", nodeID),
	).First(&load).Error; err != nil {
		return 100 // 查询失败返回满载
	}

	// 计算负载
	score := CalculateNodeLoad(&load)

	// 更新 Redis 缓存
	setNodeLoadToCache(ctx, nodeID, score)

	return score
}

// InvalidateNodeLoadCache 使指定节点的负载缓存失效
// 在节点上报新数据时调用，强制下次查询时重新计算
func InvalidateNodeLoadCache(ctx context.Context, nodeID uint64) {
	invalidateNodeLoadCache(ctx, nodeID)
}

// NodeLoadDetails contains detailed load information for evaluation
type NodeLoadDetails struct {
	Load                  int     // Calculated load score (0-100)
	TrafficUsagePercent   float64 // Traffic quota usage (0-100)
	BandwidthUsagePercent float64 // Bandwidth usage (0-100)
	IsCloudTunnel         bool    // Whether this node has sidecar (health reporting)
}

// GetNodeLoadDetails returns detailed load information for multiple nodes
// Used by the tunnel API to provide evaluation metrics
func GetNodeLoadDetails(ctx context.Context, nodeIDs []uint64) map[uint64]NodeLoadDetails {
	if len(nodeIDs) == 0 {
		return make(map[uint64]NodeLoadDetails)
	}

	result := make(map[uint64]NodeLoadDetails, len(nodeIDs))

	// Query the latest load records for all nodes
	var loads []SlaveNodeLoad
	if err := db.Get().Where("id IN (?)",
		db.Get().Model(&SlaveNodeLoad{}).
			Select("MAX(id)").
			Where("node_id IN ?", nodeIDs).
			Group("node_id"),
	).Find(&loads).Error; err != nil {
		// Query failed, return default values (not cloud tunnel)
		for _, nodeID := range nodeIDs {
			result[nodeID] = NodeLoadDetails{
				Load:          100,
				IsCloudTunnel: false,
			}
		}
		return result
	}

	// Build a set of nodes that have load records (these are Cloud Tunnels)
	nodeLoadMap := make(map[uint64]*SlaveNodeLoad, len(loads))
	for i := range loads {
		nodeLoadMap[loads[i].NodeID] = &loads[i]
	}

	// Calculate details for each node
	for _, nodeID := range nodeIDs {
		if load, exists := nodeLoadMap[nodeID]; exists {
			// Node has load records - it's a Cloud Tunnel
			loadScore := CalculateNodeLoad(load)

			// Calculate bandwidth usage (compare current bandwidth to peak)
			// Use max of up/down bandwidth vs peak speed
			bandwidthUsage := 0.0
			if load.NetworkSpeedMbps > 0 {
				currentBandwidth := load.BandwidthUpMbps + load.BandwidthDownMbps
				bandwidthUsage = (currentBandwidth / load.NetworkSpeedMbps) * 100
				if bandwidthUsage > 100 {
					bandwidthUsage = 100
				}
			}

			result[nodeID] = NodeLoadDetails{
				Load:                  loadScore,
				TrafficUsagePercent:   load.GetTrafficUsagePercent(),
				BandwidthUsagePercent: bandwidthUsage,
				IsCloudTunnel:         true,
			}
		} else {
			// Node has no load records - not a Cloud Tunnel
			result[nodeID] = NodeLoadDetails{
				Load:          100, // Default full load
				IsCloudTunnel: false,
			}
		}
	}

	return result
}

// GetNodeLoads 批量获取多个节点的负载（带 Redis 缓存，性能优化）
// 返回 map[nodeID]loadScore，避免 N+1 查询问题
//
// 性能优化策略：
// 1. 优先从 Redis 缓存获取（支持多实例部署）
// 2. 缓存未命中时使用批量查询（单次SQL，利用node_id索引）
// 3. SQL使用max(id)子查询获取最新记录（主键查询，比时间戳更快）
// 4. 计算后更新 Redis 缓存（30秒TTL，节点上报间隔60秒）
//
// SQL示例：
//
//	SELECT * FROM slave_node_loads
//	WHERE id IN (
//	    SELECT MAX(id) FROM slave_node_loads
//	    WHERE node_id IN (1,2,3,...)
//	    GROUP BY node_id
//	)
func GetNodeLoads(ctx context.Context, nodeIDs []uint64) map[uint64]int {
	if len(nodeIDs) == 0 {
		return make(map[uint64]int)
	}

	result := make(map[uint64]int, len(nodeIDs))
	missingIDs := make([]uint64, 0)

	// 先从 Redis 缓存中获取
	for _, nodeID := range nodeIDs {
		if score, hit := getNodeLoadFromCache(ctx, nodeID); hit {
			result[nodeID] = score
		} else {
			missingIDs = append(missingIDs, nodeID)
		}
	}

	// 如果全部命中缓存，直接返回
	if len(missingIDs) == 0 {
		return result
	}

	// 批量查询数据库中缺失的节点负载记录
	// 使用 max(id) 子查询获取每个节点的最新负载记录（利用 node_id 索引 + id 主键）
	var loads []SlaveNodeLoad
	if err := db.Get().Where("id IN (?)",
		db.Get().Model(&SlaveNodeLoad{}).
			Select("MAX(id)").
			Where("node_id IN ?", missingIDs).
			Group("node_id"),
	).Find(&loads).Error; err != nil {
		// 查询失败，为缺失的节点返回满载
		for _, nodeID := range missingIDs {
			result[nodeID] = 100
		}
		return result
	}

	// 计算负载并更新 Redis 缓存
	for i := range loads {
		score := CalculateNodeLoad(&loads[i])
		result[loads[i].NodeID] = score
		setNodeLoadToCache(ctx, loads[i].NodeID, score)
	}

	// 为没有负载记录的节点返回满载
	for _, nodeID := range missingIDs {
		if _, exists := result[nodeID]; !exists {
			result[nodeID] = 100
		}
	}

	return result
}
