package center

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

// =====================================================================
// 交易类触发与文案（spec 2026-11-03-ios-push-design §6.4）
// =====================================================================

func TestPushCopyLang(t *testing.T) {
	cases := []struct {
		lang  string
		brand Brand
		want  string
	}{
		{"zh-CN", BrandKaitu, "zh-CN"},
		{"zh-Hans", BrandKaitu, "zh-CN"},
		{"zh", BrandOverleap, "zh-CN"},
		{"zh-TW", BrandKaitu, "zh-TW"},
		{"zh-HK", BrandKaitu, "zh-TW"},
		{"zh-Hant", BrandKaitu, "zh-TW"},
		{"ja", BrandOverleap, "ja"},
		{"ja-JP", BrandOverleap, "ja"},
		{"en-US", BrandKaitu, "en-US"},
		{"fr", BrandOverleap, "en-US"},
		{"", BrandKaitu, "zh-CN"},
		{"", BrandOverleap, "en-US"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, pushCopyLang(c.lang, c.brand), "%q/%s", c.lang, c.brand)
	}
}

func TestPushCopy_CompleteAndBrandFree(t *testing.T) {
	for _, kind := range []pushCopyKind{pushCopyBillingRetry, pushCopyRenewal3d, pushCopyExpired1d} {
		for _, lang := range []string{"zh-CN", "zh-TW", "en-US", "ja"} {
			title, body := pushCopy(kind, lang)
			require.NotEmpty(t, title, "%s/%s", kind, lang)
			require.NotEmpty(t, body, "%s/%s", kind, lang)
			for _, banned := range []string{"Kaitu", "kaitu", "开途", "開途", "Overleap", "overleap"} {
				assert.NotContains(t, title+body, banned, "推送文案不写品牌名（iOS 自己显示 App 名）")
			}
			for _, banned := range []string{"$", "¥", "￥", "@"} {
				assert.NotContains(t, title+body, banned, "推送文案不含金额 / 邮箱")
			}
		}
	}
}

// seedAppleSub 给用户建一条 Apple 订阅，测试结束硬删。
func seedAppleSub(t *testing.T, userID uint64, status string, isTrial bool) Subscription {
	t.Helper()
	sub := Subscription{
		UserID: userID, Provider: "apple", ProviderSubscriptionID: fmt.Sprintf("otx-push-%d", time.Now().UnixNano()),
		ProductID: "p", CurrentPeriodEnd: time.Now().Add(24 * time.Hour).Unix(), AutoRenew: true,
		Environment: "Production", Status: status, IsTrial: isTrial,
	}
	require.NoError(t, db.Get().Create(&sub).Error)
	t.Cleanup(func() { db.Get().Delete(&Subscription{}, sub.ID) })
	return sub
}

func billingRetryInfo(signedMs int64) *appstore.RenewalInfo {
	return &appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_On, IsInBillingRetryPeriod: true, SignedDate: signedMs}
}

func pushLogsOf(t *testing.T, userID uint64) []PushSendLog {
	t.Helper()
	var logs []PushSendLog
	require.NoError(t, db.Get().Where("user_id = ?", userID).Order("id").Find(&logs).Error)
	return logs
}

func TestBillingRetryPush(t *testing.T) {
	skipIfNoConfig(t)

	t.Run("entering billing retry pushes once, replays do not", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true) // 没同意营销也要收到：交易类
		cleanupPushLogs(t, u.ID)
		sub := seedAppleSub(t, u.ID, "active", false)
		snapshot := sub
		now := time.Now().UnixMilli()

		require.NoError(t, applyRenewalInfo(context.Background(), &sub, billingRetryInfo(now), ""))
		require.Len(t, h.enqueued, 1)

		// Apple 重投同一事件：快照仍是 active（webhook 在事务外读的行），靠 DedupKey 挡住
		require.NoError(t, applyRenewalInfo(context.Background(), &snapshot, billingRetryInfo(now+1000), ""))
		// reconcile 读到的已是 billing_retry
		var fresh Subscription
		require.NoError(t, db.Get().First(&fresh, sub.ID).Error)
		require.NoError(t, applyRenewalInfo(context.Background(), &fresh, billingRetryInfo(now+2000), ""))
		assert.Len(t, h.enqueued, 1, "同一次扣款失败只推一次")

		logs := pushLogsOf(t, u.ID)
		require.Len(t, logs, 1)
		assert.Equal(t, fmt.Sprintf("billing_retry:%d:%d", sub.ID, sub.CurrentPeriodEnd), logs[0].DedupKey)
		assert.Equal(t, string(PushCategoryTransactional), logs[0].Category)
		assert.Equal(t, "/purchase", logs[0].Route)
	})

	t.Run("trial chain first charge failure is included", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true)
		cleanupPushLogs(t, u.ID)
		sub := seedAppleSub(t, u.ID, "active", true)
		require.NoError(t, applyRenewalInfo(context.Background(), &sub, billingRetryInfo(time.Now().UnixMilli()), ""))
		assert.Len(t, h.enqueued, 1)
	})

	t.Run("auto-renew toggle without billing failure does not push", func(t *testing.T) {
		h := withPushHarness(t)
		u, _, _ := seedPushUser(t, false, true)
		sub := seedAppleSub(t, u.ID, "active", false)
		off := &appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_Off, SignedDate: time.Now().UnixMilli()}
		require.NoError(t, applyRenewalInfo(context.Background(), &sub, off, appstore.Subtype_AUTO_RENEW_DISABLED))
		assert.Empty(t, h.enqueued)
	})

	t.Run("push failure never fails the renewal update", func(t *testing.T) {
		withPushHarness(t)
		apnsClient = nil // 通道不可用
		u, _, _ := seedPushUser(t, false, true)
		sub := seedAppleSub(t, u.ID, "active", false)
		require.NoError(t, applyRenewalInfo(context.Background(), &sub, billingRetryInfo(time.Now().UnixMilli()), ""))
		var fresh Subscription
		require.NoError(t, db.Get().First(&fresh, sub.ID).Error)
		assert.Equal(t, "billing_retry", fresh.Status)
	})
}

func TestLifecyclePushes(t *testing.T) {
	skipIfNoConfig(t)

	nowUTC := time.Now().UTC()
	dayStart := func(offset int) time.Time {
		return time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, offset)
	}

	// mkUser 按到期日、是否付过费建用户（带设备 + token）。
	mkUser := func(t *testing.T, expiredAt int64, paid bool, lang string) User {
		u, _, _ := seedPushUser(t, false, true)
		require.NoError(t, db.Get().Model(&User{}).Where("id = ?", u.ID).Updates(map[string]any{
			"expired_at": expiredAt, "is_first_order_done": paid, "language": lang,
		}).Error)
		cleanupPushLogs(t, u.ID)
		u.ExpiredAt = expiredAt
		return u
	}

	t.Run("3 days before expiry", func(t *testing.T) {
		h := withPushHarness(t)
		exp := dayStart(3).Add(12 * time.Hour).Unix()
		paid := mkUser(t, exp, true, "zh-CN")
		neverPaid := mkUser(t, exp, false, "zh-CN")
		autoRenew := mkUser(t, exp, true, "zh-CN")
		seedAppleSub(t, autoRenew.ID, "active", false)

		processLifecyclePushes(context.Background(), 3)

		logs := pushLogsOf(t, paid.ID)
		require.Len(t, logs, 1)
		assert.Equal(t, fmt.Sprintf("renewal_3:%d:%d", paid.ID, exp), logs[0].DedupKey)
		assert.Equal(t, string(PushCategoryTransactional), logs[0].Category)
		assert.Equal(t, "/purchase", logs[0].Route)
		wantTitle, _ := pushCopy(pushCopyRenewal3d, "zh-CN")
		assert.Equal(t, wantTitle, logs[0].Title)
		assert.Empty(t, pushLogsOf(t, neverPaid.ID), "从没付过费的（含试用到期）归召回会话")
		assert.Empty(t, pushLogsOf(t, autoRenew.ID), "自动续费会自动扣款，不提醒")

		processLifecyclePushes(context.Background(), 3)
		assert.Len(t, pushLogsOf(t, paid.ID), 1, "同一天重跑不重复推")
		_ = h
	})

	t.Run("1 day after expiry", func(t *testing.T) {
		withPushHarness(t)
		exp := dayStart(-1).Add(12 * time.Hour).Unix()
		paid := mkUser(t, exp, true, "ja")
		processLifecyclePushes(context.Background(), -1)
		logs := pushLogsOf(t, paid.ID)
		require.Len(t, logs, 1)
		assert.Equal(t, fmt.Sprintf("expired_1:%d:%d", paid.ID, exp), logs[0].DedupKey)
		wantTitle, _ := pushCopy(pushCopyExpired1d, "ja")
		assert.Equal(t, wantTitle, logs[0].Title)
	})

	t.Run("days without a push node do nothing", func(t *testing.T) {
		h := withPushHarness(t)
		processLifecyclePushes(context.Background(), 7)
		processLifecyclePushes(context.Background(), -7)
		assert.Empty(t, h.enqueued)
	})
}

func TestRenewalReminderTask_PushesIndependentOfEmailTemplates(t *testing.T) {
	// 推送不能被「邮件模板缺失」短路：两者在 handleRenewalReminderTask 里是并列的两段。
	raw, err := os.ReadFile("worker_renewal_reminder.go")
	require.NoError(t, err)
	src := string(raw)
	assert.True(t, strings.Contains(src, "processLifecyclePushes(ctx, days)"),
		"handleRenewalReminderTask 必须对每个触发日调用 processLifecyclePushes")
}
