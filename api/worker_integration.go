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

// =====================================================================
// Worker 集成 (基于 qtoolkit/asynq)
// =====================================================================

// 任务类型常量
const (
	TaskTypeEDMSend  = "edm:send"
	TaskTypePushSend = "push:send"
)

// EDMTaskPayload EDM 任务载荷
type EDMTaskPayload struct {
	TemplateID  uint64     `json:"templateId"`
	UserFilters UserFilter `json:"userFilters"`
}

// EDMTaskOutput EDM 任务输出
type EDMTaskOutput struct {
	BatchID      string   `json:"batchId"`
	TotalUsers   int      `json:"totalUsers"`
	SentCount    int      `json:"sentCount"`
	FailedCount  int      `json:"failedCount"`
	SkippedCount int      `json:"skippedCount"`
	FailedEmails []string `json:"failedEmails,omitempty"`
	Duration     int64    `json:"duration"`
}

// PushTaskPayload 推送任务载荷
type PushTaskPayload struct {
	UserID       uint64           `json:"userId"`       // 目标用户 ID
	Notification PushNotification `json:"notification"` // 推送通知内容
}

// InitWorker 初始化 Worker
// 注册任务处理函数
func InitWorker() {
	asynq.Handle(TaskTypeEDMSend, handleEDMTask)
	asynq.Handle(TaskTypePushSend, handlePushTask)
	asynq.Handle(TaskTypeRenewalReminder, handleRenewalReminderTask)
	asynq.Handle(TaskTypeRetailerFollowup, handleRetailerFollowupTask)

	// 注册续费提醒 Cron 任务
	// 每天北京时间 10:30 执行（UTC 02:30）
	// Cron 格式: 分 时 日 月 周
	// Unique(25h) 防止多实例重复入队同一任务
	asynq.Cron("30 2 * * *", TaskTypeRenewalReminder, nil, hibikenAsynq.Unique(25*time.Hour))

	// 注册分销商跟进提醒 Cron 任务
	// 每分钟检查一次，发送到期的 Slack 提醒
	// Unique(2min) 防止重复执行
	asynq.Cron("* * * * *", TaskTypeRetailerFollowup, nil, hibikenAsynq.Unique(2*time.Minute))

	// 注册 ECH 相关的 worker
	RegisterECHWorker()

	// 注册路由诊断 worker
	RegisterDiagnosisWorker()

	// 注册云同步 worker
	RegisterCloudWorker()

	// 注册批量脚本执行 worker
	RegisterBatchWorker()

	log.Infof(context.Background(), "[WORKER] Task handlers registered (including renewal reminder cron at 10:30 Beijing time)")
}

// RunWorker 启动 Worker 服务（阻塞）
func RunWorker() error {
	InitWorker()
	log.Infof(context.Background(), "[WORKER] Starting worker service...")
	return asynq.Run()
}

// handleEDMTask 处理 EDM 任务
func handleEDMTask(ctx context.Context, payload []byte) error {
	var p EDMTaskPayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload failed: %w", err)
	}

	// 从 context 获取 asynq task ID 作为 batchID
	batchID, _ := hibikenAsynq.GetTaskID(ctx)
	if batchID == "" {
		batchID = fmt.Sprintf("batch-%d", time.Now().UnixNano())
	}

	log.Infof(ctx, "[EDM] Starting batch=%s, templateId=%d", batchID, p.TemplateID)

	// 执行 EDM 发送逻辑
	output, err := executeEDMSend(ctx, batchID, p)
	if err != nil {
		log.Errorf(ctx, "[EDM] Batch=%s failed: %v", batchID, err)
		return err
	}

	log.Infof(ctx, "[EDM] Batch=%s completed: total=%d, sent=%d, failed=%d, skipped=%d, duration=%ds",
		batchID, output.TotalUsers, output.SentCount, output.FailedCount, output.SkippedCount, output.Duration)

	return nil
}

// executeEDMSend 执行 EDM 发送逻辑
func executeEDMSend(ctx context.Context, batchID string, p EDMTaskPayload) (*EDMTaskOutput, error) {
	startTime := time.Now()

	// 1. 验证模板存在
	var template EmailMarketingTemplate
	if err := db.Get().Where("id = ? AND is_active = ?", p.TemplateID, true).
		First(&template).Error; err != nil {
		return nil, fmt.Errorf("template not found or inactive: %w", err)
	}

	// 2. 获取目标用户
	users, err := getTargetUsersForEmailTask(ctx, p.UserFilters)
	if err != nil {
		return nil, fmt.Errorf("failed to get target users: %w", err)
	}

	if len(users) == 0 {
		log.Infof(ctx, "[EDM] No target users found for batch=%s", batchID)
		return &EDMTaskOutput{
			BatchID:    batchID,
			TotalUsers: 0,
			Duration:   int64(time.Since(startTime).Seconds()),
		}, nil
	}

	log.Infof(ctx, "[EDM] Found %d target users for batch=%s", len(users), batchID)

	// 3. 发送邮件
	output := sendEmailsWithTracking(ctx, batchID, users, &template)

	return &EDMTaskOutput{
		BatchID:      batchID,
		TotalUsers:   len(users),
		SentCount:    output.SentCount,
		FailedCount:  output.FailedCount,
		SkippedCount: output.SkippedCount,
		FailedEmails: output.FailedEmails,
		Duration:     int64(time.Since(startTime).Seconds()),
	}, nil
}

// =====================================================================
// 任务入队 API
// =====================================================================

// EnqueueEDMTask 入队 EDM 任务
func EnqueueEDMTask(ctx context.Context, templateID uint64, userFilters UserFilter, scheduledAt *time.Time) (string, error) {
	payload := EDMTaskPayload{
		TemplateID:  templateID,
		UserFilters: userFilters,
	}

	var info *asynq.TaskInfo
	var err error

	if scheduledAt != nil && scheduledAt.After(time.Now()) {
		// 定时任务
		info, err = asynq.EnqueueAt(TaskTypeEDMSend, payload, *scheduledAt)
	} else {
		// 立即执行
		info, err = asynq.Enqueue(TaskTypeEDMSend, payload)
	}

	if err != nil {
		return "", fmt.Errorf("enqueue task failed: %w", err)
	}

	log.Infof(ctx, "[EDM] Task enqueued: batchId=%s, templateId=%d", info.ID, templateID)
	return info.ID, nil
}

// =====================================================================
// 推送任务
// =====================================================================

// handlePushTask 处理推送任务
func handlePushTask(ctx context.Context, payload []byte) error {
	var p PushTaskPayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload failed: %w", err)
	}

	taskID, _ := hibikenAsynq.GetTaskID(ctx)
	log.Infof(ctx, "[PUSH] Processing task=%s, userId=%d", taskID, p.UserID)

	// 查找用户的所有活跃推送令牌
	var tokens []PushToken
	if err := db.Get().Where("user_id = ? AND status = ?", p.UserID, PushTokenStatusActive).Find(&tokens).Error; err != nil {
		log.Errorf(ctx, "[PUSH] Failed to query push tokens: %v", err)
		return fmt.Errorf("failed to query push tokens: %w", err)
	}

	if len(tokens) == 0 {
		log.Infof(ctx, "[PUSH] No active push tokens for user %d", p.UserID)
		return nil
	}

	// 向每个设备发送推送
	var successCount, failCount int
	for _, token := range tokens {
		if err := sendPushToToken(ctx, &token, &p.Notification); err != nil {
			log.Warnf(ctx, "[PUSH] Failed to send to device=%s: %v", token.DeviceUDID, err)
			failCount++
			continue
		}
		successCount++
	}

	log.Infof(ctx, "[PUSH] Task=%s completed: userId=%d, success=%d, failed=%d",
		taskID, p.UserID, successCount, failCount)
	return nil
}

// EnqueuePushTask 入队推送任务
func EnqueuePushTask(ctx context.Context, userID uint64, notification PushNotification) (string, error) {
	payload := PushTaskPayload{
		UserID:       userID,
		Notification: notification,
	}

	info, err := asynq.Enqueue(TaskTypePushSend, payload)
	if err != nil {
		return "", fmt.Errorf("enqueue push task failed: %w", err)
	}

	log.Infof(ctx, "[PUSH] Task enqueued: taskId=%s, userId=%d", info.ID, userID)
	return info.ID, nil
}
