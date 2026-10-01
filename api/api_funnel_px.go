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
	device, osName, isBot := classifyUserAgent(c.GetHeader("User-Agent"))
	event := c.Query("e")
	if isBot || !funnelEventAllowed(event, FunnelSurfaceWeb) {
		return
	}
	if _, optedOut := readFunnelSid(c); optedOut {
		return
	}
	anon := ensureFunnelSid(c)
	brand := ReqBrand(c)
	uid := funnelSilentUserID(c)
	if uid != 0 && anon != "" {
		linkFunnelIdentity(c, "sid", anon, uid, brand)
	}
	refHostForMatch := c.Request.Host
	if h, ok := funnelRefererSameSite(c, c.GetHeader("Referer")); ok {
		refHostForMatch = h
	}
	path, us, um, uc := parseFunnelLocation(c.Query("u"), c.GetHeader("Referer"), refHostForMatch)
	funnelEnqueue(FunnelEvent{
		OccurredAt:  time.Now(),
		Brand:       string(brand),
		Surface:     FunnelSurfaceWeb,
		Event:       event,
		AnonID:      anon,
		UserID:      uid,
		Plan:        funnelTruncate(c.Query("p"), 64),
		Source:      funnelTruncate(c.Query("s"), 32),
		Path:        path,
		RefHost:     sanitizeRefHost(c.Query("r")),
		UtmSource:   us,
		UtmMedium:   um,
		UtmCampaign: uc,
		Country:     funnelTruncate(CountryFromGinContext(c), 2),
		Device:      device,
		OS:          osName,
	})
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
