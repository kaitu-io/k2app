package center

import (
	"context"
	"fmt"
	"maps"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
)

const (
	TaskTypeAbandonedOrderHourly = "abandoned:hourly"
	TaskTypeAbandonedOrderDaily  = "abandoned:daily"
)

var (
	abandonedHourlyDelays = []int{1}
	abandonedDailyDelays  = []int{1, 3, 7, 14, 30}
)

type abandonedCampaign struct {
	code         string
	discountPct  int
	validDaysStr string
}

var abandonedCampaigns = map[int]abandonedCampaign{
	3:  {code: "READY4U", discountPct: 95, validDaysStr: "7 天内有效"},
	7:  {code: "STAYFREE", discountPct: 90, validDaysStr: "14 天内有效"},
	14: {code: "SMOOTHDAY", discountPct: 90, validDaysStr: "14 天内有效"},
	30: {code: "KEEPGOING", discountPct: 85, validDaysStr: "30 天内有效"},
}

func handleAbandonedOrderHourlyTask(ctx context.Context, _ []byte) error {
	log.Infof(ctx, "[ABANDONED] Starting hourly abandoned order check")

	var totalSent, totalSkipped, totalFailed int
	for _, hours := range abandonedHourlyDelays {
		sent, skipped, failed := processAbandonedHourly(ctx, hours)
		totalSent += sent
		totalSkipped += skipped
		totalFailed += failed
	}

	log.Infof(ctx, "[ABANDONED] Hourly task completed: sent=%d, skipped=%d, failed=%d",
		totalSent, totalSkipped, totalFailed)
	return nil
}

func handleAbandonedOrderDailyTask(ctx context.Context, _ []byte) error {
	log.Infof(ctx, "[ABANDONED] Starting daily abandoned order check")

	var totalSent, totalSkipped, totalFailed int
	for _, days := range abandonedDailyDelays {
		sent, skipped, failed := processAbandonedDaily(ctx, days)
		totalSent += sent
		totalSkipped += skipped
		totalFailed += failed
	}

	log.Infof(ctx, "[ABANDONED] Daily task completed: sent=%d, skipped=%d, failed=%d",
		totalSent, totalSkipped, totalFailed)
	return nil
}

func processAbandonedHourly(ctx context.Context, hoursAfter int) (sent, skipped, failed int) {
	log.Infof(ctx, "[ABANDONED] Processing %dh reminder", hoursAfter)

	now := time.Now().UTC()
	hourStr := now.Format("2006-01-02-15")

	windowEnd := now.Add(-time.Duration(hoursAfter)*time.Hour + 30*time.Minute)
	windowStart := now.Add(-time.Duration(hoursAfter)*time.Hour - 30*time.Minute)

	batchID := fmt.Sprintf("abandoned:%dh:%s", hoursAfter, hourStr)
	slug := fmt.Sprintf("abandoned-%dh", hoursAfter)

	return processAbandonedOrders(ctx, batchID, slug, windowStart, windowEnd, 0)
}

func processAbandonedDaily(ctx context.Context, daysAfter int) (sent, skipped, failed int) {
	log.Infof(ctx, "[ABANDONED] Processing %dd reminder", daysAfter)

	now := time.Now().UTC()
	todayStr := now.Format("2006-01-02")
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	windowStart := today.AddDate(0, 0, -daysAfter)
	windowEnd := windowStart.AddDate(0, 0, 1)

	batchID := fmt.Sprintf("abandoned:%dd:%s", daysAfter, todayStr)
	slug := fmt.Sprintf("abandoned-%dd", daysAfter)

	return processAbandonedOrders(ctx, batchID, slug, windowStart, windowEnd, daysAfter)
}

func processAbandonedOrders(ctx context.Context, batchID, slug string, windowStart, windowEnd time.Time, daysAfter int) (sent, skipped, failed int) {
	if !templateSlugExists(slug) {
		alertMsg := fmt.Sprintf("[ABANDONED] 模板 slug=%q 不存在，跳过。请在管理后台创建该模板。", slug)
		log.Errorf(ctx, "%s", alertMsg)
		slack.Send("alert", alertMsg)
		return 0, 0, 0
	}

	type abandonedOrderInfo struct {
		UserID    uint64
		Title     string
		PayAmount uint64
	}

	// 查询时间窗口内未支付订单，排除：
	// 1) windowStart 之后已有付款订单的用户
	// 2) 已完成首单的老客户（他们走 renewal-*/winback-* 独立邮件链路，不受 abandoned-* 新客激进折扣打扰）
	// 不使用 GROUP BY（MySQL ONLY_FULL_GROUP_BY 兼容），在 Go 侧按 user_id 去重
	var allOrders []abandonedOrderInfo
	err := db.Get().Model(&Order{}).
		Select("user_id, title, pay_amount").
		Where("is_paid = ? AND created_at >= ? AND created_at < ?", false, windowStart, windowEnd).
		Where("user_id NOT IN (?)",
			db.Get().Model(&Order{}).
				Select("DISTINCT user_id").
				Where("is_paid = ? AND created_at >= ?", true, windowStart),
		).
		Where("user_id NOT IN (?)",
			db.Get().Model(&User{}).
				Select("id").
				Where("is_first_order_done = ?", true),
		).
		Order("created_at DESC").
		Find(&allOrders).Error

	if err != nil {
		log.Errorf(ctx, "[ABANDONED] Failed to query orders: %v", err)
		return 0, 0, 1
	}

	// 按 user_id 去重，保留最新的订单（已按 created_at DESC 排序）
	seen := make(map[uint64]bool, len(allOrders))
	orders := make([]abandonedOrderInfo, 0, len(allOrders))
	for _, o := range allOrders {
		if !seen[o.UserID] {
			seen[o.UserID] = true
			orders = append(orders, o)
		}
	}

	log.Infof(ctx, "[ABANDONED] Found %d users with unpaid orders (window: %s to %s)",
		len(orders), windowStart.Format("2006-01-02 15:04"), windowEnd.Format("2006-01-02 15:04"))

	if len(orders) == 0 {
		return 0, 0, 0
	}

	userIDs := make([]uint64, len(orders))
	for i, o := range orders {
		userIDs[i] = o.UserID
	}

	var users []User
	if err := db.Get().Preload("LoginIdentifies").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		log.Errorf(ctx, "[ABANDONED] Failed to query users: %v", err)
		return 0, 0, 1
	}

	userMap := make(map[uint64]*User, len(users))
	for i := range users {
		userMap[users[i].ID] = &users[i]
	}

	vars := map[string]string{}
	campaign, hasCampaign := abandonedCampaigns[daysAfter]
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

	items := make([]SendEmailItem, 0, len(orders))
	for _, order := range orders {
		user, ok := userMap[order.UserID]
		if !ok {
			skipped++
			continue
		}

		if user.ExpiredAt > 0 && user.ExpiredAt <= time.Now().Unix() {
			skipped++
			continue
		}

		email := getUserEmailFromIdentifies(user)
		if email == "" {
			skipped++
			continue
		}

		itemVars := make(map[string]string, len(vars)+2)
		maps.Copy(itemVars, vars)
		itemVars["PlanTitle"] = order.Title
		itemVars["PayAmount"] = formatCents(order.PayAmount)

		items = append(items, SendEmailItem{
			Email:  email,
			UserID: user.ID,
			Slug:   slug,
			Vars:   itemVars,
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
		log.Errorf(ctx, "[ABANDONED] SendTemplatedEmails failed: %v", err)
		return 0, skipped, len(items)
	}

	return result.Sent, skipped + result.Skipped, result.Failed
}
