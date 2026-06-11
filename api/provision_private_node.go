package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/spf13/viper"
	asynq "github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// loadPrivateNodePlanSpec 按 PlanID 取开通参数。
func loadPrivateNodePlanSpec(tx *gorm.DB, planID uint64) (*PrivateNodePlanSpec, error) {
	var spec PrivateNodePlanSpec
	if err := tx.Where(&PrivateNodePlanSpec{PlanID: planID}).First(&spec).Error; err != nil {
		return nil, fmt.Errorf("private node plan spec not found for plan %d: %w", planID, err)
	}
	return &spec, nil
}

// firstAllowedRegion 取 spec.AllowedRegions(JSON 数组) 的首个；首版默认第一个，
// 购买时选 region 的 UI 在 Plan 5。空则返回空串。
func firstAllowedRegion(allowedRegionsJSON string) string {
	var regions []string
	_ = json.Unmarshal([]byte(allowedRegionsJSON), &regions)
	if len(regions) > 0 {
		return regions[0]
	}
	return ""
}

// createPrivateNodeSubscription 在订单事务内建一条 pending 订阅（独立时钟）。
// 幂等由 OrderID uniqueIndex 保证：重复 webhook 第二次 Create 撞唯一键 → 调用方忽略。
func createPrivateNodeSubscription(ctx context.Context, tx *gorm.DB, order *Order, plan *Plan, now int64) (*PrivateNodeSubscription, error) {
	spec, err := loadPrivateNodePlanSpec(tx, plan.ID)
	if err != nil {
		return nil, err
	}
	expiresAt := time.Unix(now, 0).AddDate(0, plan.Month, 0).Unix()
	// 优先用下单时选定的 region（持久化在 Order 上，跨越下单→支付回调的时间差）；
	// 为空时回退到允许列表首项（兼容本改动之前创建的订单）。
	region := order.PrivateNodeRegion
	if region == "" {
		region = firstAllowedRegion(spec.AllowedRegions)
	}
	sub := &PrivateNodeSubscription{
		UserID:              order.UserID,
		PlanID:              plan.ID,
		OrderID:             order.ID,
		Region:              region,
		IPType:              spec.IPType,
		TrafficTotalBytes:   spec.TrafficTotalBytes,
		Status:              PNStatusPending,
		PurchasedAt:         now,
		ExpiresAt:           expiresAt,
		ProvisionClaimToken: generateSecret(),
	}
	if err := tx.Create(sub).Error; err != nil {
		return nil, err
	}
	return sub, nil
}

// centerCallbackURL 返回 Center 自身的回调基址（节点自注册回传用）。
func centerCallbackURL() string {
	return "https://" + viper.GetString("server.domain")
}

// isDuplicateKeyErr 识别 MySQL 唯一键冲突（错误号 1062）。GORM 的 ErrDuplicatedKey 翻译
// 依赖 dialector 配置，不一定可靠，故对 *mysql.MySQLError 直接断言 Number==1062，
// 并保留字符串兜底以防驱动包装。
func isDuplicateKeyErr(err error) bool {
	var me *mysqlDriver.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1062
	}
	return strings.Contains(err.Error(), "1062") || strings.Contains(err.Error(), "Duplicate entry")
}

// emitNodeProvisionJob 写一条 NodeProvisionJob(queued) 队列行，交外部 AI agent 认领建机+部署。
// Center 不再直接 CreateInstance/cloud-init；激活仍由节点自注册带 claim 驱动（Plan 4）。
// 状态停在 provisioning（NOT active）——node 自注册回传 claim 后才转 active。
func emitNodeProvisionJob(ctx context.Context, sub *PrivateNodeSubscription, spec *PrivateNodePlanSpec) error {
	// 1. 原子门控：允许从 pending 或 provisioning 进入 provisioning。
	// 接纳 provisioning 是为了重试幂等：job 已写但后续步骤失败 → Asynq 重试时
	// 状态已是 provisioning，必须能再次进入恢复，否则 sub 永久卡死。
	// RowsAffected==0 说明 sub 已到达终态/其它状态（active/failed/...）→ 真正完成或放弃，直接返回。
	// 注意：DSN 未设 clientFoundRows，MySQL 按"实际改动行"计数 RowsAffected——
	// provisioning→provisioning 是同值写、改动 0 行。故先排除 sub 已被推进到非门控状态，
	// 再用 UPDATE 仅锁定 pending→provisioning 的并发抢占（RowsAffected==0 在 pending 时才表示被抢）。
	res := db.Get().Model(&PrivateNodeSubscription{}).
		Where("id = ? AND status IN ?", sub.ID, []string{PNStatusPending, PNStatusProvisioning}).
		Update("status", PNStatusProvisioning)
	if res.Error != nil {
		return res.Error
	}
	var gated PrivateNodeSubscription
	if err := db.Get().Select("id", "status").First(&gated, sub.ID).Error; err != nil {
		return fmt.Errorf("reload sub for gate: %w", err)
	}
	if gated.Status != PNStatusProvisioning {
		log.Debugf(ctx, "private node sub=%d in %s (not pending/provisioning), skip emit", sub.ID, gated.Status)
		return nil
	}

	// 2. 幂等写入 job 行（SubID uniqueIndex）。
	job := &NodeProvisionJob{
		SubID: sub.ID, Status: NPJStatusQueued,
		Region: sub.Region, BundleID: spec.BundleID, ImageID: spec.ImageID,
		ComposeVariant: "private", TrafficTotalBytes: sub.TrafficTotalBytes,
		IPType: sub.IPType, Domain: "",
		// K2Version left empty for now (pinned at deploy spec maturity)
	}
	if err := db.Get().Create(job).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || isDuplicateKeyErr(err) {
			log.Debugf(ctx, "node provision job for sub=%d already exists, idempotent skip", sub.ID)
			return nil
		}
		return fmt.Errorf("create node provision job: %w", err)
	}
	log.Infof(ctx, "emitted node provision job=%d for sub=%d (queued for agent)", job.ID, sub.ID)
	return nil
}

// enqueueProvision 入队开通任务。MaxRetry(3)：spec §7.5 要求重试 3 次（Asynq 默认 25）。
func enqueueProvision(ctx context.Context, subID uint64) error {
	_, err := asynq.Enqueue(TaskTypeProvisionPrivateNode, ProvisionPayload{SubID: subID}, asynq.MaxRetry(3))
	return err
}
