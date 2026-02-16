package center

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/mail"
	"github.com/wordgate/qtoolkit/slack"
)

// configSupportEmail 获取工单支持邮箱
func configSupportEmail() string {
	email := viper.GetString("support.email")
	if email == "" {
		email = "support@kaitu.me" // 默认值
	}
	return email
}

// api_create_ticket 创建工单
func api_create_ticket(c *gin.Context) {
	ctx := c.Request.Context()
	userID := ReqUserID(c)
	if userID == 0 {
		log.Warnf(ctx, "api_create_ticket: user not authenticated")
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	var req CreateTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(ctx, "api_create_ticket: invalid request: %v", err)
		Error(c, ErrorInvalidArgument, "invalid request: "+err.Error())
		return
	}

	log.Infof(ctx, "api_create_ticket: user %d creating ticket: %s", userID, req.Subject)

	// 获取用户邮箱
	userEmail, err := getUserEmail(ctx, userID)
	if err != nil {
		log.Errorf(ctx, "api_create_ticket: failed to get user email: %v", err)
		Error(c, ErrorSystemError, "failed to get user email")
		return
	}

	// Note: GitHub Issues are NOT created automatically from tickets.
	// Engineers will manually create issues after reviewing the ticket content
	// to filter out any private user information.

	// Send ticket email
	if err := sendTicketEmail(ctx, userEmail, req.Subject, req.Content, req.FeedbackID); err != nil {
		log.Errorf(ctx, "api_create_ticket: failed to send ticket email: %v", err)
		Error(c, ErrorSystemError, "failed to send ticket email")
		return
	}

	// Send Slack notification to #customer channel (async, don't block on failure)
	SafeGoWithContext(ctx, func(ctx context.Context) {
		if err := sendTicketSlackNotification(ctx, userEmail, req.Subject, req.Content, req.FeedbackID); err != nil {
			log.Warnf(ctx, "api_create_ticket: failed to send slack notification: %v", err)
		}
	})

	log.Infof(ctx, "api_create_ticket: ticket created successfully for user %d", userID)
	SuccessEmpty(c)
}

// getUserEmail 获取用户邮箱
func getUserEmail(ctx context.Context, userID uint64) (string, error) {
	identify, err := GetEmailIdentifyByUserID(ctx, int64(userID))
	if err != nil {
		return "", fmt.Errorf("failed to get user identify: %w", err)
	}
	if identify == nil || identify.EncryptedValue == "" {
		return "", fmt.Errorf("user has no email address")
	}

	decEmail, err := secretDecryptString(ctx, identify.EncryptedValue)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt email: %w", err)
	}
	return decEmail, nil
}

// sendTicketEmail sends ticket email to support
// Sends to support email, sets ReplyTo to user email, CC to user
func sendTicketEmail(ctx context.Context, userEmail, subject, content, feedbackID string) error {
	supportEmail := configSupportEmail()

	// Build email subject (add prefix to identify ticket)
	emailSubject := fmt.Sprintf("[Ticket] %s", subject)

	// Build email body
	var emailBody string
	if feedbackID != "" {
		emailBody = fmt.Sprintf(`New support ticket from: %s

Subject: %s
Feedback ID: %s (logs uploaded with this ID)

---
%s
---

Please reply to this email to respond to the user.
`, userEmail, subject, feedbackID, content)
	} else {
		emailBody = fmt.Sprintf(`New support ticket from: %s

Subject: %s

---
%s
---

Please reply to this email to respond to the user.
`, userEmail, subject, content)
	}

	log.Infof(ctx, "sendTicketEmail: sending ticket to %s, reply-to: %s, cc: %s",
		hideEmail(supportEmail), hideEmail(userEmail), hideEmail(userEmail))

	// Use qtoolkit/mail to send email
	if err := mail.Send(&mail.Message{
		To:      supportEmail,
		Subject: emailSubject,
		Body:    emailBody,
		ReplyTo: userEmail,
		Cc:      []string{userEmail},
	}); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	log.Infof(ctx, "sendTicketEmail: ticket email sent successfully")
	return nil
}

// sendTicketSlackNotification sends a notification to #customer Slack channel
func sendTicketSlackNotification(ctx context.Context, userEmail, subject, content, feedbackID string) error {
	// Truncate content for Slack preview
	contentPreview := content
	if len(contentPreview) > 200 {
		contentPreview = contentPreview[:200] + "..."
	}

	// Build Slack message
	var message string
	if feedbackID != "" {
		message = fmt.Sprintf(`:ticket: *New Support Ticket*

*Subject:* %s
*User:* %s
*Feedback ID:* `+"`%s`"+`

> %s`, subject, userEmail, feedbackID, contentPreview)
	} else {
		message = fmt.Sprintf(`:ticket: *New Support Ticket*

*Subject:* %s
*User:* %s

> %s`, subject, userEmail, contentPreview)
	}

	// Send to #customer channel
	if err := slack.Send("customer", message); err != nil {
		return fmt.Errorf("failed to send slack notification: %w", err)
	}

	log.Infof(ctx, "sendTicketSlackNotification: notification sent to #customer")
	return nil
}
