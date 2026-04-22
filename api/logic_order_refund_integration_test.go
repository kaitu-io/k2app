package center

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestProcessOrderRefund_Integration_EndToEnd exercises the full 5-step refund
// flow against a real MySQL connection. Verifies all audit records align.
// Requires config.yml — guarded by skipIfNoConfig.
func TestProcessOrderRefund_Integration_EndToEnd(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()

	now := time.Now()

	// 1. Create test user with IsFirstOrderDone=true + future ExpiredAt
	originalExpiredAt := now.Add(365 * 24 * time.Hour).Unix()
	user := User{
		UUID:             "usr-refund-test-" + time.Now().Format("20060102150405"),
		ExpiredAt:        originalExpiredAt,
		IsFirstOrderDone: BoolPtr(true),
	}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	// 2. Create paid Order for that user
	isPaid := true
	paidAt := now.Add(-10 * 24 * time.Hour)
	order := Order{
		UUID:      "ord-refund-test-" + time.Now().Format("20060102150405"),
		Title:     "Test 1y",
		UserID:    user.ID,
		PayAmount: 4900,
		IsPaid:    &isPaid,
		PaidAt:    &paidAt,
		Meta:      "{}",
	}
	require.NoError(t, db.Get().Create(&order).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&order) })

	// 3. Seed VipPurchase UserProHistory (365 days added by the order originally)
	history := UserProHistory{
		UserID:      user.ID,
		Type:        VipPurchase,
		ReferenceID: order.ID,
		Days:        365,
		Reason:      "订单支付 - " + order.UUID,
	}
	require.NoError(t, db.Get().Create(&history).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&history) })

	// 4. Call ProcessOrderRefund
	err := ProcessOrderRefund(ctx, order.ID, "E2E 测试退款", uint64(99))
	require.NoError(t, err)

	// 5a. Verify orders row
	var refreshed Order
	require.NoError(t, db.Get().First(&refreshed, order.ID).Error)
	require.NotNil(t, refreshed.IsRefunded)
	require.True(t, *refreshed.IsRefunded, "IsRefunded should be true")
	require.NotNil(t, refreshed.RefundedAt)
	require.Equal(t, uint64(4900), refreshed.RefundAmount)
	require.Equal(t, "E2E 测试退款", refreshed.RefundReason)
	// is_paid still true (historical fact)
	require.NotNil(t, refreshed.IsPaid)
	require.True(t, *refreshed.IsPaid, "IsPaid must remain true as historical fact")

	// 5b. Verify reverse UserProHistory
	var reverse UserProHistory
	require.NoError(t, db.Get().
		Where("reference_id = ? AND type = ?", order.ID, VipRefund).
		First(&reverse).Error)
	require.Equal(t, -365, reverse.Days)
	require.Equal(t, user.ID, reverse.UserID)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&reverse) })

	// 5c. Verify user.ExpiredAt reduced + IsFirstOrderDone flipped to false
	var user2 User
	require.NoError(t, db.Get().First(&user2, user.ID).Error)
	require.NotNil(t, user2.IsFirstOrderDone)
	require.False(t, *user2.IsFirstOrderDone, "IsFirstOrderDone should flip to false")
	expectedExpiredAt := originalExpiredAt - (365 * 86400)
	require.Equal(t, expectedExpiredAt, user2.ExpiredAt, "ExpiredAt should decrease by 365 days")

	// 5d. Verify wallet credited
	var wallet Wallet
	require.NoError(t, db.Get().Where(&Wallet{UserID: user.ID}).First(&wallet).Error)
	require.Equal(t, int64(4900), wallet.Balance, "Wallet balance should equal PayAmount")
	t.Cleanup(func() { db.Get().Unscoped().Delete(&wallet) })

	// 5e. Verify wallet_changes order_refund record
	var change WalletChange
	require.NoError(t, db.Get().
		Where("wallet_id = ? AND type = ? AND order_id = ?",
			wallet.ID, WalletChangeTypeOrderRefund, order.ID).
		First(&change).Error)
	require.Equal(t, int64(4900), change.Amount)
	require.Nil(t, change.FrozenUntil, "order_refund should not be frozen")
	require.NotNil(t, change.OperatorID)
	require.Equal(t, uint64(99), *change.OperatorID)
	require.Equal(t, "E2E 测试退款", change.Remark)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&change) })

	// 6. Idempotency — second call must fail
	err2 := ProcessOrderRefund(ctx, order.ID, "重复尝试", uint64(99))
	require.Error(t, err2)
	require.Contains(t, err2.Error(), "已退款")
}
