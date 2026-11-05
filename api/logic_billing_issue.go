package center

import (
	"context"

	"gorm.io/gorm"
)

// isBillingIssueStatus 订阅处于扣费失败状态（宽限期 / 计费重试期）。
func isBillingIssueStatus(status string) bool {
	return status == "grace" || status == "billing_retry"
}

// backfillBillingIssueSince 给上线前已在 grace/billing_retry 却没有 billing_issue_since 的
// Apple 订阅补上时间戳（只补 0，幂等）。返回回填行数。
func backfillBillingIssueSince(ctx context.Context, database *gorm.DB, now int64) (int64, error) {
	res := database.WithContext(ctx).Model(&Subscription{}).
		Where("provider = ? AND status IN ? AND billing_issue_since = ?",
			SubscriptionProviderApple, []string{"grace", "billing_retry"}, 0).
		Update("billing_issue_since", now)
	return res.RowsAffected, res.Error
}
