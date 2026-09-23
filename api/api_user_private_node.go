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
		items = append(items, buildPrivateNodeSubDTO(c, &subs[i], now))
	}

	Success(c, &DataPrivateNodeList{Items: items})
}
