package center

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// EDM 按邮箱发送（UserID==0）：品牌必须随任务走，收件人按 (邮箱, 品牌) 只查不建。
// 回归背景：sendSingleTemplatedEmail 在 asynq 任务里调 FindOrCreateUserByEmail(ctx)，
// ctx 不是 gin.Context → 品牌回退 kaitu → 给 overleap 用户的邮箱发信会凭空造出一个
// 同邮箱的 kaitu 幽灵账号。

func edmSendTestSetup(t *testing.T) {
	t.Helper()
	skipIfNoConfig(t)
	prev := viper.GetBool("mail.dev_mode")
	viper.Set("mail.dev_mode", true) // 绝不真发信
	t.Cleanup(func() { viper.Set("mail.dev_mode", prev) })
}

// newEDMTestBatch 生成批次号并在结束时清掉该批次的发送日志（共享 dev DB）。
func newEDMTestBatch(t *testing.T) string {
	t.Helper()
	b := "t-edm-" + generateId("b")
	t.Cleanup(func() { db.Get().Unscoped().Where("batch_id = ?", b).Delete(&EmailSendLog{}) })
	return b
}

func countUsersWithEmail(t *testing.T, email string) map[string]int {
	t.Helper()
	idx := secretHashIt(context.Background(), []byte(strings.ToLower(email)))
	var ids []LoginIdentify
	require.NoError(t, db.Get().Where("type = ? AND index_id = ?", "email", idx).Find(&ids).Error)
	out := map[string]int{}
	for _, li := range ids {
		out[li.Brand]++
	}
	return out
}

func createEDMTemplateForTest(t *testing.T, brand Brand, slug string) {
	t.Helper()
	tp := &EmailMarketingTemplate{Name: "edm-send-test", Slug: slug, Language: "en-US", Subject: "hi", Content: "hello", IsActive: BoolPtr(true), Brand: string(brand)}
	require.NoError(t, db.Get().Create(tp).Error)
	t.Cleanup(func() {
		db.Get().Where("template_id = ?", tp.ID).Delete(&EmailSendLog{})
		db.Get().Unscoped().Delete(tp)
	})
}

func TestEDMSendByEmail_ResolvesWithinBrand_NoPhantomUser(t *testing.T) {
	edmSendTestSetup(t)
	email := "edm-send-o-" + generateId("t") + "@test.local"
	u := createBrandUserWithEmail(t, BrandOverleap, email)
	require.NoError(t, db.Get().Model(u).Update("language", "en-US").Error)
	slug := "edm-send-test-" + generateId("s")
	createEDMTemplateForTest(t, BrandOverleap, slug)
	t.Cleanup(func() { cleanupTestUserByEmail(t, email) })

	res, err := SendTemplatedEmails(context.Background(), &SendEmailsRequest{
		BatchID: newEDMTestBatch(t),
		Brand:   string(BrandOverleap),
		Items:   []SendEmailItem{{Email: email, Slug: slug}},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Sent, "%+v", res.Items)
	assert.Equal(t, map[string]int{"overleap": 1}, countUsersWithEmail(t, email), "must not create a kaitu phantom user")
}

func TestEDMSendByEmail_UnknownRecipientSkipped_NeverCreated(t *testing.T) {
	edmSendTestSetup(t)
	email := "edm-send-unknown-" + generateId("t") + "@test.local"
	t.Cleanup(func() { cleanupTestUserByEmail(t, email) })

	res, err := SendTemplatedEmails(context.Background(), &SendEmailsRequest{
		BatchID: newEDMTestBatch(t),
		Brand:   string(BrandOverleap),
		Items:   []SendEmailItem{{Email: email, Slug: "whatever"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Skipped, "%+v", res.Items)
	assert.Empty(t, countUsersWithEmail(t, email), "EDM send must never create users")
}

func TestEDMSendByEmail_NoBrand_FailsWithoutCreating(t *testing.T) {
	edmSendTestSetup(t)
	email := "edm-send-nobrand-" + generateId("t") + "@test.local"
	t.Cleanup(func() { cleanupTestUserByEmail(t, email) })

	res, err := SendTemplatedEmails(context.Background(), &SendEmailsRequest{
		BatchID: newEDMTestBatch(t),
		Items:   []SendEmailItem{{Email: email, Slug: "whatever"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Failed, "%+v", res.Items)
	assert.Empty(t, countUsersWithEmail(t, email))
}

func TestEDMSendByUserID_BrandMismatchFails(t *testing.T) {
	edmSendTestSetup(t)
	u := createBrandUserWithEmail(t, BrandKaitu, "")
	require.NoError(t, db.Get().Model(u).Update("language", "en-US").Error)
	slug := "edm-send-mismatch-" + generateId("s")
	createEDMTemplateForTest(t, BrandKaitu, slug) // 模板存在：没有品牌校验时这封信会真的发出去
	res, err := SendTemplatedEmails(context.Background(), &SendEmailsRequest{
		BatchID: newEDMTestBatch(t),
		Brand:   string(BrandOverleap),
		Items:   []SendEmailItem{{Email: "x@test.local", UserID: u.ID, Slug: slug}},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Failed, "an overleap-scoped batch must not mail a kaitu user: %+v", res.Items)
}

func TestAdminEDMSend_RequiresBrandForEmailOnlyItems(t *testing.T) {
	testInitConfig()
	body, _ := json.Marshal(map[string]any{
		"batchId": "t-edm-nobrand",
		"items":   []map[string]any{{"email": "a@test.local", "slug": "x"}},
	})
	c, w := brandTestGinContext(t, http.MethodPost, "/app/edm/send", "kaitu.io", body)
	api_admin_send_templated_emails(c)
	assert.Equal(t, ErrorInvalidArgument, decodeResponseCode(t, w))

	body, _ = json.Marshal(map[string]any{
		"batchId": "t-edm-badbrand", "brand": "nonsense",
		"items": []map[string]any{{"email": "a@test.local", "userId": 1, "slug": "x"}},
	})
	c, w = brandTestGinContext(t, http.MethodPost, "/app/edm/send", "kaitu.io", body)
	api_admin_send_templated_emails(c)
	assert.Equal(t, ErrorInvalidArgument, decodeResponseCode(t, w))
}

// 审批回调必须把 brand 带进异步任务 payload。
func TestEDMSendApprovalCarriesBrand(t *testing.T) {
	var req SendTemplatedEmailsHTTPRequest
	require.NoError(t, json.Unmarshal([]byte(`{"batchId":"b","brand":"overleap","items":[{"email":"a@x","slug":"s"}]}`), &req))
	got := edmSendRequestFromHTTP(&req)
	assert.Equal(t, "overleap", got.Brand)
	require.Len(t, got.Items, 1)
	assert.Equal(t, "a@x", got.Items[0].Email)
}
