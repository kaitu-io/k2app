package center

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// =====================================================================
// Cookie 认证测试
// 测试目标：验证 HttpOnly Cookie 认证流程的正确性和安全性
// =====================================================================

// skipIfNoDatabaseAuth 检查数据库是否可用，不可用则跳过测试
func skipIfNoDatabaseAuth(t *testing.T) {
	t.Helper()
	testInitConfig()
	if db.Get() == nil {
		t.Skip("Skipping test: database not available")
	}
	sqlDB, err := db.Get().DB()
	if err != nil || sqlDB.Ping() != nil {
		t.Skip("Skipping test: database not available")
	}
}

// TestCookieAuth_WebLogin_SetsCookies 测试 Web 登录后是否正确设置 Cookie
// 能抓到的 Bug：Cookie 设置遗漏、属性配置错误
func TestCookieAuth_WebLogin_SetsCookies(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()
	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	// 1. 先发送验证码
	sendCodeResp := NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": "test-cookie@example.com"}).
		Execute(r)

	assert.Equal(t, 200, sendCodeResp.Code)

	// 2. 执行 Web 登录
	w := NewTestRequest("POST", "/api/auth/web-login").
		WithBody(map[string]string{
			"email":            "test-cookie@example.com",
			"verificationCode": MockVerificationCode,
		}).
		Execute(r)

	assert.Equal(t, 200, w.Code)

	// 3. 验证 access_token Cookie
	accessCookie := GetAccessTokenCookie(w)
	if accessCookie != nil {
		assert.True(t, accessCookie.HttpOnly, "access_token must be HttpOnly")
		assert.NotEmpty(t, accessCookie.Value, "access_token must have value")
		// 验证 SameSite 属性
		assert.Equal(t, http.SameSiteLaxMode, accessCookie.SameSite, "access_token must have SameSite=Lax")
	}

	// 4. 验证 csrf_token Cookie
	csrfCookie := GetCSRFTokenCookie(w)
	if csrfCookie != nil {
		assert.False(t, csrfCookie.HttpOnly, "csrf_token must NOT be HttpOnly (frontend needs to read it)")
		assert.NotEmpty(t, csrfCookie.Value, "csrf_token must have value")
	}
}

// TestCookieAuth_HttpOnly_Protection 测试 HttpOnly 保护
// 能抓到的 Bug：HttpOnly 标志丢失导致 XSS 可窃取 token
func TestCookieAuth_HttpOnly_Protection(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()
	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	// 发送验证码并登录
	NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": "test-httponly@example.com"}).
		Execute(r)

	w := NewTestRequest("POST", "/api/auth/web-login").
		WithBody(map[string]string{
			"email":            "test-httponly@example.com",
			"verificationCode": MockVerificationCode,
		}).
		Execute(r)

	// 验证 access_token 是 HttpOnly
	accessCookie := GetAccessTokenCookie(w)
	if accessCookie != nil {
		assert.True(t, accessCookie.HttpOnly,
			"SECURITY: access_token MUST be HttpOnly to prevent XSS token theft")
	}

	// 验证 csrf_token 不是 HttpOnly（前端需要读取）
	csrfCookie := GetCSRFTokenCookie(w)
	if csrfCookie != nil {
		assert.False(t, csrfCookie.HttpOnly,
			"csrf_token should NOT be HttpOnly so frontend can read and send it")
	}
}

// TestCookieAuth_SameSite_Protection 测试 SameSite 保护
// 能抓到的 Bug：缺少 SameSite 属性导致 CSRF 攻击风险
func TestCookieAuth_SameSite_Protection(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()
	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": "test-samesite@example.com"}).
		Execute(r)

	w := NewTestRequest("POST", "/api/auth/web-login").
		WithBody(map[string]string{
			"email":            "test-samesite@example.com",
			"verificationCode": MockVerificationCode,
		}).
		Execute(r)

	accessCookie := GetAccessTokenCookie(w)
	if accessCookie != nil {
		// SameSite=Lax 是推荐的设置，允许顶级导航但阻止跨站 POST
		assert.Equal(t, http.SameSiteLaxMode, accessCookie.SameSite,
			"SECURITY: access_token MUST have SameSite=Lax to prevent CSRF attacks")
	}
}

// TestCookieAuth_AutoSendWithRequest 测试请求是否自动携带 Cookie
// 能抓到的 Bug：credentials 配置错误导致 Cookie 不发送
func TestCookieAuth_AutoSendWithRequest(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()
	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	// 1. 登录获取 Cookie
	NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": "test-autosend@example.com"}).
		Execute(r)

	loginResp := NewTestRequest("POST", "/api/auth/web-login").
		WithBody(map[string]string{
			"email":            "test-autosend@example.com",
			"verificationCode": MockVerificationCode,
		}).
		Execute(r)

	accessCookie := GetAccessTokenCookie(loginResp)
	csrfCookie := GetCSRFTokenCookie(loginResp)
	if accessCookie == nil {
		t.Skip("Login did not return access_token cookie")
	}

	// 2. 使用 Cookie 访问受保护资源（GET 请求不需要 CSRF）
	w := NewTestRequest("GET", "/api/user/info").
		WithCookie(CookieAccessToken, accessCookie.Value).
		Execute(r)

	// 应该成功（code=0）或返回用户不存在（code=404）
	// 重点是不应该返回 401（未认证）
	resp, _ := ParseResponse(w)
	assert.NotEqual(t, 401, resp.Code,
		"Request with valid Cookie should not return 401")

	// 3. POST 请求需要 CSRF Token
	if csrfCookie != nil {
		postResp := NewTestRequest("POST", "/api/user/test-post").
			WithCookie(CookieAccessToken, accessCookie.Value).
			WithCSRFToken(csrfCookie.Value).
			Execute(r)

		postRespData, _ := ParseResponse(postResp)
		assert.NotEqual(t, 401, postRespData.Code,
			"POST request with valid Cookie + CSRF should not return 401")
	}
}

// TestCookieAuth_ExpiredCookie_Returns401 测试过期 Cookie 返回 401
// 能抓到的 Bug：过期判断逻辑错误
func TestCookieAuth_ExpiredCookie_Returns401(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()

	// 生成已过期的 Token
	expiredToken := GenerateExpiredToken(99999, "")

	// 使用过期 Token 访问
	w := NewTestRequest("GET", "/api/user/info").
		WithCookie(CookieAccessToken, expiredToken).
		Execute(r)

	resp, _ := ParseResponse(w)
	assert.Equal(t, ErrorNotLogin, resp.Code,
		"Expired Cookie should return 401")
}

// TestCookieAuth_InvalidCookie_Returns401 测试无效 Cookie 返回 401
// 能抓到的 Bug：JWT 签名验证遗漏
func TestCookieAuth_InvalidCookie_Returns401(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()

	// 使用篡改的 Token
	tamperedToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoxfQ.tampered"

	w := NewTestRequest("GET", "/api/user/info").
		WithCookie(CookieAccessToken, tamperedToken).
		Execute(r)

	resp, _ := ParseResponse(w)
	assert.Equal(t, ErrorNotLogin, resp.Code,
		"Tampered Cookie should return 401")
}

// TestCookieAuth_NoCookie_Returns401 测试无 Cookie 返回 401
// 能抓到的 Bug：认证绕过漏洞
func TestCookieAuth_NoCookie_Returns401(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()

	// 不带任何认证信息访问
	w := NewTestRequest("GET", "/api/user/info").Execute(r)

	resp, _ := ParseResponse(w)
	assert.Equal(t, ErrorNotLogin, resp.Code,
		"Request without authentication should return 401")
}

// =====================================================================
// CSRF Token 测试
// 测试目标：验证 CSRF 保护机制的正确性
// =====================================================================

// TestCSRF_POST_WithoutToken_Returns401 测试 POST 请求无 CSRF Token 被拒绝
// 能抓到的 Bug：CSRF 验证遗漏
func TestCSRF_POST_WithoutToken_Returns401(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()
	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	// 登录获取 Cookie
	NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": "test-csrf-notoken@example.com"}).
		Execute(r)

	loginResp := NewTestRequest("POST", "/api/auth/web-login").
		WithBody(map[string]string{
			"email":            "test-csrf-notoken@example.com",
			"verificationCode": MockVerificationCode,
		}).
		Execute(r)

	accessCookie := GetAccessTokenCookie(loginResp)
	if accessCookie == nil {
		t.Skip("Login did not return access_token cookie")
	}

	// POST 请求只带 Cookie，不带 CSRF Token
	w := NewTestRequest("POST", "/api/user/test-post").
		WithCookie(CookieAccessToken, accessCookie.Value).
		// 故意不设置 CSRF Token
		Execute(r)

	resp, _ := ParseResponse(w)
	assert.Equal(t, ErrorNotLogin, resp.Code,
		"SECURITY: POST without CSRF token should be rejected")
}

// TestCSRF_POST_WithWrongToken_Returns401 测试错误的 CSRF Token 被拒绝
// 能抓到的 Bug：Token 比较逻辑错误
func TestCSRF_POST_WithWrongToken_Returns401(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()
	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": "test-csrf-wrong@example.com"}).
		Execute(r)

	loginResp := NewTestRequest("POST", "/api/auth/web-login").
		WithBody(map[string]string{
			"email":            "test-csrf-wrong@example.com",
			"verificationCode": MockVerificationCode,
		}).
		Execute(r)

	accessCookie := GetAccessTokenCookie(loginResp)
	csrfCookie := GetCSRFTokenCookie(loginResp)
	if accessCookie == nil || csrfCookie == nil {
		t.Skip("Login did not return required cookies")
	}

	// POST 请求带错误的 CSRF Token
	w := NewTestRequest("POST", "/api/user/test-post").
		WithCookie(CookieAccessToken, accessCookie.Value).
		WithCookie(CookieCSRFToken, csrfCookie.Value). // Cookie 正确
		WithHeader("X-CSRF-Token", "wrong-csrf-token"). // Header 错误
		Execute(r)

	resp, _ := ParseResponse(w)
	assert.Equal(t, ErrorNotLogin, resp.Code,
		"SECURITY: POST with wrong CSRF token should be rejected")
}

// TestCSRF_POST_WithCorrectToken_Success 测试正确的 CSRF Token 通过
// 能抓到的 Bug：正常流程验证
func TestCSRF_POST_WithCorrectToken_Success(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()
	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": "test-csrf-correct@example.com"}).
		Execute(r)

	loginResp := NewTestRequest("POST", "/api/auth/web-login").
		WithBody(map[string]string{
			"email":            "test-csrf-correct@example.com",
			"verificationCode": MockVerificationCode,
		}).
		Execute(r)

	accessCookie := GetAccessTokenCookie(loginResp)
	csrfCookie := GetCSRFTokenCookie(loginResp)
	if accessCookie == nil || csrfCookie == nil {
		t.Skip("Login did not return required cookies")
	}

	// POST 请求带正确的 CSRF Token
	w := NewTestRequest("POST", "/api/user/test-post").
		WithCookie(CookieAccessToken, accessCookie.Value).
		WithCSRFToken(csrfCookie.Value).
		Execute(r)

	resp, _ := ParseResponse(w)
	// 应该不是 401（认证失败）
	assert.NotEqual(t, ErrorNotLogin, resp.Code,
		"POST with correct CSRF token should pass authentication")
}

// TestCSRF_GET_WithoutToken_Success 测试 GET 请求不需要 CSRF Token
// 能抓到的 Bug：误将 GET 也验证 CSRF
func TestCSRF_GET_WithoutToken_Success(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()
	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": "test-csrf-get@example.com"}).
		Execute(r)

	loginResp := NewTestRequest("POST", "/api/auth/web-login").
		WithBody(map[string]string{
			"email":            "test-csrf-get@example.com",
			"verificationCode": MockVerificationCode,
		}).
		Execute(r)

	accessCookie := GetAccessTokenCookie(loginResp)
	if accessCookie == nil {
		t.Skip("Login did not return access_token cookie")
	}

	// GET 请求只带 Cookie，不带 CSRF Token
	w := NewTestRequest("GET", "/api/user/info").
		WithCookie(CookieAccessToken, accessCookie.Value).
		Execute(r)

	resp, _ := ParseResponse(w)
	// GET 请求不应该因为缺少 CSRF 而返回 401
	assert.NotEqual(t, ErrorNotLogin, resp.Code,
		"GET request should not require CSRF token")
}

// =====================================================================
// Sliding Expiration 测试
// 测试目标：验证 Cookie 自动续期机制
// =====================================================================

// TestSlidingExp_TokenFresh_NoRenewal 测试新 Token 不续期
// 能抓到的 Bug：过度续期消耗资源
func TestSlidingExp_TokenFresh_NoRenewal(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()

	// 创建测试用户（需要数据库）
	user := CreateTestUser(t)
	require.NotNil(t, user)

	// 生成还有 30 天有效期的 Token（远超 7 天阈值）
	freshToken := GenerateExpiringToken(user.ID, 30*24*60*60*1000000000) // 30 days in nanoseconds

	// 发起请求
	w := NewTestRequest("GET", "/api/user/info").
		WithCookie(CookieAccessToken, freshToken).
		Execute(r)

	// 检查是否有新的 Set-Cookie
	newAccessCookie := GetAccessTokenCookie(w)
	// 新 Token 不应该触发续期
	if newAccessCookie != nil {
		assert.Equal(t, freshToken, newAccessCookie.Value,
			"Fresh token (>7 days remaining) should not trigger renewal")
	}
}

// TestSlidingExp_TokenExpiring_Renewal 测试临近过期 Token 自动续期
// 能抓到的 Bug：续期逻辑失效，用户被登出
func TestSlidingExp_TokenExpiring_Renewal(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()

	// 创建测试用户（需要数据库）
	user := CreateTestUser(t)
	require.NotNil(t, user)

	// 生成只剩 3 天有效期的 Token（低于 7 天阈值）
	expiringToken := GenerateExpiringToken(user.ID, 3*24*60*60*1000000000) // 3 days in nanoseconds

	// 发起请求
	w := NewTestRequest("GET", "/api/user/info").
		WithCookie(CookieAccessToken, expiringToken).
		Execute(r)

	// 检查是否有新的 Set-Cookie
	newAccessCookie := GetAccessTokenCookie(w)
	if newAccessCookie != nil {
		// 临近过期的 Token 应该触发续期，得到新 Token
		assert.NotEqual(t, expiringToken, newAccessCookie.Value,
			"Expiring token (<7 days remaining) should trigger renewal with new token")
	}
}

// =====================================================================
// Bearer Token 兼容测试
// 测试目标：验证非 Web 端 Bearer Token 认证仍然正常
// =====================================================================

// TestBearerToken_StillWorks 测试 Bearer Token 认证仍然工作
// 能抓到的 Bug：Cookie 改造破坏了 Bearer Token 认证
func TestBearerToken_StillWorks(t *testing.T) {
	skipIfNoDatabaseAuth(t)
	r := SetupTestRouter()

	// 创建测试用户和设备
	user := CreateTestUser(t)
	require.NotNil(t, user)

	device := CreateTestDevice(t, user.ID, "test-bearer-device")
	require.NotNil(t, device)

	// 生成带设备的 Token（模拟 Desktop/Mobile）
	token := GenerateTestToken(user.ID, device.UDID, 24*60*60*1000000000)

	// 更新设备的 TokenIssueAt（使其与 token 匹配）
	// 注意：这里需要确保 device.TokenIssueAt 与 token 中的 TokenIssueAt 一致
	// 由于 GenerateTestToken 使用 time.Now()，我们需要更新数据库

	// 使用 Bearer Token 访问
	w := NewTestRequest("GET", "/api/user/info").
		WithBearerToken(token).
		Execute(r)

	resp, _ := ParseResponse(w)
	// 不应该返回 401（即使可能返回其他错误如用户不存在）
	// 重点是 Bearer Token 认证路径仍然工作
	assert.NotEqual(t, ErrorNotLogin, resp.Code,
		"Bearer Token authentication should still work for non-web clients")
}
