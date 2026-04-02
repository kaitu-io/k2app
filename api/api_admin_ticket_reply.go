package center

import (
	"context"
	"fmt"
	"strconv"
	"time"

	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// api_admin_reply_ticket 管理员回复工单
func api_admin_reply_ticket(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid ticket id")
		return
	}

	var req AdminCreateTicketReplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request: "+err.Error())
		return
	}

	var ticket FeedbackTicket
	if err := db.Get().Where("id = ?", id).First(&ticket).Error; err != nil {
		Error(c, ErrorNotFound, "ticket not found")
		return
	}

	senderName := req.SenderName
	if senderName == "" {
		senderName = "客服"
	}

	adminID := ReqUserID(c)
	reply := TicketReply{
		TicketID:   id,
		SenderType: "admin",
		SenderID:   &adminID,
		SenderName: senderName,
		Content:    req.Content,
	}

	if err := db.Get().Create(&reply).Error; err != nil {
		log.Errorf(ctx, "api_admin_reply_ticket: failed to create reply: %v", err)
		Error(c, ErrorSystemError, "failed to create reply")
		return
	}

	// Update ticket: last_reply_at, last_reply_by
	now := time.Now()
	updates := map[string]any{
		"last_reply_at": now,
		"last_reply_by": "admin",
	}
	// Only increment unread and notify if ticket is not closed
	if ticket.Status != "closed" {
		updates["user_unread"] = gorm.Expr("user_unread + 1")
	}
	db.Get().Model(&FeedbackTicket{}).Where("id = ?", id).Updates(updates)

	// Enqueue notification (delayed + deduplicated) — skip for closed tickets
	if ticket.Status != "closed" {
		enqueueTicketNotification(ctx, id)
	}

	log.Infof(ctx, "api_admin_reply_ticket: admin replied to ticket %d", id)
	WriteAuditLog(c, "ticket_reply", "ticket", fmt.Sprintf("%d", id), nil)

	resp := TicketReplyResponse{
		ID:         reply.ID,
		SenderType: reply.SenderType,
		SenderName: reply.SenderName,
		Content:    reply.Content,
		CreatedAt:  reply.CreatedAt.Unix(),
	}
	Success(c, &resp)
}

// api_admin_list_ticket_replies 管理员查看工单回复列表
func api_admin_list_ticket_replies(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid ticket id")
		return
	}

	var replies []TicketReply
	if err := db.Get().Where("ticket_id = ?", id).Order("created_at ASC").Find(&replies).Error; err != nil {
		log.Errorf(c, "api_admin_list_ticket_replies: failed to query replies: %v", err)
		Error(c, ErrorSystemError, "failed to query replies")
		return
	}

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

// enqueueTicketNotification 入队工单通知任务（延迟 + 去重）
func enqueueTicketNotification(ctx context.Context, ticketID uint64) {
	payload := TicketNotifyPayload{TicketID: ticketID}
	_, err := asynq.Enqueue(TaskTypeTicketNotify, payload,
		hibikenAsynq.ProcessIn(5*time.Minute),
		hibikenAsynq.Unique(10*time.Minute),
	)
	if err != nil && err != hibikenAsynq.ErrDuplicateTask {
		log.Warnf(ctx, "enqueueTicketNotification: failed to enqueue for ticket %d: %v", ticketID, err)
	}
}
