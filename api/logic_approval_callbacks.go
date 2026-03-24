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
	return fmt.Errorf("not implemented: campaign_create")
}

func executeApprovalCampaignUpdate(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: campaign_update")
}

func executeApprovalCampaignDelete(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: campaign_delete")
}

func executeApprovalCampaignIssueKeys(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: campaign_issue_keys")
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
