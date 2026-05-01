package center

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wordgate/qtoolkit/ai"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/mail"
)

// edmSender sends EDM email via qtoolkit/mail's "edm" viper prefix.
// Dialer / SES client is lazy-loaded on first Send.
var edmSender = mail.Config("edm")

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

// sendEmail 通过 qtoolkit/mail 的 "edm" prefix 发送邮件（dev 模式仅打印不发送）。
func sendEmail(ctx context.Context, to, subject, body string) error {
	if isMailDevMode() {
		log.Infof(ctx, "[DEV-MAIL-EDM] TO: %s | SUBJECT: %s\n--- BODY ---\n%s\n--- END ---",
			to, subject, body)
		return nil
	}

	if err := edmSender.Send(&mail.Message{
		To:      to,
		Subject: subject,
		Body:    body,
	}); err != nil {
		log.Errorf(ctx, "failed to send EDM email to %s: %v", to, err)
		return fmt.Errorf("EDM send failed: %w", err)
	}

	log.Infof(ctx, "EDM email sent to %s", to)
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

	// 30s 上限。调用方 (getTemplateForLanguage) 在 AI 出错时退回原始模板，业务不中断。
	// 没这个 timeout，AI 上游慢/挂会卡死 worker，Asynq 把整批邮件无限重试。
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

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
