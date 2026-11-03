package center

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/db"
)

// ===================== Task 6：召回推送 =====================

type recordedPush struct {
	userID uint64
	req    PushRequest
}

// stubTrialRecallPush 替换推送出口为记录器；errFor 可按用户返回错误。
func stubTrialRecallPush(t *testing.T, errFor func(userID uint64) error) *[]recordedPush {
	t.Helper()
	var got []recordedPush
	prev := trialRecallPushToUser
	trialRecallPushToUser = func(_ context.Context, userID uint64, req PushRequest) error {
		got = append(got, recordedPush{userID, req})
		if errFor != nil {
			return errFor(userID)
		}
		return nil
	}
	t.Cleanup(func() { trialRecallPushToUser = prev })
	return &got
}

// countTrialRecallAlerts 替换 Slack 告警出口并计数。
func countTrialRecallAlerts(t *testing.T) *int {
	t.Helper()
	n := 0
	prev := trialRecallAlert
	trialRecallAlert = func(string) { n++ }
	t.Cleanup(func() { trialRecallAlert = prev })
	return &n
}

func TestTrialRecallPush_S1TransactionalEvenForHoldout(t *testing.T) {
	setupTrialRecallEnv(t, true)
	got := stubTrialRecallPush(t, nil)
	now := trialRecallFarNow
	u, _ := seedTrialRecallRecipient(t, true, now.Add(40*time.Hour).Unix())

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	require.Len(t, *got, 1, "%+v", r)
	p := (*got)[0]
	assert.Equal(t, u.ID, p.userID)
	assert.Equal(t, PushCategoryTransactional, p.req.Category)
	assert.Equal(t, trialRecallDedupKey(trialRecallS1, u.ID), p.req.DedupKey)
	assert.Equal(t, "/purchase", p.req.Route)
	assert.Contains(t, p.req.Body, trialRecallDateString(now.Add(40*time.Hour).Unix()))
	assert.Equal(t, 1, r.PushQueued)
	assert.Equal(t, 1, r.Holdout)
	assert.Equal(t, 0, r.Sent, "对照组仍不发邮件")
}

func TestTrialRecallPush_S2MarketingSkipsHoldout(t *testing.T) {
	setupTrialRecallEnv(t, true)
	got := stubTrialRecallPush(t, nil)
	now := trialRecallFarNow
	te := now.Add(-3 * 24 * time.Hour).Unix()
	seedS2 := func(holdout bool) User {
		u := seedTrialRecallNoEmail(t, holdout, te)
		require.NoError(t, db.Get().Model(&User{}).Where("id = ?", u.ID).Update("expired_at", te).Error)
		return u
	}
	hold, normal := seedS2(true), seedS2(false)

	r := processTrialRecall(context.Background(), trialRecallS2, now)
	require.Len(t, *got, 1, "%+v", r)
	assert.Equal(t, normal.ID, (*got)[0].userID)
	assert.NotEqual(t, hold.ID, (*got)[0].userID)
	assert.Equal(t, PushCategoryMarketing, (*got)[0].req.Category)
	assert.Equal(t, "试用已结束，订阅后立即恢复使用", (*got)[0].req.Body)
	assert.Equal(t, 1, r.PushQueued)
}

func TestTrialRecallPush_NoEmailStillPushed_EmptyLanguageIsChinese(t *testing.T) {
	setupTrialRecallEnv(t, true)
	got := stubTrialRecallPush(t, nil)
	now := trialRecallFarNow
	u := seedTrialRecallNoEmail(t, false, now.Add(40*time.Hour).Unix())
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", u.ID).Update("language", "").Error)

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	require.Len(t, *got, 1, "%+v", r)
	assert.Equal(t, 1, r.NoEmail)
	title, _ := trialRecallPushCopy(trialRecallS1, "zh-CN", now)
	assert.Equal(t, title, (*got)[0].req.Title)
}

func TestTrialRecallPush_ErrorsCounted(t *testing.T) {
	setupTrialRecallEnv(t, true)
	now := trialRecallFarNow
	errs := []error{ErrPushNoActiveToken, ErrPushNotConsented, ErrPushDuplicate, ErrPushInvalidRoute,
		ErrPushInvalidRequest, fmt.Errorf("redis down")}
	byUser := map[uint64]error{}
	for _, e := range errs {
		u := seedTrialRecallNoEmail(t, false, now.Add(40*time.Hour).Unix())
		byUser[u.ID] = e
	}
	stubTrialRecallPush(t, func(id uint64) error { return byUser[id] })
	alerts := countTrialRecallAlerts(t)

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Equal(t, 0, r.PushQueued, "%+v", r)
	assert.Equal(t, map[string]int{"no_token": 1, "not_consented": 1, "duplicate": 1,
		"invalid_route": 1, "invalid_request": 1, "other": 1}, r.PushErrors)
	assert.Equal(t, 1, *alerts, "非哨兵错误才告警")
}

func TestTrialRecallPush_ChannelUnavailableStopsStageNoAlert(t *testing.T) {
	setupTrialRecallEnv(t, true)
	now := trialRecallFarNow
	for i := 0; i < 3; i++ {
		seedTrialRecallNoEmail(t, false, now.Add(40*time.Hour).Unix())
	}
	got := stubTrialRecallPush(t, func(uint64) error { return ErrPushChannelUnavailable })
	alerts := countTrialRecallAlerts(t)

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Len(t, *got, 1, "通道不可用后本阶段不再逐用户重试")
	assert.Equal(t, 1, r.PushErrors["channel_unavailable"])
	assert.Equal(t, 0, *alerts)
}

func TestTrialRecallPush_StillSentWhenCampaignMissing(t *testing.T) {
	setupTrialRecallEnv(t, false)
	got := stubTrialRecallPush(t, nil)
	countTrialRecallAlerts(t)
	now := trialRecallFarNow
	u, _ := seedTrialRecallRecipient(t, false, now.Add(40*time.Hour).Unix())

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Len(t, *got, 1, "%+v", r)
	assert.Equal(t, 0, r.Sent)
	assert.Empty(t, trialRecallLogs(t, u.ID), "邮件前置不满足：不发信也不落 skipped")
}

func TestTrialRecallPush_StillSentWhenTemplateMissing(t *testing.T) {
	setupTrialRecallEnv(t, true)
	trialRecallSlugs = map[trialRecallStage]string{trialRecallS1: fmt.Sprintf("trt-no-such-template-%d", time.Now().UnixNano())}
	got := stubTrialRecallPush(t, nil)
	countTrialRecallAlerts(t)
	now := trialRecallFarNow
	seedTrialRecallRecipient(t, false, now.Add(40*time.Hour).Unix())

	r := processTrialRecall(context.Background(), trialRecallS1, now)
	assert.Len(t, *got, 1, "%+v", r)
	assert.Equal(t, 0, r.Sent)
}
