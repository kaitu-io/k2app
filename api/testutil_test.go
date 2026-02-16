package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/util"
)

// ===================== Test Initialization =====================

// testInitOnce ensures test configuration is initialized only once
var testInitOnce bool

// testMiniRedis is the in-memory Redis server for tests
var testMiniRedis *miniredis.Miniredis

// testInitConfig initializes test configuration
func testInitConfig() {
	if testInitOnce {
		return
	}
	testInitOnce = true

	// Start miniredis for asynq/redis dependencies
	var err error
	testMiniRedis, err = miniredis.Run()
	if err != nil {
		panic("failed to start miniredis: " + err.Error())
	}

	// Set Redis config BEFORE loading config file (viper merges, later values override)
	viper.Set("redis.addr", testMiniRedis.Addr())
	viper.Set("redis.password", "")
	viper.Set("redis.db", 0)

	if os.Getenv("KAITU_TEST_CONFIG") != "1" {
		os.Setenv("KAITU_TEST_CONFIG", "1")
		viper.SetConfigFile("../center/config.yml")
		viper.ReadInConfig()
		util.SetConfigFile("../center/config.yml")
	}

	// Re-set Redis config after config file load to ensure it takes precedence
	viper.Set("redis.addr", testMiniRedis.Addr())
	viper.Set("redis.password", "")
	viper.Set("redis.db", 0)

	// Enable mock verification code
	EnableMockVerificationCode = true
}

// ===================== Router 设置 =====================

// SetupTestRouter 创建包含完整中间件的测试路由器
// 用于测试认证流程（Cookie、CSRF、Bearer Token）
func SetupTestRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 认证路由（无需认证）
	auth := r.Group("/api/auth")
	{
		auth.POST("/code", api_send_auth_code)
		auth.POST("/login", api_login)
		auth.POST("/web-login", api_web_auth)
		auth.POST("/refresh", api_refresh_token)
	}

	// 用户路由（需要认证）
	user := r.Group("/api/user", AuthRequired())
	{
		user.GET("/info", api_get_user_info)
		user.GET("/devices", api_get_devices)
		user.POST("/test-post", func(c *gin.Context) {
			// 用于测试 CSRF
			c.JSON(200, gin.H{"code": 0, "message": "ok", "data": gin.H{}})
		})
	}

	return r
}

// SetupMinimalRouter 创建最小化测试路由器（不包含数据库依赖）
func SetupMinimalRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	return r
}

// ===================== 请求构建器 =====================

// TestRequest 测试请求构建器
type TestRequest struct {
	method      string
	path        string
	body        interface{}
	cookies     []*http.Cookie
	headers     map[string]string
	bearerToken string
}

// NewTestRequest 创建新的测试请求
func NewTestRequest(method, path string) *TestRequest {
	return &TestRequest{
		method:  method,
		path:    path,
		headers: make(map[string]string),
	}
}

// WithBody 设置请求体
func (r *TestRequest) WithBody(body interface{}) *TestRequest {
	r.body = body
	return r
}

// WithCookie 添加 Cookie
func (r *TestRequest) WithCookie(name, value string) *TestRequest {
	r.cookies = append(r.cookies, &http.Cookie{Name: name, Value: value})
	return r
}

// WithHeader 添加请求头
func (r *TestRequest) WithHeader(key, value string) *TestRequest {
	r.headers[key] = value
	return r
}

// WithBearerToken 设置 Bearer Token
func (r *TestRequest) WithBearerToken(token string) *TestRequest {
	r.bearerToken = token
	return r
}

// WithCSRFToken 设置 CSRF Token（同时设置 Header 和 Cookie）
func (r *TestRequest) WithCSRFToken(token string) *TestRequest {
	r.headers["X-CSRF-Token"] = token
	r.cookies = append(r.cookies, &http.Cookie{Name: CookieCSRFToken, Value: token})
	return r
}

// Build 构建 HTTP 请求
func (r *TestRequest) Build() (*http.Request, error) {
	var bodyReader *bytes.Reader
	if r.body != nil {
		bodyBytes, err := json.Marshal(r.body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(bodyBytes)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(r.method, r.path, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	// 添加 Bearer Token
	if r.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.bearerToken)
	}

	// 添加自定义 headers
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}

	// 添加 cookies
	for _, c := range r.cookies {
		req.AddCookie(c)
	}

	return req, nil
}

// Execute 执行请求并返回响应
func (r *TestRequest) Execute(router *gin.Engine) *httptest.ResponseRecorder {
	req, err := r.Build()
	if err != nil {
		panic(err)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ===================== 响应解析 =====================

// TestResponse 通用测试响应
type TestResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// ParseResponse 解析响应
func ParseResponse(w *httptest.ResponseRecorder) (*TestResponse, error) {
	var resp TestResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ParseResponseData 解析响应数据到指定结构
func ParseResponseData[T any](w *httptest.ResponseRecorder) (*T, error) {
	resp, err := ParseResponse(w)
	if err != nil {
		return nil, err
	}
	var data T
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// ===================== Token 生成 =====================

// GenerateTestToken 生成测试用 JWT Token
func GenerateTestToken(userID uint64, deviceID string, expiry time.Duration) string {
	testInitConfig()
	jwtConfig := configJwt(nil)
	jwtSecret := []byte(jwtConfig.Secret)

	now := time.Now()
	claims := TokenClaims{
		UserID:       userID,
		DeviceID:     deviceID,
		Exp:          now.Add(expiry).Unix(),
		Type:         TokenTypeAccess,
		TokenIssueAt: now.Unix(),
	}

	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
	return token
}

// GenerateExpiredToken 生成已过期的 Token
func GenerateExpiredToken(userID uint64, deviceID string) string {
	return GenerateTestToken(userID, deviceID, -1*time.Hour)
}

// GenerateExpiringToken 生成即将过期的 Token（用于测试 sliding expiration）
func GenerateExpiringToken(userID uint64, remaining time.Duration) string {
	testInitConfig()
	jwtConfig := configJwt(nil)
	jwtSecret := []byte(jwtConfig.Secret)

	now := time.Now()
	claims := TokenClaims{
		UserID:       userID,
		DeviceID:     "", // Web 认证无设备
		Exp:          now.Add(remaining).Unix(),
		Type:         TokenTypeAccess,
		TokenIssueAt: now.Unix(),
	}

	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
	return token
}

// ===================== Cookie 辅助 =====================

// GetCookieByName 从响应中获取指定名称的 Cookie
func GetCookieByName(w *httptest.ResponseRecorder, name string) *http.Cookie {
	cookies := w.Result().Cookies()
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// GetAccessTokenCookie 获取 access_token Cookie
func GetAccessTokenCookie(w *httptest.ResponseRecorder) *http.Cookie {
	return GetCookieByName(w, CookieAccessToken)
}

// GetCSRFTokenCookie 获取 csrf_token Cookie
func GetCSRFTokenCookie(w *httptest.ResponseRecorder) *http.Cookie {
	return GetCookieByName(w, CookieCSRFToken)
}

// ===================== 测试数据创建 =====================

// CreateTestUser 创建测试用户（需要数据库）
func CreateTestUser(t *testing.T) *User {
	testInitConfig()

	user := &User{
		UUID:      generateId("test-user"),
		AccessKey: generateAccessKey(),
		Language:  "zh-CN",
	}

	if err := db.Get().Create(user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	t.Cleanup(func() {
		db.Get().Delete(user)
	})

	return user
}

// CreateTestDevice 创建测试设备（需要数据库）
func CreateTestDevice(t *testing.T, userID uint64, udid string) *Device {
	testInitConfig()

	device := &Device{
		UDID:         udid,
		UserID:       userID,
		Remark:       "Test Device",
		TokenIssueAt: time.Now().Unix(),
	}

	if err := db.Get().Create(device).Error; err != nil {
		t.Fatalf("Failed to create test device: %v", err)
	}

	t.Cleanup(func() {
		db.Get().Delete(device)
	})

	return device
}

// CreateTestEmailTemplate 创建测试邮件模板（需要数据库）
func CreateTestEmailTemplate(t *testing.T) *EmailMarketingTemplate {
	testInitConfig()

	isActive := true
	template := &EmailMarketingTemplate{
		Name:     "Test Template",
		Language: "zh-CN",
		Subject:  "Test Subject",
		Content:  "Test Content",
		IsActive: &isActive,
	}

	if err := db.Get().Create(template).Error; err != nil {
		t.Fatalf("Failed to create test email template: %v", err)
	}

	t.Cleanup(func() {
		db.Get().Delete(template)
	})

	return template
}

// ===================== 断言辅助 =====================

// AssertResponseCode 断言响应业务代码
func AssertResponseCode(t *testing.T, w *httptest.ResponseRecorder, expectedCode int) {
	t.Helper()
	resp, err := ParseResponse(w)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if resp.Code != expectedCode {
		t.Errorf("Expected code %d, got %d (message: %s)", expectedCode, resp.Code, resp.Message)
	}
}

// AssertHTTPStatus 断言 HTTP 状态码
func AssertHTTPStatus(t *testing.T, w *httptest.ResponseRecorder, expectedStatus int) {
	t.Helper()
	if w.Code != expectedStatus {
		t.Errorf("Expected HTTP status %d, got %d", expectedStatus, w.Code)
	}
}

// AssertCookieExists 断言 Cookie 存在
func AssertCookieExists(t *testing.T, w *httptest.ResponseRecorder, cookieName string) *http.Cookie {
	t.Helper()
	cookie := GetCookieByName(w, cookieName)
	if cookie == nil {
		t.Errorf("Expected cookie %s to exist", cookieName)
		return nil
	}
	return cookie
}

// AssertCookieHttpOnly 断言 Cookie 是 HttpOnly
func AssertCookieHttpOnly(t *testing.T, cookie *http.Cookie) {
	t.Helper()
	if cookie == nil {
		t.Error("Cookie is nil")
		return
	}
	if !cookie.HttpOnly {
		t.Errorf("Expected cookie %s to be HttpOnly", cookie.Name)
	}
}

// AssertCookieSameSite 断言 Cookie SameSite 属性
func AssertCookieSameSite(t *testing.T, cookie *http.Cookie, expected http.SameSite) {
	t.Helper()
	if cookie == nil {
		t.Error("Cookie is nil")
		return
	}
	if cookie.SameSite != expected {
		t.Errorf("Expected cookie %s SameSite to be %v, got %v", cookie.Name, expected, cookie.SameSite)
	}
}
