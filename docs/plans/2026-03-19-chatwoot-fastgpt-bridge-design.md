# Chatwoot ↔ FastGPT Bridge 设计文档

## 概述

在 Center API (`api/`) 中构建一个轻量 bridge，将 Chatwoot 收到的客户消息转发给 FastGPT AI，将 AI 回复发回 Chatwoot。当 FastGPT 判断需要人工介入时，自动触发 handoff 流程：通知 Slack、切换会话状态，由人工客服接管。

## 核心依赖

已有的 `qtoolkit` 包提供了完整的底层能力，bridge 只需组装业务逻辑：

| 包 | 能力 | 关键 API |
|---|---|---|
| `qtoolkit/chatwoot` | Webhook 接收 + 回复 | `Mount(router, path, handler)`, `Reply(ctx, convID, text)` |
| `qtoolkit/fastgpt` | AI 对话 | `Chat(ctx, chatID, parts...)` → `Result{Content, Similarity}` |
| `qtoolkit/slack` | 通知 | `Send(channel, message)` |

### qtoolkit/chatwoot 能力边界

**已封装**：
- `Mount()` — Gin webhook 端点注册，含 HMAC-SHA256 签名验证、异步 dispatch（goroutine + 60s context timeout）、panic recovery
- `Reply()` — 发送 outgoing 消息到指定会话
- `Event` 结构体 — 解析 webhook payload，包含 `EventType`, `Content`, `ConversationID`, `MessageType`, `Sender`, `Conversation.Status`, `Attachments`

**未封装（需 bridge 自行实现）**：
- 切换会话状态（pending ↔ open）— 需直接调用 Chatwoot REST API

### qtoolkit/fastgpt 能力边界

**已封装**：
- `Chat()` — 发送消息并获取 AI 回复，支持 `chatID` 服务端上下文管理（FastGPT 自动维护对话历史）
- `Result.Similarity` — 知识库搜索相似度评分（0-1），本期不使用，留作后续置信度自动 handoff 扩展
- 多模态支持 — `Text()`, `ImageURL()`, `FileURL()` 构造器

**配置**：`stream: false`, `detail: true`（用于提取 similarity）

## 架构

### 数据流

```
用户 ──msg──▶ Chatwoot ──webhook POST──▶ Center API /webhook/chatwoot
                                              │
                                              │ 立即回 HTTP 200
                                              │ (Mount 内部异步 dispatch)
                                              ▼
                                        handleChatwootEvent()
                                              │
                                   ┌──────────┼──────────────┐
                                   ▼          ▼              ▼
                              过滤检查    conversation    sender
                              event_type  status==pending  type==contact
                              ==message_  message_type
                              created     ==incoming
                                   │
                                   ▼ 全部通过
                            fastgpt.Chat()
                            chatID = "cw-{conversationID}"
                                   │
                                   ▼
                         ┌─── 检测 [TRANSFER_HUMAN] ───┐
                         ▼                              ▼
                     有标记                          无标记
                         │                              │
                    ┌────┴────┐                          │
                    ▼         ▼                          ▼
              剔除标记   toggleStatus              chatwoot.Reply()
              Slack通知   → "open"                  正常 AI 回复
                    │         │
                    ▼         ▼
              chatwoot.Reply()
              (剔除标记后的回复)

────────────────────────────────────────────────────────

人工客服在 Chatwoot 中：
  • 直接回复用户（正常对话）
  • 交回 bot → 将会话状态改回 "pending"（Chatwoot 原生操作）
  • 标记解决 → 将会话状态改为 "resolved"
```

### 防回环机制

bridge 收到 webhook 后按顺序检查，任一不满足即丢弃：

| # | 条件 | 目的 |
|---|---|---|
| 1 | `event.EventType == "message_created"` | 只处理新消息 |
| 2 | `event.MessageType == "incoming"` | 忽略 bot 自己的 outgoing（防无限循环） |
| 3 | `event.Sender.Type == "contact"` | 忽略 agent/bot 消息 |
| 4 | `event.Conversation.Status == "pending"` | 人工接管中不转发 |

### Handoff 流程

**触发条件**：FastGPT 回复中包含 `[TRANSFER_HUMAN]` 标记。

在 FastGPT 应用的 system prompt 中配置：
> 当你无法回答用户的问题、用户明确要求人工客服、或问题超出你的能力范围时，在回复末尾加上 `[TRANSFER_HUMAN]`。

**触发后的动作序列**：

1. 从 FastGPT 回复中剔除 `[TRANSFER_HUMAN]` 标记
2. 将剔除后的回复发送给用户（如"抱歉，我无法处理这个问题，正在为您转接人工客服"）
3. 调用 Chatwoot API 将会话状态从 `pending` 改为 `open`
4. 发送 Slack 通知到 `support` channel

**人工交回 bot**：客服在 Chatwoot 界面中将会话状态改回 `pending`，后续消息自动恢复 AI 处理。这是 Chatwoot 原生操作，无需额外开发。

### 会话状态机

```
                    新会话创建
                        │
                        ▼
                   ┌─────────┐
            ┌─────▶│ pending  │◀─────┐
            │      │ (bot)    │      │
            │      └────┬─────┘      │
            │           │            │
            │   [TRANSFER_HUMAN]     │
            │           │            │ 客服改状态
            │           ▼            │ 回 pending
            │      ┌─────────┐      │
            │      │  open    │──────┘
            │      │ (human)  │
            │      └────┬─────┘
            │           │
            │       客服解决
            │           │
            │           ▼
            │      ┌──────────┐
            └──────│ resolved │  (用户再次发消息 → Chatwoot 重开会话)
                   └──────────┘
```

> **注意**：Chatwoot 重开 resolved 会话时的默认状态（`pending` vs `open`）取决于 inbox 类型和配置。
> 部署时必须验证 API inbox 的 reopen 行为，确保重开后状态为 `pending` 以便 bot 自动接管。
> 若 Chatwoot 默认重开为 `open`，需在 inbox 设置中调整，或由客服手动改回 `pending`。

## 文件结构

```
api/
├── api_chatwoot.go       # webhook handler + Chatwoot status toggle
├── config.yml            # 新增 chatwoot / fastgpt 配置段
└── route.go              # 新增 webhook 路由注册
```

只需一个新文件 `api_chatwoot.go`，加上 `route.go` 中一行路由注册。

## 详细实现

### api_chatwoot.go

```go
package center

import (
    "context"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"

    "github.com/wordgate/qtoolkit/chatwoot"
    "github.com/wordgate/qtoolkit/fastgpt"
    "github.com/wordgate/qtoolkit/log"
    "github.com/wordgate/qtoolkit/slack"
)

const transferHumanMarker = "[TRANSFER_HUMAN]"

var chatwootHTTPClient = &http.Client{Timeout: 10 * time.Second}

// handleChatwootEvent 处理 Chatwoot webhook 事件
// 由 chatwoot.Mount() 异步调用，已在 goroutine 中，有 60s context timeout
func handleChatwootEvent(ctx context.Context, event chatwoot.Event) {
    // --- 过滤 ---
    if event.EventType != "message_created" {
        return
    }
    if event.MessageType != "incoming" {
        return // 防回环：忽略 bot 自己的 outgoing
    }
    if event.Sender.Type != "contact" {
        return // 忽略 agent/bot 消息
    }
    if event.Conversation.Status != "pending" {
        return // 人工接管中，不转发
    }
    if strings.TrimSpace(event.Content) == "" {
        return // 空消息不处理
    }

    // 注意：handler 由 chatwoot.Mount() 在独立 goroutine 中异步调用，
    // 此时 gin.Context 已释放，只能使用 context.Context。
    // 这是 webhook 异步处理的固有限制，不影响功能，但 request-scoped tracing 不可用。
    log.Infof(ctx, "conversation=%d sender=%s content=%q",
        event.ConversationID, event.Sender.Name, truncate(event.Content, 100))

    // --- 构造 FastGPT 请求 ---
    chatID := fmt.Sprintf("cw-%d", event.ConversationID)
    parts := buildFastGPTParts(event)

    result, err := fastgpt.Chat(ctx, chatID, parts...)
    if err != nil {
        log.Errorf(ctx, "fastgpt error: conversation=%d err=%v", event.ConversationID, err)
        chatwoot.Reply(ctx, event.ConversationID, "抱歉，系统暂时无法处理您的消息，请稍后再试。")
        return
    }

    // --- 检测 handoff ---
    // 使用 HasSuffix 检测末尾标记，与 FastGPT system prompt 指令一致（"在回复末尾加上"），
    // 避免用户消息中恰好包含标记文本导致误触发
    if strings.HasSuffix(strings.TrimSpace(result.Content), transferHumanMarker) {
        handleTransferHuman(ctx, event.ConversationID, result.Content, event.Content)
        return
    }

    // --- 正常回复 ---
    if err := chatwoot.Reply(ctx, event.ConversationID, result.Content); err != nil {
        log.Errorf(ctx, "reply error: conversation=%d err=%v", event.ConversationID, err)
    }
}

// buildFastGPTParts 将 Chatwoot 消息转为 FastGPT 多模态 parts
func buildFastGPTParts(event chatwoot.Event) []fastgpt.Part {
    var parts []fastgpt.Part

    if event.Content != "" {
        parts = append(parts, fastgpt.Text(event.Content))
    }

    for _, att := range event.Attachments {
        switch att.FileType {
        case "image":
            parts = append(parts, fastgpt.ImageURL(att.DataURL))
        case "file":
            parts = append(parts, fastgpt.FileURL("attachment", att.DataURL))
        }
    }

    // 保底：至少有一个 text part
    if len(parts) == 0 {
        parts = append(parts, fastgpt.Text("[用户发送了无法识别的内容]"))
    }

    return parts
}

// handleTransferHuman 处理人工转接
func handleTransferHuman(ctx context.Context, conversationID int, aiReply string, customerMsg string) {
    // 1. 剔除标记，发送剩余内容给用户
    reply := strings.TrimSpace(strings.ReplaceAll(aiReply, transferHumanMarker, ""))
    if reply != "" {
        if err := chatwoot.Reply(ctx, conversationID, reply); err != nil {
            log.Errorf(ctx, "reply error on transfer: conversation=%d err=%v", conversationID, err)
        }
    }

    // 2. 切换会话状态为 open（人工接管）
    if err := toggleConversationStatus(ctx, conversationID); err != nil {
        log.Errorf(ctx, "toggle status error: conversation=%d err=%v", conversationID, err)
    }

    // 3. Slack 通知（包含客户原始消息，方便客服快速了解上下文）
    slackMsg := fmt.Sprintf("[Chatwoot] 客户需要人工客服 — 会话 #%d\n客户消息: %s",
        conversationID, truncate(customerMsg, 200))
    if err := slack.Send("support", slackMsg); err != nil {
        log.Errorf(ctx, "slack notify error: %v", err)
    }

    log.Infof(ctx, "transferred to human: conversation=%d", conversationID)
}

// toggleConversationStatus 切换会话状态（pending ↔ open）
// Chatwoot API: POST /api/v1/accounts/{account_id}/conversations/{conversation_id}/toggle_status
// qtoolkit/chatwoot 未封装此接口，直接调用 REST API
func toggleConversationStatus(ctx context.Context, conversationID int) error {
    cfg := getChatwootConfig()
    url := fmt.Sprintf("%s/api/v1/accounts/%d/conversations/%d/toggle_status",
        cfg.BaseURL, cfg.AccountID, conversationID)

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
    if err != nil {
        return fmt.Errorf("create request: %w", err)
    }
    req.Header.Set("api_access_token", cfg.APIToken)
    req.Header.Set("Content-Type", "application/json")

    resp, err := chatwootHTTPClient.Do(req)
    if err != nil {
        return fmt.Errorf("http request: %w", err)
    }
    defer resp.Body.Close()
    io.Copy(io.Discard, resp.Body)

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("unexpected status: %d", resp.StatusCode)
    }
    return nil
}

// getChatwootConfig 从 viper 读取 chatwoot 配置
// 用于 toggleConversationStatus 等 qtoolkit/chatwoot 未封装的 API
func getChatwootConfig() chatwoot.Config {
    return chatwoot.Config{
        APIToken:  viper.GetString("chatwoot.api_token"),
        BaseURL:   strings.TrimRight(viper.GetString("chatwoot.base_url"), "/"),
        AccountID: viper.GetInt("chatwoot.account_id"),
    }
}

func truncate(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen] + "..."
}
```

### route.go 变更

在现有的 webhook 路由组中新增一行：

```go
webhook := r.Group("/webhook")
webhook.Use(log.MiddlewareRequestLog(true), MiddleRecovery())
// ... 现有 webhook 路由 ...
chatwoot.Mount(webhook, "/chatwoot", handleChatwootEvent)
```

### config.yml 新增段

```yaml
chatwoot:
  api_token: "YOUR_CHATWOOT_API_TOKEN"       # Chatwoot agent/bot API token
  base_url: "https://chatwoot.example.com"    # Chatwoot 实例地址
  account_id: 1                                # Chatwoot account ID
  webhook_token: "YOUR_WEBHOOK_SECRET"         # HMAC 签名验证密钥（可选）

fastgpt:
  api_key: "fastgpt-XXXXXXXX"                 # FastGPT 应用 API Key
  base_url: "https://fastgpt.example.com"     # FastGPT 实例地址
```

## go.mod 变更

新增两个 qtoolkit 依赖：

```
require (
    github.com/wordgate/qtoolkit/chatwoot v1.5.11
    github.com/wordgate/qtoolkit/fastgpt  v1.5.11
)
```

`qtoolkit/slack` 已在 go.mod 中。

## Chatwoot 端配置

### 1. 创建 API Inbox

Settings → Inboxes → Add Inbox → 选择 "API" channel：
- Name: `AI Support`
- Webhook URL: `https://your-center-api.com/webhook/chatwoot`

### 2. 配置 Webhook（如果用通用 webhook 而非 API inbox）

Settings → Integrations → Webhooks → Add:
- URL: `https://your-center-api.com/webhook/chatwoot`
- Events: 勾选 `message_created`

### 3. 新会话默认状态

确保新会话默认状态为 `pending`，这样 bot 才能自动接管。在 Chatwoot 的 inbox 设置中可以配置。

## FastGPT 端配置

### System Prompt 补充

在 FastGPT 应用的系统提示词中加入：

> 当你遇到以下情况时，在回复末尾加上 `[TRANSFER_HUMAN]`：
> 1. 用户明确要求人工客服（如"转人工"、"找真人"、"talk to agent"）
> 2. 问题超出你的知识范围，无法给出有用回答
> 3. 用户情绪激动或表达不满
> 4. 涉及账号安全、退款、投诉等敏感操作
>
> 在加上标记的同时，给用户一个友好的过渡回复，如"我理解您的需求，正在为您转接人工客服，请稍等。"

### API Key

在 FastGPT 应用设置 → 发布为 API → 生成 API Key，填入 `config.yml` 的 `fastgpt.api_key`。

## 错误处理

| 场景 | 处理 |
|---|---|
| FastGPT 调用失败（网络/超时/5xx） | 回复用户"系统暂时无法处理"，log error |
| FastGPT 返回空内容 | 同上 |
| Chatwoot Reply 失败 | log error，不重试（避免重复消息） |
| toggle_status 失败 | log error，Slack 通知仍然发出（人工可手动改状态） |
| Slack 发送失败 | log error，不影响主流程 |
| Webhook 签名验证失败 | chatwoot.Mount 返回 401，handler 不触发 |

**设计原则**：每个外部调用独立失败，不阻塞后续步骤。Slack 和 status toggle 失败不影响用户收到回复。

## 多模态支持

Chatwoot 用户可能发送图片或文件附件。bridge 利用 `fastgpt.ImageURL()` 和 `fastgpt.FileURL()` 透传给 FastGPT：

| Chatwoot attachment.FileType | FastGPT Part |
|---|---|
| `image` | `fastgpt.ImageURL(att.DataURL)` |
| `file` | `fastgpt.FileURL("attachment", att.DataURL)` |
| `audio` / `video` | 忽略（FastGPT 不支持） |

文本和附件可以同时存在，构成多模态消息。

## 日志

遵循 Center API 日志规范（无冗余前缀），结构化 key=value 格式：

```
INFO  conversation=42 sender=张三 content="你好，请问..."
INFO  transferred to human: conversation=42
ERROR fastgpt error: conversation=42 err=context deadline exceeded
ERROR reply error: conversation=42 err=403 forbidden
```

**注意**：handler 由 `chatwoot.Mount()` 在独立 goroutine 中异步调用，此时 `gin.Context` 已释放。日志使用 `context.Context`，request-scoped tracing 不可用。这是 webhook 异步处理的固有限制。

## 测试策略

### Unit Test（api_chatwoot_test.go）

| 测试用例 | 验证点 |
|---|---|
| `TestHandleChatwootEvent_FilterOutgoing` | message_type != "incoming" 被过滤 |
| `TestHandleChatwootEvent_FilterNonContact` | sender.type != "contact" 被过滤 |
| `TestHandleChatwootEvent_FilterOpenStatus` | conversation.status != "pending" 被过滤 |
| `TestHandleChatwootEvent_FilterNonMessage` | event_type != "message_created" 被过滤 |
| `TestHandleChatwootEvent_NormalReply` | FastGPT 正常回复转发到 Chatwoot |
| `TestHandleChatwootEvent_TransferHuman` | 检测标记 → 剔除 → Reply + toggle + Slack |
| `TestHandleChatwootEvent_FastGPTError` | FastGPT 失败 → 降级回复 |
| `TestBuildFastGPTParts_TextOnly` | 纯文本消息 |
| `TestBuildFastGPTParts_WithImage` | 文本 + 图片附件 |
| `TestBuildFastGPTParts_EmptyContent` | 空消息保底 |

### 测试方法

- 使用 `httptest.NewServer` mock FastGPT 和 Chatwoot API
- `chatwoot.SetConfig()` 和 `fastgpt.SetConfig()` 注入测试配置
- 不需要数据库（bridge 无 DB 交互）

## 部署 checklist

- [ ] `config.yml` 添加 `chatwoot` 和 `fastgpt` 配置段
- [ ] `go.mod` 添加 `qtoolkit/chatwoot` 和 `qtoolkit/fastgpt` 依赖
- [ ] Chatwoot 创建 API inbox 或添加 webhook，URL 指向 `/webhook/chatwoot`
- [ ] FastGPT 应用 system prompt 添加 `[TRANSFER_HUMAN]` 规则
- [ ] Slack `support` channel 存在且 webhook 已配置
- [ ] 验证 resolved 会话被用户重开后的默认状态（需为 `pending`，否则调整 inbox 设置）
- [ ] 测试：发消息 → AI 回复；说"转人工" → Slack 通知 + 状态变 open
