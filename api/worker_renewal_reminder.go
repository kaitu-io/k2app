package center

import (
	"context"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
)

// =====================================================================
// 邮件生命周期 Worker (续费提醒 + 过期召回)
// 基于 Asynq Cron，每天 02:30 UTC (10:30 北京时间) 运行
// =====================================================================

const (
	TaskTypeRenewalReminder = "renewal:reminder"
)

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
	slug := fmt.Sprintf("renewal-%dd", daysBefore)

	if !templateSlugExists(slug) {
		alertMsg := fmt.Sprintf("[EMAIL-LIFECYCLE] 模板 slug=%q 不存在，跳过 %d 天续费提醒。请在管理后台创建该模板。", slug, daysBefore)
		log.Errorf(ctx, "%s", alertMsg)
		slack.Send("alert", alertMsg)
		return 0, 0, 0
	}

	var users []User
	err := db.Get().Model(&User{}).
		Preload("LoginIdentifies").
		Where("expired_at >= ? AND expired_at < ?", targetDate.Unix(), targetDateEnd.Unix()).
		Where("is_first_order_done = ?", true).
		Find(&users).Error

	if err != nil {
		log.Errorf(ctx, "[RENEWAL] Failed to query users: %v", err)
		return 0, 0, 1
	}

	log.Infof(ctx, "[RENEWAL] Found %d users expiring in %d days", len(users), daysBefore)

	items := make([]SendEmailItem, 0, len(users))
	for _, user := range users {
		email := getUserEmailFromIdentifies(&user)
		if email == "" {
			skipped++
			continue
		}
		items = append(items, SendEmailItem{
			Email:  email,
			UserID: user.ID,
			Slug:   slug,
			Vars:   map[string]string{},
		})
	}

	if len(items) == 0 {
		return 0, skipped, 0
	}

	result, err := SendTemplatedEmails(ctx, &SendEmailsRequest{
		BatchID: batchID,
		Items:   items,
	})
	if err != nil {
		log.Errorf(ctx, "[RENEWAL] SendTemplatedEmails failed: %v", err)
		return 0, skipped, len(items)
	}

	return result.Sent, skipped + result.Skipped, result.Failed
}

// =====================================================================
// 过期召回（到期后）
// =====================================================================

// processWinback 处理指定天数的过期召回
func processWinback(ctx context.Context, daysAfter int) (sent, skipped, failed int) {
	log.Infof(ctx, "[WINBACK] Processing %d-day winback", daysAfter)

	now := time.Now().UTC()
	todayStr := now.Format("2006-01-02")
	targetDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -daysAfter)
	targetDateEnd := targetDate.AddDate(0, 0, 1)

	batchID := fmt.Sprintf("winback:%dd:%s", daysAfter, todayStr)
	slug := fmt.Sprintf("winback-%dd", daysAfter)

	if !templateSlugExists(slug) {
		alertMsg := fmt.Sprintf("[EMAIL-LIFECYCLE] 模板 slug=%q 不存在，跳过 %d 天过期召回。请在管理后台创建该模板。", slug, daysAfter)
		log.Errorf(ctx, "%s", alertMsg)
		slack.Send("alert", alertMsg)
		return 0, 0, 0
	}

	var users []User
	err := db.Get().Model(&User{}).
		Preload("LoginIdentifies").
		Where("expired_at >= ? AND expired_at < ?", targetDate.Unix(), targetDateEnd.Unix()).
		Where("is_first_order_done = ?", true).
		Find(&users).Error

	if err != nil {
		log.Errorf(ctx, "[WINBACK] Failed to query users: %v", err)
		return 0, 0, 1
	}

	log.Infof(ctx, "[WINBACK] Found %d users expired %d days ago", len(users), daysAfter)

	vars := map[string]string{}
	campaign, hasCampaign := winbackCampaigns[daysAfter]
	if hasCampaign {
		minPrice, maxPrice, ok := getPlanPriceRange(ctx)
		if ok {
			minSave := minPrice * uint64(100-campaign.discountPct) / 100
			maxSave := maxPrice * uint64(100-campaign.discountPct) / 100
			vars["SavingsText"] = fmt.Sprintf("立减 %s 起，最高立减 %s", formatCents(minSave), formatCents(maxSave))
			vars["CampaignCode"] = campaign.code
			vars["ValidDays"] = campaign.validDaysStr
		}
	}

	items := make([]SendEmailItem, 0, len(users))
	for _, user := range users {
		if user.ExpiredAt > now.Unix() {
			skipped++
			continue
		}

		email := getUserEmailFromIdentifies(&user)
		if email == "" {
			skipped++
			continue
		}

		items = append(items, SendEmailItem{
			Email:  email,
			UserID: user.ID,
			Slug:   slug,
			Vars:   vars,
		})
	}

	if len(items) == 0 {
		return 0, skipped, 0
	}

	result, err := SendTemplatedEmails(ctx, &SendEmailsRequest{
		BatchID: batchID,
		Items:   items,
	})
	if err != nil {
		log.Errorf(ctx, "[WINBACK] SendTemplatedEmails failed: %v", err)
		return 0, skipped, len(items)
	}

	return result.Sent, skipped + result.Skipped, result.Failed
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

// templateSlugExists 检查模板 slug 是否存在
func templateSlugExists(slug string) bool {
	var count int64
	db.Get().Model(&EmailMarketingTemplate{}).
		Where("slug = ? AND is_active = ? AND origin_id IS NULL", slug, true).
		Count(&count)
	return count > 0
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
