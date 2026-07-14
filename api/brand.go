package center

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// Brand 是双品牌拆分的核心枚举。用户的 brand 是出生属性（注册时确定，终身不变）。
// 未知输入一律回退 BrandKaitu，保证老客户端零破坏。
// Spec: docs/superpowers/specs/2026-07-14-brand-split-design.md
type Brand string

const (
	BrandKaitu    Brand = "kaitu"
	BrandOverleap Brand = "overleap"
)

// 支付渠道标识（BrandConfig.PaymentChannels 取值）
const (
	PayChannelWordgate   = "wordgate"
	PayChannelAppleIAP   = "apple_iap"
	PayChannelStripe     = "stripe"
	PayChannelGooglePlay = "google_play"
)

type BrandConfig struct {
	ID          Brand
	DisplayName string   // 用户可见品牌名（邮件签名等）
	Hosts       []string // 请求 Host → brand 归属
	WebOrigins  []string // CORS 白名单（https origin 全串）
	// OTT 重定向白名单根域：host == RedirectRootDomain 或以 "."+RedirectRootDomain 结尾
	RedirectRootDomain string
	BaseURL            string // appLinks.baseURL / 邀请链接 等回退默认
	SupportEmail       string
	EDMFromName        string
	PaymentChannels    []string
}

func (bc *BrandConfig) AllowsPayment(channel string) bool {
	for _, ch := range bc.PaymentChannels {
		if ch == channel {
			return true
		}
	}
	return false
}

var brandRegistry = map[Brand]*BrandConfig{
	BrandKaitu: {
		ID:                 BrandKaitu,
		DisplayName:        "开途",
		Hosts:              []string{"kaitu.io", "www.kaitu.io"},
		WebOrigins:         []string{"https://www.kaitu.io", "https://kaitu.io"},
		RedirectRootDomain: "kaitu.io",
		BaseURL:            "https://www.kaitu.io",
		SupportEmail:       "support@kaitu.me",
		EDMFromName:        "Kaitu Team",
		PaymentChannels:    []string{PayChannelWordgate, PayChannelAppleIAP},
	},
	BrandOverleap: {
		ID:                 BrandOverleap,
		DisplayName:        "Overleap",
		Hosts:              []string{"overleap.io", "www.overleap.io"},
		WebOrigins:         []string{"https://www.overleap.io", "https://overleap.io"},
		RedirectRootDomain: "overleap.io",
		BaseURL:            "https://www.overleap.io",
		SupportEmail:       "support@overleap.io",
		EDMFromName:        "Overleap Team",
		// Phase 6 接入 Stripe / Apple IAP（新 bundle）/ Play Billing 时填充
		PaymentChannels: []string{},
	},
}

// hostBrandIndex 由 brandRegistry 构建，key 为小写 host（不含端口）
var hostBrandIndex = func() map[string]Brand {
	idx := make(map[string]Brand)
	for id, cfg := range brandRegistry {
		for _, h := range cfg.Hosts {
			idx[strings.ToLower(h)] = id
		}
	}
	return idx
}()

func (b Brand) Valid() bool {
	_, ok := brandRegistry[b]
	return ok
}

// Config 返回品牌配置；未知 brand 回退 kaitu。
func (b Brand) Config() *BrandConfig {
	if cfg, ok := brandRegistry[b]; ok {
		return cfg
	}
	return brandRegistry[BrandKaitu]
}

func AllBrands() []Brand {
	return []Brand{BrandKaitu, BrandOverleap}
}

// BrandFromHost 按请求 Host 解析品牌。host 可带端口、大小写不敏感。
// 未匹配返回 (BrandKaitu, false)。
func BrandFromHost(host string) (Brand, bool) {
	h := strings.ToLower(host)
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	if b, ok := hostBrandIndex[h]; ok {
		return b, true
	}
	return BrandKaitu, false
}

const brandContextKey = "brand"

// resolveRequestBrand: Host → X-K2-Brand header → 默认 kaitu。
// 老客户端（无 header、经任意入口域名）恒落 kaitu —— 零破坏硬要求。
func resolveRequestBrand(c *gin.Context) Brand {
	if b, ok := BrandFromHost(c.Request.Host); ok {
		return b
	}
	if h := c.GetHeader("X-K2-Brand"); h != "" {
		if b := Brand(strings.ToLower(h)); b.Valid() {
			return b
		}
	}
	return BrandKaitu
}

// BrandResolver 把请求品牌放进 gin context。挂在 /api、/app、webhook 组最前。
func BrandResolver() gin.HandlerFunc {
	return func(c *gin.Context) {
		b := resolveRequestBrand(c)
		c.Set(brandContextKey, b)
		// qtoolkit/log.MiddlewareRequestLog 不支持挂自定义字段（entry 在其内部构建，
		// 无 SetField/WithField 钩子暴露给下游中间件）。Step 5 fallback：非 kaitu 品牌打一行
		// debug 日志，kaitu 不打，避免全量噪音。日志字段待 qtoolkit 支持后再补。
		if b != BrandKaitu {
			log.Debugf(c, "request brand: %s", b)
		}
		c.Next()
	}
}

// ReqBrand 取请求品牌；未挂 BrandResolver 时现场解析兜底。
func ReqBrand(c *gin.Context) Brand {
	if v, ok := c.Get(brandContextKey); ok {
		if b, ok := v.(Brand); ok {
			return b
		}
	}
	return resolveRequestBrand(c)
}
