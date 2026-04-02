package center

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_user_list_tickets 用户工单列表
func api_user_list_tickets(c *gin.Context) {
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	pagination := PaginationFromRequest(c)

	query := db.Get().Model(&FeedbackTicket{}).Where("user_id = ?", userID)

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "api_user_list_tickets: failed to count tickets: %v", err)
		Error(c, ErrorSystemError, "failed to count tickets")
		return
	}

	var tickets []FeedbackTicket
	if err := query.Offset(pagination.Offset()).Limit(pagination.PageSize).
		Order("COALESCE(last_reply_at, created_at) DESC").
		Find(&tickets).Error; err != nil {
		log.Errorf(c, "api_user_list_tickets: failed to query tickets: %v", err)
		Error(c, ErrorSystemError, "failed to query tickets")
		return
	}

	items := make([]UserTicketListItem, len(tickets))
	for i, t := range tickets {
		// Truncate content to 100 runes
		content := t.Content
		runes := []rune(content)
		if len(runes) > 100 {
			content = string(runes[:100]) + "..."
		}

		var lastReplyAt *int64
		if t.LastReplyAt != nil {
			ts := t.LastReplyAt.Unix()
			lastReplyAt = &ts
		}

		items[i] = UserTicketListItem{
			ID:          t.ID,
			FeedbackID:  t.FeedbackID,
			Content:     content,
			Status:      t.Status,
			UserUnread:  t.UserUnread,
			LastReplyAt: lastReplyAt,
			LastReplyBy: t.LastReplyBy,
			CreatedAt:   t.CreatedAt.Unix(),
		}
	}

	List(c, items, pagination)
}

// api_user_ticket_detail 用户工单详情（含回复）
func api_user_ticket_detail(c *gin.Context) {
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid ticket id")
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

	// Get replies
	var replies []TicketReply
	if err := db.Get().Where("ticket_id = ?", id).Order("created_at ASC").Find(&replies).Error; err != nil {
		log.Errorf(c, "api_user_ticket_detail: failed to query replies: %v", err)
		Error(c, ErrorSystemError, "failed to query replies")
		return
	}

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

	Success(c, &resp)
}

// api_user_ticket_reply 用户回复工单
func api_user_ticket_reply(c *gin.Context) {
	ctx := c.Request.Context()
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid ticket id")
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
		Error(c, ErrorInvalidOperation, "ticket is closed")
		return
	}

	// Get user display name (part before @)
	senderName := "user"
	email, err := getUserEmail(ctx, userID)
	if err == nil && email != "" {
		parts := strings.SplitN(email, "@", 2)
		if len(parts) > 0 && parts[0] != "" {
			senderName = parts[0]
		}
	}

	now := time.Now()
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

	// Update ticket
	updates := map[string]any{
		"last_reply_at": now,
		"last_reply_by": "user",
	}
	// Reopen if resolved
	if ticket.Status == "resolved" {
		updates["status"] = "open"
	}
	db.Get().Model(&FeedbackTicket{}).Where("id = ?", id).Updates(updates)

	log.Infof(ctx, "api_user_ticket_reply: user %d replied to ticket %d", userID, id)

	resp := TicketReplyResponse{
		ID:         reply.ID,
		SenderType: reply.SenderType,
		SenderName: reply.SenderName,
		Content:    reply.Content,
		CreatedAt:  reply.CreatedAt.Unix(),
	}
	Success(c, &resp)
}

// api_user_tickets_unread 用户未读回复总数
func api_user_tickets_unread(c *gin.Context) {
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	var count int64
	db.Get().Model(&FeedbackTicket{}).Where("user_id = ?", userID).
		Select("COALESCE(SUM(user_unread), 0)").Scan(&count)

	resp := UnreadCountResponse{Unread: int(count)}
	Success(c, &resp)
}
