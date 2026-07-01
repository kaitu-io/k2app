package center

import (
	"context"
	"testing"
	"time"

	db "github.com/wordgate/qtoolkit/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrivateNodeLifecycleSweep verifies the daily lifecycle sweep advances
// private-node subscription status labels by exactly one step per run, performs
// the grace→suspended router cut, and recovers renewed (now < ExpiresAt) subs to
// active first. Integration test against the real dev MySQL.
func TestPrivateNodeLifecycleSweep(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()

	// Fixed OrderID band 9_600_001..9_600_012 — pre-purge any leftovers
	// (uniqueIndex on order_id makes stale rows fatal on rerun).
	for oid := uint64(9_600_001); oid <= 9_600_012; oid++ {
		db.Get().Unscoped().Where("order_id = ?", oid).Delete(&PrivateNodeSubscription{})
	}
	t.Cleanup(func() {
		for oid := uint64(9_600_001); oid <= 9_600_012; oid++ {
			db.Get().Unscoped().Where("order_id = ?", oid).Delete(&PrivateNodeSubscription{})
		}
	})

	mk := func(orderID uint64, status string, expiresAt int64) *PrivateNodeSubscription {
		s := &PrivateNodeSubscription{
			UserID:            960000,
			PlanID:            960000,
			OrderID:           orderID,
			Region:            "us-east-1",
			IPType:            IPTypeNonResidential,
			TrafficTotalBytes: 2 << 40,
			Status:            status,
			PurchasedAt:       now,
			ExpiresAt:         expiresAt,
		}
		require.NoError(t, db.Get().Create(s).Error)
		return s
	}

	// A: active just expired → grace, grace_until = ExpiresAt+7d
	subA := mk(9_600_001, PNStatusActive, now-3600)
	// B: grace past +7d → suspended, suspend_until = ExpiresAt+7d+14d
	subB := mk(9_600_002, PNStatusGrace, now-8*86400)
	// C: suspended past +21d → deprovisioned
	subC := mk(9_600_003, PNStatusSuspended, now-22*86400)
	// D: grace but renewed (now < ExpiresAt) → active, windows zeroed
	subD := mk(9_600_004, PNStatusGrace, now+5*86400)
	// E: active not yet expired → stays active
	subE := mk(9_600_005, PNStatusActive, now+30*86400)
	// F: active but 22d past expiry (long-stalled cron) → MUST advance only ONE
	//    step this sweep (active→grace), NOT cascade straight to deprovisioned.
	//    Pins the single-step-per-sweep guarantee.
	subF := mk(9_600_006, PNStatusActive, now-22*86400)
	// G: suspended but renewed (now < ExpiresAt) → active, windows zeroed.
	//    Mirrors D for the suspended branch of the renewal-recovery cohort.
	subG := mk(9_600_007, PNStatusSuspended, now+5*86400)

	require.NoError(t, handlePrivateNodeLifecycleSweep(context.Background(), nil))

	reload := func(id uint64) PrivateNodeSubscription {
		var s PrivateNodeSubscription
		require.NoError(t, db.Get().First(&s, id).Error)
		return s
	}

	rA := reload(subA.ID)
	assert.Equal(t, PNStatusGrace, rA.Status, "A: active expired → grace")
	assert.Equal(t, subA.ExpiresAt+privateNodeGraceSeconds, rA.GraceUntil, "A grace_until")

	rB := reload(subB.ID)
	assert.Equal(t, PNStatusSuspended, rB.Status, "B: grace ended → suspended")
	assert.Equal(t, subB.ExpiresAt+privateNodeGraceSeconds+privateNodeSuspendSeconds, rB.SuspendUntil, "B suspend_until")

	rC := reload(subC.ID)
	assert.Equal(t, PNStatusDeprovisioned, rC.Status, "C: suspend ended → deprovisioned")

	rD := reload(subD.ID)
	assert.Equal(t, PNStatusActive, rD.Status, "D: renewed grace → active")
	assert.Equal(t, int64(0), rD.GraceUntil, "D grace_until zeroed")
	assert.Equal(t, int64(0), rD.SuspendUntil, "D suspend_until zeroed")

	rE := reload(subE.ID)
	assert.Equal(t, PNStatusActive, rE.Status, "E: not expired → stays active")

	rF := reload(subF.ID)
	assert.Equal(t, PNStatusGrace, rF.Status,
		"F: active 22d-past-expiry must advance ONE step (active→grace), not cascade to deprovisioned")
	assert.Equal(t, subF.ExpiresAt+privateNodeGraceSeconds, rF.GraceUntil, "F grace_until")

	rG := reload(subG.ID)
	assert.Equal(t, PNStatusActive, rG.Status, "G: renewed suspended → active")
	assert.Equal(t, int64(0), rG.GraceUntil, "G grace_until zeroed")
	assert.Equal(t, int64(0), rG.SuspendUntil, "G suspend_until zeroed")
}

func TestLifecycleSweep_DispatchesStopOnSuspend(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	now := time.Now().Unix()
	ciID := uint64(999100)
	const orderID = uint64(9_600_101) // 专属 OrderID 段,pre-purge 防 uniqueIndex 残留
	db.Get().Unscoped().Where("order_id = ?", orderID).Delete(&PrivateNodeSubscription{})
	sub := &PrivateNodeSubscription{
		UserID: 990001, PlanID: 1, OrderID: orderID, Status: PNStatusGrace,
		ExpiresAt: now - privateNodeGraceSeconds - 10, CloudInstanceID: &ciID,
		Region: "ap-northeast-1", IPType: "non_residential", TrafficTotalBytes: 1,
	}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() {
		db.Get().Where("sub_id = ?", sub.ID).Delete(&NodeOperation{})
		db.Get().Unscoped().Where("order_id = ?", orderID).Delete(&PrivateNodeSubscription{})
	})

	require.NoError(t, handlePrivateNodeLifecycleSweep(ctx, nil))

	var op NodeOperation
	require.NoError(t, db.Get().Where("sub_id = ? AND action = ?", sub.ID, NodeOpStop).First(&op).Error)
	assert.Equal(t, NodeOpQueued, op.Status)
	assert.Equal(t, "system:lifecycle", op.CreatedBy)
}

func TestLifecycleSweep_RenewalCancelsOpenStop(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	now := time.Now().Unix()
	ciID := uint64(999101)
	const orderID = uint64(9_600_102) // 专属 OrderID 段,pre-purge 防 uniqueIndex 残留
	db.Get().Unscoped().Where("order_id = ?", orderID).Delete(&PrivateNodeSubscription{})
	sub := &PrivateNodeSubscription{
		UserID: 990002, PlanID: 1, OrderID: orderID, Status: PNStatusSuspended,
		ExpiresAt: now + 30*86400, CloudInstanceID: &ciID,
		Region: "ap-northeast-1", IPType: "non_residential", TrafficTotalBytes: 1,
	}
	require.NoError(t, db.Get().Create(sub).Error)
	stop := &NodeOperation{Action: NodeOpStop, SubID: sub.ID, Status: NodeOpQueued, CreatedBy: "system:lifecycle", CloudInstanceID: &ciID}
	require.NoError(t, db.Get().Create(stop).Error)
	t.Cleanup(func() {
		db.Get().Where("sub_id = ?", sub.ID).Delete(&NodeOperation{})
		db.Get().Unscoped().Where("order_id = ?", orderID).Delete(&PrivateNodeSubscription{})
	})

	require.NoError(t, handlePrivateNodeLifecycleSweep(ctx, nil))

	var gotSub PrivateNodeSubscription
	require.NoError(t, db.Get().First(&gotSub, sub.ID).Error)
	assert.Equal(t, PNStatusActive, gotSub.Status)
	var gotOp NodeOperation
	require.NoError(t, db.Get().First(&gotOp, stop.ID).Error)
	assert.Equal(t, NodeOpCanceled, gotOp.Status, "renewed sub's open stop must be canceled")
}

// TestLifecycleSweep_DeprovisionClearsBindings 验证终态(deprovisioned)释放基础设施绑定：
// VPS 即将销毁、IP 即将被云厂商回收 → 清空 slave_node_id / cloud_instance_id / bound_ipv4，
// 否则回收 IP 上的新节点注册时会按陈旧 bound_ipv4 误绑到这条已终态订阅（配合注册侧
// reconcilePrivateIdentity 的 BoundIpv4 重认领，#16 + P0 单一权威源防御纵深）。
// destroy 工单仍须被派发（用清空前的 cloud_instance_id）。
func TestLifecycleSweep_DeprovisionClearsBindings(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ctx := context.Background()
	now := time.Now().Unix()

	ciID := uint64(999102)
	nodeID := uint64(999103)
	const orderID = uint64(9_600_103) // 专属 OrderID 段，pre-purge 防 uniqueIndex 残留
	db.Get().Unscoped().Where("order_id = ?", orderID).Delete(&PrivateNodeSubscription{})
	sub := &PrivateNodeSubscription{
		UserID: 990003, PlanID: 1, OrderID: orderID, Status: PNStatusSuspended,
		ExpiresAt:   now - privateNodeGraceSeconds - privateNodeSuspendSeconds - 10, // 过停机期 → deprovisioned
		CloudInstanceID: &ciID, SlaveNodeID: &nodeID, BoundIpv4: "203.0.113.77",
		Region: "ap-northeast-1", IPType: IPTypeNonResidential, TrafficTotalBytes: 1,
	}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() {
		db.Get().Where("sub_id = ?", sub.ID).Delete(&NodeOperation{})
		db.Get().Unscoped().Where("order_id = ?", orderID).Delete(&PrivateNodeSubscription{})
	})

	require.NoError(t, handlePrivateNodeLifecycleSweep(ctx, nil))

	var got PrivateNodeSubscription
	require.NoError(t, db.Get().First(&got, sub.ID).Error)
	assert.Equal(t, PNStatusDeprovisioned, got.Status, "suspend ended → deprovisioned")
	assert.Nil(t, got.SlaveNodeID, "deprovision 必须清空 slave_node_id")
	assert.Nil(t, got.CloudInstanceID, "deprovision 必须清空 cloud_instance_id")
	assert.Equal(t, "", got.BoundIpv4, "deprovision 必须清空 bound_ipv4（防回收 IP 误绑，配合注册侧重认领）")

	// destroy 工单仍须派发（用清空前捕获的 cloud_instance_id）。
	var op NodeOperation
	require.NoError(t, db.Get().Where("sub_id = ? AND action = ?", sub.ID, NodeOpDestroy).First(&op).Error)
	assert.Equal(t, NodeOpQueued, op.Status, "destroy 工单应派发")
	require.NotNil(t, op.CloudInstanceID, "destroy 工单应带上销毁前的 cloud_instance_id")
	assert.Equal(t, ciID, *op.CloudInstanceID)
}
