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

	// Fixed OrderID band 9_600_001..9_600_010 — pre-purge any leftovers
	// (uniqueIndex on order_id makes stale rows fatal on rerun).
	for oid := uint64(9_600_001); oid <= 9_600_010; oid++ {
		db.Get().Unscoped().Where("order_id = ?", oid).Delete(&PrivateNodeSubscription{})
	}
	t.Cleanup(func() {
		for oid := uint64(9_600_001); oid <= 9_600_010; oid++ {
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
}
