package center

import (
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// ===================== 优惠活动管理 =====================

// api_admin_list_campaigns 处理获取优惠活动列表的请求（管理员）
//
func api_admin_list_campaigns(c *gin.Context) {
	log.Infof(c, "admin request to list campaigns")

	// 参数验证
	pagination := PaginationFromRequest(c)
	campaignType := c.Query("type")
	isActiveStr := c.Query("isActive")

	// 构建查询
	query := db.Get().Model(&Campaign{})

	// 类型筛选
	if campaignType != "" {
		query = query.Where(&Campaign{Type: campaignType})
	}

	// 状态筛选
	// 注意：GORM struct 查询会忽略零值（false），所以布尔字段必须用 map 查询
	if isActiveStr != "" {
		isActive, err := strconv.ParseBool(isActiveStr)
		if err != nil {
			log.Warnf(c, "invalid isActive parameter: %v", err)
			Error(c, ErrorInvalidArgument, "invalid isActive parameter")
			return
		}
		query = query.Where(&Campaign{IsActive: BoolPtr(isActive)})
	}

	// 统计总数
	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count campaigns: %v", err)
		Error(c, ErrorSystemError, "failed to count campaigns")
		return
	}

	// 分页查询
	var campaigns []Campaign
	if err := query.Offset(pagination.Offset()).Limit(pagination.PageSize).Order("created_at DESC").Find(&campaigns).Error; err != nil {
		log.Errorf(c, "failed to query campaigns: %v", err)
		Error(c, ErrorSystemError, "failed to query campaigns")
		return
	}

	// 转换响应格式
	items := make([]CampaignResponse, len(campaigns))
	for i, campaign := range campaigns {
		items[i] = convertCampaignToResponse(campaign)
	}

	log.Infof(c, "successfully retrieved %d campaigns", len(items))
	ListWithData(c, items, pagination)
}

// api_admin_get_campaign 处理获取单个优惠活动详情的请求（管理员）
//
func api_admin_get_campaign(c *gin.Context) {
	log.Infof(c, "admin request to get campaign")

	// 参数验证
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		log.Warnf(c, "invalid campaign id: %s", idStr)
		Error(c, ErrorInvalidArgument, "invalid campaign id")
		return
	}

	// 查询活动
	var campaign Campaign
	if err := db.Get().Where(&Campaign{ID: id}).First(&campaign).Error; err != nil {
		log.Warnf(c, "campaign not found: %d", id)
		Error(c, ErrorNotFound, "campaign not found")
		return
	}

	response := convertCampaignToResponse(campaign)
	log.Infof(c, "successfully retrieved campaign: %d", id)
	Success(c, &response)
}

// api_admin_create_campaign 处理创建优惠活动的请求（管理员）
//
func api_admin_create_campaign(c *gin.Context) {
	log.Infof(c, "admin request to create campaign")

	var req CampaignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 验证活动类型
	if req.Type != CampaignTypeDiscount && req.Type != CampaignTypeCoupon {
		log.Warnf(c, "invalid campaign type: %s", req.Type)
		Error(c, ErrorInvalidArgument, "invalid campaign type")
		return
	}

	// 验证匹配器类型
	validMatcherTypes := []string{"first_order", "vip", "all"}
	if !slices.Contains(validMatcherTypes, req.MatcherType) {
		log.Warnf(c, "invalid matcher type: %s", req.MatcherType)
		Error(c, ErrorInvalidArgument, "invalid matcher type")
		return
	}

	// 验证时间
	if req.StartAt >= req.EndAt {
		log.Warnf(c, "invalid time range: start %d >= end %d", req.StartAt, req.EndAt)
		Error(c, ErrorInvalidArgument, "start time must be before end time")
		return
	}

	// 检查活动代码是否已存在
	var existing Campaign
	if err := db.Get().Where(&Campaign{Code: req.Code}).First(&existing).Error; err == nil {
		log.Warnf(c, "campaign code already exists: %s", req.Code)
		Error(c, ErrorInvalidArgument, "campaign code already exists")
		return
	}

	// 创建活动
	campaign := Campaign{
		Code:        req.Code,
		Name:        req.Name,
		Type:        req.Type,
		Value:       req.Value,
		StartAt:     req.StartAt,
		EndAt:       req.EndAt,
		Description: req.Description,
		IsActive:    BoolPtr(req.IsActive),
		MatcherType: req.MatcherType,
		MaxUsage:    req.MaxUsage,
	}

	if err := db.Get().Create(&campaign).Error; err != nil {
		log.Errorf(c, "failed to create campaign: %v", err)
		Error(c, ErrorSystemError, "failed to create campaign")
		return
	}

	response := convertCampaignToResponse(campaign)
	log.Infof(c, "successfully created campaign with ID: %d", campaign.ID)
	Success(c, &response)
}

// api_admin_update_campaign 处理更新优惠活动的请求（管理员）
//
func api_admin_update_campaign(c *gin.Context) {
	log.Infof(c, "admin request to update campaign")

	// 参数验证
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		log.Warnf(c, "invalid campaign id: %s", idStr)
		Error(c, ErrorInvalidArgument, "invalid campaign id")
		return
	}

	var req CampaignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 查询现有活动
	var campaign Campaign
	if err := db.Get().Where(&Campaign{ID: id}).First(&campaign).Error; err != nil {
		log.Warnf(c, "campaign not found: %d", id)
		Error(c, ErrorNotFound, "campaign not found")
		return
	}

	// 验证活动类型
	if req.Type != CampaignTypeDiscount && req.Type != CampaignTypeCoupon {
		log.Warnf(c, "invalid campaign type: %s", req.Type)
		Error(c, ErrorInvalidArgument, "invalid campaign type")
		return
	}

	// 验证匹配器类型
	validMatcherTypes := []string{"first_order", "vip", "all"}
	if !slices.Contains(validMatcherTypes, req.MatcherType) {
		log.Warnf(c, "invalid matcher type: %s", req.MatcherType)
		Error(c, ErrorInvalidArgument, "invalid matcher type")
		return
	}

	// 验证时间
	if req.StartAt >= req.EndAt {
		log.Warnf(c, "invalid time range: start %d >= end %d", req.StartAt, req.EndAt)
		Error(c, ErrorInvalidArgument, "start time must be before end time")
		return
	}

	// 检查活动代码是否被其他活动使用
	if req.Code != campaign.Code {
		var existing Campaign
		if err := db.Get().Where(&Campaign{Code: req.Code}).First(&existing).Error; err == nil {
			log.Warnf(c, "campaign code already exists: %s", req.Code)
			Error(c, ErrorInvalidArgument, "campaign code already exists")
			return
		}
	}

	// 更新活动
	campaign.Code = req.Code
	campaign.Name = req.Name
	campaign.Type = req.Type
	campaign.Value = req.Value
	campaign.StartAt = req.StartAt
	campaign.EndAt = req.EndAt
	campaign.Description = req.Description
	campaign.IsActive = BoolPtr(req.IsActive)
	campaign.MatcherType = req.MatcherType
	campaign.MaxUsage = req.MaxUsage

	if err := db.Get().Save(&campaign).Error; err != nil {
		log.Errorf(c, "failed to update campaign: %v", err)
		Error(c, ErrorSystemError, "failed to update campaign")
		return
	}

	response := convertCampaignToResponse(campaign)
	log.Infof(c, "successfully updated campaign with ID: %d", campaign.ID)
	Success(c, &response)
}

// api_admin_delete_campaign 处理删除优惠活动的请求（管理员）
//
func api_admin_delete_campaign(c *gin.Context) {
	log.Infof(c, "admin request to delete campaign")

	// 参数验证
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		log.Warnf(c, "invalid campaign id: %s", idStr)
		Error(c, ErrorInvalidArgument, "invalid campaign id")
		return
	}

	// 查询活动是否存在
	var campaign Campaign
	if err := db.Get().Where(&Campaign{ID: id}).First(&campaign).Error; err != nil {
		log.Warnf(c, "campaign not found: %d", id)
		Error(c, ErrorNotFound, "campaign not found")
		return
	}

	// 软删除活动
	if err := db.Get().Delete(&campaign).Error; err != nil {
		log.Errorf(c, "failed to delete campaign: %v", err)
		Error(c, ErrorSystemError, "failed to delete campaign")
		return
	}

	log.Infof(c, "successfully deleted campaign with ID: %d", id)
	SuccessEmpty(c)
}

// api_admin_get_campaign_stats 获取优惠活动统计数据（管理员）
//
func api_admin_get_campaign_stats(c *gin.Context) {
	log.Infof(c, "admin request to get campaign stats")

	campaignCode := c.Param("code")
	if campaignCode == "" {
		log.Warnf(c, "empty campaign code")
		Error(c, ErrorInvalidArgument, "campaign code is required")
		return
	}

	// 检查Campaign是否存在
	var campaign Campaign
	if err := db.Get().Where(&Campaign{Code: campaignCode}).First(&campaign).Error; err != nil {
		log.Warnf(c, "campaign not found: %s", campaignCode)
		Error(c, ErrorNotFound, "campaign not found")
		return
	}

	// 获取统计数据
	stats, err := getCampaignStats(c, campaignCode)
	if err != nil {
		log.Errorf(c, "failed to get campaign stats: %v", err)
		Error(c, ErrorSystemError, "failed to get campaign stats")
		return
	}

	log.Infof(c, "successfully retrieved stats for campaign: %s", campaignCode)
	Success(c, stats)
}

// api_admin_get_campaign_orders 获取优惠活动的订单列表（管理员）
//
func api_admin_get_campaign_orders(c *gin.Context) {
	log.Infof(c, "admin request to get campaign orders")

	campaignCode := c.Param("code")
	if campaignCode == "" {
		log.Warnf(c, "empty campaign code")
		Error(c, ErrorInvalidArgument, "campaign code is required")
		return
	}

	// 检查Campaign是否存在
	var campaign Campaign
	if err := db.Get().Where(&Campaign{Code: campaignCode}).First(&campaign).Error; err != nil {
		log.Warnf(c, "campaign not found: %s", campaignCode)
		Error(c, ErrorNotFound, "campaign not found")
		return
	}

	// 参数验证
	pagination := PaginationFromRequest(c)

	// 获取订单列表
	orders, err := getCampaignOrders(c, campaignCode, pagination)
	if err != nil {
		log.Errorf(c, "failed to get campaign orders: %v", err)
		Error(c, ErrorSystemError, "failed to get campaign orders")
		return
	}

	// 转换响应格式（简化版，只返回关键信息）
	items := make([]map[string]interface{}, len(orders))
	for i, order := range orders {
		items[i] = map[string]interface{}{
			"id":                   order.ID,
			"uuid":                 order.UUID,
			"title":                order.Title,
			"originAmount":         order.OriginAmount,
			"campaignReduceAmount": order.CampaignReduceAmount,
			"payAmount":            order.PayAmount,
			"isPaid":               order.IsPaid,
			"createdAt":            order.CreatedAt.Unix(),
			"paidAt":               nil,
			"user": map[string]interface{}{
				"id": order.UserID,
			},
		}
		if order.PaidAt != nil {
			items[i]["paidAt"] = order.PaidAt.Unix()
		}
		if order.User != nil {
			userInfo := map[string]interface{}{
				"id": order.User.ID,
			}
			// 获取用户邮箱（如果有）
			if len(order.User.LoginIdentifies) > 0 {
				userInfo["email"] = order.User.LoginIdentifies[0].IndexID // 使用IndexID作为显示邮箱
			}
			items[i]["user"] = userInfo
		}
	}

	log.Infof(c, "successfully retrieved %d orders for campaign: %s", len(items), campaignCode)
	ListWithData(c, items, pagination)
}

// api_admin_get_campaign_funnel 获取优惠活动转化漏斗（管理员）
//
func api_admin_get_campaign_funnel(c *gin.Context) {
	log.Infof(c, "admin request to get campaign funnel")

	campaignCode := c.Param("code")
	if campaignCode == "" {
		log.Warnf(c, "empty campaign code")
		Error(c, ErrorInvalidArgument, "campaign code is required")
		return
	}

	// 检查Campaign是否存在
	var campaign Campaign
	if err := db.Get().Where(&Campaign{Code: campaignCode}).First(&campaign).Error; err != nil {
		log.Warnf(c, "campaign not found: %s", campaignCode)
		Error(c, ErrorNotFound, "campaign not found")
		return
	}

	// 获取漏斗数据
	funnel, err := getCampaignFunnel(c, campaignCode)
	if err != nil {
		log.Errorf(c, "failed to get campaign funnel: %v", err)
		Error(c, ErrorSystemError, "failed to get campaign funnel")
		return
	}

	log.Infof(c, "successfully retrieved funnel for campaign: %s", campaignCode)
	Success(c, &funnel)
}

// ===================== 辅助函数 =====================

// convertCampaignToResponse 转换Campaign到响应格式
func convertCampaignToResponse(campaign Campaign) CampaignResponse {
	return CampaignResponse{
		ID:          campaign.ID,
		CreatedAt:   campaign.CreatedAt.Unix(),
		UpdatedAt:   campaign.UpdatedAt.Unix(),
		Code:        campaign.Code,
		Name:        campaign.Name,
		Type:        campaign.Type,
		Value:       campaign.Value,
		StartAt:     campaign.StartAt,
		EndAt:       campaign.EndAt,
		Description: campaign.Description,
		IsActive:    campaign.IsActive != nil && *campaign.IsActive,
		MatcherType: campaign.MatcherType,
		UsageCount:  campaign.UsageCount,
		MaxUsage:    campaign.MaxUsage,
	}
}
