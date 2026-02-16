package center

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// ===================== 邮件模板管理 =====================

// api_admin_list_email_templates 处理获取邮件模板列表的请求（管理员）
//
func api_admin_list_email_templates(c *gin.Context) {
	log.Infof(c, "admin request to list email templates")

	// 参数验证
	pagination := PaginationFromRequest(c)
	isActiveStr := c.Query("isActive")

	// 构建查询 - 只返回原始模板（OriginID为NULL的模板）
	query := db.Get().Model(&EmailMarketingTemplate{}).Where("origin_id IS NULL")

	// 状态筛选
	if isActiveStr != "" {
		isActive, err := strconv.ParseBool(isActiveStr)
		if err != nil {
			log.Warnf(c, "invalid isActive parameter: %v", err)
			Error(c, ErrorInvalidArgument, "invalid isActive parameter")
			return
		}
		query = query.Where("is_active = ?", isActive)
	}

	// 统计总数
	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count templates: %v", err)
		Error(c, ErrorSystemError, "failed to count templates")
		return
	}

	// 分页查询
	var templates []EmailMarketingTemplate
	if err := query.Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&templates).Error; err != nil {
		log.Errorf(c, "failed to query templates: %v", err)
		Error(c, ErrorSystemError, "failed to query templates")
		return
	}

	// 转换响应格式
	items := make([]EmailTemplateResponse, len(templates))
	for i, template := range templates {
		items[i] = convertEmailMarketingTemplateToResponse(template)
	}

	log.Infof(c, "successfully retrieved %d templates", len(items))
	ListWithData(c, items, pagination)
}

// api_admin_create_email_template 处理创建邮件模板的请求（管理员）
//
func api_admin_create_email_template(c *gin.Context) {
	log.Infof(c, "admin request to create email template")

	var req EmailTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 标准化语言代码
	normalizedLang := NormalizeBCP47Language(req.Language)

	// 验证BCP 47语言标签
	if !IsValidBCP47Language(normalizedLang) {
		log.Warnf(c, "invalid BCP 47 language tag: %s", req.Language)
		Error(c, ErrorInvalidArgument, fmt.Sprintf("invalid BCP 47 language tag: %s", req.Language))
		return
	}

	// 创建模板
	template := EmailMarketingTemplate{
		Name:        req.Name,
		Language:    normalizedLang,
		Subject:     req.Subject,
		Content:     req.Content,
		Description: req.Description,
		IsActive:    BoolPtr(req.IsActive),
		OriginID:    req.OriginID,
	}

	// 创建模板
	if err := db.Get().Create(&template).Error; err != nil {
		log.Errorf(c, "failed to create template: %v", err)
		Error(c, ErrorSystemError, "failed to create template")
		return
	}

	response := convertEmailMarketingTemplateToResponse(template)
	log.Infof(c, "successfully created template with ID: %d", template.ID)
	Success(c, &response)
}

// api_admin_update_email_template 处理更新邮件模板的请求（管理员）
//
func api_admin_update_email_template(c *gin.Context) {
	templateID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid template ID")
		return
	}

	var req EmailTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 验证模板存在
	var template EmailMarketingTemplate
	if err := db.Get().Where("id = ?", templateID).First(&template).Error; err != nil {
		log.Errorf(c, "template not found: %v", err)
		Error(c, ErrorInvalidArgument, "template not found")
		return
	}

	// 标准化语言代码
	normalizedLang := NormalizeBCP47Language(req.Language)

	// 验证BCP 47语言标签
	if !IsValidBCP47Language(normalizedLang) {
		log.Warnf(c, "invalid BCP 47 language tag: %s", req.Language)
		Error(c, ErrorInvalidArgument, fmt.Sprintf("invalid BCP 47 language tag: %s", req.Language))
		return
	}

	// 更新模板
	updates := map[string]any{
		"name":        req.Name,
		"language":    normalizedLang,
		"subject":     req.Subject,
		"content":     req.Content,
		"description": req.Description,
		"is_active":   req.IsActive,
	}

	if err := db.Get().Model(&template).Updates(updates).Error; err != nil {
		log.Errorf(c, "failed to update template: %v", err)
		Error(c, ErrorSystemError, "failed to update template")
		return
	}

	// 重新查询更新后的模板
	if err := db.Get().Where("id = ?", templateID).First(&template).Error; err != nil {
		log.Errorf(c, "failed to reload template: %v", err)
		Error(c, ErrorSystemError, "failed to reload template")
		return
	}

	response := convertEmailMarketingTemplateToResponse(template)
	log.Infof(c, "successfully updated template %d", templateID)
	Success(c, &response)
}

// api_admin_delete_email_template 处理删除邮件模板的请求（管理员）
//
func api_admin_delete_email_template(c *gin.Context) {
	templateID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid template ID")
		return
	}

	// 验证模板存在
	var template EmailMarketingTemplate
	if err := db.Get().Where("id = ?", templateID).First(&template).Error; err != nil {
		log.Errorf(c, "template not found: %v", err)
		Error(c, ErrorInvalidArgument, "template not found")
		return
	}

	// 删除模板
	if err := db.Get().Delete(&template).Error; err != nil {
		log.Errorf(c, "failed to delete template: %v", err)
		Error(c, ErrorSystemError, "failed to delete template")
		return
	}

	log.Infof(c, "successfully deleted template %d", templateID)
	var emptyResponse struct{}
	Success(c, &emptyResponse)
}

// api_admin_translate_email_template 处理自动翻译邮件模板的请求（管理员）
//
func api_admin_translate_email_template(c *gin.Context) {
	ctx := c.Request.Context()

	// 解析参数
	templateID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid template ID")
		return
	}

	targetLang := c.Param("language")
	if targetLang == "" {
		Error(c, ErrorInvalidArgument, "target language is required")
		return
	}

	log.Infof(ctx, "admin request to translate template %d to %s", templateID, targetLang)

	// 调用 getTemplateForLanguage 触发自动翻译
	// 这个函数会：
	// 1. 查找是否已存在目标语言的翻译
	// 2. 如果不存在，使用 DeepL 自动翻译并保存到数据库
	// 3. 返回翻译后的模板
	translatedTemplate, err := getTemplateForLanguage(ctx, templateID, targetLang)
	if err != nil {
		log.Errorf(ctx, "failed to translate template: %v", err)
		Error(c, ErrorSystemError, fmt.Sprintf("翻译失败: %v", err))
		return
	}

	log.Infof(ctx, "successfully translated template %d to %s, new template ID: %d",
		templateID, targetLang, translatedTemplate.ID)

	// 返回翻译后的模板
	response := convertEmailMarketingTemplateToResponse(*translatedTemplate)
	Success(c, &response)
}

// 邮件模板可用参数（前后端约定）：
// - {{.UserEmail}}      : 用户邮箱
// - {{.ExpiredAt}}      : 过期日期 (YYYY-MM-DD)
// - {{.RemainingDays}}  : 剩余天数
// - {{.DeviceCount}}    : 已用设备数
// - {{.MaxDevices}}     : 最大设备数
// - {{.IsPro}}          : 是否Pro用户 (true/false)
// - {{.IsExpiringSoon}} : 是否即将过期 (true/false)

// ===================== 邮件营销活动管理 =====================

// ===================== 辅助函数 =====================

// convertEmailMarketingTemplateToResponse 转换EmailMarketingTemplate到响应格式
func convertEmailMarketingTemplateToResponse(template EmailMarketingTemplate) EmailTemplateResponse {
	response := EmailTemplateResponse{
		ID:          template.ID,
		CreatedAt:   template.CreatedAt.Unix(),
		UpdatedAt:   template.UpdatedAt.Unix(),
		Name:        template.Name,
		Language:    template.Language,
		Subject:     template.Subject,
		Content:     template.Content,
		Description: template.Description,
		IsActive:    template.IsActive != nil && *template.IsActive,
		OriginID:    template.OriginID,
		IsOriginal:  template.OriginID == nil, // 如果OriginID为nil，则为原始模板
	}

	return response
}

// ===================== EDM 发送任务（基于 Asynq）=====================

// EDMTaskResponse EDM任务创建响应
type EDMTaskResponse struct {
	BatchID     string `json:"batchId"`     // asynq 任务 ID
	TemplateID  uint64 `json:"templateId"`  // 模板 ID
	ScheduledAt *int64 `json:"scheduledAt"` // 计划执行时间（Unix时间戳）
}

// api_admin_create_edm_task 创建 EDM 发送任务（直接入队 Asynq）
//
func api_admin_create_edm_task(c *gin.Context) {
	log.Infof(c, "admin request to create EDM task")

	var req CreateEDMTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 验证模板存在
	var template EmailMarketingTemplate
	if err := db.Get().Where("id = ? AND is_active = ?", req.TemplateID, true).
		First(&template).Error; err != nil {
		log.Errorf(c, "template not found or inactive: %v", err)
		Error(c, ErrorInvalidArgument, "template not found or inactive")
		return
	}

	// 目前只支持单次任务，重复任务需要使用 asynq.Cron
	if req.Type == "repeat" {
		log.Warnf(c, "repeat task is not supported, use asynq cron instead")
		Error(c, ErrorInvalidArgument, "repeat task is not supported, please use asynqmon to configure cron tasks")
		return
	}

	// 确定执行时间
	var scheduledAt *time.Time
	if req.ScheduledAt != nil {
		t := time.Unix(*req.ScheduledAt, 0)
		scheduledAt = &t
	}

	// 直接入队到 Asynq
	batchID, err := EnqueueEDMTask(c.Request.Context(), req.TemplateID, req.UserFilters, scheduledAt)
	if err != nil {
		log.Errorf(c, "failed to enqueue EDM task: %v", err)
		Error(c, ErrorSystemError, "failed to enqueue task")
		return
	}

	log.Infof(c, "successfully enqueued EDM task, batchId=%s", batchID)
	resp := EDMTaskResponse{
		BatchID:     batchID,
		TemplateID:  req.TemplateID,
		ScheduledAt: req.ScheduledAt,
	}
	Success(c, &resp)
}

// api_admin_preview_edm_targets 预览EDM目标用户
//
func api_admin_preview_edm_targets(c *gin.Context) {
	log.Infof(c, "admin request to preview EDM targets")

	var req PreviewEDMTargetsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 获取目标用户
	users, err := getTargetUsersForEmailTask(c, req.UserFilters)
	if err != nil {
		log.Errorf(c, "[EDM] Failed to get target users: %v", err)
		Error(c, ErrorSystemError, "failed to get target users")
		return
	}

	// 构建预览响应
	response := PreviewEDMTargetsResponse{
		TotalCount: len(users),
		SampleUsers: make([]PreviewEDMUser, 0),
	}

	// 返回前10个用户作为样本
	sampleSize := 10
	if len(users) < sampleSize {
		sampleSize = len(users)
	}

	for i := 0; i < sampleSize; i++ {
		user := users[i]
		email, _ := getUserEmailByUser(c, &user)

		response.SampleUsers = append(response.SampleUsers, PreviewEDMUser{
			UUID:     user.UUID,
			Email:    email,
			Language: user.GetLanguagePreference(),
			IsPro:    user.IsPro(),
		})
	}

	log.Infof(c, "preview result: %d target users", response.TotalCount)
	Success(c, &response)
}

// ===================== 邮件发送日志管理 =====================

// api_admin_list_email_send_logs 获取邮件发送日志列表
//
func api_admin_list_email_send_logs(c *gin.Context) {
	log.Infof(c, "admin request to list email send logs")

	var req ListEmailSendLogsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 默认分页参数
	if req.Page < 0 {
		req.Page = 0
	}
	if req.PageSize <= 0 {
		req.PageSize = 100
	}
	if req.PageSize > 200 {
		req.PageSize = 200
	}

	// 构建查询
	query := db.Get().Model(&EmailSendLog{})

	// 按批次ID筛选
	if req.BatchID != nil && *req.BatchID != "" {
		query = query.Where("batch_id = ?", *req.BatchID)
	}

	// 按模板ID筛选
	if req.TemplateID != nil {
		query = query.Where("template_id = ?", *req.TemplateID)
	}

	// 按用户ID筛选
	if req.UserID != nil {
		query = query.Where("user_id = ?", *req.UserID)
	}

	// 按状态筛选
	if req.Status != nil && *req.Status != "" {
		query = query.Where("status = ?", *req.Status)
	}

	// 按邮箱筛选（模糊匹配）
	if req.Email != nil && *req.Email != "" {
		query = query.Where("email LIKE ?", "%"+*req.Email+"%")
	}

	// 统计各状态数量（在筛选条件基础上）
	var stats EmailSendLogStats
	statsQuery := query.Session(&gorm.Session{})
	statsQuery.Count(&stats.TotalCount)

	// 分别统计各状态
	var sentCount, failedCount, pendingCount, skippedCount int64
	query.Session(&gorm.Session{}).Where("status = ?", EmailSendLogStatusSent).Count(&sentCount)
	query.Session(&gorm.Session{}).Where("status = ?", EmailSendLogStatusFailed).Count(&failedCount)
	query.Session(&gorm.Session{}).Where("status = ?", EmailSendLogStatusPending).Count(&pendingCount)
	query.Session(&gorm.Session{}).Where("status = ?", EmailSendLogStatusSkipped).Count(&skippedCount)
	stats.SentCount = sentCount
	stats.FailedCount = failedCount
	stats.PendingCount = pendingCount
	stats.SkippedCount = skippedCount

	// 分页查询
	var logs []EmailSendLog
	offset := req.Page * req.PageSize
	if err := query.Order("created_at DESC").Offset(offset).Limit(req.PageSize).Find(&logs).Error; err != nil {
		log.Errorf(c, "failed to query email send logs: %v", err)
		Error(c, ErrorSystemError, "failed to query email send logs")
		return
	}

	// 批量获取关联数据
	templateIDs := make([]uint64, 0)
	userIDs := make([]uint64, 0)
	for _, l := range logs {
		templateIDs = append(templateIDs, l.TemplateID)
		userIDs = append(userIDs, l.UserID)
	}

	// 获取模板名称
	templateNames := make(map[uint64]string)
	if len(templateIDs) > 0 {
		var templates []EmailMarketingTemplate
		db.Get().Select("id", "name").Where("id IN ?", templateIDs).Find(&templates)
		for _, t := range templates {
			templateNames[t.ID] = t.Name
		}
	}

	// 获取用户UUID
	userUUIDs := make(map[uint64]string)
	if len(userIDs) > 0 {
		var users []User
		db.Get().Select("id", "uuid").Where("id IN ?", userIDs).Find(&users)
		for _, u := range users {
			userUUIDs[u.ID] = u.UUID
		}
	}

	// 转换响应格式
	items := make([]EmailSendLogResponse, len(logs))
	for i, l := range logs {
		items[i] = EmailSendLogResponse{
			ID:           l.ID,
			CreatedAt:    l.CreatedAt.Unix(),
			BatchID:      l.BatchID,
			TemplateID:   l.TemplateID,
			TemplateName: templateNames[l.TemplateID],
			UserID:       l.UserID,
			UserUUID:     userUUIDs[l.UserID],
			Email:        l.Email,
			Language:     l.Language,
			Status:       string(l.Status),
			ErrorMsg:     l.ErrorMsg,
		}
		if l.SentAt != nil {
			sentAtUnix := l.SentAt.Unix()
			items[i].SentAt = &sentAtUnix
		}
	}

	// 构建分页信息
	pagination := Pagination{
		Page:     req.Page,
		PageSize: req.PageSize,
		Total:    stats.TotalCount,
	}

	response := ListEmailSendLogsResponse{
		Items:      items,
		Pagination: pagination,
		Stats:      stats,
	}

	log.Infof(c, "successfully retrieved %d email send logs (total: %d)", len(items), stats.TotalCount)
	Success(c, &response)
}

// api_admin_get_email_send_log_stats 获取邮件发送统计
//
func api_admin_get_email_send_log_stats(c *gin.Context) {
	log.Infof(c, "admin request to get email send log stats")

	// 构建查询
	query := db.Get().Model(&EmailSendLog{})

	// 按批次ID筛选
	if batchID := c.Query("batchId"); batchID != "" {
		query = query.Where("batch_id = ?", batchID)
	}

	// 按模板ID筛选
	if templateIDStr := c.Query("templateId"); templateIDStr != "" {
		if templateID, err := strconv.ParseUint(templateIDStr, 10, 64); err == nil {
			query = query.Where("template_id = ?", templateID)
		}
	}

	// 统计各状态数量
	var stats EmailSendLogStats
	query.Count(&stats.TotalCount)

	var sentCount, failedCount, pendingCount, skippedCount int64
	query.Session(&gorm.Session{}).Where("status = ?", EmailSendLogStatusSent).Count(&sentCount)
	query.Session(&gorm.Session{}).Where("status = ?", EmailSendLogStatusFailed).Count(&failedCount)
	query.Session(&gorm.Session{}).Where("status = ?", EmailSendLogStatusPending).Count(&pendingCount)
	query.Session(&gorm.Session{}).Where("status = ?", EmailSendLogStatusSkipped).Count(&skippedCount)

	stats.SentCount = sentCount
	stats.FailedCount = failedCount
	stats.PendingCount = pendingCount
	stats.SkippedCount = skippedCount

	log.Infof(c, "email send log stats: total=%d, sent=%d, failed=%d, pending=%d, skipped=%d",
		stats.TotalCount, stats.SentCount, stats.FailedCount, stats.PendingCount, stats.SkippedCount)
	Success(c, &stats)
}
