package center

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
)

// validatePurchase checks whether the buyer can purchase this plan.
// First-time buyers (IsFirstOrderDone nil or false) can buy any tier.
// Repeat buyers must purchase the same tier they currently have — tier upgrades/
// downgrades require manual operator support.
func validatePurchase(buyer *User, plan *Plan) error {
	if buyer.IsFirstOrderDone == nil || !*buyer.IsFirstOrderDone {
		return nil
	}
	if plan.Tier != buyer.Tier {
		return fmt.Errorf("tier mismatch: user=%s, plan=%s", buyer.Tier, plan.Tier)
	}
	return nil
}

// CreateOrderRequest 创建订单请求数据结构
//
type CreateOrderRequest struct {
	Preview      bool   `json:"preview" example:"false"`                     // 是否预览模式
	Plan         string `json:"plan" binding:"required" example:"pro_month"` // 套餐ID
	CampaignCode string `json:"campaignCode" example:"SAVE20"`               // 优惠码（可选）
	Region       string `json:"region" example:"ap-northeast-1"`             // 专属节点购买时选定的地区（仅 Product=private_node 套餐有效）
	// Shipping 路由器版成品套餐（Plan.HardwareSKU != ""）真实下单必填；预览与其它产品忽略。
	Shipping *RouterShipping `json:"shipping,omitempty"`

	// Deprecated 2026-04-20: 代付功能已下线，下列字段仅用于检测旧客户端并拒绝其请求，不再写入 Order。
	// 详见 docs/superpowers/specs/2026-04-20-proxy-purchase-users.md
	ForUserUUIDs []string `json:"forUserUUIDs,omitempty"` // [Deprecated] 为其他用户支付（UUID列表）
	ForUsers     []string `json:"forUsers,omitempty"`     // [Deprecated, legacy pre-tier-rename name] 仅用于识别并拒绝老客户端请求
	ForMyself    *bool    `json:"forMyself,omitempty"`    // [Deprecated] 为用户自己
}

// CreateOrderResponse 创建订单响应数据结构
//
type CreateOrderResponse struct {
	PayUrl string     `json:"payUrl" example:"https://pay.example.com/order/123"` // 支付链接
	Order  *DataOrder `json:"order"`                                              // 订单信息
}

// api_create_order 创建订单
//
func api_create_order(c *gin.Context) {
	log.Debugf(c, "=== api_create_order started ===")

	var req CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid create order request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	log.Debugf(c, "create order request parsed successfully: preview=%v, plan=%s, campaignCode=%s", req.Preview, req.Plan, req.CampaignCode)

	// Reject deprecated proxy-purchase fields (forUsers / forUserUUIDs / forMyself=false)
	// 代付功能 2026-04-20 下线，参见 docs/superpowers/specs/2026-04-20-proxy-purchase-users.md
	// forUsers 是 pre-tier-rename 的老字段名，保留 alias 以便给老客户端回友好错误。
	if len(req.ForUserUUIDs) > 0 || len(req.ForUsers) > 0 || (req.ForMyself != nil && !*req.ForMyself) {
		log.Warnf(c, "rejecting deprecated proxy-purchase request: forUserUUIDs=%d, forUsers=%d, forMyselfExplicit=%v",
			len(req.ForUserUUIDs), len(req.ForUsers), req.ForMyself != nil && !*req.ForMyself)
		Error(c, ErrorProxyPurchaseDeprecated,
			"代付款功能已下线，不再支持为他人购买。请让对方使用自己的账号购买。")
		return
	}
	user := ReqUser(c)

	// 支付渠道品牌门：NextPay 是 kaitu 网页/app 下单渠道（2026-09-22 起）。overleap 用户走
	// Stripe 订阅端点，命中即拒单，绝不静默降级。（渠道是否配置好在真正建 checkout 前另查。）
	if !Brand(user.Brand).Config().AllowsPayment(PayChannelNextpay) {
		log.Warnf(c, "user %d (brand=%s) rejected: nextpay payment channel unavailable", user.ID, user.Brand)
		Error(c, ErrorPaymentChannelUnavailable, "payment channel not available for this brand")
		return
	}

	log.Infof(c, "user %d creating order, plan: %s, campaign: %s, preview: %v", user.ID, req.Plan, req.CampaignCode, req.Preview)

	// 获取套餐信息
	log.Infof(c, "getting plan information for plan ID: %s", req.Plan)
	plan := getPlanByPID(c, req.Plan, Brand(user.Brand))
	if plan == nil {
		log.Warnf(c, "invalid plan ID %s for user %d", req.Plan, user.ID)
		Error(c, ErrorInvalidArgument, "invalid plan")
		return
	}
	log.Debugf(c, "plan found successfully: PID=%s, Label=%s, Price=%d", plan.PID, plan.Label, plan.Price)

	// Tier validation: first-time buyers may pick any tier; repeat buyers must stay on their current tier.
	// 仅对 App 产品生效：专属节点/路由器版独立计费、与会员 tier 无关。
	if plan.Product == ProductApp {
		if err := validatePurchase(user, plan); err != nil {
			log.Warnf(c, "tier validation rejected user %d: %v", user.ID, err)
			Error(c, ErrorTierMismatch,
				fmt.Sprintf("您当前为「%s」档，无法购买「%s」档套餐。如需变更档位请联系客服。",
					user.Tier, plan.Tier))
			return
		}
	}

	// 防重叠(防双扣):已有任一 provider 的活跃续订订阅 → 拒绝一次性充值,
	// 含 preview——购买 UI 尽早拿到反馈,而非付款瞬间才失败。
	// 与 api_stripe_checkout 的既有守卫同判据(GetActiveSubscriptions/isSubscriptionLive)同错码。
	// 专属节点(ProductPrivateNode)/路由器版(ProductRouter)套餐豁免此门:与 Subscription/User.ExpiredAt
	// 零耦合(见 model_private_node.go),独立计费、不延长会员期限,双付论证对它不成立。
	if !isLineProduct(plan.Product) && len(GetActiveSubscriptions(user.ID)) > 0 {
		log.Warnf(c, "user %d rejected: active subscription exists, one-off purchase blocked", user.ID)
		Error(c, ErrorConflict, "you already have an active subscription")
		return
	}

	// 专属线路套餐（专属节点/路由器版）：校验选定地区在允许列表内。空 region 允许（开通时由
	// createPrivateNodeSubscription 回退到 firstAllowedRegion）。校验对 preview 与
	// 真实创建都执行，让购买 UI 能尽早拿到反馈；preview 不落库订单。
	if isLineProduct(plan.Product) && req.Region != "" {
		spec, err := loadPrivateNodePlanSpec(db.Get(), plan.ID)
		if err != nil {
			log.Errorf(c, "failed to load private node plan spec for plan %d: %v", plan.ID, err)
			Error(c, ErrorSystemError, "plan spec unavailable")
			return
		}
		var allowed []string
		_ = json.Unmarshal([]byte(spec.AllowedRegions), &allowed)
		if !slices.Contains(allowed, req.Region) {
			log.Warnf(c, "region %s not allowed for private node plan %s (user %d)", req.Region, plan.PID, user.ID)
			Error(c, ErrorInvalidArgument, "region not allowed for this plan")
			return
		}
	}

	// 一户一台（spec §8）：真实下单硬件套餐时，已有未过期台账或可续线路 → 拒绝，引导买续费套餐。
	// 预览放行，让网站照常报价。
	if plan.Product == ProductRouter && plan.HardwareSKU != "" && !req.Preview {
		has, err := userHasRouter(c, db.Get(), user.ID, time.Now().Unix())
		if err != nil {
			log.Errorf(c, "check existing router for user %d: %v", user.ID, err)
			Error(c, ErrorSystemError, "failed to check existing router")
			return
		}
		if has {
			log.Warnf(c, "user %d rejected: already has a router, hardware plan %s blocked", user.ID, plan.PID)
			Error(c, ErrorInvalidOperation, "该账户已有开途路由器，请购买续费套餐")
			return
		}
	}

	// 服务套餐仅续费：真实下单时名下没有路由器（见 userCanBuyRouterService）→ 拒绝。预览放行。
	if plan.Product == ProductRouter && plan.HardwareSKU == "" && !req.Preview {
		ok, err := userCanBuyRouterService(c, db.Get(), user.ID, time.Now().Unix())
		if err != nil {
			log.Errorf(c, "check router service eligibility for user %d: %v", user.ID, err)
			Error(c, ErrorSystemError, "failed to check existing router")
			return
		}
		if !ok {
			log.Warnf(c, "user %d rejected: no router, service plan %s is renewal-only", user.ID, plan.PID)
			Error(c, ErrorInvalidOperation, "服务套餐仅用于续费，请购买含路由器的开途路由器版")
			return
		}
	}

	// 路由器版成品：真实下单必须带收货信息（预览阶段允许缺省，让页面先报价）。
	if plan.Product == ProductRouter && plan.HardwareSKU != "" && !req.Preview {
		if req.Shipping == nil || req.Shipping.Name == "" || req.Shipping.Phone == "" || req.Shipping.Address == "" {
			log.Warnf(c, "router order for user %d missing shipping info", user.ID)
			Error(c, ErrorInvalidArgument, "shipping name/phone/address required")
			return
		}
	}

	// 代付下线后每个订单都是 buyer 自己购买，quantity 恒为 1。
	const quantity = 1

	// 创建订单
	log.Debugf(c, "creating order object for user %d", user.ID)
	totalAmount := plan.Price

	order := &Order{
		UUID:                 generateId("ord"),
		Title:                plan.Label,
		OriginAmount:         totalAmount,
		PayAmount:            totalAmount,
		CampaignReduceAmount: 0,
		UserID:               user.ID,
	}
	// 专属线路订单（专属节点/路由器版）写入 region；共享套餐忽略 req.Region（持久化为空）。
	if isLineProduct(plan.Product) {
		order.PrivateNodeRegion = req.Region
	}
	// 路由器版成品：收货信息落独立列（真实下单已在上面校验必填；此处兜底 preview/服务套餐不写）。
	// *string：nil 保持列为 SQL NULL（见 model.go Order.RouterShipping 注释），非 nil 才取地址赋值。
	if plan.Product == ProductRouter && plan.HardwareSKU != "" && req.Shipping != nil {
		if b, err := json.Marshal(req.Shipping); err == nil {
			s := string(b)
			order.RouterShipping = &s
		}
	}
	log.Debugf(c, "order object created: Title=%s, OriginAmount=%d, PayAmount=%d", order.Title, order.OriginAmount, order.PayAmount)

	var campaign *Campaign

	// 如果有优惠码，应用优惠
	if req.CampaignCode != "" {
		log.Infof(c, "processing campaign code: %s", req.CampaignCode)
		campaign = getCampaignByCode(c, req.CampaignCode, ReqBrand(c))
		if campaign != nil {
			log.Debugf(c, "campaign found: ID=%d, Type=%s", campaign.ID, campaign.Type)
			log.Infof(c, "applying campaign code %s for user %d", req.CampaignCode, user.ID)
			if matched := matchCampaign(c, campaign, user, order); matched {
				log.Infof(c, "campaign matched successfully for user %d", user.ID)
				payAmount, err := applyCampaign(c, campaign, order)
				if err == nil {
					log.Debugf(c, "campaign applied successfully: original amount=%d, new amount=%d", order.OriginAmount, payAmount)
					order.CampaignCode = &req.CampaignCode // 设置Campaign代码
					order.CampaignReduceAmount = order.OriginAmount - payAmount
					order.PayAmount = payAmount
					log.Debugf(c, "order amounts updated: CampaignCode=%s, CampaignReduceAmount=%d, PayAmount=%d",
						*order.CampaignCode, order.CampaignReduceAmount, order.PayAmount)

					// 设置订单 Meta 信息将在后面统一处理
					log.Debugf(c, "campaign information set successfully for order")
					log.Infof(c, "campaign %s applied for user %d, new amount: %d", req.CampaignCode, user.ID, order.PayAmount)
				} else {
					log.Errorf(c, "failed to apply campaign for user %d: %v", user.ID, err)
					Error(c, ErrorSystemError, "failed to apply campaign")
					return
				}
			} else {
				log.Warnf(c, "campaign code %s not matched for user %d", req.CampaignCode, user.ID)
				Error(c, ErrorInvalidCampaignCode, "invalid campaign code")
				return
			}
		} else {
			log.Warnf(c, "campaign code %s not found for user %d", req.CampaignCode, user.ID)
			Error(c, ErrorInvalidCampaignCode, "invalid campaign code")
			return
		}
	} else {
		log.Infof(c, "no campaign code provided, using original price")
	}

	// 设置订单 Meta 信息（包括 plan、campaign）
	// 代付下线后 forUserUUIDs 恒为空、forMyself 恒为 true，但 Meta 字段保留以保持旧数据兼容。
	log.Debugf(c, "setting order meta information")
	if err := order.SetOrderMeta(plan, campaign, nil, true); err != nil {
		log.Errorf(c, "failed to set order meta for order, user %d: %v", user.ID, err)
		Error(c, ErrorSystemError, err.Error())
		return
	}
	log.Debugf(c, "order meta information set successfully")

	log.Debugf(c, "creating DataOrder object")

	// 安全地获取支付时间戳
	var payAt int64
	if order.PaidAt != nil {
		payAt = order.PaidAt.Unix()
	}

	// 安全地获取创建时间戳
	var createdAt int64
	if !order.CreatedAt.IsZero() {
		createdAt = order.CreatedAt.Unix()
	} else {
		// 预览模式下使用当前时间
		createdAt = time.Now().Unix()
	}

	// 代付下线：forUsers 恒为空、forMyself 恒为 true。字段保留以保持响应结构兼容旧客户端。
	dataOrder := DataOrder{
		UUID:                 order.UUID,
		Title:                order.Title,
		OriginAmount:         order.OriginAmount,
		CampaignReduceAmount: order.CampaignReduceAmount,
		PayAmount:            order.PayAmount,
		IsPaid:               order.IsPaid != nil && *order.IsPaid,
		Plan:                 plan,
		Campaign:             campaign,
		CreatedAt:            createdAt,
		PayAt:                payAt,
		IsRefunded:           order.IsRefunded != nil && *order.IsRefunded,
		RefundedAt: func() int64 {
			if order.RefundedAt != nil {
				return order.RefundedAt.Unix()
			}
			return 0
		}(),
		RefundAmount: order.RefundAmount,
		RefundReason: order.RefundReason,
		ForUsers:     nil,
		ForMyself:    true,
	}
	log.Infof(c, "DataOrder object created: UUID=%s, PayAmount=%d", dataOrder.UUID, dataOrder.PayAmount)

	// 如果是预览模式，直接返回订单信息
	if req.Preview {
		log.Debugf(c, "preview mode enabled, returning order information without saving")
		Success(c, &CreateOrderResponse{
			PayUrl: "",
			Order:  &dataOrder,
		})
		log.Infof(c, "=== api_create_order completed (preview mode) ===")
		return
	}

	// 保存订单
	log.Debugf(c, "saving order to database for user %d", user.ID)
	if err := db.Get().Create(order).Error; err != nil {
		log.Errorf(c, "failed to save order for user %d: %v", user.ID, err)
		Error(c, ErrorSystemError, err.Error())
		return
	}
	log.Infof(c, "order saved successfully to database: OrderID=%d, UUID=%s", order.ID, order.UUID)

	// 渠道未配置（缺 access_key / webhook_secret）：订单已落库但不能建 checkout，405001。
	// 放在 preview 之后——预览只算价，不依赖支付渠道。
	if !configNextpay(c).Ready() {
		log.Errorf(c, "nextpay channel not configured; order %s created without checkout", order.UUID)
		Error(c, ErrorPaymentChannelUnavailable, "payment channel not available")
		return
	}

	// 建 NextPay checkout（建单 + confirm），拿到 Stripe Checkout URL；耐久链接与缓存写入 Meta。
	var payUrl string
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		url, err := ensureOrderCheckout(c, tx, user, order, plan)
		if err != nil {
			return err
		}
		payUrl = url
		return nil
	})
	if err != nil {
		log.Errorf(c, "failed to create nextpay checkout for order %s, user %d: %v", order.UUID, user.ID, err)
		Error(c, ErrorSystemError, "failed to create payment checkout")
		return
	}

	log.Debugf(c, "final order response prepared: PayURL=%s", payUrl)

	Success(c, &CreateOrderResponse{
		PayUrl: payUrl,
		Order:  &dataOrder,
	})
	log.Debugf(c, "=== api_create_order completed successfully ===")
}

// api_get_pro_histories 获取 Pro 历史记录
//
func api_get_pro_histories(c *gin.Context) {
	log.Debugf(c, "=== api_get_pro_histories started ===")

	userID := ReqUserID(c)
	p := PaginationFromRequest(c)
	typeFilter := c.Query("type") // 可选的类型过滤参数
	log.Infof(c, "user %d requesting pro histories, page: %d, pageSize: %d, type: %s", userID, p.Page, p.PageSize, typeFilter)

	log.Debugf(c, "counting total pro histories for user %d", userID)
	q := db.Get().Model(&UserProHistory{}).Where("user_id = ?", userID)

	// 如果指定了类型过滤，添加到查询条件
	if typeFilter != "" {
		q = q.Where("type = ?", typeFilter)
		log.Debugf(c, "applying type filter: %s", typeFilter)
	}

	if err := q.Count(&p.Total).Error; err != nil {
		log.Errorf(c, "failed to count pro histories for user %d: %v", userID, err)
		Error(c, ErrorSystemError, err.Error())
		return
	}
	log.Debugf(c, "total pro histories count for user %d: %d", userID, p.Total)

	q = q.Order("id DESC")

	// 查询用户 Pro 变更历史
	log.Debugf(c, "fetching pro histories with pagination for user %d", userID)
	var proHistories []UserProHistory
	if err := q.Offset(p.Offset()).Limit(p.PageSize).Find(&proHistories).Error; err != nil {
		log.Errorf(c, "failed to get pro histories for user %d: %v", userID, err)
		Error(c, ErrorSystemError, err.Error())
		return
	}
	log.Debugf(c, "retrieved %d pro histories for user %d", len(proHistories), userID)

	// 过滤出充值类型的记录，用于关联订单信息
	log.Debugf(c, "filtering pro histories to get recharge types for order lookup")
	chargeProHistories := util.Filter(proHistories, func(h UserProHistory) bool {
		return h.Type == VipPurchase
	})
	log.Debugf(c, "filtered to %d recharge pro histories for order lookup", len(chargeProHistories))

	orderIDs := util.Map(chargeProHistories, func(h UserProHistory) uint64 {
		return h.ReferenceID
	})
	log.Infof(c, "extracted %d order IDs from pro histories", len(orderIDs))

	var orders []Order
	orderMap := make(map[uint64]Order)
	if len(orderIDs) > 0 {
		log.Debugf(c, "fetching orders for order IDs: %v", orderIDs)
		if err := db.Get().Where("id IN (?)", orderIDs).Find(&orders).Error; err != nil {
			log.Errorf(c, "failed to get orders for pro histories for user %d: %v", userID, err)
			Error(c, ErrorSystemError, err.Error())
			return
		}
		log.Infof(c, "retrieved %d orders from database", len(orders))

		for _, o := range orders {
			orderMap[o.ID] = o
		}
		log.Debugf(c, "created order map with %d entries", len(orderMap))
	} else {
		log.Infof(c, "no order IDs to fetch")
	}

	// 组装 DataProHistory
	log.Debugf(c, "assembling DataProHistory objects")
	data := make([]DataProHistory, 0, len(proHistories))
	for i, h := range proHistories {
		log.Debugf(c, "processing pro history %d/%d: Type=%s, ReferenceID=%d", i+1, len(proHistories), h.Type, h.ReferenceID)

		var dataOrder *DataOrder = nil
		// 只为充值类型的记录关联订单信息
		if h.Type == VipPurchase {
			if o, ok := orderMap[h.ReferenceID]; ok {
				log.Debugf(c, "found order for reference ID %d: OrderID=%d, UUID=%s", h.ReferenceID, o.ID, o.UUID)

				plan, _ := o.GetPlan()
				campaign := o.Campaign

				if plan != nil {
					log.Debugf(c, "order plan: PID=%s, Label=%s", plan.PID, plan.Label)
				}
				if campaign != nil {
					log.Debugf(c, "order campaign: ID=%d, Type=%s", campaign.ID, campaign.Type)
				}

				// 安全地获取支付时间戳
				var payAt int64
				if o.PaidAt != nil {
					payAt = o.PaidAt.Unix()
				}

				dataOrder = &DataOrder{
					ID:                   strconv.FormatUint(o.ID, 10),
					UUID:                 o.UUID,
					Title:                o.Title,
					OriginAmount:         o.OriginAmount,
					CampaignReduceAmount: o.CampaignReduceAmount,
					PayAmount:            o.PayAmount,
					IsPaid:               o.IsPaid != nil && *o.IsPaid,
					CreatedAt:            o.CreatedAt.Unix(),
					Campaign:             campaign,
					Plan:                 plan,
					PayAt:                payAt,
					IsRefunded:           o.IsRefunded != nil && *o.IsRefunded,
					RefundedAt: func() int64 {
						if o.RefundedAt != nil {
							return o.RefundedAt.Unix()
						}
						return 0
					}(),
					RefundAmount: o.RefundAmount,
					RefundReason: o.RefundReason,
				}
				log.Infof(c, "created DataOrder: ID=%s, PayAmount=%d", dataOrder.ID, dataOrder.PayAmount)
			} else {
				log.Warnf(c, "order not found for reference ID %d", h.ReferenceID)
			}
		}

		data = append(data, DataProHistory{
			Type:      h.Type,
			Days:      h.Days,
			Reason:    h.Reason,
			CreatedAt: h.CreatedAt.Unix(),
			Order:     dataOrder,
		})
		log.Debugf(c, "added DataProHistory: Type=%s, Days=%d", h.Type, h.Days)
	}

	log.Infof(c, "successfully retrieved %d pro histories for user %d", len(data), userID)
	List(c, data, p)
	log.Debugf(c, "=== api_get_pro_histories completed successfully ===")
}
