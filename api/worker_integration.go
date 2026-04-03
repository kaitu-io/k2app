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
	TaskTypePushSend           = "push:send"
	TaskTypeTemplatedEmailSend = "edm:send-templated"
)

// TemplatedEmailTaskPayload 通用邮件发送任务载荷
type TemplatedEmailTaskPayload struct {
	Request SendEmailsRequest `json:"request"`
}

// PushTaskPayload 推送任务载荷
type PushTaskPayload struct {
	UserID       uint64           `json:"userId"`       // 目标用户 ID
	Notification PushNotification `json:"notification"` // 推送通知内容
}

// InitWorker 初始化 Worker
// 注册任务处理函数
func InitWorker() {
	asynq.Handle(TaskTypePushSend, handlePushTask)
	asynq.Handle(TaskTypeTemplatedEmailSend, handleTemplatedEmailTask)
	asynq.Handle(TaskTypeRenewalReminder, handleRenewalReminderTask)
	asynq.Handle(TaskTypeRetailerFollowup, handleRetailerFollowupTask)
	asynq.Handle(TaskTypeTicketNotify, handleTicketNotify)

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

	// 审批执行 handler
	asynq.Handle(TaskTypeApprovalExecute, ExecuteApproval)

	// 审批 callback 注册
	RegisterApprovalCallback("edm_send", executeApprovalEDMSend)
	RegisterApprovalCallback("campaign_create", executeApprovalCampaignCreate)
	RegisterApprovalCallback("campaign_update", executeApprovalCampaignUpdate)
	RegisterApprovalCallback("campaign_delete", executeApprovalCampaignDelete)
	RegisterApprovalCallback("license_key_batch_create", executeApprovalLicenseKeyBatchCreate)
	RegisterApprovalCallback("license_key_batch_invalidate", executeApprovalLicenseKeyBatchInvalidate)
	RegisterApprovalCallback("user_hard_delete", executeApprovalUserHardDelete)
	RegisterApprovalCallback("plan_update", executeApprovalPlanUpdate)
	RegisterApprovalCallback("plan_delete", executeApprovalPlanDelete)
	RegisterApprovalCallback("withdraw_approve", executeApprovalWithdrawApprove)
	RegisterApprovalCallback("withdraw_complete", executeApprovalWithdrawComplete)

	log.Infof(context.Background(), "[WORKER] Task handlers registered (including renewal reminder cron at 10:30 Beijing time)")
}

// RunWorker 启动 Worker 服务（阻塞）
func RunWorker() error {
	InitWorker()
	log.Infof(context.Background(), "[WORKER] Starting worker service...")
	return asynq.Run()
}

// =====================================================================
// 任务入队 API
// =====================================================================

// EnqueueTemplatedEmailTask 入队通用邮件发送任务
func EnqueueTemplatedEmailTask(ctx context.Context, req *SendEmailsRequest) (string, error) {
	payload := TemplatedEmailTaskPayload{Request: *req}

	info, err := asynq.Enqueue(TaskTypeTemplatedEmailSend, payload)
	if err != nil {
		return "", fmt.Errorf("enqueue task failed: %w", err)
	}

	log.Infof(ctx, "[EMAIL-SEND] Task enqueued: batchId=%s, items=%d", req.BatchID, len(req.Items))
	return info.ID, nil
}

// handleTemplatedEmailTask 处理通用邮件发送任务
func handleTemplatedEmailTask(ctx context.Context, payload []byte) error {
	var p TemplatedEmailTaskPayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload failed: %w", err)
	}

	log.Infof(ctx, "[EMAIL-SEND] Processing async batch=%s, items=%d", p.Request.BatchID, len(p.Request.Items))

	result, err := SendTemplatedEmails(ctx, &p.Request)
	if err != nil {
		log.Errorf(ctx, "[EMAIL-SEND] Batch %s failed: %v", p.Request.BatchID, err)
		return err
	}

	log.Infof(ctx, "[EMAIL-SEND] Batch %s completed: sent=%d, failed=%d, skipped=%d",
		result.BatchID, result.Sent, result.Failed, result.Skipped)
	return nil
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
