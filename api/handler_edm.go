package center

import (
	"context"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

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
