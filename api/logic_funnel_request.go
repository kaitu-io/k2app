package center

import (
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	db "github.com/wordgate/qtoolkit/db"
)

const (
	CookieFunnelSid  = "sid"
	funnelSidOptOut  = "optout"
	funnelSidMaxAge  = 400 * 24 * 3600
	funnelPxPerMin   = 120
	funnelPathMaxLen = 255
	funnelUtmMaxLen  = 64
)

var (
	funnelSidRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
	funnelLocaleRe = regexp.MustCompile(`^/[a-z]{2}(?:-[A-Za-z]{2})?`)
	funnelHostRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
)

// newFunnelSid: 16 随机字节 → 22 字符 base64url。
func newFunnelSid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func validFunnelSid(s string) bool { return funnelSidRe.MatchString(s) }

// readFunnelSid 读 sid cookie；非法值视为无。
func readFunnelSid(c *gin.Context) (sid string, optedOut bool) {
	v, err := c.Cookie(CookieFunnelSid)
	if err != nil {
		return "", false
	}
	if v == funnelSidOptOut {
		return "", true
	}
	if validFunnelSid(v) {
		return v, false
	}
	return "", false
}

func setFunnelSidCookie(c *gin.Context, value string) {
	isSecure := c.GetHeader("X-Forwarded-Proto") == "https" || c.Request.TLS != nil
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieFunnelSid, value, funnelSidMaxAge, "/", "", isSecure, true)
}

func funnelGPC(c *gin.Context) bool { return c.GetHeader("Sec-GPC") == "1" }

// ensureFunnelSid: 有合法 sid 返回之；optout / GPC / 未启用 → ""；否则新建并 Set-Cookie。
func ensureFunnelSid(c *gin.Context) string {
	if !funnelEnabled() {
		return ""
	}
	// GPC：即使已有合法 sid 也不使用（不删 cookie），事件按无 anon 记录。
	if funnelGPC(c) {
		return ""
	}
	sid, optedOut := readFunnelSid(c)
	if optedOut {
		return ""
	}
	if sid != "" {
		return sid
	}
	sid = newFunnelSid()
	setFunnelSidCookie(c, sid)
	return sid
}

// funnelSilentUserID: 只读解析登录用户，绝不写 cookie、绝不写 DB、绝不 abort，
// 无效凭据不打 Info/Warn。不走 ReqUser —— 那条路径会滑动续期 cookie、刷新设备信息、刷日志。
// 用户品牌与请求品牌不符按匿名处理。
func funnelSilentUserID(c *gin.Context) uint64 {
	token, _ := c.Cookie(CookieAccessToken)
	if token == "" {
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if token == "" {
		return 0
	}
	claims := &TokenClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (interface{}, error) {
		return []byte(configJwt(c).Secret), nil
	})
	if err != nil || !parsed.Valid || claims.Type != TokenTypeAccess || claims.UserID == 0 {
		return 0
	}
	var user User
	if claims.DeviceID == "" {
		if db.Get().First(&user, claims.UserID).Error != nil {
			return 0
		}
	} else {
		var device Device
		if db.Get().Preload("User").Where("udid = ? AND user_id = ?", claims.DeviceID, claims.UserID).First(&device).Error != nil ||
			device.TokenIssueAt != claims.TokenIssueAt || device.User == nil {
			return 0
		}
		user = *device.User
	}
	if user.Brand != string(ReqBrand(c)) {
		return 0
	}
	return user.ID
}

func stripHostPort(h string) string {
	h = strings.ToLower(h)
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

// funnelRefererSameSite: Referer 的 host 等于请求 Host（直连）或请求品牌的站点域名
// （官网经 Next.js rewrite 到 API，此时 Host 是 API 域名）。返回 Referer 的原始 Host。
func funnelRefererSameSite(c *gin.Context, referer string) (string, bool) {
	r, err := url.Parse(referer)
	if err != nil || r.Host == "" {
		return "", false
	}
	h := stripHostPort(r.Host)
	if h == stripHostPort(c.Request.Host) {
		return r.Host, true
	}
	for _, bh := range ReqBrand(c).Config().Hosts {
		if h == strings.ToLower(bh) {
			return r.Host, true
		}
	}
	return "", false
}

func funnelTruncate(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	for len(string(r)) > n {
		r = r[:len(r)-1]
	}
	return string(r)
}

func stripFunnelLocale(p string) string {
	if loc := funnelLocaleRe.FindString(p); loc != "" {
		rest := p[len(loc):]
		if rest == "" || rest[0] == '/' {
			p = rest
		}
	}
	if p == "" {
		p = "/"
	}
	return p
}

// parseFunnelLocation: 优先 u；u 为空用同 host 的 referer；都没有则 path 为空。
func parseFunnelLocation(u, referer, host string) (path, utmSource, utmMedium, utmCampaign string) {
	var parsed *url.URL
	if u != "" {
		parsed, _ = url.Parse(u)
	} else if referer != "" {
		if r, err := url.Parse(referer); err == nil && r.Host != "" && r.Host == host {
			parsed = r
		}
	}
	if parsed == nil {
		return "", "", "", ""
	}
	p := parsed.Path
	if !strings.HasPrefix(p, "/") {
		p = "/"
	}
	path = funnelTruncate(stripFunnelLocale(p), funnelPathMaxLen)
	q := parsed.Query()
	return path,
		funnelTruncate(q.Get("utm_source"), funnelUtmMaxLen),
		funnelTruncate(q.Get("utm_medium"), funnelUtmMaxLen),
		funnelTruncate(q.Get("utm_campaign"), funnelUtmMaxLen)
}

var funnelBotWords = []string{"bot", "crawler", "spider", "headless", "preview", "monitor"}

func classifyUserAgent(ua string) (device, os string, isBot bool) {
	l := strings.ToLower(ua)
	if l == "" {
		return "desktop", "other", true
	}
	for _, w := range funnelBotWords {
		if strings.Contains(l, w) {
			isBot = true
			break
		}
	}
	switch {
	case strings.Contains(l, "ipad"):
		device, os = "tablet", "ios"
	case strings.Contains(l, "iphone") || strings.Contains(l, "ipod"):
		device, os = "mobile", "ios"
	case strings.Contains(l, "android"):
		os = "android"
		if strings.Contains(l, "mobile") {
			device = "mobile"
		} else {
			device = "tablet"
		}
	case strings.Contains(l, "windows"):
		device, os = "desktop", "windows"
	case strings.Contains(l, "macintosh") || strings.Contains(l, "mac os x"):
		device, os = "desktop", "macos"
	case strings.Contains(l, "linux") || strings.Contains(l, "x11"):
		device, os = "desktop", "linux"
	default:
		device, os = "desktop", "other"
	}
	return
}

// sanitizeRefHost 只留小写 host；非法 → ""。
func sanitizeRefHost(r string) string {
	r = strings.TrimSpace(r)
	if r == "" {
		return ""
	}
	var h string
	if strings.Contains(r, "://") {
		u, err := url.Parse(r)
		if err != nil {
			return ""
		}
		h = u.Hostname()
	} else {
		h = r
		if i := strings.IndexAny(h, "/?#"); i >= 0 {
			h = h[:i]
		}
	}
	h = strings.ToLower(h)
	if !funnelHostRe.MatchString(h) {
		return ""
	}
	return funnelTruncate(h, 128)
}

// funnelIPLimiter: 固定窗口计数；结构照 telemetry.go 的 ruleMissIPLimiter，
// 但 map 有硬上限（满了之后新 key 共用一个溢出桶），过期桶每窗口最多清扫一次。
//
// 已知局限：IP 来自 c.ClientIP()，在可信代理未配置前取自客户端可控的头，可被伪造以绕过
// 单 IP 限额；因此另有与 IP 无关的全进程上限 funnelPxGlobal 兜底。
type funnelIPLimiter struct {
	mu        sync.Mutex
	limit     int
	buckets   map[string]*ruleMissBucket
	lastSweep time.Time
}

const (
	funnelLimiterMaxKeys = 20000
	funnelLimiterWindow  = time.Minute
	funnelOverflowKey    = "\x00overflow"
	funnelPxGlobalPerMin = 6000
)

func newFunnelIPLimiter(limit int) *funnelIPLimiter {
	return &funnelIPLimiter{limit: limit, buckets: make(map[string]*ruleMissBucket)}
}

func (l *funnelIPLimiter) Allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) >= funnelLimiterWindow {
		l.lastSweep = now
		for k, v := range l.buckets {
			if now.After(v.resetAt) {
				delete(l.buckets, k)
			}
		}
	}
	b, ok := l.buckets[ip]
	if !ok && len(l.buckets) >= funnelLimiterMaxKeys {
		ip = funnelOverflowKey
		b, ok = l.buckets[ip]
	}
	if !ok || now.After(b.resetAt) {
		l.buckets[ip] = &ruleMissBucket{resetAt: now.Add(funnelLimiterWindow), count: 1}
		return true
	}
	if b.count >= l.limit {
		return false
	}
	b.count++
	return true
}

// reset 清空所有桶（测试接缝）。
func (l *funnelIPLimiter) reset() {
	l.mu.Lock()
	l.buckets = make(map[string]*ruleMissBucket)
	l.lastSweep = time.Time{}
	l.mu.Unlock()
}

var (
	funnelPxLimiter = newFunnelIPLimiter(funnelPxPerMin)
	funnelPxGlobal  = newFunnelIPLimiter(funnelPxGlobalPerMin)
)

func funnelPxAllow(ip string) bool {
	return funnelPxLimiter.Allow(ip) && funnelPxGlobal.Allow("*")
}
