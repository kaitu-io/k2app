package center

import (
	"context"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/mail"
)

// =====================================================================
// 续费提醒 Worker (基于 Asynq Cron)
// =====================================================================

// 任务类型常量
const (
	TaskTypeRenewalReminder = "renewal:reminder"
)

// 续费提醒使用的虚拟模板 ID（0 表示系统自动生成的邮件）
const renewalReminderTemplateID = 0

// 提醒天数配置：在到期前 30、14、7、3 天发送提醒
var reminderDaysBefore = []int{30, 14, 7, 3}

// handleRenewalReminderTask 处理续费提醒任务
func handleRenewalReminderTask(ctx context.Context, _ []byte) error {
	log.Infof(ctx, "[RENEWAL] Starting daily renewal reminder task")

	// 获取北京时间（用于日志）
	loc, _ := time.LoadLocation("Asia/Shanghai")
	beijingNow := time.Now().In(loc)
	log.Infof(ctx, "[RENEWAL] Beijing time: %s", beijingNow.Format("2006-01-02 15:04:05"))

	// 统计
	var totalSent, totalSkipped, totalFailed int

	// 遍历每个提醒天数
	for _, daysBefore := range reminderDaysBefore {
		sent, skipped, failed := processRenewalReminders(ctx, daysBefore)
		totalSent += sent
		totalSkipped += skipped
		totalFailed += failed
	}

	log.Infof(ctx, "[RENEWAL] Daily task completed: sent=%d, skipped=%d, failed=%d",
		totalSent, totalSkipped, totalFailed)

	return nil
}

// processRenewalReminders 处理指定天数的续费提醒
func processRenewalReminders(ctx context.Context, daysBefore int) (sent, skipped, failed int) {
	log.Infof(ctx, "[RENEWAL] Processing %d-day reminders", daysBefore)

	// 计算目标日期范围（UTC 当天 00:00:00 到 23:59:59）
	now := time.Now().UTC()
	todayStr := now.Format("2006-01-02")
	targetDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, daysBefore)
	targetDateEnd := targetDate.AddDate(0, 0, 1)

	// 生成批次 ID：renewal:{days}d:{date}
	batchID := fmt.Sprintf("renewal:%dd:%s", daysBefore, todayStr)

	// 查询在目标日期到期的用户
	var users []User
	err := db.Get().Model(&User{}).
		Preload("LoginIdentifies").
		Where("expired_at >= ? AND expired_at < ?", targetDate.Unix(), targetDateEnd.Unix()).
		Where("is_first_order_done = ?", true). // 只提醒已付过费的用户
		Find(&users).Error

	if err != nil {
		log.Errorf(ctx, "[RENEWAL] Failed to query users for %d-day reminders: %v", daysBefore, err)
		return 0, 0, 1
	}

	log.Infof(ctx, "[RENEWAL] Found %d users expiring in %d days", len(users), daysBefore)

	// 遍历用户发送提醒
	for _, user := range users {
		// 获取用户邮箱
		email := getUserEmailFromIdentifies(&user)
		if email == "" {
			log.Warnf(ctx, "[RENEWAL] User %d has no email, skipping", user.ID)
			skipped++
			continue
		}

		// 检查是否已发送过提醒（幂等性检查）
		exists, _ := IsIdempotencyKeyExists(batchID, renewalReminderTemplateID, user.ID)
		if exists {
			skipped++
			continue
		}

		// 创建发送日志（状态为 pending）
		sendLog := createRenewalSendLog(ctx, batchID, user.ID, email)

		// 发送提醒邮件
		if err := sendRenewalReminderEmail(ctx, email, daysBefore); err != nil {
			log.Errorf(ctx, "[RENEWAL] Failed to send reminder to user %d: %v", user.ID, err)
			updateRenewalSendLogStatus(sendLog, EmailSendLogStatusFailed, err.Error())
			failed++
			continue
		}

		// 更新日志为成功
		updateRenewalSendLogStatus(sendLog, EmailSendLogStatusSent, "")
		sent++

		log.Infof(ctx, "[RENEWAL] Sent %d-day reminder to user %d (%s)", daysBefore, user.ID, hideEmail(email))
	}

	log.Infof(ctx, "[RENEWAL] %d-day reminders: sent=%d, skipped=%d, failed=%d",
		daysBefore, sent, skipped, failed)

	return sent, skipped, failed
}

// createRenewalSendLog 创建续费提醒发送日志
func createRenewalSendLog(ctx context.Context, batchID string, userID uint64, email string) *EmailSendLog {
	sendLog := &EmailSendLog{
		BatchID:    batchID,
		TemplateID: renewalReminderTemplateID,
		UserID:     userID,
		Email:      email,
		Language:   "zh-CN",
		Status:     EmailSendLogStatusPending,
	}

	if err := db.Get().Create(sendLog).Error; err != nil {
		log.Errorf(ctx, "[RENEWAL] Failed to create send log: %v", err)
		return nil
	}

	return sendLog
}

// updateRenewalSendLogStatus 更新发送日志状态
func updateRenewalSendLogStatus(sendLog *EmailSendLog, status EmailSendLogStatus, errMsg string) {
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

// getUserEmailFromIdentifies 从用户身份信息中获取邮箱
func getUserEmailFromIdentifies(user *User) string {
	for _, identity := range user.LoginIdentifies {
		if identity.Type == "email" && identity.EncryptedValue != "" {
			email, err := secretDecryptString(context.Background(), identity.EncryptedValue)
			if err == nil {
				return email
			}
		}
	}
	return ""
}

// sendRenewalReminderEmail 发送续费提醒邮件
func sendRenewalReminderEmail(ctx context.Context, email string, daysBefore int) error {
	subject, body := getRenewalReminderContent(daysBefore)

	log.Debugf(ctx, "[RENEWAL] Sending %d-day reminder to %s", daysBefore, hideEmail(email))

	// 使用 qtoolkit/mail 发送纯文本邮件
	err := mail.Send(&mail.Message{
		To:      email,
		Subject: subject,
		Body:    body,
	})

	if err != nil {
		return fmt.Errorf("mail.Send failed: %w", err)
	}

	return nil
}

// getRenewalReminderContent 获取续费提醒邮件内容
// 返回：主题、正文
func getRenewalReminderContent(daysBefore int) (string, string) {
	// 根据天数返回不同的邮件内容
	// 邮件风格：简洁、真诚，像真人写的
	switch daysBefore {
	case 30:
		return "Kaitu 会员即将到期提醒",
			`Hi，

你的 Kaitu 会员还有 30 天到期。

提前续费可以确保服务不中断，续费链接：
https://www.kaitu.io/purchase

有任何问题随时联系我们。

Kaitu 团队`

	case 14:
		return "Kaitu 会员还有 2 周到期",
			`Hi，

提醒一下，你的 Kaitu 会员还有 14 天到期。

为了避免服务中断，建议尽快续费：
https://www.kaitu.io/purchase

谢谢你的支持！

Kaitu 团队`

	case 7:
		return "Kaitu 会员下周到期",
			`Hi，

你的 Kaitu 会员将在 7 天后到期。

现在续费可以确保网络连接不受影响：
https://www.kaitu.io/purchase

如有疑问请回复此邮件。

Kaitu 团队`

	case 3:
		return "Kaitu 会员即将到期 - 3 天后",
			`Hi，

你的 Kaitu 会员还有 3 天就要到期了。

请尽快续费以保持服务连续：
https://www.kaitu.io/purchase

感谢你一直以来的使用！

Kaitu 团队`

	default:
		return fmt.Sprintf("Kaitu 会员还有 %d 天到期", daysBefore),
			fmt.Sprintf(`Hi，

你的 Kaitu 会员将在 %d 天后到期。

续费链接：https://www.kaitu.io/purchase

Kaitu 团队`, daysBefore)
	}
}
