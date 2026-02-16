package center

import (
	"context"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// sendEmailsWithTracking 顺序发送邮件（带完整追踪和幂等性检查）
// 支持 context cancellation：当 context 被取消时，会优雅地停止发送并返回当前进度
func sendEmailsWithTracking(ctx context.Context, batchID string, users []User, template *EmailMarketingTemplate) EDMTaskOutput {
	var (
		sentCount    int
		failedCount  int
		skippedCount int
		failedEmails []string
		cancelled    bool
	)

	// 速率限制器：每秒最多发送 10 封邮件
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// 顺序发送
	for i, user := range users {
		// 使用 select 同时监听 context cancellation 和速率限制器
		// 这样可以在 context 被取消时立即响应，避免 goroutine 泄漏
		select {
		case <-ctx.Done():
			// Context 被取消（超时或主动取消），优雅退出
			log.Warnf(ctx, "[EDM] Batch %s cancelled: processed %d/%d users (sent=%d, failed=%d, skipped=%d), reason: %v",
				batchID, i, len(users), sentCount, failedCount, skippedCount, ctx.Err())
			cancelled = true
			// 跳出循环，返回当前进度
			goto done
		case <-ticker.C:
			// 速率限制器允许，继续处理
		}

		// 获取用户邮箱（用于日志记录）
		email, emailErr := getUserEmailByUser(ctx, &user)
		if emailErr != nil {
			log.Errorf(ctx, "[EDM] Failed to get email for user %d: %v", user.ID, emailErr)
			failedCount++
			createEmailSendLog(ctx, batchID, template.ID, user.ID, "", "", EmailSendLogStatusFailed, emailErr.Error())
			continue
		}

		// 跨批次幂等性检查：检查 24 小时内是否已向该用户发送过此模板
		recentlySent, err := HasSentTemplateToUserRecently(template.ID, user.ID, 24)
		if err != nil {
			log.Errorf(ctx, "[EDM] Cross-batch idempotency check failed for user %d: %v", user.ID, err)
			failedCount++
			createEmailSendLog(ctx, batchID, template.ID, user.ID, email, "", EmailSendLogStatusFailed, fmt.Sprintf("cross-batch check failed: %v", err))
			continue
		}
		if recentlySent {
			log.Debugf(ctx, "[EDM] Skipping user %d (template %d already sent within 24h)", user.ID, template.ID)
			skippedCount++
			createEmailSendLog(ctx, batchID, template.ID, user.ID, email, "", EmailSendLogStatusSkipped, "already sent within 24h")
			continue
		}

		// 批次内幂等性检查：检查当前批次是否已处理过
		exists, err := IsIdempotencyKeyExists(batchID, template.ID, user.ID)
		if err != nil {
			log.Errorf(ctx, "[EDM] Batch idempotency check failed for user %d: %v", user.ID, err)
			failedCount++
			createEmailSendLog(ctx, batchID, template.ID, user.ID, email, "", EmailSendLogStatusFailed, err.Error())
			continue
		}
		if exists {
			log.Debugf(ctx, "[EDM] Skipping user %d (already processed in this batch)", user.ID)
			skippedCount++
			continue
		}

		// 获取用户语言偏好
		userLang := getUserLanguagePreference(&user)

		// 创建发送日志（状态为 pending）
		sendLog := createEmailSendLog(ctx, batchID, template.ID, user.ID, email, userLang, EmailSendLogStatusPending, "")

		// 发送邮件
		if err := sendEmailToSingleUserWithLog(ctx, &user, template, email, userLang, sendLog); err != nil {
			failedCount++

			// 记录失败的邮箱（最多10个）
			if len(failedEmails) < 10 {
				failedEmails = append(failedEmails, email)
			}

			log.Errorf(ctx, "[EDM] Failed to send email to user %d (%s): %v", user.ID, email, err)
		} else {
			sentCount++
		}

		// 每 100 封邮件打印进度
		if (i+1)%100 == 0 {
			log.Infof(ctx, "[EDM] Progress: %d/%d (sent=%d, failed=%d, skipped=%d)",
				i+1, len(users), sentCount, failedCount, skippedCount)
		}
	}

done:
	// 如果是因为取消而退出，将未处理的用户计入 skipped
	if cancelled {
		// 注意：这里不增加 skippedCount，因为这些用户实际上没有被处理
		// 调用方可以通过 TotalUsers - SentCount - FailedCount - SkippedCount 计算未处理数
		log.Infof(ctx, "[EDM] Batch %s finished early due to cancellation", batchID)
	}

	return EDMTaskOutput{
		SentCount:    sentCount,
		FailedCount:  failedCount,
		SkippedCount: skippedCount,
		FailedEmails: failedEmails,
	}
}

// sendEmailToSingleUserWithLog 发送邮件给单个用户（带日志更新）
func sendEmailToSingleUserWithLog(ctx context.Context, user *User, template *EmailMarketingTemplate, email, userLang string, sendLog *EmailSendLog) error {
	// 1. 获取对应语言的模板（支持lazy translation）
	emailTemplate, err := getTemplateForLanguage(ctx, template.ID, userLang)
	if err != nil {
		updateEmailSendLogStatus(sendLog, EmailSendLogStatusFailed, fmt.Sprintf("get template failed: %v", err))
		return fmt.Errorf("get template failed: %w", err)
	}

	// 2. 渲染邮件内容
	subject, content, err := renderEmailContent(ctx, *user, *emailTemplate)
	if err != nil {
		updateEmailSendLogStatus(sendLog, EmailSendLogStatusFailed, fmt.Sprintf("render content failed: %v", err))
		return fmt.Errorf("render content failed: %w", err)
	}

	// 3. 发送邮件
	if err := sendEmail(ctx, email, subject, content); err != nil {
		updateEmailSendLogStatus(sendLog, EmailSendLogStatusFailed, fmt.Sprintf("send failed: %v", err))
		return fmt.Errorf("send failed: %w", err)
	}

	// 4. 更新发送日志为成功
	updateEmailSendLogStatus(sendLog, EmailSendLogStatusSent, "")
	return nil
}

// createEmailSendLog 创建邮件发送日志
func createEmailSendLog(ctx context.Context, batchID string, templateID, userID uint64, email, language string, status EmailSendLogStatus, errMsg string) *EmailSendLog {
	sendLog := &EmailSendLog{
		BatchID:    batchID,
		TemplateID: templateID,
		UserID:     userID,
		Email:      email,
		Language:   language,
		Status:     status,
	}

	if errMsg != "" {
		sendLog.ErrorMsg = &errMsg
	}

	if status == EmailSendLogStatusSent {
		now := time.Now()
		sendLog.SentAt = &now
	}

	if err := db.Get().Create(sendLog).Error; err != nil {
		log.Errorf(ctx, "[EDM] Failed to create email send log: %v", err)
		return nil
	}

	return sendLog
}

// updateEmailSendLogStatus 更新邮件发送日志状态
func updateEmailSendLogStatus(sendLog *EmailSendLog, status EmailSendLogStatus, errMsg string) {
	if sendLog == nil || sendLog.ID == 0 {
		return
	}

	updates := map[string]any{
		"status": status,
	}

	if errMsg != "" {
		updates["error_msg"] = errMsg
	}

	if status == EmailSendLogStatusSent {
		now := time.Now()
		updates["sent_at"] = now
	}

	db.Get().Model(sendLog).Updates(updates)
}
