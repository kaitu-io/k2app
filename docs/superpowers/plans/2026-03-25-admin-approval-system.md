# Admin Approval System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add maker-checker approval for dangerous admin operations, with Slack DM notifications and comprehensive audit logging for all admin writes.

**Architecture:** Handler-embedded approval service. Critical handlers submit to `AdminApproval` table instead of executing directly. Asynq callback executes after another admin approves. Slack DM via direct Web API calls (not qtoolkit/slack).

**Tech Stack:** Go/Gin (Center API), GORM (MySQL), Asynq (task queue), Slack Web API (net/http), Next.js/React (admin frontend)

**Spec:** `docs/superpowers/specs/2026-03-25-admin-approval-system-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `api/model.go` | Add `AdminApproval` struct (after `AdminAuditLog`) |
| `api/migrate.go` | Add `&AdminApproval{}` to AutoMigrate list |
| `api/logic_approval.go` | **New.** Callback registry, Submit/Approve/Reject/Cancel/Execute, WriteAuditLogFromApproval, Slack DM (SlackDMByEmail, NotifyApprovalSubmitted, NotifyApprovalResult), action display name map |
| `api/api_admin_approval.go` | **New.** 5 HTTP handlers: list, detail, approve, reject, cancel |
| `api/route.go` | Register `/app/approvals` routes |
| `api/worker_integration.go` | Register `approval:execute` Asynq handler + 10 callbacks |
| `api/api_admin_edm.go` | Refactor `create_edm_task` → SubmitApproval |
| `api/api_admin_campaigns.go` | Refactor create/update/delete/issue-keys (4 handlers) → SubmitApproval |
| `api/api_admin_user.go` | Refactor `hard_delete_users` → SubmitApproval |
| `api/api_admin_plan.go` | Refactor `update_plan` / `delete_plan` → SubmitApproval |
| `api/api_admin_wallet.go` | Refactor `approve_withdraw` / `complete_withdraw` → SubmitApproval |
| `api/api_admin_*.go` (30+ files) | Add `WriteAuditLog()` calls to all normal admin write handlers |
| `web/src/lib/api.ts` | Add approval API methods |
| `web/src/app/(manager)/manager/approvals/page.tsx` | **New.** Approval list page |
| `web/src/components/manager-sidebar.tsx` | Add "审批管理" nav item with pending badge |

---

## Task 1: Data Model + Migration

**Files:**
- Modify: `api/model.go` (after `AdminAuditLog` at line 542)
- Modify: `api/migrate.go` (after `&AdminAuditLog{}` at line 60)

- [ ] **Step 1: Add AdminApproval model to model.go**

Add after the `AdminAuditLog` struct (line 542):

```go
// AdminApproval 管理员操作审批记录
// 不含 DeletedAt — 审计记录不可删除
type AdminApproval struct {
	ID        uint64    `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `gorm:"index:idx_approval_status_time" json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// 发起人
	RequestorID   uint64 `gorm:"not null;index" json:"requestorId"`
	RequestorUUID string `gorm:"type:varchar(255);not null" json:"requestorUuid"`
	RequestorName string `gorm:"type:varchar(255);not null" json:"requestorName"`

	// 操作标识（注册表 key）
	Action string `gorm:"type:varchar(64);not null;index:idx_approval_action_status" json:"action"`

	// handler 校验后的干净参数（JSON）
	Params string `gorm:"type:text;not null" json:"params"`

	// 人类可读摘要
	Summary string `gorm:"type:text;not null" json:"summary"`

	// 审批状态: pending, approved, executed, failed, rejected, cancelled
	Status string `gorm:"type:varchar(16);not null;default:pending;index:idx_approval_status_time;index:idx_approval_action_status" json:"status"`

	// 审批人
	ApproverID   *uint64    `gorm:"index" json:"approverId,omitempty"`
	ApproverUUID *string    `gorm:"type:varchar(255)" json:"approverUuid,omitempty"`
	ApproverName *string    `gorm:"type:varchar(255)" json:"approverName,omitempty"`
	ApprovedAt   *time.Time `json:"approvedAt,omitempty"`
	RejectReason *string    `gorm:"type:varchar(512)" json:"rejectReason,omitempty"`

	// 执行结果
	ExecutedAt *time.Time `json:"executedAt,omitempty"`
	ExecError  *string    `gorm:"type:text" json:"execError,omitempty"`
}
```

- [ ] **Step 2: Add to AutoMigrate in migrate.go**

After `&AdminAuditLog{}` (line 60), add:
```go
		// Admin approval system
		&AdminApproval{},
```

- [ ] **Step 3: Verify migration runs**

Run: `cd api && go build ./...`
Expected: compiles without errors.

- [ ] **Step 4: Commit**

```bash
git add api/model.go api/migrate.go
git commit -m "feat(api): add AdminApproval model and migration"
```

---

## Task 2: Approval Service Layer — Core Logic

**Files:**
- Create: `api/logic_approval.go`

This is the largest file. It contains: callback registry, action display names, SubmitApproval, ApproveApproval, RejectApproval, CancelApproval, ExecuteApproval (Asynq handler), WriteAuditLogFromApproval.

Slack DM is added in Task 3 separately — for now the notify functions are stubs that only log.

- [ ] **Step 1: Create logic_approval.go with callback registry + action names**

```go
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

// ApprovalCallback 审批通过后的执行函数
type ApprovalCallback func(ctx context.Context, params json.RawMessage) error

// 全局注册表
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

// Action 可读名映射（Slack 通知和前端展示用）
var actionDisplayNames = map[string]string{
	"edm_create_task":      "创建 EDM 邮件任务",
	"campaign_create":      "创建优惠活动",
	"campaign_update":      "修改优惠活动",
	"campaign_delete":      "删除优惠活动",
	"campaign_issue_keys":  "发放 License Key",
	"user_hard_delete":     "硬删除用户",
	"plan_update":          "修改订阅套餐",
	"plan_delete":          "删除订阅套餐",
	"withdraw_approve":     "审批提现",
	"withdraw_complete":    "完成提现",
}

func actionDisplayName(action string) string {
	if name, ok := actionDisplayNames[action]; ok {
		return name
	}
	return action
}

// Asynq task type
const TaskTypeApprovalExecute = "approval:execute"

type ApprovalExecutePayload struct {
	ApprovalID uint64 `json:"approvalId"`
}
```

- [ ] **Step 2: Add SubmitApproval function**

Append to `logic_approval.go`:

```go
// SubmitApproval 提交审批请求
func SubmitApproval(c *gin.Context, action string, params any, summary string) (uint64, error) {
	// 校验 action 已注册
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

	// 异步通知（best-effort）
	go NotifyApprovalSubmitted(context.Background(), &approval)

	return approval.ID, nil
}
```

Note: `User` has no `DisplayName()` method. Use `actor.UUID` as requestor/approver name throughout. It's the unique human-readable identifier available on the User struct without extra DB queries. If a friendlier name is desired later, the UUID-based display is still correct and the field can be updated.

- [ ] **Step 3: Add ApproveApproval function**

```go
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

	// 刷新完整记录用于通知
	db.Get().First(&approval, approvalID)

	log.Infof(c, "approval approved: id=%d action=%s by=%s", approvalID, approval.Action, approver.UUID)

	// 入队执行
	payload := ApprovalExecutePayload{ApprovalID: approvalID}
	if _, err := asynq.Enqueue(TaskTypeApprovalExecute, payload); err != nil {
		log.Errorf(c, "failed to enqueue approval execution: id=%d err=%v", approvalID, err)
		return fmt.Errorf("enqueue execution: %w", err)
	}

	// 异步通知发起人
	go NotifyApprovalResult(context.Background(), &approval)

	return nil
}
```

- [ ] **Step 4: Add RejectApproval and CancelApproval functions**

```go
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
```

- [ ] **Step 5: Add ExecuteApproval (Asynq handler) and WriteAuditLogFromApproval**

```go
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

	// 执行 callback
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
		return nil // 不 retry，标记 failed 即可
	}

	now := time.Now()
	db.Get().Model(&approval).Updates(map[string]any{
		"status":      "executed",
		"executed_at": now,
	})

	log.Infof(ctx, "[APPROVAL] executed successfully: id=%d action=%s", p.ApprovalID, approval.Action)

	// 写审计日志
	WriteAuditLogFromApproval(ctx, &approval)

	// 通知发起人
	approval.Status = "executed"
	go NotifyApprovalResult(context.Background(), &approval)

	return nil
}

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
```

- [ ] **Step 6: Add Slack notification stubs**

For now, just log. Task 3 will implement actual Slack DM.

```go
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
```

- [ ] **Step 7: Verify compilation**

Run: `cd api && go build ./...`
Expected: compiles without errors.

- [ ] **Step 8: Commit**

```bash
git add api/logic_approval.go
git commit -m "feat(api): add approval service layer with callback registry and execution"
```

---

## Task 3: Slack DM Notifications

**Files:**
- Modify: `api/logic_approval.go` (replace notification stubs)

- [ ] **Step 1: Add Slack DM implementation**

Replace the two stub functions and add SlackDMByEmail. Add these above the stubs:

```go
// ===================== Slack DM 通知 =====================

var (
	slackUserIDCache   = map[string]string{} // email → slack user ID
	slackUserIDCacheMu sync.RWMutex
)

// SlackDMByEmail 通过邮箱给个人发 Slack DM
func SlackDMByEmail(ctx context.Context, email string, message string) error {
	botToken := viper.GetString("slack.bot_token")
	if botToken == "" {
		return fmt.Errorf("slack bot_token not configured")
	}

	// 1. 查缓存或 lookupByEmail
	slackUserID, err := resolveSlackUserID(ctx, botToken, email)
	if err != nil {
		return fmt.Errorf("resolve slack user for %s: %w", email, err)
	}

	// 2. conversations.open
	channelID, err := slackOpenDM(ctx, botToken, slackUserID)
	if err != nil {
		return fmt.Errorf("open DM for %s: %w", email, err)
	}

	// 3. chat.postMessage
	return slackPostMessage(ctx, botToken, channelID, message)
}

func resolveSlackUserID(ctx context.Context, botToken, email string) (string, error) {
	slackUserIDCacheMu.RLock()
	if id, ok := slackUserIDCache[email]; ok {
		slackUserIDCacheMu.RUnlock()
		return id, nil
	}
	slackUserIDCacheMu.RUnlock()

	// API call: users.lookupByEmail (GET with query param, no body)
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("https://slack.com/api/users.lookupByEmail?email=%s", url.QueryEscape(email)), nil)
	req.Header.Set("Authorization", "Bearer "+botToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		OK   bool `json:"ok"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if !result.OK {
		return "", fmt.Errorf("slack API error: %s", result.Error)
	}

	slackUserIDCacheMu.Lock()
	slackUserIDCache[email] = result.User.ID
	slackUserIDCacheMu.Unlock()

	return result.User.ID, nil
}

func slackOpenDM(ctx context.Context, botToken, userID string) (string, error) {
	body, _ := json.Marshal(map[string]string{"users": userID})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/conversations.open", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		OK      bool `json:"ok"`
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if !result.OK {
		return "", fmt.Errorf("slack API error: %s", result.Error)
	}
	return result.Channel.ID, nil
}

func slackPostMessage(ctx context.Context, botToken, channelID, text string) error {
	body, _ := json.Marshal(map[string]string{"channel": channelID, "text": text})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.postMessage", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("slack API error: %s", result.Error)
	}
	return nil
}
```

Add required imports: `"bytes"`, `"net/http"`, `"net/url"`, `"github.com/spf13/viper"`.

The codebase uses `viper.GetString()` for config access (see `logic_config.go`, `api_member.go`).

- [ ] **Step 2: Replace notification stubs with real implementations**

```go
// NotifyApprovalSubmitted 通知所有其他 admin 有新审批请求
func NotifyApprovalSubmitted(ctx context.Context, approval *AdminApproval) {
	// 查所有 admin（排除发起人）
	var admins []User
	if err := db.Get().Where("is_admin = ? AND id != ?", true, approval.RequestorID).Find(&admins).Error; err != nil {
		log.Warnf(ctx, "[APPROVAL] failed to query admins for notification: %v", err)
		return
	}

	displayName := actionDisplayName(approval.Action)
	message := fmt.Sprintf("🔒 新的审批请求\n操作：%s\n发起人：%s\n摘要：%s\n时间：%s\n👉 前往审批：https://kaitu.io/manager/approvals",
		displayName, approval.RequestorName, approval.Summary,
		approval.CreatedAt.Format("2006-01-02 15:04"))

	for _, admin := range admins {
		email := getAdminEmail(ctx, admin.ID)
		if email == "" {
			continue
		}
		if err := SlackDMByEmail(ctx, email, message); err != nil {
			log.Warnf(ctx, "[APPROVAL] slack DM failed for admin %s: %v", admin.UUID, err)
		}
	}
}

// NotifyApprovalResult 通知发起人审批结果
func NotifyApprovalResult(ctx context.Context, approval *AdminApproval) {
	email := getAdminEmail(ctx, approval.RequestorID)
	if email == "" {
		return
	}

	displayName := actionDisplayName(approval.Action)
	var message string

	switch approval.Status {
	case "approved":
		approverName := ""
		if approval.ApproverName != nil {
			approverName = *approval.ApproverName
		}
		message = fmt.Sprintf("✅ 审批已通过\n操作：%s\n审批人：%s\n你的操作正在执行中。", displayName, approverName)
	case "rejected":
		approverName := ""
		if approval.ApproverName != nil {
			approverName = *approval.ApproverName
		}
		reason := ""
		if approval.RejectReason != nil {
			reason = *approval.RejectReason
		}
		message = fmt.Sprintf("❌ 审批被拒绝\n操作：%s\n审批人：%s\n原因：%s", displayName, approverName, reason)
	case "executed":
		message = fmt.Sprintf("🎉 操作已执行\n操作：%s", displayName)
	case "failed":
		execErr := ""
		if approval.ExecError != nil {
			execErr = *approval.ExecError
		}
		message = fmt.Sprintf("⚠️ 操作执行失败\n操作：%s\n错误：%s", displayName, execErr)
	default:
		return
	}

	if err := SlackDMByEmail(ctx, email, message); err != nil {
		log.Warnf(ctx, "[APPROVAL] slack DM failed for requestor %s: %v", approval.RequestorUUID, err)
	}
}

// getAdminEmail 获取用户的邮箱（从 login_identifies 表，解密 EncryptedValue）
// 复用现有的 GetEmailIdentifyByUserID + secretDecryptString 模式
// （参见 api_ticket.go:126 getUserEmail 和 worker_renewal_reminder.go:165 getUserEmailFromIdentifies）
func getAdminEmail(ctx context.Context, userID uint64) string {
	identify, err := GetEmailIdentifyByUserID(ctx, int64(userID))
	if err != nil || identify == nil || identify.EncryptedValue == "" {
		log.Warnf(ctx, "[APPROVAL] no email for user %d: %v", userID, err)
		return ""
	}
	email, err := secretDecryptString(ctx, identify.EncryptedValue)
	if err != nil {
		log.Warnf(ctx, "[APPROVAL] decrypt email failed for user %d: %v", userID, err)
		return ""
	}
	return email
}
```

- [ ] **Step 3: Verify compilation**

Run: `cd api && go build ./...`

- [ ] **Step 4: Commit**

```bash
git add api/logic_approval.go
git commit -m "feat(api): add Slack DM notifications for approval workflow"
```

---

## Task 4: Approval API Handlers + Routes

**Files:**
- Create: `api/api_admin_approval.go`
- Modify: `api/route.go`

- [ ] **Step 1: Create api_admin_approval.go with 5 handlers**

```go
package center

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// GET /app/approvals
func api_admin_list_approvals(c *gin.Context) {
	user := ReqUser(c)
	if user == nil {
		Error(c, ErrorNotLogin, "unauthorized")
		return
	}

	pagination := PaginationFromRequest(c)
	query := db.Get().Model(&AdminApproval{})

	// 非 admin 只能看自己的
	isAdmin := user.IsAdmin != nil && *user.IsAdmin
	if !isAdmin {
		query = query.Where("requestor_id = ?", user.ID)
	}

	// status 过滤
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Count(&total)

	var approvals []AdminApproval
	query.Order("FIELD(status, 'pending') DESC, created_at DESC").
		Offset(pagination.Offset()).Limit(pagination.PageSize).
		Find(&approvals)

	pagination.Total = total
	ListWithData(c, approvals, pagination)
}

// GET /app/approvals/:id
func api_admin_get_approval(c *gin.Context) {
	user := ReqUser(c)
	if user == nil {
		Error(c, ErrorNotLogin, "unauthorized")
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	var approval AdminApproval
	if err := db.Get().First(&approval, id).Error; err != nil {
		Error(c, ErrorNotFound, "approval not found")
		return
	}

	// 非 admin 只能看自己的
	isAdmin := user.IsAdmin != nil && *user.IsAdmin
	if !isAdmin && approval.RequestorID != user.ID {
		Error(c, ErrorForbidden, "permission denied")
		return
	}

	Success(c, &approval)
}

// POST /app/approvals/:id/approve
func api_admin_approve_approval(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	if err := ApproveApproval(c, id); err != nil {
		if strings.Contains(err.Error(), "conflict") {
			Error(c, ErrorConflict, err.Error())
			return
		}
		if strings.Contains(err.Error(), "cannot approve own") {
			Error(c, ErrorForbidden, err.Error())
			return
		}
		log.Errorf(c, "approve approval failed: %v", err)
		Error(c, ErrorSystemError, err.Error())
		return
	}

	SuccessEmpty(c)
}

// POST /app/approvals/:id/reject
func api_admin_reject_approval(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "reason is required")
		return
	}

	if err := RejectApproval(c, id, req.Reason); err != nil {
		if strings.Contains(err.Error(), "conflict") {
			Error(c, ErrorConflict, err.Error())
			return
		}
		if strings.Contains(err.Error(), "cannot reject own") {
			Error(c, ErrorForbidden, err.Error())
			return
		}
		log.Errorf(c, "reject approval failed: %v", err)
		Error(c, ErrorSystemError, err.Error())
		return
	}

	SuccessEmpty(c)
}

// POST /app/approvals/:id/cancel
func api_admin_cancel_approval(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	if err := CancelApproval(c, id); err != nil {
		if strings.Contains(err.Error(), "conflict") {
			Error(c, ErrorConflict, err.Error())
			return
		}
		if strings.Contains(err.Error(), "only requestor") {
			Error(c, ErrorForbidden, err.Error())
			return
		}
		log.Errorf(c, "cancel approval failed: %v", err)
		Error(c, ErrorSystemError, err.Error())
		return
	}

	SuccessEmpty(c)
}
```

- [ ] **Step 2: Add routes to route.go**

In `route.go`, add a new approval route group. Place it after the `admin` group (around line 323), before the `opsAdmin` group:

```go
	// 审批管理路由
	// list/detail/cancel: AuthRequired（角色用户可看自己的）
	// approve/reject: AdminRequired（仅 is_admin 可审批）
	approvalRoutes := r.Group("/app/approvals")
	approvalRoutes.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), CORSMiddleware(), AuthRequired())
	{
		approvalRoutes.GET("", api_admin_list_approvals)
		approvalRoutes.GET("/:id", api_admin_get_approval)
		approvalRoutes.POST("/:id/cancel", api_admin_cancel_approval)
	}
	approvalAdmin := r.Group("/app/approvals")
	approvalAdmin.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), CORSMiddleware(), AdminRequired())
	{
		approvalAdmin.POST("/:id/approve", api_admin_approve_approval)
		approvalAdmin.POST("/:id/reject", api_admin_reject_approval)
	}
```

- [ ] **Step 3: Verify compilation**

Run: `cd api && go build ./...`

- [ ] **Step 4: Commit**

```bash
git add api/api_admin_approval.go api/route.go
git commit -m "feat(api): add approval management API endpoints"
```

---

## Task 5: Register Asynq Handler + Callbacks in Worker

**Files:**
- Modify: `api/worker_integration.go`

- [ ] **Step 1: Register approval execute handler in InitWorker**

In `InitWorker()` (after existing `asynq.Handle` calls, around line 51), add:

```go
	// 审批执行 handler
	asynq.Handle(TaskTypeApprovalExecute, ExecuteApproval)
```

- [ ] **Step 2: Register all 10 approval callbacks in InitWorker**

After the Asynq handler registration, add callback registrations. Note: callback functions don't exist yet — they'll be created in Tasks 6-9 when we refactor each handler. For now, register placeholder callbacks that return an error:

```go
	// 审批 callback 注册（实现在各 api_admin_*.go 文件中）
	RegisterApprovalCallback("edm_create_task", executeApprovalEDMCreateTask)
	RegisterApprovalCallback("campaign_create", executeApprovalCampaignCreate)
	RegisterApprovalCallback("campaign_update", executeApprovalCampaignUpdate)
	RegisterApprovalCallback("campaign_delete", executeApprovalCampaignDelete)
	RegisterApprovalCallback("campaign_issue_keys", executeApprovalCampaignIssueKeys)
	RegisterApprovalCallback("user_hard_delete", executeApprovalUserHardDelete)
	RegisterApprovalCallback("plan_update", executeApprovalPlanUpdate)
	RegisterApprovalCallback("plan_delete", executeApprovalPlanDelete)
	RegisterApprovalCallback("withdraw_approve", executeApprovalWithdrawApprove)
	RegisterApprovalCallback("withdraw_complete", executeApprovalWithdrawComplete)
```

Create a temporary file `api/logic_approval_callbacks.go` with placeholder callbacks that panic with "not implemented" — these will be replaced in Tasks 6-9 as each handler is refactored:

```go
package center

import (
	"context"
	"encoding/json"
	"fmt"
)

// Placeholder callbacks — replaced as handlers are refactored in Tasks 6-9

func executeApprovalEDMCreateTask(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: edm_create_task")
}
func executeApprovalCampaignCreate(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: campaign_create")
}
func executeApprovalCampaignUpdate(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: campaign_update")
}
func executeApprovalCampaignDelete(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: campaign_delete")
}
func executeApprovalCampaignIssueKeys(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: campaign_issue_keys")
}
func executeApprovalUserHardDelete(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: user_hard_delete")
}
func executeApprovalPlanUpdate(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: plan_update")
}
func executeApprovalPlanDelete(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: plan_delete")
}
func executeApprovalWithdrawApprove(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: withdraw_approve")
}
func executeApprovalWithdrawComplete(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: withdraw_complete")
}
```

- [ ] **Step 3: Verify compilation**

Run: `cd api && go build ./...`

- [ ] **Step 4: Commit**

```bash
git add api/worker_integration.go api/logic_approval_callbacks.go
git commit -m "feat(api): register approval Asynq handler and placeholder callbacks"
```

---

## Task 6: Refactor EDM Handler → Approval

**Files:**
- Modify: `api/api_admin_edm.go` (refactor `api_admin_create_edm_task`)
- Modify: `api/logic_approval_callbacks.go` (replace EDM placeholder)

- [ ] **Step 1: Read current api_admin_create_edm_task implementation**

Read `api/api_admin_edm.go` from the `create_edm_task` function to understand the full handler.

- [ ] **Step 2: Refactor handler to submit approval instead of direct execution**

Replace the tail of `api_admin_create_edm_task` (after validation) with `SubmitApproval()` call. Keep parameter validation and template check. Replace `EnqueueEDMTask()` with approval submission. Return `{approvalId, status: "pending_approval"}`.

- [ ] **Step 3: Implement executeApprovalEDMCreateTask callback**

Replace the placeholder in `logic_approval_callbacks.go`:

```go
func executeApprovalEDMCreateTask(ctx context.Context, params json.RawMessage) error {
	var req CreateEDMTaskRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	// Re-validate: template still active?
	var template EmailMarketingTemplate
	if err := db.Get().Where("id = ? AND is_active = ?", req.TemplateID, true).
		First(&template).Error; err != nil {
		return fmt.Errorf("template %d no longer active or not found", req.TemplateID)
	}

	// Determine scheduled time
	var scheduledAt *time.Time
	if req.ScheduledAt != nil {
		t := time.Unix(*req.ScheduledAt, 0)
		scheduledAt = &t
	}

	_, err := EnqueueEDMTask(ctx, req.TemplateID, req.UserFilters, scheduledAt)
	return err
}
```

- [ ] **Step 4: Verify compilation**

Run: `cd api && go build ./...`

- [ ] **Step 5: Commit**

```bash
git add api/api_admin_edm.go api/logic_approval_callbacks.go
git commit -m "feat(api): refactor EDM create task to use approval workflow"
```

---

## Task 7: Refactor Campaign Handlers → Approval (4 handlers)

**Files:**
- Modify: `api/api_admin_campaigns.go`
- Modify: `api/logic_approval_callbacks.go`

- [ ] **Step 1: Read current campaign handlers**

Read `api/api_admin_campaigns.go` to understand create, update, delete, and issue-keys handlers.

- [ ] **Step 2: Refactor all 4 handlers**

For each (create, update, delete, issue_keys): keep validation, replace execution with `SubmitApproval()`. Generate appropriate summaries:
- Create: `"创建优惠活动「{name}」，折扣码 {code}"`
- Update: `"修改优惠活动「{name}」(ID:{id})"`
- Delete: `"删除优惠活动「{name}」(ID:{id})"`
- Issue keys: `"为活动「{name}」发放 License Key，预计 {count} 个"`

For handlers with path params (update/:id, delete/:id, issue-keys/:id), merge the ID into params struct.

- [ ] **Step 3: Implement 4 callbacks in logic_approval_callbacks.go**

Replace placeholders. Each callback: unmarshal params → re-validate → execute original logic. Extract the execution logic from the original handler into the callback.

- [ ] **Step 4: Verify compilation**

Run: `cd api && go build ./...`

- [ ] **Step 5: Commit**

```bash
git add api/api_admin_campaigns.go api/logic_approval_callbacks.go
git commit -m "feat(api): refactor campaign CRUD + issue-keys to use approval workflow"
```

---

## Task 8: Refactor User Hard Delete + Plan + Withdraw Handlers → Approval

**Files:**
- Modify: `api/api_admin_user.go`
- Modify: `api/api_admin_plan.go`
- Modify: `api/api_admin_wallet.go`
- Modify: `api/logic_approval_callbacks.go`

- [ ] **Step 1: Read current handlers**

Read the hard_delete_users, update_plan, delete_plan, approve_withdraw, complete_withdraw handlers.

- [ ] **Step 2: Refactor all 5 handlers**

Same pattern: keep validation, replace execution with `SubmitApproval()`. Summaries:
- Hard delete: `"硬删除 {count} 个用户"`
- Plan update: `"修改套餐「{label}」(PID:{pid})"`
- Plan delete: `"删除套餐「{label}」(PID:{pid})"`
- Withdraw approve: `"审批提现 #{id}，金额 {amount}"`
- Withdraw complete: `"完成提现 #{id}"`

- [ ] **Step 3: Implement 5 callbacks**

Replace placeholders. Extract execution logic from original handlers into callbacks.

- [ ] **Step 4: Verify compilation**

Run: `cd api && go build ./...`

- [ ] **Step 5: Commit**

```bash
git add api/api_admin_user.go api/api_admin_plan.go api/api_admin_wallet.go api/logic_approval_callbacks.go
git commit -m "feat(api): refactor user/plan/withdraw handlers to use approval workflow"
```

---

## Task 9: Delete Placeholder File + Verify All Callbacks

**Files:**
- Verify: `api/logic_approval_callbacks.go` — should have no more `"not implemented"` placeholders

- [ ] **Step 1: Verify no remaining placeholders**

Run: `grep "not implemented" api/logic_approval_callbacks.go`
Expected: no matches.

- [ ] **Step 2: Run full build**

Run: `cd api && go build ./...`

- [ ] **Step 3: Run tests**

Run: `cd api && go test ./...`
Fix any compilation issues.

- [ ] **Step 4: Commit (if any cleanup needed)**

```bash
git add api/
git commit -m "chore(api): verify all approval callbacks implemented"
```

---

## Task 10: Audit Logging for Normal Operations

**Files:**
- Modify: `api/api_admin_user.go` (8 handlers)
- Modify: `api/api_admin_edm.go` (4 template handlers)
- Modify: `api/api_admin_cloud.go` (5 handlers)
- Modify: `api/api_admin_node.go` or equivalent (2 handlers)
- Modify: `api/api_admin_tunnel.go` or equivalent (2 handlers)
- Modify: `api/api_admin_plan.go` (2 handlers: create, restore)
- Modify: `api/api_admin_wallet.go` or equivalent for retailers
- Modify: other admin handlers per spec list

- [ ] **Step 1: Add WriteAuditLog to user handlers**

In `api_admin_user.go`, add `WriteAuditLog(c, action, targetType, targetID, detail)` at the end of each successful write handler:
- `user_update_email` — after email update success
- `user_set_roles` — after role update success
- `user_add_membership` — after membership added
- `user_update_retailer_status` — after status changed
- `user_update_retailer_config` — after config updated
- `user_add_member` / `user_remove_member`
- `user_issue_test_token`

Pattern: one line added after `Success(c, ...)`, before `return`:
```go
WriteAuditLog(c, "user_update_email", "user", uuid, map[string]string{"email": req.Email})
```

- [ ] **Step 2: Add WriteAuditLog to EDM template handlers**

4 handlers: create, update, delete, translate templates.

- [ ] **Step 3: Add WriteAuditLog to cloud, node, tunnel handlers**

Cloud: sync, change-ip, create, delete, update-traffic-config.
Node: update, delete.
Tunnel: update, delete.

- [ ] **Step 4: Add WriteAuditLog to remaining handlers**

Plan: create, restore.
License Key: delete.
Retailer: update level, create/update/delete notes.
Ticket: resolve, close.

- [ ] **Step 5: Verify compilation and tests**

Run: `cd api && go build ./... && go test ./...`

- [ ] **Step 6: Commit**

```bash
git add api/api_admin_*.go
git commit -m "feat(api): add audit logging to all admin write operations"
```

---

## Task 11: Frontend — API Client + Approval Page

**Files:**
- Modify: `web/src/lib/api.ts`
- Create: `web/src/app/(manager)/manager/approvals/page.tsx`
- Modify: `web/src/components/manager-sidebar.tsx`

- [ ] **Step 1: Add approval types and API methods to api.ts**

Add types and methods to `web/src/lib/api.ts`. Follow existing patterns (e.g., `getCampaigns`):

```typescript
// Approval types
export interface AdminApproval {
  id: number;
  createdAt: string;
  updatedAt: string;
  requestorId: number;
  requestorUuid: string;
  requestorName: string;
  action: string;
  params: string; // JSON string
  summary: string;
  status: 'pending' | 'approved' | 'executed' | 'failed' | 'rejected' | 'cancelled';
  approverId?: number;
  approverUuid?: string;
  approverName?: string;
  approvedAt?: string;
  rejectReason?: string;
  executedAt?: string;
  execError?: string;
}
```

Add methods to the `api` object:

```typescript
  async getApprovals(params: { status?: string } & Partial<PaginationParams> = {}): Promise<ListResult<AdminApproval>> {
    const searchParams = new URLSearchParams();
    if (params.status) searchParams.set('status', params.status);
    if (params.page) searchParams.set('page', String(params.page));
    if (params.pageSize) searchParams.set('pageSize', String(params.pageSize));
    return this.request<ListResult<AdminApproval>>(`/app/approvals?${searchParams}`);
  },

  async getApproval(id: number): Promise<AdminApproval> {
    return this.request<AdminApproval>(`/app/approvals/${id}`);
  },

  async approveApproval(id: number): Promise<void> {
    return this.request<void>(`/app/approvals/${id}/approve`, { method: 'POST' });
  },

  async rejectApproval(id: number, reason: string): Promise<void> {
    return this.request<void>(`/app/approvals/${id}/reject`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    });
  },

  async cancelApproval(id: number): Promise<void> {
    return this.request<void>(`/app/approvals/${id}/cancel`, { method: 'POST' });
  },
```

- [ ] **Step 2: Create approvals page**

Create `web/src/app/(manager)/manager/approvals/page.tsx`. Follow existing manager page patterns (e.g., campaigns page). Use shadcn/ui Table, Badge, Button, Dialog components. Key features:
- Table with columns: Action (display name), Summary, Requestor, Time, Status (colored badge)
- Status filter tabs (All / Pending / Approved / etc.)
- Pending rows: Approve/Reject buttons (if user != requestor and isAdmin), Cancel button (if user == requestor)
- Reject dialog with required reason TextField
- Expandable row detail showing formatted params JSON
- Pagination

Action display name mapping (frontend):
```typescript
const actionNames: Record<string, string> = {
  edm_create_task: '创建 EDM 邮件任务',
  campaign_create: '创建优惠活动',
  campaign_update: '修改优惠活动',
  campaign_delete: '删除优惠活动',
  campaign_issue_keys: '发放 License Key',
  user_hard_delete: '硬删除用户',
  plan_update: '修改订阅套餐',
  plan_delete: '删除订阅套餐',
  withdraw_approve: '审批提现',
  withdraw_complete: '完成提现',
};
```

Status badge colors:
```typescript
const statusColors: Record<string, string> = {
  pending: 'bg-orange-500',
  approved: 'bg-blue-500',
  executed: 'bg-green-500',
  failed: 'bg-red-500',
  rejected: 'bg-gray-500',
  cancelled: 'bg-gray-400',
};
```

- [ ] **Step 3: Update manager-sidebar.tsx**

Add "审批管理" menu item. It should be visible to all authenticated users (both admins and role users). The existing `requiredRole` uses 0 for superadmin-only and bitmask for role-based. Use `RoleMarketing | RoleDevopsViewer | RoleDevopsEditor | RoleSupport` to make it visible to all roles (superadmin already sees everything):

```typescript
import { ShieldCheck } from "lucide-react";

// Add at the beginning of menuGroups array:
{
  title: "审批管理",
  requiredRole: RoleMarketing | RoleDevopsViewer | RoleDevopsEditor | RoleSupport,
  items: [
    { href: "/manager/approvals", icon: ShieldCheck, label: "审批管理" },
  ]
},
```

No changes to the filter logic needed — the existing bitmask check already handles this correctly (superadmin bypasses, role users match via bitwise OR).

**Frontend implementation notes for the approvals page:**
- Use `useEffect` + `useState` for data fetching (same pattern as other manager pages, no React Query)
- Use `useAuth()` from `@/contexts/AuthContext` for current user (`user?.isAdmin`, `user?.id`)
- shadcn/ui components: `Table/TableBody/TableCell/TableHead/TableHeader/TableRow`, `Badge`, `Button`, `Dialog/DialogContent/DialogHeader/DialogTitle/DialogFooter`, `Input` (reject reason)
- Status filter: use shadcn `Tabs` component with "全部" / "待审批" / "已通过" / "已拒绝" tabs
- Reject dialog: `Dialog` with required `Input` for reason, disable confirm button when empty

- [ ] **Step 4: Build and verify frontend**

Run: `cd web && yarn build`

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/api.ts web/src/app/\(manager\)/manager/approvals/page.tsx web/src/components/manager-sidebar.tsx
git commit -m "feat(web): add approval management page and sidebar navigation"
```

---

## Task 12: Frontend — Adapt Existing Critical Operation Pages

**Files:**
- Modify: relevant pages that call critical operations (EDM, campaigns, etc.)

- [ ] **Step 1: Identify frontend pages that call critical APIs**

The critical operation handlers now return `{approvalId, status: "pending_approval"}` instead of immediate results. Update the frontend pages to handle this response:
- EDM create task page
- Campaign create/edit/delete
- Campaign issue keys
- User hard delete (if triggered from frontend)
- Plan edit/delete
- Withdraw approve/complete

- [ ] **Step 2: Add pending_approval response handling**

For each affected page, after the API call, check if response contains `status: "pending_approval"`:
```typescript
const result = await api.createCampaign(data);
if (result.status === 'pending_approval') {
  toast.success('已提交审批，等待其他管理员确认');
  return;
}
```

The exact modification depends on each page's current implementation. Read each page and adapt.

- [ ] **Step 3: Build and verify**

Run: `cd web && yarn build`

- [ ] **Step 4: Commit**

```bash
git add web/src/
git commit -m "feat(web): handle pending_approval responses in critical operation pages"
```

---

## Task 13: Integration Test + Final Verification

- [ ] **Step 1: Run all backend tests**

Run: `cd api && go test ./...`
Fix any failures.

- [ ] **Step 2: Run frontend build**

Run: `cd web && yarn build`
Fix any type errors.

- [ ] **Step 3: Manual smoke test checklist**

If local dev environment is available:
1. Start API server, verify migration creates `admin_approvals` table
2. Submit a critical operation (e.g., create campaign) → verify approval record created
3. List approvals → verify pending record appears
4. Approve from another admin → verify Asynq task dispatched
5. Verify callback executes → status changes to `executed`
6. Verify Slack DM sent (if bot token configured)
7. Verify audit log written

- [ ] **Step 4: Final commit if any fixes**

```bash
git add .
git commit -m "fix(api): address integration test findings"
```
