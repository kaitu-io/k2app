package center

import (
	"context"
	"fmt"
	"strconv"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
	"gorm.io/gorm"
)

// billingIssueSlug 扣费失败提醒邮件模板 slug（服务通知，退订豁免）。
const billingIssueSlug = "billing-issue"

// billingIssueReminderDays 扣费失败后第几天提醒。0 天只发邮件（推送由事件驱动的
// notifyBillingRetryIfEntered 负责），3 / 7 天邮件 + 推送。
var billingIssueReminderDays = []int{0, 3, 7}

// isBillingIssueStatus 订阅处于扣费失败状态（宽限期 / 计费重试期）。
func isBillingIssueStatus(status string) bool {
	return status == "grace" || status == "billing_retry"
}

// backfillBillingIssueSince 给上线前已在 grace/billing_retry 却没有 billing_issue_since 的
// Apple 订阅补上时间戳（只补 0，幂等）。返回回填行数。
func backfillBillingIssueSince(ctx context.Context, database *gorm.DB, now int64) (int64, error) {
	res := database.WithContext(ctx).Model(&Subscription{}).
		Where("provider = ? AND status IN ? AND billing_issue_since = ?",
			SubscriptionProviderApple, []string{"grace", "billing_retry"}, 0).
		Update("billing_issue_since", now)
	return res.RowsAffected, res.Error
}

// billingIssueCandidates 返回扣费失败已满 day 天（不满 day+1 天）的 Apple 正式订阅：
// billing_issue_since ∈ (now-(day+1)d, now-day d]。已恢复 / 过期 / 撤销的状态不在 IN 里。
func billingIssueCandidates(day int, now time.Time) ([]Subscription, error) {
	upper := now.Unix() - int64(day)*86400
	lower := now.Unix() - int64(day+1)*86400
	var subs []Subscription
	err := db.Get().Model(&Subscription{}).
		Where("provider = ? AND environment = ? AND status IN ?",
			SubscriptionProviderApple, "Production", []string{"grace", "billing_retry"}).
		Where("billing_issue_since > ? AND billing_issue_since <= ?", lower, upper).
		Order("id").Find(&subs).Error
	return subs, err
}

// processBillingIssueReminders 每日 cron 末尾调用：对第 0 / 3 / 7 天的扣费失败订阅发邮件，
// 3 / 7 天另推一条交易类推送。推送 / 邮件失败只记日志，互不阻塞。
func processBillingIssueReminders(ctx context.Context, now time.Time) (sent, skipped, failed int) {
	for _, d := range billingIssueReminderDays {
		subs, err := billingIssueCandidates(d, now)
		if err != nil {
			log.Errorf(ctx, "[BILLING-ISSUE] day=%d query candidates: %v", d, err)
			failed++
			continue
		}
		var dSent, dSkipped, dFailed int
		if d == 3 || d == 7 {
			pushed := map[uint64]bool{} // 每用户每天最多推一条（多订阅不重复打扰）
			for i := range subs {
				sub := &subs[i]
				if pushed[sub.UserID] {
					continue
				}
				pushed[sub.UserID] = true
				sendTransactionalPush(ctx, sub.UserID, pushCopyBillingRetry,
					fmt.Sprintf("billing_issue_%d:%d:%d", d, sub.ID, sub.BillingIssueSince), "/purchase")
			}
		}
		if len(subs) > 0 {
			dSent, dSkipped, dFailed = sendBillingIssueEmails(ctx, d, now, subs)
		}
		log.Infof(ctx, "[BILLING-ISSUE] day=%d candidates=%d email sent=%d skipped=%d failed=%d",
			d, len(subs), dSent, dSkipped, dFailed)
		sent += dSent
		skipped += dSkipped
		failed += dFailed
	}
	return sent, skipped, failed
}

func sendBillingIssueEmails(ctx context.Context, day int, now time.Time, subs []Subscription) (sent, skipped, failed int) {
	if !templateSlugExists(billingIssueSlug) {
		msg := fmt.Sprintf("[BILLING-ISSUE] 模板 slug=%q 不存在，跳过第 %d 天扣费失败邮件。请在管理后台创建该模板。", billingIssueSlug, day)
		log.Errorf(ctx, "%s", msg)
		slack.Send("alert", msg)
		return 0, 0, 0
	}
	seen := map[uint64]bool{}
	items := make([]SendEmailItem, 0, len(subs))
	for i := range subs {
		uid := subs[i].UserID
		if seen[uid] {
			continue
		}
		seen[uid] = true
		var user User
		if err := db.Get().Model(&User{}).Preload("LoginIdentifies").First(&user, uid).Error; err != nil {
			log.Warnf(ctx, "[BILLING-ISSUE] load user %d: %v", uid, err)
			skipped++
			continue
		}
		email := getUserEmailFromIdentifies(&user)
		if email == "" {
			skipped++
			continue
		}
		items = append(items, SendEmailItem{
			Email: email, UserID: uid, Slug: billingIssueSlug,
			Vars: map[string]string{"day": strconv.Itoa(day)},
		})
	}
	if len(items) == 0 {
		return 0, skipped, 0
	}
	result, err := SendTemplatedEmails(ctx, &SendEmailsRequest{
		BatchID: fmt.Sprintf("billing-issue:%dd:%s", day, now.UTC().Format("2006-01-02")),
		Items:   items,
	})
	if err != nil {
		log.Errorf(ctx, "[BILLING-ISSUE] SendTemplatedEmails day=%d: %v", day, err)
		return 0, skipped, len(items)
	}
	return result.Sent, skipped + result.Skipped, result.Failed
}
