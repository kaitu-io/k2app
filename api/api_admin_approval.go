package center

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// GET /app/approvals
func api_admin_list_approvals(c *gin.Context) {
	user := ReqUser(c)
	if user == nil {
		Error(c, ErrorNotLogin, "unauthorized")
		return
	}

	pagination := PaginationFromRequest(c)
	query := db.Get().Model(&AdminApproval{})

	isAdmin := user.IsAdmin != nil && *user.IsAdmin
	if !isAdmin {
		query = query.Where("requestor_id = ?", user.ID)
	}

	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Count(&total)

	var approvals []AdminApproval
	query.Order("FIELD(status, 'pending') DESC, created_at DESC").
		Offset(pagination.Offset()).Limit(pagination.PageSize).
		Find(&approvals)

	pagination.Total = total
	ListWithData(c, approvals, pagination)
}

// GET /app/approvals/:id
func api_admin_get_approval(c *gin.Context) {
	user := ReqUser(c)
	if user == nil {
		Error(c, ErrorNotLogin, "unauthorized")
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	var approval AdminApproval
	if err := db.Get().First(&approval, id).Error; err != nil {
		Error(c, ErrorNotFound, "approval not found")
		return
	}

	isAdmin := user.IsAdmin != nil && *user.IsAdmin
	if !isAdmin && approval.RequestorID != user.ID {
		Error(c, ErrorForbidden, "permission denied")
		return
	}

	Success(c, &approval)
}

// POST /app/approvals/:id/approve
func api_admin_approve_approval(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	if err := ApproveApproval(c, id); err != nil {
		if strings.Contains(err.Error(), "conflict") {
			Error(c, ErrorConflict, err.Error())
			return
		}
		if strings.Contains(err.Error(), "cannot approve own") {
			Error(c, ErrorForbidden, err.Error())
			return
		}
		log.Errorf(c, "approve approval failed: %v", err)
		Error(c, ErrorSystemError, err.Error())
		return
	}

	SuccessEmpty(c)
}

// POST /app/approvals/:id/reject
func api_admin_reject_approval(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "reason is required")
		return
	}

	if err := RejectApproval(c, id, req.Reason); err != nil {
		if strings.Contains(err.Error(), "conflict") {
			Error(c, ErrorConflict, err.Error())
			return
		}
		if strings.Contains(err.Error(), "cannot reject own") {
			Error(c, ErrorForbidden, err.Error())
			return
		}
		log.Errorf(c, "reject approval failed: %v", err)
		Error(c, ErrorSystemError, err.Error())
		return
	}

	SuccessEmpty(c)
}

// POST /app/approvals/:id/cancel
func api_admin_cancel_approval(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	if err := CancelApproval(c, id); err != nil {
		if strings.Contains(err.Error(), "conflict") {
			Error(c, ErrorConflict, err.Error())
			return
		}
		if strings.Contains(err.Error(), "only requestor") {
			Error(c, ErrorForbidden, err.Error())
			return
		}
		log.Errorf(c, "cancel approval failed: %v", err)
		Error(c, ErrorSystemError, err.Error())
		return
	}

	SuccessEmpty(c)
}
