package center

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
)

// Placeholder approval callbacks — replaced as handlers are refactored in Tasks 6-8

func executeApprovalEDMCreateTask(ctx context.Context, params json.RawMessage) error {
	var req CreateEDMTaskRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	// Re-validate: template still active?
	var template EmailMarketingTemplate
	if err := db.Get().Where("id = ? AND is_active = ?", req.TemplateID, true).
		First(&template).Error; err != nil {
		return fmt.Errorf("template %d no longer active or not found", req.TemplateID)
	}

	var scheduledAt *time.Time
	if req.ScheduledAt != nil {
		t := time.Unix(*req.ScheduledAt, 0)
		scheduledAt = &t
	}

	_, err := EnqueueEDMTask(ctx, req.TemplateID, req.UserFilters, scheduledAt)
	return err
}

func executeApprovalCampaignCreate(ctx context.Context, params json.RawMessage) error {
	var req CampaignRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	// Re-validate code uniqueness
	var existing Campaign
	if err := db.Get().Where(&Campaign{Code: req.Code}).First(&existing).Error; err == nil {
		return fmt.Errorf("campaign code already exists: %s", req.Code)
	}

	campaign := Campaign{
		Code:          req.Code,
		Name:          req.Name,
		Type:          req.Type,
		Value:         req.Value,
		StartAt:       req.StartAt,
		EndAt:         req.EndAt,
		Description:   req.Description,
		IsActive:      BoolPtr(req.IsActive),
		MatcherType:   req.MatcherType,
		MatcherParams: req.MatcherParams,
		IsShareable:   req.IsShareable,
		SharesPerUser: req.SharesPerUser,
		MaxUsage:      req.MaxUsage,
	}

	if err := db.Get().Create(&campaign).Error; err != nil {
		return fmt.Errorf("create campaign: %w", err)
	}
	return nil
}

// campaignUpdateApprovalParams wraps both path ID and request body for campaign update approval.
type campaignUpdateApprovalParams struct {
	CampaignID uint64          `json:"campaignId"`
	Request    CampaignRequest `json:"request"`
}

func executeApprovalCampaignUpdate(ctx context.Context, params json.RawMessage) error {
	var p campaignUpdateApprovalParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	// Re-validate campaign exists
	var campaign Campaign
	if err := db.Get().Where(&Campaign{ID: p.CampaignID}).First(&campaign).Error; err != nil {
		return fmt.Errorf("campaign %d not found", p.CampaignID)
	}

	req := p.Request
	campaign.Code = req.Code
	campaign.Name = req.Name
	campaign.Type = req.Type
	campaign.Value = req.Value
	campaign.StartAt = req.StartAt
	campaign.EndAt = req.EndAt
	campaign.Description = req.Description
	campaign.IsActive = BoolPtr(req.IsActive)
	campaign.MatcherType = req.MatcherType
	campaign.MatcherParams = req.MatcherParams
	campaign.IsShareable = req.IsShareable
	campaign.SharesPerUser = req.SharesPerUser
	campaign.MaxUsage = req.MaxUsage

	if err := db.Get().Save(&campaign).Error; err != nil {
		return fmt.Errorf("update campaign: %w", err)
	}
	return nil
}

func executeApprovalCampaignDelete(ctx context.Context, params json.RawMessage) error {
	var p struct {
		CampaignID uint64 `json:"campaignId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	// Re-validate campaign exists
	var campaign Campaign
	if err := db.Get().Where(&Campaign{ID: p.CampaignID}).First(&campaign).Error; err != nil {
		return fmt.Errorf("campaign %d not found", p.CampaignID)
	}

	if err := db.Get().Delete(&campaign).Error; err != nil {
		return fmt.Errorf("delete campaign: %w", err)
	}
	return nil
}

func executeApprovalCampaignIssueKeys(ctx context.Context, params json.RawMessage) error {
	var p struct {
		CampaignID uint64 `json:"campaignId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	// Re-validate campaign exists and is shareable
	var campaign Campaign
	if err := db.Get().First(&campaign, p.CampaignID).Error; err != nil {
		return fmt.Errorf("campaign %d not found", p.CampaignID)
	}
	if !campaign.IsShareable {
		return fmt.Errorf("campaign %d is not shareable", p.CampaignID)
	}

	_, err := GenerateLicenseKeysForCampaign(ctx, &campaign)
	if err != nil {
		return fmt.Errorf("generate license keys: %w", err)
	}

	// Send gift emails — best-effort, keys already generated
	_ = SendLicenseKeyEmails(ctx, campaign.ID)
	return nil
}

// ===================== User Hard Delete =====================

func executeApprovalUserHardDelete(ctx context.Context, params json.RawMessage) error {
	var req HardDeleteUsersRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	// Re-validate users exist
	var users []User
	if err := db.Get().Where("uuid IN ?", req.UserUUIDs).Find(&users).Error; err != nil {
		return fmt.Errorf("query users: %w", err)
	}
	if len(users) == 0 {
		return fmt.Errorf("no users found for UUIDs")
	}

	userIDs := make([]uint64, len(users))
	for i, user := range users {
		userIDs[i] = user.ID
	}

	tx := db.Get().Begin()
	if tx.Error != nil {
		return fmt.Errorf("begin transaction: %w", tx.Error)
	}

	// 1. 删除登录标识
	if err := tx.Where("user_id IN ?", userIDs).Delete(&LoginIdentify{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("delete login identifies: %w", err)
	}

	// 2. 删除设备
	if err := tx.Where("user_id IN ?", userIDs).Delete(&Device{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("delete devices: %w", err)
	}

	// 3. 删除订单
	if err := tx.Where("user_id IN ?", userIDs).Delete(&Order{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("delete orders: %w", err)
	}

	// 4. 删除邀请码
	if err := tx.Where("user_id IN ?", userIDs).Delete(&InviteCode{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("delete invite codes: %w", err)
	}

	// 5. 删除Pro历史记录
	if err := tx.Where("user_id IN ?", userIDs).Delete(&UserProHistory{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("delete pro histories: %w", err)
	}

	// 6. 删除分销商配置
	if err := tx.Where("user_id IN ?", userIDs).Delete(&RetailerConfig{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("delete retailer configs: %w", err)
	}

	// 6.1 删除消息记录
	if err := tx.Where("user_id IN ?", userIDs).Delete(&Message{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("delete messages: %w", err)
	}

	// 6.2 删除会话记录
	if err := tx.Where("user_id IN ?", userIDs).Delete(&SessionAcct{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("delete session records: %w", err)
	}

	// 7. 查询所有钱包ID
	var wallets []Wallet
	if err := tx.Where("user_id IN ?", userIDs).Find(&wallets).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("query wallets: %w", err)
	}

	if len(wallets) > 0 {
		walletIDs := make([]uint64, len(wallets))
		for i, wallet := range wallets {
			walletIDs[i] = wallet.ID
		}

		// 8. 删除钱包变更记录
		if err := tx.Where("wallet_id IN ?", walletIDs).Delete(&WalletChange{}).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("delete wallet changes: %w", err)
		}

		// 9. 删除提现请求
		if err := tx.Where("wallet_id IN ?", walletIDs).Delete(&Withdraw{}).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("delete withdraw requests: %w", err)
		}

		// 10. 删除钱包
		if err := tx.Where("id IN ?", walletIDs).Delete(&Wallet{}).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("delete wallets: %w", err)
		}
	}

	// 11. 删除提现账户
	if err := tx.Where("user_id IN ?", userIDs).Delete(&WithdrawAccount{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("delete withdraw accounts: %w", err)
	}

	// 12. 删除邮件发送日志
	if err := tx.Where("user_id IN ?", userIDs).Delete(&EmailSendLog{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("delete email send logs: %w", err)
	}

	// 13. 最后删除用户本身
	if err := tx.Where("id IN ?", userIDs).Delete(&User{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("delete users: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// ===================== Plan Update =====================

// planUpdateApprovalParams wraps both path ID and request body for plan update approval.
type planUpdateApprovalParams struct {
	PlanID  string                 `json:"planId"`
	Request AdminUpdatePlanRequest `json:"request"`
}

func executeApprovalPlanUpdate(ctx context.Context, params json.RawMessage) error {
	var p planUpdateApprovalParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	// Re-validate plan exists
	var plan Plan
	if err := db.Get().First(&plan, p.PlanID).Error; err != nil {
		return fmt.Errorf("plan %s not found: %w", p.PlanID, err)
	}

	req := p.Request
	if req.Label != nil {
		plan.Label = *req.Label
	}
	if req.Price != nil {
		plan.Price = *req.Price
	}
	if req.OriginPrice != nil {
		plan.OriginPrice = *req.OriginPrice
	}
	if req.Month != nil {
		plan.Month = *req.Month
	}
	if req.Highlight != nil {
		plan.Highlight = req.Highlight
	}
	if req.IsActive != nil {
		plan.IsActive = req.IsActive
	}

	if err := db.Get().Save(&plan).Error; err != nil {
		return fmt.Errorf("update plan %s: %w", p.PlanID, err)
	}
	return nil
}

// ===================== Plan Delete =====================

func executeApprovalPlanDelete(ctx context.Context, params json.RawMessage) error {
	var p struct {
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	// Re-validate plan exists
	var plan Plan
	if err := db.Get().First(&plan, p.PlanID).Error; err != nil {
		return fmt.Errorf("plan %s not found: %w", p.PlanID, err)
	}

	if err := db.Get().Model(&plan).Update("is_active", false).Error; err != nil {
		return fmt.Errorf("delete plan %s: %w", p.PlanID, err)
	}
	return nil
}

// ===================== Withdraw Approve =====================

type withdrawApproveApprovalParams struct {
	WithdrawID  uint64 `json:"withdrawId"`
	Action      string `json:"action"`      // "approve" or "reject"
	Remark      string `json:"remark"`
	ProcessedBy uint64 `json:"processedBy"` // admin user ID
}

func executeApprovalWithdrawApprove(ctx context.Context, params json.RawMessage) error {
	var p withdrawApproveApprovalParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	var withdraw Withdraw
	if err := db.Get().First(&withdraw, p.WithdrawID).Error; err != nil {
		return fmt.Errorf("withdraw %d not found: %w", p.WithdrawID, err)
	}

	if withdraw.Status != WithdrawStatusPending {
		return fmt.Errorf("withdraw %d status is %s, expected pending", p.WithdrawID, withdraw.Status)
	}

	if p.Action == "reject" {
		withdraw.Status = WithdrawStatusRejected
		withdraw.RejectReason = p.Remark
		withdraw.ProcessedBy = &p.ProcessedBy
		now := time.Now()
		withdraw.ProcessedAt = &now
		return db.Get().Save(&withdraw).Error
	}

	// approve: keep pending, just record remark
	withdraw.Remark = p.Remark
	return db.Get().Save(&withdraw).Error
}

// ===================== Withdraw Complete =====================

type withdrawCompleteApprovalParams struct {
	WithdrawID  uint64 `json:"withdrawId"`
	TxHash      string `json:"txHash"`
	Remark      string `json:"remark"`
	ProcessedBy uint64 `json:"processedBy"` // admin user ID
}

func executeApprovalWithdrawComplete(ctx context.Context, params json.RawMessage) error {
	var p withdrawCompleteApprovalParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	var withdraw Withdraw
	if err := db.Get().First(&withdraw, p.WithdrawID).Error; err != nil {
		return fmt.Errorf("withdraw %d not found: %w", p.WithdrawID, err)
	}

	if withdraw.Status != WithdrawStatusPending {
		return fmt.Errorf("withdraw %d status is %s, expected pending", p.WithdrawID, withdraw.Status)
	}

	now := time.Now()
	withdraw.ProcessedAt = &now
	withdraw.ProcessedBy = &p.ProcessedBy
	withdraw.Status = WithdrawStatusCompleted
	withdraw.TxHash = p.TxHash
	if p.Remark != "" {
		withdraw.Remark = p.Remark
	}

	// 自动生成交易查看链接
	withdraw.TxExplorerURL = withdraw.AccountType.GetTxExplorerURL(p.TxHash)

	return db.Get().Save(&withdraw).Error
}
