package center

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
	"gorm.io/gorm"
)

// AppInfo 从 X-K2-Client header 解析出的应用信息
type AppInfo struct {
	Version     string // 应用版本，如 "1.0.0"
	Platform    string // 运行平台，如 "macos", "windows", "linux", "ios", "android"
	Arch        string // CPU架构，如 "amd64", "arm64"
	OSVersion   string // 系统版本，如 "14.5", "11", "23H2"
	DeviceModel string // 设备型号，如 "MacBookPro18,1", "iPhone15,2"
}

// X-K2-Client header 格式:
// Basic:    kaitu-service/{version} ({platform}; {arch})
// Extended: kaitu-service/{version} ({platform}; {arch}; {os_version}; {device_model})
//
// 示例:
//   kaitu-service/0.4.0-beta.1 (macos; arm64)
//   kaitu-service/0.3.15 (macos; arm64; macOS 14.5; MacBookPro18,1)
//   kaitu-service/0.3.15 (ios; arm64; iOS 17.4; iPhone15,2)
//   kaitu-service/0.3.15 (windows; amd64; Windows 11 23H2; Dell XPS 15)
var clientHeaderRegex = regexp.MustCompile(`^kaitu-service/([^\s]+)\s*\(([^;)]+);\s*([^;)]+)(?:;\s*([^;)]+))?(?:;\s*([^)]+))?\)`)

// parseClientHeader 解析 X-K2-Client header 获取应用信息
func parseClientHeader(header string) *AppInfo {
	matches := clientHeaderRegex.FindStringSubmatch(header)
	if len(matches) < 4 {
		return nil
	}

	info := &AppInfo{
		Version:  strings.TrimSpace(matches[1]),
		Platform: strings.TrimSpace(matches[2]),
		Arch:     strings.TrimSpace(matches[3]),
	}

	// Parse extended fields if present
	if len(matches) > 4 && matches[4] != "" {
		info.OSVersion = strings.TrimSpace(matches[4])
	}
	if len(matches) > 5 && matches[5] != "" {
		info.DeviceModel = strings.TrimSpace(matches[5])
	}

	return info
}

// fillDeviceAppInfo 从 X-K2-Client header 填充设备的应用版本信息（用于设备创建时）
func fillDeviceAppInfo(c *gin.Context, device *Device) {
	if clientHeader := c.GetHeader("X-K2-Client"); clientHeader != "" {
		if appInfo := parseClientHeader(clientHeader); appInfo != nil {
			device.AppVersion = appInfo.Version
			device.AppPlatform = appInfo.Platform
			device.AppArch = appInfo.Arch
			device.OSVersion = appInfo.OSVersion
			device.DeviceModel = appInfo.DeviceModel
		}
	}
}

// updateDeviceAppInfo 更新设备的应用版本信息（如果有变化）
func updateDeviceAppInfo(c *gin.Context, device *Device, appInfo *AppInfo) {
	if appInfo == nil || device == nil {
		return
	}

	// 检查是否需要更新
	if device.AppVersion == appInfo.Version &&
		device.AppPlatform == appInfo.Platform &&
		device.AppArch == appInfo.Arch &&
		device.OSVersion == appInfo.OSVersion &&
		device.DeviceModel == appInfo.DeviceModel {
		return // 没有变化，不需要更新
	}

	// 更新设备的应用信息
	updates := map[string]interface{}{
		"app_version":  appInfo.Version,
		"app_platform": appInfo.Platform,
		"app_arch":     appInfo.Arch,
		"os_version":   appInfo.OSVersion,
		"device_model": appInfo.DeviceModel,
	}

	if err := db.Get().Model(device).Updates(updates).Error; err != nil {
		log.Warnf(c, "failed to update device app info for %s: %v", device.UDID, err)
		return
	}

	log.Debugf(c, "updated device %s app info: version=%s, platform=%s, arch=%s, os=%s, model=%s",
		device.UDID, appInfo.Version, appInfo.Platform, appInfo.Arch, appInfo.OSVersion, appInfo.DeviceModel)

	// 更新内存中的设备对象
	device.AppVersion = appInfo.Version
	device.AppPlatform = appInfo.Platform
	device.AppArch = appInfo.Arch
	device.OSVersion = appInfo.OSVersion
	device.DeviceModel = appInfo.DeviceModel
}

// 认证相关错误
var (
	ErrMissingToken = errors.New("missing token")
	ErrTokenExpired = errors.New("token expired")
)

// authContext 认证上下文
type authContext struct {
	UserID uint64
	UDID   string
	Device *Device
	User   *User
}

// getAuthContext 获取认证上下文，确保只执行一次
// 认证优先级：
// 1. HttpOnly Cookie (Web 端，需要 CSRF 验证)
// 2. X-Access-Key 头部 (API 密钥认证)
// 3. Authorization: Bearer <token> (非 Web 端，如 Desktop/Mobile)
// 4. URL 查询参数 ?token=xxx (WebSocket 跨域认证，因为 WS 无法携带跨域 Cookie)
func getAuthContext(c *gin.Context) *authContext {
	if ctx, exists := c.Get("authContext"); exists {
		log.Debugf(c, "auth context already exists, reusing")
		return ctx.(*authContext)
	}

	// 1. 优先检查 HttpOnly Cookie (Web 端)
	token, cookieErr := c.Cookie(CookieAccessToken)
	log.Debugf(c, "cookie check: name=%s, hasValue=%v, err=%v", CookieAccessToken, token != "", cookieErr)
	if cookieErr == nil && token != "" {
		log.Debugf(c, "processing token from cookie, tokenLen=%d", len(token))
		// 对于非 GET 请求，验证 CSRF Token
		if c.Request.Method != "GET" {
			csrfHeader := c.GetHeader("X-CSRF-Token")
			csrfCookie, csrfCookieErr := c.Cookie(CookieCSRFToken)
			log.Debugf(c, "CSRF check: header=%q, cookie=%q, cookieErr=%v", csrfHeader, csrfCookie, csrfCookieErr)
			if csrfHeader == "" || csrfHeader != csrfCookie {
				log.Warnf(c, "CSRF token mismatch for %s request: header=%q, cookie=%q", c.Request.Method, csrfHeader, csrfCookie)
				return nil
			}
		}
		return handleCookieJWTAuth(c, token)
	} else {
		log.Debugf(c, "no valid cookie found: cookieErr=%v, tokenEmpty=%v", cookieErr, token == "")
	}

	// 2. 检查 X-Access-Key 头部（AccessKey认证）
	accessKey := c.GetHeader("X-Access-Key")
	log.Debugf(c, "X-Access-Key check: hasValue=%v", accessKey != "")
	if accessKey != "" {
		return handleAccessKeyAuth(c, accessKey)
	}

	// 3. 支持 Authorization: Bearer <token> （非 Web 端 JWT 认证）
	authHeader := c.GetHeader("Authorization")
	log.Debugf(c, "Authorization header check: hasValue=%v, isBearerPrefix=%v", authHeader != "", strings.HasPrefix(authHeader, "Bearer "))
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
		token = strings.TrimSpace(token)
		if token != "" {
			return handleJWTAuth(c, token)
		}
	}

	// 4. 检查 URL 查询参数 ?token=xxx (WebSocket 跨域认证)
	// WebSocket 连接无法携带跨域 Cookie，需要通过 URL 参数传递 token
	queryToken := c.Query("token")
	log.Debugf(c, "query token check: hasValue=%v", queryToken != "")
	if queryToken != "" {
		log.Debugf(c, "processing token from URL query parameter")
		return handleJWTAuth(c, queryToken)
	}

	log.Infof(c, "missing authentication in request: no cookie, no X-Access-Key, no Bearer token, no query token")
	return nil
}

// handleCookieJWTAuth 处理 Cookie JWT 认证（带 sliding expiration）
// 当 token 剩余有效期不足 7 天时，自动续期
func handleCookieJWTAuth(c *gin.Context, token string) *authContext {
	// 先执行标准 JWT 认证
	authCtx := handleJWTAuth(c, token)
	if authCtx == nil {
		return nil
	}

	// 对于 Web 认证（无设备），检查是否需要 sliding expiration 续期
	if authCtx.UDID == "" && authCtx.Device == nil {
		// 解析 token 获取过期时间
		parsedToken, err := jwt.ParseWithClaims(token, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
			jwtConfig := configJwt(c)
			return []byte(jwtConfig.Secret), nil
		})
		if err == nil {
			if claims, ok := parsedToken.Claims.(*TokenClaims); ok && parsedToken.Valid {
				// 检查剩余有效期
				expTime := time.Unix(claims.Exp, 0)
				remaining := time.Until(expTime)

				// 如果剩余不足 7 天，自动续期
				if remaining < WebCookieRenewalThreshold {
					log.Infof(c, "sliding expiration: renewing web cookie for user %d (remaining: %v)", authCtx.UserID, remaining)

					// 生成新的 token，保留原 token 中的 roles
					newAuthResult, _, err := generateWebCookieToken(c, authCtx.UserID, claims.Roles)
					if err != nil {
						log.Errorf(c, "failed to renew web cookie for user %d: %v", authCtx.UserID, err)
						// 续期失败不影响当前请求，继续使用旧 token
					} else {
						// 设置新的 Cookie
						setAuthCookies(c, newAuthResult)
						log.Infof(c, "successfully renewed web cookie for user %d", authCtx.UserID)
					}
				}
			}
		}
	}

	return authCtx
}

// handleAccessKeyAuth 处理AccessKey认证
func handleAccessKeyAuth(c *gin.Context, accessKey string) *authContext {
	log.Debugf(c, "processing access key authentication")

	var user User
	if err := db.Get().Where(&User{AccessKey: accessKey}).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warnf(c, "user not found for access key: %s", accessKey)
			return nil
		}
		// 数据库临时故障时返回认证失败，而非 panic 导致 500
		log.Errorf(c, "database error while finding user by access key: %v", err)
		return nil
	}

	// 创建AccessKey认证上下文（类似Web认证，无设备信息）
	log.Debugf(c, "creating new access key auth context for user %d", user.ID)
	authCtx := &authContext{
		UserID: user.ID,
		UDID:   "",
		Device: nil,
		User:   &user,
	}
	c.Set("authContext", authCtx)
	return authCtx
}

// handleJWTAuth 处理JWT认证
func handleJWTAuth(c *gin.Context, token string) *authContext {
	// 解析 JWT token
	parsedToken, err := jwt.ParseWithClaims(token, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		jwtConfig := configJwt(c)
		return []byte(jwtConfig.Secret), nil
	})
	if err != nil {
		log.Warnf(c, "failed to parse jwt token=%s: err=%v", token, err)
		return nil
	}
	claims, ok := parsedToken.Claims.(*TokenClaims)
	if !ok || !parsedToken.Valid {
		log.Warnf(c, "invalid jwt token claims")
		return nil
	}

	// 从JWT token中获取UDID
	udid := claims.DeviceID

	// 处理Web认证（无设备）和设备认证两种情况
	if udid == "" {
		// Web认证模式：无设备信息，直接通过用户ID获取用户信息
		log.Debugf(c, "processing web auth for user %d (no device)", claims.UserID)
		var user User
		if err := db.Get().First(&user, claims.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				log.Warnf(c, "user not found for web auth: %d", claims.UserID)
				return nil
			}
			// 数据库临时故障时返回认证失败，而非 panic 导致 500
			log.Errorf(c, "database error while finding user %d for web auth: %v", claims.UserID, err)
			return nil
		}

		// 创建Web认证上下文
		log.Debugf(c, "creating new web auth context for user %d", claims.UserID)
		authCtx := &authContext{
			UserID: claims.UserID,
			UDID:   "",
			Device: nil,
			User:   &user,
		}
		c.Set("authContext", authCtx)
		return authCtx
	}

	// 设备认证模式：需要查找设备并校验
	log.Debugf(c, "processing device auth for user %d, device %s", claims.UserID, udid)
	var device Device
	if err := db.Get().Preload("User").Where("udid = ? AND user_id = ?", udid, claims.UserID).First(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warnf(c, "device not found for udid: %s, user: %d", udid, claims.UserID)
			return nil
		}
		// 数据库临时故障时返回认证失败，而非 panic 导致 500
		log.Errorf(c, "database error while finding device %s: %v", udid, err)
		return nil
	}

	// 校验 TokenIssueAt
	if device.TokenIssueAt != claims.TokenIssueAt {
		log.Warnf(c, "token issue at mismatch for device: %s, user: %d", udid, claims.UserID)
		return nil
	}

	// 解析 X-K2-Client header 并更新设备的应用版本信息
	if clientHeader := c.GetHeader("X-K2-Client"); clientHeader != "" {
		if appInfo := parseClientHeader(clientHeader); appInfo != nil {
			updateDeviceAppInfo(c, &device, appInfo)
		}
	}

	// 创建设备认证上下文
	log.Debugf(c, "creating new device auth context for user %d, device %s", device.UserID, device.UDID)
	authCtx := &authContext{
		UserID: device.UserID,
		UDID:   device.UDID,
		Device: &device,
		User:   device.User,
	}
	c.Set("authContext", authCtx)
	return authCtx
}

// ReqUserID 从上下文中获取用户ID
func ReqUserID(c *gin.Context) uint64 {
	ctx := getAuthContext(c)
	if ctx == nil {
		return 0
	}
	return ctx.UserID
}

// ReqUser 从上下文中获取用户
func ReqUser(c *gin.Context) *User {
	ctx := getAuthContext(c)
	if ctx == nil {
		return nil
	}
	return ctx.User
}

// ReqDevice 从上下文中获取设备
func ReqDevice(c *gin.Context) *Device {
	ctx := getAuthContext(c)
	if ctx == nil {
		return nil
	}
	return ctx.Device
}

func ReqUDID(c *gin.Context) string {
	ctx := getAuthContext(c)
	if ctx == nil {
		return ""
	}
	return ctx.UDID
}

// AuthRequired 认证中间件
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := getAuthContext(c)
		if ctx == nil {
			log.Warnf(c, "auth failed for request to %s", c.Request.URL.Path)
			Error(c, ErrorNotLogin, "authentication failed")
			c.Abort()
			return
		}
		c.Next()
	}
}

// AuthOptional 可选认证中间件
// 尝试进行认证，但如果认证失败也允许请求继续
// 适用于支持匿名访问但登录用户可获得更多功能的接口
func AuthOptional() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 尝试认证，但不阻止请求
		// getAuthContext 会将认证信息存储到上下文中（如果认证成功）
		ctx := getAuthContext(c)
		if ctx != nil {
			// 将 claims 信息存储到上下文中，供 ReqUserIDOptional 使用
			c.Set("claims", &TokenClaims{UserID: ctx.UserID})
		}
		c.Next()
	}
}

// 允许完成首单且有效期用户
func ProRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := ReqUser(c)
		if user == nil {
			log.Warnf(c, "vip required: auth failed for request to %s", c.Request.URL.Path)
			Error(c, ErrorNotLogin, "authentication failed")
			c.Abort()
			return
		}
		if user.IsExpired() {
			log.Warnf(c, "vip required: user %d membership expired, request to %s denied", user.ID, c.Request.URL.Path)
			Error(c, ErrorPaymentRequired, "membership expired")
			c.Abort()
			return
		}
		c.Next()
	}
}

// SlaveAuthRequired authenticates slave nodes via Basic Auth (IPv4:NodeSecret).
func SlaveAuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		identifier, secretToken, ok := c.Request.BasicAuth()
		if !ok {
			log.Warnf(c, "invalid basic auth")
			Error(c, ErrorNotLogin, "invalid authorization format")
			c.Abort()
			return
		}

		// Node-level auth: IPv4 + NodeSecret
		var node SlaveNode
		err := db.Get().Where(&SlaveNode{Ipv4: identifier}).First(&node).Error
		if err != nil {
			log.Warnf(c, "invalid credentials: %s (node not found)", identifier)
			Error(c, ErrorNotLogin, "invalid node credentials")
			c.Abort()
			return
		}

		if node.SecretToken != secretToken {
			log.Warnf(c, "invalid credentials: %s (secret mismatch)", identifier)
			Error(c, ErrorNotLogin, "invalid secret token")
			c.Abort()
			return
		}

		c.Set("i_am_the_node", &node)
		c.Next()
	}
}

func ReqSlaveNode(c *gin.Context) *SlaveNode {
	node, exist := c.Get("i_am_the_node")
	if !exist {
		return nil
	}
	return node.(*SlaveNode)
}

// DeviceAuthRequired 设备认证中间件 - 要求请求必须包含有效的设备信息
func DeviceAuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := getAuthContext(c)
		if ctx == nil {
			log.Warnf(c, "device required: auth failed for request to %s", c.Request.URL.Path)
			Error(c, ErrorNotLogin, "authentication failed")
			c.Abort()
			return
		}
		if ctx.Device == nil {
			log.Warnf(c, "device required: no device info for user %d, request to %s denied", ctx.UserID, c.Request.URL.Path)
			Error(c, ErrorForbidden, "device authentication required")
			c.Abort()
			return
		}
		c.Next()
	}
}

func AdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := ReqUser(c)
		if user == nil {
			log.Warnf(c, "admin required: auth failed for request to %s", c.Request.URL.Path)
			Error(c, ErrorNotLogin, "authentication failed")
			c.Abort()
			return
		}
		if user.IsAdmin == nil || !*user.IsAdmin {
			log.Warnf(c, "admin required: user %d is not admin, request to %s denied", user.ID, c.Request.URL.Path)
			Error(c, ErrorForbidden, "permission denied")
			c.Abort()
			return
		}
		c.Next()
	}
}

// RoleRequired 细粒度权限检查：IsAdmin=true 直接通过；否则检查 user.Roles 是否包含指定角色。
// role 参数支持位或组合：RoleRequired(RoleOpsViewer | RoleOpsEditor) 表示任一满足即通过。
// 权限来源：从 DB 加载的 User 结构体（通过 ReqUser(c)），与 AdminRequired() 读取 IsAdmin 一致。
// 角色变更立即生效（下次请求），无需重新签发 token。
func RoleRequired(role uint64) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := ReqUser(c)
		if user == nil {
			Error(c, ErrorNotLogin, "authentication failed")
			c.Abort()
			return
		}
		if user.IsAdmin != nil && *user.IsAdmin {
			c.Next()
			return
		}
		if !HasRole(user.Roles, role) {
			log.Warnf(c, "role check failed: need=%d user=%d roles=%d path=%s",
				role, user.ID, user.Roles, c.Request.URL.Path)
			Error(c, ErrorForbidden, "permission denied")
			c.Abort()
			return
		}
		c.Next()
	}
}

// asynqmonAuthMiddleware provides authentication for the Asynq monitoring dashboard.
// NOTE: Returns HTML responses (not JSON) because asynqmon is a browser-facing monitoring UI.
// Users access it directly in their browser, so HTML error pages provide better UX than raw JSON.
func asynqmonAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := ReqUser(c)
		if user == nil {
			log.Warnf(c, "asynqmon auth failed: not logged in for request to %s", c.Request.URL.Path)
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.String(401, `<!DOCTYPE html>
<html>
<head><title>Authentication Required</title></head>
<body style="font-family: sans-serif; text-align: center; padding-top: 100px;">
<h1>🔒 Authentication Required</h1>
<p>Please authenticate via API before accessing Asynqmon.</p>
<p><a href="/api/auth/login">Login via API</a></p>
</body>
</html>`)
			c.Abort()
			return
		}
		if user.IsAdmin == nil || !*user.IsAdmin {
			log.Warnf(c, "asynqmon auth failed: user %d is not admin for request to %s", user.ID, c.Request.URL.Path)
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.String(403, `<!DOCTYPE html>
<html>
<head><title>Access Denied</title></head>
<body style="font-family: sans-serif; text-align: center; padding-top: 100px;">
<h1>⛔ Access Denied</h1>
<p>Admin privileges required to access Asynqmon.</p>
</body>
</html>`)
			c.Abort()
			return
		}
		c.Next()
	}
}

// RetailerRequired 分销商权限中间件
func RetailerRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := ReqUser(c)
		if user == nil {
			log.Warnf(c, "retailer required: auth failed for request to %s", c.Request.URL.Path)
			Error(c, ErrorNotLogin, "authentication failed")
			c.Abort()
			return
		}
		if user.IsRetailer == nil || !*user.IsRetailer {
			log.Warnf(c, "retailer required: user %d is not a retailer, request to %s denied", user.ID, c.Request.URL.Path)
			Error(c, ErrorForbidden, "retailer permission required")
			c.Abort()
			return
		}
		c.Next()
	}
}

func MiddleRecovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		stack := debug.Stack()
		log.Errorf(c, "request crash with err:%+v\nStack trace:\n%s", recovered, stack)
		c.AbortWithStatus(500)
		slack.Send("alert", fmt.Sprintf("[kaitu]请关注有crash问题：%v\n\n堆栈信息：\n%s", recovered, stack))
	})
}

// isPrivateOrigin checks if the origin is from localhost or RFC 1918 private network.
// Matches: localhost, 127.0.0.1, capacitor://localhost, 10.x.x.x, 172.16-31.x.x, 192.168.x.x
func isPrivateOrigin(origin string) bool {
	// capacitor://localhost (iOS WebView)
	if origin == "capacitor://localhost" {
		return true
	}

	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}

	host := parsed.Hostname()
	if host == "" {
		return false
	}

	// localhost and *.localhost (RFC 6761 — .localhost TLD always resolves to loopback)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	// 127.0.0.1
	if ip.IsLoopback() {
		return true
	}

	// RFC 1918 private ranges
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	// 10.0.0.0/8
	if ip4[0] == 10 {
		return true
	}
	// 172.16.0.0/12
	if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
		return true
	}
	// 192.168.0.0/16
	if ip4[0] == 192 && ip4[1] == 168 {
		return true
	}

	return false
}

// ApiCORSMiddleware handles CORS for /api/* client routes.
// Only allows local/LAN origins (localhost, 127.0.0.1, RFC 1918 private IPs,
// capacitor://localhost). Echoes back the specific origin with credentials support.
func ApiCORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && isPrivateOrigin(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token, X-K2-Client")
			c.Header("Access-Control-Max-Age", "86400")
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

// CORSMiddleware handles CORS for cross-origin requests from web dashboard
// Allows www.kaitu.io to access /app/* routes directly (bypassing Amplify proxy)
// Required for WebSocket connections which cannot be proxied through Next.js rewrites
func CORSMiddleware() gin.HandlerFunc {
	allowedOrigins := map[string]bool{
		"https://www.kaitu.io":  true,
		"https://kaitu.io":      true,
		"http://localhost:3000": true, // Development
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// Check if origin is allowed
		if allowedOrigins[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token, X-K2-Client, Cookie")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Max-Age", "86400") // 24 hours
		}

		// Handle preflight OPTIONS request
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
