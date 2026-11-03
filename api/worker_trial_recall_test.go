package center

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/db"
)

// trialRecallSeed 描述一条被测 user + Apple subscription。时间字段都是 unix 秒。
type trialRecallSeed struct {
	expiredAt  int64
	isTrial    bool
	autoRenew  bool
	status     string
	env        string
	brand      string
	firstOrder bool
}

func trialRecallDefaults() trialRecallSeed {
	return trialRecallSeed{status: "active", env: "Production", brand: "kaitu", isTrial: true}
}

var trialRecallSeq int

// seedTrialRecallUser 建 user 和每个 trialEnds 一条 subscription 链；cleanup 自动注册。
func seedTrialRecallUser(t *testing.T, s trialRecallSeed, trialEnds ...int64) (User, []Subscription) {
	t.Helper()
	trialRecallSeq++
	uniq := fmt.Sprintf("%d-%d", time.Now().UnixNano(), trialRecallSeq)
	u := User{UUID: "usr-tr-" + uniq, ExpiredAt: s.expiredAt, IsFirstOrderDone: BoolPtr(s.firstOrder), Brand: s.brand}
	require.NoError(t, db.Get().Create(&u).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&u) })
	var subs []Subscription
	for i, te := range trialEnds {
		sub := Subscription{
			UserID: u.ID, Provider: SubscriptionProviderApple,
			ProviderSubscriptionID: fmt.Sprintf("TR-%s-%d", uniq, i),
			ProductID:              "io.kaitu.sub.basic.1y",
			Environment:            s.env, Status: s.status, AutoRenew: s.autoRenew,
			IsTrial: s.isTrial, TrialEndsAt: te,
		}
		require.NoError(t, db.Get().Create(&sub).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&sub) })
		subs = append(subs, sub)
	}
	return u, subs
}

func trialRecallIDs(t *testing.T, stage trialRecallStage, now time.Time) map[uint64]trialRecallCandidate {
	t.Helper()
	cs, err := trialRecallCandidates(context.Background(), stage, now)
	require.NoError(t, err)
	m := map[uint64]trialRecallCandidate{}
	for _, c := range cs {
		m[c.User.ID] = c
	}
	return m
}

func TestTrialRecallCandidates_S1_SelectsTrialWhoseExpiryEqualsTrialEnd(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt = te
	u, subs := seedTrialRecallUser(t, s, te)
	got := trialRecallIDs(t, trialRecallS1, now)
	require.Contains(t, got, u.ID)
	assert.Equal(t, subs[0].ID, got[u.ID].SubscriptionID)
	assert.Equal(t, te, got[u.ID].TrialEndsAt)
}

func TestTrialRecallCandidates_S1_SkipsAutoRenewOn(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt, s.autoRenew = te, true
	u, _ := seedTrialRecallUser(t, s, te)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS1, now), u.ID)
}

func TestTrialRecallCandidates_S1_SkipsMembershipBeyondTrialByMoreThanADay(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt = te + 2*86400
	u, _ := seedTrialRecallUser(t, s, te)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS1, now), u.ID)
}

func TestTrialRecallCandidates_S2_SelectsStaleActiveStatus(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(-3 * 24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt, s.status, s.isTrial = te, "active", false
	u, _ := seedTrialRecallUser(t, s, te)
	assert.Contains(t, trialRecallIDs(t, trialRecallS2, now), u.ID)
}

func TestTrialRecallCandidates_S2_SkipsBillingRetry(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(-3 * 24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt, s.status = te, "billing_retry"
	u, _ := seedTrialRecallUser(t, s, te)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS2, now), u.ID)
}

func TestTrialRecallCandidates_S2_SkipsActiveMembership(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(-3 * 24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt = now.Add(5 * 24 * time.Hour).Unix()
	u, _ := seedTrialRecallUser(t, s, te)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS2, now), u.ID)
}

func TestTrialRecallCandidates_SkipsPaidBefore(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te1 := now.Add(24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt, s.firstOrder = te1, true
	u1, _ := seedTrialRecallUser(t, s, te1)
	te2 := now.Add(-3 * 24 * time.Hour).Unix()
	s.expiredAt = te2
	u2, _ := seedTrialRecallUser(t, s, te2)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS1, now), u1.ID)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS2, now), u2.ID)
}

func TestTrialRecallCandidates_SkipsOverleapAndSandbox(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt, s.brand = te, "overleap"
	ov, _ := seedTrialRecallUser(t, s, te)
	s.brand, s.env = "kaitu", "Sandbox"
	sb, _ := seedTrialRecallUser(t, s, te)
	got := trialRecallIDs(t, trialRecallS1, now)
	assert.NotContains(t, got, ov.ID)
	assert.NotContains(t, got, sb.ID)
}

func TestTrialRecallCandidates_TwoChainsOneUser(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	older := now.Add(-5 * 24 * time.Hour).Unix()
	newer := now.Add(-3 * 24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt = newer
	u, subs := seedTrialRecallUser(t, s, older, newer)
	cs, err := trialRecallCandidates(context.Background(), trialRecallS2, now)
	require.NoError(t, err)
	n := 0
	for _, c := range cs {
		if c.User.ID == u.ID {
			n++
			assert.Equal(t, newer, c.TrialEndsAt)
			assert.Equal(t, subs[1].ID, c.SubscriptionID)
		}
	}
	assert.Equal(t, 1, n)
}

func TestTrialRecallCandidates_WindowEdges(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	s := trialRecallDefaults()
	mk := func(te int64) User {
		s.expiredAt = te
		u, _ := seedTrialRecallUser(t, s, te)
		return u
	}
	in47 := mk(now.Add(47 * time.Hour).Unix())
	out49 := mk(now.Add(49 * time.Hour).Unix())
	s1 := trialRecallIDs(t, trialRecallS1, now)
	assert.Contains(t, s1, in47.ID)
	assert.NotContains(t, s1, out49.ID)

	in := mk(now.Add(-24*time.Hour - time.Second).Unix())
	out := mk(now.Add(-10*24*time.Hour - time.Second).Unix())
	s2 := trialRecallIDs(t, trialRecallS2, now)
	assert.Contains(t, s2, in.ID)
	assert.NotContains(t, s2, out.ID)
}

func TestInTrialRecallHoldout_StableAndAboutTenPercent(t *testing.T) {
	hits := 0
	for id := uint64(1); id <= 10000; id++ {
		a := inTrialRecallHoldout(id)
		require.Equal(t, a, inTrialRecallHoldout(id))
		if a {
			hits++
		}
	}
	assert.GreaterOrEqual(t, hits, 800)
	assert.LessOrEqual(t, hits, 1200)
}

func TestTrialRecallDedupKey(t *testing.T) {
	assert.Equal(t, "trial-recall:s2:42", trialRecallDedupKey(trialRecallS2, 42))
	assert.Equal(t, "trial-recall:s1:123", trialRecallDedupKey(trialRecallS1, 123))
}

// ===================== Task 2：召回邮件 + 每日任务 =====================

// trialRecallFarNow 远未来的固定"当前时刻"：候选窗口只看传入的 now，
// 这样共享 dev 库里的真实数据不会混进断言；活动码有效期仍按真实时钟，所以活动码按真实时间建。
var trialRecallFarNow = time.Date(2090, 1, 1, 12, 0, 0, 0, time.UTC)

// setupTrialRecallEnv 配好 dev_mode、测试专用 slug / 活动码 / 套餐 / 模板，返回活动码。
func setupTrialRecallEnv(t *testing.T, withCampaign bool) string {
	t.Helper()
	skipIfNoConfig(t)
	uniq := fmt.Sprintf("%d", time.Now().UnixNano())

	prevDev := viper.Get("mail.dev_mode")
	viper.Set("mail.dev_mode", true) // 绝不真发信
	t.Cleanup(func() { viper.Set("mail.dev_mode", prevDev) })

	prevSlugs, prevCode := trialRecallSlugs, trialRecallCampaignCode
	trialRecallSlugs = map[trialRecallStage]string{
		trialRecallS1: "trt-before-" + uniq,
		trialRecallS2: "trt-after-" + uniq,
	}
	trialRecallCampaignCode = "TRT" + uniq[len(uniq)-12:]
	t.Cleanup(func() { trialRecallSlugs, trialRecallCampaignCode = prevSlugs, prevCode })

	active := true
	for _, st := range []trialRecallStage{trialRecallS1, trialRecallS2} {
		tpl := &EmailMarketingTemplate{
			Name: "trt-" + string(st) + "-" + uniq, Slug: trialRecallSlugs[st], Language: "zh-CN",
			Subject: "试用召回 {{.CampaignCode}}", Brand: string(BrandKaitu), IsActive: &active,
			Content: "<p>{{.CampaignCode}} {{.ValidDays}} {{.SavingsText}} {{.PurchaseURL}}</p>",
		}
		if st == trialRecallS1 {
			tpl.Content += "<p>{{.TrialEndDate}}</p>"
		}
		require.NoError(t, db.Get().Create(tpl).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(tpl) })
	}

	plan := &Plan{PID: "trt" + uniq[len(uniq)-12:], Label: "trt-plan", Price: 3900, OriginPrice: 3900,
		Month: 12, IsActive: &active, Product: ProductApp, Brand: string(BrandKaitu)}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(plan) })

	if withCampaign {
		createTrialRecallCampaign(t, trialRecallCampaignCode)
	}
	return trialRecallCampaignCode
}

func createTrialRecallCampaign(t *testing.T, code string) {
	t.Helper()
	c := &Campaign{Code: code, Name: "trt", Type: CampaignTypeDiscount, Value: 80,
		StartAt: time.Now().Add(-time.Hour).Unix(), EndAt: time.Now().Add(48 * time.Hour).Unix(),
		IsActive: BoolPtr(true), MatcherType: "all", Brand: string(BrandKaitu)}
	require.NoError(t, db.Get().Create(c).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(c) })
}

// seedTrialRecallRecipient 建一个带邮箱的 S1 候选用户；反复建直到 holdout 命中符合 wantHoldout。
func seedTrialRecallRecipient(t *testing.T, wantHoldout bool, trialEnd int64) (User, string) {
	t.Helper()
	for i := 0; i < 400; i++ {
		trialRecallSeq++
		uniq := fmt.Sprintf("%d-%d", time.Now().UnixNano(), trialRecallSeq)
		u := User{UUID: "usr-trr-" + uniq, ExpiredAt: trialEnd, IsFirstOrderDone: BoolPtr(false), Brand: "kaitu", Language: "zh-CN"}
		require.NoError(t, db.Get().Create(&u).Error)
		if inTrialRecallHoldout(u.ID) != wantHoldout {
			db.Get().Unscoped().Delete(&u)
			continue
		}
		t.Cleanup(func() {
			db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&EmailSendLog{})
			db.Get().Unscoped().Delete(&u)
		})
		email := "trr-" + uniq + "@example.com"
		li := LoginIdentify{UserID: u.ID, Type: "email", IndexID: email + "-idx", EncryptedValue: email}
		require.NoError(t, db.Get().Create(&li).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&li) })
		sub := Subscription{UserID: u.ID, Provider: SubscriptionProviderApple,
			ProviderSubscriptionID: "TRR-" + uniq, ProductID: "io.kaitu.sub.basic.1y",
			Environment: "Production", Status: "active", IsTrial: true, TrialEndsAt: trialEnd}
		require.NoError(t, db.Get().Create(&sub).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&sub) })
		return u, email
	}
	t.Fatal("could not seed a user with the wanted holdout bucket")
	return User{}, ""
}

func trialRecallLogs(t *testing.T, userID uint64) []EmailSendLog {
	t.Helper()
	var logs []EmailSendLog
	require.NoError(t, db.Get().Where("user_id = ?", userID).Find(&logs).Error)
	return logs
}

func TestProcessTrialRecall_SendsOncePerUserAcrossDays(t *testing.T) {
	setupTrialRecallEnv(t, true)
	now := trialRecallFarNow
	u, _ := seedTrialRecallRecipient(t, false, now.Add(40*time.Hour).Unix())

	r1 := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, 1, r1.Candidates, "%+v", r1)
	assert.Equal(t, 1, r1.Sent, "%+v", r1)

	r2 := processTrialRecall(context.Background(), trialRecallS1, now.Add(24*time.Hour))
	assert.Equal(t, 1, r2.Candidates, "%+v", r2)
	assert.Equal(t, 1, r2.AlreadySent, "%+v", r2)
	assert.Equal(t, 0, r2.Sent, "%+v", r2)

	var n int64
	require.NoError(t, db.Get().Model(&EmailSendLog{}).Where("user_id = ? AND status = ?", u.ID, "sent").Count(&n).Error)
	assert.Equal(t, int64(1), n)
}

func TestProcessTrialRecall_FailedIsRetried(t *testing.T) {
	setupTrialRecallEnv(t, true)
	now := trialRecallFarNow
	u, email := seedTrialRecallRecipient(t, false, now.Add(40*time.Hour).Unix())

	prev := &EmailSendLog{BatchID: "trial-recall:s1:2089-12-31", TemplateID: 0, UserID: u.ID, Email: email,
		Language: "zh-CN", Status: EmailSendLogStatusFailed}
	require.NoError(t, db.Get().Create(prev).Error)

	got, err := trialRecallAlreadySent(trialRecallS1, []uint64{u.ID})
	require.NoError(t, err)
	assert.False(t, got[u.ID], "failed 日志不算已发")

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, 1, r.Sent, "%+v", r)
	assert.Equal(t, 0, r.AlreadySent)
}

func TestProcessTrialRecall_HoldoutNotEmailed(t *testing.T) {
	setupTrialRecallEnv(t, true)
	now := trialRecallFarNow
	u, _ := seedTrialRecallRecipient(t, true, now.Add(40*time.Hour).Unix())

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, 1, r.Holdout, "%+v", r)
	assert.Equal(t, 0, r.Sent, "%+v", r)
	assert.Empty(t, trialRecallLogs(t, u.ID))
}

func TestProcessTrialRecall_MissingCampaignSendsNothing(t *testing.T) {
	setupTrialRecallEnv(t, false)
	now := trialRecallFarNow
	u, _ := seedTrialRecallRecipient(t, false, now.Add(40*time.Hour).Unix())

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, 0, r.Sent, "%+v", r)
	assert.Empty(t, trialRecallLogs(t, u.ID), "活动码不可用时不发任何邮件")

	// 补建活动码后再跑：名额没有被消耗，仍然会发。
	createTrialRecallCampaign(t, trialRecallCampaignCode)
	r = processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, 1, r.Sent, "%+v", r)
}

func TestProcessTrialRecall_MissingTemplateSendsNothing(t *testing.T) {
	setupTrialRecallEnv(t, true)
	trialRecallSlugs = map[trialRecallStage]string{trialRecallS1: fmt.Sprintf("trt-no-such-template-%d", time.Now().UnixNano())}
	now := trialRecallFarNow
	u, _ := seedTrialRecallRecipient(t, false, now.Add(40*time.Hour).Unix())

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, trialRecallResult{}, r)
	assert.Empty(t, trialRecallLogs(t, u.ID))
}

func TestTrialRecallEmailVars_SavingsIgnoresRouterPlans(t *testing.T) {
	code := setupTrialRecallEnv(t, true)
	active := true
	router := &Plan{PID: "trtr" + code[len(code)-10:], Label: "trt-router", Price: 35900, OriginPrice: 35900,
		Month: 12, IsActive: &active, Product: ProductRouter, Brand: string(BrandKaitu)}
	require.NoError(t, db.Get().Create(router).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(router) })

	c := trialRecallCandidate{User: User{Language: "zh-CN"}, TrialEndsAt: trialRecallFarNow.Add(40 * time.Hour).Unix()}
	vars, ok := trialRecallEmailVars(context.Background(), trialRecallS1, c, "a@example.com")
	require.True(t, ok)
	assert.NotContains(t, vars["SavingsText"], "$71.8", "router 套餐不进区间")
	assert.Equal(t, code, vars["CampaignCode"])
	assert.Equal(t, "本周内有效", vars["ValidDays"])
	assert.Equal(t, "2090-01-03", vars["TrialEndDate"], "Asia/Shanghai 日期")
	assert.Contains(t, vars["PurchaseURL"], "campaign="+code)
}

func TestTrialRecallPurchaseURL(t *testing.T) {
	u := trialRecallPurchaseURL("a+b@example.com", "TRIAL80", "pid12", trialRecallS1)
	assert.True(t, strings.HasPrefix(u, BrandKaitu.Config().BaseURL+"/zh-CN/purchase?"), u)
	assert.Contains(t, u, "campaign=TRIAL80")
	assert.Contains(t, u, "plan=pid12")
	assert.Contains(t, u, "email=a%2Bb%40example.com")
	assert.Contains(t, u, "utm_source=email")
	assert.Contains(t, u, "utm_medium=lifecycle")
	assert.Contains(t, u, "utm_campaign=trial-recall-s1")

	s2 := trialRecallPurchaseURL("a@example.com", "TRIAL80", "", trialRecallS2)
	assert.NotContains(t, s2, "plan=")
	assert.Contains(t, s2, "utm_campaign=trial-recall-s2")
}

func TestTrialRecallLocale(t *testing.T) {
	assert.Equal(t, "zh-TW", trialRecallLocale("zh-TW"))
	assert.Equal(t, "zh-HK", trialRecallLocale("zh-HK"))
	assert.Equal(t, "zh-CN", trialRecallLocale("en-US"))
	assert.Equal(t, "zh-CN", trialRecallLocale(""))
}

func TestTrialRecallTask_Wired(t *testing.T) {
	src, err := os.ReadFile("worker_integration.go")
	require.NoError(t, err)
	s := string(src)
	assert.Contains(t, s, "asynq.Handle(TaskTypeTrialRecall, handleTrialRecallTask)")
	assert.Regexp(t, `asynq\.Cron\("0 12 \* \* \*", TaskTypeTrialRecall,`, s)
	assert.Equal(t, "trial:recall", TaskTypeTrialRecall)
}
