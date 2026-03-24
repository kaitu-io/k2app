package center

import (
	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// AdminNodeTunnel represents a tunnel in the admin node list
type AdminNodeTunnel struct {
	ID        uint64 `json:"id"`
	Name      string `json:"name"`
	Domain    string `json:"domain"`
	Protocol  string `json:"protocol"`
	Port      int64  `json:"port"`
	ServerURL string `json:"serverUrl,omitempty"`
}

// AdminNodeItem represents a node with its tunnels for admin listing
type AdminNodeItem struct {
	ID        uint64            `json:"id"`
	Name      string            `json:"name"`
	Country   string            `json:"country"`
	Region    string            `json:"region"`
	Ipv4      string            `json:"ipv4"`
	Ipv6      string            `json:"ipv6"`
	UpdatedAt int64             `json:"updatedAt"`
	Tunnels   []AdminNodeTunnel `json:"tunnels"`
}

func api_admin_list_nodes(c *gin.Context) {
	log.Infof(c, "admin request to list physical nodes")
	pagination := PaginationFromRequest(c)
	if c.Query("pageSize") == "" {
		pagination.PageSize = 500
	}

	var nodes []SlaveNode
	query := db.Get().Model(&SlaveNode{})

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count nodes: %v", err)
		Error(c, ErrorSystemError, "failed to count nodes")
		return
	}

	if err := query.Preload("Tunnels").Order("name ASC").Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&nodes).Error; err != nil {
		log.Errorf(c, "failed to get nodes: %v", err)
		Error(c, ErrorSystemError, "failed to get nodes")
		return
	}

	items := make([]AdminNodeItem, 0, len(nodes))
	for _, node := range nodes {
		tunnels := make([]AdminNodeTunnel, 0, len(node.Tunnels))
		for _, t := range node.Tunnels {
			tunnels = append(tunnels, AdminNodeTunnel{
				ID:        t.ID,
				Name:      t.Name,
				Domain:    t.Domain,
				Protocol:  string(t.Protocol),
				Port:      t.Port,
				ServerURL: t.ServerURL,
			})
		}

		items = append(items, AdminNodeItem{
			ID:        node.ID,
			Name:      node.Name,
			Country:   node.Country,
			Region:    node.Region,
			Ipv4:      node.Ipv4,
			Ipv6:      node.Ipv6,
			UpdatedAt: node.UpdatedAt.Unix(),
			Tunnels:   tunnels,
		})
	}

	log.Infof(c, "successfully listed %d physical nodes", len(nodes))
	ListWithData(c, items, pagination)
}

// AdminUpdateNodeRequest 更新物理节点请求结构体
type AdminUpdateNodeRequest struct {
	Name    *string `json:"name" example:"US-Node-01"`
	Country *string `json:"country" example:"US"`
	Ipv6    *string `json:"ipv6" example:"2001:db8::1"`
}

func api_admin_update_node(c *gin.Context) {
	nodeIPv4 := c.Param("ipv4")
	log.Infof(c, "admin request to update node %s", nodeIPv4)

	var req AdminUpdateNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request to update node %s: %v", nodeIPv4, err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var node SlaveNode
	if err := db.Get().Where("ipv4 = ?", nodeIPv4).First(&node).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "node %s not found for update", nodeIPv4)
			Error(c, ErrorNotFound, "node not found")
		} else {
			log.Errorf(c, "failed to find node %s for update: %v", nodeIPv4, err)
			Error(c, ErrorSystemError, "failed to find node")
		}
		return
	}

	updateData := make(map[string]interface{})
	if req.Name != nil {
		updateData["name"] = *req.Name
	}
	if req.Country != nil {
		updateData["country"] = *req.Country
	}
	if req.Ipv6 != nil {
		updateData["ipv6"] = *req.Ipv6
	}

	if len(updateData) > 0 {
		if err := db.Get().Model(&node).Updates(updateData).Error; err != nil {
			log.Errorf(c, "failed to update node %s: %v", nodeIPv4, err)
			Error(c, ErrorSystemError, "failed to update node")
			return
		}
	}

	log.Infof(c, "successfully updated node %s", nodeIPv4)

	result := DataSlaveNode{
		ID:        node.ID,
		Name:      node.Name,
		Country:   node.Country,
		Region:    node.Region,
		Ipv4:      node.Ipv4,
		Ipv6:      node.Ipv6,
		Load:      0,
		UpdatedAt: node.UpdatedAt.Unix(),
	}

	Success(c, &result)
	WriteAuditLog(c, "node_update", "node", nodeIPv4, nil)
}

func api_admin_delete_node(c *gin.Context) {
	nodeIPv4 := c.Param("ipv4")
	log.Infof(c, "admin request to delete node %s", nodeIPv4)

	var node SlaveNode
	if err := db.Get().Where("ipv4 = ?", nodeIPv4).First(&node).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "node %s not found for deletion", nodeIPv4)
			Error(c, ErrorNotFound, "node not found")
		} else {
			log.Errorf(c, "failed to find node %s for deletion: %v", nodeIPv4, err)
			Error(c, ErrorSystemError, "failed to find node")
		}
		return
	}

	tx := db.Get().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err := tx.Unscoped().Where("node_id = ?", node.ID).Delete(&SlaveTunnel{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to delete associated tunnels for node %s: %v", nodeIPv4, err)
		Error(c, ErrorSystemError, "failed to delete associated tunnels")
		return
	}

	if err := tx.Unscoped().Where("node_id = ?", node.ID).Delete(&SlaveNodeLoad{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to delete associated loads for node %s: %v", nodeIPv4, err)
		Error(c, ErrorSystemError, "failed to delete associated loads")
		return
	}

	if err := tx.Unscoped().Delete(&node).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to delete node %s: %v", nodeIPv4, err)
		Error(c, ErrorSystemError, "failed to delete node")
		return
	}

	if err := tx.Commit().Error; err != nil {
		log.Errorf(c, "failed to commit transaction for deleting node %s: %v", nodeIPv4, err)
		Error(c, ErrorSystemError, "failed to commit deletion")
		return
	}

	log.Infof(c, "successfully deleted node %s and its associated data", nodeIPv4)
	SuccessEmpty(c)
	WriteAuditLog(c, "node_delete", "node", nodeIPv4, nil)
}
