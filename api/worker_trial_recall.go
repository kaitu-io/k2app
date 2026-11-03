package center

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
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
// 排除 billing_retry / grace（Apple 仍在尝试扣款，有机会自愈）。
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
			Where("subscriptions.status NOT IN ?", []string{"billing_retry", "grace"})
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

// ===================== 召回邮件 + 每日任务（spec §5.2–5.6）=====================

const (
	TaskTypeTrialRecall = "trial:recall"

	// trialRecallDiscountPct TRIAL80 是 8 折：SavingsText 按 (100-80)% 计算立减。
	trialRecallDiscountPct = 80
	trialRecallValidDays   = "本周内有效"
)

// trialRecallSlugs 各阶段邮件模板 slug（kaitu）。包级 var 仅为测试可替换。
var trialRecallSlugs = map[trialRecallStage]string{
	trialRecallS1: "trial-recall-before",
	trialRecallS2: "trial-recall-after",
}

// trialRecallCampaignCode 召回活动码。包级 var 仅为测试可替换。
var trialRecallCampaignCode = "TRIAL80"

type trialRecallResult struct {
	Candidates, Holdout, AlreadySent, NoEmail, Sent, Skipped, Failed int
	// PushQueued 已入队的推送数；PushErrors 按原因计数（no_token / not_consented / duplicate /
	// channel_unavailable / invalid_route / invalid_request / other）。duplicate 表示此前已入队，不算失败。
	PushQueued int
	PushErrors map[string]int
}

// 推送与告警出口，包级 var 仅为测试可替换。
var (
	trialRecallPushToUser = PushToUser
	trialRecallAlert      = func(msg string) { _ = slack.Send("alert", msg) }
)

// trialRecallPush 给一个候选发推送，返回 false = 通道不可用（本阶段不必再试）。
// 推送与邮件相互独立：不看邮箱、不看邮件是否已发；幂等靠 DedupKey。
// S1 是账户到期提醒（transactional），对照组也发；S2 是召回（marketing），对照组不发。
func trialRecallPush(ctx context.Context, stage trialRecallStage, c trialRecallCandidate, res *trialRecallResult) bool {
	category := PushCategoryTransactional
	if stage == trialRecallS2 {
		if inTrialRecallHoldout(c.User.ID) {
			return true
		}
		category = PushCategoryMarketing
	}
	title, body := trialRecallPushCopy(stage, c.User.Language, time.Unix(c.TrialEndsAt, 0))
	err := trialRecallPushToUser(ctx, c.User.ID, PushRequest{
		Category: category,
		DedupKey: trialRecallDedupKey(stage, c.User.ID),
		Title:    title,
		Body:     body,
		Route:    "/purchase",
	})
	reason := ""
	switch {
	case err == nil:
		res.PushQueued++
		return true
	case errors.Is(err, ErrPushNoActiveToken):
		reason = "no_token"
	case errors.Is(err, ErrPushNotConsented):
		reason = "not_consented"
	case errors.Is(err, ErrPushDuplicate):
		reason = "duplicate"
	case errors.Is(err, ErrPushChannelUnavailable):
		reason = "channel_unavailable"
		log.Warnf(ctx, "[TRIAL-RECALL] stage=%s push channel unavailable (APNs not configured), skipping pushes", stage)
	case errors.Is(err, ErrPushInvalidRoute):
		reason = "invalid_route"
		log.Errorf(ctx, "[TRIAL-RECALL] push user=%d: %v", c.User.ID, err)
	case errors.Is(err, ErrPushInvalidRequest):
		reason = "invalid_request"
		log.Errorf(ctx, "[TRIAL-RECALL] push user=%d: %v", c.User.ID, err)
	default:
		reason = "other"
		log.Errorf(ctx, "[TRIAL-RECALL] push user=%d failed: %v", c.User.ID, err)
	}
	if res.PushErrors == nil {
		res.PushErrors = map[string]int{}
	}
	res.PushErrors[reason]++
	return reason != "channel_unavailable"
}

// handleTrialRecallTask 每日任务：S1 与 S2 各跑一遍。
func handleTrialRecallTask(ctx context.Context, _ []byte) error {
	now := time.Now()
	for _, stage := range []trialRecallStage{trialRecallS1, trialRecallS2} {
		processTrialRecall(ctx, stage, now)
	}
	return nil
}

// trialRecallAlreadySent 返回该阶段已成功发过邮件的用户；failed / skipped 不算，下次照发。
func trialRecallAlreadySent(stage trialRecallStage, userIDs []uint64) (map[uint64]bool, error) {
	out := map[uint64]bool{}
	if len(userIDs) == 0 {
		return out, nil
	}
	var ids []uint64
	err := db.Get().Model(&EmailSendLog{}).
		Where("batch_id LIKE ? AND user_id IN ? AND status = ?",
			fmt.Sprintf("trial-recall:%s:%%", stage), userIDs, string(EmailSendLogStatusSent)).
		Distinct().Pluck("user_id", &ids).Error
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// trialRecallRecorded 返回该阶段已有任意状态日志行的用户（sent / failed / skipped 都算），
// 仅用于决定是否补写 holdout / no_email 的 skipped 记录，绝不参与"是否已发"的判断。
func trialRecallRecorded(stage trialRecallStage, userIDs []uint64) (map[uint64]bool, error) {
	out := map[uint64]bool{}
	if len(userIDs) == 0 {
		return out, nil
	}
	var ids []uint64
	err := db.Get().Model(&EmailSendLog{}).
		Where("batch_id LIKE ? AND user_id IN ?", fmt.Sprintf("trial-recall:%s:%%", stage), userIDs).
		Distinct().Pluck("user_id", &ids).Error
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// trialRecallTemplateID 该阶段模板的 ID（供 skipped 记录关联）；查不到返回 0。
func trialRecallTemplateID(slug string) uint64 {
	var tpl EmailMarketingTemplate
	if err := db.Get().Where("slug = ? AND brand = ? AND is_active = ? AND origin_id IS NULL", slug, string(BrandKaitu), true).
		Order("id ASC").First(&tpl).Error; err != nil {
		return 0
	}
	return tpl.ID
}

// trialRecallLocale 开途官网只有 zh-CN / zh-TW / zh-HK；其余语言一律 zh-CN。
func trialRecallLocale(lang string) string {
	if lang == "zh-TW" || lang == "zh-HK" {
		return lang
	}
	return "zh-CN"
}

func trialRecallPurchaseURL(email, code, planPID string, stage trialRecallStage) string {
	return trialRecallPurchaseURLLocale("zh-CN", email, code, planPID, stage)
}

func trialRecallPurchaseURLLocale(locale, email, code, planPID string, stage trialRecallStage) string {
	q := url.Values{}
	q.Set("campaign", code)
	if planPID != "" {
		q.Set("plan", planPID)
	}
	q.Set("email", email)
	q.Set("utm_source", "email")
	q.Set("utm_medium", "lifecycle")
	q.Set("utm_campaign", "trial-recall-"+string(stage))
	return BrandKaitu.Config().BaseURL + "/" + locale + "/purchase?" + q.Encode()
}

// trialRecallAppPlans 开途在售的 App 套餐（排除 router / private_node）。
func trialRecallAppPlans(ctx context.Context) ([]Plan, error) {
	var plans []Plan
	err := db.Get().WithContext(ctx).Scopes(ScopeBrand(BrandKaitu)).
		Where("is_active = ? AND product = ?", true, ProductApp).
		Order("id ASC").Find(&plans).Error
	return plans, err
}

// trialRecallSavingsText 由在售 App 套餐价格区间算出立减文案；plans 不能为空。
func trialRecallSavingsText(plans []Plan) string {
	minPrice, maxPrice := plans[0].Price, plans[0].Price
	for _, p := range plans {
		if p.Price < minPrice {
			minPrice = p.Price
		}
		if p.Price > maxPrice {
			maxPrice = p.Price
		}
	}
	minSave := minPrice * uint64(100-trialRecallDiscountPct) / 100
	maxSave := maxPrice * uint64(100-trialRecallDiscountPct) / 100
	return fmt.Sprintf("立减 %s 起，最高立减 %s", formatCents(minSave), formatCents(maxSave))
}

// trialRecallEmailVars 计算邮件变量；false = 活动码不可用或没有在售 App 套餐，此时不应发信。
func trialRecallEmailVars(ctx context.Context, stage trialRecallStage, c trialRecallCandidate, email string) (map[string]string, bool) {
	if getCampaignByCode(ctx, trialRecallCampaignCode, BrandKaitu) == nil {
		return nil, false
	}
	plans, err := trialRecallAppPlans(ctx)
	if err != nil {
		log.Errorf(ctx, "[TRIAL-RECALL] query plans failed: %v", err)
		return nil, false
	}
	if len(plans) == 0 {
		return nil, false
	}
	planPID := ""
	for _, p := range plans {
		if p.Month == 12 {
			planPID = p.PID
			break
		}
	}
	vars := map[string]string{
		"CampaignCode": trialRecallCampaignCode,
		"ValidDays":    trialRecallValidDays,
		"SavingsText":  trialRecallSavingsText(plans),
		"PurchaseURL": trialRecallPurchaseURLLocale(trialRecallLocale(c.User.Language), email,
			trialRecallCampaignCode, planPID, stage),
	}
	if stage == trialRecallS1 {
		vars["TrialEndDate"] = trialRecallDateString(c.TrialEndsAt)
	}
	return vars, true
}

// processTrialRecall 处理一个阶段：对照组 / 已发 / 无邮箱之外的候选发邮件。
func processTrialRecall(ctx context.Context, stage trialRecallStage, now time.Time) trialRecallResult {
	var res trialRecallResult
	defer func() {
		log.Infof(ctx, "[TRIAL-RECALL] stage=%s candidates=%d holdout=%d already_sent=%d no_email=%d sent=%d skipped=%d failed=%d push_queued=%d push_errors=%v",
			stage, res.Candidates, res.Holdout, res.AlreadySent, res.NoEmail, res.Sent, res.Skipped, res.Failed, res.PushQueued, res.PushErrors)
		if res.Failed > 0 {
			trialRecallAlert(fmt.Sprintf("[TRIAL-RECALL] 阶段 %s 有 %d 封发送失败（sent=%d，push_queued=%d），请查 email_send_logs 与模板变量。",
				stage, res.Failed, res.Sent, res.PushQueued))
		}
		if n := res.PushErrors["other"]; n > 0 {
			trialRecallAlert(fmt.Sprintf("[TRIAL-RECALL] 阶段 %s 有 %d 条推送入队失败（push_queued=%d，errors=%v），请查 app.log。",
				stage, n, res.PushQueued, res.PushErrors))
		}
	}()

	// 邮件前置条件不满足时只跳过邮件（告警），推送不依赖它们，候选照常加载。
	// 活动码 / 套餐不可用：整个阶段不发邮件，名额不消耗（没有日志行，下次仍是候选）。
	slug := trialRecallSlugs[stage]
	emailReady := true
	if !templateSlugExists(slug) {
		msg := fmt.Sprintf("[TRIAL-RECALL] 模板 slug=%q 不存在，跳过阶段 %s 的邮件。请在管理后台创建该模板。", slug, stage)
		log.Errorf(ctx, "%s", msg)
		trialRecallAlert(msg)
		emailReady = false
	} else if _, ok := trialRecallEmailVars(ctx, stage, trialRecallCandidate{}, ""); !ok {
		msg := fmt.Sprintf("[TRIAL-RECALL] 活动码 %q 不可用或无在售 App 套餐，跳过阶段 %s 的邮件。", trialRecallCampaignCode, stage)
		log.Errorf(ctx, "%s", msg)
		trialRecallAlert(msg)
		emailReady = false
	}

	cands, err := trialRecallCandidates(ctx, stage, now)
	if err != nil {
		log.Errorf(ctx, "[TRIAL-RECALL] query candidates failed: %v", err)
		res.Failed++
		return res
	}
	res.Candidates = len(cands)
	if len(cands) == 0 {
		return res
	}

	// 推送先于邮件：邮件段的任何提前返回都不应让推送丢失。
	pushing := true
	for _, c := range cands {
		if pushing {
			pushing = trialRecallPush(ctx, stage, c, &res)
		}
	}
	if !emailReady {
		for _, c := range cands {
			if inTrialRecallHoldout(c.User.ID) {
				res.Holdout++
			}
		}
		return res
	}

	ids := make([]uint64, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.User.ID)
	}
	sentSet, err := trialRecallAlreadySent(stage, ids)
	if err != nil {
		log.Errorf(ctx, "[TRIAL-RECALL] query sent logs failed: %v", err)
		res.Failed++
		return res
	}

	// 对照组 / 无邮箱的候选不发信，但首次见到时落一条 skipped 记录，事后才能算召回组 vs 对照组转化。
	recorded, err := trialRecallRecorded(stage, ids)
	if err != nil {
		log.Errorf(ctx, "[TRIAL-RECALL] query recorded logs failed: %v", err)
		res.Failed++
		return res
	}
	batchID := fmt.Sprintf("trial-recall:%s:%s", stage, now.UTC().Format("2006-01-02"))
	templateID := trialRecallTemplateID(slug)
	record := func(c trialRecallCandidate, email, reason string) {
		if recorded[c.User.ID] {
			return
		}
		recorded[c.User.ID] = true
		createEmailSendLog(ctx, batchID, templateID, c.User.ID, email, "", EmailSendLogStatusSkipped, reason)
	}

	items := make([]SendEmailItem, 0, len(cands))
	for _, c := range cands {
		email := getUserEmailFromIdentifies(&c.User)
		switch {
		case inTrialRecallHoldout(c.User.ID):
			res.Holdout++
			record(c, email, "holdout")
			continue
		case sentSet[c.User.ID]:
			res.AlreadySent++
			continue
		}
		if email == "" {
			res.NoEmail++
			record(c, "", "no_email")
			continue
		}
		vars, ok := trialRecallEmailVars(ctx, stage, c, email)
		if !ok {
			res.Skipped++
			continue
		}
		items = append(items, SendEmailItem{Email: email, UserID: c.User.ID, Slug: slug, Vars: vars})
	}
	if len(items) == 0 {
		return res
	}

	result, err := SendTemplatedEmails(ctx, &SendEmailsRequest{
		BatchID: batchID,
		Brand:   string(BrandKaitu),
		Items:   items,
	})
	if err != nil {
		log.Errorf(ctx, "[TRIAL-RECALL] SendTemplatedEmails failed: %v", err)
		res.Failed += len(items)
		return res
	}
	res.Sent, res.Skipped, res.Failed = result.Sent, res.Skipped+result.Skipped, res.Failed+result.Failed
	return res
}
