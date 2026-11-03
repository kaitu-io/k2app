package center

import (
	"context"
	"fmt"
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
	// 14 / 30 天两封已停发（spec 2026-11-03-recall-offer-flatten）：4~10 月它们之后 14 天内付款率 1%~2%，
	// 「最后机会」「最大优惠」式的加码只会教用户等。
	abandonedDailyDelays = []int{1, 3, 7}
)

// abandonedOfferStages 带优惠的阶段：3 天第一次给新客统一优惠，7 天再提一次**同一个**码，不加码。
// 1 小时 / 1 天只提醒、不带折扣——历史付款几乎都发生在这两封之后，此时给折扣只是补贴本来就会买的人。
var abandonedOfferStages = map[int]bool{3: true, 7: true}

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

	offerCode := ""
	if abandonedOfferStages[daysAfter] {
		offerCode = recallOfferNewCode
	}
	mailer := newRecallMailer(batchID, slug, offerCode)
	defer func() {
		if mailer.OfferMissing {
			msg := fmt.Sprintf("[ABANDONED] 活动码 %q 不可用或无在售年付 App 套餐，%s 本次跳过了部分收件人。", offerCode, slug)
			log.Errorf(ctx, "%s", msg)
			slack.Send("alert", msg)
		}
	}()

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

		if mailer.Holdout(ctx, user, email) {
			skipped++
			continue
		}
		itemVars, ok := mailer.Vars(ctx, user, email)
		if !ok {
			skipped++
			continue
		}
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
