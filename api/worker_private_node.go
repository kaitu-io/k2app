package center

import (
	"context"
	"fmt"

	hibikenAsynq "github.com/hibiken/asynq"
	asynq "github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"

	"github.com/kaitu-io/k2app/api/cloudprovider"
)

const TaskTypeProvisionPrivateNode = "private_node:provision"

type ProvisionPayload struct {
	SubID uint64 `json:"subId"`
}

func handleProvisionPrivateNode(ctx context.Context, payload []byte) error {
	var p ProvisionPayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal provision payload: %w", err)
	}
	var sub PrivateNodeSubscription
	if err := db.Get().First(&sub, p.SubID).Error; err != nil {
		return fmt.Errorf("load sub %d: %w", p.SubID, err)
	}
	// 递增尝试计数（含重试），让 ops 能在卡住的 sub 上看到尝试次数。
	db.Get().Model(&PrivateNodeSubscription{}).Where("id = ?", p.SubID).
		UpdateColumn("provision_attempts", gorm.Expr("provision_attempts + 1"))
	spec, err := loadPrivateNodePlanSpec(db.Get(), sub.PlanID)
	if err != nil {
		return markProvisionFailed(ctx, &sub, err)
	}
	account := ConfigCloudInstanceAccountByName(spec.Provider)
	if account == nil {
		return markProvisionFailed(ctx, &sub, fmt.Errorf("cloud account not found for provider %s", spec.Provider))
	}
	provider, err := cloudprovider.NewProvider(accountToProviderConfig(account))
	if err != nil {
		return markProvisionFailed(ctx, &sub, fmt.Errorf("build provider: %w", err))
	}
	if err := provisionPrivateNode(ctx, &sub, spec, *account, provider); err != nil {
		if isLastAttempt(ctx) {
			return markProvisionFailed(ctx, &sub, err)
		}
		return err
	}
	return nil
}

func isLastAttempt(ctx context.Context) bool {
	n, ok1 := hibikenAsynq.GetRetryCount(ctx)
	maxN, ok2 := hibikenAsynq.GetMaxRetry(ctx)
	// fail-safe：拿不到重试元数据时当作最后一次尝试，宁可快速 markProvisionFailed，
	// 也不要无限重试卡死。
	if !ok1 || !ok2 {
		return true
	}
	return n >= maxN
}

func markProvisionFailed(ctx context.Context, sub *PrivateNodeSubscription, cause error) error {
	log.Errorf(ctx, "private node provision failed sub=%d: %v", sub.ID, cause)
	if err := db.Get().Model(&PrivateNodeSubscription{}).Where("id = ?", sub.ID).
		Updates(map[string]any{"status": PNStatusFailed, "last_provision_error": cause.Error()}).Error; err != nil {
		log.Errorf(ctx, "mark provision failed: DB write sub=%d: %v", sub.ID, err)
	}
	sendCloudSlackNotification(ctx, "Private Node Provision Failed",
		fmt.Sprintf("sub=%d order=%d: %v", sub.ID, sub.OrderID, cause))
	return nil
}
