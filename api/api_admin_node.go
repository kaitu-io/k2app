// Package center 提供中心服务 API
//
package center

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

func api_admin_list_nodes(c *gin.Context) {
	log.Infof(c, "admin request to list physical nodes")
	pagination := PaginationFromRequest(c)

	var nodes []SlaveNode
	query := db.Get().Model(&SlaveNode{})

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count nodes: %v", err)
		Error(c, ErrorSystemError, "failed to count nodes")
		return
	}

	if err := query.Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&nodes).Error; err != nil {
		log.Errorf(c, "failed to get nodes: %v", err)
		Error(c, ErrorSystemError, "failed to get nodes")
		return
	}

	// Convert to API response format
	items := make([]DataSlaveNode, 0, len(nodes))
	for _, node := range nodes {
		nodeData := DataSlaveNode{
			ID:        node.ID,
			Name:      node.Name,
			Country:   node.Country,
			Region:    node.Region,
			Ipv4:      node.Ipv4,
			Ipv6:      node.Ipv6,
			Load:      0, // Fixed default, no longer calculated
			UpdatedAt: node.UpdatedAt.Unix(),
		}

		items = append(items, nodeData)
	}

	log.Infof(c, "successfully listed %d physical nodes", len(nodes))
	ListWithData(c, items, pagination)
}

// AdminUpdateNodeRequest 更新物理节点请求结构体
//
type AdminUpdateNodeRequest struct {
	Name    *string `json:"name" example:"US-Node-01"`  // 节点名称
	Country *string `json:"country" example:"US"`       // 国家代码
	Ipv6    *string `json:"ipv6" example:"2001:db8::1"` // IPv6地址
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
	log.Debugf(c, "update request for node %s with data: Name=%v, Country=%v, Ipv6=%v", nodeIPv4, req.Name, req.Country, req.Ipv6)

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

	// Return converted data structure
	result := DataSlaveNode{
		ID:        node.ID,
		Name:      node.Name,
		Country:   node.Country,
		Region:    node.Region,
		Ipv4:      node.Ipv4,
		Ipv6:      node.Ipv6,
		Load:      0, // Fixed default, no longer calculated
		UpdatedAt: node.UpdatedAt.Unix(),
	}

	Success(c, &result)
}

func api_admin_delete_node(c *gin.Context) {
	nodeIPv4 := c.Param("ipv4")
	log.Infof(c, "admin request to delete node %s", nodeIPv4)

	// Find node
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

	// Check for pending/running batch tasks targeting this node
	if hasActiveBatchTask, err := nodeHasActiveBatchTasks(node.ID); err != nil {
		log.Errorf(c, "failed to check batch tasks for node %s: %v", nodeIPv4, err)
		Error(c, ErrorSystemError, "failed to check batch tasks")
		return
	} else if hasActiveBatchTask {
		log.Warnf(c, "cannot delete node %s: has active batch tasks", nodeIPv4)
		Error(c, ErrorForbidden, "无法删除：该节点有进行中的批量任务")
		return
	}

	// Begin transaction to delete node and associated data
	tx := db.Get().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 删除关联的隧道
	if err := tx.Unscoped().Where("node_id = ?", node.ID).Delete(&SlaveTunnel{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to delete associated tunnels for node %s: %v", nodeIPv4, err)
		Error(c, ErrorSystemError, "failed to delete associated tunnels")
		return
	}

	// 删除关联的负载记录
	if err := tx.Unscoped().Where("node_id = ?", node.ID).Delete(&SlaveNodeLoad{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to delete associated loads for node %s: %v", nodeIPv4, err)
		Error(c, ErrorSystemError, "failed to delete associated loads")
		return
	}

	// 删除节点本身
	if err := tx.Unscoped().Delete(&node).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to delete node %s: %v", nodeIPv4, err)
		Error(c, ErrorSystemError, "failed to delete node")
		return
	}

	// 提交事务
	if err := tx.Commit().Error; err != nil {
		log.Errorf(c, "failed to commit transaction for deleting node %s: %v", nodeIPv4, err)
		Error(c, ErrorSystemError, "failed to commit deletion")
		return
	}

	log.Infof(c, "successfully deleted node %s and its associated data", nodeIPv4)
	SuccessEmpty(c)
}

// nodeHasActiveBatchTasks checks if a node has any pending/running batch tasks
func nodeHasActiveBatchTasks(nodeID uint64) (bool, error) {
	var tasks []SlaveBatchTask
	if err := db.Get().Where("status IN ?", []string{"pending", "running"}).Find(&tasks).Error; err != nil {
		return false, err
	}

	for _, task := range tasks {
		nodeIDs, err := parseNodeIDs(task.NodeIDs)
		if err != nil {
			continue
		}
		for _, nid := range nodeIDs {
			if nid == nodeID {
				return true, nil
			}
		}
	}
	return false, nil
}

// parseNodeIDs parses JSON array of node IDs
func parseNodeIDs(jsonStr string) ([]uint64, error) {
	var nodeIDs []uint64
	if err := json.Unmarshal([]byte(jsonStr), &nodeIDs); err != nil {
		return nil, err
	}
	return nodeIDs, nil
}

// NodeBatchMatrixScript represents a script in the batch matrix
type NodeBatchMatrixScript struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

// NodeBatchMatrixResult represents a task result for a specific script
type NodeBatchMatrixResult struct {
	Status     string `json:"status"`     // "success", "failed", or null
	TaskID     uint64 `json:"taskId"`     // Batch task ID
	ExecutedAt int64  `json:"executedAt"` // Execution timestamp
	ExitCode   int    `json:"exitCode"`   // Exit code
	Stdout     string `json:"stdout"`     // Standard output (truncated)
	Stderr     string `json:"stderr"`     // Standard error (truncated)
}

// NodeBatchMatrixTunnel represents a tunnel in the batch matrix
type NodeBatchMatrixTunnel struct {
	ID       uint64 `json:"id"`
	Domain   string `json:"domain"`
	Protocol string `json:"protocol"`
	Port     int64  `json:"port"`
}

// NodeBatchMatrixNode represents a node with its batch results
type NodeBatchMatrixNode struct {
	ID          uint64                            `json:"id"`
	Name        string                            `json:"name"`
	Country     string                            `json:"country"`
	Region      string                            `json:"region"`
	Ipv4        string                            `json:"ipv4"`
	Ipv6        string                            `json:"ipv6"`
	UpdatedAt   int64                             `json:"updatedAt"`
	TunnelCount int                               `json:"tunnelCount"`
	Tunnels     []NodeBatchMatrixTunnel           `json:"tunnels"`
	Results     map[string]*NodeBatchMatrixResult `json:"results"`        // script_id -> result
	Meta        json.RawMessage                   `json:"meta,omitempty"` // 节点元数据
}

// NodeBatchMatrixResponse is the response for the batch-matrix API
type NodeBatchMatrixResponse struct {
	Scripts []NodeBatchMatrixScript `json:"scripts"`
	Nodes   []NodeBatchMatrixNode   `json:"nodes"`
}

// api_admin_nodes_batch_matrix returns last N unique tasks and their results per node
// GET /app/nodes/batch-matrix
func api_admin_nodes_batch_matrix(c *gin.Context) {
	log.Infof(c, "admin request for nodes batch matrix")

	// 1. Get all nodes
	var nodes []SlaveNode
	if err := db.Get().Order("name ASC").Find(&nodes).Error; err != nil {
		log.Errorf(c, "failed to get nodes: %v", err)
		Error(c, ErrorSystemError, "failed to get nodes")
		return
	}

	if len(nodes) == 0 {
		Success(c, &NodeBatchMatrixResponse{
			Scripts: []NodeBatchMatrixScript{},
			Nodes:   []NodeBatchMatrixNode{},
		})
		return
	}

	// 2. Get last 5 unique scripts that have been executed (by most recent task)
	var recentTasks []SlaveBatchTask
	if err := db.Get().
		Select("DISTINCT script_id").
		Order("created_at DESC").
		Limit(5).
		Find(&recentTasks).Error; err != nil {
		log.Errorf(c, "failed to get recent tasks: %v", err)
		Error(c, ErrorSystemError, "failed to get recent tasks")
		return
	}

	// Get script details
	scriptIDs := make([]uint64, 0, len(recentTasks))
	for _, task := range recentTasks {
		scriptIDs = append(scriptIDs, task.ScriptID)
	}

	var scripts []SlaveBatchScript
	if len(scriptIDs) > 0 {
		if err := db.Get().Where("id IN ?", scriptIDs).Find(&scripts).Error; err != nil {
			log.Errorf(c, "failed to get scripts: %v", err)
			Error(c, ErrorSystemError, "failed to get scripts")
			return
		}
	}

	// Build script map for quick lookup
	scriptMap := make(map[uint64]*SlaveBatchScript)
	for i := range scripts {
		scriptMap[scripts[i].ID] = &scripts[i]
	}

	// Build response scripts
	responseScripts := make([]NodeBatchMatrixScript, 0, len(scriptIDs))
	for _, sid := range scriptIDs {
		if script, ok := scriptMap[sid]; ok {
			responseScripts = append(responseScripts, NodeBatchMatrixScript{
				ID:   script.ID,
				Name: script.Name,
			})
		}
	}

	// 3. Get all tunnels per node
	var allTunnels []SlaveTunnel
	if err := db.Get().Find(&allTunnels).Error; err != nil {
		log.Warnf(c, "failed to get tunnels: %v", err)
	}

	// Build tunnel map: nodeID -> tunnels
	tunnelMap := make(map[uint64][]NodeBatchMatrixTunnel)
	for _, tunnel := range allTunnels {
		tunnelMap[tunnel.NodeID] = append(tunnelMap[tunnel.NodeID], NodeBatchMatrixTunnel{
			ID:       tunnel.ID,
			Domain:   tunnel.Domain,
			Protocol: string(tunnel.Protocol),
			Port:     tunnel.Port,
		})
	}

	// 4. For each node, get the latest result for each script
	// Query all results for recent tasks
	var allResults []SlaveBatchTaskResult
	if len(scriptIDs) > 0 {
		// Get task IDs for these scripts
		var taskIDs []uint64
		if err := db.Get().Model(&SlaveBatchTask{}).
			Select("id").
			Where("script_id IN ?", scriptIDs).
			Pluck("id", &taskIDs).Error; err != nil {
			log.Warnf(c, "failed to get task IDs: %v", err)
		}

		if len(taskIDs) > 0 {
			if err := db.Get().
				Where("task_id IN ?", taskIDs).
				Order("created_at DESC").
				Find(&allResults).Error; err != nil {
				log.Warnf(c, "failed to get task results: %v", err)
			}
		}
	}

	// Build task ID to script ID mapping
	taskToScript := make(map[uint64]uint64)
	var allTasks []SlaveBatchTask
	if len(scriptIDs) > 0 {
		if err := db.Get().Where("script_id IN ?", scriptIDs).Find(&allTasks).Error; err == nil {
			for _, task := range allTasks {
				taskToScript[task.ID] = task.ScriptID
			}
		}
	}

	// Build node results map: nodeID -> scriptID -> result
	nodeResults := make(map[uint64]map[uint64]*NodeBatchMatrixResult)
	for i := range allResults {
		result := &allResults[i]
		scriptID, ok := taskToScript[result.TaskID]
		if !ok {
			continue
		}

		if _, exists := nodeResults[result.NodeID]; !exists {
			nodeResults[result.NodeID] = make(map[uint64]*NodeBatchMatrixResult)
		}

		// Only keep the latest result for each script per node
		if _, exists := nodeResults[result.NodeID][scriptID]; !exists {
			nodeResults[result.NodeID][scriptID] = &NodeBatchMatrixResult{
				Status:     result.Status,
				TaskID:     result.TaskID,
				ExecutedAt: result.CreatedAt,
				ExitCode:   result.ExitCode,
				Stdout:     truncateString(result.Stdout, 500),
				Stderr:     truncateString(result.Stderr, 500),
			}
		}
	}

	// 5. Build response nodes
	responseNodes := make([]NodeBatchMatrixNode, 0, len(nodes))
	for _, node := range nodes {
		results := make(map[string]*NodeBatchMatrixResult)
		if nodeRes, ok := nodeResults[node.ID]; ok {
			for scriptID, result := range nodeRes {
				results[fmt.Sprintf("%d", scriptID)] = result
			}
		}

		tunnels := tunnelMap[node.ID]
		if tunnels == nil {
			tunnels = []NodeBatchMatrixTunnel{}
		}

		matrixNode := NodeBatchMatrixNode{
			ID:          node.ID,
			Name:        node.Name,
			Country:     node.Country,
			Region:      node.Region,
			Ipv4:        node.Ipv4,
			Ipv6:        node.Ipv6,
			UpdatedAt:   node.UpdatedAt.Unix(),
			TunnelCount: len(tunnels),
			Tunnels:     tunnels,
			Results:     results,
		}
		if node.Meta != "" {
			matrixNode.Meta = json.RawMessage(node.Meta)
		}
		responseNodes = append(responseNodes, matrixNode)
	}

	Success(c, &NodeBatchMatrixResponse{
		Scripts: responseScripts,
		Nodes:   responseNodes,
	})
}
