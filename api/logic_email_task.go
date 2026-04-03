package center

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"

	"github.com/wordgate/qtoolkit/ai"
	"github.com/wordgate/qtoolkit/aws/ses"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// getUserEmailByUser 从用户对象获取邮箱地址
func getUserEmailByUser(ctx context.Context, user *User) (string, error) {
	for _, identity := range user.LoginIdentifies {
		if identity.Type == "email" && identity.EncryptedValue != "" {
			return secretDecryptString(ctx, identity.EncryptedValue)
		}
	}
	return "", fmt.Errorf("user %d has no email address", user.ID)
}

// getUserLanguagePreference 获取用户语言偏好
func getUserLanguagePreference(user *User) string {
	return user.GetLanguagePreference()
}

// sendEmail 发送邮件 (支持 SMTP 和 AWS SES)
// dev 模式下仅打印日志不发送
func sendEmail(ctx context.Context, to, subject, body string) error {
	if isMailDevMode() {
		log.Infof(ctx, "[DEV-MAIL-EDM] TO: %s | SUBJECT: %s\n--- BODY ---\n%s\n--- END ---",
			to, subject, body)
		return nil
	}

	cfg := getEDMConfig(ctx)

	// 根据配置选择发送方式
	if cfg.Provider == "ses" {
		return sendEmailWithSES(ctx, to, subject, body)
	}

	// 默认使用 SMTP
	return sendEmailWithSMTP(ctx, to, subject, body)
}

// sendEmailWithSES 使用 AWS SES 发送纯文本邮件
func sendEmailWithSES(ctx context.Context, to, subject, body string) error {
	log.Debugf(ctx, "sending plain text email via AWS SES to %s", to)

	cfg := getEDMConfig(ctx)

	// 构建发件人地址（带名称）
	fromAddress := cfg.FromEmail
	if cfg.FromName != "" {
		fromAddress = fmt.Sprintf("%s <%s>", cfg.FromName, cfg.FromEmail)
	}

	// 使用 qtoolkit SES 发送纯文本邮件
	resp, err := ses.SendEmail(&ses.EmailRequest{
		From:     fromAddress,
		To:       []string{to},
		Subject:  subject,
		BodyText: body, // 使用纯文本而非 HTML
	})

	if err != nil {
		log.Errorf(ctx, "failed to send email via SES to %s: %v", to, err)
		return fmt.Errorf("SES send failed: %w", err)
	}

	log.Infof(ctx, "plain text email sent via SES to %s, MessageID: %s", to, resp.MessageID)
	return nil
}

// sendEmailWithSMTP 使用 SMTP 发送邮件
func sendEmailWithSMTP(ctx context.Context, to, subject, body string) error {
	log.Debugf(ctx, "sending email via SMTP to %s", to)

	cfg := getEDMConfig(ctx)

	if cfg.SMTPUsername == "" || cfg.SMTPPassword == "" {
		return fmt.Errorf("SMTP credentials not configured")
	}

	// 构建发件人地址
	from := cfg.FromEmail
	if cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", cfg.FromName, cfg.FromEmail)
	}

	// 构建邮件头和正文
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		from, to, subject, body))

	// SMTP 服务器地址
	host := cfg.SMTPHost
	addr := net.JoinHostPort(host, cfg.SMTPPort)

	// 创建 TLS 配置
	tlsConfig := &tls.Config{
		ServerName: host,
	}

	// 建立 TLS 连接（465 端口使用 SSL/TLS）
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		log.Errorf(ctx, "failed to connect to SMTP server %s: %v", addr, err)
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer conn.Close()

	// 创建 SMTP 客户端
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		log.Errorf(ctx, "failed to create SMTP client: %v", err)
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Quit()

	// 认证
	auth := smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, host)
	if err = client.Auth(auth); err != nil {
		log.Errorf(ctx, "SMTP authentication failed: %v", err)
		return fmt.Errorf("SMTP authentication failed: %w", err)
	}

	// 设置发件人
	if err = client.Mail(cfg.FromEmail); err != nil {
		log.Errorf(ctx, "failed to set MAIL FROM: %v", err)
		return fmt.Errorf("failed to set sender: %w", err)
	}

	// 设置收件人
	if err = client.Rcpt(to); err != nil {
		log.Errorf(ctx, "failed to set RCPT TO: %v", err)
		return fmt.Errorf("failed to set recipient: %w", err)
	}

	// 发送邮件正文
	w, err := client.Data()
	if err != nil {
		log.Errorf(ctx, "failed to get DATA writer: %v", err)
		return fmt.Errorf("failed to get data writer: %w", err)
	}

	_, err = w.Write(msg)
	if err != nil {
		log.Errorf(ctx, "failed to write email body: %v", err)
		return fmt.Errorf("failed to write email body: %w", err)
	}

	err = w.Close()
	if err != nil {
		log.Errorf(ctx, "failed to close DATA writer: %v", err)
		return fmt.Errorf("failed to close data writer: %w", err)
	}

	log.Infof(ctx, "email sent via SMTP to %s", to)
	return nil
}

// getTemplateForLanguage 获取指定语言的模板（支持Lazy翻译）
func getTemplateForLanguage(ctx context.Context, templateID uint64, targetLang string) (*EmailMarketingTemplate, error) {
	var template EmailMarketingTemplate

	// 使用优化的查询：id = templateID 或 (origin_id = templateID 且 language = targetLang)
	// 按 id 降序排列，这样优先匹配到翻译版本（id更大），如果没有则使用原始模板（id更小）
	err := db.Get().Where("(id = ? OR (origin_id = ? AND language = ?)) AND is_active = ?",
		templateID, templateID, targetLang, true).
		Order("id DESC").
		First(&template).Error

	if err != nil {
		log.Errorf(ctx, "template not found for id %d, language %s: %v", templateID, targetLang, err)
		return nil, fmt.Errorf("template not found: %v", err)
	}

	// 如果找到的模板语言已经匹配，使用AI润色
	if template.Language == targetLang {
		log.Debugf(ctx, "found template %d in target language %s, polishing content", template.ID, targetLang)

		polishedSubject, polishedContent, err := processEmailWithAI(ctx, template.Subject, template.Content, targetLang)
		if err == nil {
			template.Subject = polishedSubject
			template.Content = polishedContent
		}

		return &template, nil
	}

	// 如果找到的是原始模板但语言不匹配，需要翻译+润色
	log.Infof(ctx, "creating translation for template %d from %s to %s", template.ID, template.Language, targetLang)

	translatedSubject, translatedContent, err := processEmailWithAI(ctx, template.Subject, template.Content, targetLang)
	if err != nil {
		log.Warnf(ctx, "failed to process email with AI, using original template: %v", err)
		return &template, nil // Fallback: 使用原始模板
	}

	// 创建新的翻译模板记录
	sourceTemplateID := templateID
	newTemplate := EmailMarketingTemplate{
		Name:        template.Name + " (" + targetLang + ")",
		Language:    targetLang,
		Subject:     translatedSubject,
		Content:     translatedContent,
		IsActive:    BoolPtr(true),
		Description: "Auto-translated from " + template.Language,
		OriginID:    &sourceTemplateID, // 关联到源模板
	}

	if err := db.Get().Create(&newTemplate).Error; err != nil {
		log.Errorf(ctx, "failed to save translated template: %v", err)
		return nil, fmt.Errorf("failed to save translation: %w", err)
	}

	log.Infof(ctx, "successfully created translation %d for template %d, language %s", newTemplate.ID, sourceTemplateID, targetLang)
	return &newTemplate, nil
}

// processEmailWithAI 使用AI处理邮件（翻译+润色）
// 同时处理主题和正文，无论是翻译还是润色都使用同一接口
func processEmailWithAI(ctx context.Context, subject, content, targetLang string) (string, string, error) {
	log.Infof(ctx, "processing email with AI, target language: %s", targetLang)

	var processedSubject, processedContent string
	var err error

	// 处理主题
	if strings.TrimSpace(subject) != "" {
		processedSubject, err = ai.TranslateEmailSubject(ctx, subject, targetLang,
			ai.TranslateWithStyle("formal"),
		)
		if err != nil {
			log.Errorf(ctx, "AI process subject failed: %v", err)
			return "", "", fmt.Errorf("AI process subject failed: %w", err)
		}
	} else {
		processedSubject = subject
	}

	// 处理正文
	if strings.TrimSpace(content) != "" {
		processedContent, err = ai.TranslateEmailBody(ctx, content, targetLang,
			ai.TranslateWithStyle("formal"),
		)
		if err != nil {
			log.Errorf(ctx, "AI process content failed: %v", err)
			return "", "", fmt.Errorf("AI process content failed: %w", err)
		}
	} else {
		processedContent = content
	}

	log.Infof(ctx, "successfully processed email with AI")
	return processedSubject, processedContent, nil
}
