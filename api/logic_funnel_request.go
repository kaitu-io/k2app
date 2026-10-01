package center

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
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

// ensureFunnelSid: 有合法 sid 返回之；optout / GPC / 未启用 → ""；否则新建并 Set-Cookie。
func ensureFunnelSid(c *gin.Context) string {
	if !funnelEnabled() {
		return ""
	}
	sid, optedOut := readFunnelSid(c)
	if optedOut {
		return ""
	}
	if sid != "" {
		return sid
	}
	if c.GetHeader("Sec-GPC") == "1" {
		return ""
	}
	sid = newFunnelSid()
	setFunnelSidCookie(c, sid)
	return sid
}

// funnelSilentUserID: 静默取登录用户，绝不 abort。无凭据时不调 ReqUser（避免刷日志）；
// 用户品牌与请求品牌不符按匿名处理。
func funnelSilentUserID(c *gin.Context) uint64 {
	tok, _ := c.Cookie(CookieAccessToken)
	if tok == "" && c.GetHeader("Authorization") == "" {
		return 0
	}
	u := ReqUser(c)
	if u == nil || u.Brand != string(ReqBrand(c)) {
		return 0
	}
	return u.ID
}

func funnelTruncate(s string, n int) string {
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
	path = funnelTruncate(stripFunnelLocale(parsed.Path), funnelPathMaxLen)
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

// funnelIPLimiter: 每 IP 每分钟固定窗口；结构照 telemetry.go 的 ruleMissIPLimiter。
type funnelIPLimiter struct {
	mu      sync.Mutex
	limit   int
	buckets map[string]*ruleMissBucket
}

func newFunnelIPLimiter(limit int) *funnelIPLimiter {
	return &funnelIPLimiter{limit: limit, buckets: make(map[string]*ruleMissBucket)}
}

func (l *funnelIPLimiter) Allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) > 1024 {
		for k, v := range l.buckets {
			if now.After(v.resetAt) {
				delete(l.buckets, k)
			}
		}
	}
	b, ok := l.buckets[ip]
	if !ok || now.After(b.resetAt) {
		l.buckets[ip] = &ruleMissBucket{resetAt: now.Add(time.Minute), count: 1}
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
	l.mu.Unlock()
}

var funnelPxLimiter = newFunnelIPLimiter(funnelPxPerMin)
