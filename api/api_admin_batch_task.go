package center

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// ========================= Batch Task Management API =========================

// api_admin_batch_tasks_create creates a new batch execution task
func api_admin_batch_tasks_create(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateBatchTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Validate schedule parameters
	if req.ScheduleType == "once" && req.ExecuteAt == nil {
		Error(c, ErrorInvalidArgument, "executeAt is required when scheduleType=once")
		return
	}
	if req.ScheduleType == "cron" && req.CronExpr == "" {
		Error(c, ErrorInvalidArgument, "cronExpr is required when scheduleType=cron")
		return
	}

	// Verify script exists
	var script SlaveBatchScript
	if err := db.Get().First(&script, req.ScriptID).Error; err != nil {
		log.Errorf(ctx, "Script not found: %v", err)
		Error(c, ErrorNotFound, "Script not found")
		return
	}

	// Serialize node IDs to JSON
	nodeIDsJSON, err := json.Marshal(req.NodeIDs)
	if err != nil {
		log.Errorf(ctx, "Failed to marshal node IDs: %v", err)
		Error(c, ErrorSystemError, "Failed to marshal node IDs")
		return
	}

	// Get current user ID (from JWT or set to 0 for system)
	var userID uint64 = 0

	// Create task record
	task := SlaveBatchTask{
		ScriptID:     req.ScriptID,
		NodeIDs:      string(nodeIDsJSON),
		ScheduleType: req.ScheduleType,
		ExecuteAt:    req.ExecuteAt,
		CronExpr:     req.CronExpr,
		Status:       "pending",
		CurrentIndex: 0,
		TotalNodes:   len(req.NodeIDs),
		CreatedBy:    userID,
		IsEnabled:    true,
	}

	if err := db.Get().Create(&task).Error; err != nil {
		log.Errorf(ctx, "Failed to create task: %v", err)
		Error(c, ErrorSystemError, "Failed to create task")
		return
	}

	// Enqueue task based on schedule type
	var asynqTaskID string
	if req.ScheduleType == "once" {
		// One-time task: schedule at specific time
		executeTime := time.UnixMilli(*req.ExecuteAt)
		info, err := EnqueueBatchTaskAt(ctx, task.ID, executeTime)
		if err != nil {
			log.Errorf(ctx, "Failed to enqueue one-time task: %v", err)
			Error(c, ErrorSystemError, "Failed to schedule task")
			return
		}
		asynqTaskID = info.ID
	} else {
		// Cron task: register cron schedule
		// For cron tasks, we'll register them on worker startup
		// For now, just mark as pending and the worker will pick it up
		asynqTaskID = fmt.Sprintf("cron:%s", task.CronExpr)
	}

	// Update task with Asynq task ID
	if err := db.Get().Model(&task).Update("asynq_task_id", asynqTaskID).Error; err != nil {
		log.Errorf(ctx, "Failed to update task with asynq ID: %v", err)
		// Non-fatal, continue
	}

	resp := BatchTaskResponse{
		ID:           task.ID,
		AsynqTaskID:  asynqTaskID,
		ScriptID:     task.ScriptID,
		ScriptName:   script.Name,
		NodeIDs:      req.NodeIDs,
		ScheduleType: task.ScheduleType,
		ExecuteAt:    task.ExecuteAt,
		CronExpr:     task.CronExpr,
		Status:       task.Status,
		CurrentIndex: task.CurrentIndex,
		TotalNodes:   task.TotalNodes,
		CreatedAt:    task.CreatedAt,
		CompletedAt:  task.CompletedAt,
		ParentTaskID: task.ParentTaskID,
		IsEnabled:    task.IsEnabled,
	}

	Success(c, &resp)
}

// api_admin_batch_tasks_list lists all batch tasks (paginated)
func api_admin_batch_tasks_list(c *gin.Context) {
	ctx := c.Request.Context()

	pagination := PaginationFromRequest(c)
	status := c.Query("status") // Optional filter by status

	query := db.Get().Model(&SlaveBatchTask{})
	if status != "" {
		query = query.Where(&SlaveBatchTask{Status: status})
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		log.Errorf(ctx, "Failed to count tasks: %v", err)
		Error(c, ErrorSystemError, "Failed to count tasks")
		return
	}

	var tasks []SlaveBatchTask
	if err := query.
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Order("created_at DESC").
		Find(&tasks).Error; err != nil {
		log.Errorf(ctx, "Failed to list tasks: %v", err)
		Error(c, ErrorSystemError, "Failed to list tasks")
		return
	}

	// Load script names
	var scriptIDs []uint64
	for _, task := range tasks {
		scriptIDs = append(scriptIDs, task.ScriptID)
	}

	var scripts []SlaveBatchScript
	scriptMap := make(map[uint64]string)
	if len(scriptIDs) > 0 {
		if err := db.Get().Where("id IN ?", scriptIDs).Find(&scripts).Error; err != nil {
			log.Errorf(ctx, "Failed to load scripts: %v", err)
			// Non-fatal, continue
		}
		for _, script := range scripts {
			scriptMap[script.ID] = script.Name
		}
	}

	// Build response items
	var items []BatchTaskResponse
	for _, task := range tasks {
		var nodeIDs []uint64
		if err := json.Unmarshal([]byte(task.NodeIDs), &nodeIDs); err != nil {
			log.Errorf(ctx, "Failed to unmarshal node IDs for task %d: %v", task.ID, err)
			continue
		}

		items = append(items, BatchTaskResponse{
			ID:           task.ID,
			AsynqTaskID:  task.AsynqTaskID,
			ScriptID:     task.ScriptID,
			ScriptName:   scriptMap[task.ScriptID],
			NodeIDs:      nodeIDs,
			ScheduleType: task.ScheduleType,
			ExecuteAt:    task.ExecuteAt,
			CronExpr:     task.CronExpr,
			Status:       task.Status,
			CurrentIndex: task.CurrentIndex,
			TotalNodes:   task.TotalNodes,
			CreatedAt:    task.CreatedAt,
			CompletedAt:  task.CompletedAt,
			ParentTaskID: task.ParentTaskID,
			IsEnabled:    task.IsEnabled,
		})
	}

	pagination.Total = total
	ListWithData(c, items, pagination)
}

// api_admin_batch_tasks_detail gets task detail with all node results
func api_admin_batch_tasks_detail(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID uint64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Load task
	var task SlaveBatchTask
	if err := db.Get().First(&task, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Task not found: %v", err)
		Error(c, ErrorNotFound, "Task not found")
		return
	}

	// Load script name
	var script SlaveBatchScript
	scriptName := ""
	if err := db.Get().First(&script, task.ScriptID).Error; err == nil {
		scriptName = script.Name
	}

	// Parse node IDs
	var nodeIDs []uint64
	if err := json.Unmarshal([]byte(task.NodeIDs), &nodeIDs); err != nil {
		log.Errorf(ctx, "Failed to unmarshal node IDs: %v", err)
		Error(c, ErrorSystemError, "Failed to parse node IDs")
		return
	}

	// Load all results for this task
	var results []SlaveBatchTaskResult
	if err := db.Get().Where(&SlaveBatchTaskResult{TaskID: uri.ID}).
		Order("node_index ASC").
		Find(&results).Error; err != nil {
		log.Errorf(ctx, "Failed to load results: %v", err)
		Error(c, ErrorSystemError, "Failed to load results")
		return
	}

	// Load node info (join query)
	var nodeIDsFromResults []uint64
	for _, result := range results {
		nodeIDsFromResults = append(nodeIDsFromResults, result.NodeID)
	}

	nodeMap := make(map[uint64]*SlaveNode)
	if len(nodeIDsFromResults) > 0 {
		var nodes []SlaveNode
		if err := db.Get().Where("id IN ?", nodeIDsFromResults).Find(&nodes).Error; err != nil {
			log.Errorf(ctx, "Failed to load nodes: %v", err)
			// Non-fatal, continue
		}
		for i := range nodes {
			nodeMap[nodes[i].ID] = &nodes[i]
		}
	}

	// Build result items
	var resultItems []TaskResultItem
	for _, result := range results {
		node := nodeMap[result.NodeID]
		nodeName := ""
		nodeIPv4 := ""
		if node != nil {
			nodeName = node.Name
			nodeIPv4 = node.Ipv4
		}

		var duration *int64
		if result.StartedAt != nil && result.EndedAt != nil {
			d := *result.EndedAt - *result.StartedAt
			duration = &d
		}

		resultItems = append(resultItems, TaskResultItem{
			NodeID:    result.NodeID,
			NodeName:  nodeName,
			NodeIPv4:  nodeIPv4,
			NodeIndex: result.NodeIndex,
			Status:    result.Status,
			Stdout:    result.Stdout,
			Stderr:    result.Stderr,
			ExitCode:  result.ExitCode,
			Error:     result.Error,
			StartedAt: result.StartedAt,
			EndedAt:   result.EndedAt,
			Duration:  duration,
		})
	}

	// Count success/failed
	successCount := 0
	failedCount := 0
	for _, r := range resultItems {
		if r.Status == "success" {
			successCount++
		} else if r.Status == "failed" || r.Status == "skipped" {
			failedCount++
		}
	}

	resp := BatchTaskDetailResponse{
		BatchTaskResponse: BatchTaskResponse{
			ID:           task.ID,
			AsynqTaskID:  task.AsynqTaskID,
			ScriptID:     task.ScriptID,
			ScriptName:   scriptName,
			NodeIDs:      nodeIDs,
			ScheduleType: task.ScheduleType,
			ExecuteAt:    task.ExecuteAt,
			CronExpr:     task.CronExpr,
			Status:       task.Status,
			CurrentIndex: task.CurrentIndex,
			TotalNodes:   task.TotalNodes,
			CreatedAt:    task.CreatedAt,
			CompletedAt:  task.CompletedAt,
			ParentTaskID: task.ParentTaskID,
			IsEnabled:    task.IsEnabled,
		},
		Results:      resultItems,
		SuccessCount: successCount,
		FailedCount:  failedCount,
		ParentTaskID: task.ParentTaskID,
	}

	Success(c, &resp)
}

// api_admin_batch_tasks_pause pauses a running task
func api_admin_batch_tasks_pause(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID uint64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var task SlaveBatchTask
	if err := db.Get().First(&task, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Task not found: %v", err)
		Error(c, ErrorNotFound, "Task not found")
		return
	}

	if task.Status != "running" && task.Status != "pending" {
		Error(c, ErrorInvalidArgument, "Only running or pending tasks can be paused")
		return
	}

	if err := db.Get().Model(&task).Update("status", "paused").Error; err != nil {
		log.Errorf(ctx, "Failed to pause task: %v", err)
		Error(c, ErrorSystemError, "Failed to pause task")
		return
	}

	Success(c, &map[string]any{"success": true})
}

// api_admin_batch_tasks_resume resumes a paused task
func api_admin_batch_tasks_resume(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID uint64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var task SlaveBatchTask
	if err := db.Get().First(&task, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Task not found: %v", err)
		Error(c, ErrorNotFound, "Task not found")
		return
	}

	if task.Status != "paused" {
		Error(c, ErrorInvalidArgument, "Only paused tasks can be resumed")
		return
	}

	// Update status to pending
	if err := db.Get().Model(&task).Update("status", "pending").Error; err != nil {
		log.Errorf(ctx, "Failed to resume task: %v", err)
		Error(c, ErrorSystemError, "Failed to resume task")
		return
	}

	// Re-enqueue task (continue from CurrentIndex)
	if task.ScheduleType == "once" {
		info, err := EnqueueBatchTaskNow(ctx, task.ID)
		if err != nil {
			log.Errorf(ctx, "Failed to re-enqueue task: %v", err)
			Error(c, ErrorSystemError, "Failed to re-enqueue task")
			return
		}

		// Update Asynq task ID
		if err := db.Get().Model(&task).Update("asynq_task_id", info.ID).Error; err != nil {
			log.Errorf(ctx, "Failed to update asynq task ID: %v", err)
			// Non-fatal
		}
	}

	Success(c, &map[string]any{"success": true})
}

// api_admin_batch_tasks_delete deletes a task (only completed/failed tasks)
func api_admin_batch_tasks_delete(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID uint64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var task SlaveBatchTask
	if err := db.Get().First(&task, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Task not found: %v", err)
		Error(c, ErrorNotFound, "Task not found")
		return
	}

	if task.Status != "completed" && task.Status != "failed" {
		Error(c, ErrorForbidden, "Only completed or failed tasks can be deleted")
		return
	}

	// Delete task and results
	if err := db.Get().Where(&SlaveBatchTaskResult{TaskID: uri.ID}).Delete(&SlaveBatchTaskResult{}).Error; err != nil {
		log.Errorf(ctx, "Failed to delete task results: %v", err)
		Error(c, ErrorSystemError, "Failed to delete task results")
		return
	}

	if err := db.Get().Delete(&task).Error; err != nil {
		log.Errorf(ctx, "Failed to delete task: %v", err)
		Error(c, ErrorSystemError, "Failed to delete task")
		return
	}

	Success(c, &map[string]any{"success": true})
}

// api_admin_batch_tasks_retry retries failed nodes from a completed/failed task
func api_admin_batch_tasks_retry(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID uint64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var req RetryBatchTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Empty body is OK - retry all failed nodes
		req.NodeIDs = nil
	}

	// Load original task
	var originalTask SlaveBatchTask
	if err := db.Get().First(&originalTask, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Task not found: %v", err)
		Error(c, ErrorNotFound, "Task not found")
		return
	}

	if originalTask.Status != "completed" && originalTask.Status != "failed" {
		Error(c, ErrorInvalidArgument, "Only completed or failed tasks can be retried")
		return
	}

	// Get failed/skipped results from original task
	var failedResults []SlaveBatchTaskResult
	query := db.Get().Where("task_id = ? AND status IN ?", uri.ID, []string{"failed", "skipped"})
	if len(req.NodeIDs) > 0 {
		query = query.Where("node_id IN ?", req.NodeIDs)
	}
	if err := query.Find(&failedResults).Error; err != nil {
		log.Errorf(ctx, "Failed to load failed results: %v", err)
		Error(c, ErrorSystemError, "Failed to load failed results")
		return
	}

	if len(failedResults) == 0 {
		Error(c, ErrorInvalidArgument, "No failed nodes to retry")
		return
	}

	// Collect node IDs to retry
	var retryNodeIDs []uint64
	for _, result := range failedResults {
		retryNodeIDs = append(retryNodeIDs, result.NodeID)
	}

	// Serialize node IDs to JSON
	nodeIDsJSON, err := json.Marshal(retryNodeIDs)
	if err != nil {
		log.Errorf(ctx, "Failed to marshal node IDs: %v", err)
		Error(c, ErrorSystemError, "Failed to marshal node IDs")
		return
	}

	// Create new retry task linked to original
	now := time.Now().UnixMilli()
	retryTask := SlaveBatchTask{
		ScriptID:     originalTask.ScriptID,
		NodeIDs:      string(nodeIDsJSON),
		ScheduleType: "once",
		ExecuteAt:    &now,
		Status:       "pending",
		CurrentIndex: 0,
		TotalNodes:   len(retryNodeIDs),
		ParentTaskID: &originalTask.ID,
		IsEnabled:    true,
	}

	if err := db.Get().Create(&retryTask).Error; err != nil {
		log.Errorf(ctx, "Failed to create retry task: %v", err)
		Error(c, ErrorSystemError, "Failed to create retry task")
		return
	}

	// Enqueue for immediate execution
	info, err := EnqueueBatchTaskNow(ctx, retryTask.ID)
	if err != nil {
		log.Errorf(ctx, "Failed to enqueue retry task: %v", err)
		Error(c, ErrorSystemError, "Failed to enqueue retry task")
		return
	}

	// Update task with Asynq task ID
	if err := db.Get().Model(&retryTask).Update("asynq_task_id", info.ID).Error; err != nil {
		log.Errorf(ctx, "Failed to update task with asynq ID: %v", err)
		// Non-fatal
	}

	Success(c, &RetryBatchTaskResponse{TaskID: retryTask.ID})
}

// api_admin_batch_tasks_scheduled lists all scheduled (cron) tasks
func api_admin_batch_tasks_scheduled(c *gin.Context) {
	ctx := c.Request.Context()

	var tasks []SlaveBatchTask
	if err := db.Get().
		Where("schedule_type = ?", "cron").
		Order("created_at DESC").
		Find(&tasks).Error; err != nil {
		log.Errorf(ctx, "Failed to list scheduled tasks: %v", err)
		Error(c, ErrorSystemError, "Failed to list scheduled tasks")
		return
	}

	// Load script names
	var scriptIDs []uint64
	for _, task := range tasks {
		scriptIDs = append(scriptIDs, task.ScriptID)
	}

	scriptMap := make(map[uint64]string)
	if len(scriptIDs) > 0 {
		var scripts []SlaveBatchScript
		if err := db.Get().Where("id IN ?", scriptIDs).Find(&scripts).Error; err == nil {
			for _, s := range scripts {
				scriptMap[s.ID] = s.Name
			}
		}
	}

	// Build response
	var items []ScheduledTaskInfo
	for _, task := range tasks {
		var nodeIDs []uint64
		if err := json.Unmarshal([]byte(task.NodeIDs), &nodeIDs); err != nil {
			log.Errorf(ctx, "Failed to unmarshal node IDs for task %d: %v", task.ID, err)
			continue
		}

		// Find last execution of this scheduled task
		var lastResult SlaveBatchTask
		var lastRunAt *int64
		var lastStatus string
		if err := db.Get().
			Where("schedule_type = 'once' AND script_id = ? AND parent_task_id IS NULL", task.ScriptID).
			Order("created_at DESC").
			First(&lastResult).Error; err == nil {
			lastRunAt = &lastResult.CreatedAt
			lastStatus = lastResult.Status
		}

		items = append(items, ScheduledTaskInfo{
			ID:         task.ID,
			ScriptID:   task.ScriptID,
			ScriptName: scriptMap[task.ScriptID],
			CronExpr:   task.CronExpr,
			IsEnabled:  task.IsEnabled,
			NodeIDs:    nodeIDs,
			TotalNodes: task.TotalNodes,
			LastRunAt:  lastRunAt,
			LastStatus: lastStatus,
			CreatedAt:  task.CreatedAt,
		})
	}

	ListWithData(c, items, &Pagination{Total: int64(len(items))})
}

// api_admin_batch_tasks_schedule_update updates schedule for a cron task
func api_admin_batch_tasks_schedule_update(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID uint64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var req ScheduleBatchTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var task SlaveBatchTask
	if err := db.Get().First(&task, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Task not found: %v", err)
		Error(c, ErrorNotFound, "Task not found")
		return
	}

	if task.ScheduleType != "cron" {
		Error(c, ErrorInvalidArgument, "Can only update schedule for cron tasks")
		return
	}

	// Update schedule
	updates := map[string]any{
		"cron_expr":  req.CronExpr,
		"is_enabled": req.IsEnabled,
	}
	if err := db.Get().Model(&task).Updates(updates).Error; err != nil {
		log.Errorf(ctx, "Failed to update task schedule: %v", err)
		Error(c, ErrorSystemError, "Failed to update schedule")
		return
	}

	Success(c, &map[string]any{"success": true})
}

// api_admin_batch_tasks_schedule_delete cancels a scheduled cron task
func api_admin_batch_tasks_schedule_delete(c *gin.Context) {
	ctx := c.Request.Context()

	var uri struct {
		ID uint64 `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var task SlaveBatchTask
	if err := db.Get().First(&task, uri.ID).Error; err != nil {
		log.Errorf(ctx, "Task not found: %v", err)
		Error(c, ErrorNotFound, "Task not found")
		return
	}

	if task.ScheduleType != "cron" {
		Error(c, ErrorInvalidArgument, "Can only cancel cron tasks")
		return
	}

	// Disable the task instead of deleting (preserve history)
	if err := db.Get().Model(&task).Update("is_enabled", false).Error; err != nil {
		log.Errorf(ctx, "Failed to disable scheduled task: %v", err)
		Error(c, ErrorSystemError, "Failed to cancel schedule")
		return
	}

	Success(c, &map[string]any{"success": true})
}
