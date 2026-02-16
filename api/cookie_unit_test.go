package center

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// Cookie 安全属性单元测试
// 这些测试不需要数据库，直接测试 Cookie 设置逻辑
// =====================================================================

// TestSetAuthCookies_HttpOnly 测试 access_token 是否设置为 HttpOnly
// 能抓到的 Bug：HttpOnly 属性缺失导致 XSS 可窃取 token
func TestSetAuthCookies_HttpOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/auth/web-login", nil)

	// 创建测试数据
	authResult := &DataAuthResult{
		AccessToken:  "test-access-token-12345",
		RefreshToken: "test-refresh-token-12345",
		IssuedAt:     time.Now().Unix(),
	}

	// 调用被测函数
	setAuthCookies(c, authResult)

	// 解析响应中的 Cookie
	cookies := w.Result().Cookies()

	// 查找 access_token Cookie
	var accessCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == CookieAccessToken {
			accessCookie = cookie
			break
		}
	}

	require.NotNil(t, accessCookie, "access_token cookie should be set")
	assert.True(t, accessCookie.HttpOnly,
		"SECURITY BUG: access_token MUST be HttpOnly to prevent XSS token theft")
	assert.Equal(t, authResult.AccessToken, accessCookie.Value)
}

// TestSetAuthCookies_SameSite 测试 Cookie 的 SameSite 属性
// 能抓到的 Bug：缺少 SameSite 属性导致 CSRF 攻击
func TestSetAuthCookies_SameSite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/auth/web-login", nil)

	authResult := &DataAuthResult{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		IssuedAt:     time.Now().Unix(),
	}

	setAuthCookies(c, authResult)

	cookies := w.Result().Cookies()

	var accessCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == CookieAccessToken {
			accessCookie = cookie
			break
		}
	}

	require.NotNil(t, accessCookie, "access_token cookie should be set")
	assert.Equal(t, http.SameSiteLaxMode, accessCookie.SameSite,
		"SECURITY BUG: access_token MUST have SameSite=Lax to prevent CSRF attacks")
}

// TestSetAuthCookies_CSRFNotHttpOnly 测试 CSRF token 不是 HttpOnly
// 能抓到的 Bug：CSRF token 设为 HttpOnly 导致前端无法读取
func TestSetAuthCookies_CSRFNotHttpOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/auth/web-login", nil)

	authResult := &DataAuthResult{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		IssuedAt:     time.Now().Unix(),
	}

	setAuthCookies(c, authResult)

	cookies := w.Result().Cookies()

	var csrfCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == CookieCSRFToken {
			csrfCookie = cookie
			break
		}
	}

	require.NotNil(t, csrfCookie, "csrf_token cookie should be set")
	assert.False(t, csrfCookie.HttpOnly,
		"BUG: csrf_token should NOT be HttpOnly (frontend needs to read and send it)")
	assert.NotEmpty(t, csrfCookie.Value, "csrf_token should have a value")
}

// TestSetAuthCookies_SecureOnHTTPS 测试 HTTPS 请求时 Cookie 是否设为 Secure
// 能抓到的 Bug：HTTPS 环境下 Secure 属性缺失
func TestSetAuthCookies_SecureOnHTTPS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("POST", "/api/auth/web-login", nil)
	req.Header.Set("X-Forwarded-Proto", "https") // 模拟代理后的 HTTPS
	c.Request = req

	authResult := &DataAuthResult{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		IssuedAt:     time.Now().Unix(),
	}

	setAuthCookies(c, authResult)

	cookies := w.Result().Cookies()

	var accessCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == CookieAccessToken {
			accessCookie = cookie
			break
		}
	}

	require.NotNil(t, accessCookie, "access_token cookie should be set")
	assert.True(t, accessCookie.Secure,
		"SECURITY BUG: access_token should be Secure when request is HTTPS")
}

// TestSetAuthCookies_NotSecureOnHTTP 测试 HTTP 请求时 Cookie 不强制 Secure
// 能抓到的 Bug：本地开发 HTTP 环境无法正常工作
func TestSetAuthCookies_NotSecureOnHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/auth/web-login", nil)
	// 不设置 X-Forwarded-Proto，模拟 HTTP

	authResult := &DataAuthResult{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		IssuedAt:     time.Now().Unix(),
	}

	setAuthCookies(c, authResult)

	cookies := w.Result().Cookies()

	var accessCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == CookieAccessToken {
			accessCookie = cookie
			break
		}
	}

	require.NotNil(t, accessCookie, "access_token cookie should be set")
	assert.False(t, accessCookie.Secure,
		"access_token should NOT be Secure on HTTP (for local development)")
}

// TestSetAuthCookies_PathIsRoot 测试 Cookie Path 是否设为根路径
// 能抓到的 Bug：Cookie path 设置不当导致某些路径无法访问
func TestSetAuthCookies_PathIsRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/auth/web-login", nil)

	authResult := &DataAuthResult{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		IssuedAt:     time.Now().Unix(),
	}

	setAuthCookies(c, authResult)

	cookies := w.Result().Cookies()

	var accessCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == CookieAccessToken {
			accessCookie = cookie
			break
		}
	}

	require.NotNil(t, accessCookie, "access_token cookie should be set")
	assert.Equal(t, "/", accessCookie.Path,
		"access_token path should be / to be accessible from all paths")
}

// TestSetAuthCookies_CSRFTokenGenerated 测试 CSRF token 是否正确生成
// 能抓到的 Bug：CSRF token 生成失败或为空
func TestSetAuthCookies_CSRFTokenGenerated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/auth/web-login", nil)

	authResult := &DataAuthResult{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		IssuedAt:     time.Now().Unix(),
	}

	setAuthCookies(c, authResult)

	cookies := w.Result().Cookies()

	var csrfCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == CookieCSRFToken {
			csrfCookie = cookie
			break
		}
	}

	require.NotNil(t, csrfCookie, "csrf_token cookie should be set")
	assert.NotEmpty(t, csrfCookie.Value, "CSRF token should not be empty")
	// CSRF token 应该有足够的长度来防止猜测
	assert.GreaterOrEqual(t, len(csrfCookie.Value), 16,
		"CSRF token should be at least 16 characters for security")
}

// =====================================================================
// CSRF Token 验证逻辑单元测试
// =====================================================================

// TestValidateCSRF_MatchingTokens 测试 CSRF token 匹配时通过
func TestValidateCSRF_MatchingTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("POST", "/api/user/test", nil)

	// 设置匹配的 CSRF token
	csrfToken := "matching-csrf-token"
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.AddCookie(&http.Cookie{Name: CookieCSRFToken, Value: csrfToken})
	c.Request = req

	// 验证 CSRF
	headerToken := c.GetHeader("X-CSRF-Token")
	cookieToken, _ := c.Cookie(CookieCSRFToken)

	assert.Equal(t, headerToken, cookieToken,
		"CSRF validation should pass when header and cookie match")
}

// TestValidateCSRF_MismatchedTokens 测试 CSRF token 不匹配时失败
func TestValidateCSRF_MismatchedTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("POST", "/api/user/test", nil)

	// 设置不匹配的 CSRF token
	req.Header.Set("X-CSRF-Token", "header-token")
	req.AddCookie(&http.Cookie{Name: CookieCSRFToken, Value: "cookie-token"})
	c.Request = req

	// 验证 CSRF
	headerToken := c.GetHeader("X-CSRF-Token")
	cookieToken, _ := c.Cookie(CookieCSRFToken)

	assert.NotEqual(t, headerToken, cookieToken,
		"CSRF validation should fail when header and cookie don't match")
}

// TestValidateCSRF_MissingHeader 测试缺少 CSRF header 时失败
func TestValidateCSRF_MissingHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("POST", "/api/user/test", nil)

	// 只有 Cookie，没有 Header
	req.AddCookie(&http.Cookie{Name: CookieCSRFToken, Value: "cookie-token"})
	c.Request = req

	headerToken := c.GetHeader("X-CSRF-Token")
	assert.Empty(t, headerToken, "Header token should be empty when not set")
}

// TestValidateCSRF_GETNotRequired 测试 GET 请求不需要 CSRF
func TestValidateCSRF_GETNotRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/api/user/info", nil)
	c.Request = req

	// GET 请求不应该要求 CSRF token
	assert.Equal(t, "GET", c.Request.Method,
		"GET requests should not require CSRF token")
}

// =====================================================================
// JWT Token 生成和验证单元测试
// =====================================================================

// parseJWTToken 测试辅助函数：解析 JWT token
func parseJWTToken(tokenString string, secret []byte) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*TokenClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, jwt.ErrSignatureInvalid
}

// TestJWTToken_ValidSignature 测试有效签名的 JWT
func TestJWTToken_ValidSignature(t *testing.T) {
	testInitConfig() // 需要 JWT secret

	// 生成有效 token
	token := GenerateTestToken(12345, "test-device", 24*time.Hour)
	assert.NotEmpty(t, token, "Should generate valid token")

	// 获取 JWT secret
	jwtConfig := configJwt(nil)
	jwtSecret := []byte(jwtConfig.Secret)

	// 验证 token 可以解析
	claims, err := parseJWTToken(token, jwtSecret)
	assert.NoError(t, err, "Valid token should parse without error")
	assert.Equal(t, uint64(12345), claims.UserID)
	assert.Equal(t, "test-device", claims.DeviceID)
}

// TestJWTToken_ExpiredToken 测试过期的 JWT
func TestJWTToken_ExpiredToken(t *testing.T) {
	testInitConfig()

	// 生成已过期的 token
	expiredToken := GenerateExpiredToken(12345, "test-device")
	assert.NotEmpty(t, expiredToken, "Should generate expired token")

	// 获取 JWT secret
	jwtConfig := configJwt(nil)
	jwtSecret := []byte(jwtConfig.Secret)

	// 验证过期 token 解析失败
	_, err := parseJWTToken(expiredToken, jwtSecret)
	assert.Error(t, err, "Expired token should fail to parse")
}

// TestJWTToken_TamperedToken 测试篡改的 JWT
func TestJWTToken_TamperedToken(t *testing.T) {
	testInitConfig()

	// 生成有效 token 然后篡改
	validToken := GenerateTestToken(12345, "test-device", 24*time.Hour)
	// 篡改签名部分
	tamperedToken := validToken[:len(validToken)-5] + "XXXXX"

	// 获取 JWT secret
	jwtConfig := configJwt(nil)
	jwtSecret := []byte(jwtConfig.Secret)

	// 验证篡改 token 解析失败
	_, err := parseJWTToken(tamperedToken, jwtSecret)
	assert.Error(t, err, "Tampered token should fail to parse")
}

// TestJWTToken_MalformedToken 测试格式错误的 JWT
func TestJWTToken_MalformedToken(t *testing.T) {
	testInitConfig()

	jwtConfig := configJwt(nil)
	jwtSecret := []byte(jwtConfig.Secret)

	malformedTokens := []string{
		"",                       // 空
		"not-a-jwt",              // 非 JWT 格式
		"a.b",                    // 只有两部分
		"a.b.c.d",                // 多于三部分
		"eyJhbGciOiJIUzI1NiJ9..", // 空 payload
	}

	for _, token := range malformedTokens {
		_, err := parseJWTToken(token, jwtSecret)
		assert.Error(t, err, "Malformed token '%s' should fail to parse", token)
	}
}

// TestJWTToken_WebToken_NoDevice 测试 Web 端 token（无设备 ID）
func TestJWTToken_WebToken_NoDevice(t *testing.T) {
	testInitConfig()

	// 生成 Web 端 token（无设备 ID）
	token := GenerateExpiringToken(12345, 24*time.Hour)
	assert.NotEmpty(t, token)

	// 获取 JWT secret
	jwtConfig := configJwt(nil)
	jwtSecret := []byte(jwtConfig.Secret)

	claims, err := parseJWTToken(token, jwtSecret)
	assert.NoError(t, err)
	assert.Equal(t, uint64(12345), claims.UserID)
	assert.Empty(t, claims.DeviceID, "Web token should have empty device ID")
}

// TestGenerateCSRFToken_Uniqueness 测试 CSRF token 生成唯一性
func TestGenerateCSRFToken_Uniqueness(t *testing.T) {
	tokens := make(map[string]bool)

	// 生成 100 个 token，确保都不重复
	for i := 0; i < 100; i++ {
		token := GenerateCSRFToken()
		assert.NotEmpty(t, token, "CSRF token should not be empty")
		assert.False(t, tokens[token], "CSRF token should be unique")
		tokens[token] = true
	}
}

// TestGenerateCSRFToken_Length 测试 CSRF token 长度
func TestGenerateCSRFToken_Length(t *testing.T) {
	token := GenerateCSRFToken()
	// CSRF token 应该有足够的熵（至少 16 字节的 hex = 32 字符）
	assert.GreaterOrEqual(t, len(token), 32,
		"CSRF token should be at least 32 characters for security")
}
