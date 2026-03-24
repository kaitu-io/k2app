package center

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
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

// ===================== Submit =====================

// SubmitApproval 提交审批请求
func SubmitApproval(c *gin.Context, action string, params any, summary string) (uint64, error) {
	if _, ok := getApprovalCallback(action); !ok {
		return 0, fmt.Errorf("approval callback not registered for action: %s", action)
	}

	actor := ReqUser(c)
	if actor == nil {
		return 0, fmt.Errorf("no authenticated user")
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return 0, fmt.Errorf("marshal params: %w", err)
	}

	approval := AdminApproval{
		RequestorID:   actor.ID,
		RequestorUUID: actor.UUID,
		RequestorName: actor.UUID,
		Action:        action,
		Params:        string(paramsJSON),
		Summary:       summary,
		Status:        "pending",
	}

	if err := db.Get().Create(&approval).Error; err != nil {
		return 0, fmt.Errorf("create approval: %w", err)
	}

	log.Infof(c, "approval submitted: id=%d action=%s by=%s", approval.ID, action, actor.UUID)

	go NotifyApprovalSubmitted(context.Background(), &approval)

	return approval.ID, nil
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
		return fmt.Errorf("approval not found: %w", err)
	}

	if approval.RequestorID == approver.ID {
		return fmt.Errorf("cannot approve own request")
	}

	now := time.Now()
	result := db.Get().Model(&AdminApproval{}).
		Where("id = ? AND status = ?", approvalID, "pending").
		Updates(map[string]any{
			"status":        "approved",
			"approver_id":   approver.ID,
			"approver_uuid": approver.UUID,
			"approver_name": approver.UUID,
			"approved_at":   now,
		})

	if result.Error != nil {
		return fmt.Errorf("update approval: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("approval already processed (conflict)")
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
		return fmt.Errorf("approval not found: %w", err)
	}

	if approval.RequestorID == approver.ID {
		return fmt.Errorf("cannot reject own request")
	}

	now := time.Now()
	result := db.Get().Model(&AdminApproval{}).
		Where("id = ? AND status = ?", approvalID, "pending").
		Updates(map[string]any{
			"status":        "rejected",
			"approver_id":   approver.ID,
			"approver_uuid": approver.UUID,
			"approver_name": approver.UUID,
			"approved_at":   now,
			"reject_reason": reason,
		})

	if result.Error != nil {
		return fmt.Errorf("update approval: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("approval already processed (conflict)")
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
		return fmt.Errorf("approval not found: %w", err)
	}

	if approval.RequestorID != user.ID {
		return fmt.Errorf("only requestor can cancel")
	}

	result := db.Get().Model(&AdminApproval{}).
		Where("id = ? AND status = ?", approvalID, "pending").
		Update("status", "cancelled")

	if result.Error != nil {
		return fmt.Errorf("update approval: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("approval already processed (conflict)")
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

	if approval.Status != "approved" {
		log.Warnf(ctx, "[APPROVAL] skip execution: id=%d status=%s (expected approved)", p.ApprovalID, approval.Status)
		return nil
	}

	cb, ok := getApprovalCallback(approval.Action)
	if !ok {
		execErr := fmt.Sprintf("no callback registered for action: %s", approval.Action)
		db.Get().Model(&approval).Updates(map[string]any{"status": "failed", "exec_error": execErr})
		return fmt.Errorf(execErr)
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

// ===================== Notification Stubs =====================

// NotifyApprovalSubmitted 通知其他 admin（stub — Task 3 实现 Slack DM）
func NotifyApprovalSubmitted(ctx context.Context, approval *AdminApproval) {
	log.Infof(ctx, "[APPROVAL] notification: new pending approval id=%d action=%s requestor=%s",
		approval.ID, approval.Action, approval.RequestorName)
}

// NotifyApprovalResult 通知发起人审批结果（stub — Task 3 实现 Slack DM）
func NotifyApprovalResult(ctx context.Context, approval *AdminApproval) {
	log.Infof(ctx, "[APPROVAL] notification: approval id=%d action=%s status=%s",
		approval.ID, approval.Action, approval.Status)
}
