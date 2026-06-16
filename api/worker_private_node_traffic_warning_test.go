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

func TestTrafficWarningCrosses80SendsOnceThenDedups(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	ci := &CloudInstance{
		Provider: "test", AccountName: "a", InstanceID: "i-w80",
		IPAddress: "1.2.3.4", Region: "jp",
		TrafficUsedBytes: 85, TrafficTotalBytes: 100, TrafficEpoch: 3,
	}
	require.NoError(t, db.Get().Create(ci).Error)
	t.Cleanup(func() { db.Get().Unscoped().Where("instance_id = ?", "i-w80").Delete(&CloudInstance{}) })
	sub := &PrivateNodeSubscription{
		UserID: 9990001, PlanID: 1, OrderID: 59990001, Region: "jp",
		IPType: IPTypeNonResidential, TrafficTotalBytes: 100,
		Status: PNStatusActive, PurchasedAt: 1, ExpiresAt: 1 << 40,
		CloudInstanceID: &ci.ID,
	}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Where("order_id = ?", 59990001).Delete(&PrivateNodeSubscription{}) })

	var sent []int
	sendWarnHook = func(percent int, userID uint64) { sent = append(sent, percent) }
	t.Cleanup(func() { sendWarnHook = nil })

	require.NoError(t, runPrivateNodeTrafficWarning(ctx))
	require.Equal(t, []int{80}, sent)

	sent = nil
	require.NoError(t, runPrivateNodeTrafficWarning(ctx))
	require.Empty(t, sent) // 同 epoch 已发,不重发
}

func TestTrafficWarningEpochResetReSends(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	ci := &CloudInstance{
		Provider: "test", AccountName: "a", InstanceID: "i-wep",
		IPAddress: "1.2.3.4", Region: "jp",
		TrafficUsedBytes: 85, TrafficTotalBytes: 100, TrafficEpoch: 3,
		Warn80SentEpoch: 3,
	}
	require.NoError(t, db.Get().Create(ci).Error)
	t.Cleanup(func() { db.Get().Unscoped().Where("instance_id = ?", "i-wep").Delete(&CloudInstance{}) })
	sub := &PrivateNodeSubscription{
		UserID: 9990002, PlanID: 1, OrderID: 59990002, Region: "jp",
		IPType: IPTypeNonResidential, TrafficTotalBytes: 100,
		Status: PNStatusActive, PurchasedAt: 1, ExpiresAt: 1 << 40,
		CloudInstanceID: &ci.ID,
	}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Where("order_id = ?", 59990002).Delete(&PrivateNodeSubscription{}) })

	// epoch 推进到 4,used 重置后又涨过 80
	require.NoError(t, db.Get().Model(&CloudInstance{}).Where("id = ?", ci.ID).
		Updates(map[string]any{"traffic_epoch": 4, "traffic_used_bytes": 81}).Error)

	var sent []int
	sendWarnHook = func(percent int, userID uint64) { sent = append(sent, percent) }
	t.Cleanup(func() { sendWarnHook = nil })
	require.NoError(t, runPrivateNodeTrafficWarning(ctx))
	require.Equal(t, []int{80}, sent)
}
