package center

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// ==================== 分销商列表 ====================

// api_admin_list_retailers 获取分销商列表
func api_admin_list_retailers(c *gin.Context) {
	pagination := PaginationFromRequest(c)
	log.Infof(c, "Admin request for retailer list, page: %d, size: %d", pagination.Page, pagination.PageSize)

	// 查询分销商用户（is_retailer = true）
	dbQuery := db.Get().Model(&User{}).Where(&User{IsRetailer: BoolPtr(true)})

	// 根据 email 搜索
	if email := c.Query("email"); email != "" {
		log.Infof(c, "Filtering retailers by email: %s", email)
		indexID := secretHashIt(c, []byte(email))
		var identity LoginIdentify
		if err := db.Get().Where(&LoginIdentify{IndexID: indexID, Type: "email"}).First(&identity).Error; err == nil {
			dbQuery = dbQuery.Where(&User{ID: identity.UserID})
		} else {
			log.Warnf(c, "No retailer found for email: %s", email)
			List(c, []AdminRetailerListItem{}, pagination)
			return
		}
	}

	// 根据等级筛选
	if levelStr := c.Query("level"); levelStr != "" {
		level, err := strconv.Atoi(levelStr)
		if err == nil && level >= 1 && level <= 4 {
			log.Infof(c, "Filtering retailers by level: %d", level)
			// 需要通过子查询关联 RetailerConfig
			dbQuery = dbQuery.Where("id IN (?)",
				db.Get().Model(&RetailerConfig{}).Select("user_id").Where(&RetailerConfig{Level: level}))
		}
	}

	// 统计总数
	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		log.Errorf(c, "Failed to count retailers: %v", err)
		Error(c, ErrorSystemError, "count retailers failed")
		return
	}
	pagination.Total = total

	// 查询用户
	var users []User
	if err := dbQuery.Preload("LoginIdentifies").Order("id DESC").Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&users).Error; err != nil {
		log.Errorf(c, "Failed to list retailers: %v", err)
		Error(c, ErrorSystemError, "list retailers failed")
		return
	}

	if len(users) == 0 {
		List(c, []AdminRetailerListItem{}, pagination)
		return
	}

	userIDs := make([]uint64, len(users))
	for i, u := range users {
		userIDs[i] = u.ID
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

	// 批量查询最近沟通时间
	type LastCommunication struct {
		RetailerID     uint64
		CommunicatedAt time.Time
	}
	var lastComms []LastCommunication
	lastCommMap := make(map[uint64]time.Time)
	db.Get().Model(&RetailerNote{}).
		Select("retailer_id, MAX(communicated_at) as communicated_at").
		Where("retailer_id IN ?", userIDs).
		Group("retailer_id").
		Find(&lastComms)
	for _, lc := range lastComms {
		lastCommMap[lc.RetailerID] = lc.CommunicatedAt
	}

	// 批量查询待跟进数量
	type PendingCount struct {
		RetailerID uint64
		Count      int64
	}
	var pendingCounts []PendingCount
	pendingMap := make(map[uint64]int64)
	db.Get().Model(&RetailerNote{}).
		Select("retailer_id, COUNT(*) as count").
		Where("retailer_id IN ? AND follow_up_at IS NOT NULL AND follow_up_at <= ? AND (is_completed IS NULL OR is_completed = false)", userIDs, time.Now()).
		Group("retailer_id").
		Find(&pendingCounts)
	for _, pc := range pendingCounts {
		pendingMap[pc.RetailerID] = pc.Count
	}

	// 批量查询用户创建时间
	userCreatedAtMap := make(map[uint64]time.Time)
	for _, u := range users {
		userCreatedAtMap[u.ID] = u.CreatedAt
	}

	// 组装返回结果
	result := make([]AdminRetailerListItem, len(users))
	for i, u := range users {
		// 解密邮箱
		email := ""
		if len(u.LoginIdentifies) > 0 {
			decrypted, err := secretDecryptString(c, u.LoginIdentifies[0].EncryptedValue)
			if err == nil {
				email = decrypted
			}
		}

		item := AdminRetailerListItem{
			UUID:               u.UUID,
			Email:              email,
			HasPendingFollowUp: pendingMap[u.ID] > 0,
			PendingFollowUpCnt: int(pendingMap[u.ID]),
		}

		// 填充注册时间
		createdAt := u.CreatedAt.Unix()
		item.CreatedAt = &createdAt

		// 填充 RetailerConfig 数据
		if config, exists := retailerConfigMap[u.ID]; exists {
			item.Level = config.Level
			item.LevelName = config.GetLevelInfo().Name
			item.FirstOrderPercent = config.FirstOrderPercent
			item.RenewalPercent = config.RenewalPercent
			item.PaidUserCount = config.PaidUserCount
			item.Notes = config.Notes

			// 解析联系方式（需要解密）
			if config.Contacts != "" {
				decrypted, err := secretDecryptString(c, config.Contacts)
				if err == nil && decrypted != "" {
					var contacts []ContactInfo
					if jsonErr := json.Unmarshal([]byte(decrypted), &contacts); jsonErr == nil {
						item.Contacts = contacts
					}
				}
			}
		}

		// 填充 Wallet 数据
		if wallet, exists := walletMap[u.ID]; exists {
			frozenBalance, _ := CalculateFrozenBalance(c, wallet.ID)
			availableBalance, _ := CalculateAvailableBalance(c, wallet.ID)
			item.Wallet = &DataWallet{
				Balance:          wallet.Balance,
				AvailableBalance: availableBalance,
				FrozenBalance:    frozenBalance,
				TotalIncome:      wallet.TotalIncome,
				TotalWithdrawn:   wallet.TotalWithdrawn,
			}
			item.TotalIncome = int(wallet.TotalIncome)
			item.TotalWithdrawn = int(wallet.TotalWithdrawn)
		}

		// 填充最近沟通时间
		if lastComm, exists := lastCommMap[u.ID]; exists {
			ts := lastComm.Unix()
			item.LastCommunicatedAt = &ts
		}

		result[i] = item
	}

	List(c, result, pagination)
}

// ==================== 分销商详情 ====================

// api_admin_get_retailer_detail 获取分销商详情
func api_admin_get_retailer_detail(c *gin.Context) {
	uuid := c.Param("uuid")
	if uuid == "" {
		Error(c, ErrorInvalidArgument, "uuid is required")
		return
	}

	// 查询用户
	var user User
	if err := db.Get().Preload("LoginIdentifies").Where(&User{UUID: uuid}).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "retailer not found")
		} else {
			log.Errorf(c, "Failed to get retailer: %v", err)
			Error(c, ErrorSystemError, "get retailer failed")
		}
		return
	}

	// 检查是否是分销商
	if user.IsRetailer == nil || !*user.IsRetailer {
		Error(c, ErrorNotFound, "user is not a retailer")
		return
	}

	// 解密邮箱
	email := ""
	if len(user.LoginIdentifies) > 0 {
		decrypted, err := secretDecryptString(c, user.LoginIdentifies[0].EncryptedValue)
		if err == nil {
			email = decrypted
		}
	}

	// 查询 RetailerConfig
	var config RetailerConfig
	var dataRetailerConfig *DataRetailerConfig
	if err := db.Get().Where(&RetailerConfig{UserID: user.ID}).First(&config).Error; err == nil {
		dataRetailerConfig = ToDataRetailerConfig(&config)
	}

	// 查询 Wallet
	var dataWallet *DataWallet
	if wallet, err := GetWalletWithBalances(c, user.ID); err == nil {
		dataWallet = &DataWallet{
			Balance:          wallet.Balance,
			AvailableBalance: wallet.AvailableBalance,
			FrozenBalance:    wallet.FrozenBalance,
			TotalIncome:      wallet.TotalIncome,
			TotalWithdrawn:   wallet.TotalWithdrawn,
		}
	}

	// 统计待跟进数量
	var pendingCount int64
	db.Get().Model(&RetailerNote{}).
		Where("retailer_id = ? AND follow_up_at IS NOT NULL AND follow_up_at <= ? AND (is_completed IS NULL OR is_completed = false)", user.ID, time.Now()).
		Count(&pendingCount)

	result := AdminRetailerDetailData{
		UUID:             user.UUID,
		Email:            email,
		UserDetailLink:   "/manager/users/detail?uuid=" + user.UUID,
		RetailerConfig:   dataRetailerConfig,
		Wallet:           dataWallet,
		PendingFollowUps: int(pendingCount),
	}

	Success(c, &result)
}

// ==================== 沟通记录 CRUD ====================

// CreateRetailerNoteRequest 创建沟通记录请求
type CreateRetailerNoteRequest struct {
	Content        string  `json:"content" binding:"required"`
	CommunicatedAt int64   `json:"communicatedAt" binding:"required"`
	FollowUpAt     *int64  `json:"followUpAt,omitempty"`
	AssigneeID     *uint64 `json:"assigneeId,omitempty"` // 跟进人ID，默认为创建人
}

// api_admin_create_retailer_note 创建沟通记录
func api_admin_create_retailer_note(c *gin.Context) {
	uuid := c.Param("uuid")
	if uuid == "" {
		Error(c, ErrorInvalidArgument, "uuid is required")
		return
	}

	var req CreateRetailerNoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request: "+err.Error())
		return
	}

	// 查询分销商用户
	var user User
	if err := db.Get().Where(&User{UUID: uuid, IsRetailer: BoolPtr(true)}).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "retailer not found")
		} else {
			Error(c, ErrorSystemError, "get retailer failed")
		}
		return
	}

	// 获取当前管理员
	adminUser := ReqUser(c)
	if adminUser == nil {
		Error(c, ErrorNotLogin, "unauthorized")
		return
	}

	note := RetailerNote{
		RetailerID:     user.ID,
		Content:        req.Content,
		CommunicatedAt: time.Unix(req.CommunicatedAt, 0),
		OperatorID:     adminUser.ID,
		IsCompleted:    BoolPtr(false),
		SlackNotified:  BoolPtr(false),
	}

	if req.FollowUpAt != nil {
		followUpAt := time.Unix(*req.FollowUpAt, 0)
		note.FollowUpAt = &followUpAt
	}

	// Set assignee (defaults to creator if not specified)
	if req.AssigneeID != nil {
		note.AssigneeID = req.AssigneeID
	}

	if err := db.Get().Create(&note).Error; err != nil {
		log.Errorf(c, "Failed to create retailer note: %v", err)
		Error(c, ErrorSystemError, "create note failed")
		return
	}

	noteData := ToDataRetailerNote(&note)
	Success(c, &noteData)
}

// api_admin_list_retailer_notes 获取沟通记录列表
func api_admin_list_retailer_notes(c *gin.Context) {
	uuid := c.Param("uuid")
	if uuid == "" {
		Error(c, ErrorInvalidArgument, "uuid is required")
		return
	}

	pagination := PaginationFromRequest(c)

	// 查询分销商用户
	var user User
	if err := db.Get().Where(&User{UUID: uuid, IsRetailer: BoolPtr(true)}).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "retailer not found")
		} else {
			Error(c, ErrorSystemError, "get retailer failed")
		}
		return
	}

	// 统计总数
	var total int64
	db.Get().Model(&RetailerNote{}).Where(&RetailerNote{RetailerID: user.ID}).Count(&total)
	pagination.Total = total

	// 查询记录
	var notes []RetailerNote
	if err := db.Get().Where(&RetailerNote{RetailerID: user.ID}).
		Preload("Operator.LoginIdentifies").
		Preload("Assignee.LoginIdentifies").
		Order("communicated_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&notes).Error; err != nil {
		log.Errorf(c, "Failed to list retailer notes: %v", err)
		Error(c, ErrorSystemError, "list notes failed")
		return
	}

	// 转换为响应数据
	result := make([]DataRetailerNote, len(notes))
	for i, note := range notes {
		result[i] = ToDataRetailerNote(&note)
		// 填充操作人名称
		if note.Operator != nil && len(note.Operator.LoginIdentifies) > 0 {
			decrypted, err := secretDecryptString(c, note.Operator.LoginIdentifies[0].EncryptedValue)
			if err == nil {
				result[i].OperatorName = decrypted
			}
		}
		// 填充跟进人名称
		if note.Assignee != nil && len(note.Assignee.LoginIdentifies) > 0 {
			decrypted, err := secretDecryptString(c, note.Assignee.LoginIdentifies[0].EncryptedValue)
			if err == nil {
				result[i].AssigneeName = decrypted
			}
		}
	}

	List(c, result, pagination)
}

// UpdateRetailerNoteRequest 更新沟通记录请求
type UpdateRetailerNoteRequest struct {
	Content     *string `json:"content,omitempty"`
	FollowUpAt  *int64  `json:"followUpAt,omitempty"`
	IsCompleted *bool   `json:"isCompleted,omitempty"`
	AssigneeID  *uint64 `json:"assigneeId,omitempty"` // 跟进人ID
}

// api_admin_update_retailer_note 更新沟通记录
func api_admin_update_retailer_note(c *gin.Context) {
	uuid := c.Param("uuid")
	noteIDStr := c.Param("noteId")
	if uuid == "" || noteIDStr == "" {
		Error(c, ErrorInvalidArgument, "uuid and noteId are required")
		return
	}

	noteID, err := strconv.ParseUint(noteIDStr, 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid noteId")
		return
	}

	var req UpdateRetailerNoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request: "+err.Error())
		return
	}

	// 查询分销商用户
	var user User
	if err := db.Get().Where(&User{UUID: uuid, IsRetailer: BoolPtr(true)}).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "retailer not found")
		} else {
			Error(c, ErrorSystemError, "get retailer failed")
		}
		return
	}

	// 查询并更新记录
	var note RetailerNote
	if err := db.Get().Where(&RetailerNote{ID: noteID, RetailerID: user.ID}).First(&note).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "note not found")
		} else {
			Error(c, ErrorSystemError, "get note failed")
		}
		return
	}

	// 更新字段
	updates := make(map[string]any)
	if req.Content != nil {
		updates["content"] = *req.Content
	}
	if req.FollowUpAt != nil {
		if *req.FollowUpAt == 0 {
			updates["follow_up_at"] = nil
			updates["slack_notified"] = false // Reset notification status when removing follow-up
		} else {
			updates["follow_up_at"] = time.Unix(*req.FollowUpAt, 0)
			updates["slack_notified"] = false // Reset notification status when changing follow-up time
		}
	}
	if req.IsCompleted != nil {
		updates["is_completed"] = *req.IsCompleted
	}
	if req.AssigneeID != nil {
		if *req.AssigneeID == 0 {
			updates["assignee_id"] = nil // Clear assignee (defaults back to operator)
		} else {
			updates["assignee_id"] = *req.AssigneeID
		}
	}

	if len(updates) > 0 {
		if err := db.Get().Model(&note).Updates(updates).Error; err != nil {
			log.Errorf(c, "Failed to update retailer note: %v", err)
			Error(c, ErrorSystemError, "update note failed")
			return
		}
	}

	// 重新查询返回最新数据
	db.Get().First(&note, noteID)
	noteData := ToDataRetailerNote(&note)
	Success(c, &noteData)
}

// api_admin_delete_retailer_note 删除沟通记录
func api_admin_delete_retailer_note(c *gin.Context) {
	uuid := c.Param("uuid")
	noteIDStr := c.Param("noteId")
	if uuid == "" || noteIDStr == "" {
		Error(c, ErrorInvalidArgument, "uuid and noteId are required")
		return
	}

	noteID, err := strconv.ParseUint(noteIDStr, 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid noteId")
		return
	}

	// 查询分销商用户
	var user User
	if err := db.Get().Where(&User{UUID: uuid, IsRetailer: BoolPtr(true)}).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "retailer not found")
		} else {
			Error(c, ErrorSystemError, "get retailer failed")
		}
		return
	}

	// 删除记录（软删除）
	if err := db.Get().Where(&RetailerNote{ID: noteID, RetailerID: user.ID}).Delete(&RetailerNote{}).Error; err != nil {
		log.Errorf(c, "Failed to delete retailer note: %v", err)
		Error(c, ErrorSystemError, "delete note failed")
		return
	}

	SuccessEmpty(c)
}

// ==================== 分销待办列表 ====================

// api_admin_list_retailer_todos 获取分销待办列表
func api_admin_list_retailer_todos(c *gin.Context) {
	pagination := PaginationFromRequest(c)
	log.Infof(c, "Admin request for retailer todos, page: %d, size: %d", pagination.Page, pagination.PageSize)

	now := time.Now()

	// 统计总数
	var total int64
	db.Get().Model(&RetailerNote{}).
		Where("follow_up_at IS NOT NULL AND follow_up_at <= ? AND (is_completed IS NULL OR is_completed = false)", now).
		Count(&total)
	pagination.Total = total

	// 查询待办事项
	var notes []RetailerNote
	if err := db.Get().
		Preload("Retailer.LoginIdentifies").
		Preload("Operator.LoginIdentifies").
		Preload("Assignee.LoginIdentifies").
		Where("follow_up_at IS NOT NULL AND follow_up_at <= ? AND (is_completed IS NULL OR is_completed = false)", now).
		Order("follow_up_at ASC"). // 最早的跟进优先
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&notes).Error; err != nil {
		log.Errorf(c, "Failed to list retailer todos: %v", err)
		Error(c, ErrorSystemError, "list todos failed")
		return
	}

	if len(notes) == 0 {
		List(c, []RetailerTodoItem{}, pagination)
		return
	}

	// 批量查询分销商的 RetailerConfig
	retailerIDs := make([]uint64, len(notes))
	for i, n := range notes {
		retailerIDs[i] = n.RetailerID
	}

	var configs []RetailerConfig
	configMap := make(map[uint64]*RetailerConfig)
	db.Get().Where("user_id IN ?", retailerIDs).Find(&configs)
	for i := range configs {
		configMap[configs[i].UserID] = &configs[i]
	}

	// 组装返回结果
	result := make([]RetailerTodoItem, len(notes))
	for i, note := range notes {
		// 解密邮箱
		email := ""
		uuid := ""
		if note.Retailer != nil {
			uuid = note.Retailer.UUID
			if len(note.Retailer.LoginIdentifies) > 0 {
				decrypted, err := secretDecryptString(c, note.Retailer.LoginIdentifies[0].EncryptedValue)
				if err == nil {
					email = decrypted
				}
			}
		}

		item := RetailerTodoItem{
			NoteID:        note.ID,
			RetailerUUID:  uuid,
			RetailerEmail: email,
			NoteContent:   truncateString(note.Content, 100),
			FollowUpAt:    note.FollowUpAt.Unix(),
			DaysOverdue:   note.DaysOverdue(),
			AssigneeID:    note.AssigneeID,
			OperatorID:    note.OperatorID,
		}

		// 填充操作人名称
		if note.Operator != nil && len(note.Operator.LoginIdentifies) > 0 {
			decrypted, err := secretDecryptString(c, note.Operator.LoginIdentifies[0].EncryptedValue)
			if err == nil {
				item.OperatorName = decrypted
			}
		}

		// 填充跟进人名称
		if note.Assignee != nil && len(note.Assignee.LoginIdentifies) > 0 {
			decrypted, err := secretDecryptString(c, note.Assignee.LoginIdentifies[0].EncryptedValue)
			if err == nil {
				item.AssigneeName = decrypted
			}
		}

		// 填充等级信息
		if config, exists := configMap[note.RetailerID]; exists {
			item.Level = config.Level
			item.LevelName = config.GetLevelInfo().Name
		}

		result[i] = item
	}

	List(c, result, pagination)
}

// ==================== 分销商配置更新 ====================

// UpdateRetailerNotesRequest 更新分销商备注请求
type UpdateRetailerNotesRequest struct {
	Notes string `json:"notes"` // 备注
}

// api_admin_update_retailer_notes 更新分销商备注
func api_admin_update_retailer_notes(c *gin.Context) {
	uuid := c.Param("uuid")
	if uuid == "" {
		Error(c, ErrorInvalidArgument, "uuid is required")
		return
	}

	var req UpdateRetailerNotesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request: "+err.Error())
		return
	}

	// 查询分销商用户
	var user User
	if err := db.Get().Where(&User{UUID: uuid, IsRetailer: BoolPtr(true)}).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "retailer not found")
		} else {
			Error(c, ErrorSystemError, "get retailer failed")
		}
		return
	}

	// 查询或创建 RetailerConfig
	config, err := GetOrCreateRetailerConfig(c, user.ID)
	if err != nil {
		log.Errorf(c, "Failed to get or create retailer config: %v", err)
		Error(c, ErrorSystemError, "get config failed")
		return
	}

	// 更新备注
	if err := db.Get().Model(&config).Update("notes", req.Notes).Error; err != nil {
		log.Errorf(c, "Failed to update retailer notes: %v", err)
		Error(c, ErrorSystemError, "update notes failed")
		return
	}

	SuccessEmpty(c)
}

// ==================== 管理员列表（用于跟进人选择） ====================

// AdminUserSimple 管理员简要信息
type AdminUserSimple struct {
	ID    uint64 `json:"id"`
	Email string `json:"email"`
}

// api_admin_list_admin_users 获取管理员用户列表（用于跟进人选择）
func api_admin_list_admin_users(c *gin.Context) {
	// 查询所有管理员用户
	var users []User
	if err := db.Get().
		Preload("LoginIdentifies").
		Where(&User{IsAdmin: BoolPtr(true)}).
		Order("id ASC").
		Find(&users).Error; err != nil {
		log.Errorf(c, "Failed to list admin users: %v", err)
		Error(c, ErrorSystemError, "list admin users failed")
		return
	}

	result := make([]AdminUserSimple, len(users))
	for i, u := range users {
		email := ""
		if len(u.LoginIdentifies) > 0 {
			decrypted, err := secretDecryptString(c, u.LoginIdentifies[0].EncryptedValue)
			if err == nil {
				email = decrypted
			}
		}
		result[i] = AdminUserSimple{
			ID:    u.ID,
			Email: email,
		}
	}

	Success(c, &result)
}

// ==================== 辅助函数 ====================

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
