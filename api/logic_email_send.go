package center

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// SendEmailItem 单封邮件的发送请求
type SendEmailItem struct {
	Email  string            `json:"email"`
	UserID uint64            `json:"userId"`
	Slug   string            `json:"slug"`
	Vars   map[string]string `json:"vars"`
}

// SendEmailsRequest 批量发送请求
type SendEmailsRequest struct {
	BatchID string          `json:"batchId"`
	Items   []SendEmailItem `json:"items"`
}

// SendEmailResultItem 单封邮件发送结果
type SendEmailResultItem struct {
	Email  string `json:"email"`
	Status string `json:"status"` // "sent", "failed", "skipped"
	Error  string `json:"error,omitempty"`
}

// SendEmailsResult 批量发送结果
type SendEmailsResult struct {
	BatchID string                `json:"batchId"`
	Total   int                   `json:"total"`
	Sent    int                   `json:"sent"`
	Failed  int                   `json:"failed"`
	Skipped int                   `json:"skipped"`
	Items   []SendEmailResultItem `json:"items,omitempty"`
}

var templateVarRegex = regexp.MustCompile(`\{\{\s*\.(\w+)\s*\}\}`)

func extractTemplateVars(subject, content string) []string {
	seen := make(map[string]bool)
	var vars []string
	for _, match := range templateVarRegex.FindAllStringSubmatch(subject+"\n"+content, -1) {
		name := match[1]
		if !seen[name] {
			seen[name] = true
			vars = append(vars, name)
		}
	}
	return vars
}

func validateTemplateVars(required []string, provided map[string]string) error {
	var missing []string
	for _, name := range required {
		if _, ok := provided[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing template variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

func renderTemplateString(name, tmplStr string, vars map[string]string) (string, error) {
	tmpl, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", name, err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("failed to execute template %s: %w", name, err)
	}
	return buf.String(), nil
}

// SendTemplatedEmails 通用邮件发送入口（同步模式）
func SendTemplatedEmails(ctx context.Context, req *SendEmailsRequest) (*SendEmailsResult, error) {
	if req.BatchID == "" {
		return nil, fmt.Errorf("batchId is required")
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("items cannot be empty")
	}

	log.Infof(ctx, "[EMAIL-SEND] Starting batch=%s, items=%d", req.BatchID, len(req.Items))

	result := &SendEmailsResult{
		BatchID: req.BatchID,
		Total:   len(req.Items),
		Items:   make([]SendEmailResultItem, 0, len(req.Items)),
	}

	templateCache := make(map[string]*EmailMarketingTemplate)
	varsCache := make(map[string][]string)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for i, item := range req.Items {
		select {
		case <-ctx.Done():
			log.Warnf(ctx, "[EMAIL-SEND] Batch %s cancelled at %d/%d", req.BatchID, i, len(req.Items))
			return result, nil
		case <-ticker.C:
		}

		itemResult := sendSingleTemplatedEmail(ctx, req.BatchID, &item, templateCache, varsCache)
		result.Items = append(result.Items, itemResult)

		switch itemResult.Status {
		case "sent":
			result.Sent++
		case "failed":
			result.Failed++
		case "skipped":
			result.Skipped++
		}

		if (i+1)%100 == 0 {
			log.Infof(ctx, "[EMAIL-SEND] Progress: %d/%d (sent=%d, failed=%d, skipped=%d)",
				i+1, len(req.Items), result.Sent, result.Failed, result.Skipped)
		}
	}

	log.Infof(ctx, "[EMAIL-SEND] Batch %s completed: sent=%d, failed=%d, skipped=%d",
		req.BatchID, result.Sent, result.Failed, result.Skipped)
	return result, nil
}

func sendSingleTemplatedEmail(
	ctx context.Context,
	batchID string,
	item *SendEmailItem,
	templateCache map[string]*EmailMarketingTemplate,
	varsCache map[string][]string,
) SendEmailResultItem {
	resultItem := SendEmailResultItem{Email: item.Email}

	// 1. Resolve user
	userID := item.UserID
	if userID == 0 {
		user, err := FindOrCreateUserByEmail(ctx, item.Email)
		if err != nil {
			resultItem.Status = "failed"
			resultItem.Error = fmt.Sprintf("resolve user failed: %v", err)
			createEmailSendLog(ctx, batchID, 0, 0, item.Email, "", EmailSendLogStatusFailed, resultItem.Error)
			return resultItem
		}
		userID = user.ID
	}

	// 2. Lookup template by slug (cached within batch)
	tmpl, ok := templateCache[item.Slug]
	if !ok {
		var t EmailMarketingTemplate
		if err := db.Get().Where("slug = ? AND is_active = ? AND origin_id IS NULL", item.Slug, true).
			First(&t).Error; err != nil {
			resultItem.Status = "failed"
			resultItem.Error = fmt.Sprintf("template slug %q not found", item.Slug)
			createEmailSendLog(ctx, batchID, 0, userID, item.Email, "", EmailSendLogStatusFailed, resultItem.Error)
			return resultItem
		}
		templateCache[item.Slug] = &t
		tmpl = &t
		varsCache[item.Slug] = extractTemplateVars(t.Subject, t.Content)
	}

	// 3. Validate variables
	if err := validateTemplateVars(varsCache[item.Slug], item.Vars); err != nil {
		resultItem.Status = "failed"
		resultItem.Error = err.Error()
		createEmailSendLog(ctx, batchID, tmpl.ID, userID, item.Email, "", EmailSendLogStatusFailed, resultItem.Error)
		return resultItem
	}

	// 4. Idempotency check
	exists, err := IsIdempotencyKeyExists(batchID, tmpl.ID, userID)
	if err != nil {
		resultItem.Status = "failed"
		resultItem.Error = fmt.Sprintf("idempotency check failed: %v", err)
		createEmailSendLog(ctx, batchID, tmpl.ID, userID, item.Email, "", EmailSendLogStatusFailed, resultItem.Error)
		return resultItem
	}
	if exists {
		resultItem.Status = "skipped"
		return resultItem
	}

	// 5. Language handling: find translated template if needed
	emailTemplate := tmpl
	userLang := "zh-CN"
	if userID > 0 {
		var user User
		if err := db.Get().Preload("LoginIdentifies").First(&user, userID).Error; err == nil {
			userLang = getUserLanguagePreference(&user)
			if userLang != tmpl.Language {
				if translated, err := getTemplateForLanguage(ctx, tmpl.ID, userLang); err == nil {
					emailTemplate = translated
				}
			}
		}
	}

	// 6. Render subject and content
	subject, err := renderTemplateString("subject", emailTemplate.Subject, item.Vars)
	if err != nil {
		resultItem.Status = "failed"
		resultItem.Error = fmt.Sprintf("render subject failed: %v", err)
		createEmailSendLog(ctx, batchID, tmpl.ID, userID, item.Email, userLang, EmailSendLogStatusFailed, resultItem.Error)
		return resultItem
	}

	content, err := renderTemplateString("content", emailTemplate.Content, item.Vars)
	if err != nil {
		resultItem.Status = "failed"
		resultItem.Error = fmt.Sprintf("render content failed: %v", err)
		createEmailSendLog(ctx, batchID, tmpl.ID, userID, item.Email, userLang, EmailSendLogStatusFailed, resultItem.Error)
		return resultItem
	}

	// 7. Create log (pending)
	sendLog := createEmailSendLog(ctx, batchID, tmpl.ID, userID, item.Email, userLang, EmailSendLogStatusPending, "")

	// 8. Send
	if err := sendEmail(ctx, item.Email, subject, content); err != nil {
		resultItem.Status = "failed"
		resultItem.Error = fmt.Sprintf("send failed: %v", err)
		updateEmailSendLogStatus(sendLog, EmailSendLogStatusFailed, resultItem.Error)
		return resultItem
	}

	// 9. Success
	updateEmailSendLogStatus(sendLog, EmailSendLogStatusSent, "")
	resultItem.Status = "sent"
	return resultItem
}
