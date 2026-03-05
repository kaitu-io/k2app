package center

import (
	"context"
	"fmt"

	"strings"

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

	log.Infof(ctx, "api_create_ticket: user %d creating ticket, os=%s version=%s channel=%s",
		userID, req.OS, req.AppVersion, req.Channel)

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
	if err := sendTicketEmail(ctx, userEmail, req); err != nil {
		log.Errorf(ctx, "api_create_ticket: failed to send ticket email: %v", err)
		Error(c, ErrorSystemError, "failed to send ticket email")
		return
	}

	// Send Slack notification to #customer channel (async, don't block on failure)
	SafeGoWithContext(ctx, func(ctx context.Context) {
		if err := sendTicketSlackNotification(ctx, userEmail, req); err != nil {
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

// ticketSubject 生成邮件主题：优先使用老客户端传的 Subject，否则从 content 截取前 50 字符
func ticketSubject(req CreateTicketRequest) string {
	if req.Subject != "" {
		return req.Subject
	}
	runes := []rune(req.Content)
	// 取第一行或前 50 字符
	for i, r := range runes {
		if r == '\n' {
			runes = runes[:i]
			break
		}
	}
	if len(runes) > 50 {
		return string(runes[:50]) + "..."
	}
	return string(runes)
}

// formatSystemInfo 格式化系统信息
func formatSystemInfo(req CreateTicketRequest) string {
	return fmt.Sprintf("OS: %s | Version: %s | Channel: %s | Language: %s | VPN: %s | Time: %s",
		req.OS, req.AppVersion, req.Channel, req.Language, req.VPNState, req.SubmitTime)
}

// sendTicketEmail sends ticket email to support
// Sends to support email, sets ReplyTo to user email, CC to user
func sendTicketEmail(ctx context.Context, userEmail string, req CreateTicketRequest) error {
	supportEmail := configSupportEmail()

	emailSubject := fmt.Sprintf("[Ticket] %s", ticketSubject(req))
	sysInfo := formatSystemInfo(req)

	var emailBody string
	if req.FeedbackID != "" {
		emailBody = fmt.Sprintf(`New support ticket from: %s

System: %s
Feedback ID: %s (logs uploaded with this ID)

---
%s
---

Please reply to this email to respond to the user.
`, userEmail, sysInfo, req.FeedbackID, req.Content)
	} else {
		emailBody = fmt.Sprintf(`New support ticket from: %s

System: %s

---
%s
---

Please reply to this email to respond to the user.
`, userEmail, sysInfo, req.Content)
	}

	log.Infof(ctx, "sendTicketEmail: sending ticket to %s, reply-to: %s, cc: %s",
		hideEmail(supportEmail), hideEmail(userEmail), hideEmail(userEmail))

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

// api_feedback_notify 接收客户端日志上传后的通知，发送 Slack 提醒
func api_feedback_notify(c *gin.Context) {
	ctx := c.Request.Context()
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	var req struct {
		Reason     string `json:"reason"`
		Platform   string `json:"platform"`
		Version    string `json:"version"`
		FeedbackID string `json:"feedbackId"`
		S3Keys     []struct {
			Name  string `json:"name"`
			S3Key string `json:"s3Key"`
		} `json:"s3Keys"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request")
		return
	}
	if len(req.S3Keys) == 0 {
		Error(c, ErrorInvalidArgument, "no s3 keys")
		return
	}

	userEmail, err := getUserEmail(ctx, userID)
	if err != nil {
		log.Warnf(ctx, "api_feedback_notify: failed to get user email: %v", err)
		userEmail = fmt.Sprintf("user#%d", userID)
	}

	SafeGoWithContext(ctx, func(ctx context.Context) {
		sendFeedbackSlackNotification(ctx, userEmail, req.Reason, req.Platform, req.Version, req.FeedbackID, req.S3Keys)
	})

	SuccessEmpty(c)
}

const s3LogBucketURL = "https://kaitu-service-logs.s3.ap-northeast-1.amazonaws.com"

func sendFeedbackSlackNotification(ctx context.Context, email, reason, platform, version, feedbackID string, s3Keys []struct {
	Name  string `json:"name"`
	S3Key string `json:"s3Key"`
}) {
	var links []string
	for _, k := range s3Keys {
		links = append(links, fmt.Sprintf("<%s/%s|%s>", s3LogBucketURL, k.S3Key, k.Name))
	}

	message := fmt.Sprintf(`:warning: *Service Log Report*

*User:* %s
*Platform:* %s | *Version:* %s
*Reason:* %s
*Feedback ID:* `+"`%s`"+`
*Log Files:* %s`, email, platform, version, reason, feedbackID, strings.Join(links, " | "))

	if err := slack.Send("customer", message); err != nil {
		log.Warnf(ctx, "sendFeedbackSlackNotification: failed: %v", err)
	} else {
		log.Infof(ctx, "sendFeedbackSlackNotification: sent to #customer")
	}
}

// sendTicketSlackNotification sends a notification to #customer Slack channel
func sendTicketSlackNotification(ctx context.Context, userEmail string, req CreateTicketRequest) error {
	sysInfo := formatSystemInfo(req)

	var message string
	if req.FeedbackID != "" {
		message = fmt.Sprintf(`:ticket: *New Support Ticket*

*User:* %s
*System:* %s
*Feedback ID:* `+"`%s`"+`

> %s`, userEmail, sysInfo, req.FeedbackID, req.Content)
	} else {
		message = fmt.Sprintf(`:ticket: *New Support Ticket*

*User:* %s
*System:* %s

> %s`, userEmail, sysInfo, req.Content)
	}

	if err := slack.Send("customer", message); err != nil {
		return fmt.Errorf("failed to send slack notification: %w", err)
	}

	log.Infof(ctx, "sendTicketSlackNotification: notification sent to #customer")
	return nil
}
