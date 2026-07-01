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
	warnThreshold70  = 70
	warnThreshold80  = 80
	warnThreshold90  = 90
	exhaustThreshold = 100
)

// pickTrafficTier 返回该用量应发的最高档位(0=无)。纯函数,便于测试。
func pickTrafficTier(used, total int64) int {
	if total <= 0 {
		return 0
	}
	percent := int(used * 100 / total)
	switch {
	case percent >= exhaustThreshold:
		return exhaustThreshold
	case percent >= warnThreshold90:
		return warnThreshold90
	case percent >= warnThreshold80:
		return warnThreshold80
	case percent >= warnThreshold70:
		return warnThreshold70
	default:
		return 0
	}
}

// sentEpochFor 返回某档位的去重列指针。
func sentEpochFor(u *NodeUsage, tier int) *int64 {
	switch tier {
	case exhaustThreshold:
		return &u.Exhausted100SentEpoch
	case warnThreshold90:
		return &u.Warn90SentEpoch
	case warnThreshold80:
		return &u.Warn80SentEpoch
	case warnThreshold70:
		return &u.Warn70SentEpoch
	}
	return nil
}

// handlePrivateNodeTrafficWarn 是 Asynq cron 任务处理器,转调扫描逻辑。
func handlePrivateNodeTrafficWarn(ctx context.Context, _ []byte) error {
	return runPrivateNodeTrafficWarning(ctx)
}

// runPrivateNodeTrafficWarning 扫 active 专属线路,跨 70/80/90/100% 阈值发预警,按 epoch 去重。
// 配额计数来自节点权威镜像 NodeUsage(按 sub.BoundIpv4);跨阈值发一次,去重键 = NodeUsage.Epoch
// (节点进入新计费周期 epoch 推进即重新允许发信)。同一轮取最高档位,只发一封。
func runPrivateNodeTrafficWarning(ctx context.Context) error {
	var subs []PrivateNodeSubscription
	if err := db.Get().Where("status = ? AND slave_node_id IS NOT NULL AND bound_ipv4 <> ''", PNStatusActive).
		Find(&subs).Error; err != nil {
		return fmt.Errorf("scan active subs: %w", err)
	}
	for i := range subs {
		sub := &subs[i]
		var u NodeUsage
		if err := db.Get().Where("ipv4 = ?", sub.BoundIpv4).First(&u).Error; err != nil {
			log.Warnf(ctx, "traffic-warn: sub %d node usage missing (ip=%s): %v", sub.ID, sub.BoundIpv4, err)
			continue
		}

		tier := pickTrafficTier(u.UsedBytes, u.QuotaTotalBytes)
		if tier == 0 {
			continue
		}
		marker := sentEpochFor(&u, tier)
		if marker == nil || *marker == u.Epoch {
			continue // 该档本 epoch 已发
		}
		fireTrafficWarn(ctx, sub, &u, tier)
		col := map[int]string{
			warnThreshold70:  "warn70_sent_epoch",
			warnThreshold80:  "warn80_sent_epoch",
			warnThreshold90:  "warn90_sent_epoch",
			exhaustThreshold: "exhausted100_sent_epoch",
		}[tier]
		if err := db.Get().Model(&NodeUsage{}).Where("ipv4 = ?", u.Ipv4).
			Update(col, u.Epoch).Error; err != nil {
			log.Errorf(ctx, "traffic-warn: persist %s for ip %s failed: %v", col, u.Ipv4, err)
		}
	}
	return nil
}

func fireTrafficWarn(ctx context.Context, sub *PrivateNodeSubscription, u *NodeUsage, percent int) {
	if sendWarnHook != nil {
		sendWarnHook(percent, sub.UserID)
		return
	}
	meta := PrivateNodeTrafficWarnMeta{
		Percent:   percent,
		UsedGB:    formatGB(u.UsedBytes),
		TotalGB:   formatGB(u.QuotaTotalBytes),
		Region:    sub.Region,
		ResetDate: formatResetDate(u.Epoch),
	}
	tmpl := privateNodeTrafficWarnTemplate
	if percent >= exhaustThreshold {
		tmpl = privateNodeTrafficExhaustedTemplate
	}
	if err := emailToUser(ctx, int64(sub.UserID), tmpl, meta); err != nil {
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
