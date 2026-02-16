package center

import (
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// api_admin_list_orders 处理获取订单列表的请求（管理员）
//
func api_admin_list_orders(c *gin.Context) {
	log.Infof(c, "admin request to list orders")
	pagination := PaginationFromRequest(c)

	var req AdminOrderListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		log.Warnf(c, "invalid request to list orders: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 构建查询，预加载关联的User和Campaign
	query := db.Get().Model(&Order{}).Preload("User.LoginIdentifies").Preload("Campaign")

	// 如果提供了登录标识进行筛选
	if req.LoginProvider != "" && req.LoginIdentity != "" {
		log.Debugf(c, "filtering orders by login provider %s and identity %s", req.LoginProvider, req.LoginIdentity)
		// 先查找符合条件的用户
		var userIDs []uint64
		identifyQuery := db.Get().Model(&LoginIdentify{}).Select("user_id")

		identifyQuery = identifyQuery.Where("type = ?", req.LoginProvider)

		// 对于email类型，使用索引ID(加密前的邮箱)查询
		if req.LoginProvider == "email" {
			encryptedIdentity, err := secretEncryptString(c, req.LoginIdentity)
			if err != nil {
				log.Errorf(c, "failed to encrypt login identity: %v", err)
				Error(c, ErrorSystemError, "failed to encrypt login identity")
				return
			}
			identifyQuery = identifyQuery.Where("index_id = ?", encryptedIdentity)
		} else {
			// 对于其他类型，直接匹配加密后的值
			encryptedIdentity, err := secretEncryptString(c, req.LoginIdentity)
			if err != nil {
				log.Errorf(c, "failed to encrypt login identity: %v", err)
				Error(c, ErrorSystemError, "failed to encrypt login identity")
				return
			}
			identifyQuery = identifyQuery.Where("encrypted_value = ?", encryptedIdentity)
		}

		if err := identifyQuery.Find(&userIDs).Error; err != nil {
			log.Errorf(c, "failed to find users by login identity: %v", err)
			Error(c, ErrorSystemError, "failed to find users")
			return
		}

		if len(userIDs) > 0 {
			query = query.Where("user_id IN ?", userIDs)
		} else {
			// 没有找到用户，返回空结果
			log.Infof(c, "no users found for login provider %s and identity %s", req.LoginProvider, req.LoginIdentity)
			ListWithData(c, []AdminOrderListItem{}, pagination)
			return
		}
	}

	// 支付状态筛选
	if req.IsPaid != nil {
		log.Debugf(c, "filtering orders by paid status: %v", *req.IsPaid)
		query = query.Where("is_paid = ?", *req.IsPaid)
	}

	// 时间范围筛选
	if req.CreatedAtStart > 0 {
		log.Debugf(c, "filtering orders from %d", req.CreatedAtStart)
		query = query.Where("created_at >= ?", time.Unix(req.CreatedAtStart, 0))
	}
	if req.CreatedAtEnd > 0 {
		log.Debugf(c, "filtering orders to %d", req.CreatedAtEnd)
		query = query.Where("created_at <= ?", time.Unix(req.CreatedAtEnd, 0))
	}

	// 计算总数
	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count orders: %v", err)
		Error(c, ErrorSystemError, "failed to count orders")
		return
	}

	// 分页查询
	var orders []Order
	if err := query.
		Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&orders).Error; err != nil {
		log.Errorf(c, "failed to get orders: %v", err)
		Error(c, ErrorSystemError, "failed to get orders")
		return
	}

	// 收集所有订单ID用于批量查询返现信息
	orderIDs := make([]uint64, len(orders))
	for i, order := range orders {
		orderIDs[i] = order.ID
	}

	// 批量查询返现记录（WalletChange）
	var walletChanges []WalletChange
	walletChangeMap := make(map[uint64]*WalletChange) // orderID -> WalletChange
	if len(orderIDs) > 0 {
		db.Get().Where(&WalletChange{Type: WalletChangeTypeIncome}).
			Where("order_id IN ?", orderIDs).
			Preload("Wallet.User.LoginIdentifies").
			Find(&walletChanges)

		for i := range walletChanges {
			if walletChanges[i].OrderID != nil {
				walletChangeMap[*walletChanges[i].OrderID] = &walletChanges[i]
			}
		}
	}

	// 转换为响应格式
	items := make([]AdminOrderListItem, 0, len(orders))
	for _, order := range orders {
		item := AdminOrderListItem{
			UUID:                 order.UUID,
			Title:                order.Title,
			OriginAmount:         order.OriginAmount,
			CampaignReduceAmount: order.CampaignReduceAmount,
			PayAmount:            order.PayAmount,
			IsPaid:               order.IsPaid != nil && *order.IsPaid,
			CreatedAt:            order.CreatedAt.Unix(),
		}

		// 设置支付时间
		if order.PaidAt != nil {
			item.PaidAt = order.PaidAt.Unix()
		}

		// 设置用户资源
		if order.User != nil {
			item.User.UUID = order.User.UUID

			// 查找用户的email标识
			for _, identify := range order.User.LoginIdentifies {
				if identify.Type == "email" {
					// 解密email
					if decrypted, err := secretDecryptString(c, identify.EncryptedValue); err == nil {
						item.User.Email = decrypted
						break
					}
				}
			}
		}

		// 设置返现资源
		if walletChange, exists := walletChangeMap[order.ID]; exists && walletChange.Wallet != nil && walletChange.Wallet.User != nil {
			cashback := &ResourceCashback{
				RetailerUUID:  walletChange.Wallet.User.UUID,
				Amount:        walletChange.Amount,
			}

			// 查找分销商的email
			for _, identify := range walletChange.Wallet.User.LoginIdentifies {
				if identify.Type == "email" {
					if decrypted, err := secretDecryptString(c, identify.EncryptedValue); err == nil {
						cashback.RetailerEmail = decrypted
						break
					}
				}
			}

			// 判断返现状态（根据冻结期）
			if walletChange.FrozenUntil != nil {
				cashback.FrozenUntil = walletChange.FrozenUntil.Unix()
				if time.Now().Before(*walletChange.FrozenUntil) {
					cashback.Status = "pending" // 仍在冻结期
				} else {
					cashback.Status = "completed" // 冻结期已过
				}
			} else {
				cashback.Status = "completed" // 无冻结期，直接完成
			}

			item.Cashback = cashback
		}

		items = append(items, item)
	}

	log.Infof(c, "successfully listed %d orders", len(orders))
	ListWithData(c, items, pagination)
}

// api_admin_get_order_detail 处理获取订单详情的请求（管理员）
//
func api_admin_get_order_detail(c *gin.Context) {
	orderUUID := c.Param("uuid")
	log.Infof(c, "admin request to get order detail %s", orderUUID)

	if orderUUID == "" {
		log.Warnf(c, "missing order uuid")
		Error(c, ErrorInvalidArgument, "missing order uuid")
		return
	}

	var order Order
	err := db.Get().Where("uuid = ?", orderUUID).First(&order).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "order %s not found", orderUUID)
			Error(c, ErrorNotFound, "order not found")
		} else {
			log.Errorf(c, "failed to find order %s: %v", orderUUID, err)
			Error(c, ErrorSystemError, "failed to find order")
		}
		return
	}

	// 获取计划和活动信息
	plan, _ := order.GetPlan()
	campaign := order.Campaign

	dataOrder := DataOrder{
		UUID:                 order.UUID,
		Title:                order.Title,
		OriginAmount:         order.OriginAmount,
		CampaignReduceAmount: order.CampaignReduceAmount,
		PayAmount:            order.PayAmount,
		IsPaid:               order.IsPaid != nil && *order.IsPaid,
		CreatedAt:            order.CreatedAt.Unix(),
		Plan:                 plan,
		Campaign:             campaign,
	}

	if order.PaidAt != nil {
		dataOrder.PayAt = order.PaidAt.Unix()
	}

	log.Infof(c, "successfully retrieved order detail %s", orderUUID)
	Success(c, &dataOrder)
}
