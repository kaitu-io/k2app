package center

import (
	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// SlaveStatusReportRequest 节点状态报告请求结构体
type SlaveStatusReportRequest struct {
	UpdatedAt int64             `json:"updatedAt" example:"1640995200"` // 报告时间戳
	Health    SlaveTunnelHealth `json:"health"`                         // 节点健康指标
}

// SlaveStatusReportResponse 节点状态报告响应结构体
type SlaveStatusReportResponse struct {
	Success bool `json:"success" example:"true"` // 处理是否成功
}

func api_slave_report_status(c *gin.Context) {
	var req SlaveStatusReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request body: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 仅支持物理节点认证（k2-slave-sidecar使用）
	physicalNode := ReqSlaveNode(c)
	if physicalNode == nil {
		log.Errorf(c, "no valid node authentication found")
		Error(c, ErrorNotLogin, "node authentication required")
		return
	}

	// 计算服务器负载评分
	serverLoad := calculateServerLoad(req.Health)

	log.Infof(c, "NodeID=%d CPU=%.1f%% Mem=%.1f%% Disk=%.1f%% Conn=%d BW=%.2f/%.2fMbps Loss=%.2f%% Load=%d",
		physicalNode.ID, req.Health.CPUUsage, req.Health.MemoryUsage, req.Health.DiskUsage, req.Health.Connections,
		req.Health.BandwidthUpMbps, req.Health.BandwidthDownMbps,
		req.Health.PacketLossPercent, serverLoad)

	// 创建负载记录（包含所有指标）
	load := SlaveNodeLoad{
		NodeID: physicalNode.ID,
		Load:   serverLoad,

		// 网络性能指标
		NetworkSpeedMbps:  req.Health.NetworkSpeedMbps,
		BandwidthUpMbps:   req.Health.BandwidthUpMbps,
		BandwidthDownMbps: req.Health.BandwidthDownMbps,
		NetworkLatencyMs:  req.Health.NetworkLatencyMs,
		PacketLossPercent: req.Health.PacketLossPercent,

		// 系统资源指标
		MemoryUsagePercent: req.Health.MemoryUsage,
		DiskUsagePercent:   req.Health.DiskUsage,
		ConnectionCount:    req.Health.Connections,

		// 流量统计
		TotalBytesReceived: uint64(req.Health.NetworkIn),
		TotalBytesSent:     uint64(req.Health.NetworkOut),

		// 月度流量追踪
		BillingCycleEndAt:        req.Health.BillingCycleEndAt,
		MonthlyTrafficLimitBytes: req.Health.MonthlyTrafficLimitBytes,
		UsedTrafficBytes:         req.Health.UsedTrafficBytes,
	}
	if err := db.Get().Create(&load).Error; err != nil {
		log.Errorf(c, "failed to save load record: %v", err)
		Error(c, ErrorSystemError, "failed to save load record")
		return
	}

	// 清除节点负载缓存，强制下次查询时重新计算
	InvalidateNodeLoadCache(c, physicalNode.ID)

	log.Infof(c, "saved: NodeID=%d Load=%d", physicalNode.ID, serverLoad)
	Success(c, &SlaveStatusReportResponse{
		Success: true,
	})
}

// calculateServerLoad 计算服务器负载评分
//
// 算法设计原则：
//  1. 木桶原则：任何一个关键指标异常都应该显著提高负载分数
//  2. 分层评估：先检测严重问题（直接高分），再计算综合负载
//  3. 最大值优先：取各维度负载的最大值作为基础分，再叠加其他因素
func calculateServerLoad(health SlaveTunnelHealth) int {
	// === 第一层：严重问题检测（直接返回高分）===
	// 任何一个指标达到危险阈值，直接返回高负载

	// CPU >= 90%：服务器基本不可用
	if health.CPUUsage >= 90 {
		return 95
	}
	// 内存 >= 95%：可能 OOM
	if health.MemoryUsage >= 95 {
		return 95
	}
	// 磁盘 >= 95%：可能无法写入
	if health.DiskUsage >= 95 {
		return 90
	}
	// 丢包 >= 10%：网络严重问题
	if health.PacketLossPercent >= 10 {
		return 90
	}

	// === 第二层：计算各维度负载分数（0-100）===

	// CPU 负载：0-100 直接映射
	cpuLoad := int(health.CPUUsage)

	// 内存负载：0-100 直接映射
	memoryLoad := int(health.MemoryUsage)

	// 磁盘负载：0-100 直接映射
	diskLoad := int(health.DiskUsage)

	// 丢包负载：0%=0, 1%=20, 3%=50, 5%=70, 10%=100
	packetLossLoad := 0
	if health.PacketLossPercent >= 5 {
		packetLossLoad = 70 + int(health.PacketLossPercent*3)
	} else if health.PacketLossPercent >= 3 {
		packetLossLoad = 50 + int((health.PacketLossPercent-3)*10)
	} else if health.PacketLossPercent >= 1 {
		packetLossLoad = 20 + int((health.PacketLossPercent-1)*15)
	} else {
		packetLossLoad = int(health.PacketLossPercent * 20)
	}
	if packetLossLoad > 100 {
		packetLossLoad = 100
	}

	// 带宽负载：基于实际使用量
	// 0-100Mbps=0-10, 100-500=10-30, 500-1000=30-60, 1000-2000=60-90, 2000+=90-100
	totalBandwidth := health.BandwidthUpMbps + health.BandwidthDownMbps
	bandwidthLoad := 0
	if totalBandwidth >= 2000 {
		bandwidthLoad = 90 + int((totalBandwidth-2000)/200)
	} else if totalBandwidth >= 1000 {
		bandwidthLoad = 60 + int((totalBandwidth-1000)/33)
	} else if totalBandwidth >= 500 {
		bandwidthLoad = 30 + int((totalBandwidth-500)/17)
	} else if totalBandwidth >= 100 {
		bandwidthLoad = 10 + int((totalBandwidth-100)/20)
	} else {
		bandwidthLoad = int(totalBandwidth / 10)
	}
	if bandwidthLoad > 100 {
		bandwidthLoad = 100
	}

	// 连接数负载：0-100=0, 100-500=10-30, 500-2000=30-60, 2000-5000=60-90, 5000+=90-100
	connectionLoad := 0
	if health.Connections >= 5000 {
		connectionLoad = 90 + (health.Connections-5000)/500
	} else if health.Connections >= 2000 {
		connectionLoad = 60 + (health.Connections-2000)/100
	} else if health.Connections >= 500 {
		connectionLoad = 30 + (health.Connections-500)/50
	} else if health.Connections >= 100 {
		connectionLoad = 10 + (health.Connections-100)/20
	} else {
		connectionLoad = health.Connections / 10
	}
	if connectionLoad > 100 {
		connectionLoad = 100
	}

	// === 第三层：综合计算（木桶+加权）===

	// 找出最大负载（木桶短板）
	maxLoad := cpuLoad
	if memoryLoad > maxLoad {
		maxLoad = memoryLoad
	}
	if diskLoad > maxLoad {
		maxLoad = diskLoad
	}
	if packetLossLoad > maxLoad {
		maxLoad = packetLossLoad
	}

	// 计算加权平均（带宽和连接数作为补充因素）
	// 权重：CPU 25%, Memory 25%, Disk 10%, PacketLoss 15%, Bandwidth 15%, Connections 10%
	weightedAvg := int(
		float64(cpuLoad)*0.25 +
			float64(memoryLoad)*0.25 +
			float64(diskLoad)*0.10 +
			float64(packetLossLoad)*0.15 +
			float64(bandwidthLoad)*0.15 +
			float64(connectionLoad)*0.10)

	// 最终分数 = max(木桶短板 * 0.7, 加权平均)
	// 这样确保单一指标异常时不会被稀释太多
	shortBoardScore := maxLoad * 7 / 10
	finalScore := weightedAvg
	if shortBoardScore > finalScore {
		finalScore = shortBoardScore
	}

	if finalScore > 100 {
		finalScore = 100
	}

	return finalScore
}
