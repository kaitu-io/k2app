package center

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 复现 2026-11-04 生产故障：withdraws 表残留 NOT NULL 无默认值的 wallet_address / network，
// STRICT 模式下 INSERT 失败；dropLegacyWithdrawColumns 之后同一 INSERT 必须成功。
func TestDropLegacyWithdrawColumns_UnblocksInsert(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	database := db.Get()
	mig := database.Migrator()
	require.True(t, mig.HasTable(&Withdraw{}))

	require.NoError(t, database.Exec("SET SESSION sql_mode = 'STRICT_TRANS_TABLES'").Error)
	for _, ddl := range []string{
		"ALTER TABLE withdraws ADD COLUMN wallet_address varchar(100) NOT NULL",
		"ALTER TABLE withdraws ADD COLUMN network varchar(20) NOT NULL",
	} {
		require.NoError(t, database.Exec(ddl).Error)
	}
	t.Cleanup(func() { _ = dropLegacyWithdrawColumns(ctx, database) })

	newRow := func() *Withdraw {
		return &Withdraw{UserID: 999999001, WalletID: 999999001, Amount: 100, NetAmount: 100,
			WithdrawAccountID: 999999001, AccountType: "alipay", AccountID: "x", Currency: "cny",
			Status: WithdrawStatusPending}
	}
	tx := database.Begin()
	err := tx.Create(newRow()).Error
	tx.Rollback()
	require.Error(t, err, "legacy NOT NULL columns must break the insert (reproduction)")
	require.Contains(t, err.Error(), "wallet_address")

	require.NoError(t, dropLegacyWithdrawColumns(ctx, database))
	require.False(t, mig.HasColumn(&Withdraw{}, "wallet_address"))
	require.False(t, mig.HasColumn(&Withdraw{}, "network"))
	require.NoError(t, dropLegacyWithdrawColumns(ctx, database), "idempotent")

	tx = database.Begin()
	require.NoError(t, tx.Create(newRow()).Error)
	tx.Rollback()
}
