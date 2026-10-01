package center

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseFunnelLocation(t *testing.T) {
	cases := []struct {
		name, u, referer, host string
		path, src, med, camp   string
	}{
		{"locale+utm", "/en-GB/pricing?utm_source=x&utm_campaign=y&secret=1", "", "h", "/pricing", "x", "", "y"},
		{"locale only", "/zh-CN", "", "h", "/", "", "", ""},
		{"plain", "/install", "", "h", "/install", "", "", ""},
		{"referer same host", "", "https://overleap.io/ja/pricing", "overleap.io", "/pricing", "", "", ""},
		{"referer foreign", "", "https://evil.example/ja/pricing", "overleap.io", "", "", "", ""},
		{"not a locale", "/enterprise", "", "h", "/enterprise", "", "", ""},
		{"two letters not locale", "/en-x/a", "", "h", "/en-x/a", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, s, m, ca := parseFunnelLocation(c.u, c.referer, c.host)
			assert.Equal(t, c.path, p)
			assert.Equal(t, c.src, s)
			assert.Equal(t, c.med, m)
			assert.Equal(t, c.camp, ca)
		})
	}
	// 无前导斜杠 → "/"；完整 URL 只保留 path；非法 UTF-8 被清洗
	p0, _, _, _ := parseFunnelLocation("pricing", "", "h")
	assert.Equal(t, "/", p0)
	p1, _, _, _ := parseFunnelLocation("https://x.example/y", "", "h")
	assert.Equal(t, "/y", p1)
	assert.Equal(t, "ab", funnelTruncate("a\xffb", 10))
	long := "/" + strings.Repeat("a", 400) + "?utm_source=" + strings.Repeat("b", 100)
	p, s, _, _ := parseFunnelLocation(long, "", "h")
	assert.Len(t, p, 255)
	assert.Len(t, s, 64)
}

func TestClassifyUserAgent(t *testing.T) {
	cases := []struct {
		name, ua, device, os string
		bot                  bool
	}{
		{"iphone", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", "mobile", "ios", false},
		{"ipad", "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", "tablet", "ios", false},
		{"windows", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", "desktop", "windows", false},
		{"android", "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36", "mobile", "android", false},
		{"mac", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Safari/605.1.15", "desktop", "macos", false},
		{"linux", "Mozilla/5.0 (X11; Linux x86_64) Chrome/120.0 Safari/537.36", "desktop", "linux", false},
		{"googlebot", "Googlebot/2.1 (+http://www.google.com/bot.html)", "desktop", "other", true},
		{"empty", "", "desktop", "other", true},
		{"headless", "Mozilla/5.0 (X11; Linux x86_64) HeadlessChrome/120.0 Safari/537.36", "desktop", "linux", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, o, b := classifyUserAgent(c.ua)
			assert.Equal(t, c.bot, b)
			if !c.bot {
				assert.Equal(t, c.device, d)
				assert.Equal(t, c.os, o)
			}
		})
	}
}

func TestValidFunnelSid(t *testing.T) {
	assert.True(t, validFunnelSid(strings.Repeat("a", 22)))
	assert.True(t, validFunnelSid("AZaz09_-AZaz09_-AZaz09"[:22]))
	assert.False(t, validFunnelSid("optout"))
	assert.False(t, validFunnelSid(strings.Repeat("a", 21)))
	assert.False(t, validFunnelSid(strings.Repeat("a", 23)))
	assert.False(t, validFunnelSid(strings.Repeat("a", 21)+";"))
	assert.True(t, validFunnelSid(newFunnelSid()))
	assert.NotEqual(t, newFunnelSid(), newFunnelSid())
}

func TestSanitizeRefHost(t *testing.T) {
	assert.Equal(t, "news.ycombinator.com", sanitizeRefHost("https://News.Ycombinator.com/item?id=1"))
	assert.Equal(t, "google.com", sanitizeRefHost("google.com"))
	assert.Equal(t, "", sanitizeRefHost("javascript:alert(1)"))
	assert.Equal(t, "", sanitizeRefHost(""))
	assert.Equal(t, "", sanitizeRefHost("not a host"))
	assert.Len(t, sanitizeRefHost(strings.Repeat("a", 300)+".com"), 128)
}
