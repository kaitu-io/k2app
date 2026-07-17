package center

import (
	"context"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
	"gorm.io/gorm"
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
		// Phase A：Stripe（官网 Checkout）+ Apple IAP（io.overleap 独立 bundle，
		// appstore.bundleIds.overleap 配置后 verify 生效）；google_play 随 App 上架再填
		PaymentChannels: []string{PayChannelStripe, PayChannelAppleIAP},
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

// ScopeBrand 是面向用户查询的唯一合法品牌过滤入口。
// admin 路由（/app/*）不用它——admin 是唯一合法跨品牌视角，用显式 ?brand= 筛选。
func ScopeBrand(b Brand) func(*gorm.DB) *gorm.DB {
	return func(tx *gorm.DB) *gorm.DB {
		return tx.Where("brand = ?", string(b))
	}
}

// parseBrandFilter 解析 admin ?brand= 筛选参数；空/非法返回 (BrandKaitu, false) 表示不过滤。
func parseBrandFilter(raw string) (Brand, bool) {
	b := Brand(strings.ToLower(raw))
	if b.Valid() {
		return b, true
	}
	return BrandKaitu, false
}

// BrandForCreate 解析 admin 创建路径上用户提交的 brand 字符串：
// 空 → BrandKaitu（老 admin UI 零破坏）；非空但非法 → error（拒绝，绝不静默降级成 kaitu）。
func BrandForCreate(s string) (Brand, error) {
	if s == "" {
		return BrandKaitu, nil
	}
	b := Brand(strings.ToLower(s))
	if !b.Valid() {
		return "", fmt.Errorf("invalid brand: %q", s)
	}
	return b, nil
}

// alertPaymentBrandMismatch 是支付品牌错配哨兵的统一告警出口：error 日志 + Slack "alert" 频道。
// 哨兵语义（fail-loud，Phase 1 既定）：命中即为 bug，拒绝入账并任由 provider 重试风暴
// 反复告警——持久出现应视为 page 级事件。Slack 发送 best-effort，绝不阻断主流程；
// var 形态供测试替换。wordgate/apple/stripe 三条渠道共用。
var alertPaymentBrandMismatch = func(ctx context.Context, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Errorf(ctx, "%s", msg)
	if err := slack.Send("alert", "[PAYMENT-BRAND-MISMATCH] "+msg); err != nil {
		log.Errorf(ctx, "failed to send brand-mismatch slack alert: %v", err)
	}
}
