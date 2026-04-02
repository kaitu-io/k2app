package center

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

const TaskTypeTicketNotify = "ticket:notify"

// TicketNotifyPayload 工单通知任务载荷
type TicketNotifyPayload struct {
	TicketID uint64 `json:"ticketId"`
}

// handleTicketNotify 处理工单通知：聚合未通知的管理员回复，发送邮件
func handleTicketNotify(ctx context.Context, payload []byte) error {
	var p TicketNotifyPayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload failed: %w", err)
	}

	// Query pending (un-notified) admin replies
	var replies []TicketReply
	if err := db.Get().Where("ticket_id = ? AND sender_type = ? AND notified_at IS NULL",
		p.TicketID, "admin").Order("created_at ASC").Find(&replies).Error; err != nil {
		return fmt.Errorf("failed to query pending replies: %w", err)
	}

	if len(replies) == 0 {
		log.Debugf(ctx, "[TICKET_NOTIFY] No pending replies for ticket %d, skipping", p.TicketID)
		return nil
	}

	// Get ticket
	var ticket FeedbackTicket
	if err := db.Get().Where("id = ?", p.TicketID).First(&ticket).Error; err != nil {
		return fmt.Errorf("failed to get ticket: %w", err)
	}

	// Get user email
	var userEmail string
	if ticket.UserID != nil {
		email, err := getUserEmail(ctx, *ticket.UserID)
		if err != nil {
			return fmt.Errorf("failed to get user email: %w", err)
		}
		userEmail = email
	} else {
		userEmail = ticket.Email
	}

	if userEmail == "" {
		log.Warnf(ctx, "[TICKET_NOTIFY] No email for ticket %d, skipping", p.TicketID)
		return nil
	}

	// Build aggregated email body
	var replyTexts []string
	for _, r := range replies {
		replyTexts = append(replyTexts, fmt.Sprintf("[%s] %s:\n%s",
			r.CreatedAt.Format("2006-01-02 15:04"), r.SenderName, r.Content))
	}

	subject := fmt.Sprintf("[Kaitu] 您的工单有新回复 (#%d)", p.TicketID)
	body := fmt.Sprintf(`您好，

您的工单 (#%d) 收到了新的回复：

---
%s
---

请登录 Kaitu 客户端查看完整对话。
`, p.TicketID, strings.Join(replyTexts, "\n\n"))

	if err := sendSystemEmail(ctx, userEmail, subject, body); err != nil {
		return fmt.Errorf("failed to send notification email: %w", err)
	}

	// Mark all replies as notified
	now := time.Now()
	replyIDs := make([]uint64, len(replies))
	for i, r := range replies {
		replyIDs[i] = r.ID
	}
	if err := db.Get().Model(&TicketReply{}).Where("id IN ?", replyIDs).
		Update("notified_at", now).Error; err != nil {
		log.Warnf(ctx, "[TICKET_NOTIFY] Failed to update notified_at for ticket %d: %v", p.TicketID, err)
	}

	log.Infof(ctx, "[TICKET_NOTIFY] Notification sent for ticket %d: %d replies, to %s",
		p.TicketID, len(replies), hideEmail(userEmail))
	return nil
}
