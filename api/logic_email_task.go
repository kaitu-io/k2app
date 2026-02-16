package center

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"text/template"
	"time"

	"github.com/wordgate/qtoolkit/ai"
	"github.com/wordgate/qtoolkit/aws/ses"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// getTargetUsersForEmailTask 根据筛选条件获取目标用户（优化版：所有筛选条件下推到SQL）
// 注意：ctx 参数用于日志追踪，确保 execution ID 能够贯穿整个执行链
func getTargetUsersForEmailTask(ctx context.Context, filters UserFilter) ([]User, error) {
	// 打印接收到的筛选条件
	log.Infof(ctx, "[EDM] getTargetUsersForEmailTask: UserStatus=%s, ActivatedDate=%+v, ExpireDays=%s, SpecificUsers=%d, RetailerLevels=%v",
		filters.UserStatus, filters.ActivatedDate, filters.ExpireDays, len(filters.SpecificUsers), filters.RetailerLevels)

	// 基础查询，预加载关联数据
	query := db.Get().Model(&User{}).Preload("LoginIdentifies").Preload("Devices")

	// ==================== 特定用户筛选（优先级最高） ====================
	// 如果指定了特定用户，直接返回这些用户，忽略其他筛选条件
	if len(filters.SpecificUsers) > 0 {
		var users []User
		if err := query.Where("uuid IN ?", filters.SpecificUsers).Find(&users).Error; err != nil {
			return nil, fmt.Errorf("failed to query specific users: %w", err)
		}
		log.Infof(ctx, "[EDM] Specific users query returned %d users", len(users))
		return users, nil
	}

	// ==================== 用户状态筛选（SQL层） ====================
	// 使用复合索引 idx_user_status (is_activated, is_first_order_done)
	// 注意：GORM 在使用结构体查询时会忽略零值字段（如 false），必须使用字符串查询
	if filters.UserStatus != "" {
		switch filters.UserStatus {
		case "not_activated":
			// 注册但未激活 (使用字符串查询避免 GORM 零值问题)
			query = query.Where("is_activated = ?", false)
		case "activated_no_order":
			// 已激活但未完成首单（两个字段都需要显式指定）
			query = query.Where("is_activated = ?", true).Where("is_first_order_done = ?", false)
		case "first_order_done":
			// 已完成首单（不管是否过期）
			query = query.Where("is_first_order_done = ?", true)
		case "first_order_done_but_expired":
			// 已完成首单但已过期
			now := time.Now().Unix()
			query = query.Where("is_first_order_done = ?", true).Where("expired_at < ?", now)
		default:
			log.Warnf(ctx, "[EDM] Unknown user status filter: %s", filters.UserStatus)
		}
	}

	// ==================== 激活日期筛选（SQL层） ====================
	if filters.ActivatedDate.Start != "" {
		if startTime, err := time.Parse("2006-01-02", filters.ActivatedDate.Start); err == nil {
			query = query.Where("activated_at >= ?", startTime)
		}
	}
	if filters.ActivatedDate.End != "" {
		if endTime, err := time.Parse("2006-01-02", filters.ActivatedDate.End); err == nil {
			// 包含结束日期当天的所有时间
			query = query.Where("activated_at < ?", endTime.Add(24*time.Hour))
		}
	}

	// ==================== 过期天数筛选（SQL层） ====================
	if filters.ExpireDays != "" {
		now := time.Now().UTC()
		// 将当前时间归零到当天00:00:00
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

		// 根据不同的过期天数条件，计算目标日期（精确到天）
		var targetDate time.Time
		switch filters.ExpireDays {
		case "expire_in_30":
			targetDate = todayStart.AddDate(0, 0, 30)
		case "expire_in_14":
			targetDate = todayStart.AddDate(0, 0, 14)
		case "expire_in_7":
			targetDate = todayStart.AddDate(0, 0, 7)
		case "expire_in_3":
			targetDate = todayStart.AddDate(0, 0, 3)
		case "expire_in_1":
			targetDate = todayStart.AddDate(0, 0, 1)
		case "expired_1":
			targetDate = todayStart.AddDate(0, 0, -1)
		case "expired_3":
			targetDate = todayStart.AddDate(0, 0, -3)
		case "expired_7":
			targetDate = todayStart.AddDate(0, 0, -7)
		case "expired_14":
			targetDate = todayStart.AddDate(0, 0, -14)
		case "expired_30":
			targetDate = todayStart.AddDate(0, 0, -30)
		case "expired":
			// 已过期超过30天：过期时间 < 今天-30天
			targetDate = todayStart.AddDate(0, 0, -30)
			query = query.Where("expired_at < ?", targetDate.Unix())
		default:
			log.Warnf(ctx, "[EDM] Unknown expire days filter: %s", filters.ExpireDays)
		}

		// 对于非 "expired" 的情况，匹配精确的那一天
		// expired_at 的日期部分必须等于 targetDate
		if filters.ExpireDays != "expired" {
			dayStart := targetDate.Unix()
			dayEnd := targetDate.AddDate(0, 0, 1).Unix() // 下一天的00:00:00
			query = query.Where("expired_at >= ? AND expired_at < ?", dayStart, dayEnd)
		}
	}

	// ==================== 分销商等级筛选（SQL层） ====================
	// 通过 JOIN retailer_configs 表并筛选等级
	if len(filters.RetailerLevels) > 0 {
		query = query.Joins("JOIN retailer_configs ON retailer_configs.user_id = users.id").
			Where("retailer_configs.level IN ?", filters.RetailerLevels)
		log.Infof(ctx, "[EDM] Filtering by retailer levels: %v", filters.RetailerLevels)
	}

	// ==================== 执行查询 ====================
	var users []User
	if err := query.Find(&users).Error; err != nil {
		return nil, fmt.Errorf("failed to query users with filters: %w", err)
	}

	log.Infof(ctx, "[EDM] SQL query returned %d users (all filters applied at database level)", len(users))
	return users, nil
}

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

// renderEmailContent 渲染纯文本邮件内容（替换变量）
func renderEmailContent(ctx context.Context, user User, emailTemplate EmailMarketingTemplate) (string, string, error) {
	// 构建模板数据
	data := buildEmailTemplateData(ctx, user)

	// 渲染主题
	subjectTemplate, err := template.New("subject").Parse(emailTemplate.Subject)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse subject template: %v", err)
	}

	var subjectBuf strings.Builder
	if err := subjectTemplate.Execute(&subjectBuf, data); err != nil {
		return "", "", fmt.Errorf("failed to execute subject template: %v", err)
	}

	// 渲染纯文本内容
	contentTemplate, err := template.New("content").Parse(emailTemplate.Content)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse content template: %v", err)
	}

	var contentBuf strings.Builder
	if err := contentTemplate.Execute(&contentBuf, data); err != nil {
		return "", "", fmt.Errorf("failed to execute content template: %v", err)
	}

	return subjectBuf.String(), contentBuf.String(), nil
}

// buildEmailTemplateData 构建邮件模板数据（精简版，只包含关键参数）
// 可用参数见 api_admin_edm.go 中的注释
func buildEmailTemplateData(_ context.Context, user User) map[string]interface{} {
	now := time.Now()
	expiredTime := time.Unix(user.ExpiredAt, 0)

	data := map[string]interface{}{
		// 关键参数
		"ExpiredAt":      expiredTime.Format("2006-01-02"),
		"DeviceCount":    len(user.Devices),
		"MaxDevices":     user.MaxDevice,
		"IsPro":          user.IsPro(),
		"IsExpiringSoon": false,
	}

	// 计算剩余天数
	if user.ExpiredAt > now.Unix() {
		remainingDays := int((user.ExpiredAt - now.Unix()) / (24 * 3600))
		data["RemainingDays"] = remainingDays
		data["IsExpiringSoon"] = remainingDays <= 7 && remainingDays > 0
	} else {
		data["RemainingDays"] = 0
	}

	// 获取用户邮箱
	for _, identity := range user.LoginIdentifies {
		if identity.Type == "email" {
			data["UserEmail"] = identity.IndexID
			break
		}
	}

	return data
}

// sendEmail 发送邮件 (支持 SMTP 和 AWS SES)
func sendEmail(ctx context.Context, to, subject, body string) error {
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
