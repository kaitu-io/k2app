package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/nextpay"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// checkoutReuseWindow：Stripe Checkout Session 默认 24h 过期，留 1h 余量复用缓存链接。
const checkoutReuseWindow = 23 * time.Hour

// nextpayCheckout 是一次 NextPay 建单 + confirm 的结果。
type nextpayCheckout struct {
	NextpayOrderID string
	CheckoutURL    string // checkout.stripe.com 链接，直接交给用户浏览器
}

// kaituSiteLocale 把 users.language 映射到开途官网 locale（只有 zh-CN / zh-TW / zh-HK）。
func kaituSiteLocale(language string) string {
	switch language {
	case "zh-TW", "zh-HK":
		return language
	default:
		return "zh-CN"
	}
}

// payResultURL 是 Stripe 支付成功后的回跳落点（官网静态页，零关键路径 fetch）。
// 不能带 query——NextPay 会在其后拼 "?session_id={CHECKOUT_SESSION_ID}"。
func payResultURL(locale, orderUUID string) string {
	return fmt.Sprintf("%s/%s/pay-result/%s", BrandKaitu.Config().BaseURL, locale, orderUUID)
}

// payRedirectURL 是写进代付邮件的耐久链接：Center 收到后按需复用/重建 Stripe session 再 302。
func payRedirectURL(orderUUID string) string {
	// src=mail 让漏斗能把代付邮件的点击与官网购买页的点击分开数（见 api_order_pay_redirect）。
	return fmt.Sprintf("%s/api/orders/%s/pay?src=mail", BrandKaitu.Config().BaseURL, orderUUID)
}

// createNextpayCheckoutFn 在 NextPay 建一张一次性订单并 confirm，换回 Stripe Checkout URL。
// var 形态供测试替换（对标 NextPay 自身的 createStripeCheckoutSessionFn seam）。
var createNextpayCheckoutFn = createNextpayCheckout

func createNextpayCheckout(ctx context.Context, user *User, order *Order, plan *Plan) (*nextpayCheckout, error) {
	email, err := getUserEmail(ctx, user.ID)
	if err != nil {
		// kaitu 全员邮箱注册；NextPay 要求合法 email。到这里就是数据异常，fail-loud。
		return nil, fmt.Errorf("user %d has no usable email for nextpay: %w", user.ID, err)
	}
	locale := kaituSiteLocale(user.Language)
	metadata, _ := json.Marshal(map[string]string{"brand": string(BrandKaitu), "plan": plan.PID})

	res, err := nextpay.CreateOrder(ctx, &nextpay.OrderRequest{
		UserID:             user.UUID,
		Email:              email,
		ProductName:        order.Title,
		ProductDescription: plan.Label,
		Amount:             order.PayAmount,
		Currency:           "usd",
		ObjectID:           order.UUID,
		SuccessURL:         payResultURL(locale, order.UUID),
		CancelURL:          fmt.Sprintf("%s/%s/purchase", BrandKaitu.Config().BaseURL, locale),
		Metadata:           string(metadata),
	})
	if err != nil {
		return nil, fmt.Errorf("nextpay create order: %w", err)
	}
	conf, err := nextpay.ConfirmPayment(ctx, res.OrderID, &nextpay.ConfirmPaymentRequest{
		PaymentMethod: configNextpay(ctx).PaymentMethod,
	})
	if err != nil {
		return nil, fmt.Errorf("nextpay confirm order %s: %w", res.OrderID, err)
	}
	if conf.CheckoutURL == "" {
		return nil, fmt.Errorf("nextpay confirm order %s returned empty checkoutUrl", res.OrderID)
	}
	log.Infof(ctx, "nextpay checkout created: order=%s nextpay=%s", order.UUID, res.OrderID)
	return &nextpayCheckout{NextpayOrderID: res.OrderID, CheckoutURL: conf.CheckoutURL}, nil
}

// errOrderAlreadyPaid：重建 checkout 期间订单已被（并发 webhook）入账，新 session 未落库。
var errOrderAlreadyPaid = errors.New("order already paid")

// ensureOrderCheckout 保证 order 有一个可用的 Stripe Checkout URL 并返回它：
// 缓存未超过 checkoutReuseWindow 直接复用；否则新建（NextPay 订单 30 分钟后拒绝
// confirm，只能整单重建）并把 NextpayOrderID / Channel / Meta 落库。
//
// 调用方传入的 order 可能是事务外的无锁快照（302 端点），而 NextPay 往返要几秒——期间
// webhook 可能已把这张单入账。所以落库前在事务内 FOR UPDATE 重读 is_paid（此时才加锁，
// 不在 HTTP 往返里持锁），已付就返回 errOrderAlreadyPaid；写入也只挑自己拥有的三列，
// 绝不整行 Save——整行写回会把并发提交的 is_paid/paid_at 洗掉，让第二个 session 二次入账。
func ensureOrderCheckout(ctx context.Context, tx *gorm.DB, user *User, order *Order, plan *Plan) (string, error) {
	if url, at := order.GetCheckout(); url != "" && time.Since(time.Unix(at, 0)) < checkoutReuseWindow {
		return url, nil
	}
	co, err := createNextpayCheckoutFn(ctx, user, order, plan)
	if err != nil {
		return "", err
	}
	var fresh Order
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "is_paid").
		Where("id = ?", order.ID).Take(&fresh).Error; err != nil {
		return "", fmt.Errorf("relock order %s: %w", order.UUID, err)
	}
	if fresh.IsPaid != nil && *fresh.IsPaid {
		log.Warnf(ctx, "order %s paid during checkout rebuild; dropping nextpay %s", order.UUID, co.NextpayOrderID)
		return "", errOrderAlreadyPaid
	}
	order.NextpayOrderID = co.NextpayOrderID
	order.Channel = OrderChannelNextpay
	if err := order.SetOrderCheckout(payRedirectURL(order.UUID), co.CheckoutURL, time.Now().Unix()); err != nil {
		return "", fmt.Errorf("set order checkout meta: %w", err)
	}
	if err := tx.Model(order).Select("NextpayOrderID", "Channel", "Meta").Updates(order).Error; err != nil {
		return "", fmt.Errorf("persist order checkout: %w", err)
	}
	return co.CheckoutURL, nil
}
