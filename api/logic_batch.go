package center

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"context"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// ========================= Batch Execution Logic =========================

// ExecuteBatchTask executes a batch task (called by Asynq worker)
func ExecuteBatchTask(ctx context.Context, taskID uint64) error {
	// Load task
	var task SlaveBatchTask
	if err := db.Get().First(&task, taskID).Error; err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	// Check task status (support interruption)
	if task.Status == "paused" {
		log.Infof(ctx, "Task %d is paused, skip execution", taskID)
		return nil // Not an error, wait for resume
	}

	// Update task status to running
	if err := db.Get().Model(&task).Updates(map[string]any{
		"status": "running",
	}).Error; err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}

	// Load script content (decrypt)
	script, err := loadAndDecryptScript(ctx, task.ScriptID)
	if err != nil {
		updateTaskStatus(ctx, taskID, "failed")
		return fmt.Errorf("failed to load script: %w", err)
	}

	// Parse node ID list
	var nodeIDs []uint64
	if err := json.Unmarshal([]byte(task.NodeIDs), &nodeIDs); err != nil {
		updateTaskStatus(ctx, taskID, "failed")
		return fmt.Errorf("invalid node_ids: %w", err)
	}

	// Execute on each node sequentially
	for i := task.CurrentIndex; i < len(nodeIDs); i++ {
		// Re-check task status before each node execution
		if err := db.Get().First(&task, taskID).Error; err != nil {
			return fmt.Errorf("failed to reload task: %w", err)
		}
		if task.Status == "paused" {
			log.Infof(ctx, "Task %d paused at node index %d", taskID, i)
			return nil // Pause, wait for resume
		}

		// Execute on single node
		nodeID := nodeIDs[i]
		if err := executeOnNode(ctx, taskID, nodeID, i, script); err != nil {
			log.Errorf(ctx, "Failed to execute on node %d: %v", nodeID, err)
			// Continue to next node, don't abort entire task
		}

		// Update task progress
		if err := db.Get().Model(&task).Update("current_index", i+1).Error; err != nil {
			log.Errorf(ctx, "Failed to update task progress: %v", err)
		}
	}

	// Task completed
	now := time.Now().UnixMilli()
	if err := db.Get().Model(&task).Updates(map[string]any{
		"status":       "completed",
		"completed_at": now,
	}).Error; err != nil {
		return fmt.Errorf("failed to mark task completed: %w", err)
	}

	log.Infof(ctx, "Batch task %d completed successfully", taskID)
	return nil
}

// executeOnNode executes script on a single node
func executeOnNode(ctx context.Context, taskID, nodeID uint64, nodeIndex int, script *SlaveBatchScript) error {
	startTime := time.Now().UnixMilli()

	// Query node
	var node SlaveNode
	if err := db.Get().First(&node, nodeID).Error; err != nil {
		// Record failure result
		recordResult(ctx, taskID, nodeID, nodeIndex, "failed", "", "", -1, fmt.Sprintf("Node not found: %v", err), nil, nil)
		return fmt.Errorf("node %d not found: %w", nodeID, err)
	}

	// Note: All nodes in DB are active by design
	// Node existence check above is sufficient

	// Generate script filename (human-readable)
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	scriptName := fmt.Sprintf("%s_%d.sh", timestamp, taskID)
	remotePath := fmt.Sprintf("/home/ubuntu/.tasks/%s", scriptName)

	// Ensure .tasks directory exists
	mkdirCmd := "mkdir -p /home/ubuntu/.tasks"
	if _, err := node.SSHExec(ctx, mkdirCmd); err != nil {
		recordResult(ctx, taskID, nodeID, nodeIndex, "failed", "", "", -1, fmt.Sprintf("Failed to create .tasks directory: %v", err), &startTime, nil)
		return fmt.Errorf("failed to create .tasks directory on node %d: %w", nodeID, err)
	}

	// Upload script to node
	scriptContent := script.Content // Already decrypted
	if err := node.SSHCopyFile(ctx, []byte(scriptContent), remotePath); err != nil {
		recordResult(ctx, taskID, nodeID, nodeIndex, "failed", "", "", -1, fmt.Sprintf("Failed to upload script: %v", err), &startTime, nil)
		return fmt.Errorf("failed to upload script to node %d: %w", nodeID, err)
	}

	// Add execute permission
	chmodCmd := fmt.Sprintf("chmod +x %s", remotePath)
	if _, err := node.SSHExec(ctx, chmodCmd); err != nil {
		recordResult(ctx, taskID, nodeID, nodeIndex, "failed", "", "", -1, fmt.Sprintf("Failed to chmod: %v", err), &startTime, nil)
		return fmt.Errorf("failed to chmod script on node %d: %w", nodeID, err)
	}

	// Execute script (with optional sudo)
	execCmd := remotePath
	if script.ExecuteWithSudo {
		execCmd = "sudo " + remotePath
	}
	result, err := node.SSHExec(ctx, execCmd)
	endTime := time.Now().UnixMilli()

	if err != nil {
		// SSH execution error (network error, etc.)
		recordResult(ctx, taskID, nodeID, nodeIndex, "failed", "", "", -1, fmt.Sprintf("SSH execution error: %v", err), &startTime, &endTime)
		return fmt.Errorf("ssh exec failed on node %d: %w", nodeID, err)
	}

	// Record execution result (success or script error)
	status := "success"
	if result.ExitCode != 0 {
		status = "failed"
	}
	recordResult(ctx, taskID, nodeID, nodeIndex, status, result.Stdout, result.Stderr, result.ExitCode, "", &startTime, &endTime)

	log.Infof(ctx, "Node %d execution completed with exit code %d", nodeID, result.ExitCode)
	return nil
}

// loadAndDecryptScript loads and decrypts script content
func loadAndDecryptScript(ctx context.Context, scriptID uint64) (*SlaveBatchScript, error) {
	var script SlaveBatchScript
	if err := db.Get().First(&script, scriptID).Error; err != nil {
		return nil, fmt.Errorf("script not found: %w", err)
	}

	// Decrypt script content
	encryptedBytes, err := base64.StdEncoding.DecodeString(script.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to decode encrypted content: %w", err)
	}

	decrypted, err := secretDecrypt(ctx, encryptedBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt script: %w", err)
	}

	script.Content = string(decrypted)
	return &script, nil
}

// recordResult records node execution result
func recordResult(ctx context.Context, taskID, nodeID uint64, nodeIndex int, status, stdout, stderr string, exitCode int, errorMsg string, startedAt, endedAt *int64) {
	result := SlaveBatchTaskResult{
		TaskID:    taskID,
		NodeID:    nodeID,
		NodeIndex: nodeIndex,
		Status:    status,
		Stdout:    stdout,
		Stderr:    stderr,
		ExitCode:  exitCode,
		Error:     errorMsg,
		StartedAt: startedAt,
		EndedAt:   endedAt,
	}

	if err := db.Get().Create(&result).Error; err != nil {
		log.Errorf(ctx, "Failed to record result for node %d: %v", nodeID, err)
	}
}

// updateTaskStatus updates task status
func updateTaskStatus(ctx context.Context, taskID uint64, status string) {
	updates := map[string]any{"status": status}
	if status == "completed" || status == "failed" {
		updates["completed_at"] = time.Now().UnixMilli()
	}
	if err := db.Get().Model(&SlaveBatchTask{}).Where(&SlaveBatchTask{ID: taskID}).Updates(updates).Error; err != nil {
		log.Errorf(ctx, "Failed to update task status: %v", err)
	}
}
