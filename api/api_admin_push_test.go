package center

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// =====================================================================
// 后台手动群发（营销类，spec 2026-11-03-ios-push-design §6.5）
// =====================================================================

func adminPushRouter(t *testing.T) *gin.Engine {
	t.Helper()
	admin, _ := seedBrandUser(t, BrandKaitu)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(injectSuperAdmin(admin.ID, admin.UUID))
	r.POST("/app/push/send", api_admin_push_send)
	r.GET("/app/push/batches/:batchId", api_admin_push_batch_stats)
	RegisterApprovalCallback("push_send", executeApprovalPushSend)
	t.Cleanup(func() { db.Get().Where("action = ?", "push_send").Where("requestor_id = ?", admin.ID).Delete(&AdminApproval{}) })
	return r
}

// withInlinePushBatch 让批次任务在请求内同步跑完（替换 asynq 入队）。
func withInlinePushBatch(t *testing.T) {
	t.Helper()
	prev := enqueuePushBatch
	enqueuePushBatch = func(ctx context.Context, req AdminPushSendRequest) error {
		runAdminPushBatch(ctx, req)
		return nil
	}
	t.Cleanup(func() { enqueuePushBatch = prev })
}

func setLang(t *testing.T, userID uint64, lang string) {
	t.Helper()
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", userID).Update("language", lang).Error)
}

func TestAdminPushSend(t *testing.T) {
	skipIfNoConfig(t)

	copyMap := map[string]any{
		"zh-CN": map[string]string{"title": "标题", "body": "正文"},
		"en-US": map[string]string{"title": "Title", "body": "Body"},
	}

	t.Run("dry run counts without sending", func(t *testing.T) {
		h := withPushHarness(t)
		r := adminPushRouter(t)
		consented, _, _ := seedPushUser(t, true, true)
		noConsent, _, _ := seedPushUser(t, false, true)
		noToken, _, _ := seedPushUser(t, true, false)
		other, _ := seedBrandUser(t, BrandOverleap)

		resp := doJSON(t, r, "POST", "/app/push/send", map[string]any{
			"batchId": fmt.Sprintf("dry-%d", consented.ID), "brand": "kaitu", "dryRun": true, "route": "/purchase",
			"userIds": []uint64{consented.ID, noConsent.ID, noToken.ID, other.ID}, "copy": copyMap,
		})
		require.Equal(t, 0, resp.Code)
		var got AdminPushDryRunResult
		require.NoError(t, json.Unmarshal(resp.Data, &got))
		assert.Equal(t, AdminPushDryRunResult{Requested: 4, Matched: 3, WithToken: 2, Consented: 1}, got)
		assert.Empty(t, h.enqueued)
	})

	t.Run("send only reaches consented users of the brand, ignores a category field", func(t *testing.T) {
		h := withPushHarness(t)
		withInlinePushBatch(t)
		r := adminPushRouter(t)
		consented, _, _ := seedPushUser(t, true, true)
		noConsent, _, _ := seedPushUser(t, false, true)
		other, _, _ := seedPushUser(t, true, true)
		require.NoError(t, db.Get().Model(&User{}).Where("id = ?", other.ID).Update("brand", "overleap").Error)
		for _, u := range []User{consented, noConsent, other} {
			cleanupPushLogs(t, u.ID)
		}
		batch := fmt.Sprintf("b-%d", consented.ID)

		resp := doJSON(t, r, "POST", "/app/push/send", map[string]any{
			"batchId": batch, "brand": "kaitu", "route": "/purchase", "category": "transactional",
			"userIds": []uint64{consented.ID, noConsent.ID, other.ID}, "copy": copyMap,
		})
		require.Equal(t, 0, resp.Code)
		require.Len(t, h.enqueued, 1)
		assert.Len(t, pushLogsOf(t, consented.ID), 1)
		assert.Empty(t, pushLogsOf(t, noConsent.ID), "category 参数被忽略，没同意的收不到")
		assert.Empty(t, pushLogsOf(t, other.ID), "别的品牌的用户不发")
		lg := pushLogsOf(t, consented.ID)[0]
		assert.Equal(t, string(PushCategoryMarketing), lg.Category)
		assert.Equal(t, batch, lg.BatchID)
		assert.Equal(t, fmt.Sprintf("push_batch:%s:%d", batch, consented.ID), lg.DedupKey)

		// 同一批次重发：不重复推
		require.Equal(t, 0, doJSON(t, r, "POST", "/app/push/send", map[string]any{
			"batchId": batch, "brand": "kaitu", "route": "/purchase",
			"userIds": []uint64{consented.ID}, "copy": copyMap,
		}).Code)
		assert.Len(t, h.enqueued, 1)

		// 批次统计
		resp = doJSON(t, r, "GET", "/app/push/batches/"+batch, nil)
		require.Equal(t, 0, resp.Code)
		var stats AdminPushBatchStats
		require.NoError(t, json.Unmarshal(resp.Data, &stats))
		assert.Equal(t, int64(1), stats.Total)
		assert.Equal(t, int64(1), stats.Queued)
	})

	t.Run("copy language fallback", func(t *testing.T) {
		withPushHarness(t)
		withInlinePushBatch(t)
		r := adminPushRouter(t)
		ja, _, _ := seedPushUser(t, true, true)
		hk, _, _ := seedPushUser(t, true, true)
		setLang(t, ja.ID, "ja")
		setLang(t, hk.ID, "zh-HK")
		cleanupPushLogs(t, ja.ID)
		cleanupPushLogs(t, hk.ID)
		batch := fmt.Sprintf("lang-%d", ja.ID)
		require.Equal(t, 0, doJSON(t, r, "POST", "/app/push/send", map[string]any{
			"batchId": batch, "brand": "kaitu", "userIds": []uint64{ja.ID, hk.ID},
			"copy": map[string]any{
				"zh-CN": map[string]string{"title": "简", "body": "b"},
				"zh-TW": map[string]string{"title": "繁", "body": "b"},
			},
		}).Code)
		assert.Equal(t, "简", pushLogsOf(t, ja.ID)[0].Title, "缺 ja → 品牌默认 zh-CN")
		assert.Equal(t, "繁", pushLogsOf(t, hk.ID)[0].Title, "zh-HK 归一到 zh-TW")
	})

	t.Run("validation", func(t *testing.T) {
		withPushHarness(t)
		r := adminPushRouter(t)
		base := func() map[string]any {
			return map[string]any{"batchId": "v", "brand": "kaitu", "userIds": []uint64{1}, "copy": copyMap}
		}
		cases := map[string]func(m map[string]any){
			"missing brand":   func(m map[string]any) { delete(m, "brand") },
			"bad brand":       func(m map[string]any) { m["brand"] = "nope" },
			"no users":        func(m map[string]any) { m["userIds"] = []uint64{} },
			"no copy":         func(m map[string]any) { m["copy"] = map[string]any{} },
			"empty body":      func(m map[string]any) { m["copy"] = map[string]any{"en-US": map[string]string{"title": "t"}} },
			"route not white": func(m map[string]any) { m["route"] = "/evil" },
			"long batch id":   func(m map[string]any) { m["batchId"] = strings.Repeat("a", 101) },
		}
		for name, mutate := range cases {
			m := base()
			mutate(m)
			assert.Equal(t, int(ErrorInvalidArgument), doJSON(t, r, "POST", "/app/push/send", m).Code, name)
		}
	})
}
