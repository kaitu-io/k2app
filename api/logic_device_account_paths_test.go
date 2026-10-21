package center

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

// 零身份（设备账号）用户会撞上的现有路径（spec §4.3 / §4.4 / §4.6.1 / §4.7）。

// dapUser 建一个开途用户；email 非空则带 email 身份（真实加密），password 非空则设密码。
func dapUser(t *testing.T, email, password string) User {
	t.Helper()
	ctx := context.Background()
	u := User{UUID: generateId("user"), Brand: string(BrandKaitu), Tier: TierBasic, Language: "zh-CN", IsActivated: BoolPtr(true)}
	if password != "" {
		hash, err := UserPasswordHash(password)
		require.NoError(t, err)
		u.PasswordHash = hash
	}
	require.NoError(t, db.Get().Create(&u).Error)
	t.Cleanup(func() {
		funnelFlushForTest()
		db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&Device{})
		db.Get().Where("user_id = ?", u.ID).Delete(&LoginIdentify{})
		db.Get().Where("user_id = ?", u.ID).Delete(&FunnelIdentity{})
		db.Get().Where("user_id = ?", u.ID).Delete(&FunnelEvent{})
		db.Get().Where("user_id = ?", u.ID).Delete(&FeedbackTicket{})
		db.Get().Unscoped().Delete(&User{}, u.ID)
	})
	if email != "" {
		enc, err := secretEncryptString(ctx, email)
		require.NoError(t, err)
		require.NoError(t, db.Get().Create(&LoginIdentify{UserID: u.ID, Type: "email",
			IndexID: secretHashIt(ctx, []byte(email)), EncryptedValue: enc, Brand: string(BrandKaitu)}).Error)
	}
	return u
}

func dapEmail(tag string) string {
	return fmt.Sprintf("dap-%s-%d@example.com", tag, time.Now().UnixNano())
}

func dapDevice(t *testing.T, userID uint64, udid string) {
	t.Helper()
	require.NoError(t, db.Get().Create(&Device{UDID: udid, UserID: userID, Remark: "iPhone", TokenIssueAt: 1, TokenLastUsedAt: 1}).Error)
	t.Cleanup(func() { db.Get().Unscoped().Where("udid = ?", udid).Delete(&Device{}) })
}

// dapMailSpy 记录每个收件用户被尝试发信的次数（resolveUserMailTarget 入口，emailToUser 的唯一取数处）。
func dapMailSpy(t *testing.T) func(userID uint64) int {
	t.Helper()
	var mu sync.Mutex
	counts := map[int64]int{}
	prev := mailTargetHook
	mailTargetHook = func(userID int64) {
		mu.Lock()
		counts[userID]++
		mu.Unlock()
	}
	t.Cleanup(func() { mailTargetHook = prev })
	prevDev := viper.GetBool("mail.dev_mode")
	viper.Set("mail.dev_mode", true) // 绝不真发信
	t.Cleanup(func() { viper.Set("mail.dev_mode", prevDev) })
	return func(userID uint64) int {
		mu.Lock()
		defer mu.Unlock()
		return counts[int64(userID)]
	}
}

// dapFunnelSpy 换成不消费的队列，返回取出指定事件的函数。
func dapFunnelSpy(t *testing.T) func(event string) []FunnelEvent {
	t.Helper()
	orig := funnelQ
	q := newFunnelQueue(256)
	q.startOnce.Do(func() {})
	// 不消费事件，但应答 flush（dapUser 的清理会 funnelFlushForTest，否则死等）。
	done := make(chan struct{})
	go func() {
		for {
			select {
			case ack := <-q.flushReq:
				close(ack)
			case <-done:
				return
			}
		}
	}()
	funnelQ = q
	t.Cleanup(func() { funnelQ = orig; close(done) })
	var all []FunnelEvent
	return func(event string) []FunnelEvent {
		for len(q.ch) > 0 {
			all = append(all, <-q.ch)
		}
		var got []FunnelEvent
		for _, ev := range all {
			if ev.Event == event {
				got = append(got, ev)
			}
		}
		return got
	}
}

func dapAs(u *User, udid string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: u.ID, UDID: udid, User: u})
		c.Next()
	}
}

func dapPost(t *testing.T, r *gin.Engine, path string, body any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return respCode(t, w)
}

func dapDB(t *testing.T) {
	t.Helper()
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	testInitConfig()
	gin.SetMode(gin.TestMode)
}

func dapDeviceCount(t *testing.T, udid string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Get().Model(&Device{}).Where("udid = ?", udid).Count(&n).Error)
	return n
}

// 零身份登出不删设备行、只作废 token；/device-account 不凭 udid 重签（403006），账号经 Apple 恢复拿回。
// 有身份用户登出照旧删设备行。
func TestLogout_ZeroIdentityKeepsDevice(t *testing.T) {
	r := devAcctRouter(t)
	udid := devAcctUDID(t)
	require.Equal(t, 0, postDeviceAccount(t, r, "www.kaitu.io", udid).Code)
	d := devAcctDevice(t, udid)
	var zero User
	require.NoError(t, db.Get().First(&zero, d.UserID).Error)

	r.POST("/zero/logout", dapAs(&zero, udid), api_logout)
	require.Equal(t, 0, dapPost(t, r, "/zero/logout", map[string]any{}))
	require.Equal(t, int64(1), dapDeviceCount(t, udid), "零身份登出保留设备行")
	require.NotEqual(t, d.TokenIssueAt, devAcctDevice(t, udid).TokenIssueAt,
		"零身份登出必须推进 token_issue_at，使已签发 token 被 TokenIssueAt 校验拒绝")

	again := postDeviceAccount(t, r, "www.kaitu.io", udid)
	require.Equal(t, int(ErrorDeviceAccountProofRequired), again.Code, "登出后不能凭 udid 拿回 token")
	require.Empty(t, again.Data.AccessToken)
	require.Equal(t, zero.ID, devAcctDevice(t, udid).UserID, "设备行仍属原账号，可经 Apple 恢复拿回")

	ident := dapUser(t, dapEmail("logout"), "")
	udid2 := fmt.Sprintf("dap-logout-%d", time.Now().UnixNano())
	dapDevice(t, ident.ID, udid2)
	r.POST("/ident/logout", dapAs(&ident, udid2), api_logout)
	require.Equal(t, 0, dapPost(t, r, "/ident/logout", map[string]any{}))
	require.Equal(t, int64(0), dapDeviceCount(t, udid2), "有身份用户登出照旧删设备行")
}

// 零身份用户的 udid 被邮箱 / 密码登录转走：不给原主（无邮箱）发设备转移邮件；有身份原主照旧发。
func TestLogin_TransferFromZeroIdentity_NoMail(t *testing.T) {
	dapDB(t)
	EnableMockVerificationCode = true
	t.Cleanup(func() { EnableMockVerificationCode = false })
	mails := dapMailSpy(t)

	const password = "k7N#mq2P!xT9"
	r := gin.New()
	r.POST("/api/auth/login", api_login)
	r.POST("/api/auth/login/password", api_password_login)

	for _, mode := range []string{"code", "password"} {
		t.Run(mode, func(t *testing.T) {
			email := dapEmail("xfer-" + mode)
			me := dapUser(t, email, password)
			login := func(udid string) int {
				if mode == "code" {
					return dapPost(t, r, "/api/auth/login", map[string]string{
						"email": email, "verificationCode": MockVerificationCode, "udid": udid})
				}
				return dapPost(t, r, "/api/auth/login/password", map[string]string{
					"email": email, "password": password, "udid": udid})
			}

			zero := dapUser(t, "", "")
			udidZ := fmt.Sprintf("dap-xfer-z-%s-%d", mode, time.Now().UnixNano())
			dapDevice(t, zero.ID, udidZ)
			require.Equal(t, 0, login(udidZ))
			assert.Equal(t, me.ID, devAcctDevice(t, udidZ).UserID, "设备转给登录账号")
			assert.Equal(t, 0, mails(zero.ID), "零身份原主不发转移邮件")

			owner := dapUser(t, dapEmail("owner-"+mode), "")
			udidO := fmt.Sprintf("dap-xfer-o-%s-%d", mode, time.Now().UnixNano())
			dapDevice(t, owner.ID, udidO)
			require.Equal(t, 0, login(udidO))
			assert.Equal(t, 1, mails(owner.ID), "有身份原主照旧发转移邮件")
		})
	}
}

// 踢设备邮件：零身份用户不发；有身份用户照旧发。
func TestDeviceKick_ZeroIdentity_NoMail(t *testing.T) {
	dapDB(t)
	mails := dapMailSpy(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()

	zero := dapUser(t, "", "")
	dapDevice(t, zero.ID, fmt.Sprintf("dap-kick-z-%d", stamp))
	require.NoError(t, checkDeviceLimitOrKick(ctx, db.Get(), &zero, false))
	require.Equal(t, int64(0), dapDeviceCount(t, fmt.Sprintf("dap-kick-z-%d", stamp)), "前提：零身份已踢")
	assert.Equal(t, 0, mails(zero.ID), "零身份不发踢设备邮件")

	ident := dapUser(t, dapEmail("kick"), "")
	for i := 0; i < ident.Quota().MaxDevice; i++ {
		dapDevice(t, ident.ID, fmt.Sprintf("dap-kick-i-%d-%d", stamp, i))
	}
	require.NoError(t, checkDeviceLimitOrKick(ctx, db.Get(), &ident, false))
	assert.Equal(t, 1, mails(ident.ID), "有身份用户照旧发踢设备邮件")
}

// 零身份用户绑邮箱 → 有 email 身份 + identity_bound 一次；已有身份的用户换邮箱 → 不发。
func TestUpdateEmail_ZeroIdentityBindsAndEmitsIdentityBound(t *testing.T) {
	dapDB(t)
	EnableMockVerificationCode = true
	t.Cleanup(func() { EnableMockVerificationCode = false })
	events := dapFunnelSpy(t)

	zero := dapUser(t, "", "")
	r := gin.New()
	r.POST("/zero/email", dapAs(&zero, ""), api_update_login_email)
	require.Equal(t, 0, dapPost(t, r, "/zero/email", map[string]string{
		"email": dapEmail("bind"), "verificationCode": MockVerificationCode}))
	has, err := userHasIdentity(db.Get(), zero.ID)
	require.NoError(t, err)
	require.True(t, has, "绑上 email 身份")
	got := events("identity_bound")
	require.Len(t, got, 1)
	assert.Equal(t, zero.ID, got[0].UserID)
	assert.Equal(t, FunnelSurfaceApp, got[0].Surface)
	assert.Equal(t, string(BrandKaitu), got[0].Brand)

	ident := dapUser(t, dapEmail("old"), "")
	r.POST("/ident/email", dapAs(&ident, ""), api_update_login_email)
	require.Equal(t, 0, dapPost(t, r, "/ident/email", map[string]string{
		"email": dapEmail("new"), "verificationCode": MockVerificationCode}))
	assert.Empty(t, events("identity_bound")[1:], "换邮箱不发 identity_bound")
}

// 零身份用户绑第三方身份（首个身份）→ identity_bound 一次；已有身份的用户再绑 → 不发。
func TestBindSocial_ZeroIdentityEmitsIdentityBound(t *testing.T) {
	dapDB(t)
	events := dapFunnelSpy(t)
	ctx := context.Background()
	sfx := socialTestSuffix()

	zero := dapUser(t, "", "")
	require.NoError(t, bindSocialIdentity(ctx, BrandKaitu, zero.ID, &socialClaims{
		Provider: SocialProviderGoogle, Sub: "dap-g-zero-" + sfx, Email: dapEmail("g"), EmailTrusted: true}))
	assert.Equal(t, map[string]int{"email": 1, "google": 1}, identitiesOfUser(t, zero.ID))
	got := events("identity_bound")
	require.Len(t, got, 1)
	assert.Equal(t, zero.ID, got[0].UserID)

	// 幂等重绑同一身份：不再发
	require.NoError(t, bindSocialIdentity(ctx, BrandKaitu, zero.ID, &socialClaims{
		Provider: SocialProviderGoogle, Sub: "dap-g-zero-" + sfx, Email: dapEmail("g"), EmailTrusted: true}))
	// 已有身份的用户绑 Apple：不发
	require.NoError(t, bindSocialIdentity(ctx, BrandKaitu, zero.ID, &socialClaims{
		Provider: SocialProviderApple, Sub: "dap-a-zero-" + sfx}))
	assert.Len(t, events("identity_bound"), 1, "只有首个身份发 identity_bound")
}

// 零身份用户提工单：用 req.Email 作联系邮箱，不新建 email 身份；不带 email → 400。
func TestTicket_ZeroIdentityUsesContactEmail(t *testing.T) {
	dapDB(t)
	dapMailSpy(t)
	zero := dapUser(t, "", "")
	r := gin.New()
	r.POST("/ticket", dapAs(&zero, ""), api_create_ticket)

	contact := dapEmail("contact")
	fid := fmt.Sprintf("dap-fb-%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Get().Where("feedback_id = ?", fid).Delete(&FeedbackTicket{}) })
	// AutoGenerated：不发 Slack / 邮件，只落库（本测试只关心联系邮箱的解析）。
	require.Equal(t, 0, dapPost(t, r, "/ticket", map[string]any{
		"content": "连不上", "email": contact, "feedbackId": fid, "auto_generated": true}))
	var tk FeedbackTicket
	require.NoError(t, db.Get().Where("feedback_id = ?", fid).First(&tk).Error)
	assert.Equal(t, contact, tk.Email, "回复邮箱 = req.Email")
	require.NotNil(t, tk.UserID)
	assert.Equal(t, zero.ID, *tk.UserID)
	has, err := userHasIdentity(db.Get(), zero.ID)
	require.NoError(t, err)
	assert.False(t, has, "联系邮箱不绑定为登录身份")

	assert.Equal(t, int(ErrorInvalidArgument), dapPost(t, r, "/ticket", map[string]any{"content": "连不上"}))
}

// 订阅链属零身份用户 D，用户 U2 verify 同链交易 → 409004，订阅仍属 D，无 credit 行。
// 新链但 token 属 D（D 尚未 verify），U2 verify → 同样 409004。
func TestAppleVerify_OwnedElsewhere409004(t *testing.T) {
	trialDB(t)
	setTestAppleBundleID(t)
	txns := map[string]*appstore.TransactionInfo{}
	fakeAppleTxn(t, func(id string) (*appstore.TransactionInfo, error) {
		info, ok := txns[id]
		if !ok {
			return nil, fmt.Errorf("unknown txn %s", id)
		}
		x := *info
		return &x, nil
	})
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, creditInfo(t, f.buyer.ID, f.paidTxn("DA-OE1", t0, t0+30*aDay)))

	u2 := dapUser(t, dapEmail("u2"), "")
	t.Cleanup(func() {
		db.Get().Unscoped().Where("user_id = ?", u2.ID).Delete(&SubscriptionCredit{})
		db.Get().Unscoped().Where("user_id = ?", u2.ID).Delete(&Subscription{})
		db.Get().Unscoped().Where("user_id = ?", u2.ID).Delete(&UserProHistory{})
		db.Get().Unscoped().Where("user_id = ?", u2.ID).Delete(&Order{})
	})
	r := gin.New()
	r.POST("/verify", dapAs(&u2, ""), api_apple_iap_verify)

	// ① 已绑定的链（"already bound"）
	renew := f.paidTxn("DA-OE2", t0+30*aDay, t0+60*aDay)
	renew.AppAccountToken = deriveAppleAccountToken(u2.UUID)
	txns["DA-OE2"] = renew
	assert.Equal(t, int(ErrorAppleSubscriptionOwnedElsewhere), dapPost(t, r, "/verify", map[string]string{"transactionId": "DA-OE2"}))
	assert.Equal(t, f.buyer.ID, f.subNow(t).UserID, "订阅仍属 D")
	var n int64
	require.NoError(t, db.Get().Model(&SubscriptionCredit{}).Where("transaction_id = ?", "DA-OE2").Count(&n).Error)
	assert.Zero(t, n, "无 credit 行")

	// ② 首笔 token 属别的账号（"appAccountToken mismatch"）
	f2 := setupIAPOrderFixture(t, 30, 10)
	f2.noInvite(t)
	txns["DA-OE3"] = f2.paidTxn("DA-OE3", t0, t0+30*aDay)
	assert.Equal(t, int(ErrorAppleSubscriptionOwnedElsewhere), dapPost(t, r, "/verify", map[string]string{"transactionId": "DA-OE3"}))
	require.NoError(t, db.Get().Model(&SubscriptionCredit{}).Where("transaction_id = ?", "DA-OE3").Count(&n).Error)
	assert.Zero(t, n, "无 credit 行")
}

// App 路线「建号或登录」= device_account_created ∪ auth_done；末步「绑定身份」作后置观察。
func TestFunnelPaths_AppLoginStepIncludesDeviceAccount(t *testing.T) {
	p, ok := funnelPathByKey("app_activation")
	require.True(t, ok)
	require.GreaterOrEqual(t, len(p.Steps), 2)
	login := p.Steps[1]
	assert.Equal(t, "建号或登录", login.Label)
	assert.ElementsMatch(t, []string{"auth_done", "device_account_created"}, login.Events)
	assert.Equal(t, FunnelSurfaceApp, login.Surface)
	// identity_bound 只是注册事件，不是路线步骤：安卓 / 桌面 / 邮箱用户永远不发，当步骤会改写「走完」的含义。
	for i, st := range p.Steps {
		assert.NotContains(t, st.Events, "identity_bound", "step %d", i)
	}
	assert.Equal(t, "连上", p.Steps[len(p.Steps)-1].Label)
	assert.True(t, funnelEventAllowed("identity_bound", FunnelSurfaceApp), "事件仍注册")
}

// 只有第三方身份（无 email 身份）的用户提工单：同样用 req.Email；不带 → 400。
func TestTicket_SocialOnlyUsesContactEmail(t *testing.T) {
	dapDB(t)
	dapMailSpy(t)
	u := dapUser(t, "", "")
	require.NoError(t, db.Get().Create(&LoginIdentify{UserID: u.ID, Type: SocialProviderGoogle,
		IndexID: fmt.Sprintf("dap-g-%d", time.Now().UnixNano()), Brand: string(BrandKaitu)}).Error)
	r := gin.New()
	r.POST("/ticket", dapAs(&u, ""), api_create_ticket)

	contact := dapEmail("social-contact")
	fid := fmt.Sprintf("dap-fb-s-%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Get().Where("feedback_id = ?", fid).Delete(&FeedbackTicket{}) })
	require.Equal(t, 0, dapPost(t, r, "/ticket", map[string]any{
		"content": "连不上", "email": contact, "feedbackId": fid, "auto_generated": true}))
	var tk FeedbackTicket
	require.NoError(t, db.Get().Where("feedback_id = ?", fid).First(&tk).Error)
	assert.Equal(t, contact, tk.Email)
	var n int64
	require.NoError(t, db.Get().Model(&LoginIdentify{}).Where("user_id = ? AND type = ?", u.ID, "email").Count(&n).Error)
	assert.Zero(t, n, "联系邮箱不绑定为登录身份")

	assert.Equal(t, int(ErrorInvalidArgument), dapPost(t, r, "/ticket", map[string]any{"content": "连不上"}))
}

// 有 email 身份的用户提工单：照旧用账号邮箱，忽略 req.Email。
func TestTicket_EmailUserIgnoresContactEmail(t *testing.T) {
	dapDB(t)
	dapMailSpy(t)
	own := dapEmail("own")
	u := dapUser(t, own, "")
	r := gin.New()
	r.POST("/ticket", dapAs(&u, ""), api_create_ticket)
	fid := fmt.Sprintf("dap-fb-e-%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Get().Where("feedback_id = ?", fid).Delete(&FeedbackTicket{}) })
	require.Equal(t, 0, dapPost(t, r, "/ticket", map[string]any{
		"content": "连不上", "email": dapEmail("other"), "feedbackId": fid, "auto_generated": true}))
	var tk FeedbackTicket
	require.NoError(t, db.Get().Where("feedback_id = ?", fid).First(&tk).Error)
	assert.Equal(t, own, tk.Email)
}

// 管理员给零身份用户设邮箱 = 首个身份 → identity_bound 一次；已有身份（第三方 / 邮箱）的用户 → 不发。
func TestAdminSetEmail_ZeroIdentityEmitsIdentityBound(t *testing.T) {
	dapDB(t)
	events := dapFunnelSpy(t)
	set := func(u User) {
		t.Helper()
		resp, w := adminJSON(t, http.MethodPut, "/app/users/:uuid/email", "/app/users/"+u.UUID+"/email",
			api_admin_update_user_email, gin.H{"email": dapEmail("admin")})
		require.Equal(t, 0, resp.Code, w.Body.String())
	}

	zero := dapUser(t, "", "")
	set(zero)
	got := events("identity_bound")
	require.Len(t, got, 1)
	assert.Equal(t, zero.ID, got[0].UserID)
	assert.Equal(t, string(BrandKaitu), got[0].Brand)
	assert.Equal(t, FunnelSurfaceApp, got[0].Surface)

	social := dapUser(t, "", "")
	require.NoError(t, db.Get().Create(&LoginIdentify{UserID: social.ID, Type: SocialProviderGoogle,
		IndexID: fmt.Sprintf("dap-g-adm-%d", time.Now().UnixNano()), Brand: string(BrandKaitu)}).Error)
	set(social)
	set(dapUser(t, dapEmail("has"), ""))
	assert.Len(t, events("identity_bound"), 1, "已有身份的用户不发")
}
