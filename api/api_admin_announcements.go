package center

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// AnnouncementRequest 创建/更新公告请求
type AnnouncementRequest struct {
	Message   string `json:"message" binding:"required"`
	LinkURL   string `json:"linkUrl"`
	LinkText  string `json:"linkText"`
	OpenMode  string `json:"openMode"`  // external | webview，默认 external
	ExpiresAt int64  `json:"expiresAt"` // Unix秒，0=不过期
	IsActive  *bool  `json:"isActive"`
}

// AnnouncementResponse 公告响应
type AnnouncementResponse struct {
	ID        uint64 `json:"id"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	Message   string `json:"message"`
	LinkURL   string `json:"linkUrl"`
	LinkText  string `json:"linkText"`
	OpenMode  string `json:"openMode"`
	ExpiresAt int64  `json:"expiresAt"`
	IsActive  bool   `json:"isActive"`
}

func convertAnnouncementToResponse(a Announcement) AnnouncementResponse {
	return AnnouncementResponse{
		ID:        a.ID,
		CreatedAt: a.CreatedAt.Unix(),
		UpdatedAt: a.UpdatedAt.Unix(),
		Message:   a.Message,
		LinkURL:   a.LinkURL,
		LinkText:  a.LinkText,
		OpenMode:  a.OpenMode,
		ExpiresAt: a.ExpiresAt,
		IsActive:  a.IsActive != nil && *a.IsActive,
	}
}

// api_admin_list_announcements 列出所有公告
func api_admin_list_announcements(c *gin.Context) {
	log.Infof(c, "admin request to list announcements")

	pagination := PaginationFromRequest(c)
	query := db.Get().Model(&Announcement{})

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count announcements: %v", err)
		Error(c, ErrorSystemError, "failed to count announcements")
		return
	}

	var announcements []Announcement
	if err := query.Offset(pagination.Offset()).Limit(pagination.PageSize).Order("created_at DESC").Find(&announcements).Error; err != nil {
		log.Errorf(c, "failed to query announcements: %v", err)
		Error(c, ErrorSystemError, "failed to query announcements")
		return
	}

	items := make([]AnnouncementResponse, len(announcements))
	for i, a := range announcements {
		items[i] = convertAnnouncementToResponse(a)
	}

	log.Infof(c, "successfully retrieved %d announcements", len(items))
	ListWithData(c, items, pagination)
}

// api_admin_create_announcement 创建公告
func api_admin_create_announcement(c *gin.Context) {
	log.Infof(c, "admin request to create announcement")

	var req AnnouncementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	if len(req.Message) > 500 {
		Error(c, ErrorInvalidArgument, "message too long (max 500)")
		return
	}

	openMode := req.OpenMode
	if openMode == "" {
		openMode = "external"
	}
	if openMode != "external" && openMode != "webview" {
		Error(c, ErrorInvalidArgument, "openMode must be 'external' or 'webview'")
		return
	}

	isActive := req.IsActive != nil && *req.IsActive

	tx := db.Get().Begin()

	// 如果要激活，先 deactivate 其他所有
	if isActive {
		if err := tx.Model(&Announcement{}).Where("is_active = ?", true).Update("is_active", false).Error; err != nil {
			tx.Rollback()
			log.Errorf(c, "failed to deactivate existing announcements: %v", err)
			Error(c, ErrorSystemError, "failed to create announcement")
			return
		}
	}

	announcement := Announcement{
		Message:   req.Message,
		LinkURL:   req.LinkURL,
		LinkText:  req.LinkText,
		OpenMode:  openMode,
		ExpiresAt: req.ExpiresAt,
		IsActive:  BoolPtr(isActive),
	}

	if err := tx.Create(&announcement).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to create announcement: %v", err)
		Error(c, ErrorSystemError, "failed to create announcement")
		return
	}

	tx.Commit()

	response := convertAnnouncementToResponse(announcement)
	log.Infof(c, "successfully created announcement: %d", announcement.ID)
	Success(c, &response)
}

// api_admin_update_announcement 更新公告
func api_admin_update_announcement(c *gin.Context) {
	log.Infof(c, "admin request to update announcement")

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid announcement id")
		return
	}

	var req AnnouncementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	if len(req.Message) > 500 {
		Error(c, ErrorInvalidArgument, "message too long (max 500)")
		return
	}

	var announcement Announcement
	if err := db.Get().First(&announcement, id).Error; err != nil {
		Error(c, ErrorNotFound, "announcement not found")
		return
	}

	openMode := req.OpenMode
	if openMode == "" {
		openMode = "external"
	}
	if openMode != "external" && openMode != "webview" {
		Error(c, ErrorInvalidArgument, "openMode must be 'external' or 'webview'")
		return
	}

	updates := map[string]interface{}{
		"message":    req.Message,
		"link_url":   req.LinkURL,
		"link_text":  req.LinkText,
		"open_mode":  openMode,
		"expires_at": req.ExpiresAt,
	}

	if err := db.Get().Model(&announcement).Updates(updates).Error; err != nil {
		log.Errorf(c, "failed to update announcement: %v", err)
		Error(c, ErrorSystemError, "failed to update announcement")
		return
	}

	db.Get().First(&announcement, id)
	response := convertAnnouncementToResponse(announcement)
	log.Infof(c, "successfully updated announcement: %d", id)
	Success(c, &response)
}

// api_admin_delete_announcement 删除公告
func api_admin_delete_announcement(c *gin.Context) {
	log.Infof(c, "admin request to delete announcement")

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid announcement id")
		return
	}

	var announcement Announcement
	if err := db.Get().First(&announcement, id).Error; err != nil {
		Error(c, ErrorNotFound, "announcement not found")
		return
	}

	if err := db.Get().Delete(&announcement).Error; err != nil {
		log.Errorf(c, "failed to delete announcement: %v", err)
		Error(c, ErrorSystemError, "failed to delete announcement")
		return
	}

	log.Infof(c, "successfully deleted announcement: %d", id)
	SuccessEmpty(c)
}

// api_admin_activate_announcement 激活公告（同时 deactivate 其他所有）
func api_admin_activate_announcement(c *gin.Context) {
	log.Infof(c, "admin request to activate announcement")

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid announcement id")
		return
	}

	var announcement Announcement
	if err := db.Get().First(&announcement, id).Error; err != nil {
		Error(c, ErrorNotFound, "announcement not found")
		return
	}

	if announcement.ExpiresAt > 0 && time.Now().Unix() > announcement.ExpiresAt {
		Error(c, ErrorInvalidOperation, "cannot activate expired announcement")
		return
	}

	tx := db.Get().Begin()

	if err := tx.Model(&Announcement{}).Where("is_active = ?", true).Update("is_active", false).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to deactivate announcements: %v", err)
		Error(c, ErrorSystemError, "failed to activate announcement")
		return
	}

	if err := tx.Model(&announcement).Update("is_active", true).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to activate announcement: %v", err)
		Error(c, ErrorSystemError, "failed to activate announcement")
		return
	}

	tx.Commit()

	db.Get().First(&announcement, id)
	response := convertAnnouncementToResponse(announcement)
	log.Infof(c, "successfully activated announcement: %d (deactivated all others)", id)
	Success(c, &response)
}

// api_admin_deactivate_announcement 停用公告
func api_admin_deactivate_announcement(c *gin.Context) {
	log.Infof(c, "admin request to deactivate announcement")

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid announcement id")
		return
	}

	var announcement Announcement
	if err := db.Get().First(&announcement, id).Error; err != nil {
		Error(c, ErrorNotFound, "announcement not found")
		return
	}

	if err := db.Get().Model(&announcement).Update("is_active", false).Error; err != nil {
		log.Errorf(c, "failed to deactivate announcement: %v", err)
		Error(c, ErrorSystemError, "failed to deactivate announcement")
		return
	}

	db.Get().First(&announcement, id)
	response := convertAnnouncementToResponse(announcement)
	log.Infof(c, "successfully deactivated announcement: %d", id)
	Success(c, &response)
}

// getActiveAnnouncement 获取当前活跃且未过期的公告（供 /api/app/config 使用）
func getActiveAnnouncement() *DataAnnouncement {
	var announcement Announcement
	err := db.Get().
		Where("is_active = ? AND (expires_at = 0 OR expires_at > ?)", true, time.Now().Unix()).
		First(&announcement).Error
	if err != nil {
		return nil
	}

	return &DataAnnouncement{
		ID:        fmt.Sprintf("%d", announcement.ID),
		Message:   announcement.Message,
		LinkURL:   announcement.LinkURL,
		LinkText:  announcement.LinkText,
		OpenMode:  announcement.OpenMode,
		ExpiresAt: announcement.ExpiresAt,
	}
}
