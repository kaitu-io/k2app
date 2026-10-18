package center

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// pxGIF 是 43 字节透明 GIF。
var pxGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xff, 0xff, 0xff, 0x21, 0xf9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00,
	0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
}

func writePxGIF(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "image/gif", pxGIF)
}

// api_funnel_px: GET /api/px —— 官网行为事件像素。所有分支都返回 GIF，分析失败不影响页面。
func api_funnel_px(c *gin.Context) {
	defer writePxGIF(c)

	if !funnelEnabled() || !funnelPxAllow(c.ClientIP()) {
		return
	}
	// 原始输入先按字节封顶，再做任何解析/清洗（未认证端点的处理量有界）。
	q := func(key string) string { return funnelCapRaw(c.Query(key), funnelRawParamMax) }
	referer := funnelCapRaw(c.GetHeader("Referer"), funnelRawURLMax)
	device, osName, isBot := classifyUserAgent(funnelCapRaw(c.GetHeader("User-Agent"), funnelRawURLMax))
	event := q("e")
	if isBot || !funnelEventAllowed(event, FunnelSurfaceWeb) {
		return
	}
	brand := ReqBrand(c)
	cookieless := brand.Config().FunnelCookieless
	var anon string
	if cookieless {
		anon = funnelPageAnon(c, q("a"))
	} else {
		if _, optedOut := readFunnelSid(c); optedOut {
			return
		}
		anon = ensureFunnelSid(c)
	}
	// GPC = 完全不归因：anon 已为空（ensureFunnelSid），登录用户也不记，连凭据都不解析。
	var uid uint64
	if !funnelGPC(c) {
		uid = funnelSilentUserID(c)
	}
	if uid != 0 && anon != "" {
		linkFunnelIdentity(c, "sid", anon, uid, brand)
	}
	refHostForMatch := c.Request.Host
	if h, ok := funnelRefererSameSite(c, referer); ok {
		refHostForMatch = h
	}
	path, us, um, uc := parseFunnelLocation(funnelCapRaw(c.Query("u"), funnelRawURLMax), referer, refHostForMatch)
	funnelEnqueue(FunnelEvent{
		OccurredAt:  time.Now(),
		Brand:       string(brand),
		Surface:     FunnelSurfaceWeb,
		Event:       event,
		AnonID:      anon,
		UserID:      uid,
		Plan:        funnelTruncate(q("p"), 64),
		Source:      funnelTruncate(q("s"), 32),
		Path:        path,
		RefHost:     sanitizeRefHost(q("r")),
		UtmSource:   us,
		UtmMedium:   um,
		UtmCampaign: uc,
		Country:     funnelTruncate(CountryFromGinContext(c), 2),
		Device:      device,
		OS:          osName,
	})
	// 推断归因：只为有 sid 的点击记出口（GPC 访客 anon 为空，optout 早已 return）。
	if event == "install_click" && anon != "" && !cookieless {
		funnelAttrRecordClick(c, brand, q("p"), c.ClientIP(), anon)
	}
}

// funnelPageAnon：cookieless 品牌的访客标识——页面内存里的随机 id（格式同 sid）。GPC 时不用。
// 服务端不 Set-Cookie，也不读 sid cookie：同一浏览器整页刷新后就是另一个访客。
func funnelPageAnon(c *gin.Context, raw string) string {
	if funnelGPC(c) || !validFunnelSid(raw) {
		return ""
	}
	return raw
}

// api_funnel_px_optout: GET /api/px/optout —— 写 sid=optout，回到 Referer 同 host 的路径。
func api_funnel_px_optout(c *gin.Context) {
	setFunnelSidCookie(c, funnelSidOptOut)
	dest := "/"
	if _, ok := funnelRefererSameSite(c, c.GetHeader("Referer")); ok {
		r, _ := url.Parse(c.GetHeader("Referer"))
		p := r.EscapedPath()
		// 只接受单斜杠开头的站内路径，杜绝 //host 形式的开放重定向。
		if strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.Contains(p, `\`) {
			dest = p
		}
	}
	c.Redirect(http.StatusFound, dest)
}
