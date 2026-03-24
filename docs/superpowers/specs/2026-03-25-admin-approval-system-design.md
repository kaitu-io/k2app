# Admin 危险操作审批系统设计

## 概述

为 Center API 的 admin 后台添加双人审批（maker-checker）机制。危险写操作（Critical）需要另一名 admin 审批后才执行；普通写操作（Normal）仅记录审计日志。

## 决策记录

| 决策项 | 选择 | 备选 | 理由 |
|--------|------|------|------|
| 审批模式 | 双人审批（maker-checker） | 单人确认+审计 | 安全优先，防止误操作和恶意操作 |
| 审批人范围 | 任何 is_admin，不能审批自己 | super_admin / 跨角色互审 | 团队小，简单有效 |
| 过期策略 | 永不过期 | 24h 自动过期 / 可配置 | 简化实现，cancelled 状态可手动撤回 |
| 通知方式 | 后台列表 + Slack DM | 仅后台 / 邮件 | Slack 可按邮箱找到个人发 DM，及时性好 |
| 执行方式 | 审批通过即自动执行 | 通知发起人手动执行 | 参数已确认，避免遗忘 |
| 实现方案 | Handler 内嵌 + 审批服务层（方案 B） | Middleware 拦截 / 两阶段 Draft | Callback 精确可控，不依赖 HTTP 重放 |

## 风险分级

### Critical（需双人审批）

| Action Key | 操作 | 影响范围 | Handler |
|------------|------|---------|---------|
| `edm_create_task` | 创建 EDM 邮件任务 | 批量邮件，可能数千用户 | `api_admin_edm.go` |
| `campaign_create` | 创建优惠活动 | 新建营销活动 | `api_admin_campaigns.go` |
| `campaign_update` | 修改优惠活动 | 变更活动规则 | `api_admin_campaigns.go` |
| `campaign_delete` | 删除优惠活动 | 移除活动 | `api_admin_campaigns.go` |
| `campaign_issue_keys` | 发放 license key | 批量生成数百~数千 key | `api_admin_campaigns.go` |
| `user_hard_delete` | 硬删除用户 | 级联删除 13 张表 | `api_admin_user.go` |
| `plan_update` | 修改订阅套餐 | 影响订阅体系 | `api_admin_plans.go` |
| `plan_delete` | 删除订阅套餐 | 影响订阅体系 | `api_admin_plans.go` |
| `withdraw_approve` | 审批提现 | 资金操作 | `api_admin_orders.go` |
| `withdraw_complete` | 完成提现 | 资金操作 | `api_admin_orders.go` |

### Normal（仅审计日志）

所有其他 admin 写操作，包括：用户信息修改、EDM 模板 CRUD、Cloud 实例操作、Node/Tunnel 修改、Retailer 配置、License key 单个删除、Access key 管理（已有审计）。

## 数据模型

```go
// AdminApproval 管理员操作审批记录
// 不含 DeletedAt — 审计记录不可删除
type AdminApproval struct {
    ID        uint64    `gorm:"primarykey"`
    CreatedAt time.Time `gorm:"index:idx_approval_status_time"`
    UpdatedAt time.Time

    // 发起人
    RequestorID   uint64 `gorm:"not null;index"`
    RequestorUUID string `gorm:"type:varchar(255);not null"`
    RequestorName string `gorm:"type:varchar(255);not null"` // 冗余存储，列表展示避免 N+1

    // 操作标识（注册表 key，如 "edm_create_task"）
    Action string `gorm:"type:varchar(64);not null;index"`

    // handler 校验后的干净参数（JSON）
    Params string `gorm:"type:text;not null"`

    // 人类可读摘要（审批人看这个决策，不需要读 Params JSON）
    Summary string `gorm:"type:text;not null"`

    // 审批状态
    Status string `gorm:"type:varchar(16);not null;default:pending;index:idx_approval_status_time"`

    // 审批人（approved/rejected 时填充）
    ApproverID   *uint64    `gorm:"index"`
    ApproverUUID *string    `gorm:"type:varchar(255)"`
    ApproverName *string    `gorm:"type:varchar(255)"`
    ApprovedAt   *time.Time
    RejectReason *string    `gorm:"type:varchar(512)"` // rejected 时必填

    // 执行结果（executed/failed 时填充）
    ExecutedAt *time.Time
    ExecError  *string `gorm:"type:text"`
}
```

### Status 状态机

```
                 ┌── rejected（审批人拒绝，RejectReason 必填）
                 │
pending ────┬────┤
            │    └── cancelled（发起人自行撤回）
            │
            └─── approved ──┬── executed（callback 成功，ExecutedAt 记录时间）
                            └── failed（callback 出错，ExecError 记录原因）
```

### 并发安全

approve/reject/cancel 均使用原子更新：
```sql
UPDATE admin_approvals SET status = 'approved', ... WHERE id = ? AND status = 'pending'
```
`RowsAffected == 0` 时返回 409 Conflict（已被其他人处理或已取消）。

### 索引

- `idx_approval_status_time(status, created_at)` — 复合索引，主查询 "pending 按时间排序" 走索引
- `requestor_id` — "我的请求" 查询
- `approver_id` — "我审批的" 查询
- `action` — 按操作类型过滤
- `idx_approval_action_status(action, status)` — "某操作是否有 pending 审批" 查询

## Callback 注册表

```go
// ApprovalCallback 审批通过后的执行函数
type ApprovalCallback func(ctx context.Context, params json.RawMessage) error

// 全局注册表，InitWorker() 时填充
var approvalRegistry = map[string]ApprovalCallback{}

func RegisterApprovalCallback(action string, cb ApprovalCallback) {
    approvalRegistry[action] = cb
}
```

### Callback 契约

1. 接收 `context.Context`（Asynq task context，有 tracing），**不是** `*gin.Context`
2. `params` 是 handler 校验后序列化的 JSON，callback 内部 unmarshal 为具体类型
3. **params 必须包含所有执行所需数据**，包括 URL path 参数（如 withdraw ID、campaign ID）。Handler 在调用 `SubmitApproval` 前将 path 参数合并到 params struct 中（见 Handler 改造模式中的 path 参数示例）
4. **必须 re-validate 关键前置条件**（模板是否 active、用户是否存在、提现状态是否正确）— 提交到执行之间状态可能变化
5. 成功后写审计日志。因为 callback 运行在 Asynq context（无 `*gin.Context`），使用 `WriteAuditLogFromApproval(ctx, approval)` 代替 `WriteAuditLog(c, ...)`。该函数从 approval 记录取 requestor 信息作为 actor
6. 返回 error 时，审批记录标记为 `failed`，ExecError 记录错误

### 10 个 Callback

| Action | Callback 逻辑 | Re-validate |
|--------|--------------|-------------|
| `edm_create_task` | 调用 `EnqueueEDMTask()` | 模板是否仍 active |
| `campaign_create` | 创建 Campaign 记录 | campaign code 唯一性（DB unique constraint 兜底，callback 提前检查给友好错误） |
| `campaign_update` | 更新 Campaign 记录 | campaign 是否存在 |
| `campaign_delete` | 删除 Campaign 记录 | campaign 是否存在 |
| `campaign_issue_keys` | `GenerateLicenseKeysForCampaign()` + 异步发邮件 | campaign 存在 + isShareable |
| `user_hard_delete` | 事务级联删除 13 表 | 用户是否存在 |
| `plan_update` | 更新 Plan 字段 | plan 是否存在 |
| `plan_delete` | 软删除 Plan | plan 是否存在 |
| `withdraw_approve` | 更新提现状态 | 提现记录状态是否仍 pending |
| `withdraw_complete` | 标记提现完成 | 提现记录状态是否 approved |

## 审批服务层（`logic_approval.go`）

### 核心函数

```go
// SubmitApproval 提交审批请求
// 由 critical handler 在参数校验通过后调用
// 流程：
//   1. 校验 action 在 approvalRegistry 中已注册
//   2. 获取 ReqUser(c) 填充 requestor 字段
//   3. json.Marshal(params) 序列化
//   4. 创建 AdminApproval 记录 (status=pending)
//   5. 异步发 Slack DM 通知所有其他 admin
//   6. 返回 approval ID
func SubmitApproval(c *gin.Context, action string, params any, summary string) (uint64, error)

// ApproveApproval 审批通过
// 流程：
//   1. 校验 approverID != requestorID
//   2. 原子更新 status pending → approved
//   3. RowsAffected==0 → 返回 409 Conflict
//   4. 入队 Asynq task "approval:execute"
//   5. 异步 Slack DM 通知发起人
func ApproveApproval(c *gin.Context, approvalID uint64) error

// RejectApproval 拒绝审批
// 流程：
//   1. 校验 approverID != requestorID
//   2. 校验 reason 非空
//   3. 原子更新 status pending → rejected
//   4. RowsAffected==0 → 返回 409 Conflict
//   5. 异步 Slack DM 通知发起人（含拒绝原因）
func RejectApproval(c *gin.Context, approvalID uint64, reason string) error

// CancelApproval 发起人取消
// 流程：
//   1. 校验 requestorID == currentUserID
//   2. 原子更新 status pending → cancelled
//   3. RowsAffected==0 → 返回 409 Conflict
func CancelApproval(c *gin.Context, approvalID uint64) error

// ExecuteApproval Asynq task handler
// 流程：
//   1. 查找 AdminApproval 记录
//   2. 校验 status == approved
//   3. 从 approvalRegistry 查找 callback
//   4. 调用 callback(ctx, params)
//   5. 成功 → status=executed, ExecutedAt=now, 写 AdminAuditLog（via WriteAuditLogFromApproval）
//   6. 失败 → status=failed, ExecError=err.Error()
//   7. 无论成败，Slack DM 通知发起人执行结果
func ExecuteApproval(ctx context.Context, payload []byte) error
```

### Asynq 集成

```go
const TaskTypeApprovalExecute = "approval:execute"

type ApprovalExecutePayload struct {
    ApprovalID uint64 `json:"approvalId"`
}
```

在 `InitWorker()` 中注册：
```go
asynq.Handle(TaskTypeApprovalExecute, ExecuteApproval)
```

## Slack DM 通知

### 新增能力

现有 `slack.Send(channel, message)` 发到频道（通过 Webhook）。需新增按邮箱发 DM 的能力。

**实现位置**：在 `logic_approval.go` 中本地实现，使用 `net/http` 直接调用 Slack Web API。不修改 `qtoolkit/slack` 包（那是独立仓库，且 Webhook 和 Bot Token API 是不同的认证模型，不应混在一起）。

**Slack Web API 调用链：**
1. `users.lookupByEmail` — 邮箱 → Slack user ID
2. `conversations.open` — 打开 DM channel
3. `chat.postMessage` — 发送消息

**需要的 Slack Bot Token scope：** `users:read.email`, `chat:write`, `im:write`

### 实现

```go
// SlackDMByEmail 通过邮箱给个人发 Slack DM
// 本地实现，直接调用 Slack Web API（net/http + Bot Token）
// email → Slack user ID（内存缓存） → DM channel → 发消息
func SlackDMByEmail(ctx context.Context, email string, message string) error

// NotifyApprovalSubmitted 通知所有其他 admin 有新审批请求
// 查 is_admin=true AND id != requestorID 的用户邮箱，逐个发 DM
func NotifyApprovalSubmitted(ctx context.Context, approval *AdminApproval)

// NotifyApprovalResult 通知发起人审批结果（通过/拒绝/执行成功/执行失败）
func NotifyApprovalResult(ctx context.Context, approval *AdminApproval)
```

### Slack user ID 缓存

`map[string]string`（email → slackUserID），admin 人数少（<10），内存缓存足够。`sync.RWMutex` 保护。cache miss 时调 `lookupByEmail`。进程重启时重建。

### 消息模板

**新审批请求（发给其他 admin）：**
```
🔒 新的审批请求
操作：{Action 可读名}
发起人：{RequestorName}
摘要：{Summary}
时间：{CreatedAt}
👉 前往审批：https://kaitu.io/manager/approvals
```

**审批结果（发给发起人）：**
```
✅ 审批已通过 / ❌ 审批被拒绝
操作：{Action 可读名}
审批人：{ApproverName}
[拒绝原因：{RejectReason}]
```

**执行结果（发给发起人）：**
```
🎉 操作已执行 / ⚠️ 操作执行失败
操作：{Action 可读名}
[错误：{ExecError}]
```

### 失败处理

Slack 通知是 best-effort。失败只记 `log.Warnf`，不影响审批流程。异步执行（goroutine + context），与 `WriteAuditLog` 模式一致。

## API 端点

```
GET    /app/approvals              — 审批列表
GET    /app/approvals/:id          — 审批详情
POST   /app/approvals/:id/approve  — 审批通过
POST   /app/approvals/:id/reject   — 拒绝（body: {reason: string}）
POST   /app/approvals/:id/cancel   — 取消（仅发起人）
```

### 权限模型：提交者 vs 审批者

**提交者**：任何有权访问 critical handler 的用户。例如 `RoleMarketing` 用户可以调用 EDM 创建（因为 handler 挂在 `opsAdmin` 组下），handler 内部调用 `SubmitApproval()` 创建审批记录。提交者不需要 `is_admin=true`。

**审批者**：必须 `is_admin=true`，且不能是提交者本人。

**查看权限**：审批列表和详情端点使用 `AuthRequired()`（非 `AdminRequired()`），但返回数据按角色过滤：
- `is_admin=true`：看到所有审批记录，可以审批/拒绝
- 非 admin 用户：只看到自己提交的记录（`requestor_id = currentUserID`），只能取消

这样 Marketing 角色提交 EDM 审批后，可以在审批页面查看自己的提交状态。

### 列表端点

- 支持 `?status=pending` 过滤
- 默认排序：pending 置顶，然后按 `created_at DESC`
- 分页：复用现有 `PaginationFromRequest(c)` 模式
- 权限：`AuthRequired()` + 角色过滤（见上）

### 详情端点

- 返回完整审批记录，含 `Params` JSON（前端格式化展示）
- 权限：`AuthRequired()` + 自己的记录或 `is_admin=true`

### approve/reject 端点

- 权限：`AdminRequired()`（只有 is_admin 可以审批）
- 校验：`approverID != approval.RequestorID`，否则 403
- 原子更新 + 409 Conflict 处理
- reject 需 body `{reason: string}`，reason 非空校验

### cancel 端点

- 权限：`AuthRequired()`
- 校验：`currentUserID == approval.RequestorID`，否则 403
- 仅 pending 可取消

## Handler 改造模式

### 改造前（以 EDM 为例）

```go
func api_admin_create_edm_task(c *gin.Context) {
    var req CreateEDMTaskRequest
    if err := c.ShouldBindJSON(&req); err != nil { ... }
    // 验证模板
    var template EmailMarketingTemplate
    if err := db.Get().Where(...).First(&template).Error; err != nil { ... }
    // 直接入队执行
    batchID, err := EnqueueEDMTask(c.Request.Context(), req.TemplateID, req.UserFilters, scheduledAt)
    Success(c, &EDMTaskResponse{BatchID: batchID, ...})
}
```

### 改造后

```go
func api_admin_create_edm_task(c *gin.Context) {
    var req CreateEDMTaskRequest
    if err := c.ShouldBindJSON(&req); err != nil { ... }
    // 验证模板（不变）
    var template EmailMarketingTemplate
    if err := db.Get().Where(...).First(&template).Error; err != nil { ... }
    // 构造摘要
    previewCount := previewEDMTargetCount(c.Request.Context(), req.UserFilters)
    summary := fmt.Sprintf("发送模板「%s」给约 %d 名用户", template.Subject, previewCount)
    // 提交审批（替代直接执行）
    approvalID, err := SubmitApproval(c, "edm_create_task", &req, summary)
    if err != nil { ... }
    Success(c, gin.H{"approvalId": approvalID, "status": "pending_approval"})
}
```

### Callback 函数（body 参数示例 — EDM）

```go
// executeEDMCreateTask 审批通过后执行
func executeEDMCreateTask(ctx context.Context, params json.RawMessage) error {
    var req CreateEDMTaskRequest
    if err := json.Unmarshal(params, &req); err != nil { return err }
    // re-validate: 模板是否仍 active
    var template EmailMarketingTemplate
    if err := db.Get().Where("id = ? AND is_active = ?", req.TemplateID, true).
        First(&template).Error; err != nil {
        return fmt.Errorf("template %d no longer active", req.TemplateID)
    }
    // 执行
    _, err := EnqueueEDMTask(ctx, req.TemplateID, req.UserFilters, nil)
    return err
}
```

### Path 参数示例（withdraw — ID 来自 URL path）

Handler 改造：将 path 参数合并到 params struct：

```go
func api_admin_approve_withdraw(c *gin.Context) {
    id, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil { ... }
    // 验证提现记录存在且状态正确
    var withdraw WithdrawRequest
    if err := db.Get().First(&withdraw, id).Error; err != nil { ... }
    if withdraw.Status != "pending" { ... }
    // 构造包含 path 参数的 params
    params := struct {
        WithdrawID uint64 `json:"withdrawId"`
    }{WithdrawID: id}
    summary := fmt.Sprintf("审批提现 #%d，金额 %.2f 元，用户 %s", id, withdraw.Amount, withdraw.UserUUID)
    approvalID, err := SubmitApproval(c, "withdraw_approve", &params, summary)
    if err != nil { ... }
    Success(c, gin.H{"approvalId": approvalID, "status": "pending_approval"})
}
```

Callback：从 params 取 withdrawId：

```go
func executeWithdrawApprove(ctx context.Context, params json.RawMessage) error {
    var p struct { WithdrawID uint64 `json:"withdrawId"` }
    if err := json.Unmarshal(params, &p); err != nil { return err }
    // re-validate: 提现记录状态是否仍 pending
    var withdraw WithdrawRequest
    if err := db.Get().First(&withdraw, p.WithdrawID).Error; err != nil {
        return fmt.Errorf("withdraw %d not found", p.WithdrawID)
    }
    if withdraw.Status != "pending" {
        return fmt.Errorf("withdraw %d status is %s, expected pending", p.WithdrawID, withdraw.Status)
    }
    // 执行审批逻辑...
    return nil
}
```

### 改造工作量

每个 handler 改造 3 步：
1. 保留参数校验和前置验证
2. 抽取执行逻辑为 `func executeXxx(ctx, params) error`
3. Handler 尾部替换为 `SubmitApproval()` + 返回 pending 响应

10 个 handler，每个约 30-50 行改动。

## Normal 操作审计日志扩展

当前 `WriteAuditLog()` 只在 access key 操作使用。本次一并补齐所有 admin 写操作的审计日志。

### 已有审计（2 个）

- `access_key_generate` — `api_admin_user.go:1001`
- `access_key_revoke` — `api_admin_user.go:1036`

### 需补充审计（约 30 个 handler）

| 模块 | 操作 | Action |
|------|------|--------|
| User | 修改邮箱 | `user_update_email` |
| User | 修改角色 | `user_set_roles` |
| User | 添加会员时长 | `user_add_membership` |
| User | 修改分销商状态 | `user_update_retailer_status` |
| User | 修改分销商配置 | `user_update_retailer_config` |
| User | 添加成员 | `user_add_member` |
| User | 移除成员 | `user_remove_member` |
| User | 签发测试 token | `user_issue_test_token` |
| EDM | 创建模板 | `edm_create_template` |
| EDM | 更新模板 | `edm_update_template` |
| EDM | 删除模板 | `edm_delete_template` |
| EDM | 翻译模板 | `edm_translate_template` |
| Cloud | 同步实例 | `cloud_sync_instances` |
| Cloud | 换 IP | `cloud_change_ip` |
| Cloud | 创建实例 | `cloud_create_instance` |
| Cloud | 删除实例 | `cloud_delete_instance` |
| Cloud | 更新流量配置 | `cloud_update_traffic_config` |
| Node | 更新节点 | `node_update` |
| Node | 删除节点 | `node_delete` |
| Tunnel | 更新隧道 | `tunnel_update` |
| Tunnel | 删除隧道 | `tunnel_delete` |
| Plan | 创建套餐 | `plan_create` |
| Plan | 恢复套餐 | `plan_restore` |
| License Key | 删除 | `license_key_delete` |
| Retailer | 更新等级 | `retailer_update_level` |
| Retailer | 创建备注 | `retailer_create_note` |
| Retailer | 更新备注 | `retailer_update_note` |
| Retailer | 删除备注 | `retailer_delete_note` |
| Ticket | 解决工单 | `ticket_resolve` |
| Ticket | 关闭工单 | `ticket_close` |

每个 handler 末尾加一行 `WriteAuditLog(c, action, targetType, targetID, detail)` 调用。

## 前端变更

### 新增页面：`/manager/approvals`

- 审批列表表格：Action 可读名、Summary、发起人、时间、状态
- Pending 行显示操作按钮：
  - 当前用户 != 发起人 → "通过" + "拒绝" 按钮
  - 当前用户 == 发起人 → "取消" 按钮
- 拒绝弹窗：TextField 输入原因（必填）
- 点击行展开详情：Params JSON 格式化展示
- 状态 chip 颜色：pending=orange, approved=blue, executed=green, failed=red, rejected=grey, cancelled=grey

### 导航栏

`manager-sidebar.tsx` 新增 "审批管理" 菜单项，带 pending 数量 badge（轮询或页面进入时刷新）。

### 现有页面适配

Critical 操作的前端调用返回 `{approvalId, status: "pending_approval"}` 时：
- 展示 toast/snackbar："已提交审批，等待其他管理员确认"
- 不再期望操作立即完成的响应格式

### API 函数

`web/src/lib/api.ts` 新增：
```typescript
// 审批列表
getApprovals(params?: { status?: string; page?: number; pageSize?: number })
// 审批详情
getApproval(id: number)
// 审批通过
approveApproval(id: number)
// 拒绝
rejectApproval(id: number, reason: string)
// 取消
cancelApproval(id: number)
```

## 边界情况

| 场景 | 处理 |
|------|------|
| 仅一个 admin | Critical 操作永远 pending。设计意图：安全优先。文档注明至少需要 2 名 admin。 |
| Callback 执行失败 | status → failed，ExecError 记录错误，Slack 通知发起人。不自动重试——危险操作失败后应人工检查，发起人重新提交新审批。 |
| 审批期间实体被删 | Callback re-validate 发现实体不存在 → 返回 error → status=failed。 |
| 重复提交同一操作 | 不限制。同一操作可有多个 pending 审批（参数可能不同）。 |
| Slack 通知失败 | 只记日志，不影响审批流程。admin 通过后台列表发现 pending 请求。 |
| 并发审批 | 原子 UPDATE WHERE status='pending'，第二人收到 409。 |
| 审批后发起人被删 | 不影响执行——callback 不依赖发起人身份，参数已序列化。 |

## 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `api/model.go` | 修改 | 新增 `AdminApproval` 模型 |
| `api/migrate.go` | 修改 | AutoMigrate 列表添加 `&AdminApproval{}` |
| `api/logic_approval.go` | **新建** | Submit/Approve/Reject/Cancel/Execute + Slack DM + callback registry |
| `api/api_admin_approval.go` | **新建** | 5 个审批管理 API handler |
| `api/route.go` | 修改 | 新增 `/app/approvals` 路由组 |
| `api/worker_integration.go` | 修改 | 注册 `approval:execute` handler + 10 个 callback |
| `api/api_admin_edm.go` | 修改 | 改造 `create_edm_task` |
| `api/api_admin_campaigns.go` | 修改 | 改造 create/update/delete/issue-keys (4 个 handler) |
| `api/api_admin_user.go` | 修改 | 改造 `hard_delete_users` |
| `api/api_admin_orders.go` | 修改 | 改造 `approve_withdraw` / `complete_withdraw` |
| `api/api_admin_plans.go` | 修改 | 改造 `update_plan` / `delete_plan` |
| `api/api_admin_*.go` (30+ 处) | 修改 | 补充 `WriteAuditLog()` 调用 |
| `web/src/app/(manager)/manager/approvals/page.tsx` | **新建** | 审批列表页 |
| `web/src/components/manager-sidebar.tsx` | 修改 | 新增审批管理导航 + badge |
| `web/src/lib/api.ts` | 修改 | 新增审批相关 API 函数 |

## 与现有 AdminAuditLog 的关系

- `AdminApproval` 记录审批流程（谁发起、谁审批、参数、状态流转）
- `AdminAuditLog` 记录操作执行事实（什么操作、什么目标、执行详情）
- Critical 操作：先有 approval 记录，callback 执行成功后再写 audit log
- Normal 操作：直接在 handler 写 audit log，不经过 approval

两张表职责正交，不重复。

### WriteAuditLogFromApproval

Callback 运行在 Asynq context，无 `*gin.Context`。新增变体函数：

```go
// WriteAuditLogFromApproval 从审批记录写审计日志（Asynq context 用）
// 用 approval.RequestorID/UUID 作为 actor（是谁发起的操作）
// 用 approval.ApproverID/UUID 记录在 detail 中（是谁审批的）
func WriteAuditLogFromApproval(ctx context.Context, approval *AdminApproval, targetType, targetID string, detail any)
```

这样审计日志同时记录了"谁发起"和"谁审批"的完整链路。

## Slack Bot Token 配置

需要在 Center API 配置中新增 Slack Bot Token（区别于现有的 Webhook URL）：

```yaml
slack:
  bot_token: "xoxb-..."  # 新增，用于 DM
  # 现有 webhook channels 不变
```

**所需 scope：** `users:read.email`, `chat:write`, `im:write`
