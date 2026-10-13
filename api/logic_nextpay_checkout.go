package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/nextpay"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// checkout 路径：开途下单后把用户送去哪里付款。
//   - checkoutModeStripeCheckout：NextPay 建单 + confirm(more)，拿 checkout.stripe.com 链接。
//   - checkoutModePaymentPage：NextPay 只建单，用它返回的自有支付页（pay.arbella.group/pay/<id>），
//     用户在页上选支付宝 / 卡；支付宝走服务端 PaymentIntent，失败或超时当场提示重付。
//
// 两条路径结算都由 NextPay 发同一个 order.paid，Center 入账逻辑不区分。
const (
	checkoutModeStripeCheckout = "stripe_checkout"
	checkoutModePaymentPage    = "payment_page"
)

// checkoutReuseWindow：Stripe Checkout Session 默认 24h 过期，留 1h 余量复用缓存链接。
const checkoutReuseWindow = 23 * time.Hour

// paymentPageReuseWindow：NextPay 订单从建单起 30 分钟后拒绝付款（懒过期），支付页链接随之作废；
// 留 5 分钟余量，超过就整单重建。
const paymentPageReuseWindow = 25 * time.Minute

// reuseWindowFor 返回某路径缓存链接的可复用时长。
func reuseWindowFor(mode string) time.Duration {
	if mode == checkoutModePaymentPage {
		return paymentPageReuseWindow
	}
	return checkoutReuseWindow
}

// nextpayPaymentPageBucket 决定某用户是否分到支付页路径。按用户 UUID 哈希固定分组：
// 同一用户重试 / 重新下单总走同一条路，两组可比；哈希值 < percent 才命中，所以放量
// （20→50）只会把新用户加进来，不会把已在支付页组的人切回 Stripe Checkout。
func nextpayPaymentPageBucket(userUUID string, percent int) bool {
	if percent <= 0 {
		return false
	}
	if percent >= 100 {
		return true
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte("nextpay-payment-page:" + userUUID))
	return int(h.Sum32()%100) < percent
}

// nextpayCheckout 是一次 NextPay 建单（+ 可能的 confirm）的结果。
type nextpayCheckout struct {
	NextpayOrderID string
	CheckoutURL    string // 交给用户浏览器的付款链接：checkout.stripe.com 或 NextPay 支付页
	Mode           string // checkoutModeStripeCheckout / checkoutModePaymentPage
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
	cfg := configNextpay(ctx)
	if nextpayPaymentPageBucket(user.UUID, cfg.PaymentPagePercent) {
		// 支付页路径：不 confirm——confirm 会把订单锁定到某个支付方式，支付页要让用户自己选。
		if res.PaymentURL == "" {
			return nil, fmt.Errorf("nextpay create order %s returned empty paymentUrl", res.OrderID)
		}
		log.Infof(ctx, "nextpay payment page created: order=%s nextpay=%s", order.UUID, res.OrderID)
		return &nextpayCheckout{NextpayOrderID: res.OrderID, CheckoutURL: res.PaymentURL, Mode: checkoutModePaymentPage}, nil
	}
	conf, err := nextpay.ConfirmPayment(ctx, res.OrderID, &nextpay.ConfirmPaymentRequest{
		PaymentMethod: cfg.PaymentMethod,
	})
	if err != nil {
		return nil, fmt.Errorf("nextpay confirm order %s: %w", res.OrderID, err)
	}
	if conf.CheckoutURL == "" {
		return nil, fmt.Errorf("nextpay confirm order %s returned empty checkoutUrl", res.OrderID)
	}
	log.Infof(ctx, "nextpay checkout created: order=%s nextpay=%s", order.UUID, res.OrderID)
	return &nextpayCheckout{NextpayOrderID: res.OrderID, CheckoutURL: conf.CheckoutURL, Mode: checkoutModeStripeCheckout}, nil
}

// errOrderAlreadyPaid：重建 checkout 期间订单已被（并发 webhook）入账，新 session 未落库。
var errOrderAlreadyPaid = errors.New("order already paid")

// ensureOrderCheckout 保证 order 有一个可用的付款链接并返回它：缓存未超过该路径的
// 复用窗口（reuseWindowFor：Stripe Checkout 23h / 支付页 25min）直接复用；否则新建
// （NextPay 订单 30 分钟后拒绝 confirm / 付款，只能整单重建）并把 NextpayOrderID / Channel / Meta 落库。
//
// 调用方传入的 order 可能是事务外的无锁快照（302 端点），而 NextPay 往返要几秒——期间
// webhook 可能已把这张单入账。所以落库前在事务内 FOR UPDATE 重读 is_paid（此时才加锁，
// 不在 HTTP 往返里持锁），已付就返回 errOrderAlreadyPaid；写入也只挑自己拥有的三列，
// 绝不整行 Save——整行写回会把并发提交的 is_paid/paid_at 洗掉，让第二个 session 二次入账。
func ensureOrderCheckout(ctx context.Context, tx *gorm.DB, user *User, order *Order, plan *Plan) (string, error) {
	if url, at := order.GetCheckout(); url != "" && time.Since(time.Unix(at, 0)) < reuseWindowFor(order.GetCheckoutMode()) {
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
	if err := order.SetCheckoutMode(co.Mode); err != nil {
		return "", fmt.Errorf("set order checkout mode: %w", err)
	}
	if err := tx.Model(order).Select("NextpayOrderID", "Channel", "Meta").Updates(order).Error; err != nil {
		return "", fmt.Errorf("persist order checkout: %w", err)
	}
	return co.CheckoutURL, nil
}
