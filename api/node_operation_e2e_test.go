package center

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 全链路:期满进 grace(不派活)→ 宽限结束派 stop → 停机结束派 destroy。单步推进。
func TestNodeOperation_FullLifecycle(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ctx := context.Background()
	now := time.Now().Unix()
	ciID := uint64(999200)
	const orderID = uint64(9_600_201)
	db.Get().Unscoped().Where("order_id = ?", orderID).Delete(&PrivateNodeSubscription{})
	sub := &PrivateNodeSubscription{
		UserID: 990050, PlanID: 1, OrderID: orderID, Status: PNStatusActive,
		ExpiresAt: now - 1, CloudInstanceID: &ciID, // 刚过期
		Region: "ap-northeast-1", IPType: IPTypeNonResidential, TrafficTotalBytes: 1,
	}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() {
		db.Get().Where("sub_id = ?", sub.ID).Delete(&NodeOperation{})
		db.Get().Unscoped().Where("order_id = ?", orderID).Delete(&PrivateNodeSubscription{})
	})

	// 第一次扫描:active → grace(不派任何运维任务)。
	require.NoError(t, handlePrivateNodeLifecycleSweep(ctx, nil))
	var s1 PrivateNodeSubscription
	require.NoError(t, db.Get().First(&s1, sub.ID).Error)
	assert.Equal(t, PNStatusGrace, s1.Status)
	var stopCount0 int64
	require.NoError(t, db.Get().Model(&NodeOperation{}).Where("sub_id = ? AND action = ?", sub.ID, NodeOpStop).Count(&stopCount0).Error)
	assert.Equal(t, int64(0), stopCount0, "no stop op dispatched on active->grace")

	// 推到宽限期外,第二次扫描:grace → suspended + 派 stop。
	require.NoError(t, db.Get().Model(&PrivateNodeSubscription{}).Where("id = ?", sub.ID).
		Update("expires_at", now-privateNodeGraceSeconds-10).Error)
	require.NoError(t, handlePrivateNodeLifecycleSweep(ctx, nil))
	var s2 PrivateNodeSubscription
	require.NoError(t, db.Get().First(&s2, sub.ID).Error)
	assert.Equal(t, PNStatusSuspended, s2.Status)
	var stopCount int64
	require.NoError(t, db.Get().Model(&NodeOperation{}).Where("sub_id = ? AND action = ?", sub.ID, NodeOpStop).Count(&stopCount).Error)
	assert.Equal(t, int64(1), stopCount, "stop op dispatched on grace->suspended")

	// 推到停机期外,第三次扫描:suspended → deprovisioned + 派 destroy。
	require.NoError(t, db.Get().Model(&PrivateNodeSubscription{}).Where("id = ?", sub.ID).
		Update("expires_at", now-privateNodeGraceSeconds-privateNodeSuspendSeconds-10).Error)
	require.NoError(t, handlePrivateNodeLifecycleSweep(ctx, nil))
	var s3 PrivateNodeSubscription
	require.NoError(t, db.Get().First(&s3, sub.ID).Error)
	assert.Equal(t, PNStatusDeprovisioned, s3.Status)
	var destroyCount int64
	require.NoError(t, db.Get().Model(&NodeOperation{}).Where("sub_id = ? AND action = ?", sub.ID, NodeOpDestroy).Count(&destroyCount).Error)
	assert.Equal(t, int64(1), destroyCount, "destroy op dispatched on suspended->deprovisioned")
}
