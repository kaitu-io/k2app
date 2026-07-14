package center

// 跨品牌隔离集成测试矩阵（Task 12）——Phase 1 的验收测试套件。
//
// 这是验收测试，不是 TDD 起点：Task 1-11 全部已落地，矩阵内每条断言开跑时都
// "应该" PASS。任何 FAIL 说明前序某个 Task 有缺口，必须先定位归属再回去修，
// 不能在本文件里绕过。
//
// 矩阵条目 #1（同邮箱双品牌注册 → 两个独立 user）已由 Task 4 的
// TestFindOrCreateUserByEmail_BrandIsolation（brand_auth_test.go）覆盖，这里
// 只挂一个指向它的 skip 占位，不重复写断言。
//
// 每条断言一个 t.Run 子测试，命名前缀数字对应 spec「隔离测试矩阵」的条目号。
// 全部真 MySQL（skipIfNoConfig 门控），测试数据统一 "brandiso-" 前缀 + 时间戳
// 唯一后缀，t.Cleanup 硬删，不污染共享 dev 库。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// ===================== 共享 helpers =====================

// isoUniq 生成本文件测试数据的唯一后缀，避免与其它测试文件/并发 dev 库使用者冲突。
func isoUniq() string {
	return time.Now().Format("20060102150405.000000000")
}

// isoShortUniq 是 isoUniq 的短版本，供窄 varchar 列（如 Plan.PID varchar(30)）使用。
func isoShortUniq() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

// createBrandIsoAccessKeyUser 造一个用 X-Access-Key 认证的测试用户（brand/admin 可配置）。
// 复用 Task 4 brand_auth_test.go 确定的 access-key 构造方式：比 JWT 更简单，不需要
// device/token 发行链路。
func createBrandIsoAccessKeyUser(t *testing.T, brand Brand, admin bool) (user *User, plaintext string) {
	t.Helper()
	plaintext = accessKeyPrefix + generateId("brandiso-ak")
	hash := HashAccessKey(plaintext)
	user = &User{
		UUID:      generateId("brandiso-user"),
		Brand:     string(brand),
		AccessKey: &hash,
	}
	if admin {
		yes := true
		user.IsAdmin = &yes
	}
	require.NoError(t, db.Get().Create(user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(user) })
	return user, plaintext
}

// setupBrandIsolationRouter 按 route.go 同样的中间件链手工挂本文件需要、但未在
// SetupTestRouter 注册的路由：品牌 Host 驱动的公开端点 + admin 统计端点。
func setupBrandIsolationRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	api := r.Group("/api")
	api.Use(BrandResolver())
	{
		api.GET("/plans", api_get_plans)
		api.GET("/app/config", api_get_app_config)
		api.GET("/invite/code", api_get_invite_code)
	}

	admin := r.Group("/app")
	admin.Use(BrandResolver(), AdminRequired())
	{
		admin.GET("/users/statistics", api_admin_get_user_statistics)
	}

	return r
}

// ginCtxWithAuthAndHost 构造一个带 authContext + 指定 Host 的 gin.Context，用于直接
// 调用 handler（绕开完整中间件链——ProRequired/DeviceAuthRequired 等在本文件要验证的
// 品牌隔离断言里不是被测对象，注入更聚焦，也是 Task 6/7 既有测试建立的模式）。
func ginCtxWithAuthAndHost(method, path, host string, body []byte, user *User) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var reader *strings.Reader
	if body == nil {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(string(body))
	}
	c.Request = httptest.NewRequest(method, path, reader)
	c.Request.Host = host
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("authContext", &authContext{UserID: user.ID, User: user})
	return c, w
}

func TestBrandIsolationMatrix(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	// ---------- #1: 同邮箱双品牌注册 → 两个独立 user ----------
	t.Run("01_SameEmail_DualBrand_TwoIndependentUsers", func(t *testing.T) {
		t.Skip("covered by TestFindOrCreateUserByEmail_BrandIsolation in brand_auth_test.go (Task 4) — not duplicated per brief")
	})

	// ---------- #2: overleap 用户 token 访问默认(kaitu)品牌请求 → 403003 ----------
	t.Run("02_OverleapUserToken_DefaultKaituBrandRequest_403003", func(t *testing.T) {
		skipIfNoConfig(t)
		_, plaintext := createBrandIsoAccessKeyUser(t, BrandOverleap, false)

		router := SetupTestRouter()
		req, err := http.NewRequest(http.MethodGet, "/api/user/info", nil)
		require.NoError(t, err)
		req.Header.Set("X-Access-Key", plaintext)
		// 无 Host 覆盖、无 X-K2-Brand header → resolveRequestBrand 回退默认 kaitu.

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, int(ErrorBrandMismatch), resp.Code)
	})

	// ---------- #3: kaitu 用户 token + X-K2-Brand: overleap → 403003 ----------
	t.Run("03_KaituUserToken_OverleapBrandHeader_403003", func(t *testing.T) {
		skipIfNoConfig(t)
		_, plaintext := createBrandIsoAccessKeyUser(t, BrandKaitu, false)

		router := SetupTestRouter()
		req, err := http.NewRequest(http.MethodGet, "/api/user/info", nil)
		require.NoError(t, err)
		req.Header.Set("X-Access-Key", plaintext)
		req.Header.Set("X-K2-Brand", "overleap")
		// httptest 默认 Host="example.com"，不映射任何品牌 → resolveRequestBrand 落到
		// X-K2-Brand header。

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, int(ErrorBrandMismatch), resp.Code)
	})

	// ---------- #4: admin 用户跨品牌访问 → 放行 ----------
	t.Run("04_AdminUser_CrossBrandAccess_Allowed", func(t *testing.T) {
		skipIfNoConfig(t)
		_, plaintext := createBrandIsoAccessKeyUser(t, BrandKaitu, true)

		router := SetupTestRouter()
		req, err := http.NewRequest(http.MethodGet, "/api/user/info", nil)
		require.NoError(t, err)
		req.Header.Set("X-Access-Key", plaintext)
		req.Header.Set("X-K2-Brand", "overleap") // 用户品牌 kaitu，请求品牌 overleap：不一致

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.NotEqual(t, int(ErrorBrandMismatch), resp.Code, "admin must bypass brand-mismatch check")
		assert.Equal(t, 0, resp.Code, "admin request should succeed, got code=%d msg=%s", resp.Code, resp.Message)
	})

	// ---------- #5: GET /api/plans（Host: overleap.io）只返回 brand=overleap 的 plans ----------
	t.Run("05_Plans_HostOverleap_OnlyOverleapPlans", func(t *testing.T) {
		skipIfNoConfig(t)
		uniq := isoShortUniq()
		pidK := "biso-pk-" + uniq
		pidO := "biso-po-" + uniq
		pK := Plan{PID: pidK, Label: "k", Price: 100, Month: 1, Product: ProductApp, Brand: string(BrandKaitu), IsActive: BoolPtr(true)}
		pO := Plan{PID: pidO, Label: "o", Price: 900, Month: 1, Product: ProductApp, Brand: string(BrandOverleap), IsActive: BoolPtr(true)}
		require.NoError(t, db.Get().Create(&pK).Error)
		require.NoError(t, db.Get().Create(&pO).Error)
		t.Cleanup(func() {
			db.Get().Unscoped().Where("pid IN ?", []string{pidK, pidO}).Delete(&Plan{})
		})

		router := setupBrandIsolationRouter()
		req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
		req.Host = "overleap.io"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), pidO, "overleap /api/plans should include the overleap plan")
		assert.NotContains(t, w.Body.String(), pidK, "overleap /api/plans must not leak the kaitu plan")
	})

	// ---------- #6: GET /api/app/config（Host: overleap.io）appLinks.baseURL 正确 +
	// announcements 只含 overleap 品牌公告 ----------
	t.Run("06_AppConfig_HostOverleap_BaseURLAndAnnouncementsScoped", func(t *testing.T) {
		skipIfNoConfig(t)
		uniq := isoUniq()
		msgK := "brandiso-appcfg-k-" + uniq
		msgO := "brandiso-appcfg-o-" + uniq
		aK := Announcement{Message: msgK, IsActive: BoolPtr(true), Brand: string(BrandKaitu)}
		aO := Announcement{Message: msgO, IsActive: BoolPtr(true), Brand: string(BrandOverleap)}
		require.NoError(t, db.Get().Create(&aK).Error)
		require.NoError(t, db.Get().Create(&aO).Error)
		t.Cleanup(func() {
			db.Get().Unscoped().Where("message IN ?", []string{msgK, msgO}).Delete(&Announcement{})
		})

		router := setupBrandIsolationRouter()
		req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
		req.Host = "overleap.io"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		data, err := ParseResponseData[DataAppConfig](w)
		require.NoError(t, err)
		assert.Equal(t, "https://www.overleap.io", data.AppLinks.BaseURL)

		var foundK, foundO bool
		for _, a := range data.Announcements {
			if a.Message == msgK {
				foundK = true
			}
			if a.Message == msgO {
				foundO = true
			}
		}
		assert.False(t, foundK, "overleap /api/app/config must not leak kaitu announcement")
		assert.True(t, foundO, "overleap /api/app/config should include its own announcement")
	})

	// ---------- #7: overleap 用户 GET /api/tunnels → 不含 visible_overleap=false 的节点 ----------
	t.Run("07_Tunnels_OverleapUser_HidesDefaultKaituOnlyNode", func(t *testing.T) {
		skipIfNoConfig(t)
		uniq := isoUniq()

		// 默认可见性节点（VisibleKaitu/VisibleOverleap 零值）→ kaitu 可见，overleap 不可见。
		node := SlaveNode{
			Ipv4: "10.99.20.1", SecretToken: "brandiso-tun-s1", Country: "JP", Region: "japan",
			Name: "brandiso-tun-jp-" + uniq, Class: NodeClassShared,
		}
		require.NoError(t, db.Get().Create(&node).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })

		domain := "brandiso-tun-" + uniq + ".example"
		tun := SlaveTunnel{
			Domain: domain, SecretToken: "brandiso-tun-t1", Name: "brandiso-tun-jp-tun-" + uniq,
			Protocol: TunnelProtocolK2V5, Port: 443, NodeID: node.ID,
			IsTest: BoolPtr(false), ServerURL: "k2v5://" + domain + ":443",
		}
		require.NoError(t, db.Get().Create(&tun).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

		tunnelsBodyFor := func(user *User) string {
			t.Helper()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/tunnels", nil)
			c.Set("authContext", &authContext{UserID: user.ID, User: user})
			api_k2_tunnels(c)
			require.Equal(t, http.StatusOK, w.Code)
			return w.Body.String()
		}

		overleapUser := &User{ID: 999801, Brand: string(BrandOverleap)}
		assert.NotContains(t, tunnelsBodyFor(overleapUser), domain,
			"default (kaitu-visible-only) node must NOT leak to an overleap user's /api/tunnels")

		// 正向对照：同一节点对 kaitu 用户必须可见。没有这条，未来任何无关过滤
		// （配额隐藏、协议过滤等）把该节点顺带排除都会让上面的 NotContains 静默
		// 变成 vacuous pass——Contains 锁死"节点消失的唯一原因是品牌过滤"。
		kaituUser := &User{ID: 999802, Brand: string(BrandKaitu)}
		assert.Contains(t, tunnelsBodyFor(kaituUser), domain,
			"the same default node must remain visible to a kaitu user — zero regression control")
	})

	// ---------- #8: overleap 用户 POST /api/user/orders → 405001 PaymentChannelUnavailable ----------
	t.Run("08_Orders_OverleapUser_PaymentChannelUnavailable", func(t *testing.T) {
		skipIfNoConfig(t)
		user, _ := createBrandIsoAccessKeyUser(t, BrandOverleap, false)

		body := []byte(`{"preview":true,"plan":"any-plan-id"}`)
		c, w := ginCtxWithAuthAndHost(http.MethodPost, "/api/user/orders", "overleap.io", body, user)
		api_create_order(c)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, int(ErrorPaymentChannelUnavailable), resp.Code,
			"overleap has no payment channels in Phase 1 — order must be rejected before plan/campaign lookup")
	})

	// ---------- #9: overleap 用户 POST /api/user/apple-iap/verify → 405001 ----------
	t.Run("09_AppleIAPVerify_OverleapUser_PaymentChannelUnavailable", func(t *testing.T) {
		skipIfNoConfig(t)
		user, _ := createBrandIsoAccessKeyUser(t, BrandOverleap, false)

		body := []byte(`{"transactionId":"tx-brandiso-test"}`)
		c, w := ginCtxWithAuthAndHost(http.MethodPost, "/api/user/apple-iap/verify", "overleap.io", body, user)
		api_apple_iap_verify(c)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, int(ErrorPaymentChannelUnavailable), resp.Code,
			"apple_iap is a kaitu-only channel (bundle id bound); overleap must be rejected before Apple verify call")
	})

	// ---------- #10: OTT: kaitu 用户 issue redirect=https://overleap.io/x → 拒绝 ----------
	t.Run("10_OTTIssue_KaituUser_CrossBrandRedirect_Rejected", func(t *testing.T) {
		skipIfNoConfig(t)
		user, _ := createBrandIsoAccessKeyUser(t, BrandKaitu, false)

		gin.SetMode(gin.TestMode)
		r := gin.New() // no Recovery: a panic in the handler must fail the test loudly
		r.POST("/api/user/ott", func(c *gin.Context) {
			c.Set("authContext", &authContext{UserID: user.ID, User: user})
		}, api_issue_ott)

		req := httptest.NewRequest(http.MethodPost, "/api/user/ott",
			strings.NewReader(`{"redirect":"https://overleap.io/x"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, int(ErrorInvalidArgument), resp.Code,
			"kaitu user's OTT issue must reject a redirect pointing at overleap.io")
	})

	// ---------- #11: brand=kaitu 授权码，overleap 用户 redeem → ErrorLicenseKeyNotFound ----------
	t.Run("11_LicenseKeyRedeem_KaituKey_OverleapUser_NotFound", func(t *testing.T) {
		skipIfNoConfig(t)
		uniq := isoUniq()
		code := "BIK" + strings.ToUpper(uniq[len(uniq)-5:]) // <=8 chars, license_keys.code is varchar(8)
		key := LicenseKey{
			UUID: "brandiso-lk-" + uniq, Code: code, PlanDays: 30,
			ExpiresAt: 9999999999, Brand: string(BrandKaitu), RecipientMatcher: "all",
		}
		require.NoError(t, db.Get().Create(&key).Error)
		t.Cleanup(func() { db.Get().Unscoped().Where("code = ?", code).Delete(&LicenseKey{}) })

		user, _ := createBrandIsoAccessKeyUser(t, BrandOverleap, false)

		c, w := ginCtxWithAuthAndHost(http.MethodPost, "/api/license-keys/code/"+code+"/redeem", "overleap.io", nil, user)
		c.Params = gin.Params{{Key: "code", Value: code}}
		api_redeem_license_key(c)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, int(ErrorLicenseKeyNotFound), resp.Code,
			"a kaitu-brand license key must be invisible to an overleap redeemer")
	})

	// ---------- #12: brand=kaitu 活动码在 overleap 用户下单 preview 中不生效 ----------
	t.Run("12_CampaignCode_KaituBrand_DoesNotApplyForOverleapUser", func(t *testing.T) {
		skipIfNoConfig(t)
		uniq := isoUniq()
		now := time.Now().Unix()
		code := "BIC" + strings.ToUpper(uniq[len(uniq)-5:])
		camp := Campaign{
			Code: code, Name: "brandiso-camp", Type: CampaignTypeDiscount, Value: 90,
			StartAt: now - 3600, EndAt: now + 3600, MatcherType: "all",
			Brand: string(BrandKaitu), IsActive: BoolPtr(true),
		}
		require.NoError(t, db.Get().Create(&camp).Error)
		t.Cleanup(func() { db.Get().Unscoped().Where("code = ?", code).Delete(&Campaign{}) })

		// Layer 1 (today's actual enforcement point): the order endpoint rejects
		// every overleap order at the payment-channel gate (Task 10), before it
		// ever reaches campaign lookup — so the kaitu campaign trivially "does not
		// apply" because nothing applies. Pin that HTTP-level behavior.
		user, _ := createBrandIsoAccessKeyUser(t, BrandOverleap, false)
		body := []byte(`{"preview":true,"plan":"any-plan-id","campaignCode":"` + code + `"}`)
		c, w := ginCtxWithAuthAndHost(http.MethodPost, "/api/user/orders", "overleap.io", body, user)
		api_create_order(c)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, int(ErrorPaymentChannelUnavailable), resp.Code,
			"overleap order preview is rejected at the payment-channel gate before campaign lookup")

		// Layer 2 (defense in depth): even independent of the payment gate, the
		// campaign-lookup helper the order handler calls (getCampaignByCode) must
		// not resolve a kaitu-scoped code under an overleap brand context — so if
		// the gate above is ever relaxed for a future overleap payment channel,
		// isolation still holds at the campaign layer.
		got := getCampaignByCode(context.Background(), code, BrandOverleap)
		assert.Nil(t, got, "kaitu-brand campaign code must not resolve under overleap brand scope")
		gotKaitu := getCampaignByCode(context.Background(), code, BrandKaitu)
		assert.NotNil(t, gotKaitu, "sanity: the campaign does resolve under its own (kaitu) brand")
	})

	// ---------- #13: 验证码跨品牌隔离不变式 ----------
	t.Run("13_VerificationCode_CrossBrandIsolation", func(t *testing.T) {
		skipIfNoConfig(t)
		// EnableMockVerificationCode short-circuits both issue+verify to a fixed
		// "123456" regardless of brand — bypass it for this subtest so the real
		// per-brand Redis key path (verificationCodeKey) is exercised.
		prevMock := EnableMockVerificationCode
		EnableMockVerificationCode = false
		t.Cleanup(func() { EnableMockVerificationCode = prevMock })

		email := "brandiso-vcode-" + isoUniq() + "@test.local"
		hash := secretHashIt(context.Background(), []byte(strings.ToLower(email)))

		gcOverleap := ginCtxWithBrand(BrandOverleap)
		code, err := issueOrRefreshVerificationCode(gcOverleap, hash)
		require.NoError(t, err)
		require.NotEmpty(t, code)

		gcKaitu := ginCtxWithBrand(BrandKaitu)
		result := verifyEmailCode(gcKaitu, hash, code)
		assert.Equal(t, VerifyCodeNotIssued, result,
			"a code issued under an overleap request context must not verify under a kaitu request context for the same email")

		// Sanity: the same code verifies fine back under its own (overleap) brand.
		okResult := verifyEmailCode(gcOverleap, hash, code)
		assert.Equal(t, VerifyCodeOK, okResult)
	})

	// ---------- #14: ReqBrand 未挂中间件兜底 ----------
	t.Run("14_ReqBrand_NoMiddleware_HostAndDefaultFallback", func(t *testing.T) {
		// 纯内存断言，不需要 DB，但按全局约定统一 skipIfNoConfig 门控。
		skipIfNoConfig(t)
		gin.SetMode(gin.TestMode)

		// Host=overleap.io，裸 gin.New() 无 BrandResolver 挂载 → ReqBrand 现场兜底解析。
		w1 := httptest.NewRecorder()
		c1, _ := gin.CreateTestContext(w1)
		c1.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c1.Request.Host = "overleap.io"
		assert.Equal(t, BrandOverleap, ReqBrand(c1))

		// 未知 host、无 header → 默认 kaitu。
		w2 := httptest.NewRecorder()
		c2, _ := gin.CreateTestContext(w2)
		c2.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c2.Request.Host = "unknown-host.example"
		assert.Equal(t, BrandKaitu, ReqBrand(c2))
	})

	// ---------- #15: admin 统计 brand filter 口径 ----------
	t.Run("15_AdminUserStatistics_BrandFilter_OnlyCountsFilteredBrand", func(t *testing.T) {
		skipIfNoConfig(t)
		adminUser, adminKey := createBrandIsoAccessKeyUser(t, BrandKaitu, true)

		router := setupBrandIsolationRouter()
		statsForOverleap := func() int64 {
			req := httptest.NewRequest(http.MethodGet, "/app/users/statistics?brand=overleap", nil)
			req.Header.Set("X-Access-Key", adminKey)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
			data, err := ParseResponseData[UserStatisticsResponse](w)
			require.NoError(t, err)
			return data.TotalUsers
		}

		before := statsForOverleap()

		// 造一对同批用户：一个 kaitu、一个 overleap。overleap 过滤口径下新增的
		// overleap 用户必须被计入，kaitu 那个必须被排除在外。
		_, _ = createBrandIsoAccessKeyUser(t, BrandKaitu, false)
		_, _ = createBrandIsoAccessKeyUser(t, BrandOverleap, false)

		after := statsForOverleap()

		// 包含性：>= before+1 而非 == before+1 —— 共享 dev 库上并发的 overleap
		// 注册（其它测试/会话）会让精确 delta 偶发失败；方向性断言并发安全。
		assert.GreaterOrEqual(t, after, before+1,
			"brand=overleap filter must count the newly created overleap user")

		// 排他性（ground truth 对照）：endpoint 的 TotalUsers 必须等于 DB 里
		// brand='overleap' 的真实行数（GORM 软删过滤与 endpoint 口径一致）。
		// 若 endpoint 漏掉 brand filter，它会把全库（含 kaitu）用户都计进来，
		// 与 overleap ground truth 差出几个数量级，立即失败。并发 kaitu 写入
		// 不影响等式两边；仅两次查询间的并发 overleap 写入会破坏等式——dev 库
		// 上该品牌几乎只有测试数据，毫秒级窗口可忽略。
		var overleapGroundTruth int64
		require.NoError(t, db.Get().Model(&User{}).Where("brand = ?", string(BrandOverleap)).Count(&overleapGroundTruth).Error)
		assert.Equal(t, overleapGroundTruth, after,
			"endpoint TotalUsers under brand=overleap must match the DB ground-truth overleap row count — kaitu users excluded")
		_ = adminUser
	})

	// ---------- #16: 公开邀请码链接品牌决策 pin ----------
	t.Run("16_InviteCodeLink_HostDriven_PerBrandDomain", func(t *testing.T) {
		skipIfNoConfig(t)
		owner, _ := createBrandIsoAccessKeyUser(t, BrandKaitu, false)
		invite := InviteCode{UserID: owner.ID, Remark: "brandiso-invite-" + isoUniq()}
		require.NoError(t, db.Get().Create(&invite).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&invite) })
		code := invite.GetCode()

		router := setupBrandIsolationRouter()

		// Host=kaitu.io → link 落在 kaitu.io/s
		reqK := httptest.NewRequest(http.MethodGet, "/api/invite/code?code="+url.QueryEscape(code), nil)
		reqK.Host = "kaitu.io"
		wK := httptest.NewRecorder()
		router.ServeHTTP(wK, reqK)
		require.Equal(t, http.StatusOK, wK.Code)
		dataK, err := ParseResponseData[DataInviteCode](wK)
		require.NoError(t, err)
		assert.Contains(t, dataK.Link, "kaitu.io/s",
			"documented decision (Task 8 review): invite link host is request-Host-driven, pinned here")
		assert.NotContains(t, dataK.Link, "overleap.io")

		// Host=overleap.io → 同一邀请码，link 改落 overleap.io/s
		reqO := httptest.NewRequest(http.MethodGet, "/api/invite/code?code="+url.QueryEscape(code), nil)
		reqO.Host = "overleap.io"
		wO := httptest.NewRecorder()
		router.ServeHTTP(wO, reqO)
		require.Equal(t, http.StatusOK, wO.Code)
		dataO, err := ParseResponseData[DataInviteCode](wO)
		require.NoError(t, err)
		assert.Contains(t, dataO.Link, "overleap.io/s")
		assert.NotContains(t, dataO.Link, "kaitu.io")
	})
}
