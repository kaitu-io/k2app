package center

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func TestDispatchNodeOperation_Idempotent(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	sub := seedTestPrivateSub(t)
	ciID := uint64(999001)

	require.NoError(t, dispatchNodeOperation(ctx, sub.ID, &ciID, NodeOpStop, "system:lifecycle", StopParams{Reason: "grace ended"}))
	require.NoError(t, dispatchNodeOperation(ctx, sub.ID, &ciID, NodeOpStop, "system:lifecycle", StopParams{Reason: "grace ended"}))

	var count int64
	require.NoError(t, db.Get().Model(&NodeOperation{}).Where("sub_id = ? AND action = ?", sub.ID, NodeOpStop).Count(&count).Error)
	assert.Equal(t, int64(1), count, "duplicate open stop op must be deduped")
}

func TestDispatchNodeOperation_NilInstanceGuard(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	sub := seedTestPrivateSub(t)

	require.NoError(t, dispatchNodeOperation(ctx, sub.ID, nil, NodeOpStop, "system:lifecycle", StopParams{Reason: "x"}))
	var count int64
	require.NoError(t, db.Get().Model(&NodeOperation{}).Where("sub_id = ? AND action = ?", sub.ID, NodeOpStop).Count(&count).Error)
	assert.Equal(t, int64(0), count, "nil-instance stop must be skipped")
}

func TestCancelOpenNodeOperations(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	sub := seedTestPrivateSub(t)
	ciID := uint64(999002)
	require.NoError(t, dispatchNodeOperation(ctx, sub.ID, &ciID, NodeOpStop, "system:lifecycle", StopParams{Reason: "x"}))

	require.NoError(t, cancelOpenNodeOperations(db.Get(), []uint64{sub.ID}, []string{NodeOpStop, NodeOpDestroy}))

	var op NodeOperation
	require.NoError(t, db.Get().Where("sub_id = ? AND action = ?", sub.ID, NodeOpStop).First(&op).Error)
	assert.Equal(t, NodeOpCanceled, op.Status)
}

func seedTestPrivateSub(t *testing.T) *PrivateNodeSubscription {
	t.Helper()
	now := nowUnixForTest()
	sub := &PrivateNodeSubscription{
		UserID: 990000, PlanID: 1, OrderID: 0,
		Region: "ap-northeast-1", IPType: "non_residential",
		TrafficTotalBytes: 2 << 40, Status: PNStatusActive,
		PurchasedAt: now, ExpiresAt: now + 86400,
	}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() {
		db.Get().Where("sub_id = ?", sub.ID).Delete(&NodeOperation{})
		db.Get().Delete(&PrivateNodeSubscription{}, sub.ID)
	})
	return sub
}

func nowUnixForTest() int64 { return 1750000000 }
