package center

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/ai"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/mail"
)

// edmSender sends EDM email via qtoolkit/mail's "edm" viper prefix (kaitu brand).
// Dialer / SES client is lazy-loaded on first Send.
var edmSender = mail.Config("edm")

// edmSenderOverleap sends EDM email under the Overleap brand identity, bound to
// the "edm_overleap" viper prefix (see config.yml for the placeholder block).
// Kept as a distinct *mail.Sender rather than a per-message override because
// qtoolkit/mail.Message has no From field at all — the From header is always
// derived from the Sender's own bound config prefix (send_from). So a branded
// from_name/from_email pair can only be delivered via a second prefix, not a
// per-call parameter.
var edmSenderOverleap = mail.Config("edm_overleap")

// edmSenderForBrand picks the EDM sender identity for the recipient's brand.
// Overleap's own sending domain requires Phase 0 domain verification with the
// mail provider before edm_overleap.* can be filled in; edm.overleap_from_email
// is the readiness signal ops flips once that's done. Until then this falls
// back to the shared kaitu sender so Overleap EDM keeps sending (under the
// kaitu identity) instead of failing outright on an unconfigured prefix.
func edmSenderForBrand(b Brand) *mail.Sender {
	if b == BrandOverleap && viper.GetString("edm.overleap_from_email") != "" {
		return edmSenderOverleap
	}
	return edmSender
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

// sendEmail 通过 qtoolkit/mail 发送邮件（dev 模式仅打印不发送）。发件身份按
// brand 选择 sender（见 edmSenderForBrand）——from_name 由 BrandConfig.EDMFromName
// 定义，但受 qtoolkit/mail 无 per-message From 的限制，实际生效值烘焙进对应 sender
// 绑定的 viper 前缀（edm.send_from / edm_overleap.send_from），本函数只负责选对
// sender，不直接拼装 From 头。
func sendEmail(ctx context.Context, to, subject, body string, brand Brand) error {
	if isMailDevMode() {
		log.Infof(ctx, "[DEV-MAIL-EDM] TO: %s | SUBJECT: %s | BRAND: %s (from_name=%s)\n--- BODY ---\n%s\n--- END ---",
			to, subject, brand, brand.Config().EDMFromName, body)
		return nil
	}

	if err := edmSenderForBrand(brand).Send(&mail.Message{
		To:      to,
		Subject: subject,
		Body:    body,
	}); err != nil {
		log.Errorf(ctx, "failed to send EDM email to %s (brand=%s): %v", to, brand, err)
		return fmt.Errorf("EDM send failed: %w", err)
	}

	log.Infof(ctx, "EDM email sent to %s (brand=%s)", to, brand)
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
	newTemplate := buildTranslatedTemplate(template, targetLang, translatedSubject, translatedContent)

	if err := db.Get().Create(&newTemplate).Error; err != nil {
		log.Errorf(ctx, "failed to save translated template: %v", err)
		return nil, fmt.Errorf("failed to save translation: %w", err)
	}

	log.Infof(ctx, "successfully created translation %d for template %d, language %s", newTemplate.ID, templateID, targetLang)
	return &newTemplate, nil
}

// buildTranslatedTemplate 组装懒翻译产出的模板记录。抽成纯函数（无 AI/DB 调用）便于
// 单独测试——之前这段内联在 getTemplateForLanguage 里时漏拷贝了 source.Brand，
// 导致所有自动翻译的模板一律落 kaitu（EmailMarketingTemplate.Brand 的 GORM
// default:'kaitu'），品牌信息在翻译这一跳丢失。
func buildTranslatedTemplate(source EmailMarketingTemplate, targetLang, translatedSubject, translatedContent string) EmailMarketingTemplate {
	sourceTemplateID := source.ID
	return EmailMarketingTemplate{
		Name:        source.Name + " (" + targetLang + ")",
		Language:    targetLang,
		Subject:     translatedSubject,
		Content:     translatedContent,
		IsActive:    BoolPtr(true),
		Description: "Auto-translated from " + source.Language,
		OriginID:    &sourceTemplateID, // 关联到源模板
		Brand:       source.Brand,
	}
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
