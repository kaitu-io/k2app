// Package center 提供中心服务 API
//
package center

import (
	"fmt"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

func api_admin_list_plans(c *gin.Context) {
	log.Infof(c, "admin request to list plans")
	pagination := PaginationFromRequest(c)

	var plans []Plan
	query := db.Get().Model(&Plan{})

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count plans: %v", err)
		Error(c, ErrorSystemError, "failed to count plans")
		return
	}

	if err := query.Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&plans).Error; err != nil {
		log.Errorf(c, "failed to get plans: %v", err)
		Error(c, ErrorSystemError, "failed to get plans")
		return
	}
	log.Infof(c, "successfully listed %d plans", len(plans))
	List(c, plans, pagination)
}

// AdminCreatePlanRequest 创建套餐请求结构体
//
type AdminCreatePlanRequest struct {
	PID         string `json:"pid" binding:"required" example:"pro_monthly"`  // 套餐标识符
	Label       string `json:"label" binding:"required" example:"Pro 月付套餐"`   // 套餐名称
	Price       uint64 `json:"price" binding:"required" example:"999"`        // 价格（美分）
	OriginPrice uint64 `json:"originPrice" binding:"required" example:"1299"` // 原价（美分）
	Month       int    `json:"month" binding:"required" example:"1"`          // 月数
	Highlight   bool   `json:"highlight" example:"true"`                      // 是否高亮显示
	IsActive    bool   `json:"isActive" example:"true"`                       // 是否激活
}

func api_admin_create_plan(c *gin.Context) {
	log.Infof(c, "admin request to create plan")

	var req AdminCreatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request to create plan: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	log.Debugf(c, "create plan request: %+v", req)

	err := db.Get().Transaction(func(tx *gorm.DB) error {
		// 检查 PID 是否已存在

		plan := Plan{
			PID:         req.PID,
			Label:       req.Label,
			Price:       req.Price,
			OriginPrice: req.OriginPrice,
			Month:       req.Month,
			Highlight:   BoolPtr(req.Highlight),
			IsActive:    BoolPtr(req.IsActive),
		}

		if err := tx.Create(&plan).Error; err != nil {
			log.Errorf(c, "failed to create plan: %v", err)
			return err
		}
		log.Infof(c, "successfully created plan %s", plan.PID)
		return nil
	})
	if err != nil {
		Error(c, ErrorSystemError, "failed to create plan")
		return
	}
	// 查询返回最新plan
	var plan Plan
	db.Get().Where("pid = ?", req.PID).First(&plan)
	Success[Plan](c, &plan)
	WriteAuditLog(c, "plan_create", "plan", fmt.Sprintf("%d", plan.ID), nil)
}

// AdminUpdatePlanRequest 更新套餐请求结构体
//
type AdminUpdatePlanRequest struct {
	Label       *string `json:"label" example:"Pro 月付套餐"`   // 套餐名称
	Price       *uint64 `json:"price" example:"999"`        // 价格（美分）
	OriginPrice *uint64 `json:"originPrice" example:"1299"` // 原价（美分）
	Month       *int    `json:"month" example:"1"`          // 月数
	Highlight   *bool   `json:"highlight" example:"true"`   // 是否高亮显示
	IsActive    *bool   `json:"isActive" example:"true"`    // 是否激活
}

func api_admin_update_plan(c *gin.Context) {
	planID := c.Param("id")
	if planID == "" {
		Error(c, ErrorNotFound, "plan id is required")
		return
	}
	log.Infof(c, "admin request to update plan %s", planID)

	var req AdminUpdatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request to update plan %s: %v", planID, err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	log.Debugf(c, "update request for plan %s with data: %+v", planID, req)

	approvalID, err := SubmitApproval(c, "plan_update", planUpdateApprovalParams{
		PlanID:  planID,
		Request: req,
	}, fmt.Sprintf("修改套餐 (ID:%s)", planID))
	if err != nil {
		log.Errorf(c, "提交修改套餐审批失败: %v", err)
		Error(c, ErrorSystemError, "submit approval failed")
		return
	}

	Success(c, &ApprovalSubmitResponse{
		ApprovalID: approvalID,
		Status:     "pending_approval",
	})
}

func api_admin_delete_plan(c *gin.Context) {
	planID := c.Param("id")
	if planID == "" {
		Error(c, ErrorNotFound, "plan id is required")
		return
	}
	log.Infof(c, "admin request to delete plan %s", planID)

	approvalID, err := SubmitApproval(c, "plan_delete", struct {
		PlanID string `json:"planId"`
	}{PlanID: planID}, fmt.Sprintf("删除套餐 (ID:%s)", planID))
	if err != nil {
		log.Errorf(c, "提交删除套餐审批失败: %v", err)
		Error(c, ErrorSystemError, "submit approval failed")
		return
	}

	Success(c, &ApprovalSubmitResponse{
		ApprovalID: approvalID,
		Status:     "pending_approval",
	})
}

func api_admin_restore_plan(c *gin.Context) {
	planID := c.Param("id")
	if planID == "" {
		Error(c, ErrorNotFound, "plan id is required")
		return
	}
	log.Infof(c, "admin request to restore plan %s", planID)
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		var plan Plan
		if err := tx.First(&plan, planID).Error; err != nil {
			log.Errorf(c, "failed to find plan %s for restore: %v", planID, err)
			return err
		}
		if err := tx.Model(&plan).Update("is_active", true).Error; err != nil {
			log.Errorf(c, "failed to restore plan %s: %v", planID, err)
			return err
		}
		log.Infof(c, "successfully restored plan %s", plan.PID)
		return nil
	})
	if err != nil {
		Error(c, ErrorSystemError, "failed to restore plan")
		return
	}
	var plan Plan
	db.Get().First(&plan, planID)
	Success[Plan](c, &plan)
	WriteAuditLog(c, "plan_restore", "plan", planID, nil)
}
