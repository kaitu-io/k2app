// Package center 提供中心服务 API
//
package center

import (
	"strings"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

func api_admin_list_tunnels(c *gin.Context) {
	log.Infof(c, "admin request to list tunnels")
	pagination := PaginationFromRequest(c)

	// 获取协议筛选参数（可选）
	protocol := c.Query("protocol")
	if protocol != "" {
		// 向后兼容：规范化协议字符串，去除 + 和空格
		// - 旧版客户端可能发送 k2wss
		// - URL编码可能将 + 解码为空格（k2 wss）
		// - 统一转换为新标准 k2wss
		protocol = strings.ReplaceAll(protocol, "+", "")
		protocol = strings.ReplaceAll(protocol, " ", "")
		log.Debugf(c, "filtering tunnels by protocol: %s (normalized)", protocol)
	}

	var tunnels []SlaveTunnel
	query := db.Get().Model(&SlaveTunnel{}).Preload("Node")

	// 应用协议筛选
	if protocol != "" {
		query = query.Where(&SlaveTunnel{Protocol: TunnelProtocol(protocol)})
	}

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count tunnels: %v", err)
		Error(c, ErrorSystemError, "failed to count tunnels")
		return
	}

	// 按节点名称排序
	if err := query.Joins("LEFT JOIN slave_nodes ON slave_nodes.id = slave_tunnels.node_id").
		Order("slave_nodes.name ASC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&tunnels).Error; err != nil {
		log.Errorf(c, "failed to get tunnels: %v", err)
		Error(c, ErrorSystemError, "failed to get tunnels")
		return
	}

	// 收集所有节点ID，用于批量查询负载
	nodeIDs := make([]uint64, 0, len(tunnels))
	for _, tunnel := range tunnels {
		if tunnel.Node.ID != 0 {
			nodeIDs = append(nodeIDs, tunnel.Node.ID)
		}
	}

	// 批量查询节点负载，避免N+1查询问题
	nodeLoads := GetNodeLoads(c, nodeIDs)

	// 转换为API响应格式
	items := make([]DataSlaveTunnel, 0, len(tunnels))
	for _, tunnel := range tunnels {
		// 从批量查询结果中获取负载
		load := 100 // 默认满载
		if l, exists := nodeLoads[tunnel.Node.ID]; exists {
			load = l
		}

		nodeData := DataSlaveNode{
			Name:    tunnel.Node.Name,
			Country: tunnel.Node.Country,
			Region:  tunnel.Node.Region,
			Ipv4:    tunnel.Node.Ipv4,
			Ipv6:    tunnel.Node.Ipv6,
			Load:    load, // 使用真实的负载值
		}

		item := DataSlaveTunnel{
			ID:        tunnel.ID,
			Domain:    tunnel.Domain,
			Name:      tunnel.Name,
			Protocol:  tunnel.Protocol,
			Port:      tunnel.Port,
			ServerUrl: tunnel.ServerURL,
			Node:      nodeData,
		}
		items = append(items, item)
	}

	log.Infof(c, "successfully listed %d tunnels (protocol filter: %s)", len(tunnels), protocol)
	ListWithData(c, items, pagination)
}

// AdminUpdateTunnelRequest 更新隧道请求结构体
//
type AdminUpdateTunnelRequest struct {
	Protocol *TunnelProtocol `json:"protocol" example:"k2wss"`  // 隧道协议
	Port     *int64          `json:"port" example:"10001"`      // 隧道端口
	Url      *string         `json:"url" example:"https://..."` // 隧道URL (maps to server_url column)
}

func api_admin_update_tunnel(c *gin.Context) {
	tunnelID := c.Param("id")
	log.Infof(c, "admin request to update tunnel %s", tunnelID)

	var req AdminUpdateTunnelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request to update tunnel %s: %v", tunnelID, err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	log.Debugf(c, "update request for tunnel %s with data: Protocol=%v, Port=%v", tunnelID, req.Protocol, req.Port)

	var tunnel SlaveTunnel
	if err := db.Get().Preload("Node").First(&tunnel, tunnelID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "tunnel %s not found for update", tunnelID)
			Error(c, ErrorNotFound, "tunnel not found")
		} else {
			log.Errorf(c, "failed to find tunnel %s for update: %v", tunnelID, err)
			Error(c, ErrorSystemError, "failed to find tunnel")
		}
		return
	}

	updateData := make(map[string]interface{})
	if req.Protocol != nil {
		updateData["protocol"] = *req.Protocol
	}
	if req.Port != nil {
		updateData["port"] = *req.Port
	}
	if req.Url != nil {
		updateData["server_url"] = *req.Url
	}

	if len(updateData) > 0 {
		if err := db.Get().Model(&tunnel).Updates(updateData).Error; err != nil {
			log.Errorf(c, "failed to update tunnel %s: %v", tunnelID, err)
			Error(c, ErrorSystemError, "failed to update tunnel")
			return
		}
	}

	log.Infof(c, "successfully updated tunnel %s", tunnelID)

	// 返回转换后的数据结构
	nodeData := DataSlaveNode{
		Name:    tunnel.Node.Name,
		Country: tunnel.Node.Country,
		Region:  tunnel.Node.Region,
		Ipv4:    tunnel.Node.Ipv4,
		Ipv6:    tunnel.Node.Ipv6,
	}

	// Re-read tunnel to get updated fields (including server_url)
	db.Get().First(&tunnel, tunnelID)

	result := DataSlaveTunnel{
		ID:        tunnel.ID,
		Domain:    tunnel.Domain,
		Name:      tunnel.Name,
		Protocol:  tunnel.Protocol,
		Port:      tunnel.Port,
		ServerUrl: tunnel.ServerURL,
		Node:      nodeData,
	}

	Success(c, &result)
}

func api_admin_delete_tunnel(c *gin.Context) {
	nodeID := c.Param("id")
	log.Infof(c, "admin request to delete node %s", nodeID)

	if err := db.Get().Unscoped().Delete(&SlaveTunnel{}, nodeID).Error; err != nil {
		log.Errorf(c, "failed to delete node %s: %v", nodeID, err)
		Error(c, ErrorSystemError, "failed to delete node")
		return
	}
	log.Infof(c, "successfully deleted node %s", nodeID)
	SuccessEmpty(c)
}
