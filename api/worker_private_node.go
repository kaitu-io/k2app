package center

import (
	"context"
	"fmt"
	"time"

	hibikenAsynq "github.com/hibiken/asynq"
	asynq "github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

const TaskTypeProvisionPrivateNode = "private_node:provision"

const TaskTypeProvisionTimeoutSweep = "private_node:provision_timeout"

// provisionTimeoutSeconds：provisioning 状态超过此秒数仍未被节点自注册激活，判定开通失败。
// agent 驱动流程整链耗时较长（建实例 + SSH + provision-node.sh 装 Docker + 拉镜像 +
// compose up + 启动 + 自注册），给 30 分钟容忍窗口，避免误杀慢但正常的开通。
const provisionTimeoutSeconds int64 = 30 * 60

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
	// Center 不再直接建机：只写 NodeProvisionJob(queued) 队列行，交外部 AI agent 认领。
	if err := emitNodeProvisionJob(ctx, &sub, spec); err != nil {
		if isLastAttempt(ctx) {
			return markProvisionFailed(ctx, &sub, err)
		}
		return err
	}
	return nil
}

// handleProvisionTimeoutSweep 把卡在 provisioning 超时的订阅置 failed（节点始终未到场）。
func handleProvisionTimeoutSweep(ctx context.Context, payload []byte) error {
	cutoff := time.Now().Unix() - provisionTimeoutSeconds
	var stale []PrivateNodeSubscription
	if err := db.Get().Where("status = ? AND updated_at < ?", PNStatusProvisioning, cutoff).Find(&stale).Error; err != nil {
		return fmt.Errorf("query stale provisioning subs: %w", err)
	}
	for i := range stale {
		s := &stale[i]
		if err := db.Get().Model(&PrivateNodeSubscription{}).Where("id = ? AND status = ?", s.ID, PNStatusProvisioning).
			Updates(map[string]any{"status": PNStatusFailed, "last_provision_error": "provisioning timed out: node never self-registered"}).Error; err != nil {
			log.Errorf(ctx, "timeout sweep: mark failed sub=%d: %v", s.ID, err)
			continue
		}
		// 孤儿 job 同步置 failed：否则它仍停在 queued/claimed/provisioning，agent 会
		// 认领一个对应 sub 已失败的 job、白白建一台 VPS。best-effort，不阻断清扫。
		if err := db.Get().Model(&NodeProvisionJob{}).
			Where("sub_id = ? AND status NOT IN ?", s.ID, []string{NPJStatusSucceeded, NPJStatusFailed}).
			Updates(map[string]any{"status": NPJStatusFailed, "last_error": "subscription provisioning timed out"}).Error; err != nil {
			log.Errorf(ctx, "timeout sweep: fail orphan job sub=%d: %v", s.ID, err)
		}
		sendCloudSlackNotification(ctx, "Private Node Provision Timeout",
			fmt.Sprintf("sub=%d order=%d stuck in provisioning > %ds → failed", s.ID, s.OrderID, provisionTimeoutSeconds))
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
