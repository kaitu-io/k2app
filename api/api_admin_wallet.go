package center

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// ==================== 提现请求管理 ====================

// api_admin_list_withdraw_requests 获取提现请求列表
//
func api_admin_list_withdraw_requests(c *gin.Context) {
	pagination := PaginationFromRequest(c)

	dbQuery := db.Get().Model(&Withdraw{})

	// 状态筛选
	if status := c.Query("status"); status != "" {
		dbQuery = dbQuery.Where(&Withdraw{Status: WithdrawStatus(status)})
	}

	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		log.Errorf(c, "统计提现请求失败: %v", err)
		Error(c, ErrorSystemError, "count withdraws failed")
		return
	}
	pagination.Total = total

	var withdraws []Withdraw
	if err := dbQuery.
		Preload("Wallet.User.LoginIdentifies").
		Preload("WithdrawAccount").
		Order("id DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&withdraws).Error; err != nil {
		log.Errorf(c, "查询提现请求失败: %v", err)
		Error(c, ErrorSystemError, "list withdraws failed")
		return
	}

	items := make([]AdminWithdrawListItem, len(withdraws))
	for i, w := range withdraws {
		var userEmail string
		var userUUID string
		if w.Wallet != nil && w.Wallet.User != nil {
			userUUID = w.Wallet.User.UUID
			for _, identify := range w.Wallet.User.LoginIdentifies {
				if identify.Type == "email" {
					decryptedValue, err := secretDecryptString(c, identify.EncryptedValue)
					if err == nil {
						userEmail = decryptedValue
					}
					break
				}
			}
		}

		var processedAt *int64
		if w.ProcessedAt != nil {
			t := w.ProcessedAt.Unix()
			processedAt = &t
		}

		item := AdminWithdrawListItem{
			ID:        w.ID,
			CreatedAt: w.CreatedAt.Unix(),
			User: ResourceUser{
				UUID:  userUUID,
				Email: userEmail,
			},
			Amount:    w.Amount,
			FeeAmount: w.FeeAmount,
			NetAmount: w.NetAmount,
			Status:    string(w.Status),
			Account: ResourceWithdrawAccount{
				AccountType: string(w.AccountType),
				AccountID:   w.AccountID,
				Currency:    string(w.Currency),
			},
			Remark:      w.Remark,
			ProcessedAt: processedAt,
		}

		// 设置交易信息（如果有）
		if w.TxHash != "" {
			item.Transaction = &ResourceTransaction{
				TxHash:      w.TxHash,
				ExplorerURL: w.TxExplorerURL,
			}
		}

		items[i] = item
	}

	ListWithData(c, items, pagination)
}

// AdminWithdrawApproveRequest 审批提现请求参数
type AdminWithdrawApproveRequest struct {
	Action string `json:"action" binding:"required"` // approve, reject
	Remark string `json:"remark"`                    // 备注
}

// api_admin_approve_withdraw 审批提现请求
//
func api_admin_approve_withdraw(c *gin.Context) {
	withdrawID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid withdraw id")
		return
	}

	var req AdminWithdrawApproveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	if req.Action != "approve" && req.Action != "reject" {
		Error(c, ErrorInvalidArgument, "action must be approve or reject")
		return
	}

	// 预校验 withdraw 存在且状态正确
	var withdraw Withdraw
	if err := db.Get().First(&withdraw, withdrawID).Error; err != nil {
		Error(c, ErrorNotFound, "withdraw not found")
		return
	}
	if withdraw.Status != WithdrawStatusPending {
		Error(c, ErrorInvalidArgument, "withdraw is not in pending status")
		return
	}

	summary := fmt.Sprintf("审批提现 #%d (通过)", withdrawID)
	if req.Action == "reject" {
		summary = fmt.Sprintf("审批提现 #%d (拒绝)", withdrawID)
	}

	approvalID, executed, err := SubmitApproval(c, "withdraw_approve", withdrawApproveApprovalParams{
		WithdrawID:  withdrawID,
		Action:      req.Action,
		Remark:      req.Remark,
		ProcessedBy: ReqUserID(c),
	}, summary)
	if err != nil {
		log.Errorf(c, "提交审批提现审批失败: %v", err)
		Error(c, ErrorSystemError, "submit approval failed")
		return
	}

	if !executed {
		PendingApproval(c, approvalID)
		return
	}
	SuccessEmpty(c)
}

// AdminWithdrawCompleteRequest 完成提现请求参数
type AdminWithdrawCompleteRequest struct {
	TxHash string `json:"txHash" binding:"required"` // 交易哈希
	Remark string `json:"remark"`                    // 备注
}

// api_admin_complete_withdraw 标记提现已完成
//
func api_admin_complete_withdraw(c *gin.Context) {
	withdrawID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid withdraw id")
		return
	}

	var req AdminWithdrawCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 预校验 withdraw 存在且状态正确
	var withdraw Withdraw
	if err := db.Get().First(&withdraw, withdrawID).Error; err != nil {
		Error(c, ErrorNotFound, "withdraw not found")
		return
	}
	if withdraw.Status != WithdrawStatusPending {
		Error(c, ErrorInvalidArgument, "withdraw is not in pending status")
		return
	}

	approvalID, executed, err := SubmitApproval(c, "withdraw_complete", withdrawCompleteApprovalParams{
		WithdrawID:  withdrawID,
		TxHash:      req.TxHash,
		Remark:      req.Remark,
		ProcessedBy: ReqUserID(c),
	}, fmt.Sprintf("完成提现 #%d, TxHash: %s", withdrawID, req.TxHash))
	if err != nil {
		log.Errorf(c, "提交完成提现审批失败: %v", err)
		Error(c, ErrorSystemError, "submit approval failed")
		return
	}

	if !executed {
		PendingApproval(c, approvalID)
		return
	}
	SuccessEmpty(c)
}

// ==================== 分销商等级管理 ====================

// AdminRetailerLevelUpdateRequest 更新分销商等级请求
type AdminRetailerLevelUpdateRequest struct {
	Level  int    `json:"level" binding:"required,min=1,max=4"` // 目标等级 1-4
	Reason string `json:"reason"`                               // 变更原因（可选）
}

// api_admin_update_retailer_config 更新用户的分销商等级配置
//
func api_admin_update_retailer_config(c *gin.Context) {
	uuid := c.Param("uuid")

	var req AdminRetailerLevelUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var user User
	if err := db.Get().Where(&User{UUID: uuid}).First(&user).Error; err != nil {
		log.Errorf(c, "查询用户失败: %v", err)
		Error(c, ErrorNotFound, "user not found")
		return
	}

	// 获取或创建 RetailerConfig（使用统一方法，自动从 config.yml 读取默认值）
	config, err := GetOrCreateRetailerConfig(c, user.ID)
	if err != nil {
		log.Errorf(c, "获取或创建分销商配置失败: %v", err)
		Error(c, ErrorSystemError, "get or create retailer config failed")
		return
	}

	// 获取当前管理员ID
	adminUser := ReqUser(c)
	var adminID *uint64
	if adminUser != nil {
		adminID = &adminUser.ID
	}

	// 使用等级升级函数更新（会记录历史）
	reason := req.Reason
	if reason == "" {
		reason = "admin_manual_adjustment"
	}
	if err := UpgradeRetailerLevel(c, config.ID, req.Level, reason, adminID); err != nil {
		log.Errorf(c, "更新分销商等级失败: %v", err)
		Error(c, ErrorSystemError, "update retailer level failed")
		return
	}

	Success(c, &gin.H{})
	WriteAuditLog(c, "user_update_retailer_config", "user", uuid, req)
}
