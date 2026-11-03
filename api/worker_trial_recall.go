package center

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"time"

	"github.com/wordgate/qtoolkit/db"
)

// 开途 iOS 试用未转化召回——候选判定与对照组（spec 2026-11-03-ios-trial-recall-design §5.1）。

type trialRecallStage string

const (
	// trialRecallS1 试用结束前 48h 内、已关自动续订的用户。
	trialRecallS1 trialRecallStage = "s1"
	// trialRecallS2 试用结束后 1~10 天、会员已过期的用户。
	trialRecallS2 trialRecallStage = "s2"
)

type trialRecallCandidate struct {
	User           User // 预加载 LoginIdentifies
	SubscriptionID uint64
	TrialEndsAt    int64
}

// trialRecallCandidates 返回指定阶段的候选；同一用户多条订阅链只保留 trial_ends_at 最新的一条。
//
// S1 不能用 "expired_at > now" 判断：Apple 试用分支会把 user.expired_at 推到试用结束时刻，
// 所以用 expired_at <= trial_ends_at + 1 天 表示"没有别的付费权益盖过试用"。
// S2 不要求 is_trial：试用结束后该标记可能已被清掉；也不看 subscription.status（可能滞留 active），
// 只排除 billing_retry（扣款重试中，仍有机会自愈）。
func trialRecallCandidates(ctx context.Context, stage trialRecallStage, now time.Time) ([]trialRecallCandidate, error) {
	q := db.Get().WithContext(ctx).Model(&Subscription{}).
		Joins("JOIN users ON users.id = subscriptions.user_id").
		Where("users.brand = ?", string(BrandKaitu)).
		Where("users.is_first_order_done IS NULL OR users.is_first_order_done = ?", false).
		Where("subscriptions.provider = ? AND subscriptions.environment = ?", SubscriptionProviderApple, "Production").
		Where("subscriptions.trial_ends_at > ?", 0)

	switch stage {
	case trialRecallS1:
		q = q.Where("subscriptions.is_trial = ? AND subscriptions.auto_renew = ?", true, false).
			Where("subscriptions.trial_ends_at >= ? AND subscriptions.trial_ends_at < ?",
				now.Unix(), now.Add(48*time.Hour).Unix()).
			Where("users.expired_at <= subscriptions.trial_ends_at + ?", 86400)
	case trialRecallS2:
		q = q.Where("subscriptions.trial_ends_at >= ? AND subscriptions.trial_ends_at < ?",
			now.Add(-10*24*time.Hour).Unix(), now.Add(-24*time.Hour).Unix()).
			Where("users.expired_at <= ?", now.Unix()).
			Where("subscriptions.status <> ?", "billing_retry")
	default:
		return nil, fmt.Errorf("unknown trial recall stage %q", stage)
	}

	var subs []Subscription
	if err := q.Order("subscriptions.trial_ends_at DESC, subscriptions.id DESC").Find(&subs).Error; err != nil {
		return nil, err
	}

	// 按 trial_ends_at 倒序，首次出现即该用户最新的一条链。
	seen := map[uint64]bool{}
	var picked []Subscription
	var userIDs []uint64
	for _, s := range subs {
		if seen[s.UserID] {
			continue
		}
		seen[s.UserID] = true
		picked = append(picked, s)
		userIDs = append(userIDs, s.UserID)
	}
	if len(picked) == 0 {
		return nil, nil
	}

	var users []User
	if err := db.Get().WithContext(ctx).Preload("LoginIdentifies").
		Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint64]User, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}

	out := make([]trialRecallCandidate, 0, len(picked))
	for _, s := range picked {
		u, ok := byID[s.UserID]
		if !ok {
			continue
		}
		out = append(out, trialRecallCandidate{User: u, SubscriptionID: s.ID, TrialEndsAt: s.TrialEndsAt})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].User.ID < out[j].User.ID })
	return out, nil
}

// inTrialRecallHoldout 10% 对照组：命中者不发邮件、不发营销推送（S1 交易类推送照发）。
// 盐 "trial-recall-v1:" 固定，改盐会让对照组换人、破坏对照。
func inTrialRecallHoldout(userID uint64) bool {
	h := fnv.New32a()
	_, _ = h.Write([]byte("trial-recall-v1:" + strconv.FormatUint(userID, 10)))
	return h.Sum32()%10 == 0
}

// trialRecallDedupKey 幂等键，如 "trial-recall:s1:123"。
func trialRecallDedupKey(stage trialRecallStage, userID uint64) string {
	return fmt.Sprintf("trial-recall:%s:%d", stage, userID)
}
