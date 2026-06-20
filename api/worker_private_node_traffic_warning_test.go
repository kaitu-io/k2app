package center

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func TestPickTrafficTier(t *testing.T) {
	cases := []struct {
		used, total int64
		want        int
	}{
		{65, 100, 0}, {72, 100, 70}, {85, 100, 80}, {95, 100, 90},
		{100, 100, 100}, {120, 100, 100}, {0, 0, 0},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, pickTrafficTier(c.used, c.total), "used=%d total=%d", c.used, c.total)
	}
}

// seedActivePrivateLineWithUsage stands up an owner + private SlaveNode + active
// PrivateNodeSubscription (SlaveNodeID set) + a NodeUsage row keyed by that node
// at `pct`% of `total`. The NodeUsage is the metering authority the worker reads.
// Cleanup removes all rows. Returns the sub + node for assertions.
func seedActivePrivateLineWithUsage(t *testing.T, total int64, pct int) (*PrivateNodeSubscription, *SlaveNode) {
	t.Helper()
	const epoch int64 = 3
	owner := CreateTestUser(t)

	db.Get().Unscoped().Where("ipv4 = ?", "10.77.0.1").Delete(&SlaveNode{})
	node := SlaveNode{
		Ipv4:               "10.77.0.1",
		SecretToken:        "secret-nu-warn",
		Country:            "JP",
		Region:             "jp",
		Name:               "private-jp-nu-warn",
		Class:              NodeClassPrivate,
		PrivateOwnerUserID: &owner.ID,
	}
	require.NoError(t, db.Get().Create(&node).Error)

	sub := PrivateNodeSubscription{
		UserID: owner.ID, PlanID: 1, OrderID: node.ID, Region: "jp",
		IPType: IPTypeNonResidential, TrafficTotalBytes: total,
		Status: PNStatusActive, PurchasedAt: 1, ExpiresAt: 1 << 40,
		SlaveNodeID: &node.ID, BoundIpv4: node.Ipv4,
	}
	require.NoError(t, db.Get().Create(&sub).Error)
	require.NoError(t, db.Get().Model(&node).Update("private_sub_id", sub.ID).Error)

	// +1% headroom so integer truncation in pickTrafficTier's used*100/total can't
	// drop us below the requested tier boundary (matters for large `total`).
	used := total * int64(pct) / 100
	if pct < 100 {
		used += total / 100
	}
	u := NodeUsage{
		NodeID:          node.ID,
		Ipv4:            node.Ipv4,
		Epoch:           epoch,
		QuotaTotalBytes: total,
		UsedBytes:       used,
		LastReportAt:    1,
	}
	require.NoError(t, db.Get().Create(&u).Error)

	t.Cleanup(func() {
		db.Get().Unscoped().Where("ipv4 = ?", node.Ipv4).Delete(&NodeUsage{})
		db.Get().Unscoped().Delete(&sub)
		db.Get().Unscoped().Delete(&node)
	})
	return &sub, &node
}

func TestTrafficWarn_FromNodeUsage(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	var fired []int
	sendWarnHook = func(percent int, userID uint64) { fired = append(fired, percent) }
	t.Cleanup(func() { sendWarnHook = nil })

	sub, node := seedActivePrivateLineWithUsage(t, 1<<40, 80)
	_ = node
	require.NoError(t, runPrivateNodeTrafficWarning(context.Background()))
	require.NoError(t, runPrivateNodeTrafficWarning(context.Background()))
	assert.Equal(t, []int{80}, fired, "fire once, dedup by epoch")
	_ = sub
}

func TestTrafficWarningEpochResetReSends(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	var sent []int
	sendWarnHook = func(percent int, userID uint64) { sent = append(sent, percent) }
	t.Cleanup(func() { sendWarnHook = nil })

	_, node := seedActivePrivateLineWithUsage(t, 100, 85)

	// 标记本 epoch(3) 已发 80 档 → 同 epoch 不重发。
	require.NoError(t, db.Get().Model(&NodeUsage{}).Where("node_id = ?", node.ID).
		Update("warn80_sent_epoch", 3).Error)
	require.NoError(t, runPrivateNodeTrafficWarning(ctx))
	require.Empty(t, sent, "同 epoch 已发,不重发")

	// epoch 推进到 4,used 重置后又涨过 80 → 重新发。
	require.NoError(t, db.Get().Model(&NodeUsage{}).Where("node_id = ?", node.ID).
		Updates(map[string]any{"epoch": 4, "used_bytes": 81}).Error)
	require.NoError(t, runPrivateNodeTrafficWarning(ctx))
	require.Equal(t, []int{80}, sent)
}
