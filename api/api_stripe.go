package center

import (
	"github.com/gin-gonic/gin"
	stripe "github.com/stripe/stripe-go/v82"
	portalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	chksession "github.com/stripe/stripe-go/v82/checkout/session"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// SDK 调用的测试替换点（对标 fetchAppleTransaction 的包级 var 模式，不打真 Stripe）。
//
// key 逐调用传入，绝不写 stripe.Key 包级全局：那是每请求写共享变量，按 Go 内存模型即真
// 数据竞争（值恒定所以实践无害，但并发压测挂 -race 会报）。stripe-go 的资源 Client 结构
// 本就支持 per-call key —— 包级函数只是 getC() 读全局的便利壳。
var (
	stripeNewCheckoutSession = func(key string, params *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
		return chksession.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}.New(params)
	}
	stripeNewPortalSession = func(key string, params *stripe.BillingPortalSessionParams) (*stripe.BillingPortalSession, error) {
		return portalsession.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}.New(params)
	}
)

// CreateStripeCheckoutRequest 创建 Stripe Checkout Session 请求
type CreateStripeCheckoutRequest struct {
	Plan string `json:"plan" binding:"required" example:"ol_pro_month"` // 套餐 PID（overleap 品牌）
}

// api_stripe_checkout 创建 Stripe Checkout Session（mode=subscription），返回跳转 URL。
// overleap 官网购买入口；kaitu 用户被 AllowsPayment 门拒（405001）。
// 优惠：不走本地 Campaign 表（那是 wordgate 定价链路），开 AllowPromotionCodes 由
// Stripe Dashboard 的 Coupon/Promotion Code 承接。
func api_stripe_checkout(c *gin.Context) {
	user := ReqUser(c)

	// 支付渠道品牌门：stripe 是 overleap 专属渠道。
	if !Brand(user.Brand).Config().AllowsPayment(PayChannelStripe) {
		log.Warnf(c, "[Stripe] user %d (brand=%s) rejected: stripe channel unavailable for brand", user.ID, user.Brand)
		Error(c, ErrorPaymentChannelUnavailable, "payment channel not available for this brand")
		return
	}
	// 缺配置 = 渠道自动不可用（绝不 panic）。
	cfg := configStripe(c)
	if !cfg.Ready() {
		log.Errorf(c, "[Stripe] channel allowed for brand %s but stripe.* config missing", user.Brand)
		Error(c, ErrorPaymentChannelUnavailable, "stripe is not configured")
		return
	}

	var req CreateStripeCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 品牌隔离的 plan 解析（跨品牌 PID 必须解析为 nil——getPlanByPID 既有守卫）。
	plan := getPlanByPID(c, req.Plan, Brand(user.Brand))
	if plan == nil || plan.StripePriceID == "" {
		log.Warnf(c, "[Stripe] invalid/non-stripe plan %s for user %d (brand=%s)", req.Plan, user.ID, user.Brand)
		Error(c, ErrorInvalidArgument, "invalid plan")
		return
	}

	// Tier 校验：复购必须同档（与 wordgate 下单一致）。
	if err := validatePurchase(user, plan); err != nil {
		log.Warnf(c, "[Stripe] tier validation rejected user %d: %v", user.ID, err)
		Error(c, ErrorTierMismatch, "your account tier does not match this plan; contact support to change tiers")
		return
	}

	// 防重叠（防双扣）：已有任一 provider 的活跃续订订阅 → 拒绝二次订阅。
	if len(GetActiveSubscriptions(user.ID)) > 0 {
		Error(c, ErrorConflict, "you already have an active subscription")
		return
	}

	params := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(plan.StripePriceID), Quantity: stripe.Int64(1)},
		},
		SuccessURL:          stripe.String(cfg.SuccessURL),
		CancelURL:           stripe.String(cfg.CancelURL),
		ClientReferenceID:   stripe.String(user.UUID),
		AllowPromotionCodes: stripe.Bool(true),
		// metadata 烘焙进订阅对象 → 每张 invoice 的 parent.subscription_details.metadata
		// 原样携带 → invoice.paid 自足完成绑定+入账（webhook 不依赖事件先后序）。
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: map[string]string{
				"user_uuid": user.UUID,
				"plan_pid":  plan.PID,
				"brand":     user.Brand,
			},
		},
	}
	// 复用既有 Stripe Customer：到期后重新订阅若恒传 CustomerEmail，Stripe 会新建**第二个**
	// Customer —— 账单历史就此割裂，portal 只看得到最新那个（portal 按 provider_customer_id
	// 开会话）。有存量 customer id 就传 Customer；没有才预填邮箱让 Stripe 新建。
	// 二者互斥，Stripe 不接受同时传（400）。
	var existing Subscription
	if err := db.Get().
		Where("user_id = ? AND provider = ? AND provider_customer_id <> ''", user.ID, "stripe").
		Order("id DESC").First(&existing).Error; err == nil {
		params.Customer = stripe.String(existing.ProviderCustomerID)
	} else {
		// 预填邮箱（best-effort：取不到就让 Stripe 收集）。
		var full User
		if err := db.Get().Preload("LoginIdentifies").First(&full, user.ID).Error; err == nil {
			if email := getUserEmailFromIdentifies(&full); email != "" {
				params.CustomerEmail = stripe.String(email)
			}
		}
	}

	sess, err := stripeNewCheckoutSession(cfg.SecretKey, params)
	if err != nil {
		log.Errorf(c, "[Stripe] create checkout session failed for user %d plan %s: %v", user.ID, plan.PID, err)
		Error(c, ErrorSystemError, "failed to create checkout session")
		return
	}
	log.Infof(c, "[Stripe] checkout session created for user %d plan %s", user.ID, plan.PID)
	Success(c, &DataStripeRedirect{URL: sess.URL})
}

// api_stripe_portal 创建 Stripe Billing Portal 会话（订阅管理/取消面）。
// 客户端在 DataSubscription.Manage.Kind == "stripe_portal" 时调用本端点换 URL 再跳转。
func api_stripe_portal(c *gin.Context) {
	user := ReqUser(c)
	if !Brand(user.Brand).Config().AllowsPayment(PayChannelStripe) {
		Error(c, ErrorPaymentChannelUnavailable, "payment channel not available for this brand")
		return
	}
	cfg := configStripe(c)
	if !cfg.Ready() {
		Error(c, ErrorPaymentChannelUnavailable, "stripe is not configured")
		return
	}

	// 任意状态的 stripe 订阅行都可开 portal（用户可能要看已取消订阅的账单历史）。
	var sub Subscription
	if err := db.Get().
		Where("user_id = ? AND provider = ? AND provider_customer_id <> ''", user.ID, "stripe").
		Order("id DESC").First(&sub).Error; err != nil {
		Error(c, ErrorNotFound, "no stripe subscription found")
		return
	}

	ps, err := stripeNewPortalSession(cfg.SecretKey, &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(sub.ProviderCustomerID),
		ReturnURL: stripe.String(cfg.PortalReturnURL),
	})
	if err != nil {
		log.Errorf(c, "[Stripe] create portal session failed for user %d: %v", user.ID, err)
		Error(c, ErrorSystemError, "failed to create portal session")
		return
	}
	Success(c, &DataStripeRedirect{URL: ps.URL})
}
