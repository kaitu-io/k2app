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
// 邮件生命周期 Worker (续费提醒 + 过期召回)
// 基于 Asynq Cron，每天 02:30 UTC (10:30 北京时间) 运行
// =====================================================================

const (
	TaskTypeRenewalReminder = "renewal:reminder"
)

// 系统自动邮件使用虚拟模板 ID（0 表示非模板邮件）
const systemEmailTemplateID = 0

// 触发天数配置
// 正数 = 到期前提醒，负数 = 到期后召回
var emailTriggerDays = []int{30, 14, 7, 3, -1, -7, -30}

// handleRenewalReminderTask 处理每日邮件生命周期任务（续费提醒 + 过期召回）
func handleRenewalReminderTask(ctx context.Context, _ []byte) error {
	log.Infof(ctx, "[EMAIL-LIFECYCLE] Starting daily email lifecycle task")

	loc, _ := time.LoadLocation("Asia/Shanghai")
	beijingNow := time.Now().In(loc)
	log.Infof(ctx, "[EMAIL-LIFECYCLE] Beijing time: %s", beijingNow.Format("2006-01-02 15:04:05"))

	var totalSent, totalSkipped, totalFailed int

	for _, days := range emailTriggerDays {
		var sent, skipped, failed int
		if days > 0 {
			sent, skipped, failed = processRenewalReminders(ctx, days)
		} else {
			sent, skipped, failed = processWinback(ctx, -days)
		}
		totalSent += sent
		totalSkipped += skipped
		totalFailed += failed
	}

	log.Infof(ctx, "[EMAIL-LIFECYCLE] Daily task completed: sent=%d, skipped=%d, failed=%d",
		totalSent, totalSkipped, totalFailed)

	return nil
}

// =====================================================================
// 续费提醒（到期前）
// =====================================================================

// processRenewalReminders 处理指定天数的续费提醒
func processRenewalReminders(ctx context.Context, daysBefore int) (sent, skipped, failed int) {
	log.Infof(ctx, "[RENEWAL] Processing %d-day reminders", daysBefore)

	now := time.Now().UTC()
	todayStr := now.Format("2006-01-02")
	targetDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, daysBefore)
	targetDateEnd := targetDate.AddDate(0, 0, 1)

	batchID := fmt.Sprintf("renewal:%dd:%s", daysBefore, todayStr)

	var users []User
	err := db.Get().Model(&User{}).
		Preload("LoginIdentifies").
		Where("expired_at >= ? AND expired_at < ?", targetDate.Unix(), targetDateEnd.Unix()).
		Where("is_first_order_done = ?", true).
		Find(&users).Error

	if err != nil {
		log.Errorf(ctx, "[RENEWAL] Failed to query users for %d-day reminders: %v", daysBefore, err)
		return 0, 0, 1
	}

	log.Infof(ctx, "[RENEWAL] Found %d users expiring in %d days", len(users), daysBefore)

	for _, user := range users {
		email := getUserEmailFromIdentifies(&user)
		if email == "" {
			log.Warnf(ctx, "[RENEWAL] User %d has no email, skipping", user.ID)
			skipped++
			continue
		}

		exists, _ := IsIdempotencyKeyExists(batchID, systemEmailTemplateID, user.ID)
		if exists {
			skipped++
			continue
		}

		sendLog := createSystemSendLog(ctx, batchID, user.ID, email)

		subject, body := getRenewalReminderContent(daysBefore)
		if err := sendLifecycleEmail(ctx, email, subject, body); err != nil {
			log.Errorf(ctx, "[RENEWAL] Failed to send reminder to user %d: %v", user.ID, err)
			updateSystemSendLogStatus(sendLog, EmailSendLogStatusFailed, err.Error())
			failed++
			continue
		}

		updateSystemSendLogStatus(sendLog, EmailSendLogStatusSent, "")
		sent++

		log.Infof(ctx, "[RENEWAL] Sent %d-day reminder to user %d (%s)", daysBefore, user.ID, hideEmail(email))
	}

	log.Infof(ctx, "[RENEWAL] %d-day reminders: sent=%d, skipped=%d, failed=%d",
		daysBefore, sent, skipped, failed)

	return sent, skipped, failed
}

// getRenewalReminderContent 获取续费提醒邮件内容
func getRenewalReminderContent(daysBefore int) (string, string) {
	switch daysBefore {
	case 30:
		return "你的开途账号还有 30 天到期",
			"Hi，\n\n你的开途账号将于 30 天后到期。\n\n提前续费，避免连接中断：\nhttps://kaitu.io/purchase\n\n开途团队"

	case 14:
		return "开途账号即将到期，建议尽快续费",
			"Hi，\n\n你的开途账号将于 14 天后到期，建议尽快续费，避免服务中断影响使用。\n\n续费链接：\nhttps://kaitu.io/purchase\n\n开途团队"

	case 7:
		return "开途账号下周到期",
			"Hi，\n\n你的开途账号将于 7 天后到期。到期后所有设备连接将自动断开。\n\n续费链接：\nhttps://kaitu.io/purchase\n\n开途团队"

	case 3:
		return "还有 3 天，开途账号即将到期",
			"Hi，\n\n你的开途账号还有 3 天到期。到期后连接立即中断，请尽快续费。\n\n续费链接：\nhttps://kaitu.io/purchase\n\n开途团队"

	default:
		return fmt.Sprintf("开途账号还有 %d 天到期", daysBefore),
			fmt.Sprintf("Hi，\n\n你的开途账号将于 %d 天后到期。\n\n续费链接：\nhttps://kaitu.io/purchase\n\n开途团队", daysBefore)
	}
}

// =====================================================================
// 过期召回（到期后）
// =====================================================================

// processWinback 处理指定天数的过期召回
func processWinback(ctx context.Context, daysAfter int) (sent, skipped, failed int) {
	log.Infof(ctx, "[WINBACK] Processing %d-day winback", daysAfter)

	now := time.Now().UTC()
	todayStr := now.Format("2006-01-02")

	// 目标到期日 = 今天 - daysAfter
	targetDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -daysAfter)
	targetDateEnd := targetDate.AddDate(0, 0, 1)

	batchID := fmt.Sprintf("winback:%dd:%s", daysAfter, todayStr)

	var users []User
	err := db.Get().Model(&User{}).
		Preload("LoginIdentifies").
		Where("expired_at >= ? AND expired_at < ?", targetDate.Unix(), targetDateEnd.Unix()).
		Where("is_first_order_done = ?", true).
		Find(&users).Error

	if err != nil {
		log.Errorf(ctx, "[WINBACK] Failed to query users for %d-day winback: %v", daysAfter, err)
		return 0, 0, 1
	}

	log.Infof(ctx, "[WINBACK] Found %d users expired %d days ago", len(users), daysAfter)

	// 循环外查一次价格区间，避免 N 次重复查询
	subject, body := getWinbackContent(ctx, daysAfter)

	for _, user := range users {
		// 用户已续费（expired_at 移到未来），跳过
		if user.ExpiredAt > now.Unix() {
			skipped++
			continue
		}

		email := getUserEmailFromIdentifies(&user)
		if email == "" {
			log.Warnf(ctx, "[WINBACK] User %d has no email, skipping", user.ID)
			skipped++
			continue
		}

		exists, _ := IsIdempotencyKeyExists(batchID, systemEmailTemplateID, user.ID)
		if exists {
			skipped++
			continue
		}

		sendLog := createSystemSendLog(ctx, batchID, user.ID, email)

		if err := sendLifecycleEmail(ctx, email, subject, body); err != nil {
			log.Errorf(ctx, "[WINBACK] Failed to send winback to user %d: %v", user.ID, err)
			updateSystemSendLogStatus(sendLog, EmailSendLogStatusFailed, err.Error())
			failed++
			continue
		}

		updateSystemSendLogStatus(sendLog, EmailSendLogStatusSent, "")
		sent++

		log.Infof(ctx, "[WINBACK] Sent %d-day winback to user %d (%s)", daysAfter, user.ID, hideEmail(email))
	}

	log.Infof(ctx, "[WINBACK] %d-day winback: sent=%d, skipped=%d, failed=%d",
		daysAfter, sent, skipped, failed)

	return sent, skipped, failed
}

// winbackCampaign 召回邮件的活动码配置
type winbackCampaign struct {
	code         string // 活动码
	discountPct  int    // 折扣百分比（90 = 九折）
	validDaysStr string // 有效期描述
}

// 召回活动码配置：daysAfter → campaign
var winbackCampaigns = map[int]winbackCampaign{
	7:  {code: "BACK90", discountPct: 90, validDaysStr: "14 天内有效"},
	30: {code: "BACK85", discountPct: 85, validDaysStr: "30 天内有效"},
}

// getPlanPriceRange 查询当前激活套餐的最低和最高售价（美分）
func getPlanPriceRange(ctx context.Context) (minPrice, maxPrice uint64, ok bool) {
	var plans []Plan
	if err := db.Get().Where("is_active = ?", true).Select("price").Find(&plans).Error; err != nil {
		log.Errorf(ctx, "[WINBACK] Failed to query plans: %v", err)
		return 0, 0, false
	}
	if len(plans) == 0 {
		return 0, 0, false
	}
	minPrice = plans[0].Price
	maxPrice = plans[0].Price
	for _, p := range plans[1:] {
		if p.Price < minPrice {
			minPrice = p.Price
		}
		if p.Price > maxPrice {
			maxPrice = p.Price
		}
	}
	return minPrice, maxPrice, true
}

// formatCents 美分转美元字符串（如 390 → "$3.9", 2235 → "$22.35"）
func formatCents(cents uint64) string {
	dollars := cents / 100
	remaining := cents % 100
	if remaining == 0 {
		return fmt.Sprintf("$%d", dollars)
	}
	if remaining%10 == 0 {
		return fmt.Sprintf("$%d.%d", dollars, remaining/10)
	}
	return fmt.Sprintf("$%d.%02d", dollars, remaining)
}

// getWinbackContent 获取召回邮件内容
func getWinbackContent(ctx context.Context, daysAfter int) (string, string) {
	switch daysAfter {
	case 1:
		return "你的开途连接已断开",
			"Hi，\n\n你的开途账号已于昨天到期，所有设备的连接已中断。\n\n续费后立即恢复：\nhttps://kaitu.io/purchase\n\n开途团队"

	case 7, 30:
		campaign, hasCampaign := winbackCampaigns[daysAfter]
		if !hasCampaign {
			break
		}

		minPrice, maxPrice, ok := getPlanPriceRange(ctx)
		if !ok {
			// 查不到价格就发不带金额的简版
			break
		}

		// 立减金额 = 售价 × (1 - discountPct/100)
		minSave := minPrice * uint64(100-campaign.discountPct) / 100
		maxSave := maxPrice * uint64(100-campaign.discountPct) / 100

		savingsText := fmt.Sprintf("立减 %s 起，最高立减 %s", formatCents(minSave), formatCents(maxSave))

		if daysAfter == 7 {
			return fmt.Sprintf("回来看看？续费%s", savingsText),
				fmt.Sprintf("Hi，\n\n你已经有一周没有使用开途了。\n\n限时续费优惠（%s），%s：\n%s\n\n使用方式：前往 https://kaitu.io/purchase，结算时输入优惠码即可。\n\n开途团队",
					campaign.validDaysStr, savingsText, campaign.code)
		}

		return fmt.Sprintf("开途升级了很多，%s欢迎回来", savingsText),
			fmt.Sprintf("Hi，\n\n你上次使用开途已经是一个月前了。\n\n这段时间我们发布了 v0.4，全平台支持（iOS/Android/桌面端），连接稳定性大幅提升，还新增了 AI 工具直接管理网络连接的能力。\n\n限时续费优惠（%s），%s：\n%s\n\n使用方式：前往 https://kaitu.io/purchase，结算时输入优惠码即可。\n\n开途团队",
				campaign.validDaysStr, savingsText, campaign.code)
	}

	// fallback
	return fmt.Sprintf("开途账号已过期 %d 天", daysAfter),
		fmt.Sprintf("Hi，\n\n你的开途账号已过期 %d 天。\n\n续费链接：\nhttps://kaitu.io/purchase\n\n开途团队", daysAfter)
}

// =====================================================================
// 共享工具函数
// =====================================================================

// createSystemSendLog 创建系统邮件发送日志
func createSystemSendLog(ctx context.Context, batchID string, userID uint64, email string) *EmailSendLog {
	sendLog := &EmailSendLog{
		BatchID:    batchID,
		TemplateID: systemEmailTemplateID,
		UserID:     userID,
		Email:      email,
		Language:   "zh-CN",
		Status:     EmailSendLogStatusPending,
	}

	if err := db.Get().Create(sendLog).Error; err != nil {
		log.Errorf(ctx, "[EMAIL-LIFECYCLE] Failed to create send log: %v", err)
		return nil
	}

	return sendLog
}

// updateSystemSendLogStatus 更新发送日志状态
func updateSystemSendLogStatus(sendLog *EmailSendLog, status EmailSendLogStatus, errMsg string) {
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

// sendLifecycleEmail 发送生命周期邮件
func sendLifecycleEmail(ctx context.Context, email, subject, body string) error {
	err := MailSend(ctx, &mail.Message{
		To:      email,
		Subject: subject,
		Body:    body,
	})
	if err != nil {
		return fmt.Errorf("MailSend failed: %w", err)
	}
	return nil
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
