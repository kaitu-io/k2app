package center

import (
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_get_user_private_nodes 返回当前用户拥有的专属节点订阅（只读视图）。
// 严格 owner 隔离：只查 user_id = 当前用户。流量已用量来自节点权威镜像 NodeUsage（按
// SlaveNodeID）；CloudInstance 仅用于展示 IP/Region。
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

		// 流量已用 + 耗尽：来自节点权威镜像 NodeUsage（按 SlaveNodeID）。
		if s.SlaveNodeID != nil {
			var u NodeUsage
			if err := db.Get().Where("node_id = ?", *s.SlaveNodeID).First(&u).Error; err == nil {
				d.TrafficUsedBytes = u.UsedBytes
				d.QuotaResetAt = u.Epoch
				d.QuotaExhausted = isNodeOverQuota(&u)
			}
		}
		// 节点连接信息（IP/Region）仍取自 CloudInstance（仅展示用）。
		if s.CloudInstanceID != nil {
			var ci CloudInstance
			if err := db.Get().Select("ip_address", "region").First(&ci, *s.CloudInstanceID).Error; err == nil {
				d.Node = &DataPrivateNodeNode{IP: ci.IPAddress, Region: ci.Region}
			}
		}

		items = append(items, d)
	}

	Success(c, &DataPrivateNodeList{Items: items})
}
