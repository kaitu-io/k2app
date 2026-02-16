package center

import (
	"errors"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
)

// api_my_latest_invite_code 获取我的最新邀请码
//
func api_my_latest_invite_code(c *gin.Context) {
	// 获取当前用户
	userID := ReqUserID(c)
	log.Infof(c, "user %d requesting latest invite code", userID)
	if userID == 0 {
		// This case should be handled by AuthRequired middleware, but as a safeguard:
		log.Warnf(c, "attempt to get latest invite code with user ID 0")
		Error(c, ErrorNotLogin, "not login")
		return
	}

	// 查询最新的邀请码
	var inviteCode InviteCode
	err := db.Get().Where("user_id = ?", userID).
		Order("id DESC").
		First(&inviteCode).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Infof(c, "no invite code found for user %d, creating a new one", userID)
			// 没有邀请码则自动创建一个
			inviteCode = InviteCode{
				UserID: userID,
				Remark: "Invite Code",
			}
			err = db.Get().Create(&inviteCode).Error
			if err != nil {
				log.Errorf(c, "failed to create invite code for user %d: %v", userID, err)
				Error(c, ErrorSystemError, "create invite code failed")
				return
			}
			log.Infof(c, "created new invite code %s for user %d", inviteCode.GetCode(), userID)
		} else {
			log.Errorf(c, "failed to get invite code for user %d: %v", userID, err)
			Error(c, ErrorSystemError, "get invite code failed")
			return
		}
	}

	// 获取注册人数统计（从 User 表统计）
	var registerCount int64
	err = db.Get().Model(&User{}).
		Where(&User{InvitedByCodeID: inviteCode.ID}).
		Count(&registerCount).Error
	if err != nil {
		log.Errorf(c, "failed to count register users for code %s: %v", inviteCode.GetCode(), err)
		Error(c, ErrorSystemError, "get register count failed")
		return
	}

	// 获取购买人数统计（从 User 表统计已完成首单的用户）
	var purchaseCount int64
	err = db.Get().Model(&User{}).
		Where(&User{InvitedByCodeID: inviteCode.ID, IsFirstOrderDone: BoolPtr(true)}).
		Count(&purchaseCount).Error
	if err != nil {
		log.Errorf(c, "failed to count purchase users for code %s: %v", inviteCode.GetCode(), err)
		Error(c, ErrorSystemError, "get purchase count failed")
		return
	}

	// 获取购买奖励总天数（从 UserProHistory 统计）
	var purchaseRewardDays int64
	err = db.Get().Model(&UserProHistory{}).
		Select("COALESCE(SUM(days), 0)").
		Where(&UserProHistory{Type: VipInviteReward, ReferenceID: inviteCode.ID}).
		Scan(&purchaseRewardDays).Error
	if err != nil {
		log.Errorf(c, "failed to sum purchase reward days for code %s: %v", inviteCode.GetCode(), err)
		Error(c, ErrorSystemError, "get purchase reward sum failed")
		return
	}

	// 转换为 API 响应格式
	code := inviteCode.GetCode()
	data := DataMyInviteCode{
		Code:           code,
		CreatedAt:      inviteCode.CreatedAt.Unix(),
		Remark:         inviteCode.Remark,
		Link:           inviteCode.Link(),
		Config:         configInvite(c),
		RegisterCount:  registerCount,
		PurchaseCount:  purchaseCount,
		PurchaseReward: purchaseRewardDays,
	}

	log.Infof(c, "successfully retrieved latest invite code %s for user %d", data.Code, userID)
	Success[DataMyInviteCode](c, &data)
}

// api_my_inviteCodes 获取我的邀请码列表
//
func api_my_inviteCodes(c *gin.Context) {
	// 获取分页参数
	pagination := PaginationFromRequest(c)

	// 获取当前用户
	userID := ReqUserID(c)
	log.Infof(c, "user %d requesting invite codes list, page: %d, pageSize: %d", userID, pagination.Page, pagination.PageSize)
	if userID == 0 {
		log.Warnf(c, "attempt to get invite codes list with user ID 0")
		Error(c, ErrorNotLogin, "not login")
		return
	}

	// 查询邀请码列表
	var total int64
	var inviteCodes []InviteCode
	err := db.Get().Model(&InviteCode{}).Where("user_id = ?", userID).Count(&total).Error
	if err != nil {
		log.Errorf(c, "failed to count invite codes for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "get invite codes failed")
		return
	}

	err = db.Get().Where("user_id = ?", userID).
		Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&inviteCodes).Error
	if err != nil {
		log.Errorf(c, "failed to get invite codes list for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "get invite codes failed")
		return
	}

	// 获取所有邀请码ID（用于查询统计）
	codeIDsUint := util.Map(inviteCodes, func(code InviteCode) uint64 {
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

	// 从 User 表查询注册人数统计（按 InvitedByCodeID 分组）
	var registerStats []UserCountStat
	err = db.Get().Model(&User{}).
		Select("invited_by_code_id, COUNT(*) as count").
		Where("invited_by_code_id IN ?", codeIDsUint).
		Group("invited_by_code_id").
		Scan(&registerStats).Error
	if err != nil {
		log.Errorf(c, "failed to get register stats for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "get register stats failed")
		return
	}

	// 从 User 表查询购买人数统计（已完成首单的用户）
	var purchaseUserStats []UserCountStat
	err = db.Get().Model(&User{}).
		Select("invited_by_code_id, COUNT(*) as count").
		Where("invited_by_code_id IN ? AND is_first_order_done = ?", codeIDsUint, true).
		Group("invited_by_code_id").
		Scan(&purchaseUserStats).Error
	if err != nil {
		log.Errorf(c, "failed to get purchase user stats for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "get purchase user stats failed")
		return
	}

	// 从 UserProHistory 查询购买奖励总天数统计
	var purchaseRewardStats []RewardStat
	err = db.Get().Model(&UserProHistory{}).
		Select("reference_id, COALESCE(SUM(days), 0) as sum_days").
		Where(&UserProHistory{Type: VipInviteReward}).
		Where("reference_id IN ?", codeIDsUint).
		Group("reference_id").
		Scan(&purchaseRewardStats).Error
	if err != nil {
		log.Errorf(c, "failed to get purchase reward stats for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "get purchase reward stats failed")
		return
	}

	// 转换为 map 方便查找
	registerCountMap := make(map[uint64]int64)
	for _, stat := range registerStats {
		registerCountMap[stat.InvitedByCodeID] = stat.Count
	}
	log.Infof(c, "register stats for user %d: %d codes have registrations", userID, len(registerCountMap))

	purchaseCountMap := make(map[uint64]int64)
	for _, stat := range purchaseUserStats {
		purchaseCountMap[stat.InvitedByCodeID] = stat.Count
	}
	log.Infof(c, "purchase stats for user %d: %d codes have purchases", userID, len(purchaseCountMap))

	purchaseRewardMap := make(map[uint64]int64)
	for _, stat := range purchaseRewardStats {
		purchaseRewardMap[stat.ReferenceID] = stat.SumDays
	}
	log.Infof(c, "purchase reward stats for user %d: %d codes have rewards", userID, len(purchaseRewardMap))

	// 转换为 API 响应格式
	items := make([]DataMyInviteCode, len(inviteCodes))
	for i, code := range inviteCodes {
		codeStr := code.GetCode()
		codeID := code.ID

		registerCount := registerCountMap[codeID]
		purchaseCount := purchaseCountMap[codeID]
		purchaseReward := purchaseRewardMap[codeID]

		items[i] = DataMyInviteCode{
			Code:           codeStr,
			CreatedAt:      code.CreatedAt.Unix(),
			Remark:         code.Remark,
			Link:           code.Link(),
			Config:         configInvite(c),
			RegisterCount:  registerCount,
			PurchaseCount:  purchaseCount,
			PurchaseReward: purchaseReward,
		}

		log.Infof(c, "invite code %s (id=%d): register=%d, purchase=%d, reward=%d days",
			codeStr, codeID, registerCount, purchaseCount, purchaseReward)
	}

	// 更新总数
	pagination.Total = total

	// 使用统一的分页响应格式
	List(c, items, pagination)
}

// api_create_my_invite_code 创建我的邀请码
//
func api_create_my_invite_code(c *gin.Context) {
	// 获取当前用户
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "not login")
		return
	}

	// 创建新的邀请码
	inviteCode := InviteCode{
		UserID: userID,
		Remark: "邀请码",
	}
	err := db.Get().Create(&inviteCode).Error
	if err != nil {
		Error(c, ErrorSystemError, "create invite code failed")
		return
	}

	// 获取邀请码
	code := inviteCode.GetCode()
	log.Infof(c, "created invite code %s for user %d", code, userID)

	// 转换为 API 响应格式
	data := DataMyInviteCode{
		Code:           code,
		CreatedAt:      inviteCode.CreatedAt.Unix(),
		Remark:         inviteCode.Remark,
		Link:           inviteCode.Link(),
		Config:         configInvite(c),
		RegisterCount:  0,
		PurchaseCount:  0,
		PurchaseReward: 0,
	}

	Success[DataMyInviteCode](c, &data)
}

// DataUpdateMyInviteCodeRemarkRequest 更新邀请码备注请求数据结构
//
type DataUpdateMyInviteCodeRemarkRequest struct {
	Remark string `json:"remark" binding:"required" example:"我的邀请码"` // 新的备注
}

// api_update_my_invite_code_remark 更新我的邀请码备注
//
func api_update_my_invite_code_remark(c *gin.Context) {
	var req DataUpdateMyInviteCodeRemarkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 获取当前用户
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "not login")
		return
	}

	// 获取邀请码
	code := c.Param("code")
	if code == "" {
		Error(c, ErrorInvalidArgument, "code is required")
		return
	}

	// 将邀请码解码为ID
	inviteCodeID := InviteCodeID(code)
	if inviteCodeID == 0 {
		log.Warnf(c, "invalid invite code format: %s", code)
		Error(c, ErrorInvalidArgument, "invalid invite code")
		return
	}

	// 查询邀请码（验证是否属于当前用户）
	var inviteCode InviteCode
	err := db.Get().Where(&InviteCode{ID: inviteCodeID, UserID: userID}).First(&inviteCode).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warnf(c, "invite code %s (id=%d) not found for user %d", code, inviteCodeID, userID)
			Error(c, ErrorNotFound, "invite code not found")
			return
		}
		log.Errorf(c, "failed to get invite code %s (id=%d) for user %d: %v", code, inviteCodeID, userID, err)
		Error(c, ErrorSystemError, "get invite code failed")
		return
	}

	// 更新备注
	err = db.Get().Model(&inviteCode).Update("remark", req.Remark).Error
	if err != nil {
		log.Errorf(c, "failed to update remark for invite code %s (id=%d): %v", code, inviteCodeID, err)
		Error(c, ErrorSystemError, "update invite code remark failed")
		return
	}

	log.Infof(c, "successfully updated remark for invite code %s (id=%d) for user %d", code, inviteCodeID, userID)
	SuccessEmpty(c)
}
