package center

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// ===================== Campaign License Key 发放 =====================

// POST /app/campaigns/:id/issue-keys
// DryRun=true returns count only; DryRun=false generates and sends keys.
func api_admin_issue_license_keys(c *gin.Context) {
	log.Infof(c, "admin request to issue license keys for campaign")

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		log.Warnf(c, "invalid campaign id: %s", c.Param("id"))
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	var req IssueKeysRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request body: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var campaign Campaign
	if err := db.Get().First(&campaign, id).Error; err != nil {
		log.Warnf(c, "campaign %d not found: %v", id, err)
		Error(c, ErrorNotFound, "campaign not found")
		return
	}
	if !campaign.IsShareable {
		log.Warnf(c, "campaign %d is not shareable", id)
		Error(c, ErrorInvalidArgument, "campaign is not shareable")
		return
	}

	ctx := c.Request.Context()

	if req.DryRun {
		count, err := CountEligibleUsers(ctx, &campaign)
		if err != nil {
			log.Errorf(c, "failed to count eligible users for campaign %d: %v", id, err)
			Error(c, ErrorSystemError, err.Error())
			return
		}
		resp := IssueKeysResponse{
			EligibleUsers: count,
			KeysToIssue:   count * campaign.SharesPerUser,
			Issued:        false,
		}
		log.Infof(c, "dry run: campaign %d eligible=%d keysToIssue=%d", id, count, resp.KeysToIssue)
		Success(c, &resp)
		return
	}

	// 提交审批
	params := struct {
		CampaignID uint64 `json:"campaignId"`
	}{CampaignID: id}
	summary := fmt.Sprintf("为活动「%s」发放 License Key", campaign.Name)
	approvalID, err := SubmitApproval(c, "campaign_issue_keys", params, summary)
	if err != nil {
		log.Errorf(c, "failed to submit approval for issue keys campaign %d: %v", id, err)
		Error(c, ErrorSystemError, "failed to submit approval")
		return
	}

	log.Infof(c, "issue keys for campaign %d submitted for approval: %d", id, approvalID)
	Success(c, &ApprovalSubmitResponse{ApprovalID: approvalID, Status: "pending"})
}

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
	validMatcherTypes := []string{"first_order", "vip", "all", "paid_before", "paid_before_active"}
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

	// 提交审批
	summary := fmt.Sprintf("创建优惠活动「%s」，代码 %s", req.Name, req.Code)
	approvalID, err := SubmitApproval(c, "campaign_create", req, summary)
	if err != nil {
		log.Errorf(c, "failed to submit approval for campaign create: %v", err)
		Error(c, ErrorSystemError, "failed to submit approval")
		return
	}

	log.Infof(c, "campaign create submitted for approval: %d", approvalID)
	Success(c, &ApprovalSubmitResponse{ApprovalID: approvalID, Status: "pending"})
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
	validMatcherTypes := []string{"first_order", "vip", "all", "paid_before", "paid_before_active"}
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

	// 提交审批
	params := campaignUpdateApprovalParams{
		CampaignID: id,
		Request:    req,
	}
	summary := fmt.Sprintf("修改优惠活动「%s」(ID:%d)", req.Name, id)
	approvalID, err := SubmitApproval(c, "campaign_update", params, summary)
	if err != nil {
		log.Errorf(c, "failed to submit approval for campaign update: %v", err)
		Error(c, ErrorSystemError, "failed to submit approval")
		return
	}

	log.Infof(c, "campaign update submitted for approval: %d", approvalID)
	Success(c, &ApprovalSubmitResponse{ApprovalID: approvalID, Status: "pending"})
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

	// 提交审批
	params := struct {
		CampaignID uint64 `json:"campaignId"`
	}{CampaignID: id}
	summary := fmt.Sprintf("删除优惠活动「%s」(ID:%d)", campaign.Name, id)
	approvalID, err := SubmitApproval(c, "campaign_delete", params, summary)
	if err != nil {
		log.Errorf(c, "failed to submit approval for campaign delete: %v", err)
		Error(c, ErrorSystemError, "failed to submit approval")
		return
	}

	log.Infof(c, "campaign delete submitted for approval: %d", approvalID)
	Success(c, &ApprovalSubmitResponse{ApprovalID: approvalID, Status: "pending"})
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
		ID:            campaign.ID,
		CreatedAt:     campaign.CreatedAt.Unix(),
		UpdatedAt:     campaign.UpdatedAt.Unix(),
		Code:          campaign.Code,
		Name:          campaign.Name,
		Type:          campaign.Type,
		Value:         campaign.Value,
		StartAt:       campaign.StartAt,
		EndAt:         campaign.EndAt,
		Description:   campaign.Description,
		IsActive:      campaign.IsActive != nil && *campaign.IsActive,
		MatcherType:   campaign.MatcherType,
		MatcherParams: campaign.MatcherParams,
		IsShareable:   campaign.IsShareable,
		SharesPerUser: campaign.SharesPerUser,
		UsageCount:    campaign.UsageCount,
		MaxUsage:      campaign.MaxUsage,
	}
}
