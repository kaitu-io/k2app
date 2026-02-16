package center

import (
	"context"
	"fmt"
	"time"

	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// ========================= Batch Script Execution Worker =========================

// Task type constants
const (
	TaskTypeSlaveBatchExec = "slave:batch:exec"
)

// BatchTaskPayload batch execution task payload
type BatchTaskPayload struct {
	TaskID uint64 `json:"taskId"`
}

// RegisterBatchWorker registers batch execution worker
func RegisterBatchWorker() {
	ctx := context.Background()
	asynq.Handle(TaskTypeSlaveBatchExec, handleBatchTask)
	log.Infof(ctx, "[WORKER] Batch execution worker registered")

	// Register all cron tasks from database
	if err := RegisterBatchCronTasks(ctx); err != nil {
		log.Errorf(ctx, "[WORKER] Failed to register batch cron tasks: %v", err)
		// Non-fatal error, continue worker initialization
	}
}

// handleBatchTask handles batch execution task
func handleBatchTask(ctx context.Context, payload []byte) error {
	var p BatchTaskPayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload failed: %w", err)
	}

	log.Infof(ctx, "[BATCH] Starting task execution: taskID=%d", p.TaskID)

	// Execute batch task
	if err := ExecuteBatchTask(ctx, p.TaskID); err != nil {
		log.Errorf(ctx, "[BATCH] Task execution failed: taskID=%d, error=%v", p.TaskID, err)
		return fmt.Errorf("batch task execution failed: %w", err)
	}

	log.Infof(ctx, "[BATCH] Task execution completed: taskID=%d", p.TaskID)
	return nil
}

// EnqueueBatchTaskNow enqueues a batch task for immediate execution
func EnqueueBatchTaskNow(ctx context.Context, taskID uint64) (*hibikenAsynq.TaskInfo, error) {
	payload := BatchTaskPayload{TaskID: taskID}
	info, err := asynq.Enqueue(TaskTypeSlaveBatchExec, payload)
	if err != nil {
		return nil, fmt.Errorf("enqueue batch task failed: %w", err)
	}
	log.Infof(ctx, "[BATCH] Task enqueued: taskID=%d, asynqID=%s", taskID, info.ID)
	return info, nil
}

// EnqueueBatchTaskAt enqueues a batch task for execution at a specific time
func EnqueueBatchTaskAt(ctx context.Context, taskID uint64, executeAt time.Time) (*hibikenAsynq.TaskInfo, error) {
	payload := BatchTaskPayload{TaskID: taskID}
	info, err := asynq.EnqueueAt(TaskTypeSlaveBatchExec, payload, executeAt)
	if err != nil {
		return nil, fmt.Errorf("enqueue batch task at %v failed: %w", executeAt, err)
	}
	log.Infof(ctx, "[BATCH] Task scheduled: taskID=%d, asynqID=%s, executeAt=%v", taskID, info.ID, executeAt)
	return info, nil
}

// RegisterBatchCronTasks registers cron tasks from database
// This should be called during worker startup to load all active cron tasks
func RegisterBatchCronTasks(ctx context.Context) error {
	var tasks []SlaveBatchTask

	// Query all cron tasks that are pending or running
	if err := db.Get().Where(&SlaveBatchTask{ScheduleType: "cron"}).
		Where("status IN ?", []string{"pending", "running"}).
		Find(&tasks).Error; err != nil {
		log.Errorf(ctx, "[BATCH] Failed to load cron tasks: %v", err)
		return fmt.Errorf("failed to load cron tasks: %w", err)
	}

	if len(tasks) == 0 {
		log.Infof(ctx, "[BATCH] No active cron tasks to register")
		return nil
	}

	// Register each cron task
	for _, task := range tasks {
		if task.CronExpr == "" {
			log.Warnf(ctx, "[BATCH] Task %d has empty cron expression, skipping", task.ID)
			continue
		}

		// Create payload for this specific task
		payload := BatchTaskPayload{TaskID: task.ID}

		// Register with Asynq
		// Use Unique option to prevent duplicate cron entries
		asynq.Cron(task.CronExpr, TaskTypeSlaveBatchExec, payload, hibikenAsynq.Unique(24*time.Hour))

		log.Infof(ctx, "[BATCH] Registered cron task %d: %s", task.ID, task.CronExpr)
	}

	log.Infof(ctx, "[BATCH] Successfully registered %d cron tasks", len(tasks))
	return nil
}
