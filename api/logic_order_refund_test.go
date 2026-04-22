package center

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestProcessOrderRefund_HappyPath: paid order + user with Pro + retailer cashback → refund succeeds
// Verifies the full 5-step flow writes: order update, reverse UserProHistory, user update,
// cashback reversal (wallet_change), user wallet credit (wallet_change), order refund state update.
func TestProcessOrderRefund_HappyPath(t *testing.T) {
	m := SetupMockDB(t)

	// Swap getDB to point at mock; restore in Cleanup
	orig := getDB
	getDB = func() *gorm.DB { return m.DB }
	t.Cleanup(func() { getDB = orig })

	ctx := context.Background()

	now := time.Now()
	paidAt := now.Add(-30 * 24 * time.Hour)
	isPaid := true
	isRefundedFalse := false

	// Tx: BEGIN
	m.Mock.ExpectBegin()

	// 1. SELECT order FOR UPDATE + preload user
	m.Mock.ExpectQuery(`SELECT \* FROM .orders. WHERE .orders...id. = \? .* FOR UPDATE`).
		WithArgs(uint64(1), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "user_id", "pay_amount", "is_paid", "paid_at", "is_refunded", "refund_amount"}).
			AddRow(uint64(1), "ord-test-1", uint64(100), uint64(4900), &isPaid, &paidAt, &isRefundedFalse, uint64(0)))
	m.Mock.ExpectQuery(`SELECT \* FROM .users. WHERE .users...id. = \?`).
		WithArgs(uint64(100)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "expired_at", "is_first_order_done"}).
			AddRow(uint64(100), now.Add(335*24*time.Hour).Unix(), true))

	// 2. SUM(days) for VipPurchase + reference_id=1 + days > 0
	m.Mock.ExpectQuery(`SELECT COALESCE\(SUM\(days\), 0\) FROM .user_pro_histories. WHERE .*`).
		WillReturnRows(sqlmock.NewRows([]string{"days"}).AddRow(365))

	// 2b. FOR UPDATE lock on user row (re-load under lock, separate from Preload)
	m.Mock.ExpectQuery(`SELECT \* FROM .users. WHERE .users...id. = \? .* FOR UPDATE`).
		WithArgs(uint64(100), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "expired_at", "is_first_order_done"}).
			AddRow(uint64(100), now.Add(335*24*time.Hour).Unix(), true))

	// INSERT reverse UserProHistory
	m.Mock.ExpectExec(`INSERT INTO .user_pro_histories.`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// COUNT other valid paid orders for IsFirstOrderDone check (returns 0 → flip to false)
	m.Mock.ExpectQuery(`SELECT count\(\*\) FROM .orders. WHERE user_id = \? AND is_paid = \? AND \(is_refunded IS NULL OR is_refunded = \?\) AND id != \?`).
		WithArgs(uint64(100), true, false, uint64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// UPDATE user — ExpiredAt rolled back + IsFirstOrderDone=false
	m.Mock.ExpectExec(`UPDATE .users. SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 3. refundCashbackInTx — find income record, reverse it
	m.Mock.ExpectQuery(`SELECT \* FROM .wallet_changes. WHERE .*`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "wallet_id", "amount"}).AddRow(uint64(50), uint64(200), 500))
	m.Mock.ExpectQuery(`SELECT \* FROM .wallets. WHERE .wallets...id. = \?`).
		WithArgs(uint64(200), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "balance", "total_income"}).AddRow(uint64(200), 500, 500))
	// refundCashbackInTx does two chained Update() calls → two SQL statements
	m.Mock.ExpectExec(`UPDATE .wallets. SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.Mock.ExpectExec(`UPDATE .wallets. SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.Mock.ExpectExec(`INSERT INTO .wallet_changes.`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// 4. getOrCreateWalletInTx for user 100
	m.Mock.ExpectQuery(`SELECT \* FROM .wallets. WHERE .wallets...user_id. = \?`).
		WithArgs(uint64(100), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "balance"}).AddRow(uint64(300), uint64(100), 0))

	// INSERT order_refund wallet_change
	m.Mock.ExpectExec(`INSERT INTO .wallet_changes.`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// UPDATE user wallet balance — two chained Update() calls → two SQL statements
	m.Mock.ExpectExec(`UPDATE .wallets. SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.Mock.ExpectExec(`UPDATE .wallets. SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 5. UPDATE order — set IsRefunded/RefundedAt/RefundAmount/RefundReason
	m.Mock.ExpectExec(`UPDATE .orders. SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	m.Mock.ExpectCommit()

	err := ProcessOrderRefund(ctx, uint64(1), "测试退款原因", uint64(99))
	require.NoError(t, err)

	require.NoError(t, m.Mock.ExpectationsWereMet())
}

// TestProcessOrderRefund_OrderNotPaid: order with IsPaid=false → returns "订单未支付" error + rollback.
// No writes to any table.
func TestProcessOrderRefund_OrderNotPaid(t *testing.T) {
	m := SetupMockDB(t)

	orig := getDB
	getDB = func() *gorm.DB { return m.DB }
	t.Cleanup(func() { getDB = orig })

	ctx := context.Background()
	isPaid := false

	m.Mock.ExpectBegin()

	// SELECT order FOR UPDATE (GORM First adds implicit LIMIT 1 arg)
	m.Mock.ExpectQuery(`SELECT \* FROM .orders. WHERE .orders...id. = \? .* FOR UPDATE`).
		WithArgs(uint64(1), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "user_id", "is_paid"}).
			AddRow(uint64(1), "ord-test-np", uint64(100), &isPaid))
	// Preload("User") fires eagerly before validation
	m.Mock.ExpectQuery(`SELECT \* FROM .users. WHERE .users...id. = \?`).
		WithArgs(uint64(100)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uint64(100)))

	m.Mock.ExpectRollback()

	err := ProcessOrderRefund(ctx, uint64(1), "测试", uint64(99))
	require.Error(t, err)
	require.Contains(t, err.Error(), "订单未支付")

	require.NoError(t, m.Mock.ExpectationsWereMet())
}

// TestProcessOrderRefund_AlreadyRefunded: order with IsPaid=true, IsRefunded=true → returns "订单已退款" error + rollback.
// No writes to any table.
func TestProcessOrderRefund_AlreadyRefunded(t *testing.T) {
	m := SetupMockDB(t)

	orig := getDB
	getDB = func() *gorm.DB { return m.DB }
	t.Cleanup(func() { getDB = orig })

	ctx := context.Background()
	isPaid := true
	isRefunded := true

	m.Mock.ExpectBegin()

	// SELECT order FOR UPDATE
	m.Mock.ExpectQuery(`SELECT \* FROM .orders. WHERE .orders...id. = \? .* FOR UPDATE`).
		WithArgs(uint64(1), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "user_id", "is_paid", "is_refunded"}).
			AddRow(uint64(1), "ord-test-ar", uint64(100), &isPaid, &isRefunded))
	// Preload("User") fires eagerly before validation
	m.Mock.ExpectQuery(`SELECT \* FROM .users. WHERE .users...id. = \?`).
		WithArgs(uint64(100)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uint64(100)))

	m.Mock.ExpectRollback()

	err := ProcessOrderRefund(ctx, uint64(1), "测试", uint64(99))
	require.Error(t, err)
	require.Contains(t, err.Error(), "订单已退款")

	require.NoError(t, m.Mock.ExpectationsWereMet())
}

// TestProcessOrderRefund_NoProHistory: paid + refundable order with no VipPurchase days.
// Function warns and continues: no reverse UserProHistory INSERT, no IsFirstOrderDone check
// (user.IsFirstOrderDone=false), cashback ErrRecordNotFound absorbed silently.
// Wallet credit and order update still execute — full success.
func TestProcessOrderRefund_NoProHistory(t *testing.T) {
	m := SetupMockDB(t)

	orig := getDB
	getDB = func() *gorm.DB { return m.DB }
	t.Cleanup(func() { getDB = orig })

	ctx := context.Background()
	now := time.Now()
	paidAt := now.Add(-30 * 24 * time.Hour)
	isPaid := true
	isRefundedFalse := false

	m.Mock.ExpectBegin()

	// 1. SELECT order FOR UPDATE + Preload user
	m.Mock.ExpectQuery(`SELECT \* FROM .orders. WHERE .orders...id. = \? .* FOR UPDATE`).
		WithArgs(uint64(1), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "user_id", "pay_amount", "is_paid", "paid_at", "is_refunded", "refund_amount"}).
			AddRow(uint64(1), "ord-test-nh", uint64(100), uint64(4900), &isPaid, &paidAt, &isRefundedFalse, uint64(0)))
	m.Mock.ExpectQuery(`SELECT \* FROM .users. WHERE .users...id. = \?`).
		WithArgs(uint64(100)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "is_first_order_done"}).AddRow(uint64(100), false))

	// 2. SUM(days) returns 0 — no VipPurchase records for this order
	m.Mock.ExpectQuery(`SELECT COALESCE\(SUM\(days\), 0\) FROM .user_pro_histories. WHERE .*`).
		WillReturnRows(sqlmock.NewRows([]string{"days"}).AddRow(0))

	// 2b. FOR UPDATE lock on user row
	m.Mock.ExpectQuery(`SELECT \* FROM .users. WHERE .users...id. = \? .* FOR UPDATE`).
		WithArgs(uint64(100), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "is_first_order_done"}).AddRow(uint64(100), false))

	// purchaseDays=0 → no INSERT reverse UserProHistory
	// user.IsFirstOrderDone=false → no COUNT other paid orders

	// UPDATE user (ExpiredAt + IsFirstOrderDone)
	m.Mock.ExpectExec(`UPDATE .users. SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 3. refundCashbackInTx: no income cashback record → absorbed silently
	m.Mock.ExpectQuery(`SELECT \* FROM .wallet_changes. WHERE .*`).
		WillReturnError(gorm.ErrRecordNotFound)

	// 4. getOrCreateWalletInTx — existing wallet found
	m.Mock.ExpectQuery(`SELECT \* FROM .wallets. WHERE .wallets...user_id. = \?`).
		WithArgs(uint64(100), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "balance"}).AddRow(uint64(300), uint64(100), 0))

	// INSERT order_refund wallet_change
	m.Mock.ExpectExec(`INSERT INTO .wallet_changes.`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// UPDATE wallet balance — two chained Update() calls → two SQL statements
	m.Mock.ExpectExec(`UPDATE .wallets. SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.Mock.ExpectExec(`UPDATE .wallets. SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 5. UPDATE order — set IsRefunded/RefundedAt/RefundAmount/RefundReason
	m.Mock.ExpectExec(`UPDATE .orders. SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	m.Mock.ExpectCommit()

	err := ProcessOrderRefund(ctx, uint64(1), "测试无记录", uint64(99))
	require.NoError(t, err)

	require.NoError(t, m.Mock.ExpectationsWereMet())
}
