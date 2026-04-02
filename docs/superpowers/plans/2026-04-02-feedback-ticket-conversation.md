# Feedback Ticket Conversation System — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable users to view their feedback tickets and have two-way conversations with support staff in the webapp, with email notifications for new replies.

**Architecture:** Backend-first approach. Add `TicketReply` model + new API endpoints (user + admin), Asynq delayed email notification task, then build webapp Feedback page (ticket list + conversation detail), update admin dashboard with reply UI, and add MCP tool. Image upload deferred to a follow-up task to avoid S3 dependency scope creep in this PR.

**Tech Stack:** Go/Gin/GORM (backend), Asynq (notification), React/MUI/Zustand (webapp), Next.js/shadcn (admin), TypeScript MCP tools

**Spec:** `docs/superpowers/specs/2026-04-02-feedback-ticket-conversation-design.md`

---

## File Map

### Backend (api/)

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `api/api_ticket_reply.go` | User-facing ticket reply handlers (list, detail, reply, unread) |
| Create | `api/api_admin_ticket_reply.go` | Admin reply + list replies handlers |
| Create | `api/worker_ticket_notify.go` | Asynq task: aggregate + send reply notification email |
| Modify | `api/model.go` | Add `TicketReply` model, add fields to `FeedbackTicket` |
| Modify | `api/type.go` | Add request/response types for ticket replies |
| Modify | `api/route.go` | Register new user + admin routes |
| Modify | `api/migrate.go` | Add `TicketReply` to AutoMigrate |
| Modify | `api/worker_integration.go` | Register `ticket:notify` handler |
| Modify | `api/api_ticket.go` | Anonymous submit: FindOrCreateUserByEmail, set last_reply_at |
| Modify | `api/api_admin_device_log.go` | Add lastReplyAt/lastReplyBy to list response, add lastReplyBy filter |

### Webapp (webapp/)

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `webapp/src/pages/Feedback.tsx` | Feedback center: ticket list + detail + reply |
| Create | `webapp/src/stores/feedback.store.ts` | Zustand store: unread count, polling |
| Modify | `webapp/src/App.tsx` | Add /feedback route, redirect /submit-ticket |
| Modify | `webapp/src/components/FeedbackButton.tsx` | Navigate to /feedback, show unread badge |
| Modify | `webapp/src/services/api-types.ts` | Add ticket reply types |
| Modify | `webapp/src/i18n/locales/zh-CN/ticket.json` | Add feedback center i18n keys |
| Modify | `webapp/src/i18n/locales/en-US/ticket.json` | English translations |

### Admin Dashboard (web/)

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `web/src/app/(manager)/manager/tickets/page.tsx` | Add reply timeline + input to detail dialog |
| Modify | `web/src/lib/api.ts` | Add replyFeedbackTicket, getTicketReplies methods + types |

### MCP Tools (tools/kaitu-center/)

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `tools/kaitu-center/src/tools/admin-feedback-tickets.ts` | Add reply_feedback_ticket, list_ticket_replies tools |

---

## Task 1: Backend Data Model

**Files:**
- Modify: `api/model.go` (after line 1034, FeedbackTicket struct)
- Modify: `api/migrate.go` (AutoMigrate list)

- [ ] **Step 1: Add TicketReply model and extend FeedbackTicket**

In `api/model.go`, after the `FeedbackTicket` struct (line 1034), add:

```go
type TicketReply struct {
	ID         uint64    `gorm:"primarykey" json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	TicketID   uint64    `gorm:"index;not null" json:"ticketId"`
	SenderType string    `gorm:"type:varchar(16);not null" json:"senderType"` // "user" | "admin"
	SenderID   *uint64   `json:"senderId,omitempty"`
	SenderName string    `gorm:"type:varchar(64)" json:"senderName"`
	Content    string    `gorm:"type:text;not null" json:"content"`
	NotifiedAt *time.Time `gorm:"index" json:"-"`
}
```

Add three fields to the `FeedbackTicket` struct (before the closing brace):

```go
	LastReplyAt  *time.Time `gorm:"index" json:"lastReplyAt,omitempty"`
	LastReplyBy  string     `gorm:"type:varchar(16)" json:"lastReplyBy,omitempty"`
	UserUnread   int        `gorm:"not null;default:0" json:"userUnread"`
```

- [ ] **Step 2: Register TicketReply in AutoMigrate**

In `api/migrate.go`, find the `&FeedbackTicket{}` line in the AutoMigrate call and add `&TicketReply{}` after it:

```go
	&FeedbackTicket{},
	&TicketReply{},
```

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/david/projects/kaitu-io/k2app/api && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add api/model.go api/migrate.go
git commit -m "feat(api): add TicketReply model and extend FeedbackTicket"
```

---

## Task 2: Backend Request/Response Types

**Files:**
- Modify: `api/type.go` (after ResolveFeedbackTicketRequest, around line 814)

- [ ] **Step 1: Add ticket reply types**

In `api/type.go`, after `ResolveFeedbackTicketRequest` (line 814), add:

```go
// Ticket reply request/response types

type CreateTicketReplyRequest struct {
	Content string `json:"content" binding:"required,min=1,max=2000"`
}

type AdminCreateTicketReplyRequest struct {
	Content    string `json:"content" binding:"required,min=1,max=2000"`
	SenderName string `json:"senderName,omitempty"` // defaults to "客服"
}

type TicketReplyResponse struct {
	ID         uint64 `json:"id"`
	SenderType string `json:"senderType"`
	SenderName string `json:"senderName"`
	Content    string `json:"content"`
	CreatedAt  int64  `json:"createdAt"`
}

type UserTicketListItem struct {
	ID          uint64 `json:"id"`
	FeedbackID  string `json:"feedbackId"`
	Content     string `json:"content"`     // truncated to 100 chars
	Status      string `json:"status"`
	UserUnread  int    `json:"userUnread"`
	LastReplyAt *int64 `json:"lastReplyAt,omitempty"`
	LastReplyBy string `json:"lastReplyBy,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
}

type UserTicketDetailResponse struct {
	ID         uint64                `json:"id"`
	FeedbackID string                `json:"feedbackId"`
	Content    string                `json:"content"`
	Status     string                `json:"status"`
	CreatedAt  int64                 `json:"createdAt"`
	ResolvedAt *int64                `json:"resolvedAt,omitempty"`
	Replies    []TicketReplyResponse `json:"replies"`
}

type UnreadCountResponse struct {
	Unread int `json:"unread"`
}
```

- [ ] **Step 2: Extend FeedbackTicketResponse**

Find `FeedbackTicketResponse` (line 830) and add fields before `LogCount`:

```go
	LastReplyAt *int64  `json:"lastReplyAt,omitempty"`
	LastReplyBy string  `json:"lastReplyBy,omitempty"`
```

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/david/projects/kaitu-io/k2app/api && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add api/type.go
git commit -m "feat(api): add ticket reply request/response types"
```

---

## Task 3: User-Facing Ticket API Handlers

**Files:**
- Create: `api/api_ticket_reply.go`
- Modify: `api/api_ticket.go` (anonymous user handling + set last_reply_at)

- [ ] **Step 1: Create api_ticket_reply.go with user handlers**

Create `api/api_ticket_reply.go`:

```go
package center

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_user_list_tickets returns the current user's tickets ordered by last_reply_at desc
func api_user_list_tickets(c *gin.Context) {
	ctx := c.Request.Context()
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	pagination := PaginationFromRequest(c)

	var tickets []FeedbackTicket
	query := db.Get().Model(&FeedbackTicket{}).Where("user_id = ?", userID)
	query.Count(&pagination.Total)
	query.Order("COALESCE(last_reply_at, created_at) DESC").
		Offset(pagination.Offset()).Limit(pagination.PageSize).
		Find(&tickets)

	items := make([]UserTicketListItem, len(tickets))
	for i, t := range tickets {
		content := t.Content
		if len([]rune(content)) > 100 {
			content = string([]rune(content)[:100]) + "..."
		}
		item := UserTicketListItem{
			ID:         t.ID,
			FeedbackID: t.FeedbackID,
			Content:    content,
			Status:     t.Status,
			UserUnread: t.UserUnread,
			LastReplyBy: t.LastReplyBy,
			CreatedAt:  t.CreatedAt.Unix(),
		}
		if t.LastReplyAt != nil {
			ts := t.LastReplyAt.Unix()
			item.LastReplyAt = &ts
		}
		items[i] = item
	}

	log.Debugf(ctx, "api_user_list_tickets: user=%d, total=%d", userID, pagination.Total)
	List(c, items, pagination)
}

// api_user_ticket_detail returns ticket detail + all replies, clears user_unread
func api_user_ticket_detail(c *gin.Context) {
	ctx := c.Request.Context()
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	id := parseUintParam(c, "id")
	if id == 0 {
		return
	}

	var ticket FeedbackTicket
	if err := db.Get().Where("id = ? AND user_id = ?", id, userID).First(&ticket).Error; err != nil {
		Error(c, ErrorNotFound, "ticket not found")
		return
	}

	// Clear unread count
	if ticket.UserUnread > 0 {
		db.Get().Model(&FeedbackTicket{}).Where("id = ?", id).Update("user_unread", 0)
	}

	var replies []TicketReply
	db.Get().Where("ticket_id = ?", id).Order("created_at ASC").Find(&replies)

	replyItems := make([]TicketReplyResponse, len(replies))
	for i, r := range replies {
		replyItems[i] = TicketReplyResponse{
			ID:         r.ID,
			SenderType: r.SenderType,
			SenderName: r.SenderName,
			Content:    r.Content,
			CreatedAt:  r.CreatedAt.Unix(),
		}
	}

	var resolvedAt *int64
	if ticket.ResolvedAt != nil {
		ts := ticket.ResolvedAt.Unix()
		resolvedAt = &ts
	}

	resp := UserTicketDetailResponse{
		ID:         ticket.ID,
		FeedbackID: ticket.FeedbackID,
		Content:    ticket.Content,
		Status:     ticket.Status,
		CreatedAt:  ticket.CreatedAt.Unix(),
		ResolvedAt: resolvedAt,
		Replies:    replyItems,
	}

	log.Debugf(ctx, "api_user_ticket_detail: user=%d, ticket=%d, replies=%d", userID, id, len(replies))
	Success(c, &resp)
}

// api_user_ticket_reply allows user to reply to their ticket
func api_user_ticket_reply(c *gin.Context) {
	ctx := c.Request.Context()
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	id := parseUintParam(c, "id")
	if id == 0 {
		return
	}

	var req CreateTicketReplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request: "+err.Error())
		return
	}

	var ticket FeedbackTicket
	if err := db.Get().Where("id = ? AND user_id = ?", id, userID).First(&ticket).Error; err != nil {
		Error(c, ErrorNotFound, "ticket not found")
		return
	}

	if ticket.Status == "closed" {
		Error(c, ErrorInvalidOperation, "cannot reply to a closed ticket")
		return
	}

	// Get user display name (email prefix)
	senderName := "user"
	if email, err := getUserEmail(ctx, userID); err == nil && email != "" {
		parts := splitEmailPrefix(email)
		senderName = parts
	}

	now := timeNow()
	reply := TicketReply{
		TicketID:   id,
		SenderType: "user",
		SenderID:   &userID,
		SenderName: senderName,
		Content:    req.Content,
	}
	if err := db.Get().Create(&reply).Error; err != nil {
		log.Errorf(ctx, "api_user_ticket_reply: failed to create reply: %v", err)
		Error(c, ErrorSystemError, "failed to create reply")
		return
	}

	// Update ticket: last_reply_at, last_reply_by, reopen if resolved
	updates := map[string]any{
		"last_reply_at": now,
		"last_reply_by": "user",
	}
	if ticket.Status == "resolved" {
		updates["status"] = "open"
	}
	db.Get().Model(&FeedbackTicket{}).Where("id = ?", id).Updates(updates)

	log.Infof(ctx, "api_user_ticket_reply: user=%d, ticket=%d, reopened=%v", userID, id, ticket.Status == "resolved")

	resp := TicketReplyResponse{
		ID:         reply.ID,
		SenderType: reply.SenderType,
		SenderName: reply.SenderName,
		Content:    reply.Content,
		CreatedAt:  reply.CreatedAt.Unix(),
	}
	Success(c, &resp)
}

// api_user_tickets_unread returns total unread count across all user tickets
func api_user_tickets_unread(c *gin.Context) {
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	var total int64
	db.Get().Model(&FeedbackTicket{}).
		Where("user_id = ? AND user_unread > 0", userID).
		Select("COALESCE(SUM(user_unread), 0)").
		Scan(&total)

	resp := UnreadCountResponse{Unread: int(total)}
	Success(c, &resp)
}

// splitEmailPrefix returns the part before @ in an email
func splitEmailPrefix(email string) string {
	for i, c := range email {
		if c == '@' {
			return email[:i]
		}
	}
	return email
}

// parseUintParam parses a uint64 from URL param, sends error response if invalid
func parseUintParam(c *gin.Context, name string) uint64 {
	val := c.Param(name)
	var id uint64
	for _, ch := range val {
		if ch < '0' || ch > '9' {
			Error(c, ErrorInvalidArgument, "invalid "+name)
			return 0
		}
		id = id*10 + uint64(ch-'0')
	}
	if id == 0 {
		Error(c, ErrorInvalidArgument, "invalid "+name)
		return 0
	}
	return id
}

// timeNow returns current time (extracted for testability)
func timeNow() time.Time {
	return time.Now()
}
```

Wait — `time` import is needed. Let me include the full import block. Also check: does `strconv` already exist? Let me use manual parsing to avoid import. Actually, let me just use strconv.ParseUint since it's standard.

Replace the `parseUintParam` with using strconv:

```go
import (
	"strconv"
	"time"
	// ... other imports already listed
)
```

And `parseUintParam`:

```go
func parseUintParam(c *gin.Context, name string) uint64 {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		Error(c, ErrorInvalidArgument, "invalid "+name)
		return 0
	}
	return id
}
```

- [ ] **Step 2: Modify api_ticket.go — anonymous user auto-creation + set last_reply_at**

In `api/api_ticket.go`, in the `api_create_ticket` handler, find the section where anonymous email is validated (around line 50-60). After email validation for anonymous users, add user creation:

```go
	// After validating anonymous email, create user
	if userID == 0 && userEmail != "" {
		user, err := FindOrCreateUserByEmail(ctx, userEmail, req.Language)
		if err != nil {
			log.Warnf(ctx, "api_create_ticket: failed to create user for anonymous email: %v", err)
		} else {
			userID = user.ID
			userIDPtr = &userID
		}
	}
```

Also, when creating the FeedbackTicket struct, add `LastReplyAt` initialization:

```go
	now := time.Now()
	ticket := FeedbackTicket{
		// ... existing fields ...
		LastReplyAt: &now,
	}
```

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/david/projects/kaitu-io/k2app/api && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add api/api_ticket_reply.go api/api_ticket.go
git commit -m "feat(api): add user-facing ticket reply handlers"
```

---

## Task 4: Admin Ticket Reply Handlers

**Files:**
- Create: `api/api_admin_ticket_reply.go`
- Modify: `api/api_admin_device_log.go` (extend list response)

- [ ] **Step 1: Create api_admin_ticket_reply.go**

Create `api/api_admin_ticket_reply.go`:

```go
package center

import (
	"time"

	"github.com/gin-gonic/gin"
	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_admin_reply_ticket allows admin to reply to a ticket
func api_admin_reply_ticket(c *gin.Context) {
	ctx := c.Request.Context()
	adminUserID := ReqUserID(c)

	id := parseUintParam(c, "id")
	if id == 0 {
		return
	}

	var req AdminCreateTicketReplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request: "+err.Error())
		return
	}

	// Verify ticket exists
	var ticket FeedbackTicket
	if err := db.Get().Where("id = ?", id).First(&ticket).Error; err != nil {
		Error(c, ErrorNotFound, "ticket not found")
		return
	}

	senderName := req.SenderName
	if senderName == "" {
		senderName = "客服"
	}

	now := time.Now()
	reply := TicketReply{
		TicketID:   id,
		SenderType: "admin",
		SenderID:   &adminUserID,
		SenderName: senderName,
		Content:    req.Content,
	}
	if err := db.Get().Create(&reply).Error; err != nil {
		log.Errorf(ctx, "api_admin_reply_ticket: failed to create reply: %v", err)
		Error(c, ErrorSystemError, "failed to create reply")
		return
	}

	// Update ticket: last_reply_at, last_reply_by, increment user_unread
	db.Get().Model(&FeedbackTicket{}).Where("id = ?", id).Updates(map[string]any{
		"last_reply_at": now,
		"last_reply_by": "admin",
		"user_unread":   ticket.UserUnread + 1,
	})

	// Enqueue delayed notification (5 min, deduplicated per ticket)
	enqueueTicketNotification(ctx, id)

	WriteAuditLog(c, "ticket_reply", "ticket", formatUint(id), map[string]any{
		"content": req.Content,
	})

	log.Infof(ctx, "api_admin_reply_ticket: admin=%d, ticket=%d, senderName=%s", adminUserID, id, senderName)

	resp := TicketReplyResponse{
		ID:         reply.ID,
		SenderType: reply.SenderType,
		SenderName: reply.SenderName,
		Content:    reply.Content,
		CreatedAt:  reply.CreatedAt.Unix(),
	}
	Success(c, &resp)
}

// api_admin_list_ticket_replies returns all replies for a ticket
func api_admin_list_ticket_replies(c *gin.Context) {
	id := parseUintParam(c, "id")
	if id == 0 {
		return
	}

	var replies []TicketReply
	db.Get().Where("ticket_id = ?", id).Order("created_at ASC").Find(&replies)

	items := make([]TicketReplyResponse, len(replies))
	for i, r := range replies {
		items[i] = TicketReplyResponse{
			ID:         r.ID,
			SenderType: r.SenderType,
			SenderName: r.SenderName,
			Content:    r.Content,
			CreatedAt:  r.CreatedAt.Unix(),
		}
	}

	ItemsAll(c, items)
}

// enqueueTicketNotification enqueues a delayed notification task
func enqueueTicketNotification(ctx context.Context, ticketID uint64) {
	payload := TicketNotifyPayload{TicketID: ticketID}
	_, err := asynq.Enqueue(TaskTypeTicketNotify, payload,
		hibikenAsynq.ProcessIn(5*time.Minute),
		hibikenAsynq.Unique(10*time.Minute),
	)
	if err != nil && err != hibikenAsynq.ErrDuplicateTask {
		log.Errorf(ctx, "enqueueTicketNotification: failed to enqueue for ticket %d: %v", ticketID, err)
	}
}

// formatUint is a simple uint64 to string helper
func formatUint(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte(n%10) + '0'
		n /= 10
	}
	return string(buf[i:])
}
```

- [ ] **Step 2: Extend admin list response in api_admin_device_log.go**

In `api_admin_device_log.go`, in the `api_admin_list_feedback_tickets` handler, find where `FeedbackTicketResponse` is built (around line 110-130). Add `LastReplyAt` and `LastReplyBy` fields:

```go
	resp := FeedbackTicketResponse{
		// ... existing fields ...
	}
	if t.LastReplyAt != nil {
		ts := t.LastReplyAt.Unix()
		resp.LastReplyAt = &ts
	}
	resp.LastReplyBy = t.LastReplyBy
```

Also add `last_reply_by` filter support in the query section:

```go
	if lastReplyBy := c.Query("lastReplyBy"); lastReplyBy != "" {
		query = query.Where("last_reply_by = ?", lastReplyBy)
	}
```

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/david/projects/kaitu-io/k2app/api && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add api/api_admin_ticket_reply.go api/api_admin_device_log.go
git commit -m "feat(api): add admin ticket reply handlers"
```

---

## Task 5: Asynq Notification Worker

**Files:**
- Create: `api/worker_ticket_notify.go`
- Modify: `api/worker_integration.go`

- [ ] **Step 1: Create worker_ticket_notify.go**

Create `api/worker_ticket_notify.go`:

```go
package center

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

const TaskTypeTicketNotify = "ticket:notify"

type TicketNotifyPayload struct {
	TicketID uint64 `json:"ticketId"`
}

func handleTicketNotify(ctx context.Context, payload []byte) error {
	var p TicketNotifyPayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	log.Infof(ctx, "[TICKET-NOTIFY] Processing notification for ticket %d", p.TicketID)

	// Find pending (un-notified) admin replies
	var replies []TicketReply
	db.Get().Where("ticket_id = ? AND sender_type = ? AND notified_at IS NULL", p.TicketID, "admin").
		Order("created_at ASC").
		Find(&replies)

	if len(replies) == 0 {
		log.Debugf(ctx, "[TICKET-NOTIFY] No pending replies for ticket %d, skipping", p.TicketID)
		return nil
	}

	// Get the ticket and user email
	var ticket FeedbackTicket
	if err := db.Get().Where("id = ?", p.TicketID).First(&ticket).Error; err != nil {
		return fmt.Errorf("ticket %d not found: %w", p.TicketID, err)
	}

	if ticket.UserID == nil {
		log.Warnf(ctx, "[TICKET-NOTIFY] Ticket %d has no user_id, skipping email", p.TicketID)
		return nil
	}

	userEmail, err := getUserEmail(ctx, *ticket.UserID)
	if err != nil || userEmail == "" {
		log.Warnf(ctx, "[TICKET-NOTIFY] Cannot get email for user %d: %v", *ticket.UserID, err)
		return nil // Don't retry — user has no email
	}

	// Build email body
	var body bytes.Buffer
	body.WriteString("您好，\n\n")
	body.WriteString(fmt.Sprintf("您的反馈工单有 %d 条新回复：\n\n", len(replies)))
	body.WriteString("────────────────────────────────────\n")
	for _, r := range replies {
		body.WriteString(fmt.Sprintf("[%s] %s\n", r.SenderName, r.CreatedAt.Format("2006-01-02 15:04")))
		body.WriteString(r.Content)
		body.WriteString("\n\n")
	}
	body.WriteString("────────────────────────────────────\n\n")
	body.WriteString("— Kaitu 团队\n")

	subject := "[Kaitu] 您的反馈工单有新回复"
	if err := sendSystemEmail(ctx, userEmail, subject, body.String()); err != nil {
		log.Errorf(ctx, "[TICKET-NOTIFY] Failed to send email to %s for ticket %d: %v",
			hideEmail(userEmail), p.TicketID, err)
		return fmt.Errorf("send email: %w", err) // Will retry
	}

	// Mark replies as notified
	now := time.Now()
	replyIDs := make([]uint64, len(replies))
	for i, r := range replies {
		replyIDs[i] = r.ID
	}
	db.Get().Model(&TicketReply{}).Where("id IN ?", replyIDs).Update("notified_at", now)

	log.Infof(ctx, "[TICKET-NOTIFY] Sent %d reply notifications for ticket %d to %s",
		len(replies), p.TicketID, hideEmail(userEmail))
	return nil
}
```

- [ ] **Step 2: Register handler in worker_integration.go**

In `api/worker_integration.go`, in the `InitWorker` function, add after the existing handler registrations:

```go
	// Ticket reply notification handler
	asynq.Handle(TaskTypeTicketNotify, handleTicketNotify)
```

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/david/projects/kaitu-io/k2app/api && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add api/worker_ticket_notify.go api/worker_integration.go
git commit -m "feat(api): add ticket reply notification worker (5min aggregation)"
```

---

## Task 6: Route Registration

**Files:**
- Modify: `api/route.go`

- [ ] **Step 1: Add user ticket routes**

In `api/route.go`, find the user route group (around line 151-155, where `/ticket` and `/feedback-notify` are). Add after the existing ticket route:

```go
	// Ticket conversation (user)
	user.GET("/tickets", AuthRequired(), api_user_list_tickets)
	user.GET("/tickets/unread", AuthRequired(), api_user_tickets_unread)
	user.GET("/tickets/:id", AuthRequired(), api_user_ticket_detail)
	user.POST("/tickets/:id/reply", AuthRequired(), api_user_ticket_reply)
```

- [ ] **Step 2: Add admin ticket routes**

In `api/route.go`, find the opsAdmin feedback-tickets section (around line 388-392). Add after the existing close route:

```go
	opsAdmin.POST("/feedback-tickets/:id/reply", RoleRequired(RoleSupport), api_admin_reply_ticket)
	opsAdmin.GET("/feedback-tickets/:id/replies", RoleRequired(allOpsRoles), api_admin_list_ticket_replies)
```

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/david/projects/kaitu-io/k2app/api && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add api/route.go
git commit -m "feat(api): register ticket conversation routes"
```

---

## Task 7: Webapp Types and Store

**Files:**
- Modify: `webapp/src/services/api-types.ts`
- Create: `webapp/src/stores/feedback.store.ts`

- [ ] **Step 1: Add ticket reply types to api-types.ts**

In `webapp/src/services/api-types.ts`, after `CreateTicketRequest` (line 511), add:

```typescript
// Ticket conversation types
export interface TicketReply {
  id: number;
  senderType: 'user' | 'admin';
  senderName: string;
  content: string;
  createdAt: number;
}

export interface UserTicketListItem {
  id: number;
  feedbackId: string;
  content: string;
  status: 'open' | 'resolved' | 'closed';
  userUnread: number;
  lastReplyAt?: number;
  lastReplyBy?: string;
  createdAt: number;
}

export interface UserTicketDetail {
  id: number;
  feedbackId: string;
  content: string;
  status: 'open' | 'resolved' | 'closed';
  createdAt: number;
  resolvedAt?: number;
  replies: TicketReply[];
}

export interface UnreadCount {
  unread: number;
}
```

- [ ] **Step 2: Create feedback.store.ts**

Create `webapp/src/stores/feedback.store.ts`:

```typescript
import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import { cloudApi } from '../services/cloud-api';
import type { UnreadCount } from '../services/api-types';

interface FeedbackState {
  unreadCount: number;
  _pollTimer: ReturnType<typeof setInterval> | null;

  fetchUnread: () => Promise<void>;
  startPolling: () => void;
  stopPolling: () => void;
  decrementUnread: (count: number) => void;
}

export const useFeedbackStore = create<FeedbackState>()(
  subscribeWithSelector((set, get) => ({
    unreadCount: 0,
    _pollTimer: null,

    fetchUnread: async () => {
      const response = await cloudApi.get<UnreadCount>('/api/user/tickets/unread');
      if (response.code === 0 && response.data) {
        set({ unreadCount: response.data.unread });
      }
    },

    startPolling: () => {
      const state = get();
      if (state._pollTimer) return; // Already polling

      // Fetch immediately
      state.fetchUnread();

      // Then every 60s
      const timer = setInterval(() => {
        get().fetchUnread();
      }, 60_000);
      set({ _pollTimer: timer });
    },

    stopPolling: () => {
      const timer = get()._pollTimer;
      if (timer) {
        clearInterval(timer);
        set({ _pollTimer: null, unreadCount: 0 });
      }
    },

    decrementUnread: (count: number) => {
      set((state) => ({
        unreadCount: Math.max(0, state.unreadCount - count),
      }));
    },
  }))
);
```

- [ ] **Step 3: Verify TypeScript compilation**

Run: `cd /Users/david/projects/kaitu-io/k2app/webapp && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add webapp/src/services/api-types.ts webapp/src/stores/feedback.store.ts
git commit -m "feat(webapp): add ticket reply types and feedback store"
```

---

## Task 8: Webapp Feedback Page

**Files:**
- Create: `webapp/src/pages/Feedback.tsx`

- [ ] **Step 1: Create Feedback.tsx**

Create `webapp/src/pages/Feedback.tsx`. This is the main page with two views: ticket list and ticket detail/conversation.

```tsx
import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Box,
  Typography,
  Card,
  CardContent,
  CardActionArea,
  Chip,
  Stack,
  CircularProgress,
  Alert,
  Button,
  TextField,
  Divider,
  Badge,
} from '@mui/material';
import {
  Add as AddIcon,
  ArrowBack as ArrowBackIcon,
  Send as SendIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

import BackButton from '../components/BackButton';
import { cloudApi } from '../services/cloud-api';
import { useFeedbackStore } from '../stores/feedback.store';
import type {
  UserTicketListItem,
  UserTicketDetail,
  TicketReply,
} from '../services/api-types';

const STATUS_COLORS: Record<string, 'warning' | 'success' | 'default'> = {
  open: 'warning',
  resolved: 'success',
  closed: 'default',
};

function formatRelativeTime(timestamp: number): string {
  const now = Date.now() / 1000;
  const diff = now - timestamp;
  if (diff < 60) return '刚刚';
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`;
  if (diff < 2592000) return `${Math.floor(diff / 86400)} 天前`;
  return new Date(timestamp * 1000).toLocaleDateString();
}

function formatTime(timestamp: number): string {
  return new Date(timestamp * 1000).toLocaleString(undefined, {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

// ── Ticket List View ──────────────────────────────────────

function TicketList({
  onSelect,
  onNew,
}: {
  onSelect: (id: number) => void;
  onNew: () => void;
}) {
  const { t } = useTranslation();
  const [tickets, setTickets] = useState<UserTicketListItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchTickets = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await cloudApi.get<{
        items: UserTicketListItem[];
        pagination: { total: number };
      }>('/api/user/tickets?pageSize=50');
      if (response.code === 0 && response.data) {
        setTickets(response.data.items || []);
      } else {
        setError(t('ticket:feedback.loadError'));
      }
    } catch {
      setError(t('ticket:feedback.loadError'));
    } finally {
      setIsLoading(false);
    }
  }, [t]);

  useEffect(() => {
    fetchTickets();
  }, [fetchTickets]);

  if (isLoading) {
    return (
      <Box display="flex" justifyContent="center" py={6}>
        <CircularProgress />
      </Box>
    );
  }

  if (error) {
    return (
      <Alert
        severity="error"
        action={
          <Button color="inherit" size="small" onClick={fetchTickets}>
            {t('common:common.retry')}
          </Button>
        }
      >
        {error}
      </Alert>
    );
  }

  // No tickets → show new ticket form directly
  if (tickets.length === 0) {
    return null; // Parent will render SubmitTicket
  }

  return (
    <Stack spacing={1.5}>
      <Stack direction="row" justifyContent="space-between" alignItems="center">
        <Typography variant="h6">{t('ticket:feedback.title')}</Typography>
        <Button
          variant="contained"
          size="small"
          startIcon={<AddIcon />}
          onClick={onNew}
        >
          {t('ticket:feedback.newTicket')}
        </Button>
      </Stack>

      {tickets.map((ticket) => (
        <Card key={ticket.id}>
          <CardActionArea onClick={() => onSelect(ticket.id)}>
            <CardContent sx={{ py: 1.5 }}>
              <Stack spacing={0.5}>
                <Stack direction="row" alignItems="center" gap={1}>
                  {ticket.userUnread > 0 && (
                    <Badge
                      color="error"
                      variant="dot"
                      sx={{ '& .MuiBadge-dot': { width: 8, height: 8 } }}
                    >
                      <Box />
                    </Badge>
                  )}
                  <Typography
                    variant="body2"
                    sx={{
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                      flex: 1,
                      fontWeight: ticket.userUnread > 0 ? 600 : 400,
                    }}
                  >
                    {ticket.content}
                  </Typography>
                </Stack>
                <Stack direction="row" alignItems="center" gap={1}>
                  <Chip
                    label={t(`ticket:feedback.status.${ticket.status}`)}
                    size="small"
                    color={STATUS_COLORS[ticket.status] || 'default'}
                    sx={{ height: 20, fontSize: '0.7rem' }}
                  />
                  <Typography variant="caption" color="text.secondary">
                    {ticket.lastReplyBy === 'admin'
                      ? t('ticket:feedback.adminReplied')
                      : ''}
                    {' '}
                    {formatRelativeTime(ticket.lastReplyAt || ticket.createdAt)}
                  </Typography>
                </Stack>
              </Stack>
            </CardContent>
          </CardActionArea>
        </Card>
      ))}
    </Stack>
  );
}

// ── Ticket Detail View ────────────────────────────────────

function TicketDetail({
  ticketId,
  onBack,
}: {
  ticketId: number;
  onBack: () => void;
}) {
  const { t } = useTranslation();
  const decrementUnread = useFeedbackStore((s) => s.decrementUnread);

  const [detail, setDetail] = useState<UserTicketDetail | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Reply form
  const [replyContent, setReplyContent] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);

  const fetchDetail = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await cloudApi.get<UserTicketDetail>(
        `/api/user/tickets/${ticketId}`
      );
      if (response.code === 0 && response.data) {
        setDetail(response.data);
        // Decrement unread in local store (detail API clears server-side)
        decrementUnread(response.data.replies.length); // approximate
      } else {
        setError(t('ticket:feedback.loadError'));
      }
    } catch {
      setError(t('ticket:feedback.loadError'));
    } finally {
      setIsLoading(false);
    }
  }, [ticketId, t, decrementUnread]);

  useEffect(() => {
    fetchDetail();
  }, [fetchDetail]);

  const handleReply = async () => {
    if (!replyContent.trim()) return;
    setIsSubmitting(true);
    try {
      const response = await cloudApi.post<TicketReply>(
        `/api/user/tickets/${ticketId}/reply`,
        { content: replyContent.trim() }
      );
      if (response.code === 0 && response.data) {
        setDetail((prev) =>
          prev
            ? {
                ...prev,
                replies: [...prev.replies, response.data!],
                status: prev.status === 'resolved' ? 'open' : prev.status,
              }
            : prev
        );
        setReplyContent('');
      }
    } catch (err) {
      console.error('[Feedback] Failed to submit reply:', err);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Stack spacing={1.5} sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* Header */}
      <Stack direction="row" alignItems="center" gap={1}>
        <Button size="small" startIcon={<ArrowBackIcon />} onClick={onBack}>
          {t('common:common.back')}
        </Button>
        <Box flex={1} />
        {detail && (
          <Chip
            label={t(`ticket:feedback.status.${detail.status}`)}
            size="small"
            color={STATUS_COLORS[detail.status] || 'default'}
          />
        )}
      </Stack>

      {/* Content */}
      {isLoading ? (
        <Box display="flex" justifyContent="center" py={4}>
          <CircularProgress />
        </Box>
      ) : error ? (
        <Alert severity="error">{error}</Alert>
      ) : detail ? (
        <>
          {/* Original ticket */}
          <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
            <Card
              sx={{
                maxWidth: '80%',
                bgcolor: 'primary.dark',
              }}
            >
              <CardContent sx={{ py: 1, px: 1.5, '&:last-child': { pb: 1 } }}>
                <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap' }}>
                  {detail.content}
                </Typography>
                <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: 'block' }}>
                  {formatTime(detail.createdAt)}
                </Typography>
              </CardContent>
            </Card>
          </Box>

          {/* Replies */}
          {detail.replies.map((reply) => {
            const isUser = reply.senderType === 'user';
            return (
              <Box
                key={reply.id}
                sx={{
                  display: 'flex',
                  justifyContent: isUser ? 'flex-end' : 'flex-start',
                }}
              >
                <Card
                  sx={{
                    maxWidth: '80%',
                    bgcolor: isUser ? 'primary.dark' : 'background.paper',
                    ...(isUser ? {} : { borderLeft: '3px solid', borderColor: 'primary.main' }),
                  }}
                >
                  <CardContent sx={{ py: 1, px: 1.5, '&:last-child': { pb: 1 } }}>
                    {!isUser && (
                      <Typography variant="caption" color="primary" fontWeight={600}>
                        {reply.senderName}
                      </Typography>
                    )}
                    <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap' }}>
                      {reply.content}
                    </Typography>
                    <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: 'block' }}>
                      {formatTime(reply.createdAt)}
                    </Typography>
                  </CardContent>
                </Card>
              </Box>
            );
          })}

          {/* Reply input */}
          {detail.status !== 'closed' ? (
            <Card sx={{ mt: 'auto' }}>
              <CardContent sx={{ py: 1.5, '&:last-child': { pb: 1.5 } }}>
                <Stack direction="row" spacing={1} alignItems="flex-end">
                  <TextField
                    multiline
                    maxRows={4}
                    placeholder={t('ticket:feedback.replyPlaceholder')}
                    value={replyContent}
                    onChange={(e) => setReplyContent(e.target.value)}
                    disabled={isSubmitting}
                    fullWidth
                    size="small"
                    inputProps={{ maxLength: 2000 }}
                  />
                  <Button
                    variant="contained"
                    onClick={handleReply}
                    disabled={isSubmitting || !replyContent.trim()}
                    sx={{ minWidth: 'auto', px: 1.5 }}
                  >
                    {isSubmitting ? (
                      <CircularProgress size={20} color="inherit" />
                    ) : (
                      <SendIcon fontSize="small" />
                    )}
                  </Button>
                </Stack>
              </CardContent>
            </Card>
          ) : (
            <Alert severity="info" sx={{ mt: 'auto' }}>
              {t('ticket:feedback.closedNotice')}
            </Alert>
          )}
        </>
      ) : null}
    </Stack>
  );
}

// ── Main Feedback Page ────────────────────────────────────

export default function Feedback() {
  const [selectedTicketId, setSelectedTicketId] = useState<number | null>(null);
  const [showNewTicket, setShowNewTicket] = useState(false);
  const navigate = useNavigate();

  // If creating new ticket, navigate to existing submit-ticket page
  // (will be redirected back here after submission in a future iteration)
  if (showNewTicket) {
    navigate('/submit-ticket-form');
    return null;
  }

  if (selectedTicketId !== null) {
    return (
      <Box sx={{ width: '100%', height: '100%', position: 'relative' }}>
        <Box
          sx={{
            width: '100%',
            height: '100%',
            display: 'flex',
            justifyContent: 'center',
            pt: 2,
            px: 2,
          }}
        >
          <Box sx={{ width: 500, height: '100%' }}>
            <TicketDetail
              ticketId={selectedTicketId}
              onBack={() => setSelectedTicketId(null)}
            />
          </Box>
        </Box>
      </Box>
    );
  }

  return (
    <Box sx={{ width: '100%', height: '100%', position: 'relative' }}>
      <BackButton to="/" />
      <Box
        sx={{
          width: '100%',
          height: '100%',
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'flex-start',
          pt: 9,
          px: 2,
        }}
      >
        <Box sx={{ width: 500, pb: 4 }}>
          <TicketList
            onSelect={setSelectedTicketId}
            onNew={() => setShowNewTicket(true)}
          />
        </Box>
      </Box>
    </Box>
  );
}
```

Note: The "no tickets" case currently returns null from TicketList. In the actual implementation, the Feedback page should detect this and render the SubmitTicket form inline. For now, "New Ticket" navigates to `/submit-ticket-form` (which we'll set up as the old SubmitTicket route).

- [ ] **Step 2: Verify TypeScript compilation**

Run: `cd /Users/david/projects/kaitu-io/k2app/webapp && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add webapp/src/pages/Feedback.tsx
git commit -m "feat(webapp): add Feedback page with ticket list and conversation"
```

---

## Task 9: Webapp Routing and FeedbackButton Updates

**Files:**
- Modify: `webapp/src/App.tsx`
- Modify: `webapp/src/components/FeedbackButton.tsx`

- [ ] **Step 1: Update App.tsx routes**

In `webapp/src/App.tsx`, find the feedback routes section (around line 88-95). Replace:

```tsx
{appConfig.features.feedback && (
  <>
    <Route path="faq" element={<FAQ />} />
    <Route path="issues" element={<LoginRequiredGuard pagePath="/issues"><Issues /></LoginRequiredGuard>} />
    <Route path="issues/:number" element={<LoginRequiredGuard pagePath="/issues"><IssueDetail /></LoginRequiredGuard>} />
    <Route path="submit-ticket" element={<MembershipGuard><SubmitTicket /></MembershipGuard>} />
  </>
)}
```

With:

```tsx
{appConfig.features.feedback && (
  <>
    <Route path="faq" element={<FAQ />} />
    <Route path="issues" element={<LoginRequiredGuard pagePath="/issues"><Issues /></LoginRequiredGuard>} />
    <Route path="issues/:number" element={<LoginRequiredGuard pagePath="/issues"><IssueDetail /></LoginRequiredGuard>} />
    <Route path="feedback" element={<LoginRequiredGuard pagePath="/feedback"><Feedback /></LoginRequiredGuard>} />
    <Route path="submit-ticket" element={<Navigate to="/feedback" replace />} />
    <Route path="submit-ticket-form" element={<MembershipGuard><SubmitTicket /></MembershipGuard>} />
  </>
)}
```

Add imports at the top of the file:

```tsx
import Feedback from './pages/Feedback';
import { Navigate } from 'react-router-dom';
```

(Note: `Navigate` may already be imported — check first.)

- [ ] **Step 2: Update FeedbackButton.tsx**

In `webapp/src/components/FeedbackButton.tsx`, make two changes:

1. Change navigation target (line 38):
```tsx
navigate('/feedback');
```
(Remove the `?feedback=true` query param)

2. Add unread badge. Import the store and Badge:
```tsx
import { Badge } from '@mui/material';
import { useFeedbackStore } from '../stores/feedback.store';
```

Inside the component, add:
```tsx
const unreadCount = useFeedbackStore((s) => s.unreadCount);
```

Wrap the existing `<Fab>` with a Badge:
```tsx
<Badge
  badgeContent={unreadCount}
  color="error"
  invisible={unreadCount === 0}
  sx={{
    '& .MuiBadge-badge': {
      right: 4,
      top: 4,
      minWidth: 16,
      height: 16,
      fontSize: '0.65rem',
    },
  }}
>
  <Fab ... >
    <FeedbackIcon />
  </Fab>
</Badge>
```

- [ ] **Step 3: Start/stop polling on auth state change**

In `webapp/src/stores/feedback.store.ts` — no changes needed, but we need to connect polling to auth state. In `webapp/src/stores/index.ts`, find `initializeAllStores()` and add feedback polling start after auth init:

```typescript
import { useFeedbackStore } from './feedback.store';

// In initializeAllStores, after auth initialization:
const authUnsub = useAuthStore.subscribe(
  (state) => state.isAuthenticated,
  (isAuthenticated) => {
    if (isAuthenticated) {
      useFeedbackStore.getState().startPolling();
    } else {
      useFeedbackStore.getState().stopPolling();
    }
  }
);

// In the cleanup function:
useFeedbackStore.getState().stopPolling();
```

- [ ] **Step 4: Verify TypeScript and visual check**

Run: `cd /Users/david/projects/kaitu-io/k2app/webapp && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add webapp/src/App.tsx webapp/src/components/FeedbackButton.tsx webapp/src/stores/index.ts webapp/src/stores/feedback.store.ts
git commit -m "feat(webapp): wire up feedback routes, badge, and polling"
```

---

## Task 10: i18n Translations

**Files:**
- Modify: `webapp/src/i18n/locales/zh-CN/ticket.json`
- Modify: `webapp/src/i18n/locales/en-US/ticket.json`

- [ ] **Step 1: Add zh-CN feedback keys**

In `webapp/src/i18n/locales/zh-CN/ticket.json`, add a `"feedback"` section at the top level:

```json
  "feedback": {
    "title": "反馈中心",
    "newTicket": "提交新工单",
    "loadError": "加载失败，请重试",
    "status": {
      "open": "处理中",
      "resolved": "已解决",
      "closed": "已关闭"
    },
    "adminReplied": "客服回复于",
    "replyPlaceholder": "请输入回复...",
    "closedNotice": "工单已关闭",
    "loginToView": "使用邮箱 {{email}} 登录即可查看进展和回复"
  }
```

- [ ] **Step 2: Add en-US feedback keys**

In `webapp/src/i18n/locales/en-US/ticket.json`, add:

```json
  "feedback": {
    "title": "Feedback Center",
    "newTicket": "New Ticket",
    "loadError": "Failed to load, please retry",
    "status": {
      "open": "Open",
      "resolved": "Resolved",
      "closed": "Closed"
    },
    "adminReplied": "Support replied",
    "replyPlaceholder": "Type your reply...",
    "closedNotice": "This ticket is closed",
    "loginToView": "Log in with {{email}} to view progress and replies"
  }
```

- [ ] **Step 3: Commit**

```bash
git add webapp/src/i18n/locales/zh-CN/ticket.json webapp/src/i18n/locales/en-US/ticket.json
git commit -m "feat(i18n): add feedback center translations (zh-CN, en-US)"
```

---

## Task 11: Admin Dashboard Reply UI

**Files:**
- Modify: `web/src/app/(manager)/manager/tickets/page.tsx`
- Modify: `web/src/lib/api.ts`

- [ ] **Step 1: Add API methods to web/src/lib/api.ts**

After the `closeFeedbackTicket` method (line ~2116), add:

```typescript
async replyFeedbackTicket(id: number, content: string, senderName?: string): Promise<void> {
  return this.request<void>(`/app/feedback-tickets/${id}/reply`, {
    method: 'POST',
    body: JSON.stringify({ content, senderName }),
  });
}

async getTicketReplies(id: number): Promise<{ items: FeedbackTicketReply[] }> {
  return this.request<{ items: FeedbackTicketReply[] }>(`/app/feedback-tickets/${id}/replies`);
}
```

Add the `FeedbackTicketReply` type after `FeedbackTicketListParams` (line ~2449):

```typescript
export interface FeedbackTicketReply {
  id: number;
  senderType: 'user' | 'admin';
  senderName: string;
  content: string;
  createdAt: number;
}
```

Add `lastReplyAt`, `lastReplyBy` to the existing `FeedbackTicket` interface:

```typescript
export interface FeedbackTicket {
  // ... existing fields ...
  lastReplyAt?: number;
  lastReplyBy?: string;
}
```

- [ ] **Step 2: Add reply UI to tickets page detail dialog**

In `web/src/app/(manager)/manager/tickets/page.tsx`, enhance the detail dialog section (around line 399-511). Add reply timeline and input:

1. Add state for replies and reply input:
```tsx
const [replies, setReplies] = useState<FeedbackTicketReply[]>([]);
const [replyContent, setReplyContent] = useState('');
const [isReplying, setIsReplying] = useState(false);
```

2. Fetch replies when dialog opens:
```tsx
useEffect(() => {
  if (selectedTicket) {
    api.getTicketReplies(selectedTicket.id).then((data) => {
      if (data?.items) setReplies(data.items);
    });
  }
}, [selectedTicket]);
```

3. Add reply handler:
```tsx
const handleReply = async () => {
  if (!replyContent.trim() || !selectedTicket) return;
  setIsReplying(true);
  try {
    await api.replyFeedbackTicket(selectedTicket.id, replyContent.trim());
    toast.success('Reply sent');
    setReplyContent('');
    // Refresh replies
    const data = await api.getTicketReplies(selectedTicket.id);
    if (data?.items) setReplies(data.items);
  } catch {
    toast.error('Failed to send reply');
  } finally {
    setIsReplying(false);
  }
};
```

4. In the DialogContent, after existing ticket info, add reply timeline:
```tsx
{/* Reply Timeline */}
<div className="mt-4 border-t pt-4">
  <h4 className="text-sm font-medium mb-3">Replies ({replies.length})</h4>
  <div className="space-y-3 max-h-64 overflow-y-auto">
    {replies.map((reply) => (
      <div
        key={reply.id}
        className={`p-2 rounded text-sm ${
          reply.senderType === 'admin'
            ? 'bg-blue-500/10 border-l-2 border-blue-500'
            : 'bg-muted'
        }`}
      >
        <div className="flex items-center gap-2 mb-1">
          <span className="font-medium text-xs">
            {reply.senderType === 'admin' ? reply.senderName : 'User'}
          </span>
          <span className="text-xs text-muted-foreground">
            {new Date(reply.createdAt * 1000).toLocaleString()}
          </span>
        </div>
        <p className="whitespace-pre-wrap">{reply.content}</p>
      </div>
    ))}
  </div>

  {/* Reply Input */}
  <div className="mt-3 flex gap-2">
    <Textarea
      placeholder="Type reply..."
      value={replyContent}
      onChange={(e) => setReplyContent(e.target.value)}
      className="flex-1"
      rows={2}
    />
    <Button
      onClick={handleReply}
      disabled={isReplying || !replyContent.trim()}
      size="sm"
    >
      {isReplying ? 'Sending...' : 'Send'}
    </Button>
  </div>
</div>
```

5. Add "Waiting for reply" indicator in list. In the columns definition, add `lastReplyBy` to the existing data display or as a visual indicator:
```tsx
// In the Status column cell renderer, add:
{row.original.lastReplyBy === 'user' && row.original.status === 'open' && (
  <Badge variant="outline" className="ml-1 text-xs">
    Awaiting
  </Badge>
)}
```

- [ ] **Step 3: Add Textarea import**

Import Textarea from shadcn:
```tsx
import { Textarea } from "@/components/ui/textarea";
```

(If Textarea doesn't exist, use a standard `<textarea>` with Tailwind classes.)

- [ ] **Step 4: Verify build**

Run: `cd /Users/david/projects/kaitu-io/k2app/web && yarn build`
Expected: BUILD SUCCESS

- [ ] **Step 5: Commit**

```bash
git add web/src/app/\(manager\)/manager/tickets/page.tsx web/src/lib/api.ts
git commit -m "feat(web): add reply UI to admin tickets page"
```

---

## Task 12: MCP Tools

**Files:**
- Modify: `tools/kaitu-center/src/tools/admin-feedback-tickets.ts`

- [ ] **Step 1: Add reply and list-replies tools**

In `tools/kaitu-center/src/tools/admin-feedback-tickets.ts`, after the existing `close_feedback_ticket` tool definition, add:

```typescript
export const replyFeedbackTicket = defineApiTool("reply_feedback_ticket", {
  description: "Reply to a feedback ticket as admin. Triggers aggregated email notification to the user after 5 minutes.",
  path: "/app/feedback-tickets/{id}/reply",
  method: "POST",
  params: {
    id: z.number().describe("Feedback ticket ID"),
    content: z.string().min(1).max(2000).describe("Reply content"),
    sender_name: z.string().optional().default("claude").describe("Display name shown to user"),
  },
  buildRequest: (params) => ({
    pathParams: { id: params.id },
    body: { content: params.content, senderName: params.sender_name },
  }),
});

export const listTicketReplies = defineApiTool("list_ticket_replies", {
  description: "List all replies for a feedback ticket, ordered by creation time.",
  path: "/app/feedback-tickets/{id}/replies",
  method: "GET",
  params: {
    id: z.number().describe("Feedback ticket ID"),
  },
  buildRequest: (params) => ({
    pathParams: { id: params.id },
  }),
});
```

- [ ] **Step 2: Register tools in index**

Check if tools are auto-registered or need explicit export/registration in the tools index. Find the pattern used by existing tools.

- [ ] **Step 3: Verify build**

Run: `cd /Users/david/projects/kaitu-io/k2app/tools/kaitu-center && npm run build`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add tools/kaitu-center/src/tools/admin-feedback-tickets.ts
git commit -m "feat(mcp): add reply_feedback_ticket and list_ticket_replies tools"
```

---

## Task 13: Final Verification

- [ ] **Step 1: Run all backend tests**

Run: `cd /Users/david/projects/kaitu-io/k2app/api && go test ./...`
Expected: All tests pass

- [ ] **Step 2: Run webapp type check**

Run: `cd /Users/david/projects/kaitu-io/k2app/webapp && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Run webapp tests**

Run: `cd /Users/david/projects/kaitu-io/k2app/webapp && yarn test`
Expected: All tests pass

- [ ] **Step 4: Run web build**

Run: `cd /Users/david/projects/kaitu-io/k2app/web && yarn build`
Expected: BUILD SUCCESS

- [ ] **Step 5: Run MCP build**

Run: `cd /Users/david/projects/kaitu-io/k2app/tools/kaitu-center && npm run build`
Expected: BUILD SUCCESS

---

## Deferred (Not in This PR)

- **Image upload in replies**: Requires adding `aws-sdk-go-v2/service/s3` dependency, S3 bucket config, presigned URL or server-upload endpoint. Separate PR.
- **Other locale translations**: ja, zh-TW, zh-HK, en-AU, en-GB — add in follow-up.
- **Empty-state inline ticket form**: When user has no tickets, Feedback page should render SubmitTicket form inline. Current MVP navigates to separate route.
- **Anonymous submit success guidance**: Show "log in with this email to view progress" after anonymous ticket submission.
