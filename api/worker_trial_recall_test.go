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

func TestTrialRecallCandidates_S2_SkipsGrace(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(-3 * 24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt, s.status = te, "grace"
	u, _ := seedTrialRecallUser(t, s, te)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS2, now), u.ID, "Apple 仍在扣款宽限期内")
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

// 北京时间锚点：trialRecallFarNow = 2090-01-01 20:00 CST。
func trialRecallCST(day, hour, min int) time.Time {
	return time.Date(2090, 1, day, hour, min, 0, 0, trialRecallLocation())
}

func TestTrialRecallSendAt_PerUserClockWithQuietHours(t *testing.T) {
	cases := []struct {
		name  string
		stage trialRecallStage
		ends  time.Time
		want  time.Time
	}{
		{"S1 白天结束：前一天同一钟点", trialRecallS1, trialRecallCST(3, 14, 37), trialRecallCST(2, 14, 37)},
		{"S1 08:00 正好不静默", trialRecallS1, trialRecallCST(3, 8, 0), trialRecallCST(2, 8, 0)},
		{"S1 21:59 不静默", trialRecallS1, trialRecallCST(3, 21, 59), trialRecallCST(2, 21, 59)},
		{"S1 22:00 起静默：当晚 22:00", trialRecallS1, trialRecallCST(3, 23, 30), trialRecallCST(2, 22, 0)},
		{"S1 凌晨结束：提前到再前一晚 22:00", trialRecallS1, trialRecallCST(3, 1, 0), trialRecallCST(1, 22, 0)},
		{"S1 07:59 也算凌晨", trialRecallS1, trialRecallCST(3, 7, 59), trialRecallCST(1, 22, 0)},
		{"S2 白天结束：后一天同一钟点", trialRecallS2, trialRecallCST(3, 14, 37), trialRecallCST(4, 14, 37)},
		{"S2 凌晨结束：结束当晚 22:00", trialRecallS2, trialRecallCST(3, 1, 0), trialRecallCST(3, 22, 0)},
		{"S2 深夜结束：次日 22:00", trialRecallS2, trialRecallCST(3, 23, 30), trialRecallCST(4, 22, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := trialRecallSendAt(c.stage, c.ends.Unix())
			assert.True(t, got.Equal(c.want), "got %s want %s", got, c.want)
			h := got.In(trialRecallLocation()).Hour()
			assert.True(t, h >= trialRecallQuietUntil && h <= trialRecallQuietFrom, "发送时刻必须在 08:00~22:00，got %s", got)
		})
	}
	// SQL 预筛的宽度必须盖住静默提前后的极端值（凌晨 07:59 结束 / 结束后最早）。
	assert.Equal(t, 34*time.Hour, trialRecallS1MaxBefore)
	assert.Equal(t, 14*time.Hour, trialRecallS2MinAfter)
	assert.Equal(t, trialRecallCST(3, 7, 59).Sub(trialRecallSendAt(trialRecallS1, trialRecallCST(3, 7, 59).Unix())),
		trialRecallS1MaxBefore-time.Minute)
}

func TestTrialRecallQuietRun(t *testing.T) {
	for h, want := range map[int]bool{7: true, 8: false, 12: false, 21: false, 22: false, 23: true, 0: true, 3: true} {
		assert.Equal(t, want, trialRecallQuietRun(trialRecallCST(2, h, 0)), "hour %d", h)
	}
	assert.Equal(t, trialRecallFarNow, trialRecallCST(1, 20, 0).UTC(), "测试锚点是北京 20:00，非静默")
}

func TestHandleTrialRecallTask_QuietHourDoesNothing(t *testing.T) {
	setupTrialRecallEnv(t, true)
	pushed := map[uint64]bool{}
	trialRecallPushToUser = func(_ context.Context, uid uint64, _ PushRequest) error { pushed[uid] = true; return nil }
	prevNow := trialRecallNow
	t.Cleanup(func() { trialRecallNow = prevNow })

	// 结束于 03 日 01:00 → 发送时刻提前到 01 日 22:00；23:00 那次运行静默，不发。
	u, _ := seedTrialRecallRecipient(t, false, trialRecallCST(3, 1, 0).Unix())
	trialRecallNow = func() time.Time { return trialRecallCST(1, 23, 0) }
	require.NoError(t, handleTrialRecallTask(context.Background(), nil))
	assert.False(t, pushed[u.ID], "静默时段空转")
	assert.Empty(t, trialRecallLogs(t, u.ID))

	// 22:00 那次（当晚最后一次非静默运行）照常发。
	trialRecallNow = func() time.Time { return trialRecallCST(1, 22, 0) }
	require.NoError(t, handleTrialRecallTask(context.Background(), nil))
	assert.True(t, pushed[u.ID])
}

func TestTrialRecallCandidates_DueWindows(t *testing.T) {
	skipIfNoConfig(t)
	now := trialRecallFarNow // 北京 2090-01-01 20:00
	s := trialRecallDefaults()
	mk := func(isTrial bool, te time.Time) User {
		s.expiredAt, s.isTrial = te.Unix(), isTrial
		u, _ := seedTrialRecallUser(t, s, te.Unix())
		return u
	}
	// S1
	due := mk(true, trialRecallCST(2, 20, 0))       // 发送时刻 = 01 20:00
	notYet := mk(true, trialRecallCST(2, 21, 0))    // 01 21:00 才到点
	nightShift := mk(true, trialRecallCST(3, 3, 0)) // 02 03:00 静默 → 01 22:00
	ended := mk(true, trialRecallCST(1, 19, 59))    // 已结束，不再是 S1
	beyond := mk(true, now.Add(trialRecallS1MaxBefore+time.Hour))
	s1 := trialRecallIDs(t, trialRecallS1, now)
	assert.Contains(t, s1, due.ID)
	assert.NotContains(t, s1, notYet.ID)
	assert.NotContains(t, s1, nightShift.ID)
	assert.NotContains(t, s1, ended.ID)
	assert.NotContains(t, s1, beyond.ID)
	at21 := trialRecallIDs(t, trialRecallS1, trialRecallCST(1, 21, 0))
	assert.Contains(t, at21, notYet.ID, "下一个整点到点")
	assert.NotContains(t, at21, nightShift.ID)
	assert.Contains(t, trialRecallIDs(t, trialRecallS1, trialRecallCST(1, 22, 0)), nightShift.ID, "凌晨结束的人在前一晚 22:00 收到")

	// S2
	s2due := mk(false, trialRecallCST(0, 20, 0)) // 结束后 24h = 01 20:00
	s2notYet := mk(false, trialRecallCST(0, 21, 0))
	s2early := mk(false, trialRecallCST(1, 3, 0)) // 02 03:00 静默 → 01 22:00
	s2old := mk(false, now.Add(-trialRecallS2MaxAfter-time.Second))
	s2 := trialRecallIDs(t, trialRecallS2, now)
	assert.Contains(t, s2, s2due.ID)
	assert.NotContains(t, s2, s2notYet.ID)
	assert.NotContains(t, s2, s2early.ID)
	assert.NotContains(t, s2, s2old.ID)
	assert.Contains(t, trialRecallIDs(t, trialRecallS2, trialRecallCST(1, 22, 0)), s2early.ID, "结束后 19h 的 22:00 也要能圈到")
}

func TestTrialRecallAlertDaily_OncePerStageKindAndDay(t *testing.T) {
	resetTrialRecallAlertDedupe(t)
	n := 0
	prev := trialRecallAlert
	trialRecallAlert = func(string) { n++ }
	t.Cleanup(func() { trialRecallAlert = prev })

	trialRecallAlertDaily(trialRecallCST(1, 9, 0), trialRecallS1, "template_missing", "m")
	trialRecallAlertDaily(trialRecallCST(1, 10, 0), trialRecallS1, "template_missing", "m")
	assert.Equal(t, 1, n, "同一天同一类只报一次")
	trialRecallAlertDaily(trialRecallCST(1, 10, 0), trialRecallS2, "template_missing", "m")
	trialRecallAlertDaily(trialRecallCST(1, 10, 0), trialRecallS1, "email_failed", "m")
	assert.Equal(t, 3, n, "不同阶段 / 不同类各自报")
	trialRecallAlertDaily(trialRecallCST(2, 9, 0), trialRecallS1, "template_missing", "m")
	assert.Equal(t, 4, n, "第二天再报")
}

func TestProcessTrialRecall_NotDueIsUntouched(t *testing.T) {
	setupTrialRecallEnv(t, true)
	now := trialRecallFarNow
	pushed := false
	trialRecallPushToUser = func(context.Context, uint64, PushRequest) error { pushed = true; return nil }
	u, _ := seedTrialRecallRecipient(t, false, now.Add(30*time.Hour).Unix()) // 发送时刻在 6 小时后

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, 0, r.Candidates, "%+v", r)
	assert.False(t, pushed)
	assert.Empty(t, trialRecallLogs(t, u.ID))
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

// resetTrialRecallAlertDedupe 清空按天告警去重：测试共用固定日期，不清会互相吞告警。
func resetTrialRecallAlertDedupe(t *testing.T) {
	t.Helper()
	trialRecallAlertedMu.Lock()
	prev := trialRecallAlerted
	trialRecallAlerted = map[string]bool{}
	trialRecallAlertedMu.Unlock()
	t.Cleanup(func() {
		trialRecallAlertedMu.Lock()
		trialRecallAlerted = prev
		trialRecallAlertedMu.Unlock()
	})
}

// setupTrialRecallEnv 配好 dev_mode、测试专用 slug / 活动码 / 套餐 / 模板，返回活动码。
func setupTrialRecallEnv(t *testing.T, withCampaign bool) string {
	t.Helper()
	skipIfNoConfig(t)
	uniq := fmt.Sprintf("%d", time.Now().UnixNano())

	// 默认把推送与告警出口换成空操作：绝不触达真 PushToUser / Slack；需要断言的测试再自行覆盖。
	prevPush, prevAlert := trialRecallPushToUser, trialRecallAlert
	trialRecallPushToUser = func(context.Context, uint64, PushRequest) error { return nil }
	trialRecallAlert = func(string) {}
	t.Cleanup(func() { trialRecallPushToUser, trialRecallAlert = prevPush, prevAlert })
	resetTrialRecallAlertDedupe(t)

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

// seedTrialRecallNoEmail 建一个没有邮箱的 S1 候选用户（holdout 桶由 wantHoldout 决定）。
func seedTrialRecallNoEmail(t *testing.T, wantHoldout bool, trialEnd int64) User {
	t.Helper()
	for i := 0; i < 400; i++ {
		trialRecallSeq++
		uniq := fmt.Sprintf("%d-%d", time.Now().UnixNano(), trialRecallSeq)
		u := User{UUID: "usr-trn-" + uniq, ExpiredAt: trialEnd, IsFirstOrderDone: BoolPtr(false), Brand: "kaitu", Language: "zh-CN"}
		require.NoError(t, db.Get().Create(&u).Error)
		if inTrialRecallHoldout(u.ID) != wantHoldout {
			db.Get().Unscoped().Delete(&u)
			continue
		}
		t.Cleanup(func() {
			db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&EmailSendLog{})
			db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&LoginIdentify{})
			db.Get().Unscoped().Delete(&u)
		})
		sub := Subscription{UserID: u.ID, Provider: SubscriptionProviderApple,
			ProviderSubscriptionID: "TRN-" + uniq, ProductID: "io.kaitu.sub.basic.1y",
			Environment: "Production", Status: "active", IsTrial: true, TrialEndsAt: trialEnd}
		require.NoError(t, db.Get().Create(&sub).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&sub) })
		return u
	}
	t.Fatal("could not seed a user with the wanted holdout bucket")
	return User{}
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
	u, _ := seedTrialRecallRecipient(t, false, now.Add(20*time.Hour).Unix())

	r1 := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, 1, r1.Candidates, "%+v", r1)
	assert.Equal(t, 1, r1.Sent, "%+v", r1)

	r2 := processTrialRecall(context.Background(), trialRecallS1, now.Add(time.Hour))
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
	u, email := seedTrialRecallRecipient(t, false, now.Add(20*time.Hour).Unix())

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
	u, _ := seedTrialRecallRecipient(t, true, now.Add(20*time.Hour).Unix())

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, 1, r.Holdout, "%+v", r)
	assert.Equal(t, 0, r.Sent, "%+v", r)

	// 首次见到即落一条 skipped / holdout 记录；下一小时再跑不会堆积。
	logs := trialRecallLogs(t, u.ID)
	require.Len(t, logs, 1)
	assert.Equal(t, EmailSendLogStatusSkipped, logs[0].Status)
	require.NotNil(t, logs[0].ErrorMsg)
	assert.Equal(t, "holdout", *logs[0].ErrorMsg)
	assert.Equal(t, "trial-recall:s1:2090-01-01T20", logs[0].BatchID)
	assert.NotZero(t, logs[0].TemplateID)

	processTrialRecall(context.Background(), trialRecallS1, now.Add(time.Hour))
	assert.Len(t, trialRecallLogs(t, u.ID), 1, "下一小时不重复写")

	// skipped 记录绝不算已发。
	got, err := trialRecallAlreadySent(trialRecallS1, []uint64{u.ID})
	require.NoError(t, err)
	assert.False(t, got[u.ID])
}

func TestProcessTrialRecall_NoEmailRecordedOnce(t *testing.T) {
	setupTrialRecallEnv(t, true)
	now := trialRecallFarNow
	u := seedTrialRecallNoEmail(t, false, now.Add(20*time.Hour).Unix())

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, 1, r.NoEmail, "%+v", r)
	logs := trialRecallLogs(t, u.ID)
	require.Len(t, logs, 1)
	assert.Equal(t, EmailSendLogStatusSkipped, logs[0].Status)
	require.NotNil(t, logs[0].ErrorMsg)
	assert.Equal(t, "no_email", *logs[0].ErrorMsg)
	assert.Equal(t, "", logs[0].Email)

	processTrialRecall(context.Background(), trialRecallS1, now.Add(time.Hour))
	assert.Len(t, trialRecallLogs(t, u.ID), 1, "下一小时不重复写")
}

func TestProcessTrialRecall_NoEmailThenBoundEmailIsSent(t *testing.T) {
	setupTrialRecallEnv(t, true)
	now := trialRecallFarNow
	u := seedTrialRecallNoEmail(t, false, now.Add(20*time.Hour).Unix())

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, 1, r.NoEmail, "%+v", r)

	email := fmt.Sprintf("trn-%d@example.com", time.Now().UnixNano())
	require.NoError(t, db.Get().Create(&LoginIdentify{UserID: u.ID, Type: "email", IndexID: email + "-idx", EncryptedValue: email}).Error)

	r = processTrialRecall(context.Background(), trialRecallS1, now.Add(time.Hour))
	assert.Equal(t, 1, r.Sent, "%+v", r)
	assert.Equal(t, 0, r.AlreadySent)
	var n int64
	require.NoError(t, db.Get().Model(&EmailSendLog{}).Where("user_id = ? AND status = ?", u.ID, "sent").Count(&n).Error)
	assert.Equal(t, int64(1), n)
}

func TestProcessTrialRecall_MissingCampaignSendsNothing(t *testing.T) {
	setupTrialRecallEnv(t, false)
	now := trialRecallFarNow
	u, _ := seedTrialRecallRecipient(t, false, now.Add(20*time.Hour).Unix())

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
	u, _ := seedTrialRecallRecipient(t, false, now.Add(20*time.Hour).Unix())

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, 1, r.Candidates, "%+v", r)
	assert.Equal(t, 0, r.Sent, "%+v", r)
	assert.Empty(t, trialRecallLogs(t, u.ID))
}

func TestTrialRecallEmailVars_ConcreteOfferIgnoresRouterPlans(t *testing.T) {
	code := setupTrialRecallEnv(t, true)
	active := true
	router := &Plan{PID: "trtr" + code[len(code)-10:], Label: "trt-router", Price: 35900, OriginPrice: 35900,
		Month: 12, IsActive: &active, Product: ProductRouter, Brand: string(BrandKaitu)}
	require.NoError(t, db.Get().Create(router).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(router) })

	c := trialRecallCandidate{User: User{Language: "zh-CN"}, TrialEndsAt: trialRecallFarNow.Add(40 * time.Hour).Unix()}
	vars, ok := trialRecallEmailVars(context.Background(), trialRecallS1, c, "a@example.com")
	require.True(t, ok)

	// 文案必须等于「在售年付 App 套餐 + 活动码」经下单同一算法算出的值；共享开发库里可能有别的套餐，
	// 所以从生产函数推导期望值，而不是写死金额。
	plan := recallOfferPlan(context.Background(), BrandKaitu)
	require.NotNil(t, plan)
	assert.Equal(t, ProductApp, plan.Product, "router 套餐不能被当成召回推荐的年付套餐")
	campaign := getCampaignByCode(context.Background(), code, BrandKaitu)
	require.NotNil(t, campaign)
	offer := recallOfferPrice(campaign, plan)
	assert.Equal(t, recallOfferText(offer, plan.Price), vars["OfferText"])
	assert.Equal(t, vars["OfferText"], vars["SavingsText"], "旧模板变量与新文案一致")
	assert.NotContains(t, vars["OfferText"], "$359", "router 价格不得出现")
	assert.NotContains(t, vars["OfferText"], "最高", "不再写「最高立减」")
	assert.Equal(t, code, vars["CampaignCode"])
	assert.Equal(t, recallValidUntil(campaign.EndAt), vars["ValidDays"], "期限 = 活动码真实截止日")
	assert.Equal(t, "2090-01-03", vars["TrialEndDate"], "Asia/Shanghai 日期")
	assert.Contains(t, vars["PurchaseURL"], "campaign="+code)
	assert.Contains(t, vars["PurchaseURL"], "plan="+plan.PID)
}

func TestTrialRecallPurchaseURL(t *testing.T) {
	u := trialRecallPurchaseURL("a+b@example.com", "TRIAL80", "pid12", trialRecallS1)
	assert.True(t, strings.HasPrefix(u, BrandKaitu.Config().BaseURL+"/zh-CN/purchase?"), u)
	assert.Contains(t, u, "campaign=TRIAL80")
	assert.Contains(t, u, "plan=pid12")
	assert.True(t, strings.HasSuffix(u, "#email=a%2Bb%40example.com"), "邮箱只在 fragment：%s", u)
	assert.NotContains(t, strings.SplitN(u, "#", 2)[0], "email=", "邮箱不得进 query（GA4 会采集 query）")
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
	assert.Regexp(t, `asynq\.Cron\("0 \* \* \* \*", TaskTypeTrialRecall,`, s)
	assert.Equal(t, "trial:recall", TaskTypeTrialRecall)
}
