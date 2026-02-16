package center

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
)

// AdminUserDetailData 包含了后台用户详情页所需的所有信息
//
type AdminUserDetailData struct {
	DataUser
	Devices       []DataDevice       `json:"devices"`       // 用户设备列表
	Orders        []DataOrder        `json:"orders"`        // 用户订单列表
	ProHistories  []DataProHistory   `json:"proHistories"`  // 用户 Pro 历史记录
	InviteCodes   []DataMyInviteCode `json:"inviteCodes"`   // 用户邀请码列表
	WalletChanges []DataWalletChange `json:"walletChanges"` // 钱包变更记录
}

// api_admin_list_users 处理获取用户列表的请求（管理员）
//
func api_admin_list_users(c *gin.Context) {
	pagination := PaginationFromRequest(c)
	log.Infof(c, "Admin request for user list, page: %d, size: %d", pagination.Page, pagination.PageSize)

	dbQuery := db.Get().Model(&User{})

	// 根据 email 搜索（严格匹配）
	if email := c.Query("email"); email != "" {
		log.Infof(c, "Filtering users by email: %s", email)
		// 不能直接搜索密文，所以我们先找到 index_id
		indexID := secretHashIt(c, []byte(email))
		var identity LoginIdentify
		if err := db.Get().Where(&LoginIdentify{IndexID: indexID, Type: "email"}).First(&identity).Error; err == nil {
			dbQuery = dbQuery.Where(&User{ID: identity.UserID})
		} else {
			log.Warnf(c, "No user found for email: %s", email)
			List(c, []DataUser{}, pagination)
			return
		}
	}

	// 根据 expired_at 范围搜索
	if expiredStartStr, expiredEndStr := c.Query("expired_at_start"), c.Query("expired_at_end"); expiredStartStr != "" && expiredEndStr != "" {
		expiredStart, _ := strconv.ParseInt(expiredStartStr, 10, 64)
		expiredEnd, _ := strconv.ParseInt(expiredEndStr, 10, 64)
		if expiredStart > 0 && expiredEnd > 0 {
			log.Infof(c, "Filtering users by expiration range: %d to %d", expiredStart, expiredEnd)
			dbQuery = dbQuery.Where("expired_at BETWEEN ? AND ?", time.Unix(expiredStart, 0), time.Unix(expiredEnd, 0))
		}
	}

	// 根据首单情况搜索
	if hasOrdered := c.Query("has_ordered"); hasOrdered != "" {
		log.Infof(c, "Filtering users by order status: %s", hasOrdered)
		dbQuery = dbQuery.Where(&User{IsFirstOrderDone: BoolPtr(hasOrdered == "true")})
	}

	// 根据分销商状态搜索
	if isRetailer := c.Query("is_retailer"); isRetailer != "" {
		log.Infof(c, "Filtering users by retailer status: %s", isRetailer)
		dbQuery = dbQuery.Where(&User{IsRetailer: BoolPtr(isRetailer == "true")})
	}

	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		log.Errorf(c, "Failed to count users: %v", err)
		Error(c, ErrorSystemError, "count users failed")
		return
	}
	pagination.Total = total

	var users []User
	if err := dbQuery.Preload("Devices").Preload("LoginIdentifies").Order("id DESC").Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&users).Error; err != nil {
		log.Errorf(c, "Failed to list users: %v", err)
		Error(c, ErrorSystemError, "list users failed")
		return
	}

	userIDs := util.Map(users, func(u User) uint64 { return u.ID })
	if len(userIDs) == 0 {
		List(c, []DataUser{}, pagination)
		return
	}

	// 批量查询 RetailerConfig
	var retailerConfigs []RetailerConfig
	retailerConfigMap := make(map[uint64]*RetailerConfig)
	if err := db.Get().Where("user_id IN ?", userIDs).Find(&retailerConfigs).Error; err == nil {
		for i := range retailerConfigs {
			retailerConfigMap[retailerConfigs[i].UserID] = &retailerConfigs[i]
		}
	}

	// 批量查询 Wallet
	var wallets []Wallet
	walletMap := make(map[uint64]*Wallet)
	if err := db.Get().Where("user_id IN ?", userIDs).Find(&wallets).Error; err == nil {
		for i := range wallets {
			walletMap[wallets[i].UserID] = &wallets[i]
		}
	}

	// 组装返回结果
	result := make([]DataUser, len(users))
	for i, u := range users {
		loginIdentifies := make([]DataLoginIdentify, len(u.LoginIdentifies))
		for j, li := range u.LoginIdentifies {
			decryptedValue, err := secretDecryptString(c, li.EncryptedValue)
			if err != nil {
				log.Warnf(c, "failed to decrypt value for identity %d: %v", li.ID, err)
				decryptedValue = "[decryption error]"
			}
			loginIdentifies[j] = DataLoginIdentify{
				Type:  li.Type,
				Value: decryptedValue,
			}
		}

		// 组装 RetailerConfig 数据
		var dataRetailerConfig *DataRetailerConfig
		if config, exists := retailerConfigMap[u.ID]; exists {
			dataRetailerConfig = ToDataRetailerConfig(config)
		}

		// 组装 Wallet 数据并实时计算余额
		var dataWallet *DataWallet
		if wallet, exists := walletMap[u.ID]; exists {
			// 实时计算冻结余额和可用余额
			frozenBalance, _ := CalculateFrozenBalance(c, wallet.ID)
			availableBalance, _ := CalculateAvailableBalance(c, wallet.ID)

			dataWallet = &DataWallet{
				Balance:          wallet.Balance,
				AvailableBalance: availableBalance,
				FrozenBalance:    frozenBalance,
				TotalIncome:      wallet.TotalIncome,
				TotalWithdrawn:   wallet.TotalWithdrawn,
			}
		}

		result[i] = DataUser{
			UUID:             u.UUID,
			ExpiredAt:        u.ExpiredAt,
			IsFirstOrderDone: u.IsFirstOrderDone != nil && *u.IsFirstOrderDone,
			LoginIdentifies:  loginIdentifies,
			DeviceCount:      int64(len(u.Devices)),
			IsRetailer:       u.IsRetailer != nil && *u.IsRetailer,
			RetailerConfig:   dataRetailerConfig,
			Wallet:           dataWallet,
		}
	}

	log.Infof(c, "Successfully retrieved %d users for admin list", len(result))
	List(c, result, pagination)
}

// api_admin_get_user_detail 处理获取单个用户详细信息的请求（管理员）
//
func api_admin_get_user_detail(c *gin.Context) {
	uuid := c.Param("uuid")
	var user User
	err := db.Get().WithContext(c).
		Where(&User{UUID: uuid}).
		Preload("LoginIdentifies").
		Preload("Devices", func(db *gorm.DB) *gorm.DB {
			return db.Order("id DESC")
		}).
		Preload("Orders", func(db *gorm.DB) *gorm.DB {
			return db.Order("id DESC")
		}).
		Preload("InviteCodes", func(db *gorm.DB) *gorm.DB {
			return db.Order("id DESC")
		}).
		Preload("ProHistories", func(db *gorm.DB) *gorm.DB {
			return db.Order("id DESC")
		}).
		First(&user).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "Failed to get user details for %s: %v", uuid, err)
		Error(c, ErrorSystemError, "get user details failed")
		return
	}

	// --- 数据转换 ---

	// 转换 LoginIdentifies
	loginIdentifies := util.Map(user.LoginIdentifies, func(li LoginIdentify) DataLoginIdentify {
		val, _ := secretDecryptString(c, li.EncryptedValue)
		return DataLoginIdentify{Type: li.Type, Value: val}
	})

	// 转换 Devices
	devices := util.Map(user.Devices, func(d Device) DataDevice {
		return DataDevice{UDID: d.UDID, Remark: d.Remark, TokenLastUsedAt: d.TokenLastUsedAt}
	})

	// 转换 Orders
	orders := make([]DataOrder, len(user.Orders))
	for i, o := range user.Orders {
		plan, _ := o.GetPlan()
		campaign := o.Campaign
		var paidAt int64
		if o.PaidAt != nil {
			paidAt = o.PaidAt.Unix()
		}
		orders[i] = DataOrder{
			ID:                   o.UUID,
			UUID:                 o.UUID,
			Title:                o.Title,
			OriginAmount:         o.OriginAmount,
			CampaignReduceAmount: o.CampaignReduceAmount,
			PayAmount:            o.PayAmount,
			IsPaid:               o.IsPaid != nil && *o.IsPaid,
			CreatedAt:            o.CreatedAt.Unix(),
			Campaign:             campaign,
			Plan:                 plan,
			PayAt:                paidAt,
		}
	}

	proHistories := make([]DataProHistory, len(user.ProHistories))

	for i, h := range user.ProHistories {
		var dataOrder *DataOrder
		if h.Type == VipPurchase {
			var order Order
			// 这里可以优化，从已经 prelaod 的 orders 中查找，避免再次查询
			if db.Get().WithContext(c).Where(&Order{ID: h.ReferenceID}).First(&order).Error == nil {
				plan, _ := order.GetPlan()
				campaign := order.Campaign
				var paidAt int64
				if order.PaidAt != nil {
					paidAt = order.PaidAt.Unix()
				}
				dataOrder = &DataOrder{
					ID:        order.UUID,
					UUID:      order.UUID,
					Title:     order.Title,
					PayAmount: order.PayAmount,
					IsPaid:    order.IsPaid != nil && *order.IsPaid,
					CreatedAt: order.CreatedAt.Unix(),
					Plan:      plan,
					Campaign:  campaign,
					PayAt:     paidAt,
				}
			}
		}
		proHistories[i] = DataProHistory{
			Type:      h.Type,
			Days:      h.Days,
			Reason:    h.Reason,
			CreatedAt: h.CreatedAt.Unix(),
			Order:     dataOrder,
		}
	}

	// 获取并转换 InviteCodes
	inviteCodes := []DataMyInviteCode{}
	if len(user.InviteCodes) > 0 {
		// 获取所有邀请码ID
		codeIDsUint := util.Map(user.InviteCodes, func(code InviteCode) uint64 {
			return code.ID
		})

		// 定义统计结构体
		type UserCountStat struct {
			InvitedByCodeID uint64
			Count           int64
		}
		type RewardStat struct {
			ReferenceID uint64
			SumDays     int64
		}

		// 从 User 表查询注册人数统计
		var registerStats []UserCountStat
		_ = db.Get().WithContext(c).Model(&User{}).
			Select("invited_by_code_id, COUNT(*) as count").
			Where("invited_by_code_id IN ?", codeIDsUint).
			Group("invited_by_code_id").
			Scan(&registerStats).Error

		// 从 User 表查询购买人数统计
		var purchaseUserStats []UserCountStat
		_ = db.Get().WithContext(c).Model(&User{}).
			Select("invited_by_code_id, COUNT(*) as count").
			Where("invited_by_code_id IN ? AND is_first_order_done = ?", codeIDsUint, true).
			Group("invited_by_code_id").
			Scan(&purchaseUserStats).Error

		// 从 UserProHistory 查询购买奖励总天数统计
		var purchaseRewardStats []RewardStat
		_ = db.Get().WithContext(c).Model(&UserProHistory{}).
			Select("reference_id, COALESCE(SUM(days), 0) as sum_days").
			Where(&UserProHistory{Type: VipInviteReward}).
			Where("reference_id IN ?", codeIDsUint).
			Group("reference_id").
			Scan(&purchaseRewardStats).Error

		// 转换为 map
		registerCountMap := make(map[uint64]int64)
		for _, stat := range registerStats {
			registerCountMap[stat.InvitedByCodeID] = stat.Count
		}

		purchaseCountMap := make(map[uint64]int64)
		for _, stat := range purchaseUserStats {
			purchaseCountMap[stat.InvitedByCodeID] = stat.Count
		}

		purchaseRewardMap := make(map[uint64]int64)
		for _, stat := range purchaseRewardStats {
			purchaseRewardMap[stat.ReferenceID] = stat.SumDays
		}

		inviteCodes = make([]DataMyInviteCode, len(user.InviteCodes))
		for i, code := range user.InviteCodes {
			codeStr := code.GetCode()
			codeID := code.ID
			inviteCodes[i] = DataMyInviteCode{
				Code:           codeStr,
				CreatedAt:      code.CreatedAt.Unix(),
				Remark:         code.Remark,
				Link:           code.Link(),
				Config:         configInvite(c),
				RegisterCount:  registerCountMap[codeID],
				PurchaseCount:  purchaseCountMap[codeID],
				PurchaseReward: purchaseRewardMap[codeID],
			}
		}
	}

	// 查询 RetailerConfig
	var dataRetailerConfig *DataRetailerConfig
	var retailerConfig RetailerConfig
	if err := db.Get().Where(&RetailerConfig{UserID: user.ID}).First(&retailerConfig).Error; err == nil {
		dataRetailerConfig = ToDataRetailerConfig(&retailerConfig)
	}

	// 查询或创建 Wallet 并计算实时余额
	var dataWallet *DataWallet
	wallet, err := GetOrCreateWallet(c, user.ID)
	if err == nil {
		// 实时计算冻结余额和可用余额
		frozenBalance, _ := CalculateFrozenBalance(c, wallet.ID)
		availableBalance, _ := CalculateAvailableBalance(c, wallet.ID)

		dataWallet = &DataWallet{
			Balance:          wallet.Balance,
			AvailableBalance: availableBalance,
			FrozenBalance:    frozenBalance,
			TotalIncome:      wallet.TotalIncome,
			TotalWithdrawn:   wallet.TotalWithdrawn,
		}
	}

	// 查询钱包变更记录（最近20条）
	var walletChanges []DataWalletChange
	if wallet.ID > 0 {
		var changes []WalletChange
		db.Get().Where(&WalletChange{WalletID: wallet.ID}).
			Order("id DESC").
			Limit(20).
			Find(&changes)

		for _, change := range changes {
			dataChange := DataWalletChange{
				ID:           change.ID,
				Type:         string(change.Type),
				Amount:       change.Amount,
				BalanceAfter: change.BalanceAfter,
				Description:  change.Remark,
				CreatedAt:    change.CreatedAt.Unix(),
			}
			if change.FrozenUntil != nil {
				frozenUntil := change.FrozenUntil.Unix()
				dataChange.FrozenUntil = &frozenUntil
			}
			walletChanges = append(walletChanges, dataChange)
		}
	}

	resp := AdminUserDetailData{
		DataUser: DataUser{
			UUID:             user.UUID,
			ExpiredAt:        user.ExpiredAt,
			IsFirstOrderDone: user.IsFirstOrderDone != nil && *user.IsFirstOrderDone,
			LoginIdentifies:  loginIdentifies,
			DeviceCount:      int64(len(devices)),
			IsRetailer:       user.IsRetailer != nil && *user.IsRetailer,
			RetailerConfig:   dataRetailerConfig,
			Wallet:           dataWallet,
		},
		Devices:       devices,
		Orders:        orders,
		ProHistories:  proHistories,
		InviteCodes:   inviteCodes,
		WalletChanges: walletChanges,
	}

	log.Infof(c, "Successfully retrieved details for user %s", uuid)
	Success(c, &resp)
}

// UpdateUserRetailerStatusRequest 更新用户分销商状态请求
type UpdateUserRetailerStatusRequest struct {
	IsRetailer bool `json:"isRetailer"`
}

// api_admin_update_user_retailer_status 更新用户分销商状态
//
func api_admin_update_user_retailer_status(c *gin.Context) {
	uuid := c.Param("uuid")

	var req UpdateUserRetailerStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 查找用户
	var user User
	if err := db.Get().Where(&User{UUID: uuid}).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "查询用户失败: %v", err)
		Error(c, ErrorSystemError, "query user failed")
		return
	}

	// 更新分销商状态
	user.IsRetailer = BoolPtr(req.IsRetailer)
	if err := db.Get().Save(&user).Error; err != nil {
		log.Errorf(c, "更新分销商状态失败: %v", err)
		Error(c, ErrorSystemError, "update retailer status failed")
		return
	}

	log.Infof(c, "成功更新用户 %s 的分销商状态为: %v", uuid, req.IsRetailer)
	Success(c, &gin.H{})
}

// UpdateRetailerContactsRequest 更新分销商联系方式请求
type UpdateRetailerContactsRequest struct {
	Contacts []ContactInfo `json:"contacts"` // 联系方式列表
}

// api_admin_update_retailer_contacts 更新分销商联系方式
//
func api_admin_update_retailer_contacts(c *gin.Context) {
	uuid := c.Param("uuid")

	var req UpdateRetailerContactsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 查找用户
	var user User
	if err := db.Get().Where(&User{UUID: uuid}).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "查询用户失败: %v", err)
		Error(c, ErrorSystemError, "query user failed")
		return
	}

	// 检查用户是否为分销商
	if user.IsRetailer == nil || !*user.IsRetailer {
		Error(c, ErrorForbidden, "user is not a retailer")
		return
	}

	// 获取或创建分销商配置
	config, err := GetOrCreateRetailerConfig(c, user.ID)
	if err != nil {
		log.Errorf(c, "获取分销商配置失败: %v", err)
		Error(c, ErrorSystemError, "get retailer config failed")
		return
	}

	// 将联系方式转为 JSON
	contactsJSON, err := json.Marshal(req.Contacts)
	if err != nil {
		log.Errorf(c, "序列化联系方式失败: %v", err)
		Error(c, ErrorSystemError, "serialize contacts failed")
		return
	}

	// 加密联系方式
	encryptedContacts, err := secretEncryptString(c, string(contactsJSON))
	if err != nil {
		log.Errorf(c, "加密联系方式失败: %v", err)
		Error(c, ErrorSystemError, "encrypt contacts failed")
		return
	}

	// 更新分销商配置
	config.Contacts = encryptedContacts
	if err := db.Get().Save(config).Error; err != nil {
		log.Errorf(c, "更新分销商联系方式失败: %v", err)
		Error(c, ErrorSystemError, "update retailer contacts failed")
		return
	}

	log.Infof(c, "成功更新用户 %s 的分销商联系方式，共 %d 条", uuid, len(req.Contacts))
	Success(c, &gin.H{})
}

// AddUserMembershipRequest 添加用户会员时长请求
type AddUserMembershipRequest struct {
	Months int    `json:"months" binding:"required,min=1,max=120"` // 添加的月数（1-120个月）
	Reason string `json:"reason"`                                  // 变更原因（可选）
}

// AddUserMembershipResponse 添加用户会员时长响应
type AddUserMembershipResponse struct {
	ExpiredAt int64 `json:"expiredAt"` // 新的过期时间（Unix时间戳）
	Months    int   `json:"months"`    // 添加的月数
}

// HardDeleteUsersRequest 硬删除用户请求
type HardDeleteUsersRequest struct {
	UserUUIDs []string `json:"userUuids" binding:"required,min=1"` // 要删除的用户UUID列表
}

// api_admin_add_user_membership 为用户添加会员时长
//
func api_admin_add_user_membership(c *gin.Context) {
	uuid := c.Param("uuid")

	var req AddUserMembershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 查找用户
	var user User
	if err := db.Get().Where(&User{UUID: uuid}).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "查询用户失败: %v", err)
		Error(c, ErrorSystemError, "query user failed")
		return
	}

	// 计算新的过期时间
	now := time.Now()
	var newExpiredAt time.Time
	if user.ExpiredAt > 0 && user.ExpiredAt > now.Unix() {
		// 如果用户当前有有效期，从有效期末尾追加
		newExpiredAt = time.Unix(user.ExpiredAt, 0).AddDate(0, req.Months, 0)
	} else {
		// 如果用户没有有效期或已过期，从当前时间开始计算
		newExpiredAt = now.AddDate(0, req.Months, 0)
	}

	// 开始数据库事务
	tx := db.Get().Begin()
	if tx.Error != nil {
		log.Errorf(c, "开始事务失败: %v", tx.Error)
		Error(c, ErrorSystemError, "start transaction failed")
		return
	}

	// 更新用户过期时间
	user.ExpiredAt = newExpiredAt.Unix()
	if err := tx.Save(&user).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "更新用户过期时间失败: %v", err)
		Error(c, ErrorSystemError, "update user expired time failed")
		return
	}

	// 构建变更原因
	reason := req.Reason
	if reason == "" {
		reason = "管理员手动添加会员时长"
	}

	// 创建Pro历史记录（按30天/月估算用于显示）
	// 使用时间戳作为 ReferenceID 确保唯一性（系统发放没有关联订单）
	proHistory := UserProHistory{
		UserID:      user.ID,
		ReferenceID: uint64(time.Now().UnixNano()),
		Type:        VipSystemGrant,
		Days:        req.Months * 30,
		Reason:      reason,
	}

	if err := tx.Create(&proHistory).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "创建Pro历史记录失败: %v", err)
		Error(c, ErrorSystemError, "create pro history failed")
		return
	}

	// 提交事务
	if err := tx.Commit().Error; err != nil {
		log.Errorf(c, "提交事务失败: %v", err)
		Error(c, ErrorSystemError, "commit transaction failed")
		return
	}

	log.Infof(c, "成功为用户 %s 添加 %d 个月会员时长，新过期时间: %v", uuid, req.Months, newExpiredAt)
	Success(c, &AddUserMembershipResponse{
		ExpiredAt: newExpiredAt.Unix(),
		Months:    req.Months,
	})
}

// UpdateUserEmailRequest 管理员更新用户邮箱请求
type UpdateUserEmailRequest struct {
	Email string `json:"email" binding:"required,email"` // 新邮箱地址
}

// api_admin_update_user_email 管理员更新用户邮箱（无需验证码）
//
func api_admin_update_user_email(c *gin.Context) {
	uuid := c.Param("uuid")

	var req UpdateUserEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 查找用户
	var user User
	if err := db.Get().Where(&User{UUID: uuid}).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "查询用户失败: %v", err)
		Error(c, ErrorSystemError, "query user failed")
		return
	}

	// 计算新邮箱的索引ID
	newIndexID := secretHashIt(c, []byte(req.Email))

	// 检查邮箱是否已被其他用户使用
	var existingIdentify LoginIdentify
	err := db.Get().Where(&LoginIdentify{
		Type:    "email",
		IndexID: newIndexID,
	}).First(&existingIdentify).Error

	if err == nil && existingIdentify.UserID != user.ID {
		// 邮箱已被其他用户使用
		Error(c, ErrorConflict, "email already in use by another user")
		return
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Errorf(c, "检查邮箱唯一性失败: %v", err)
		Error(c, ErrorSystemError, "check email uniqueness failed")
		return
	}

	// 加密新邮箱
	encryptedEmail, err := secretEncryptString(c, req.Email)
	if err != nil {
		log.Errorf(c, "加密邮箱失败: %v", err)
		Error(c, ErrorSystemError, "encrypt email failed")
		return
	}

	// 更新用户的邮箱登录标识
	var identify LoginIdentify
	err = db.Get().Where("user_id = ? AND type = ?", user.ID, "email").First(&identify).Error
	if err == nil {
		// 更新现有记录
		identify.IndexID = newIndexID
		identify.EncryptedValue = encryptedEmail
		if err := db.Get().Save(&identify).Error; err != nil {
			log.Errorf(c, "更新邮箱失败: %v", err)
			Error(c, ErrorSystemError, "update email failed")
			return
		}
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		// 创建新记录（用户之前没有邮箱）
		identify = LoginIdentify{
			UserID:         user.ID,
			Type:           "email",
			IndexID:        newIndexID,
			EncryptedValue: encryptedEmail,
		}
		if err := db.Get().Create(&identify).Error; err != nil {
			log.Errorf(c, "创建邮箱登录标识失败: %v", err)
			Error(c, ErrorSystemError, "create email identity failed")
			return
		}
	} else {
		log.Errorf(c, "查询邮箱登录标识失败: %v", err)
		Error(c, ErrorSystemError, "query email identity failed")
		return
	}

	log.Infof(c, "管理员成功更新用户 %s 的邮箱为: %s", uuid, req.Email)
	Success(c, &gin.H{
		"email": req.Email,
	})
}

// api_admin_hard_delete_users 硬删除用户（批量）
//
func api_admin_hard_delete_users(c *gin.Context) {
	var req HardDeleteUsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	if len(req.UserUUIDs) == 0 {
		Error(c, ErrorInvalidArgument, "至少需要提供一个用户UUID")
		return
	}

	// 查找所有要删除的用户
	var users []User
	if err := db.Get().Where("uuid IN ?", req.UserUUIDs).Find(&users).Error; err != nil {
		log.Errorf(c, "查询用户失败: %v", err)
		Error(c, ErrorSystemError, "query users failed")
		return
	}

	if len(users) == 0 {
		Error(c, ErrorNotFound, "未找到任何要删除的用户")
		return
	}

	if len(users) != len(req.UserUUIDs) {
		log.Warnf(c, "部分用户不存在: 请求 %d 个，找到 %d 个", len(req.UserUUIDs), len(users))
	}

	// 收集所有用户ID
	userIDs := make([]uint64, len(users))
	for i, user := range users {
		userIDs[i] = user.ID
	}

	// 开始事务
	tx := db.Get().Begin()
	if tx.Error != nil {
		log.Errorf(c, "开始事务失败: %v", tx.Error)
		Error(c, ErrorSystemError, "start transaction failed")
		return
	}

	// 硬删除所有关联数据
	// 1. 删除登录标识
	if err := tx.Where("user_id IN ?", userIDs).Delete(&LoginIdentify{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "删除登录标识失败: %v", err)
		Error(c, ErrorSystemError, "delete login identifies failed")
		return
	}

	// 2. 删除设备
	if err := tx.Where("user_id IN ?", userIDs).Delete(&Device{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "删除设备失败: %v", err)
		Error(c, ErrorSystemError, "delete devices failed")
		return
	}

	// 3. 删除订单
	if err := tx.Where("user_id IN ?", userIDs).Delete(&Order{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "删除订单失败: %v", err)
		Error(c, ErrorSystemError, "delete orders failed")
		return
	}

	// 4. 删除邀请码
	if err := tx.Where("user_id IN ?", userIDs).Delete(&InviteCode{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "删除邀请码失败: %v", err)
		Error(c, ErrorSystemError, "delete invite codes failed")
		return
	}

	// 5. 删除Pro历史记录
	if err := tx.Where("user_id IN ?", userIDs).Delete(&UserProHistory{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "删除Pro历史记录失败: %v", err)
		Error(c, ErrorSystemError, "delete pro histories failed")
		return
	}

	// 6. 删除分销商配置
	if err := tx.Where("user_id IN ?", userIDs).Delete(&RetailerConfig{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "删除分销商配置失败: %v", err)
		Error(c, ErrorSystemError, "delete retailer configs failed")
		return
	}

	// 6.1 删除消息记录
	if err := tx.Where("user_id IN ?", userIDs).Delete(&Message{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "删除消息记录失败: %v", err)
		Error(c, ErrorSystemError, "delete messages failed")
		return
	}

	// 6.2 删除会话记录
	if err := tx.Where("user_id IN ?", userIDs).Delete(&SessionAcct{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "删除会话记录失败: %v", err)
		Error(c, ErrorSystemError, "delete session records failed")
		return
	}

	// 7. 查询所有钱包ID
	var wallets []Wallet
	if err := tx.Where("user_id IN ?", userIDs).Find(&wallets).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "查询钱包失败: %v", err)
		Error(c, ErrorSystemError, "query wallets failed")
		return
	}

	if len(wallets) > 0 {
		walletIDs := make([]uint64, len(wallets))
		for i, wallet := range wallets {
			walletIDs[i] = wallet.ID
		}

		// 8. 删除钱包变更记录
		if err := tx.Where("wallet_id IN ?", walletIDs).Delete(&WalletChange{}).Error; err != nil {
			tx.Rollback()
			log.Errorf(c, "删除钱包变更记录失败: %v", err)
			Error(c, ErrorSystemError, "delete wallet changes failed")
			return
		}

		// 9. 删除提现请求
		if err := tx.Where("wallet_id IN ?", walletIDs).Delete(&Withdraw{}).Error; err != nil {
			tx.Rollback()
			log.Errorf(c, "删除提现请求失败: %v", err)
			Error(c, ErrorSystemError, "delete withdraw requests failed")
			return
		}

		// 10. 删除钱包
		if err := tx.Where("id IN ?", walletIDs).Delete(&Wallet{}).Error; err != nil {
			tx.Rollback()
			log.Errorf(c, "删除钱包失败: %v", err)
			Error(c, ErrorSystemError, "delete wallets failed")
			return
		}
	}

	// 11. 删除提现账户（使用 user_id）
	if err := tx.Where("user_id IN ?", userIDs).Delete(&WithdrawAccount{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "删除提现账户失败: %v", err)
		Error(c, ErrorSystemError, "delete withdraw accounts failed")
		return
	}

	// 12. 删除邮件发送日志
	if err := tx.Where("user_id IN ?", userIDs).Delete(&EmailSendLog{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "删除邮件发送日志失败: %v", err)
		Error(c, ErrorSystemError, "delete email send logs failed")
		return
	}

	// 13. 最后删除用户本身
	if err := tx.Where("id IN ?", userIDs).Delete(&User{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "删除用户失败: %v", err)
		Error(c, ErrorSystemError, "delete users failed")
		return
	}

	// 提交事务
	if err := tx.Commit().Error; err != nil {
		log.Errorf(c, "提交事务失败: %v", err)
		Error(c, ErrorSystemError, "commit transaction failed")
		return
	}

	log.Infof(c, "成功硬删除 %d 个用户及其所有关联数据", len(users))
	Success(c, &gin.H{
		"deletedCount": len(users),
	})
}
