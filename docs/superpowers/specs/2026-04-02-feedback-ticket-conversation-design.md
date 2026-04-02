# Feedback Ticket Conversation System

用户提交反馈工单后，能在 webapp 内查看工单进展、与客服双向对话，并通过邮件收到回复通知。

## 决策记录

| 决策 | 结论 |
|------|------|
| 回复形态 | 工单内对话，双向沟通 |
| 通知方式 | 应用内 badge + 邮件（Asynq 5 分钟聚合） |
| 匿名用户 | 提交时用邮箱 FindOrCreateUserByEmail，工单归属 user_id |
| 用户入口 | `/feedback` 反馈中心，FeedbackButton 导航目标改为此 |
| 管理员入口 | web 管理台 + MCP 工具，共享后端 API |
| closed 工单 | 不可回复、不可重开，用户需新建工单 |
| 已有无头工单 | 不做迁移，无 user_id 的工单不在用户列表显示 |
| 轮询频率 | 60 秒 |
| 回复附件 | 支持图片上传（S3），MVP 仅图片 |

## 一、数据模型

### 新增 ticket_replies 表

```sql
CREATE TABLE ticket_replies (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    created_at DATETIME NOT NULL,
    ticket_id BIGINT UNSIGNED NOT NULL,       -- FK → feedback_tickets.id
    sender_type VARCHAR(16) NOT NULL,          -- 'user' | 'admin'
    sender_id BIGINT UNSIGNED,                 -- user_id (user) or admin user_id (admin)
    sender_name VARCHAR(64),                   -- display: admin → "客服", user → email prefix
    content TEXT NOT NULL,                      -- 1-2000 chars
    images JSON,                               -- ["s3key1", "s3key2"], max 4 images per reply
    notified_at DATETIME,                      -- NULL = pending notification

    INDEX idx_ticket_id (ticket_id),
    INDEX idx_notified_at (notified_at)
);
```

### feedback_tickets 表新增字段

```sql
ALTER TABLE feedback_tickets
    ADD COLUMN last_reply_at DATETIME,         -- 最新回复时间，排序用；新建时 = created_at
    ADD COLUMN last_reply_by VARCHAR(16),       -- 'user' | 'admin'，判断谁在等谁
    ADD COLUMN user_unread INT NOT NULL DEFAULT 0;  -- 用户未读管理员回复数
```

### Go Model

```go
type TicketReply struct {
    ID         uint64    `gorm:"primarykey"`
    CreatedAt  time.Time
    TicketID   uint64    `gorm:"index;not null"`
    SenderType string    `gorm:"type:varchar(16);not null"`  // "user" | "admin"
    SenderID   *uint64
    SenderName string    `gorm:"type:varchar(64)"`
    Content    string    `gorm:"type:text;not null"`
    Images     string    `gorm:"type:json"`                  // JSON array of S3 keys
    NotifiedAt *time.Time `gorm:"index"`
}
```

feedback_tickets model 新增：

```go
LastReplyAt  *time.Time `gorm:"index"`
LastReplyBy  string     `gorm:"type:varchar(16)"`
UserUnread   int        `gorm:"not null;default:0"`
```

### 状态流转

```
open ──(admin resolve)──→ resolved ──(admin close)──→ closed
  ↑                          │
  └──(user reply to resolved)┘  (reopen)

closed: 不可回复、不可重开
```

## 二、后端 API

### 用户端 `/api/user/` (AuthRequired)

#### GET /api/user/tickets — 我的工单列表

```
Query: page, pageSize (default 20)
Response: {
    list: [{
        id, feedbackId, content (truncate 100 chars),
        status, userUnread,
        lastReplyAt, lastReplyBy,
        createdAt
    }],
    total
}
Order: last_reply_at DESC (有新回复的排最前)
Filter: WHERE user_id = currentUser AND deleted_at IS NULL
```

#### GET /api/user/tickets/:id — 工单详情 + 回复

```
Response: {
    ticket: { id, feedbackId, content, status, createdAt, resolvedAt },
    replies: [{ id, senderType, senderName, content, images, createdAt }]
}
Side effect: 将该工单 user_unread 重置为 0
```

Note: 虽然 GET 通常不应有副作用，但"标记已读"是查看详情的隐含语义，
且独立的 read 接口会导致前端每次进详情多一次请求。这是一个务实的取舍。
如果后续有"未读列表"等需要精确控制已读状态的场景，再拆出 POST /read。

#### POST /api/user/tickets/:id/reply — 用户回复

```
Body: { content: string (1-2000), images?: string[] (S3 keys, max 4) }
Logic:
  1. 验证工单属于当前用户，且 status != "closed"
  2. 写入 ticket_replies (sender_type="user", sender_id=currentUser)
  3. 更新 feedback_tickets: last_reply_at=now, last_reply_by="user"
  4. 若 status == "resolved" → 重置为 "open" (reopen)
```

#### GET /api/user/tickets/unread — 未读总数

```
Response: { unread: int }
Logic: SELECT SUM(user_unread) FROM feedback_tickets WHERE user_id = currentUser AND deleted_at IS NULL
```

#### POST /api/user/tickets/upload-image — 上传图片

```
Body: multipart/form-data, field "image"
Validation: max 5MB, image/jpeg | image/png | image/webp
Logic:
  1. 生成 S3 key: ticket-images/{userId}/{uuid}.{ext}
  2. 上传 S3
  3. 返回 { key: "ticket-images/..." , url: "https://cdn/..." }
```

### 管理端 `/app/` (RoleRequired: allOpsRoles)

#### POST /app/feedback-tickets/:id/reply — 管理员回复

```
Body: { content: string (1-2000), senderName?: string (default "客服"), images?: string[] }
Logic:
  1. 写入 ticket_replies (sender_type="admin", sender_id=adminUser)
  2. 更新 feedback_tickets: last_reply_at=now, last_reply_by="admin", user_unread += 1
  3. Enqueue Asynq task "ticket:notify" delay 5min, payload={ticketId}, Unique(ticketId)
```

#### GET /app/feedback-tickets/:id/replies — 获取回复列表

```
Response: { replies: [{ id, senderType, senderName, content, images, createdAt }] }
```

### 匿名提交改造

`POST /api/user/ticket` (AuthOptional) 中：

```go
// 无 token 但有 email 时
if userID == 0 && req.Email != "" {
    user, err := FindOrCreateUserByEmail(ctx, req.Email, req.Language)
    // 将 user.ID 写入 ticket.UserID
}
```

### 现有管理端列表增强

`GET /app/feedback-tickets` response 新增 `lastReplyAt`, `lastReplyBy` 字段，
支持按 `last_reply_by=user` 筛选"等待客服回复"的工单。

## 三、邮件聚合通知

### Asynq 任务 `ticket:notify`

```go
const TaskTypeTicketNotify = "ticket:notify"

type TicketNotifyPayload struct {
    TicketID uint64
}
```

**流程：**

1. 管理员回复写入 DB → `notified_at = NULL`
2. Enqueue `ticket:notify` delay 5 min, `Unique(fmt.Sprintf("ticket:%d", ticketID))`
3. 5 分钟后 handler 执行：
   - 查询该工单所有 `notified_at IS NULL AND sender_type = 'admin'` 的回复
   - 若无未通知回复 → return nil（幂等）
   - 查用户邮箱
   - 合并回复内容，发送邮件
   - 批量更新 `notified_at = now()`

**邮件模板：**

```
Subject: [Kaitu] 您的反馈工单有新回复

您好，

您的反馈工单有 {N} 条新回复：

────────────────
[客服] 2026-04-02 14:30
已确认是 DNS 设置问题，请尝试切换到智能模式。

[客服] 2026-04-02 14:32
如果仍有问题，请上传最新日志，我们会进一步排查。
────────────────

查看详情：{appUrl}/feedback

— Kaitu 团队
```

## 四、用户端前端 (webapp)

### 路由

```tsx
// App.tsx
{appConfig.features.feedback && (
  <>
    <Route path="feedback" element={
      <LoginRequiredGuard pagePath="/feedback">
        <Feedback />
      </LoginRequiredGuard>
    } />
    {/* 旧路由兼容 */}
    <Route path="submit-ticket" element={<Navigate to="/feedback" replace />} />
    <Route path="faq" element={<FAQ />} />
  </>
)}
```

Guard 改为 `LoginRequiredGuard`（不再要求 membership，因为匿名提交自动创建了账户）。

### Feedback 页面

**有工单时：**

```
┌─────────────────────────────────────────┐
│  反馈中心                    [提交新工单] │
├─────────────────────────────────────────┤
│ ┌─────────────────────────────────────┐ │
│ │ 🔴 无法连接到服务器...               │ │  ← 未读红点
│ │ 状态: 处理中  ·  客服回复于 2 小时前   │ │
│ └─────────────────────────────────────┘ │
│ ┌─────────────────────────────────────┐ │
│ │ 速度不稳定，切换节点后...             │ │
│ │ 状态: 已解决  ·  3 天前               │ │
│ └─────────────────────────────────────┘ │
└─────────────────────────────────────────┘
```

**点击工单 → 对话详情：**

```
┌─────────────────────────────────────────┐
│  ← 返回                    状态: 处理中  │
├─────────────────────────────────────────┤
│                                         │
│  ┌──────────────────────┐               │
│  │ 无法连接到服务器，     │  ← 用户消息   │
│  │ 试了3个节点都不行      │    (右对齐)   │
│  └──────────────────────┘  4/1 10:30    │
│                                         │
│         ┌──────────────────────┐        │
│  客服 → │ 已确认是 DNS 问题，   │        │
│         │ 请尝试切换智能模式    │        │
│         └──────────────────────┘        │
│                          4/2 14:30      │
│                                         │
├─────────────────────────────────────────┤
│ [📷]  请输入回复...            [发送]    │
└─────────────────────────────────────────┘
```

- 用户消息右对齐，客服消息左对齐（聊天 UI 惯例）
- 图片以缩略图展示，点击放大
- closed 工单隐藏输入框，显示"工单已关闭"

**无工单时：** 直接展示新建工单表单（复用现有 SubmitTicket 逻辑）。

**新建工单提交成功后：** 跳转到该工单的对话详情页（而非 success 静态页）。

**匿名提交成功后：** 显示提示"工单已提交。使用邮箱 xxx@xxx.com 登录即可查看进展和回复。"

### FeedbackButton 改造

```tsx
// 导航目标: /feedback
// Badge: 未读数 > 0 时显示红色圆点 + 数字
// 数据源: feedbackStore.unreadCount
```

### feedback.store.ts (Zustand)

```typescript
interface FeedbackState {
    unreadCount: number;
    pollUnread: () => void;     // GET /api/user/tickets/unread, 60s interval
    stopPolling: () => void;
}
```

- 登录后启动轮询，登出后停止
- 进入工单详情时本地 unreadCount 减去该工单的 userUnread（乐观更新）

### i18n

新增 namespace `feedback.json` 扩展（或复用现有 ticket.json）：

```json
{
  "feedbackCenter": "反馈中心",
  "newTicket": "提交新工单",
  "myTickets": "我的工单",
  "noTickets": "暂无反馈工单",
  "status": {
    "open": "处理中",
    "resolved": "已解决",
    "closed": "已关闭"
  },
  "reply": {
    "placeholder": "请输入回复...",
    "send": "发送",
    "closed": "工单已关闭",
    "reopened": "工单已重新打开",
    "imageUpload": "上传图片",
    "imageLimitExceeded": "最多上传 4 张图片"
  },
  "notification": {
    "newReply": "您的反馈有新回复",
    "loginToView": "使用邮箱 {{email}} 登录即可查看进展和回复"
  },
  "adminReply": "客服",
  "lastReply": "回复于 {{time}}"
}
```

## 五、管理端前端 (web)

### /manager/tickets 页面改造

**列表增强：**
- 新增列：`最后回复`（时间 + 来源）
- 新增筛选：`等待回复`（last_reply_by = user）
- 行高亮：last_reply_by = "user" 且 status = "open" 的工单用浅色背景标记

**详情弹窗改造：**
- 现有弹窗底部新增对话时间线（所有 replies，时间正序）
- 图片缩略图展示
- 底部加回复输入框 + 图片上传 + 发送按钮

## 六、MCP 工具

### 新增 reply_feedback_ticket

```typescript
// tools/kaitu-center/src/tools/admin-feedback-tickets.ts
defineApiTool("reply_feedback_ticket", {
    description: "Reply to a feedback ticket as admin. Triggers email notification to user.",
    path: "/app/feedback-tickets/{id}/reply",
    method: "POST",
    params: {
        id: { type: "number", required: true },
        content: { type: "string", required: true, description: "Reply content, 1-2000 chars" },
        sender_name: { type: "string", default: "claude", description: "Display name" },
    },
    bodyMap: { content: "content", senderName: "sender_name" },
});
```

### 新增 list_ticket_replies

```typescript
defineApiTool("list_ticket_replies", {
    description: "List all replies for a feedback ticket.",
    path: "/app/feedback-tickets/{id}/replies",
    method: "GET",
    params: {
        id: { type: "number", required: true },
    },
});
```

## 七、S3 图片存储

路径：`ticket-images/{userId}/{uuid}.{ext}`

- 复用现有 S3 上传基础设施
- CDN 通过 CloudFront 分发
- 图片限制：单张 5MB，单条回复最多 4 张，格式 JPEG/PNG/WebP

## 八、不做的事

- 工单优先级 / 分类标签 — 工单量不大，不需要
- 用户回复频率限制 — 信任用户
- WebSocket 实时推送 — 60 秒轮询足够
- 回复编辑 / 删除 — 增加复杂度，不值得
- 富文本编辑器 — 纯文本 + 图片足够
- 已有无头工单迁移 — 不在用户列表显示即可
