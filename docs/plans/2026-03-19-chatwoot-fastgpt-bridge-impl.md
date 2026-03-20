# Chatwoot ↔ FastGPT Bridge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a lightweight bridge in Center API that routes Chatwoot customer messages to FastGPT AI, returns replies, and handles human handoff via Slack notification + conversation status toggle.

**Architecture:** Single new file `api_chatwoot.go` with webhook handler + helpers. `chatwoot.Mount()` handles async dispatch, HMAC verification, and panic recovery. `fastgpt.Chat()` handles AI conversation with server-side context. No database interaction.

**Tech Stack:** Go 1.24, Gin, qtoolkit/chatwoot v1.5.14, qtoolkit/fastgpt v1.5.14, qtoolkit/slack (existing)

**Spec:** `docs/plans/2026-03-19-chatwoot-fastgpt-bridge-design.md`

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `api/api_chatwoot.go` | Create | Webhook handler, filter logic, FastGPT call, handoff, toggle status |
| `api/api_chatwoot_test.go` | Create | Unit tests: filters, normal reply, handoff, error fallback, multimodal parts |
| `api/route.go` | Modify (1 line) | Register `chatwoot.Mount()` on webhook group |
| `api/go.mod` | Modify | Add `qtoolkit/chatwoot` and `qtoolkit/fastgpt` v1.5.14 |
| `api/config.yml` | Modify | Add `chatwoot` and `fastgpt` config sections |

---

### Task 1: Add dependencies and config

**Files:**
- Modify: `api/go.mod`
- Modify: `api/go.sum` (auto-generated)
- Modify: `api/config.yml`

- [ ] **Step 1: Add qtoolkit/chatwoot and qtoolkit/fastgpt to go.mod**

```bash
cd api && go get github.com/wordgate/qtoolkit/chatwoot@v1.5.14 github.com/wordgate/qtoolkit/fastgpt@v1.5.14 && go mod tidy
```

Expected: `go.mod` updated with two new entries, `go.sum` updated. Note: existing qtoolkit packages remain at v1.5.11 — this is fine, Go modules support mixed versions within the same module prefix. If `go mod tidy` bumps other qtoolkit packages, accept the changes.

- [ ] **Step 2: Add chatwoot and fastgpt config sections to config.yml**

Append to `api/config.yml`:

```yaml
# Chatwoot AI 客服 bridge
chatwoot:
  api_token: ""
  base_url: ""
  account_id: 0
  webhook_token: ""

# FastGPT AI 对话
fastgpt:
  api_key: ""
  base_url: ""
```

- [ ] **Step 3: Verify build**

```bash
cd api && go build ./...
```

Expected: Build succeeds (no code uses the new packages yet, but they're resolvable).

Note: Both `qtoolkit/chatwoot` and `qtoolkit/fastgpt` use lazy initialization via `sync.Once` — they auto-read from viper on first API call. No explicit `SetConfig()` startup call is needed. The config.yml values are sufficient. `SetConfig()` is only used in tests to override viper.

- [ ] **Step 4: Commit**

```bash
git add api/go.mod api/go.sum api/config.yml
git commit -m "chore(api): add qtoolkit/chatwoot and qtoolkit/fastgpt dependencies"
```

---

### Task 2: Create api_chatwoot.go — filter logic + buildFastGPTParts

**Files:**
- Create: `api/api_chatwoot.go`
- Create: `api/api_chatwoot_test.go`

This task implements the filter guard and multimodal parts builder — no external calls yet.

- [ ] **Step 1: Write filter tests**

Create `api/api_chatwoot_test.go`:

```go
package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wordgate/qtoolkit/chatwoot"
)

func TestShouldProcessEvent_Filters(t *testing.T) {
	base := chatwoot.Event{
		EventType:      "message_created",
		MessageType:    "incoming",
		Content:        "hello",
		ConversationID: 1,
		Sender:         chatwoot.Sender{Type: "contact", Name: "Test"},
		Conversation:   chatwoot.Conversation{Status: "pending"},
	}

	tests := []struct {
		name   string
		modify func(e *chatwoot.Event)
		want   bool
	}{
		{"valid event passes", func(e *chatwoot.Event) {}, true},
		{"wrong event type", func(e *chatwoot.Event) { e.EventType = "conversation_created" }, false},
		{"outgoing message", func(e *chatwoot.Event) { e.MessageType = "outgoing" }, false},
		{"agent sender", func(e *chatwoot.Event) { e.Sender.Type = "user" }, false},
		{"open status", func(e *chatwoot.Event) { e.Conversation.Status = "open" }, false},
		{"resolved status", func(e *chatwoot.Event) { e.Conversation.Status = "resolved" }, false},
		{"empty content", func(e *chatwoot.Event) { e.Content = "" }, false},
		{"whitespace content", func(e *chatwoot.Event) { e.Content = "  \n  " }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := base
			tt.modify(&e)
			assert.Equal(t, tt.want, shouldProcessChatwootEvent(e))
		})
	}
}

func TestBuildFastGPTParts_TextOnly(t *testing.T) {
	event := chatwoot.Event{Content: "hello"}
	parts := buildFastGPTParts(event)
	assert.Len(t, parts, 1)
}

func TestBuildFastGPTParts_WithImage(t *testing.T) {
	event := chatwoot.Event{
		Content: "check this",
		Attachments: []chatwoot.Attachment{
			{FileType: "image", DataURL: "https://example.com/img.png"},
		},
	}
	parts := buildFastGPTParts(event)
	assert.Len(t, parts, 2)
}

func TestBuildFastGPTParts_WithFile(t *testing.T) {
	event := chatwoot.Event{
		Content: "see attachment",
		Attachments: []chatwoot.Attachment{
			{FileType: "file", DataURL: "https://example.com/doc.pdf"},
		},
	}
	parts := buildFastGPTParts(event)
	assert.Len(t, parts, 2)
}

func TestBuildFastGPTParts_IgnoresAudioVideo(t *testing.T) {
	event := chatwoot.Event{
		Content: "voice msg",
		Attachments: []chatwoot.Attachment{
			{FileType: "audio", DataURL: "https://example.com/audio.mp3"},
			{FileType: "video", DataURL: "https://example.com/video.mp4"},
		},
	}
	parts := buildFastGPTParts(event)
	assert.Len(t, parts, 1) // only text
}

func TestBuildFastGPTParts_EmptyContentWithUnsupportedAttachment(t *testing.T) {
	event := chatwoot.Event{
		Content: "",
		Attachments: []chatwoot.Attachment{
			{FileType: "audio", DataURL: "https://example.com/audio.mp3"},
		},
	}
	parts := buildFastGPTParts(event)
	assert.Len(t, parts, 1) // fallback text part
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```bash
cd api && go test -run TestShouldProcess -v ./...
```

Expected: FAIL — `shouldProcessChatwootEvent` undefined.

- [ ] **Step 3: Write api_chatwoot.go with filter + parts builder**

Create `api/api_chatwoot.go`:

```go
package center

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/chatwoot"
	"github.com/wordgate/qtoolkit/fastgpt"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
)

const transferHumanMarker = "[TRANSFER_HUMAN]"

var chatwootHTTPClient = &http.Client{Timeout: 10 * time.Second}

// shouldProcessChatwootEvent checks all filter conditions.
// Returns true only if the event should be forwarded to FastGPT.
func shouldProcessChatwootEvent(event chatwoot.Event) bool {
	if event.EventType != "message_created" {
		return false
	}
	if event.MessageType != "incoming" {
		return false
	}
	if event.Sender.Type != "contact" {
		return false
	}
	if event.Conversation.Status != "pending" {
		return false
	}
	if strings.TrimSpace(event.Content) == "" {
		return false
	}
	return true
}

// handleChatwootEvent processes Chatwoot webhook events.
// Called asynchronously by chatwoot.Mount() in a goroutine with 60s context timeout.
// Note: gin.Context is not available here (already released), so we use context.Context.
// Request-scoped tracing is not available — this is inherent to async webhook processing.
func handleChatwootEvent(ctx context.Context, event chatwoot.Event) {
	if !shouldProcessChatwootEvent(event) {
		return
	}

	log.Infof(ctx, "conversation=%d sender=%s content=%q",
		event.ConversationID, event.Sender.Name, truncateString(event.Content, 100))

	chatID := fmt.Sprintf("cw-%d", event.ConversationID)
	parts := buildFastGPTParts(event)

	result, err := fastgpt.Chat(ctx, chatID, parts...)
	if err != nil {
		log.Errorf(ctx, "fastgpt error: conversation=%d err=%v", event.ConversationID, err)
		chatwoot.Reply(ctx, event.ConversationID, "抱歉，系统暂时无法处理您的消息，请稍后再试。")
		return
	}

	if strings.TrimSpace(result.Content) == "" {
		log.Warnf(ctx, "fastgpt returned empty content: conversation=%d", event.ConversationID)
		chatwoot.Reply(ctx, event.ConversationID, "抱歉，系统暂时无法处理您的消息，请稍后再试。")
		return
	}

	if strings.HasSuffix(strings.TrimSpace(result.Content), transferHumanMarker) {
		handleTransferHuman(ctx, event.ConversationID, result.Content, event.Content)
		return
	}

	if err := chatwoot.Reply(ctx, event.ConversationID, result.Content); err != nil {
		log.Errorf(ctx, "reply error: conversation=%d err=%v", event.ConversationID, err)
	}
}

// buildFastGPTParts converts a Chatwoot event into FastGPT multimodal parts.
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

	if len(parts) == 0 {
		parts = append(parts, fastgpt.Text("[用户发送了无法识别的内容]"))
	}

	return parts
}

// handleTransferHuman handles the human handoff flow:
// 1. Strip marker and reply to user
// 2. Toggle conversation status to "open"
// 3. Send Slack notification with customer's original message
func handleTransferHuman(ctx context.Context, conversationID int, aiReply string, customerMsg string) {
	reply := strings.TrimSpace(strings.ReplaceAll(aiReply, transferHumanMarker, ""))
	if reply != "" {
		if err := chatwoot.Reply(ctx, conversationID, reply); err != nil {
			log.Errorf(ctx, "reply error on transfer: conversation=%d err=%v", conversationID, err)
		}
	}

	if err := toggleConversationStatus(ctx, conversationID); err != nil {
		log.Errorf(ctx, "toggle status error: conversation=%d err=%v", conversationID, err)
	}

	slackMsg := fmt.Sprintf("[Chatwoot] 客户需要人工客服 — 会话 #%d\n客户消息: %s",
		conversationID, truncateString(customerMsg, 200))
	if err := slack.Send("support", slackMsg); err != nil {
		log.Errorf(ctx, "slack notify error: %v", err)
	}

	log.Infof(ctx, "transferred to human: conversation=%d", conversationID)
}

// toggleConversationStatus calls Chatwoot REST API to toggle conversation status (pending ↔ open).
// This API is not wrapped by qtoolkit/chatwoot, so we call it directly.
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

// getChatwootConfig reads chatwoot config from viper.
// Used for APIs not wrapped by qtoolkit/chatwoot (e.g., toggle_status).
func getChatwootConfig() chatwoot.Config {
	return chatwoot.Config{
		APIToken:  viper.GetString("chatwoot.api_token"),
		BaseURL:   strings.TrimRight(viper.GetString("chatwoot.base_url"), "/"),
		AccountID: viper.GetInt("chatwoot.account_id"),
	}
}
```

Note: Reuses existing `truncateString()` from `api_admin_retailer.go` (same package, rune-safe).

- [ ] **Step 4: Run all tests — expect pass**

```bash
cd api && go test -run "TestShouldProcess|TestBuildFastGPTParts" -v ./...
```

Expected: All 8 tests pass.

- [ ] **Step 5: Commit**

```bash
git add api/api_chatwoot.go api/api_chatwoot_test.go
git commit -m "feat(api): add chatwoot bridge filter logic and multimodal parts builder"
```

---

### Task 3: Test handoff detection

**Files:**
- Modify: `api/api_chatwoot_test.go`

- [ ] **Step 1: Write handoff marker detection tests**

Append to `api/api_chatwoot_test.go`:

```go
func TestDetectTransferMarker(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"marker at end", "正在转接人工客服[TRANSFER_HUMAN]", true},
		{"marker at end with trailing space", "正在转接[TRANSFER_HUMAN]  ", true},
		{"marker at end with newline", "正在转接[TRANSFER_HUMAN]\n", true},
		{"no marker", "普通回复内容", false},
		{"marker in middle", "请[TRANSFER_HUMAN]稍等", false},
		{"marker at start", "[TRANSFER_HUMAN]请稍等", false},
		{"empty content", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.HasSuffix(strings.TrimSpace(tt.content), transferHumanMarker)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStripTransferMarker(t *testing.T) {
	input := "我理解您的需求，正在为您转接人工客服，请稍等。[TRANSFER_HUMAN]"
	result := strings.TrimSpace(strings.ReplaceAll(input, transferHumanMarker, ""))
	assert.Equal(t, "我理解您的需求，正在为您转接人工客服，请稍等。", result)
}
```

Add `"strings"` to the import block if not already present.

- [ ] **Step 2: Run tests — expect pass**

```bash
cd api && go test -run "TestDetectTransfer|TestStripTransfer" -v ./...
```

Expected: All 8 tests pass.

- [ ] **Step 3: Commit**

```bash
git add api/api_chatwoot_test.go
git commit -m "test(api): add chatwoot handoff marker detection tests"
```

---

### Task 4: Test toggleConversationStatus

**Files:**
- Modify: `api/api_chatwoot_test.go`

- [ ] **Step 1: Write toggle status tests with httptest mock**

Add `"context"`, `"net/http"`, `"net/http/httptest"` to the existing import block in `api/api_chatwoot_test.go`, then append these test functions:

Note: `toggleConversationStatus` calls `getChatwootConfig()` which reads from viper, so tests set viper values (not `chatwoot.SetConfig()`).

```go
func TestToggleConversationStatus_Success(t *testing.T) {
	var receivedPath string
	var receivedToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedToken = r.Header.Get("api_access_token")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	viper.Set("chatwoot.api_token", "test-token")
	viper.Set("chatwoot.base_url", server.URL)
	viper.Set("chatwoot.account_id", 42)
	t.Cleanup(func() {
		viper.Set("chatwoot.api_token", "")
		viper.Set("chatwoot.base_url", "")
		viper.Set("chatwoot.account_id", 0)
	})

	err := toggleConversationStatus(context.Background(), 123)
	assert.NoError(t, err)
	assert.Equal(t, "/api/v1/accounts/42/conversations/123/toggle_status", receivedPath)
	assert.Equal(t, "test-token", receivedToken)
}

func TestToggleConversationStatus_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	viper.Set("chatwoot.api_token", "test-token")
	viper.Set("chatwoot.base_url", server.URL)
	viper.Set("chatwoot.account_id", 1)
	t.Cleanup(func() {
		viper.Set("chatwoot.api_token", "")
		viper.Set("chatwoot.base_url", "")
		viper.Set("chatwoot.account_id", 0)
	})

	err := toggleConversationStatus(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}
```

- [ ] **Step 2: Run tests — expect pass**

```bash
cd api && go test -run TestToggleConversation -v ./...
```

Expected: Both tests pass.

- [ ] **Step 3: Commit**

```bash
git add api/api_chatwoot_test.go
git commit -m "test(api): add chatwoot toggle conversation status tests"
```

---

### Task 5: Register webhook route

**Files:**
- Modify: `api/route.go:34` (near existing webhook line)

- [ ] **Step 1: Add chatwoot.Mount() to route.go**

In `api/route.go`, after line 34 (`r.POST("/webhook/wordgate", ...)`), add these lines:

```go
	// Chatwoot → FastGPT AI bridge
	chatwootWebhook := r.Group("/webhook")
	chatwootWebhook.Use(log.MiddlewareRequestLog(true), MiddleRecovery())
	chatwoot.Mount(chatwootWebhook, "/chatwoot", handleChatwootEvent)
```

Also add to the import block:

```go
	"github.com/wordgate/qtoolkit/chatwoot"
```

Note: The existing wordgate webhook at line 34 is registered flat (`r.POST`). We create a separate group for chatwoot — this is fine in Gin, both routes coexist under `/webhook/`.

- [ ] **Step 2: Verify build**

```bash
cd api && go build ./...
```

Expected: Build succeeds.

- [ ] **Step 3: Run all tests to ensure no regression**

```bash
cd api && go test ./...
```

Expected: All tests pass (existing + new).

- [ ] **Step 4: Commit**

```bash
git add api/route.go
git commit -m "feat(api): register chatwoot webhook route at /webhook/chatwoot"
```

---

### Task 6: Integration smoke test (manual)

This task is not automated — it verifies the full flow with real Chatwoot and FastGPT instances.

- [ ] **Step 1: Fill in config.yml with real credentials**

Set actual values for `chatwoot.api_token`, `chatwoot.base_url`, `chatwoot.account_id`, `fastgpt.api_key`, `fastgpt.base_url`.

- [ ] **Step 2: Start the Center API locally**

```bash
cd api/cmd && go build -o kaitu-center . && ./kaitu-center start -f -c ../config.yml
```

- [ ] **Step 3: Configure Chatwoot webhook**

In Chatwoot: Settings → Integrations → Webhooks → Add:
- URL: `http://your-dev-host:5800/webhook/chatwoot`
- Events: `message_created`

Or use ngrok/cloudflared tunnel if running locally.

- [ ] **Step 4: Test normal AI reply**

Send a message from a Chatwoot contact. Verify:
- FastGPT is called (check Center API logs)
- AI reply appears in Chatwoot conversation

- [ ] **Step 5: Test human handoff**

Send a message that triggers `[TRANSFER_HUMAN]` in FastGPT (e.g., "转人工"). Verify:
- Reply appears in Chatwoot (marker stripped)
- Conversation status changes to "open"
- Slack notification received in `support` channel

- [ ] **Step 6: Test bot re-engagement**

In Chatwoot, change conversation status back to "pending". Send a new message. Verify bot responds again.
