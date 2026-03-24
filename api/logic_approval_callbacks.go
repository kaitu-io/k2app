package center

import (
	"context"
	"encoding/json"
	"fmt"
)

// Placeholder approval callbacks — replaced as handlers are refactored in Tasks 6-8

func executeApprovalEDMCreateTask(ctx context.Context, params json.RawMessage) error {
	return fmt.Errorf("not implemented: edm_create_task")
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
