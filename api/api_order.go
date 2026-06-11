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
	"github.com/wordgate/wordgate-sdk"
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
	Region       string `json:"region" example:"ap-northeast-1"`             // 专属节点购买时选定的地区（仅 Kind=private_node 套餐有效）

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

	log.Infof(c, "user %d creating order, plan: %s, campaign: %s, preview: %v", user.ID, req.Plan, req.CampaignCode, req.Preview)

	// 获取套餐信息
	log.Infof(c, "getting plan information for plan ID: %s", req.Plan)
	plan := getPlanByPID(c, req.Plan)
	if plan == nil {
		log.Warnf(c, "invalid plan ID %s for user %d", req.Plan, user.ID)
		Error(c, ErrorInvalidArgument, "invalid plan")
		return
	}
	log.Debugf(c, "plan found successfully: PID=%s, Label=%s, Price=%d", plan.PID, plan.Label, plan.Price)

	// Tier validation: first-time buyers may pick any tier; repeat buyers must stay on their current tier.
	if err := validatePurchase(user, plan); err != nil {
		log.Warnf(c, "tier validation rejected user %d: %v", user.ID, err)
		Error(c, ErrorTierMismatch,
			fmt.Sprintf("您当前为「%s」档，无法购买「%s」档套餐。如需变更档位请联系客服。",
				user.Tier, plan.Tier))
		return
	}

	// 专属节点套餐：校验选定地区在允许列表内。空 region 允许（开通时由
	// createPrivateNodeSubscription 回退到 firstAllowedRegion）。校验对 preview 与
	// 真实创建都执行，让购买 UI 能尽早拿到反馈；preview 不落库订单。
	if plan.Kind == PlanKindPrivateNode && req.Region != "" {
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
	// 仅专属节点订单写入 region；共享套餐忽略 req.Region（持久化为空）。
	if plan.Kind == PlanKindPrivateNode {
		order.PrivateNodeRegion = req.Region
	}
	log.Debugf(c, "order object created: Title=%s, OriginAmount=%d, PayAmount=%d", order.Title, order.OriginAmount, order.PayAmount)

	var campaign *Campaign

	// 如果有优惠码，应用优惠
	if req.CampaignCode != "" {
		log.Infof(c, "processing campaign code: %s", req.CampaignCode)
		campaign = getCampaignByCode(c, req.CampaignCode)
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

	var payUrl string

	// 创建 wordgate 订单并保存关联
	log.Debugf(c, "starting wordgate order creation transaction")
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		log.Debugf(c, "creating wordgate client")
		client := createWordgateClient(c)

		// 先获取或创建 Wordgate 用户
		userResp, err := client.FindOrCreateUser(&wordgate.FindOrCreateUserRequest{
			Provider: "kaitu",
			Identity: user.UUID,
			Nickname: user.UUID, // 使用 UUID 作为昵称
		})
		if err != nil {
			log.Errorf(c, "failed to find or create wordgate user for user %d: %v", user.ID, err.Error())
			return err
		}
		log.Debugf(c, "wordgate user found/created: UserUID=%s, Created=%v", userResp.User.UID, userResp.Created)

		// 使用 CreateAppCustomOrder API 创建订单，支持明确设置订单价格
		// 这允许我们根据预览计算的价格（包括优惠码折扣）来创建订单
		log.Debugf(c, "calling wordgate CreateAppCustomOrder API with explicit price, plan PID: %s, quantity: %d, price: %d", plan.PID, quantity, order.PayAmount)
		cfg := configWordgate(c)

		// 计算单价（将总价除以数量）
		unitPrice := int64(order.PayAmount) / int64(quantity)

		orderResp, err := client.CreateAppCustomOrder(&wordgate.CreateAppCustomOrderRequest{
			UserUID:   userResp.User.UID,      // 使用 Wordgate 用户的 UID
			Subject:   order.Title,            // 使用订单标题作为主题
			Amount:    int64(order.PayAmount), // 使用预览计算出的明确价格（总价）
			NotifyURL: cfg.WebhookUrl,
			Items: []wordgate.CustomOrderItem{
				{
					ItemCode:       plan.PID,
					ItemName:       plan.Label, // 添加商品名称
					Quantity:       quantity,
					UnitPrice:      unitPrice, // 单价
					RequireAddress: false,     // 套餐订单不需要收货地址
				},
			},
		})
		if err != nil {
			log.Errorf(c, "failed to create wordgate order for order %s, user %d: %v", order.UUID, user.ID, err.Error())
			return err
		}
		log.Debugf(c, "wordgate order created successfully with explicit price: OrderNo=%s, PayURL=%s, Amount=%d", orderResp.OrderNo, orderResp.PayURL, order.PayAmount)

		// 保存 wordgate 订单号到本地订单
		log.Infof(c, "saving wordgate order number to local order: %s", orderResp.OrderNo)
		order.WordgateOrderNo = orderResp.OrderNo
		err = tx.Save(order).Error
		if err != nil {
			log.Errorf(c, "failed to save wordgate order no for order %s, user %d: %v", order.UUID, user.ID, err.Error())
			return err
		}
		log.Debugf(c, "wordgate order number saved successfully to local order")

		log.Infof(c, "successfully created wordgate order %s for order %s, user %d", orderResp.OrderNo, order.UUID, user.ID)
		payUrl = orderResp.PayURL

		// Persist payUrl into order.Meta so delegate-notify (and future retries) can read it
		if err := order.SetOrderPayUrl(payUrl); err != nil {
			log.Errorf(c, "failed to save payUrl into meta for order %s: %v", order.UUID, err)
			return err
		}
		if err := tx.Save(order).Error; err != nil {
			log.Errorf(c, "failed to persist order meta after payUrl update: %v", err)
			return err
		}
		return nil
	})

	if err != nil {
		log.Errorf(c, "failed to create wordgate order for order %s, user %d: %v", order.UUID, user.ID, err)
		Error(c, ErrorSystemError, "failed to create wordgate order")
		return
	}
	log.Infof(c, "wordgate order creation transaction completed successfully")

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
