package center

import (
	"context"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// sendWarnHook 测试注入点:非 nil 时替代真实发信,记录 (percent,userID)。
var sendWarnHook func(percent int, userID uint64)

const (
	warnThreshold80 = 80
	warnThreshold95 = 95
)

// handlePrivateNodeTrafficWarn 是 Asynq cron 任务处理器,转调扫描逻辑。
func handlePrivateNodeTrafficWarn(ctx context.Context, _ []byte) error {
	return runPrivateNodeTrafficWarning(ctx)
}

// runPrivateNodeTrafficWarning 扫 active 专属线路,跨 80/95% 阈值发预警,按 epoch 去重。
// 每条 active 订阅绑定的 CloudInstance 持有配额计数;跨阈值发一次,去重键 = TrafficEpoch
// (重置 epoch +1 即重新允许发信)。同一轮 95% 优先于 80%,只发一封。
func runPrivateNodeTrafficWarning(ctx context.Context) error {
	var subs []PrivateNodeSubscription
	if err := db.Get().Where("status = ? AND cloud_instance_id IS NOT NULL", PNStatusActive).
		Find(&subs).Error; err != nil {
		return fmt.Errorf("scan active subs: %w", err)
	}
	for i := range subs {
		sub := &subs[i]
		var ci CloudInstance
		if err := db.Get().First(&ci, *sub.CloudInstanceID).Error; err != nil {
			log.Warnf(ctx, "traffic-warn: sub %d cloud instance missing: %v", sub.ID, err)
			continue
		}
		if ci.TrafficTotalBytes <= 0 {
			continue
		}
		percent := int(ci.TrafficUsedBytes * 100 / ci.TrafficTotalBytes)
		if percent >= warnThreshold95 && ci.Warn95SentEpoch != ci.TrafficEpoch {
			fireTrafficWarn(ctx, sub, &ci, warnThreshold95)
			if err := db.Get().Model(&CloudInstance{}).Where("id = ?", ci.ID).
				Update("warn95_sent_epoch", ci.TrafficEpoch).Error; err != nil {
				// Failure to persist the dedup marker means the next run re-sends;
				// log it so a stuck marker is visible rather than silently spamming.
				log.Errorf(ctx, "traffic-warn: persist warn95 epoch for instance %d failed: %v", ci.ID, err)
			}
			continue
		}
		if percent >= warnThreshold80 && percent < warnThreshold95 && ci.Warn80SentEpoch != ci.TrafficEpoch {
			fireTrafficWarn(ctx, sub, &ci, warnThreshold80)
			if err := db.Get().Model(&CloudInstance{}).Where("id = ?", ci.ID).
				Update("warn80_sent_epoch", ci.TrafficEpoch).Error; err != nil {
				log.Errorf(ctx, "traffic-warn: persist warn80 epoch for instance %d failed: %v", ci.ID, err)
			}
		}
	}
	return nil
}

func fireTrafficWarn(ctx context.Context, sub *PrivateNodeSubscription, ci *CloudInstance, percent int) {
	if sendWarnHook != nil {
		sendWarnHook(percent, sub.UserID)
		return
	}
	meta := PrivateNodeTrafficWarnMeta{
		Percent:   percent,
		UsedGB:    formatGB(ci.TrafficUsedBytes),
		TotalGB:   formatGB(ci.TrafficTotalBytes),
		Region:    sub.Region,
		ResetDate: formatResetDate(ci.TrafficResetAt),
	}
	if err := emailToUser(ctx, int64(sub.UserID), privateNodeTrafficWarnTemplate, meta); err != nil {
		log.Errorf(ctx, "traffic-warn: send to user %d failed: %v", sub.UserID, err)
	}
}

// formatGB 把字节格式化为 "X.XGB"。
func formatGB(b int64) string {
	return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
}

// formatResetDate 把 Unix 秒格式化为本地日期 "2006-01-02";0 时返回 "下个计费周期"。
func formatResetDate(ts int64) string {
	if ts <= 0 {
		return "下个计费周期"
	}
	return time.Unix(ts, 0).Format("2006-01-02")
}
