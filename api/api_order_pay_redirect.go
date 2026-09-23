package center

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// api_order_pay_redirect 是代付邮件里的耐久支付链接：
// GET /api/orders/:uuid/pay → 302 到一个仍可支付的 Stripe Checkout URL。
//
// 为什么需要它：Stripe Checkout Session 24h 过期、NextPay 订单 30 分钟后拒绝 confirm，
// 直接把 Stripe URL 写进邮件，代付人隔天打开就是死链。app / 官网的即时下单不经过这里。
//
// 无认证：uuid（36 位随机）即能力凭证，与原 WordGate 公开 order_no 同等暴露面。
// 所有分支都 302（不返 JSON）——这是给浏览器点开的链接，不是 API。
//
// 它同时是**购买漏斗的唯一服务端观测点**。此前"下单"与"支付成功"之间是全黑的：
// 订单行和 webhook 都有记录，中间那段只能靠猜。官网不再直接跳 Stripe URL 而是跳这里，
// 于是"用户真的点了去支付"变成我们自己域上的一条日志——**这一点至关重要，因为
// GA/googletagmanager 在大陆被墙**，客户端埋点恰好丢掉我们唯一关心的那批用户。
//
// 每次调用记一行 `[PayFunnel] order=<uuid> src=<来源> outcome=<结局>`：
//
//	src:     web（官网购买页首次点击）/ retry（支付页没打开，用户点重开）/ mail（代付邮件）/ -（未标注）
//	outcome: checkout（已发往 Stripe）/ already_paid / fallback:<原因>
//
// 可读出来的数：`outcome=checkout` 计数 − order.paid webhook 计数 = 死在 Stripe 页上的人；
// `src=retry` 占比 = Stripe 页打不开的直接代理指标（2026-09-23 实测 checkout.stripe.com
// 从贵州电信只有 56% 可达，而 hooks.stripe.com 100%）。
func api_order_pay_redirect(c *gin.Context) {
	base := BrandKaitu.Config().BaseURL
	fallback := fmt.Sprintf("%s/%s/purchase", base, "zh-CN")

	orderUUID := c.Param("uuid")
	src := c.Query("src")
	if src == "" {
		src = "-"
	}
	// 每条路径都必须记一行，否则漏斗会少掉分母。
	funnel := func(outcome string) {
		log.Infof(c, "[PayFunnel] order=%s src=%s outcome=%s", orderUUID, src, outcome)
	}
	var order Order
	if err := db.Get().Preload("User").Where("uuid = ?", orderUUID).First(&order).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			log.Errorf(c, "pay redirect: load order %s: %v", orderUUID, err)
		}
		funnel("fallback:order_not_found")
		c.Redirect(http.StatusFound, fallback)
		return
	}
	if order.User == nil {
		log.Errorf(c, "pay redirect: order %s has no user", orderUUID)
		funnel("fallback:no_user")
		c.Redirect(http.StatusFound, fallback)
		return
	}
	locale := kaituSiteLocale(order.User.Language)
	purchase := fmt.Sprintf("%s/%s/purchase", base, locale)

	// 品牌门：只有允许 nextpay 的品牌（kaitu）才会为它建 checkout；其它品牌的订单 uuid 打到这里
	// 一律回 purchase，绝不替它去 NextPay 建单。
	if !Brand(order.User.Brand).Config().AllowsPayment(PayChannelNextpay) {
		log.Warnf(c, "pay redirect: order %s user brand %s does not allow nextpay", orderUUID, order.User.Brand)
		funnel("fallback:brand_denied")
		c.Redirect(http.StatusFound, purchase)
		return
	}

	if order.IsPaid != nil && *order.IsPaid {
		funnel("already_paid")
		c.Redirect(http.StatusFound, payResultURL(locale, order.UUID))
		return
	}
	if !configNextpay(c).Ready() {
		log.Errorf(c, "pay redirect: nextpay not configured, order %s", orderUUID)
		funnel("fallback:nextpay_unconfigured")
		c.Redirect(http.StatusFound, purchase)
		return
	}
	plan, err := order.GetPlan()
	if err != nil || plan == nil {
		log.Errorf(c, "pay redirect: order %s has no plan in meta: %v", orderUUID, err)
		funnel("fallback:no_plan")
		c.Redirect(http.StatusFound, purchase)
		return
	}

	var checkoutURL string
	err = db.Get().Transaction(func(tx *gorm.DB) error {
		url, err := ensureOrderCheckout(c, tx, order.User, &order, plan)
		if err != nil {
			return err
		}
		checkoutURL = url
		return nil
	})
	if errors.Is(err, errOrderAlreadyPaid) {
		// 快照读到未付、重建期间 webhook 已入账：当已付处理。
		funnel("already_paid")
		c.Redirect(http.StatusFound, payResultURL(locale, order.UUID))
		return
	}
	if err != nil {
		log.Errorf(c, "pay redirect: ensure checkout for order %s: %v", orderUUID, err)
		funnel("fallback:checkout_failed")
		c.Redirect(http.StatusFound, purchase)
		return
	}
	funnel("checkout")
	c.Redirect(http.StatusFound, checkoutURL)
}
