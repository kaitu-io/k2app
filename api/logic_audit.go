package center

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// WriteAuditLog 写入管理员审计日志
// detail 可以是任意可 JSON 序列化的值，nil 表示无详情
// NOTE: Uses c.Copy() for goroutine safety — Gin recycles contexts after handler returns.
func WriteAuditLog(c *gin.Context, action, targetType, targetID string, detail any) {
	actor := ReqUser(c)
	if actor == nil {
		log.Warnf(c, "audit log skipped: no authenticated user for action=%s", action)
		return
	}

	var detailStr string
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			detailStr = string(b)
		}
	}

	entry := AdminAuditLog{
		ActorID:    actor.ID,
		ActorUUID:  actor.UUID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Detail:     detailStr,
	}

	// 异步写入，不阻塞请求
	// IMPORTANT: c.Copy() is required — Gin recycles *gin.Context after handler returns.
	cc := c.Copy()
	go func() {
		if err := db.Get().Create(&entry).Error; err != nil {
			log.Errorf(cc, "failed to write audit log: action=%s target=%s/%s err=%v",
				action, targetType, targetID, err)
		}
	}()
}
