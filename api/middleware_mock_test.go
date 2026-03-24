package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// =====================================================================
// 认证中间件单元测试
// 测试不需要数据库的认证逻辑：JWT 解析、CSRF 验证、Cookie 处理
//
// 注意：API 设计使用 HTTP 200 状态码 + 响应体中的业务错误码
// 所以测试需要检查响应体中的 code 字段，而不是 HTTP 状态码
// =====================================================================

// createMockRouter 创建带认证中间件的测试路由器
func createMockRouter(authMiddleware gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 测试路由组
	protected := r.Group("/api", authMiddleware)
	{
		protected.GET("/test", func(c *gin.Context) {
			userID, _ := c.Get("user_id")
			c.JSON(200, gin.H{"code": 0, "user_id": userID})
		})
		protected.POST("/test-post", func(c *gin.Context) {
			userID, _ := c.Get("user_id")
			c.JSON(200, gin.H{"code": 0, "user_id": userID})
		})
	}

	return r
}

// parseResponseCode 解析响应中的业务错误码
func parseResponseCode(w *httptest.ResponseRecorder) int {
	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		return -1
	}
	return resp.Code
}

// assertAuthFailed 断言认证失败（业务错误码 401）
func assertAuthFailed(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, 200, w.Code, "HTTP status should be 200 (API convention)")
	code := parseResponseCode(w)
	assert.Equal(t, int(ErrorNotLogin), code, "Business code should be 401 (not login)")
}

// assertAuthSuccess 断言认证成功（业务错误码 0）
func assertAuthSuccess(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, 200, w.Code, "HTTP status should be 200")
	code := parseResponseCode(w)
	assert.Equal(t, 0, code, "Business code should be 0 (success)")
}

// ===================== JWT Token 解析错误测试（不需要数据库） =====================

// TestMiddleware_JWT_InvalidToken 测试无效 JWT Token 格式
func TestMiddleware_JWT_InvalidToken(t *testing.T) {
	testInitConfig()

	router := createMockRouter(AuthRequired())

	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: "invalid-token"})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 无效 token 格式应返回业务错误码 401
	assertAuthFailed(t, w)
}

// TestMiddleware_JWT_MalformedToken 测试格式错误的 JWT Token
func TestMiddleware_JWT_MalformedToken(t *testing.T) {
	testInitConfig()

	router := createMockRouter(AuthRequired())

	// 只有两段的 token（正常应该有三段）
	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: "header.payload"})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assertAuthFailed(t, w)
}

// TestMiddleware_JWT_ExpiredToken 测试过期的 JWT Token
func TestMiddleware_JWT_ExpiredToken(t *testing.T) {
	testInitConfig()

	// 生成已过期 token
	token := GenerateExpiredToken(123, "test-device")

	router := createMockRouter(AuthRequired())

	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: token})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 过期 token 应返回业务错误码 401
	assertAuthFailed(t, w)
}

// TestMiddleware_JWT_TamperedSignature 测试篡改签名的 JWT Token
func TestMiddleware_JWT_TamperedSignature(t *testing.T) {
	testInitConfig()

	// 生成有效 token
	token := GenerateTestToken(123, "test-device", 1*time.Hour)

	// 篡改签名（修改最后几个字符）
	tamperedToken := token[:len(token)-5] + "XXXXX"

	router := createMockRouter(AuthRequired())

	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: tamperedToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 篡改签名应返回业务错误码 401
	assertAuthFailed(t, w)
}

// TestMiddleware_JWT_WrongSecret 测试使用错误密钥签名的 Token
func TestMiddleware_JWT_WrongSecret(t *testing.T) {
	testInitConfig()

	// 使用错误的密钥手动创建 token
	wrongSecretToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoxMjN9.INVALID_SIGNATURE"

	router := createMockRouter(AuthRequired())

	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: wrongSecretToken})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assertAuthFailed(t, w)
}

// ===================== CSRF 验证测试（不需要数据库） =====================

// TestMiddleware_CSRF_POST_NoHeader 测试 POST 请求缺少 CSRF Header
func TestMiddleware_CSRF_POST_NoHeader(t *testing.T) {
	testInitConfig()

	// 使用有效 access token
	accessToken := GenerateTestToken(123, "", 1*time.Hour)

	router := createMockRouter(AuthRequired())

	// POST 请求带 Cookie 但无 CSRF Header
	req, _ := http.NewRequest("POST", "/api/test-post", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: accessToken})
	req.AddCookie(&http.Cookie{Name: CookieCSRFToken, Value: "csrf-token-value"})
	// 注意：没有设置 X-CSRF-Token header

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// CSRF 验证失败应返回业务错误码 401
	assertAuthFailed(t, w)
}

// TestMiddleware_CSRF_POST_NoCookie 测试 POST 请求缺少 CSRF Cookie
func TestMiddleware_CSRF_POST_NoCookie(t *testing.T) {
	testInitConfig()

	accessToken := GenerateTestToken(123, "", 1*time.Hour)

	router := createMockRouter(AuthRequired())

	req, _ := http.NewRequest("POST", "/api/test-post", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: accessToken})
	// 缺少 CSRF Cookie
	req.Header.Set("X-CSRF-Token", "some-token")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// CSRF Cookie 缺失应返回业务错误码 401
	assertAuthFailed(t, w)
}

// TestMiddleware_CSRF_POST_Mismatch 测试 POST 请求 CSRF Token 不匹配
func TestMiddleware_CSRF_POST_Mismatch(t *testing.T) {
	testInitConfig()

	accessToken := GenerateTestToken(123, "", 1*time.Hour)

	router := createMockRouter(AuthRequired())

	req, _ := http.NewRequest("POST", "/api/test-post", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: accessToken})
	req.AddCookie(&http.Cookie{Name: CookieCSRFToken, Value: "cookie-csrf-token"})
	req.Header.Set("X-CSRF-Token", "header-csrf-token") // 不匹配

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// CSRF 不匹配应返回业务错误码 401
	assertAuthFailed(t, w)
}

// TestMiddleware_CSRF_POST_EmptyHeader 测试 POST 请求空 CSRF Header
func TestMiddleware_CSRF_POST_EmptyHeader(t *testing.T) {
	testInitConfig()

	accessToken := GenerateTestToken(123, "", 1*time.Hour)

	router := createMockRouter(AuthRequired())

	req, _ := http.NewRequest("POST", "/api/test-post", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: accessToken})
	req.AddCookie(&http.Cookie{Name: CookieCSRFToken, Value: "csrf-token"})
	req.Header.Set("X-CSRF-Token", "") // 空 Header

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 空 CSRF header 应返回业务错误码 401
	assertAuthFailed(t, w)
}

// ===================== 认证缺失测试（不需要数据库） =====================

// TestMiddleware_NoAuth 测试完全没有认证信息
func TestMiddleware_NoAuth(t *testing.T) {
	testInitConfig()

	router := createMockRouter(AuthRequired())

	req, _ := http.NewRequest("GET", "/api/test", nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 无认证信息应返回业务错误码 401
	assertAuthFailed(t, w)
}

// TestMiddleware_EmptyCookie 测试空 Cookie 值
func TestMiddleware_EmptyCookie(t *testing.T) {
	testInitConfig()

	router := createMockRouter(AuthRequired())

	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: ""})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 空 Cookie 应返回业务错误码 401
	assertAuthFailed(t, w)
}

// TestMiddleware_EmptyBearerToken 测试空 Bearer Token
func TestMiddleware_EmptyBearerToken(t *testing.T) {
	testInitConfig()

	router := createMockRouter(AuthRequired())

	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer ")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 空 Bearer token 应返回业务错误码 401
	assertAuthFailed(t, w)
}

// TestMiddleware_InvalidAuthorizationFormat 测试错误的 Authorization 格式
func TestMiddleware_InvalidAuthorizationFormat(t *testing.T) {
	testInitConfig()

	router := createMockRouter(AuthRequired())

	// 使用 Basic 而不是 Bearer
	req, _ := http.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Basic sometoken")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 非 Bearer 格式应返回业务错误码 401
	assertAuthFailed(t, w)
}

// ===================== HTTP 方法与 CSRF 关系测试 =====================

// TestMiddleware_CSRF_PUT_Required 测试 PUT 请求需要 CSRF
func TestMiddleware_CSRF_PUT_Required(t *testing.T) {
	testInitConfig()

	accessToken := GenerateTestToken(123, "", 1*time.Hour)

	// 创建路由器并添加 PUT 路由
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	protected := r.Group("/api", AuthRequired())
	protected.PUT("/test-put", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 0})
	})

	// PUT 请求（修改操作）也需要 CSRF
	req, _ := http.NewRequest("PUT", "/api/test-put", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: accessToken})
	req.AddCookie(&http.Cookie{Name: CookieCSRFToken, Value: "csrf-token"})
	// 缺少 CSRF header

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// PUT 请求缺少 CSRF 应返回业务错误码 401
	assertAuthFailed(t, w)
}

// TestMiddleware_CSRF_DELETE_Required 测试 DELETE 请求需要 CSRF
func TestMiddleware_CSRF_DELETE_Required(t *testing.T) {
	testInitConfig()

	accessToken := GenerateTestToken(123, "", 1*time.Hour)

	// 创建路由器并添加 DELETE 路由
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	protected := r.Group("/api", AuthRequired())
	protected.DELETE("/test-delete", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 0})
	})

	// DELETE 请求（删除操作）也需要 CSRF
	req, _ := http.NewRequest("DELETE", "/api/test-delete", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: accessToken})
	req.AddCookie(&http.Cookie{Name: CookieCSRFToken, Value: "csrf-token"})
	// 缺少 CSRF header

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// DELETE 请求缺少 CSRF 应返回业务错误码 401
	assertAuthFailed(t, w)
}

// TestMiddleware_CSRF_PATCH_Required 测试 PATCH 请求需要 CSRF
func TestMiddleware_CSRF_PATCH_Required(t *testing.T) {
	testInitConfig()

	accessToken := GenerateTestToken(123, "", 1*time.Hour)

	// 创建路由器并添加 PATCH 路由
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	protected := r.Group("/api", AuthRequired())
	protected.PATCH("/test-patch", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 0})
	})

	// PATCH 请求也需要 CSRF
	req, _ := http.NewRequest("PATCH", "/api/test-patch", nil)
	req.AddCookie(&http.Cookie{Name: CookieAccessToken, Value: accessToken})
	req.AddCookie(&http.Cookie{Name: CookieCSRFToken, Value: "csrf-token"})
	// 缺少 CSRF header

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// PATCH 请求缺少 CSRF 应返回业务错误码 401
	assertAuthFailed(t, w)
}

// ===================== X-K2-Client header 解析测试 =====================

// TestParseClientHeader_Basic 测试基本格式（version + platform + arch）
func TestParseClientHeader_Basic(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected *AppInfo
	}{
		{
			name:   "macos desktop",
			header: "kaitu-service/0.4.0-beta.1 (macos; arm64)",
			expected: &AppInfo{
				Version:  "0.4.0-beta.1",
				Platform: "macos",
				Arch:     "arm64",
			},
		},
		{
			name:   "windows desktop",
			header: "kaitu-service/0.3.15 (windows; amd64)",
			expected: &AppInfo{
				Version:  "0.3.15",
				Platform: "windows",
				Arch:     "amd64",
			},
		},
		{
			name:   "ios mobile",
			header: "kaitu-service/0.4.0 (ios; arm64)",
			expected: &AppInfo{
				Version:  "0.4.0",
				Platform: "ios",
				Arch:     "arm64",
			},
		},
		{
			name:   "android mobile",
			header: "kaitu-service/0.4.0 (android; arm64)",
			expected: &AppInfo{
				Version:  "0.4.0",
				Platform: "android",
				Arch:     "arm64",
			},
		},
		{
			name:   "standalone web",
			header: "kaitu-service/standalone (web; unknown)",
			expected: &AppInfo{
				Version:  "standalone",
				Platform: "web",
				Arch:     "unknown",
			},
		},
		{
			name:   "with extra spaces",
			header: "kaitu-service/1.0.0 ( macos ; arm64 )",
			expected: &AppInfo{
				Version:  "1.0.0",
				Platform: "macos",
				Arch:     "arm64",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseClientHeader(tc.header)
			assert.NotNil(t, result, "parseClientHeader should not return nil")
			assert.Equal(t, tc.expected.Version, result.Version)
			assert.Equal(t, tc.expected.Platform, result.Platform)
			assert.Equal(t, tc.expected.Arch, result.Arch)
		})
	}
}

// TestParseClientHeader_Extended 测试扩展格式（包含 OS 版本和设备型号）
func TestParseClientHeader_Extended(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected *AppInfo
	}{
		{
			name:   "macOS with model",
			header: "kaitu-service/0.3.15 (macos; arm64; macOS 14.5; MacBookPro18,1)",
			expected: &AppInfo{
				Version:     "0.3.15",
				Platform:    "macos",
				Arch:        "arm64",
				OSVersion:   "macOS 14.5",
				DeviceModel: "MacBookPro18,1",
			},
		},
		{
			name:   "iOS with model",
			header: "kaitu-service/0.3.15 (ios; arm64; iOS 17.4; iPhone15,2)",
			expected: &AppInfo{
				Version:     "0.3.15",
				Platform:    "ios",
				Arch:        "arm64",
				OSVersion:   "iOS 17.4",
				DeviceModel: "iPhone15,2",
			},
		},
		{
			name:   "Windows with model",
			header: "kaitu-service/0.3.15 (windows; amd64; Windows 11 23H2; Dell XPS 15)",
			expected: &AppInfo{
				Version:     "0.3.15",
				Platform:    "windows",
				Arch:        "amd64",
				OSVersion:   "Windows 11 23H2",
				DeviceModel: "Dell XPS 15",
			},
		},
		{
			name:   "Linux with OS version only",
			header: "kaitu-service/0.3.15 (linux; amd64; Ubuntu 22.04)",
			expected: &AppInfo{
				Version:     "0.3.15",
				Platform:    "linux",
				Arch:        "amd64",
				OSVersion:   "Ubuntu 22.04",
				DeviceModel: "",
			},
		},
		{
			name:   "Android with model",
			header: "kaitu-service/0.3.15 (android; arm64; Android 14; Pixel 8 Pro)",
			expected: &AppInfo{
				Version:     "0.3.15",
				Platform:    "android",
				Arch:        "arm64",
				OSVersion:   "Android 14",
				DeviceModel: "Pixel 8 Pro",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseClientHeader(tc.header)
			assert.NotNil(t, result, "parseClientHeader should not return nil")
			assert.Equal(t, tc.expected.Version, result.Version)
			assert.Equal(t, tc.expected.Platform, result.Platform)
			assert.Equal(t, tc.expected.Arch, result.Arch)
			assert.Equal(t, tc.expected.OSVersion, result.OSVersion)
			assert.Equal(t, tc.expected.DeviceModel, result.DeviceModel)
		})
	}
}

// TestParseClientHeader_Invalid 测试无效格式
func TestParseClientHeader_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{
			name:   "empty string",
			header: "",
		},
		{
			name:   "browser user agent",
			header: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		},
		{
			name:   "wrong app name",
			header: "other-app/1.0 (macos; amd64)",
		},
		{
			name:   "missing parentheses",
			header: "kaitu-service/1.0 macos amd64",
		},
		{
			name:   "missing semicolon",
			header: "kaitu-service/1.0 (macos amd64)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseClientHeader(tc.header)
			assert.Nil(t, result, "parseClientHeader should return nil for invalid header")
		})
	}
}

// TestFillDeviceAppInfo 测试从 X-K2-Client header 填充设备信息
func TestFillDeviceAppInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("fills device fields from header", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/api/auth/login", nil)
		c.Request.Header.Set("X-K2-Client", "kaitu-service/0.4.0-beta.1 (macos; arm64)")

		device := &Device{}
		fillDeviceAppInfo(c, device)

		assert.Equal(t, "0.4.0-beta.1", device.AppVersion)
		assert.Equal(t, "macos", device.AppPlatform)
		assert.Equal(t, "arm64", device.AppArch)
	})

	t.Run("no header leaves device fields empty", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/api/auth/login", nil)

		device := &Device{}
		fillDeviceAppInfo(c, device)

		assert.Empty(t, device.AppVersion)
		assert.Empty(t, device.AppPlatform)
		assert.Empty(t, device.AppArch)
	})

	t.Run("invalid header leaves device fields empty", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/api/auth/login", nil)
		c.Request.Header.Set("X-K2-Client", "Mozilla/5.0 invalid")

		device := &Device{}
		fillDeviceAppInfo(c, device)

		assert.Empty(t, device.AppVersion)
		assert.Empty(t, device.AppPlatform)
	})

	t.Run("extended header fills all fields", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/api/auth/login", nil)
		c.Request.Header.Set("X-K2-Client", "kaitu-service/0.3.15 (ios; arm64; iOS 17.4; iPhone15,2)")

		device := &Device{}
		fillDeviceAppInfo(c, device)

		assert.Equal(t, "0.3.15", device.AppVersion)
		assert.Equal(t, "ios", device.AppPlatform)
		assert.Equal(t, "arm64", device.AppArch)
		assert.Equal(t, "iOS 17.4", device.OSVersion)
		assert.Equal(t, "iPhone15,2", device.DeviceModel)
	})
}

// ===================== RoleRequired 中间件测试（不需要数据库） =====================

// createRoleTestRouter 创建带角色检查的测试路由器
// 直接注入 authContext，绕过数据库查询
func createRoleTestRouter(user *User, requiredRole uint64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 直接注入认证上下文（模拟已登录用户）
	r.Use(func(c *gin.Context) {
		if user != nil {
			c.Set("authContext", &authContext{UserID: user.ID, User: user})
		}
		c.Next()
	})

	r.GET("/test", RoleRequired(requiredRole), func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 0})
	})
	return r
}

// assertForbidden 断言权限不足（业务错误码 403）
func assertForbidden(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, 200, w.Code, "HTTP status should be 200 (API convention)")
	var resp struct{ Code int `json:"code"` }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, int(ErrorForbidden), resp.Code, "Business code should be 403 (forbidden)")
}

// TestRoleRequired_NoUser 未登录用户被拒绝
func TestRoleRequired_NoUser(t *testing.T) {
	testInitConfig()
	r := createRoleTestRouter(nil, RoleDevopsViewer)
	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertAuthFailed(t, w)
}

// TestRoleRequired_ExactRole 拥有精确角色的用户通过
func TestRoleRequired_ExactRole(t *testing.T) {
	testInitConfig()
	user := &User{ID: 1, Roles: RoleDevopsViewer}
	r := createRoleTestRouter(user, RoleDevopsViewer)
	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertAuthSuccess(t, w)
}

// TestRoleRequired_WrongRole 拥有不同角色的用户被拒绝
func TestRoleRequired_WrongRole(t *testing.T) {
	testInitConfig()
	user := &User{ID: 2, Roles: RoleSupport} // 有 Support，但需要 DevopsViewer
	r := createRoleTestRouter(user, RoleDevopsViewer)
	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertForbidden(t, w)
}

// TestRoleRequired_RoleUser_Denied 普通用户（RoleUser）被拒绝
func TestRoleRequired_RoleUser_Denied(t *testing.T) {
	testInitConfig()
	user := &User{ID: 3, Roles: RoleUser}
	r := createRoleTestRouter(user, RoleDevopsViewer)
	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertForbidden(t, w)
}

// TestRoleRequired_IsAdmin_Bypass 超级管理员（IsAdmin=true）直接通过，不检查 Roles
func TestRoleRequired_IsAdmin_Bypass(t *testing.T) {
	testInitConfig()
	isAdmin := true
	user := &User{ID: 4, Roles: RoleUser, IsAdmin: &isAdmin} // Roles 没有 DevopsViewer，但 IsAdmin=true
	r := createRoleTestRouter(user, RoleDevopsViewer)
	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertAuthSuccess(t, w)
}

// TestRoleRequired_CombinedRole_EitherSuffices 组合角色检查（任一满足即通过）
func TestRoleRequired_CombinedRole_EitherSuffices(t *testing.T) {
	testInitConfig()
	viewOrEdit := RoleDevopsViewer | RoleDevopsEditor

	// 只有 DevopsViewer：应通过
	t.Run("viewer passes viewOrEdit check", func(t *testing.T) {
		user := &User{ID: 5, Roles: RoleDevopsViewer}
		r := createRoleTestRouter(user, viewOrEdit)
		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertAuthSuccess(t, w)
	})

	// 只有 DevopsEditor：应通过
	t.Run("editor passes viewOrEdit check", func(t *testing.T) {
		user := &User{ID: 6, Roles: RoleDevopsEditor}
		r := createRoleTestRouter(user, viewOrEdit)
		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertAuthSuccess(t, w)
	})

	// 只有 Support（不在组合内）：应被拒绝
	t.Run("support denied for viewOrEdit check", func(t *testing.T) {
		user := &User{ID: 7, Roles: RoleSupport}
		r := createRoleTestRouter(user, viewOrEdit)
		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertForbidden(t, w)
	})
}

// TestRoleRequired_MultipleRoles 用户拥有多个角色时，任一满足即通过
func TestRoleRequired_MultipleRoles(t *testing.T) {
	testInitConfig()
	// 用户同时有 DevopsViewer + Support
	user := &User{ID: 8, Roles: RoleDevopsViewer | RoleSupport}

	// 检查需要 DevopsEditor：应失败（没有 DevopsEditor bit）
	t.Run("missing editor role denied", func(t *testing.T) {
		r := createRoleTestRouter(user, RoleDevopsEditor)
		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertForbidden(t, w)
	})

	// 检查需要 Support：应通过
	t.Run("has support role passes", func(t *testing.T) {
		r := createRoleTestRouter(user, RoleSupport)
		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertAuthSuccess(t, w)
	})
}
