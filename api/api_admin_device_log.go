package center

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_admin_list_device_logs 管理员查询设备日志记录
func api_admin_list_device_logs(c *gin.Context) {
	pagination := PaginationFromRequest(c)

	query := db.Get().Model(&DeviceLog{})

	// Filters
	if udid := c.Query("udid"); udid != "" {
		query = query.Where("udid = ?", udid)
	}
	if userID := c.Query("user_id"); userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if feedbackID := c.Query("feedback_id"); feedbackID != "" {
		query = query.Where("feedback_id = ?", feedbackID)
	}
	if reason := c.Query("reason"); reason != "" {
		query = query.Where("reason = ?", reason)
	}
	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			query = query.Where("created_at >= ?", t)
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			query = query.Where("created_at <= ?", t)
		}
	}

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count device logs: %v", err)
		Error(c, ErrorSystemError, "failed to count device logs")
		return
	}

	var logs []DeviceLog
	if err := query.Offset(pagination.Offset()).Limit(pagination.PageSize).Order("created_at DESC").Find(&logs).Error; err != nil {
		log.Errorf(c, "failed to query device logs: %v", err)
		Error(c, ErrorSystemError, "failed to query device logs")
		return
	}

	items := make([]DeviceLogResponse, len(logs))
	for i, l := range logs {
		items[i] = DeviceLogResponse{
			ID:         l.ID,
			UDID:       l.UDID,
			UserID:     l.UserID,
			FeedbackID: l.FeedbackID,
			S3Key:      l.S3Key,
			LogType:    l.LogType,
			Reason:     l.Reason,
			Meta:       l.Meta,
			CreatedAt:  l.CreatedAt.Unix(),
		}
	}

	ListWithData(c, items, pagination)
}

// api_admin_list_feedback_tickets 管理员查询反馈工单
func api_admin_list_feedback_tickets(c *gin.Context) {
	pagination := PaginationFromRequest(c)

	query := db.Get().Model(&FeedbackTicket{})

	if udid := c.Query("udid"); udid != "" {
		query = query.Where("udid = ?", udid)
	}
	if email := c.Query("email"); email != "" {
		query = query.Where("email LIKE ?", "%"+email+"%")
	}
	if userID := c.Query("user_id"); userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	if lastReplyBy := c.Query("lastReplyBy"); lastReplyBy != "" {
		query = query.Where("last_reply_by = ?", lastReplyBy)
	}
	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			query = query.Where("created_at >= ?", t)
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			query = query.Where("created_at <= ?", t)
		}
	}

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count feedback tickets: %v", err)
		Error(c, ErrorSystemError, "failed to count feedback tickets")
		return
	}

	var tickets []FeedbackTicket
	if err := query.Offset(pagination.Offset()).Limit(pagination.PageSize).Order("created_at DESC").Find(&tickets).Error; err != nil {
		log.Errorf(c, "failed to query feedback tickets: %v", err)
		Error(c, ErrorSystemError, "failed to query feedback tickets")
		return
	}

	// Get log counts per ticket
	items := make([]FeedbackTicketResponse, len(tickets))
	for i, t := range tickets {
		var logCount int64
		db.Get().Model(&DeviceLog{}).Where("feedback_id = ?", t.FeedbackID).Count(&logCount)

		var resolvedAt *int64
		if t.ResolvedAt != nil {
			ts := t.ResolvedAt.Unix()
			resolvedAt = &ts
		}

		var lastReplyAt *int64
		if t.LastReplyAt != nil {
			ts := t.LastReplyAt.Unix()
			lastReplyAt = &ts
		}

		items[i] = FeedbackTicketResponse{
			ID:          t.ID,
			FeedbackID:  t.FeedbackID,
			UDID:        t.UDID,
			UserID:      t.UserID,
			Email:       t.Email,
			Content:     t.Content,
			Status:      t.Status,
			ResolvedBy:  t.ResolvedBy,
			ResolvedAt:  resolvedAt,
			LastReplyAt: lastReplyAt,
			LastReplyBy: t.LastReplyBy,
			Meta:        t.Meta,
			CreatedAt:   t.CreatedAt.Unix(),
			LogCount:    logCount,
		}
	}

	ListWithData(c, items, pagination)
}

// api_admin_resolve_feedback_ticket 标记工单为 resolved
func api_admin_resolve_feedback_ticket(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid ticket id")
		return
	}

	var req ResolveFeedbackTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request: "+err.Error())
		return
	}

	now := time.Now()
	result := db.Get().Model(&FeedbackTicket{}).Where("id = ?", id).Updates(map[string]any{
		"status":      "resolved",
		"resolved_by": req.ResolvedBy,
		"resolved_at": now,
	})
	if result.Error != nil {
		log.Errorf(c, "failed to resolve feedback ticket: %v", result.Error)
		Error(c, ErrorSystemError, "failed to resolve feedback ticket")
		return
	}
	if result.RowsAffected == 0 {
		Error(c, ErrorNotFound, "ticket not found")
		return
	}

	log.Infof(c, "feedback ticket %d resolved by %s", id, req.ResolvedBy)
	SuccessEmpty(c)
	WriteAuditLog(c, "ticket_resolve", "ticket", fmt.Sprintf("%d", id), nil)
}

// api_admin_close_feedback_ticket 标记工单为 closed
func api_admin_close_feedback_ticket(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid ticket id")
		return
	}

	result := db.Get().Model(&FeedbackTicket{}).Where("id = ?", id).Update("status", "closed")
	if result.Error != nil {
		log.Errorf(c, "failed to close feedback ticket: %v", result.Error)
		Error(c, ErrorSystemError, "failed to close feedback ticket")
		return
	}
	if result.RowsAffected == 0 {
		Error(c, ErrorNotFound, "ticket not found")
		return
	}

	log.Infof(c, "feedback ticket %d closed", id)
	SuccessEmpty(c)
	WriteAuditLog(c, "ticket_close", "ticket", fmt.Sprintf("%d", id), nil)
}
