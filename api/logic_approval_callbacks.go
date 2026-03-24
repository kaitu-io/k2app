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

func executeApprovalUserHardDelete(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: user_hard_delete")
}

func executeApprovalPlanUpdate(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: plan_update")
}

func executeApprovalPlanDelete(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: plan_delete")
}

func executeApprovalWithdrawApprove(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: withdraw_approve")
}

func executeApprovalWithdrawComplete(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: withdraw_complete")
}
