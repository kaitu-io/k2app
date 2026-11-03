package center

import (
	"context"
	"errors"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// =====================================================================
// 自动触发的账户提醒（交易类，spec 2026-11-03-ios-push-design §6.4）
// =====================================================================
//
// 推送失败（没 token / 通道不可用 / 重复）一律只记日志：这些触发点挂在 webhook 与日常 worker 上，
// 推送绝不能让它们失败。试用「到期未转化」的召回不在这里，归 docs/superpowers/specs/2026-11-03-ios-trial-recall-design.md。

// sendTransactionalPush 按用户语言取文案并发送交易类推送。
func sendTransactionalPush(ctx context.Context, userID uint64, kind pushCopyKind, dedupKey, route string) {
	var u User
	if err := db.Get().Select("id", "brand", "language").First(&u, userID).Error; err != nil {
		log.Warnf(ctx, "[PUSH-TRIGGER] %s: load user %d: %v", kind, userID, err)
		return
	}
	title, body := pushCopy(kind, pushCopyLang(u.Language, Brand(u.Brand)))
	err := PushToUser(ctx, userID, PushRequest{
		Category: PushCategoryTransactional, DedupKey: dedupKey, Title: title, Body: body, Route: route,
	})
	switch {
	case err == nil:
		log.Infof(ctx, "[PUSH-TRIGGER] %s queued user=%d key=%s", kind, userID, dedupKey)
	case errors.Is(err, ErrPushNoActiveToken), errors.Is(err, ErrPushDuplicate):
		log.Debugf(ctx, "[PUSH-TRIGGER] %s skipped user=%d: %v", kind, userID, err)
	default:
		log.Warnf(ctx, "[PUSH-TRIGGER] %s user=%d: %v", kind, userID, err)
	}
}

// notifyBillingRetryIfEntered 订阅从正常状态进入扣款失败（grace / billing_retry）时推一次。
// prevStatus 是更新前的快照状态；同一次失败的重投 / reconcile 重放由 DedupKey（订阅 + 当时的周期末）挡住。
// 试用链的第一笔扣款失败也推：用户本来就想转付费，只是付款失败（spec §6.4）。
func notifyBillingRetryIfEntered(ctx context.Context, sub *Subscription, prevStatus, newStatus string) {
	if newStatus != "grace" && newStatus != "billing_retry" {
		return
	}
	if prevStatus == "grace" || prevStatus == "billing_retry" {
		return
	}
	key := fmt.Sprintf("billing_retry:%d:%d", sub.ID, sub.CurrentPeriodEnd)
	sendTransactionalPush(ctx, sub.UserID, pushCopyBillingRetry, key, "/purchase")
}

// lifecyclePushKinds 到期提醒 worker 里哪些触发日发推送（其余日只发邮件）。
// 30/14/7 天离到期太远；-7/-30 是带折扣码的 winback，属营销类，不在这里发。
var lifecyclePushKinds = map[int]struct {
	kind   pushCopyKind
	prefix string
}{
	3:  {pushCopyRenewal3d, "renewal_3"},
	-1: {pushCopyExpired1d, "expired_1"},
}

// processLifecyclePushes 与同一天的提醒邮件用完全相同的候选人：付过费、当天窗口到期 / 过期、
// 没有活跃自动续费。days>0 = 到期前，days<0 = 过期后。
func processLifecyclePushes(ctx context.Context, days int) {
	spec, ok := lifecyclePushKinds[days]
	if !ok {
		return
	}
	now := time.Now().UTC()
	target := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, days)
	users, err := lifecycleEmailCandidates(target, target.AddDate(0, 0, 1))
	if err != nil {
		log.Errorf(ctx, "[PUSH-TRIGGER] lifecycle %d: query candidates: %v", days, err)
		return
	}
	ids := make([]uint64, 0, len(users))
	for i := range users {
		ids = append(ids, users[i].ID)
	}
	autoRenew := usersWithLiveAutoRenew(ids)

	var attempted int
	for _, u := range users {
		if autoRenew[u.ID] {
			continue
		}
		if days < 0 && u.ExpiredAt > now.Unix() {
			continue // 已续上
		}
		attempted++
		key := fmt.Sprintf("%s:%d:%d", spec.prefix, u.ID, u.ExpiredAt)
		sendTransactionalPush(ctx, u.ID, spec.kind, key, "/purchase")
	}
	log.Infof(ctx, "[PUSH-TRIGGER] lifecycle %d: candidates=%d attempted=%d", days, len(users), attempted)
}
