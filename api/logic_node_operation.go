package center

import (
	"context"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// StopParams / DestroyParams / ChangeIPParams 是对应 action 的 Params JSON。
type StopParams struct {
	Reason string `json:"reason"`
}
type DestroyParams struct {
	Reason string `json:"reason"`
}
type ChangeIPParams struct {
	TargetRegion string `json:"targetRegion,omitempty"`
	Reason       string `json:"reason"`
}

// hasOpenNodeOperation 报告 (subID, action) 是否已有未结运维任务。
// 调用方应在事务内、锁 sub 行后调用以防并发双插。
func hasOpenNodeOperation(tx *gorm.DB, subID uint64, action string) (bool, error) {
	var count int64
	err := tx.Model(&NodeOperation{}).
		Where("sub_id = ? AND action = ? AND status IN ?", subID, action, nodeOpOpenStatuses).
		Count(&count).Error
	return count > 0, err
}

// dispatchNodeOperation 幂等派发一条运维任务(供 cron/system 用,open 已存在则静默跳过)。
// 非 provision 动作且无 cloud instance → 跳过(无可操作对象)。事务内 FOR UPDATE 锁 sub
// 行,序列化并发派发(cron vs 管理员手动 create)。
func dispatchNodeOperation(ctx context.Context, subID uint64, cloudInstanceID *uint64, action, createdBy string, params any) error {
	if action != NodeOpProvision && cloudInstanceID == nil {
		log.Warnf(ctx, "skip %s op for sub=%d: no cloud instance to act on", action, subID)
		return nil
	}
	return db.Get().Transaction(func(tx *gorm.DB) error {
		var sub PrivateNodeSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&sub, subID).Error; err != nil {
			return err
		}
		open, err := hasOpenNodeOperation(tx, subID, action)
		if err != nil {
			return err
		}
		if open {
			log.Debugf(ctx, "open %s op already exists for sub=%d, skip dispatch", action, subID)
			return nil
		}
		op := &NodeOperation{
			Action: action, SubID: subID, CloudInstanceID: cloudInstanceID,
			Status: NodeOpQueued, CreatedBy: createdBy, Params: mustJSON(params),
		}
		if err := tx.Create(op).Error; err != nil {
			return err
		}
		log.Infof(ctx, "dispatched %s op=%d for sub=%d by %s", action, op.ID, subID, createdBy)
		return nil
	})
}

// cancelOpenNodeOperations 把指定 sub 集合下、指定动作的未结任务批量置 canceled。
// 用于续费回收:已续费的 sub 不该再被执行 stop/destroy。
func cancelOpenNodeOperations(tx *gorm.DB, subIDs []uint64, actions []string) error {
	if len(subIDs) == 0 {
		return nil
	}
	return tx.Model(&NodeOperation{}).
		Where("sub_id IN ? AND action IN ? AND status IN ?", subIDs, actions, nodeOpOpenStatuses).
		Updates(map[string]any{"status": NodeOpCanceled}).Error
}
