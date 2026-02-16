package center

import (
	"context"
	"time"

	"github.com/wordgate/qtoolkit/asynq"
	"github.com/wordgate/qtoolkit/log"
)

// 任务类型常量
const (
	TaskTypeECHKeyRotation = "ech:key_rotation"
)

// ECH 密钥轮换调度间隔（24小时检查一次，与 grace period 匹配）
const echRotationCheckInterval = 24 * time.Hour

// RegisterECHWorker 注册 ECH 相关的 worker
// 在 InitWorker 中调用
func RegisterECHWorker() {
	asynq.Handle(TaskTypeECHKeyRotation, handleECHKeyRotation)
	log.Infof(context.Background(), "[WORKER] ECH key rotation handler registered")
}

// handleECHKeyRotation 处理 ECH 密钥轮换任务
func handleECHKeyRotation(ctx context.Context, payload []byte) error {
	log.Infof(ctx, "[ECH] Starting key rotation task")

	if err := RotateECHKeys(ctx); err != nil {
		log.Errorf(ctx, "[ECH] Key rotation failed: %v", err)
		return err
	}

	log.Infof(ctx, "[ECH] Key rotation completed successfully")
	return nil
}

// EnqueueECHKeyRotation 入队 ECH 密钥轮换任务
// 可用于手动触发轮换或由外部调度器调用
func EnqueueECHKeyRotation(ctx context.Context) (string, error) {
	info, err := asynq.Enqueue(TaskTypeECHKeyRotation, nil)
	if err != nil {
		return "", err
	}
	log.Infof(ctx, "[ECH] Key rotation task enqueued: taskId=%s", info.ID)
	return info.ID, nil
}

// StartECHKeyRotationScheduler 启动 ECH 密钥轮换调度器
// 每24小时执行一次密钥轮换检查：
// - 将过期的 active 密钥转为 grace_period
// - 将超过 grace period 的密钥标记为 retired
// - 如果没有 active 密钥，生成新的
// - 清理过期的 retired 密钥
func StartECHKeyRotationScheduler(ctx context.Context) {
	log.Infof(ctx, "[ECH] Starting ECH key rotation scheduler (interval: 24 hours)")

	// 立即执行一次密钥轮换
	go func() {
		if err := RotateECHKeys(ctx); err != nil {
			log.Errorf(ctx, "[ECH] Initial key rotation failed: %v", err)
		}
	}()

	// 启动定时器
	ticker := time.NewTicker(echRotationCheckInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				log.Infof(ctx, "[ECH] Running scheduled key rotation")
				if err := RotateECHKeys(ctx); err != nil {
					log.Errorf(ctx, "[ECH] Scheduled key rotation failed: %v", err)
				}
			case <-ctx.Done():
				ticker.Stop()
				log.Infof(ctx, "[ECH] Stopping ECH key rotation scheduler")
				return
			}
		}
	}()
}
