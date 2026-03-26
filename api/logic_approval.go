package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
)

// ===================== Sentinel Errors =====================

var (
	ErrApprovalNotFound   = errors.New("approval not found")
	ErrApprovalConflict   = errors.New("approval already processed")
	ErrApprovalSelfAction = errors.New("cannot approve/reject own request")
	ErrApprovalNotOwner   = errors.New("only requestor can cancel")
)

// ===================== Callback Registry =====================

// ApprovalCallback 审批通过后的执行函数
type ApprovalCallback func(ctx context.Context, params json.RawMessage) error

var (
	approvalRegistry   = map[string]ApprovalCallback{}
	approvalRegistryMu sync.RWMutex
)

// RegisterApprovalCallback 注册审批回调（InitWorker 时调用）
func RegisterApprovalCallback(action string, cb ApprovalCallback) {
	approvalRegistryMu.Lock()
	defer approvalRegistryMu.Unlock()
	approvalRegistry[action] = cb
}

func getApprovalCallback(action string) (ApprovalCallback, bool) {
	approvalRegistryMu.RLock()
	defer approvalRegistryMu.RUnlock()
	cb, ok := approvalRegistry[action]
	return cb, ok
}

// ===================== Action Display Names =====================

var actionDisplayNames = map[string]string{
	"edm_create_task":     "创建 EDM 邮件任务",
	"campaign_create":     "创建优惠活动",
	"campaign_update":     "修改优惠活动",
	"campaign_delete":     "删除优惠活动",
	"campaign_issue_keys": "发放 License Key",
	"user_hard_delete":    "硬删除用户",
	"plan_update":         "修改订阅套餐",
	"plan_delete":         "删除订阅套餐",
	"withdraw_approve":    "审批提现",
	"withdraw_complete":   "完成提现",
}

func actionDisplayName(action string) string {
	if name, ok := actionDisplayNames[action]; ok {
		return name
	}
	return action
}

// ===================== Asynq Task =====================

const TaskTypeApprovalExecute = "approval:execute"

type ApprovalExecutePayload struct {
	ApprovalID uint64 `json:"approvalId"`
}

// ApprovalSubmitResponse 审批提交响应（各 handler 共用）
type ApprovalSubmitResponse struct {
	ApprovalID uint64 `json:"approvalId"`
	Status     string `json:"status"`
}

// ===================== Submit =====================

// SubmitApproval 提交审批请求
// 返回 (approvalID, executed, error)：
//   - is_admin 超管：同步执行 callback，executed=true
//   - 非超管：创建 pending 记录，executed=false
//
// Handler 用法：
//
//	approvalID, executed, err := SubmitApproval(c, action, params, summary)
//	if !executed { PendingApproval(c, approvalID); return }
//	Success(c, data) // 超管直通成功
func SubmitApproval(c *gin.Context, action string, params any, summary string) (uint64, bool, error) {
	cb, ok := getApprovalCallback(action)
	if !ok {
		return 0, false, fmt.Errorf("approval callback not registered for action: %s", action)
	}

	actor := ReqUser(c)
	if actor == nil {
		return 0, false, fmt.Errorf("no authenticated user")
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return 0, false, fmt.Errorf("marshal params: %w", err)
	}

	// 用邮箱作为显示名（fallback UUID）
	requestorName := actor.UUID
	if email := getAdminEmail(c.Request.Context(), actor.ID); email != "" {
		requestorName = email
	}

	isSuperAdmin := actor.IsAdmin != nil && *actor.IsAdmin

	// 超管直通：同步执行，不走异步审批
	if isSuperAdmin {
		now := time.Now()
		approval := AdminApproval{
			RequestorID:   actor.ID,
			RequestorUUID: actor.UUID,
			RequestorName: requestorName,
			Action:        action,
			Params:        string(paramsJSON),
			Summary:       summary,
			Status:        "executed",
			ApproverID:    &actor.ID,
			ApproverUUID:  &actor.UUID,
			ApproverName:  &requestorName,
			ApprovedAt:    &now,
			ExecutedAt:    &now,
		}

		if err := db.Get().Create(&approval).Error; err != nil {
			return 0, false, fmt.Errorf("create approval: %w", err)
		}

		// 同步执行 callback
		if err := cb(c.Request.Context(), json.RawMessage(paramsJSON)); err != nil {
			execErr := err.Error()
			db.Get().Model(&approval).Updates(map[string]any{"status": "failed", "exec_error": execErr})
			return 0, false, fmt.Errorf("execute action: %w", err)
		}

		log.Infof(c, "approval auto-executed by superadmin: id=%d action=%s by=%s", approval.ID, action, actor.UUID)
		WriteAuditLogFromApproval(c.Request.Context(), &approval)
		return approval.ID, true, nil
	}

	// 非超管：创建 pending 记录，走异步审批
	approval := AdminApproval{
		RequestorID:   actor.ID,
		RequestorUUID: actor.UUID,
		RequestorName: requestorName,
		Action:        action,
		Params:        string(paramsJSON),
		Summary:       summary,
		Status:        "pending",
	}

	if err := db.Get().Create(&approval).Error; err != nil {
		return 0, false, fmt.Errorf("create approval: %w", err)
	}

	log.Infof(c, "approval submitted: id=%d action=%s by=%s", approval.ID, action, actor.UUID)

	go NotifyApprovalSubmitted(context.Background(), &approval)

	return approval.ID, false, nil
}

// ===================== Approve =====================

// ApproveApproval 审批通过
func ApproveApproval(c *gin.Context, approvalID uint64) error {
	approver := ReqUser(c)
	if approver == nil {
		return fmt.Errorf("no authenticated user")
	}

	var approval AdminApproval
	if err := db.Get().First(&approval, approvalID).Error; err != nil {
		return fmt.Errorf("%w: %v", ErrApprovalNotFound, err)
	}

	if approval.RequestorID == approver.ID {
		return ErrApprovalSelfAction
	}

	approverName := approver.UUID
	if email := getAdminEmail(c.Request.Context(), approver.ID); email != "" {
		approverName = email
	}

	now := time.Now()
	result := db.Get().Model(&AdminApproval{}).
		Where("id = ? AND status = ?", approvalID, "pending").
		Updates(map[string]any{
			"status":        "approved",
			"approver_id":   approver.ID,
			"approver_uuid": approver.UUID,
			"approver_name": approverName,
			"approved_at":   now,
		})

	if result.Error != nil {
		return fmt.Errorf("update approval: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrApprovalConflict
	}

	db.Get().First(&approval, approvalID)

	log.Infof(c, "approval approved: id=%d action=%s by=%s", approvalID, approval.Action, approver.UUID)

	payload := ApprovalExecutePayload{ApprovalID: approvalID}
	if _, err := asynq.Enqueue(TaskTypeApprovalExecute, payload); err != nil {
		log.Errorf(c, "failed to enqueue approval execution: id=%d err=%v", approvalID, err)
		return fmt.Errorf("enqueue execution: %w", err)
	}

	go NotifyApprovalResult(context.Background(), &approval)

	return nil
}

// ===================== Reject =====================

// RejectApproval 拒绝审批
func RejectApproval(c *gin.Context, approvalID uint64, reason string) error {
	approver := ReqUser(c)
	if approver == nil {
		return fmt.Errorf("no authenticated user")
	}

	var approval AdminApproval
	if err := db.Get().First(&approval, approvalID).Error; err != nil {
		return fmt.Errorf("%w: %v", ErrApprovalNotFound, err)
	}

	if approval.RequestorID == approver.ID {
		return ErrApprovalSelfAction
	}

	approverName := approver.UUID
	if email := getAdminEmail(c.Request.Context(), approver.ID); email != "" {
		approverName = email
	}

	result := db.Get().Model(&AdminApproval{}).
		Where("id = ? AND status = ?", approvalID, "pending").
		Updates(map[string]any{
			"status":        "rejected",
			"approver_id":   approver.ID,
			"approver_uuid": approver.UUID,
			"approver_name": approverName,
			"reject_reason": reason,
		})

	if result.Error != nil {
		return fmt.Errorf("update approval: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrApprovalConflict
	}

	db.Get().First(&approval, approvalID)
	log.Infof(c, "approval rejected: id=%d action=%s by=%s reason=%s", approvalID, approval.Action, approver.UUID, reason)

	go NotifyApprovalResult(context.Background(), &approval)
	return nil
}

// ===================== Cancel =====================

// CancelApproval 发起人取消
func CancelApproval(c *gin.Context, approvalID uint64) error {
	user := ReqUser(c)
	if user == nil {
		return fmt.Errorf("no authenticated user")
	}

	var approval AdminApproval
	if err := db.Get().First(&approval, approvalID).Error; err != nil {
		return fmt.Errorf("%w: %v", ErrApprovalNotFound, err)
	}

	if approval.RequestorID != user.ID {
		return ErrApprovalNotOwner
	}

	result := db.Get().Model(&AdminApproval{}).
		Where("id = ? AND status = ?", approvalID, "pending").
		Update("status", "cancelled")

	if result.Error != nil {
		return fmt.Errorf("update approval: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrApprovalConflict
	}

	log.Infof(c, "approval cancelled: id=%d action=%s by=%s", approvalID, approval.Action, user.UUID)
	return nil
}

// ===================== Execute (Asynq Handler) =====================

// ExecuteApproval Asynq task handler
func ExecuteApproval(ctx context.Context, payload []byte) error {
	var p ApprovalExecutePayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	taskID, _ := hibikenAsynq.GetTaskID(ctx)
	log.Infof(ctx, "[APPROVAL] executing: approvalId=%d taskId=%s", p.ApprovalID, taskID)

	var approval AdminApproval
	if err := db.Get().First(&approval, p.ApprovalID).Error; err != nil {
		return fmt.Errorf("approval %d not found: %w", p.ApprovalID, err)
	}

	// Atomic guard: claim this approval for execution (prevents double-execution on Asynq retry)
	result := db.Get().Model(&AdminApproval{}).
		Where("id = ? AND status = ?", p.ApprovalID, "approved").
		Update("status", "executing")
	if result.RowsAffected == 0 {
		log.Warnf(ctx, "[APPROVAL] skip execution: id=%d status=%s (already executing/executed or not approved)", p.ApprovalID, approval.Status)
		return nil
	}

	cb, ok := getApprovalCallback(approval.Action)
	if !ok {
		execErr := fmt.Sprintf("no callback registered for action: %s", approval.Action)
		db.Get().Model(&approval).Updates(map[string]any{"status": "failed", "exec_error": execErr})
		return nil // 不 retry — callback 结构性缺失，重试无意义
	}

	if err := cb(ctx, json.RawMessage(approval.Params)); err != nil {
		execErr := err.Error()
		now := time.Now()
		db.Get().Model(&approval).Updates(map[string]any{
			"status":      "failed",
			"executed_at": now,
			"exec_error":  execErr,
		})
		log.Errorf(ctx, "[APPROVAL] execution failed: id=%d action=%s err=%v", p.ApprovalID, approval.Action, err)
		approval.Status = "failed"
		approval.ExecError = &execErr
		go NotifyApprovalResult(context.Background(), &approval)
		return nil
	}

	now := time.Now()
	db.Get().Model(&approval).Updates(map[string]any{
		"status":      "executed",
		"executed_at": now,
	})

	log.Infof(ctx, "[APPROVAL] executed successfully: id=%d action=%s", p.ApprovalID, approval.Action)

	WriteAuditLogFromApproval(ctx, &approval)

	approval.Status = "executed"
	go NotifyApprovalResult(context.Background(), &approval)

	return nil
}

// ===================== Audit Log =====================

// WriteAuditLogFromApproval 从审批记录写审计日志（Asynq context 用）
func WriteAuditLogFromApproval(ctx context.Context, approval *AdminApproval) {
	detail := map[string]any{
		"approvalId": approval.ID,
		"params":     json.RawMessage(approval.Params),
	}
	if approval.ApproverID != nil {
		detail["approverId"] = *approval.ApproverID
		detail["approverUuid"] = *approval.ApproverUUID
	}

	detailJSON, _ := json.Marshal(detail)

	entry := AdminAuditLog{
		ActorID:    approval.RequestorID,
		ActorUUID:  approval.RequestorUUID,
		Action:     approval.Action,
		TargetType: "approval",
		TargetID:   fmt.Sprintf("%d", approval.ID),
		Detail:     string(detailJSON),
	}

	if err := db.Get().Create(&entry).Error; err != nil {
		log.Errorf(ctx, "failed to write audit log from approval: id=%d err=%v", approval.ID, err)
	}
}

// getAdminEmail resolves an admin's email address by user ID.
// Reuses getUserEmail (api_ticket.go) — same logic, just swallows errors for best-effort notifications.
func getAdminEmail(ctx context.Context, userID uint64) string {
	email, err := getUserEmail(ctx, userID)
	if err != nil {
		log.Warnf(ctx, "[APPROVAL] resolve email for user %d: %v", userID, err)
		return ""
	}
	return email
}

// ===================== Notifications =====================

// NotifyApprovalSubmitted 通知其他 admin 有新的审批请求
func NotifyApprovalSubmitted(ctx context.Context, approval *AdminApproval) {
	log.Infof(ctx, "[APPROVAL] notification: new pending approval id=%d action=%s requestor=%s",
		approval.ID, approval.Action, approval.RequestorName)

	var admins []User
	if err := db.Get().Where("is_admin = ? AND id != ?", true, approval.RequestorID).Find(&admins).Error; err != nil {
		log.Errorf(ctx, "[APPROVAL] failed to query admins: %v", err)
		return
	}

	displayName := actionDisplayName(approval.Action)
	message := fmt.Sprintf("🔒 新的审批请求\n操作：%s\n发起人：%s\n摘要：%s\n时间：%s\n👉 前往审批：https://kaitu.io/manager/approvals",
		displayName,
		approval.RequestorName,
		approval.Summary,
		approval.CreatedAt.Format("2006-01-02 15:04:05"),
	)

	for _, admin := range admins {
		email := getAdminEmail(ctx, admin.ID)
		if email == "" {
			continue
		}
		if err := slack.SendDM(email, message); err != nil {
			log.Warnf(ctx, "[APPROVAL] failed to send Slack DM to admin %d: %v", admin.ID, err)
		}
	}
}

// NotifyApprovalResult 通知发起人审批结果
func NotifyApprovalResult(ctx context.Context, approval *AdminApproval) {
	log.Infof(ctx, "[APPROVAL] notification: approval id=%d action=%s status=%s",
		approval.ID, approval.Action, approval.Status)

	email := getAdminEmail(ctx, approval.RequestorID)
	if email == "" {
		return
	}

	displayName := actionDisplayName(approval.Action)
	var message string

	switch approval.Status {
	case "approved":
		message = fmt.Sprintf("✅ 审批已通过\n操作：%s\n摘要：%s\n正在执行中…", displayName, approval.Summary)
	case "rejected":
		reason := ""
		if approval.RejectReason != nil && *approval.RejectReason != "" {
			reason = fmt.Sprintf("\n原因：%s", *approval.RejectReason)
		}
		message = fmt.Sprintf("❌ 审批已拒绝\n操作：%s\n摘要：%s%s", displayName, approval.Summary, reason)
	case "executed":
		message = fmt.Sprintf("🎉 审批已执行完成\n操作：%s\n摘要：%s", displayName, approval.Summary)
	case "failed":
		execErr := ""
		if approval.ExecError != nil && *approval.ExecError != "" {
			execErr = fmt.Sprintf("\n错误：%s", *approval.ExecError)
		}
		message = fmt.Sprintf("⚠️ 审批执行失败\n操作：%s\n摘要：%s%s", displayName, approval.Summary, execErr)
	default:
		return
	}

	if err := slack.SendDM(email, message); err != nil {
		log.Warnf(ctx, "[APPROVAL] failed to send Slack DM to requestor %d: %v", approval.RequestorID, err)
	}
}
