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
	t.Cleanup(func() {
		db.Get().Where("action = ?", "push_send").Where("requestor_id = ?", admin.ID).Delete(&AdminApproval{})
	})
	return r
}

// withInlinePushBatch 让批次任务在请求内同步跑完（替换 asynq 入队）。
func withInlinePushBatch(t *testing.T) {
	t.Helper()
	prev := enqueuePushBatch
	enqueuePushBatch = func(ctx context.Context, req AdminPushSendRequest) error {
		_ = runAdminPushBatch(ctx, req)
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
			"batch id chars":  func(m map[string]any) { m["batchId"] = "promo/11" },
			"too many users": func(m map[string]any) {
				ids := make([]uint64, adminPushMaxUsers+1)
				for i := range ids {
					ids[i] = uint64(i + 1)
				}
				m["userIds"] = ids
			},
			"params overflow approval column": func(m map[string]any) {
				ids := make([]uint64, adminPushMaxUsers)
				for i := range ids {
					ids[i] = uint64(1_000_000 + i)
				}
				m["userIds"] = ids
				long := map[string]any{}
				for _, lang := range []string{"zh-CN", "zh-TW", "en-US", "ja", "ko", "de", "fr", "es", "ru", "vi",
					"ar", "fa", "id", "it", "km", "ms", "my", "pt-BR", "th", "tr"} {
					long[lang] = map[string]string{"title": strings.Repeat("标", 100), "body": strings.Repeat("题", 500)}
				}
				m["copy"] = long
			},
		}
		for name, mutate := range cases {
			m := base()
			mutate(m)
			resp := doJSON(t, r, "POST", "/app/push/send", m)
			assert.Equal(t, int(ErrorInvalidArgument), resp.Code, "%s: %s", name, resp.Message)
		}
	})
}

// 接近字节上限的请求必须真的能写进 admin_approvals（真实 INSERT + 审批执行），不只是算出来放得下。
func TestAdminPushSend_NearLimitRequestIsStored(t *testing.T) {
	skipIfNoConfig(t)
	withPushHarness(t)
	withInlinePushBatch(t)
	r := adminPushRouter(t)
	ids := make([]uint64, adminPushMaxUsers)
	for i := range ids {
		ids[i] = uint64(1_000_000 + i)
	}
	cp := map[string]any{}
	for _, lang := range []string{"zh-CN", "zh-TW", "en-US", "ja", "ko", "de", "fr", "es", "ru", "vi"} {
		cp[lang] = map[string]string{"title": strings.Repeat("标", 100), "body": strings.Repeat("题", 500)}
	}
	body := map[string]any{"batchId": fmt.Sprintf("near-limit-%d", ids[0]), "brand": "kaitu", "userIds": ids, "copy": cp}
	raw, _ := json.Marshal(body)
	require.Greater(t, len(raw), 55000, "确实贴近上限")
	resp := doJSON(t, r, "POST", "/app/push/send", body)
	require.Equal(t, 0, resp.Code, resp.Message)
}

// 上限必须真的可达：5000 个 7 位 id + 两种语言满长文案，序列化后仍放得进 admin_approvals.params。
func TestAdminPushSendRequest_CapIsReachable(t *testing.T) {
	ids := make([]uint64, adminPushMaxUsers)
	for i := range ids {
		ids[i] = uint64(9_000_000 + i)
	}
	req := AdminPushSendRequest{
		BatchID: "promo:2026-11.v1", Brand: "kaitu", UserIDs: ids,
		Copy: map[string]AdminPushCopy{
			"zh-CN": {Title: strings.Repeat("标", adminPushMaxTitleLength), Body: strings.Repeat("题", adminPushMaxBodyLength)},
			"en-US": {Title: strings.Repeat("t", adminPushMaxTitleLength), Body: strings.Repeat("b", adminPushMaxBodyLength)},
		},
	}
	require.NoError(t, req.validate())
	b, _ := json.Marshal(req)
	assert.Less(t, len(b), 65535, "TEXT 列上限")
}

func TestRunAdminPushBatch_InterruptedBatchRetriesAndResumes(t *testing.T) {
	skipIfNoConfig(t)
	h := withPushHarness(t)
	a, _, _ := seedPushUser(t, true, true)
	b, _, _ := seedPushUser(t, true, true)
	cleanupPushLogs(t, a.ID)
	cleanupPushLogs(t, b.ID)
	req := AdminPushSendRequest{
		BatchID: fmt.Sprintf("resume-%d", a.ID), Brand: "kaitu", UserIDs: []uint64{a.ID, b.ID},
		Copy: map[string]AdminPushCopy{"zh-CN": {Title: "t", Body: "b"}},
	}
	require.NoError(t, req.validate())

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Error(t, runAdminPushBatch(cancelled, req), "被中断要返回 error 让 asynq 重试")
	assert.Empty(t, h.enqueued)

	require.NoError(t, runAdminPushBatch(context.Background(), req))
	assert.Len(t, h.enqueued, 2)
	require.NoError(t, runAdminPushBatch(context.Background(), req), "重跑安全")
	assert.Len(t, h.enqueued, 2, "已入队的人不会重复")

	// 有人入队失败：返回 error 让整批重试，重试只补这一个人
	c, _, _ := seedPushUser(t, true, true)
	cleanupPushLogs(t, c.ID)
	req.UserIDs = append(req.UserIDs, c.ID)
	enqueuePushTask = func(context.Context, uint64) error { return fmt.Errorf("redis down") }
	assert.Error(t, runAdminPushBatch(context.Background(), req))
	enqueuePushTask = func(_ context.Context, id uint64) error { h.enqueued = append(h.enqueued, id); return nil }
	require.NoError(t, runAdminPushBatch(context.Background(), req))
	assert.Len(t, h.enqueued, 3)
}
