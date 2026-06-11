package center

import (
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_get_user_private_nodes 返回当前用户拥有的专属节点订阅（只读视图）。
// 严格 owner 隔离：只查 user_id = 当前用户。流量已用量来自绑定的 CloudInstance。
func api_get_user_private_nodes(c *gin.Context) {
	userID := ReqUserID(c)

	var subs []PrivateNodeSubscription
	if err := db.Get().Where(&PrivateNodeSubscription{UserID: userID}).
		Order("id DESC").Find(&subs).Error; err != nil {
		log.Errorf(c, "failed to load private node subs for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to load private nodes")
		return
	}

	now := time.Now().Unix()
	items := make([]DataPrivateNodeSubscription, 0, len(subs))
	for i := range subs {
		s := &subs[i]
		d := DataPrivateNodeSubscription{
			ID:                s.ID,
			Status:            s.Status,
			IsServiceable:     s.IsServiceable(now),
			Region:            s.Region,
			IPType:            s.IPType,
			TrafficTotalBytes: s.TrafficTotalBytes,
			PurchasedAt:       s.PurchasedAt,
			ExpiresAt:         s.ExpiresAt,
			GraceUntil:        s.GraceUntil,
			SuspendUntil:      s.SuspendUntil,
		}

		// 套餐标签（best-effort，缺失不阻断）
		var plan Plan
		if err := db.Get().Select("label").First(&plan, s.PlanID).Error; err == nil {
			d.PlanLabel = plan.Label
		} else {
			log.Warnf(c, "private sub %d references missing Plan %d: %v", s.ID, s.PlanID, err)
		}

		// 绑定实例 → 流量已用 + 节点连接信息（仅开通后存在）
		if s.CloudInstanceID != nil {
			var ci CloudInstance
			if err := db.Get().First(&ci, *s.CloudInstanceID).Error; err == nil {
				d.TrafficUsedBytes = ci.TrafficUsedBytes
				d.Node = &DataPrivateNodeNode{IP: ci.IPAddress, Region: ci.Region}
				d.QuotaResetAt = ci.TrafficResetAt
				// 同 slave_api_usage.go 的整数阈值（同包常量），避免漂移。
				if ci.TrafficTotalBytes > 0 &&
					ci.TrafficUsedBytes*trafficStopThresholdDen >= ci.TrafficTotalBytes*trafficStopThresholdNum {
					d.QuotaExhausted = true
				}
			} else {
				log.Warnf(c, "private sub %d references missing CloudInstance %d: %v", s.ID, *s.CloudInstanceID, err)
			}
		}

		items = append(items, d)
	}

	Success(c, &DataPrivateNodeList{Items: items})
}
