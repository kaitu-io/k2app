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
			// 专属节点订阅独立时钟（PrivateNodeSubscription.ExpiresAt），
			// 与共享池 User.ExpiredAt 解耦，需并行提醒。仅到期前（days>0）。
			pnSent, pnSkipped, pnFailed := processPrivateNodeRenewalReminders(ctx, days)
			sent += pnSent
			skipped += pnSkipped
			failed += pnFailed
		} else {
			sent, skipped, failed = processWinback(ctx, -days)
		}
		totalSent += sent
		totalSkipped += skipped
		totalFailed += failed

		// iOS 推送与邮件并列，不受邮件模板缺失影响（spec 2026-11-03-ios-push-design §6.4）。
		processLifecyclePushes(ctx, days)
	}

	log.Infof(ctx, "[EMAIL-LIFECYCLE] Daily task completed: sent=%d, skipped=%d, failed=%d",
		totalSent, totalSkipped, totalFailed)

	return nil
}

// =====================================================================
// 续费提醒（到期前）
// =====================================================================

// lifecycleEmailCandidates 续费提醒 / 过期召回共用的候选查询：expired_at 落在 [from, to) 且
// 付过费（is_first_order_done）。从没付过费的用户——包括 Apple 试用到期未转付费的——
// 不在其中：给他们发"续费"语义错误（spec 2026-10-19 §3.5，TestRenewalReminder_ExcludesNeverPaid 守着）。
func lifecycleEmailCandidates(from, to time.Time) ([]User, error) {
	var users []User
	err := db.Get().Model(&User{}).
		Preload("LoginIdentifies").
		Where("expired_at >= ? AND expired_at < ?", from.Unix(), to.Unix()).
		Where("is_first_order_done = ?", true).
		Find(&users).Error
	return users, err
}

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

	users, err := lifecycleEmailCandidates(targetDate, targetDateEnd)

	if err != nil {
		log.Errorf(ctx, "[RENEWAL] Failed to query users: %v", err)
		return 0, 0, 1
	}

	log.Infof(ctx, "[RENEWAL] Found %d users expiring in %d days", len(users), daysBefore)

	// 活跃自动续订用户（apple/stripe）卡会自动扣款——跳过"手动续费"提醒。
	userIDs := make([]uint64, 0, len(users))
	for i := range users {
		userIDs = append(userIDs, users[i].ID)
	}
	autoRenewSet := usersWithLiveAutoRenew(userIDs)

	items := make([]SendEmailItem, 0, len(users))
	for _, user := range users {
		if autoRenewSet[user.ID] {
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
// 专属节点续费提醒（到期前，独立时钟）
// =====================================================================

// processPrivateNodeRenewalReminders 处理专属节点订阅的续费提醒。
// 与共享池 processRenewalReminders 平行：用同一 UTC 当日窗口
// [dayStart, dayEnd) 命中 now+daysBefore 到期的订阅，但查 PrivateNodeSubscription
// （独立时钟 ExpiresAt，不碰 User.ExpiredAt），从主人 LoginIdentifies 解析邮箱，
// 经 SendTemplatedEmails 用模板 slug private-node-renewal-{N}d 发送。
func processPrivateNodeRenewalReminders(ctx context.Context, daysBefore int) (sent, skipped, failed int) {
	log.Infof(ctx, "[PN-RENEWAL] Processing %d-day private-node reminders", daysBefore)

	now := time.Now().UTC()
	todayStr := now.Format("2006-01-02")
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, daysBefore)
	dayEnd := dayStart.AddDate(0, 0, 1)

	batchID := fmt.Sprintf("pn-renewal-%dd-%s", daysBefore, todayStr)
	slug := fmt.Sprintf("private-node-renewal-%dd", daysBefore)

	// 模板缺失时降级跳过（不报 failed），与共享路径同语义：运营侧未配模板不应让 cron
	// 视作发送失败。早返：在任何 DB 扫描 + N 次 user preload 之前守卫，避免模板尚未
	// provision 时每日 cron 全表扫订阅 + 逐订阅查 user。模板内容由运营侧 provision。
	if !templateSlugExists(slug) {
		alertMsg := fmt.Sprintf("[EMAIL-LIFECYCLE] 模板 slug=%q 不存在，跳过 %d 天专属节点续费提醒。请在管理后台创建该模板。", slug, daysBefore)
		log.Errorf(ctx, "%s", alertMsg)
		slack.Send("alert", alertMsg)
		return 0, 0, 0
	}

	var subs []PrivateNodeSubscription
	err := db.Get().Model(&PrivateNodeSubscription{}).
		Where("status IN ?", []string{PNStatusActive, PNStatusGrace}).
		Where("expires_at >= ? AND expires_at < ?", dayStart.Unix(), dayEnd.Unix()).
		Find(&subs).Error
	if err != nil {
		log.Errorf(ctx, "[PN-RENEWAL] Failed to query subscriptions: %v", err)
		return 0, 0, 1
	}

	log.Infof(ctx, "[PN-RENEWAL] Found %d private-node subs expiring in %d days", len(subs), daysBefore)

	if len(subs) == 0 {
		return 0, 0, 0
	}

	items := make([]SendEmailItem, 0, len(subs))
	for _, sub := range subs {
		var user User
		if err := db.Get().Model(&User{}).
			Preload("LoginIdentifies").
			First(&user, sub.UserID).Error; err != nil {
			log.Warnf(ctx, "[PN-RENEWAL] Failed to load user %d for sub %d: %v", sub.UserID, sub.ID, err)
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
		log.Errorf(ctx, "[PN-RENEWAL] SendTemplatedEmails failed: %v", err)
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

	users, err := lifecycleEmailCandidates(targetDate, targetDateEnd)

	if err != nil {
		log.Errorf(ctx, "[WINBACK] Failed to query users: %v", err)
		return 0, 0, 1
	}

	log.Infof(ctx, "[WINBACK] Found %d users expired %d days ago", len(users), daysAfter)

	// 优惠变量按收件用户品牌计算（活动码与套餐均品牌隔离），recallMailer 内按品牌缓存。
	offerCode := ""
	if winbackOfferStages[daysAfter] {
		offerCode = recallOfferBackCode
	}
	mailer := newRecallMailer(batchID, slug, offerCode)
	defer func() {
		if mailer.OfferMissing {
			msg := fmt.Sprintf("[WINBACK] 活动码 %q 不可用或无在售年付 App 套餐，%s 本次跳过了部分收件人。", offerCode, slug)
			log.Errorf(ctx, "%s", msg)
			slack.Send("alert", msg)
		}
	}()

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

		if mailer.Holdout(ctx, &user, email) {
			skipped++
			continue
		}
		vars, ok := mailer.Vars(ctx, &user, email)
		if !ok {
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

// winbackOfferStages 过期召回带优惠的阶段：7 天、30 天给**同一个**老客优惠（BACK10），
// 不再 9 折 → 85 折逐步加深（spec 2026-11-03-recall-offer-flatten）。1 天只提醒「连接已断开」，不带折扣。
var winbackOfferStages = map[int]bool{7: true, 30: true}

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

// usersWithLiveAutoRenew 返回给定用户集中"当前存在活跃自动续订订阅（apple/stripe）"的
// user_id 集合。这些用户的卡会自动扣款，不应收到"请手动续费"提醒邮件。
// 查询失败容错返回空集——宁可多发提醒，不可漏发。
func usersWithLiveAutoRenew(userIDs []uint64) map[uint64]bool {
	out := map[uint64]bool{}
	if len(userIDs) == 0 {
		return out
	}
	var subs []Subscription
	if err := db.Get().
		Where("user_id IN ? AND auto_renew = ? AND status IN ?", userIDs, true, activeSubStatuses).
		Find(&subs).Error; err != nil {
		return out
	}
	now := time.Now().Unix()
	for i := range subs {
		if isSubscriptionLive(&subs[i], now) {
			out[subs[i].UserID] = true
		}
	}
	return out
}
