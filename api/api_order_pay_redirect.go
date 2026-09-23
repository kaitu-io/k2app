package center

import (
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
func api_order_pay_redirect(c *gin.Context) {
	base := BrandKaitu.Config().BaseURL
	fallback := fmt.Sprintf("%s/%s/purchase", base, "zh-CN")

	orderUUID := c.Param("uuid")
	var order Order
	if err := db.Get().Preload("User").Where("uuid = ?", orderUUID).First(&order).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			log.Errorf(c, "pay redirect: load order %s: %v", orderUUID, err)
		}
		c.Redirect(http.StatusFound, fallback)
		return
	}
	if order.User == nil {
		log.Errorf(c, "pay redirect: order %s has no user", orderUUID)
		c.Redirect(http.StatusFound, fallback)
		return
	}
	locale := kaituSiteLocale(order.User.Language)
	purchase := fmt.Sprintf("%s/%s/purchase", base, locale)

	// 品牌门：只有允许 nextpay 的品牌（kaitu）才会为它建 checkout；其它品牌的订单 uuid 打到这里
	// 一律回 purchase，绝不替它去 NextPay 建单。
	if !Brand(order.User.Brand).Config().AllowsPayment(PayChannelNextpay) {
		log.Warnf(c, "pay redirect: order %s user brand %s does not allow nextpay", orderUUID, order.User.Brand)
		c.Redirect(http.StatusFound, purchase)
		return
	}

	if order.IsPaid != nil && *order.IsPaid {
		c.Redirect(http.StatusFound, payResultURL(locale, order.UUID))
		return
	}
	if !configNextpay(c).Ready() {
		log.Errorf(c, "pay redirect: nextpay not configured, order %s", orderUUID)
		c.Redirect(http.StatusFound, purchase)
		return
	}
	plan, err := order.GetPlan()
	if err != nil || plan == nil {
		log.Errorf(c, "pay redirect: order %s has no plan in meta: %v", orderUUID, err)
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
	if err != nil {
		log.Errorf(c, "pay redirect: ensure checkout for order %s: %v", orderUUID, err)
		c.Redirect(http.StatusFound, purchase)
		return
	}
	c.Redirect(http.StatusFound, checkoutURL)
}
