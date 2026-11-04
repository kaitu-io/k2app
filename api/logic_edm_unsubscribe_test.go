package center

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/mail"
)

// ---- 单元（不碰 DB） ----

func withJWTSecret(t *testing.T, secret string) {
	t.Helper()
	prev := viper.Get("jwt.secret")
	viper.Set("jwt.secret", secret)
	t.Cleanup(func() { viper.Set("jwt.secret", prev) })
}

func TestEDMUnsubscribeToken_RoundTrip(t *testing.T) {
	withJWTSecret(t, "unit-test-secret")
	for _, id := range []uint64{1, 42, 123456789, 1<<63 + 7} {
		tok := signEDMUnsubscribeToken(id)
		require.NotEmpty(t, tok)
		got, err := parseEDMUnsubscribeToken(tok)
		require.NoError(t, err)
		assert.Equal(t, id, got)
	}
}

func TestEDMUnsubscribeToken_TamperRejected(t *testing.T) {
	withJWTSecret(t, "unit-test-secret")
	tok := signEDMUnsubscribeToken(1000) // base36 "rs"：含字母，大小写变体才有意义
	uid, sig, _ := strings.Cut(tok, ".")

	other := signEDMUnsubscribeToken(1001)
	_, otherSig, _ := strings.Cut(other, ".")
	flipped := []byte(sig)
	if flipped[0] == 'A' {
		flipped[0] = 'B'
	} else {
		flipped[0] = 'A'
	}

	bad := []string{
		"",
		uid,
		uid + ".",
		"." + sig,
		strconv.FormatUint(1001, 36) + "." + sig, // 换用户 ID，沿用原签名
		uid + "." + otherSig,                     // 别人的签名
		uid + "." + string(flipped),              // 签名被改
		uid + "." + sig + "x",                    // 尾部追加
		strings.ToUpper(uid) + "." + sig,         // 非规范形式
		"0" + uid + "." + sig,                    // 前导零
		"0.AAAAAAAAAAAAAAAAAAAAAA",
	}
	for _, b := range bad {
		_, err := parseEDMUnsubscribeToken(b)
		assert.Error(t, err, "token %q must be rejected", b)
	}

	// 换了 secret 的令牌全部失效
	withJWTSecret(t, "another-secret")
	_, err := parseEDMUnsubscribeToken(tok)
	assert.Error(t, err)
}

func TestEDMUnsubscribeToken_NoSecretRefuses(t *testing.T) {
	withJWTSecret(t, "unit-test-secret")
	tok := signEDMUnsubscribeToken(42)
	withJWTSecret(t, "")
	assert.Empty(t, signEDMUnsubscribeToken(42))
	_, err := parseEDMUnsubscribeToken(tok)
	assert.Error(t, err)
	_, ok := edmTemplateVars(&User{ID: 42, Brand: "kaitu"}, nil)
	assert.False(t, ok, "no secret → EDM must not go out without a working unsubscribe link")
}

func TestEDMUnsubscribeToken_DoesNotExposeEmail(t *testing.T) {
	withJWTSecret(t, "unit-test-secret")
	u := &User{ID: 42, Brand: string(BrandKaitu)}
	link := edmUnsubscribeURL(u)
	assert.NotContains(t, link, "@")
	assert.NotContains(t, link, "%40")
}

// 宿主必须属于用户品牌（跨层不变量是宿主归属，不是字符串相等）。
func TestEDMUnsubscribeURL_HostBelongsToUserBrand(t *testing.T) {
	withJWTSecret(t, "unit-test-secret")
	for _, b := range AllBrands() {
		link := edmUnsubscribeURL(&User{ID: 7, Brand: string(b)})
		u, err := url.Parse(link)
		require.NoError(t, err)
		assert.Equal(t, "https", u.Scheme)
		assert.Contains(t, b.Config().Hosts, u.Hostname(), "brand %s", b)
		assert.Equal(t, edmUnsubscribePath, u.Path)
		id, err := parseEDMUnsubscribeToken(u.Query().Get("t"))
		require.NoError(t, err)
		assert.Equal(t, uint64(7), id)
	}
}

func TestEDMTemplateVars_InjectsUnsubscribeURLAndKeepsCallerVars(t *testing.T) {
	withJWTSecret(t, "unit-test-secret")
	caller := map[string]string{"CampaignCode": "BACK10", edmUnsubscribeVar: "https://evil.example/x"}
	vars, ok := edmTemplateVars(&User{ID: 9, Brand: string(BrandKaitu)}, caller)
	require.True(t, ok)
	assert.Equal(t, "BACK10", vars["CampaignCode"])
	assert.True(t, strings.HasPrefix(vars[edmUnsubscribeVar], BrandKaitu.Config().BaseURL+edmUnsubscribePath+"?t="),
		"system UnsubscribeURL must override a caller-supplied one: %s", vars[edmUnsubscribeVar])
	assert.Equal(t, "https://evil.example/x", caller[edmUnsubscribeVar], "caller map must not be mutated")

	vars, ok = edmTemplateVars(&User{ID: 9, Brand: string(BrandKaitu)}, nil)
	require.True(t, ok)
	assert.NotEmpty(t, vars[edmUnsubscribeVar])
}

// 页面文案：品牌名只能来自 BrandConfig；开途页面中文且不出现裸词 Kaitu。
func TestEDMUnsubCopy_BrandLiteralsFromRegistry(t *testing.T) {
	for _, cp := range []edmUnsubCopy{edmUnsubCopyKaitu, edmUnsubCopyOverleap} {
		all := strings.Join([]string{cp.ConfirmTitle, cp.ConfirmBody, cp.Button, cp.DoneTitle, cp.DoneBody,
			cp.InvalidTitle, cp.InvalidBody, cp.ErrorTitle, cp.ErrorBody}, "\n")
		for _, b := range AllBrands() {
			cfg := b.Config()
			for _, lit := range []string{cfg.DisplayName, cfg.SupportEmail, "Kaitu", "kaitu", "Overleap", "overleap", "开途"} {
				assert.NotContains(t, all, lit, "brand literal %q hardcoded in page copy", lit)
			}
		}
	}
	assert.Equal(t, "zh-CN", edmUnsubCopyFor(BrandKaitu).Lang)
	assert.Equal(t, "en", edmUnsubCopyFor(BrandOverleap).Lang)
}

// 结构守卫：sendEmail（EDM 通道）只能由 sendSingleTemplatedEmail 调用——退订过滤与 UnsubscribeURL 注入
// 都在那里。新 EDM 链路如果直接调 sendEmail，会绕开退订；这个测试会红。
func TestEDMSendEmail_SingleCaller(t *testing.T) {
	files, err := os.ReadDir(".")
	require.NoError(t, err)
	call := regexp.MustCompile(`\bsendEmail\(`)
	var callers []string
	for _, f := range files {
		n := f.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		src, err := os.ReadFile(n)
		require.NoError(t, err)
		for i, line := range strings.Split(string(src), "\n") {
			if call.MatchString(line) && !strings.Contains(line, "func sendEmail(") && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				callers = append(callers, n+":"+strconv.Itoa(i+1))
			}
		}
	}
	require.Len(t, callers, 1, "EDM sendEmail must only be called from sendSingleTemplatedEmail: %v", callers)
	assert.True(t, strings.HasPrefix(callers[0], "logic_email_send.go:"), "%v", callers)
}

// ---- 集成（真实 DB） ----

func edmUnsubRequest(t *testing.T, method, rawQuery, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := SetupRouter()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, edmUnsubscribePath+"?"+rawQuery, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, edmUnsubscribePath+"?"+rawQuery, nil)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func reloadEDMOptOut(t *testing.T, id uint64) User {
	t.Helper()
	var u User
	require.NoError(t, db.Get().Select("id", "edm_opt_out", "edm_opt_out_at").First(&u, id).Error)
	return u
}

func TestEDMUnsubscribe_GETDoesNotOptOut(t *testing.T) {
	skipIfNoConfig(t)
	u := createBrandUserWithEmail(t, BrandKaitu, "")
	tok := signEDMUnsubscribeToken(u.ID)
	require.NotEmpty(t, tok)

	w := edmUnsubRequest(t, http.MethodGet, "t="+url.QueryEscape(tok), "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	body := w.Body.String()
	assert.Contains(t, body, `method="post"`)
	assert.Contains(t, body, "开途")
	assert.NotContains(t, body, "Kaitu")
	assert.NotContains(t, body, "kaitu")
	assert.False(t, reloadEDMOptOut(t, u.ID).EdmOptOut, "GET must never opt out (link scanners prefetch GETs)")
}

func TestEDMUnsubscribe_POSTOptsOutIdempotent(t *testing.T) {
	skipIfNoConfig(t)
	u := createBrandUserWithEmail(t, BrandKaitu, "")
	tok := signEDMUnsubscribeToken(u.ID)

	w := edmUnsubRequest(t, http.MethodPost, "t="+url.QueryEscape(tok), "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "已退订")
	got := reloadEDMOptOut(t, u.ID)
	require.True(t, got.EdmOptOut)
	require.NotZero(t, got.EdmOptOutAt)

	// 首次时刻保留
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", u.ID).Update("edm_opt_out_at", 1000).Error)
	w = edmUnsubRequest(t, http.MethodPost, "t="+url.QueryEscape(tok), "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "已退订")
	got = reloadEDMOptOut(t, u.ID)
	assert.True(t, got.EdmOptOut)
	assert.Equal(t, int64(1000), got.EdmOptOutAt, "second POST must not move the opt-out timestamp")

	// 已退订后 GET 直接显示已退订，不再给按钮
	w = edmUnsubRequest(t, http.MethodGet, "t="+url.QueryEscape(tok), "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), `method="post"`)
}

func TestEDMUnsubscribe_RFC8058OneClick_Overleap(t *testing.T) {
	skipIfNoConfig(t)
	u := createBrandUserWithEmail(t, BrandOverleap, "")
	tok := signEDMUnsubscribeToken(u.ID)

	w := edmUnsubRequest(t, http.MethodPost, "t="+url.QueryEscape(tok), "List-Unsubscribe=One-Click")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := w.Body.String()
	assert.Contains(t, body, "Overleap")
	assert.Contains(t, body, "unsubscribed")
	assert.NotContains(t, body, "开途")
	assert.True(t, reloadEDMOptOut(t, u.ID).EdmOptOut)
}

func TestEDMUnsubscribe_InvalidToken400NoLeak(t *testing.T) {
	skipIfNoConfig(t)
	u := createBrandUserWithEmail(t, BrandKaitu, "edm-unsub-leak-"+generateId("t")+"@test.local")
	tok := signEDMUnsubscribeToken(u.ID)
	uid, _, _ := strings.Cut(tok, ".")

	for _, q := range []string{"", "t=", "t=garbage", "t=" + uid + ".AAAAAAAAAAAAAAAAAAAAAA",
		"t=" + url.QueryEscape(signEDMUnsubscribeToken(1<<62)) /* 签名合法但用户不存在 */} {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			w := edmUnsubRequest(t, m, q, "")
			assert.Equal(t, http.StatusBadRequest, w.Code, "%s ?%s", m, q)
			assert.NotContains(t, w.Body.String(), "@test.local")
			assert.NotContains(t, w.Body.String(), `method="post"`)
		}
	}
	assert.False(t, reloadEDMOptOut(t, u.ID).EdmOptOut)
}

// EDM 发送：已退订用户 skipped + 日志 error_msg=edm_opt_out；未退订用户照发且模板里的
// {{.UnsubscribeURL}} 被渲染成该用户的链接（调用方没传这个变量）。
func TestEDMSend_SkipsOptedOutAndInjectsUnsubscribeURL(t *testing.T) {
	skipIfNoConfig(t)
	prevDev := viper.Get("mail.dev_mode")
	viper.Set("mail.dev_mode", false) // 走到 edmChannelSend 接缝以捕获正文；接缝已替换，不会真发
	t.Cleanup(func() { viper.Set("mail.dev_mode", prevDev) })
	var sent []*mail.Message
	prevSend := edmChannelSend
	edmChannelSend = func(_ *mail.Sender, msg *mail.Message) error { sent = append(sent, msg); return nil }
	t.Cleanup(func() { edmChannelSend = prevSend })

	slug := "edm-unsub-test-" + generateId("s")
	tp := &EmailMarketingTemplate{Name: "edm-unsub-test", Slug: slug, Language: "en-US", Subject: "hi",
		Content: "hello\n退订：{{.UnsubscribeURL}}", IsActive: BoolPtr(true), Brand: string(BrandKaitu)}
	require.NoError(t, db.Get().Create(tp).Error)
	t.Cleanup(func() {
		db.Get().Where("template_id = ?", tp.ID).Delete(&EmailSendLog{})
		db.Get().Unscoped().Delete(tp)
	})

	active := createBrandUserWithEmail(t, BrandKaitu, "")
	optedOut := createBrandUserWithEmail(t, BrandKaitu, "")
	for _, u := range []*User{active, optedOut} {
		require.NoError(t, db.Get().Model(u).Update("language", "en-US").Error)
	}
	require.NoError(t, optOutEDM(context.Background(), optedOut.ID, time.Now()))

	batch := newEDMTestBatch(t)
	req := &SendEmailsRequest{BatchID: batch, Brand: string(BrandKaitu), Items: []SendEmailItem{
		{Email: "edm-active@test.local", UserID: active.ID, Slug: slug},
		{Email: "edm-optout@test.local", UserID: optedOut.ID, Slug: slug},
	}}
	res, err := SendTemplatedEmails(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Sent, "%+v", res.Items)
	assert.Equal(t, 1, res.Skipped, "%+v", res.Items)

	require.Len(t, sent, 1)
	assert.Equal(t, "edm-active@test.local", sent[0].To)
	assert.Contains(t, sent[0].Body, edmUnsubscribeURL(active), "UnsubscribeURL must be rendered with the recipient's own link")

	var logs []EmailSendLog
	require.NoError(t, db.Get().Where("batch_id = ? AND user_id = ?", batch, optedOut.ID).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, EmailSendLogStatusSkipped, logs[0].Status)
	require.NotNil(t, logs[0].ErrorMsg)
	assert.Equal(t, edmOptOutSkipReason, *logs[0].ErrorMsg)

	// 同批次重跑：不重复落 skipped 行，也不发
	res, err = SendTemplatedEmails(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Sent)
	assert.Len(t, sent, 1)
	var n int64
	db.Get().Model(&EmailSendLog{}).Where("batch_id = ? AND user_id = ?", batch, optedOut.ID).Count(&n)
	assert.Equal(t, int64(1), n)
}

// 事务邮件（mail 通道：验证码、账户通知）完全不看 edm_opt_out。
func TestEDMOptOut_TransactionalMailUnaffected(t *testing.T) {
	skipIfNoConfig(t)
	email := "edm-unsub-tx-" + generateId("t") + "@test.local"
	u := createBrandUserWithEmail(t, BrandKaitu, email)
	require.NoError(t, optOutEDM(context.Background(), u.ID, time.Now()))

	prevDev := viper.Get("mail.dev_mode")
	viper.Set("mail.dev_mode", true)
	t.Cleanup(func() { viper.Set("mail.dev_mode", prevDev) })
	var to []string
	systemMailToHook = func(addr string) { to = append(to, addr) }
	t.Cleanup(func() { systemMailToHook = nil })

	err := emailToUser(context.Background(), int64(u.ID), brandedVerificationCodeTemplate,
		VerificationCodeMeta{UserEmail: email, Code: "123456", ExpireMinutes: 5})
	require.NoError(t, err)
	require.NoError(t, emailVerificationCode(context.Background(), BrandKaitu, email,
		VerificationCodeMeta{UserEmail: email, Code: "123456", ExpireMinutes: 5}))
	assert.Equal(t, []string{email, email}, to, "opted-out user must still receive transactional mail")
}
