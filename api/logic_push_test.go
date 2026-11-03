package center

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// =====================================================================
// 推送发送通道测试（spec 2026-11-03-ios-push-design §6.1 / §6.2）
// 真实 DB；APNs、入队、告警全部替身化，绝不打真 Apple。
// =====================================================================

type fakeAPNsCall struct {
	Token   string
	Topic   string
	Sandbox bool
	Title   string
	Route   string
}

// fakeAPNs 按 (token, sandbox) 返回预设结果；未预设 = 200。
type fakeAPNs struct {
	mu      sync.Mutex
	results map[string]apnsResult // key: token + "|" + "prod"/"dev"
	calls   []fakeAPNsCall
}

func (f *fakeAPNs) key(token string, sandbox bool) string {
	if sandbox {
		return token + "|dev"
	}
	return token + "|prod"
}

func (f *fakeAPNs) Send(_ context.Context, n apnsNotification) apnsResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeAPNsCall{Token: n.Token, Topic: n.Topic, Sandbox: n.Sandbox, Title: n.Title, Route: n.Route})
	if r, ok := f.results[f.key(n.Token, n.Sandbox)]; ok {
		return r
	}
	return apnsResult{StatusCode: 200}
}

type pushHarness struct {
	apns     *fakeAPNs
	enqueued []uint64
	alerts   []string
}

// withPushHarness 替换 APNs 客户端 / 入队 / 告警三处出口，测试结束还原。
func withPushHarness(t *testing.T) *pushHarness {
	t.Helper()
	h := &pushHarness{apns: &fakeAPNs{results: map[string]apnsResult{}}}
	prevClient, prevEnqueue, prevAlert := apnsClient, enqueuePushTask, pushAlert
	pushAuthAlertMu.Lock()
	pushAuthAlertLast = time.Time{}
	pushAuthAlertMu.Unlock()
	apnsClient = h.apns
	enqueuePushTask = func(_ context.Context, logID uint64) error {
		h.enqueued = append(h.enqueued, logID)
		return nil
	}
	pushAlert = func(msg string) { h.alerts = append(h.alerts, msg) }
	t.Cleanup(func() { apnsClient, enqueuePushTask, pushAlert = prevClient, prevEnqueue, prevAlert })
	return h
}

// seedPushUser 建一个用户 + 设备 + （可选）APNs token，全部在测试结束时硬删。
func seedPushUser(t *testing.T, optIn bool, withToken bool) (User, Device, *PushToken) {
	t.Helper()
	uniq := time.Now().UnixNano()
	u := User{UUID: fmt.Sprintf("usr-push-%d", uniq), Brand: string(BrandKaitu), PushMarketingOptIn: optIn}
	require.NoError(t, db.Get().Create(&u).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&User{}, u.ID) })

	d := Device{UDID: fmt.Sprintf("udid-push-%d", uniq), UserID: u.ID, TokenIssueAt: time.Now().Unix(), TokenLastUsedAt: time.Now().Unix()}
	require.NoError(t, db.Get().Create(&d).Error)
	t.Cleanup(func() { db.Get().Where("udid = ?", d.UDID).Delete(&Device{}) })

	if !withToken {
		return u, d, nil
	}
	tok := seedPushToken(t, u.ID, d.UDID, fmt.Sprintf("apns-tok-%d", uniq))
	return u, d, tok
}

func seedPushToken(t *testing.T, userID uint64, udid, token string) *PushToken {
	t.Helper()
	sandbox := false
	tok := PushToken{
		UserID: userID, DeviceUDID: udid, Platform: PushPlatformIOS, Provider: PushProviderAPNs,
		Token: token, Topic: "com.allnationconnect.anc.wgios", Sandbox: &sandbox,
		AppFlavor: AppFlavorChina, Status: PushTokenStatusActive, LastSeenAt: time.Now().Unix(),
	}
	require.NoError(t, db.Get().Create(&tok).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&PushToken{}, tok.ID) })
	return &tok
}

func cleanupPushLogs(t *testing.T, userID uint64) {
	t.Cleanup(func() { db.Get().Where("user_id = ?", userID).Delete(&PushSendLog{}) })
}

func pushReq(cat PushCategory, dedup string) PushRequest {
	return PushRequest{Category: cat, DedupKey: dedup, Title: "t", Body: "b", Route: "/purchase"}
}

func TestPushToUser_SyncResults(t *testing.T) {
	skipIfNoConfig(t)

	t.Run("no active token", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, true, false)
		cleanupPushLogs(t, u.ID)
		err := PushToUser(context.Background(), u.ID, pushReq(PushCategoryTransactional, fmt.Sprintf("t-notoken-%d", u.ID)))
		assert.ErrorIs(t, err, ErrPushNoActiveToken)
		assert.Empty(t, h.enqueued)
	})

	t.Run("marketing without consent", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true)
		cleanupPushLogs(t, u.ID)
		err := PushToUser(context.Background(), u.ID, pushReq(PushCategoryMarketing, fmt.Sprintf("t-noconsent-%d", u.ID)))
		assert.ErrorIs(t, err, ErrPushNotConsented)
		assert.Empty(t, h.enqueued)
	})

	t.Run("transactional ignores marketing consent", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true)
		cleanupPushLogs(t, u.ID)
		err := PushToUser(context.Background(), u.ID, pushReq(PushCategoryTransactional, fmt.Sprintf("t-tx-%d", u.ID)))
		require.NoError(t, err)
		assert.Len(t, h.enqueued, 1)
	})

	t.Run("channel unconfigured", func(t *testing.T) {
		withPushHarness(t)
		apnsClient = nil
		u, _, _ := seedPushUser(t, true, true)
		cleanupPushLogs(t, u.ID)
		err := PushToUser(context.Background(), u.ID, pushReq(PushCategoryTransactional, fmt.Sprintf("t-nochan-%d", u.ID)))
		assert.ErrorIs(t, err, ErrPushChannelUnavailable)
	})

	t.Run("duplicate dedup key", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, true, true)
		cleanupPushLogs(t, u.ID)
		key := fmt.Sprintf("t-dup-%d", u.ID)
		require.NoError(t, PushToUser(context.Background(), u.ID, pushReq(PushCategoryMarketing, key)))
		err := PushToUser(context.Background(), u.ID, pushReq(PushCategoryMarketing, key))
		assert.ErrorIs(t, err, ErrPushDuplicate)
		assert.Len(t, h.enqueued, 1, "重复的 DedupKey 只入队一次")
	})

	t.Run("route outside whitelist", func(t *testing.T) {
		withPushHarness(t)
		u, _, _ := seedPushUser(t, true, true)
		req := pushReq(PushCategoryTransactional, "t-route")
		req.Route = "/evil"
		assert.ErrorIs(t, PushToUser(context.Background(), u.ID, req), ErrPushInvalidRoute)
	})

	t.Run("empty route is allowed", func(t *testing.T) {
		withPushHarness(t)
		u, _, _ := seedPushUser(t, true, true)
		cleanupPushLogs(t, u.ID)
		req := pushReq(PushCategoryTransactional, fmt.Sprintf("t-noroute-%d", u.ID))
		req.Route = ""
		assert.NoError(t, PushToUser(context.Background(), u.ID, req))
	})

	t.Run("invalid request", func(t *testing.T) {
		withPushHarness(t)
		u, _, _ := seedPushUser(t, true, true)
		for _, req := range []PushRequest{
			{Category: PushCategoryMarketing, Title: "t", Body: "b"},     // 缺 DedupKey
			{Category: PushCategoryMarketing, DedupKey: "k", Body: "b"},  // 缺 Title
			{Category: PushCategoryMarketing, DedupKey: "k", Title: "t"}, // 缺 Body
			{Category: "promo", DedupKey: "k", Title: "t", Body: "b"},    // 未知分类
		} {
			assert.ErrorIs(t, PushToUser(context.Background(), u.ID, req), ErrPushInvalidRequest)
		}
	})

	t.Run("token of a device now owned by someone else is not used", func(t *testing.T) {
		h := withPushHarness(t)
		a, _, _ := seedPushUser(t, true, false)
		_, dB, _ := seedPushUser(t, true, false)
		// token 仍记在 A 名下，但设备已归 B（换绑 / 被踢后 B 登录）
		seedPushToken(t, a.ID, dB.UDID, fmt.Sprintf("apns-foreign-%d", a.ID))
		cleanupPushLogs(t, a.ID)
		err := PushToUser(context.Background(), a.ID, pushReq(PushCategoryTransactional, fmt.Sprintf("t-foreign-%d", a.ID)))
		assert.ErrorIs(t, err, ErrPushNoActiveToken)
		assert.Empty(t, h.enqueued)
	})

	t.Run("token of a deleted device is not used", func(t *testing.T) {
		withPushHarness(t)
		u, d, _ := seedPushUser(t, true, true)
		require.NoError(t, db.Get().Where("udid = ?", d.UDID).Delete(&Device{}).Error)
		err := PushToUser(context.Background(), u.ID, pushReq(PushCategoryTransactional, fmt.Sprintf("t-deldev-%d", u.ID)))
		assert.ErrorIs(t, err, ErrPushNoActiveToken)
	})

	t.Run("enqueue failure frees the dedup key for a replay", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, true, true)
		cleanupPushLogs(t, u.ID)
		key := fmt.Sprintf("t-enqfail-%d", u.ID)
		enqueuePushTask = func(context.Context, uint64) error { return fmt.Errorf("redis: connection refused") }
		err := PushToUser(context.Background(), u.ID, pushReq(PushCategoryTransactional, key))
		require.Error(t, err)
		for _, sentinel := range []error{ErrPushDuplicate, ErrPushNoActiveToken, ErrPushNotConsented, ErrPushChannelUnavailable} {
			assert.NotErrorIs(t, err, sentinel)
		}
		var n int64
		db.Get().Model(&PushSendLog{}).Where("dedup_key = ?", key).Count(&n)
		assert.Zero(t, n, "日志行必须删掉，否则重放永远是 duplicate")

		enqueuePushTask = func(_ context.Context, id uint64) error { h.enqueued = append(h.enqueued, id); return nil }
		assert.NoError(t, PushToUser(context.Background(), u.ID, pushReq(PushCategoryTransactional, key)), "同一个 key 可以重放")
	})

	t.Run("queued log records the request", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, true, true)
		cleanupPushLogs(t, u.ID)
		req := pushReq(PushCategoryMarketing, fmt.Sprintf("t-log-%d", u.ID))
		req.BatchID = "batch-x"
		require.NoError(t, PushToUser(context.Background(), u.ID, req))
		require.Len(t, h.enqueued, 1)
		var lg PushSendLog
		require.NoError(t, db.Get().First(&lg, h.enqueued[0]).Error)
		assert.Equal(t, PushSendStatusQueued, lg.Status)
		assert.Equal(t, "batch-x", lg.BatchID)
		assert.Equal(t, string(PushCategoryMarketing), lg.Category)
		assert.Equal(t, "/purchase", lg.Route)
	})
}

func TestPushToUsers_PerUserResults(t *testing.T) {
	skipIfNoConfig(t)
	h := withPushHarness(t)
	ok, _, _ := seedPushUser(t, true, true)
	none, _, _ := seedPushUser(t, true, false)
	cleanupPushLogs(t, ok.ID)

	res := PushToUsers(context.Background(), []uint64{ok.ID, none.ID}, pushReq(PushCategoryMarketing, fmt.Sprintf("t-batch-%d", ok.ID)))
	assert.NoError(t, res[ok.ID])
	assert.ErrorIs(t, res[none.ID], ErrPushNoActiveToken)
	assert.Len(t, h.enqueued, 1)

	var lg PushSendLog
	require.NoError(t, db.Get().First(&lg, h.enqueued[0]).Error)
	assert.Equal(t, fmt.Sprintf("t-batch-%d:%d", ok.ID, ok.ID), lg.DedupKey, "批量时 DedupKey 按用户派生")
}

// queueOne 走正常入口拿到一条 queued 日志 ID。
func queueOne(t *testing.T, h *pushHarness, userID uint64) uint64 {
	t.Helper()
	cleanupPushLogs(t, userID)
	require.NoError(t, PushToUser(context.Background(), userID, pushReq(PushCategoryTransactional, fmt.Sprintf("t-deliver-%d-%d", userID, time.Now().UnixNano()))))
	require.NotEmpty(t, h.enqueued)
	return h.enqueued[len(h.enqueued)-1]
}

func reloadToken(t *testing.T, id uint64) PushToken {
	t.Helper()
	var tok PushToken
	require.NoError(t, db.Get().First(&tok, id).Error)
	return tok
}

func reloadLog(t *testing.T, id uint64) PushSendLog {
	t.Helper()
	var lg PushSendLog
	require.NoError(t, db.Get().First(&lg, id).Error)
	return lg
}

func TestDeliverPush(t *testing.T) {
	skipIfNoConfig(t)

	t.Run("success marks log sent with payload", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, tok := seedPushUser(t, true, true)
		logID := queueOne(t, h, u.ID)
		require.NoError(t, deliverPush(context.Background(), logID))
		lg := reloadLog(t, logID)
		assert.Equal(t, PushSendStatusSent, lg.Status)
		assert.Equal(t, 1, lg.SentCount)
		require.Len(t, h.apns.calls, 1)
		assert.Equal(t, tok.Token, h.apns.calls[0].Token)
		assert.Equal(t, "com.allnationconnect.anc.wgios", h.apns.calls[0].Topic)
		assert.Equal(t, "/purchase", h.apns.calls[0].Route)
		assert.False(t, h.apns.calls[0].Sandbox)
	})

	t.Run("410 unregistered deactivates token", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, tok := seedPushUser(t, true, true)
		h.apns.results[h.apns.key(tok.Token, false)] = apnsResult{StatusCode: 410, Reason: "Unregistered"}
		logID := queueOne(t, h, u.ID)
		require.NoError(t, deliverPush(context.Background(), logID), "token 失效不重试")
		assert.Equal(t, PushTokenStatusInactive, reloadToken(t, tok.ID).Status)
		lg := reloadLog(t, logID)
		assert.Equal(t, PushSendStatusFailed, lg.Status)
		assert.Equal(t, 1, lg.FailCount)
	})

	t.Run("BadDeviceToken on production falls back to sandbox", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, tok := seedPushUser(t, true, true)
		h.apns.results[h.apns.key(tok.Token, false)] = apnsResult{StatusCode: 400, Reason: "BadDeviceToken"}
		logID := queueOne(t, h, u.ID)
		require.NoError(t, deliverPush(context.Background(), logID))
		got := reloadToken(t, tok.ID)
		assert.Equal(t, PushTokenStatusActive, got.Status)
		require.NotNil(t, got.Sandbox)
		assert.True(t, *got.Sandbox, "sandbox 成功后翻转 token 的环境")
		assert.Equal(t, PushSendStatusSent, reloadLog(t, logID).Status)
	})

	t.Run("BadDeviceToken on both environments deactivates", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, tok := seedPushUser(t, true, true)
		h.apns.results[h.apns.key(tok.Token, false)] = apnsResult{StatusCode: 400, Reason: "BadDeviceToken"}
		h.apns.results[h.apns.key(tok.Token, true)] = apnsResult{StatusCode: 400, Reason: "BadDeviceToken"}
		logID := queueOne(t, h, u.ID)
		require.NoError(t, deliverPush(context.Background(), logID))
		assert.Equal(t, PushTokenStatusInactive, reloadToken(t, tok.ID).Status)
	})

	t.Run("DeviceTokenNotForTopic deactivates", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, tok := seedPushUser(t, true, true)
		h.apns.results[h.apns.key(tok.Token, false)] = apnsResult{StatusCode: 400, Reason: "DeviceTokenNotForTopic"}
		logID := queueOne(t, h, u.ID)
		require.NoError(t, deliverPush(context.Background(), logID))
		assert.Equal(t, PushTokenStatusInactive, reloadToken(t, tok.ID).Status)
	})

	t.Run("5xx asks for retry and leaves token active", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, tok := seedPushUser(t, true, true)
		h.apns.results[h.apns.key(tok.Token, false)] = apnsResult{StatusCode: 503, Reason: "ServiceUnavailable"}
		logID := queueOne(t, h, u.ID)
		assert.Error(t, deliverPush(context.Background(), logID))
		assert.Equal(t, PushTokenStatusActive, reloadToken(t, tok.ID).Status)
		assert.Equal(t, PushSendStatusQueued, reloadLog(t, logID).Status)
	})

	t.Run("network error asks for retry", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, tok := seedPushUser(t, true, true)
		h.apns.results[h.apns.key(tok.Token, false)] = apnsResult{Err: fmt.Errorf("dial tcp: timeout")}
		logID := queueOne(t, h, u.ID)
		assert.Error(t, deliverPush(context.Background(), logID))
	})

	t.Run("403 auth error alerts once, keeps the token and retries", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, tok := seedPushUser(t, true, true)
		h.apns.results[h.apns.key(tok.Token, false)] = apnsResult{StatusCode: 403, Reason: "InvalidProviderToken"}
		logID := queueOne(t, h, u.ID)
		assert.Error(t, deliverPush(context.Background(), logID), "配置修好后 asynq 退避重试能补发")
		assert.Equal(t, PushTokenStatusActive, reloadToken(t, tok.ID).Status)
		assert.Len(t, h.alerts, 1, "凭据错误必须告警")
		assert.Equal(t, PushSendStatusQueued, reloadLog(t, logID).Status, "不在第一次就把 dedup key 烧成 failed")

		// 同一窗口内第二条日志（群发里的下一个人）撞上同样的错误：不再刷屏
		u2, _, tok2 := seedPushUser(t, true, true)
		h.apns.results[h.apns.key(tok2.Token, false)] = apnsResult{StatusCode: 403, Reason: "InvalidProviderToken"}
		logID2 := queueOne(t, h, u2.ID)
		assert.Error(t, deliverPush(context.Background(), logID2))
		assert.Len(t, h.alerts, 1, "告警限流")
	})

	t.Run("auth alert throttle reopens after the interval", func(t *testing.T) {
		withPushHarness(t)
		t0 := time.Now()
		assert.True(t, pushAuthAlertAllowed(t0))
		assert.False(t, pushAuthAlertAllowed(t0.Add(pushAuthAlertInterval-time.Second)))
		assert.True(t, pushAuthAlertAllowed(t0.Add(pushAuthAlertInterval)))
	})

	t.Run("inconclusive sandbox retry keeps the token and retries", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, tok := seedPushUser(t, true, true)
		h.apns.results[h.apns.key(tok.Token, false)] = apnsResult{StatusCode: 400, Reason: "BadDeviceToken"}
		h.apns.results[h.apns.key(tok.Token, true)] = apnsResult{StatusCode: 503, Reason: "ServiceUnavailable"}
		logID := queueOne(t, h, u.ID)
		assert.Error(t, deliverPush(context.Background(), logID))
		got := reloadToken(t, tok.ID)
		assert.Equal(t, PushTokenStatusActive, got.Status, "可能是有效的 sandbox token，不能停用")
		require.NotNil(t, got.Sandbox)
		assert.False(t, *got.Sandbox)
		assert.Equal(t, PushSendStatusQueued, reloadLog(t, logID).Status)
	})

	t.Run("marketing consent withdrawn before delivery is not sent", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, true, true)
		cleanupPushLogs(t, u.ID)
		require.NoError(t, PushToUser(context.Background(), u.ID, pushReq(PushCategoryMarketing, fmt.Sprintf("t-withdraw-%d", u.ID))))
		require.Len(t, h.enqueued, 1)
		require.NoError(t, db.Get().Model(&User{}).Where("id = ?", u.ID).Update("push_marketing_opt_in", false).Error)
		require.NoError(t, deliverPush(context.Background(), h.enqueued[0]))
		assert.Empty(t, h.apns.calls)
		assert.Equal(t, PushSendStatusFailed, reloadLog(t, h.enqueued[0]).Status)
	})

	t.Run("one success among several tokens does not retry", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, tok := seedPushUser(t, true, true)
		_, d2, _ := seedPushUser(t, true, false)
		// 第二台设备也归 u
		require.NoError(t, db.Get().Model(&Device{}).Where("udid = ?", d2.UDID).Update("user_id", u.ID).Error)
		tok2 := seedPushToken(t, u.ID, d2.UDID, fmt.Sprintf("apns-second-%d", u.ID))
		h.apns.results[h.apns.key(tok2.Token, false)] = apnsResult{StatusCode: 503}
		logID := queueOne(t, h, u.ID)
		require.NoError(t, deliverPush(context.Background(), logID), "已有设备收到，不重试以免重复打扰")
		lg := reloadLog(t, logID)
		assert.Equal(t, PushSendStatusSent, lg.Status)
		assert.Equal(t, 1, lg.SentCount)
		assert.Equal(t, 1, lg.FailCount)
		_ = tok
	})

	t.Run("already processed log is not resent", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, true, true)
		logID := queueOne(t, h, u.ID)
		require.NoError(t, deliverPush(context.Background(), logID))
		require.NoError(t, deliverPush(context.Background(), logID))
		assert.Len(t, h.apns.calls, 1)
	})

	t.Run("all tokens gone by delivery time marks no_token", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, tok := seedPushUser(t, true, true)
		logID := queueOne(t, h, u.ID)
		require.NoError(t, db.Get().Model(&PushToken{}).Where("id = ?", tok.ID).Update("status", PushTokenStatusInactive).Error)
		require.NoError(t, deliverPush(context.Background(), logID))
		assert.Equal(t, PushSendStatusNoToken, reloadLog(t, logID).Status)
		assert.Empty(t, h.apns.calls)
	})
}

func TestMarkPushLogFailed(t *testing.T) {
	skipIfNoConfig(t)
	h := withPushHarness(t)
	u, _, _ := seedPushUser(t, true, true)
	logID := queueOne(t, h, u.ID)
	markPushLogFailed(context.Background(), logID, "retries exhausted")
	lg := reloadLog(t, logID)
	assert.Equal(t, PushSendStatusFailed, lg.Status)
	assert.Equal(t, "retries exhausted", lg.LastError)
}
