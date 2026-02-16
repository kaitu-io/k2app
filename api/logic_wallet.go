package center

import (
	"context"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
)

// ==================== 钱包创建与查询 ====================

// GetOrCreateWallet 查找或创建用户钱包
// 如果钱包不存在，自动创建一个初始余额为0的钱包
func GetOrCreateWallet(ctx context.Context, userID uint64) (*Wallet, error) {
	var wallet Wallet
	err := db.Get().Where(&Wallet{UserID: userID}).First(&wallet).Error

	if err == gorm.ErrRecordNotFound {
		// 钱包不存在，自动创建
		wallet = Wallet{
			UserID:           userID,
			Balance:          0,
			AvailableBalance: 0,
			FrozenBalance:    0,
			TotalIncome:      0,
			TotalWithdrawn:   0,
			Version:          0,
		}
		if err := db.Get().Create(&wallet).Error; err != nil {
			log.Errorf(ctx, "创建钱包失败: %v", err)
			return nil, fmt.Errorf("创建钱包失败: %v", err)
		}
		log.Infof(ctx, "为用户 %d 自动创建钱包: wallet_id=%d", userID, wallet.ID)
		return &wallet, nil
	} else if err != nil {
		log.Errorf(ctx, "查询钱包失败: %v", err)
		return nil, fmt.Errorf("查询钱包失败: %v", err)
	}

	return &wallet, nil
}

// ==================== 钱包余额计算逻辑 ====================

// CalculateFrozenBalance 计算钱包的实时冻结余额
// 通过 SQL 聚合查询所有未到期的 income 记录（frozen_until > NOW()）
func CalculateFrozenBalance(ctx context.Context, walletID uint64) (int64, error) {
	var frozenBalance int64

	// 查询所有未到期的 income 类型记录的总金额
	// 只有 income 类型才有冻结期，且 amount 为正数
	// Note: frozen_until 字段需要用原生 SQL，因为 GORM 不支持 "IS NOT NULL AND > ?" 的结构体查询
	err := db.Get().Model(&WalletChange{}).
		Select("COALESCE(SUM(amount), 0)").
		Where(&WalletChange{
			WalletID: walletID,
			Type:     WalletChangeTypeIncome,
		}).
		Where("frozen_until IS NOT NULL AND frozen_until > ?", time.Now()).
		Scan(&frozenBalance).Error

	if err != nil {
		log.Errorf(ctx, "计算冻结余额失败: %v", err)
		return 0, err
	}

	log.Debugf(ctx, "wallet %d frozen balance: %d", walletID, frozenBalance)
	return frozenBalance, nil
}

// CalculateAvailableBalance 计算钱包的实时可用余额
// 可用余额 = 总余额 - 实时冻结余额
func CalculateAvailableBalance(ctx context.Context, walletID uint64) (int64, error) {
	// 查询钱包
	var wallet Wallet
	if err := db.Get().First(&wallet, walletID).Error; err != nil {
		log.Errorf(ctx, "查询钱包失败: %v", err)
		return 0, err
	}

	// 计算实时冻结余额（frozen_until > NOW() 的 income）
	frozenBalance, err := CalculateFrozenBalance(ctx, walletID)
	if err != nil {
		return 0, err
	}

	// 可用余额 = 总余额 - 冻结余额
	availableBalance := wallet.Balance - frozenBalance

	if availableBalance < 0 {
		log.Warnf(ctx, "wallet %d available balance is negative: %d (balance=%d, frozen=%d)",
			walletID, availableBalance, wallet.Balance, frozenBalance)
		availableBalance = 0
	}

	log.Debugf(ctx, "wallet %d available balance: %d (balance=%d, frozen=%d)",
		walletID, availableBalance, wallet.Balance, frozenBalance)

	return availableBalance, nil
}

// GetWalletWithBalances 获取钱包及其实时计算的余额信息
// 如果钱包不存在，会自动创建
func GetWalletWithBalances(ctx context.Context, userID uint64) (*Wallet, error) {
	// 使用 GetOrCreateWallet 确保钱包存在
	wallet, err := GetOrCreateWallet(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 实时计算冻结余额
	frozenBalance, err := CalculateFrozenBalance(ctx, wallet.ID)
	if err != nil {
		return nil, fmt.Errorf("计算冻结余额失败")
	}

	// 实时计算可用余额
	availableBalance, err := CalculateAvailableBalance(ctx, wallet.ID)
	if err != nil {
		return nil, fmt.Errorf("计算可用余额失败")
	}

	// 更新钱包对象（仅用于返回，不写入数据库）
	wallet.FrozenBalance = frozenBalance
	wallet.AvailableBalance = availableBalance

	return wallet, nil
}

// ==================== 返现收入处理 ====================

// AddCashbackIncome 添加返现收入
// freezeDays: 冻结天数，0 表示不冻结
func AddCashbackIncome(ctx context.Context, userID uint64, orderID uint64, amount int64, freezeDays int, remark string) error {
	return db.Get().Transaction(func(tx *gorm.DB) error {
		return addCashbackIncomeInTx(ctx, tx, userID, orderID, amount, freezeDays, remark)
	})
}

// addCashbackIncomeInTx 在给定事务中添加返现收入
// 此函数用于在已有事务中执行，确保与订单支付的原子性
func addCashbackIncomeInTx(ctx context.Context, tx *gorm.DB, userID uint64, orderID uint64, amount int64, freezeDays int, remark string) error {
	// 1. 查找或创建钱包
	var wallet Wallet
	err := tx.Where(&Wallet{UserID: userID}).First(&wallet).Error
	if err == gorm.ErrRecordNotFound {
		// 自动创建钱包
		wallet = Wallet{
			UserID:           userID,
			Balance:          0,
			AvailableBalance: 0,
			FrozenBalance:    0,
			TotalIncome:      0,
			TotalWithdrawn:   0,
			Version:          0,
		}
		if err := tx.Create(&wallet).Error; err != nil {
			return fmt.Errorf("创建钱包失败: %v", err)
		}
	} else if err != nil {
		return fmt.Errorf("查询钱包失败: %v", err)
	}

	// 2. 先尝试创建变动记录（利用唯一索引防重复）
	// 计算冻结到期时间
	var frozenUntil *time.Time
	if freezeDays > 0 {
		t := time.Now().AddDate(0, 0, freezeDays)
		frozenUntil = &t
	}

	// 记录变动前余额
	balanceBefore := wallet.Balance

	// 创建钱包变动记录
	change := WalletChange{
		WalletID:      wallet.ID,
		Type:          WalletChangeTypeIncome,
		Amount:        amount, // 正数
		BalanceBefore: balanceBefore,
		BalanceAfter:  balanceBefore + amount,
		FrozenUntil:   frozenUntil, // 设置冻结期
		OrderID:       &orderID,
		Remark:        remark,
	}

	if err := tx.Create(&change).Error; err != nil {
		// 检查是否是唯一索引冲突（重复返现）
		if util.DbIsDuplicatedErr(err) {
			log.Warnf(ctx, "订单 %d 已经返现过，跳过（唯一索引冲突）", orderID)
			return nil
		}
		return fmt.Errorf("记录钱包变动失败: %v", err)
	}

	// 3. 创建成功后才更新钱包总余额和累计收入
	if err := tx.Model(&wallet).
		Update("balance", gorm.Expr("balance + ?", amount)).
		Update("total_income", gorm.Expr("total_income + ?", amount)).
		Error; err != nil {
		return fmt.Errorf("更新钱包余额失败: %v", err)
	}

	log.Infof(ctx, "添加返现收入成功: wallet_id=%d, order_id=%d, amount=%d, freeze_days=%d",
		wallet.ID, orderID, amount, freezeDays)

	return nil
}

// RefundCashback 退款处理：将对应订单的返现标记为退款
func RefundCashback(ctx context.Context, orderID uint64) error {
	return db.Get().Transaction(func(tx *gorm.DB) error {
		return refundCashbackInTx(ctx, tx, orderID)
	})
}

// refundCashbackInTx 在给定事务中处理退款
func refundCashbackInTx(ctx context.Context, tx *gorm.DB, orderID uint64) error {
	// 查找该订单的 income 记录
	var incomeChange WalletChange
	err := tx.Where(&WalletChange{
		Type:    WalletChangeTypeIncome,
		OrderID: &orderID,
	}).First(&incomeChange).Error

	if err == gorm.ErrRecordNotFound {
		log.Warnf(ctx, "订单 %d 没有对应的返现记录，无需退款", orderID)
		return nil
	} else if err != nil {
		return fmt.Errorf("查询返现记录失败: %v", err)
	}

	// 查询钱包
	var wallet Wallet
	if err := tx.First(&wallet, incomeChange.WalletID).Error; err != nil {
		return fmt.Errorf("查询钱包失败: %v", err)
	}

	// 记录变动前余额
	balanceBefore := wallet.Balance

	// 扣减总余额和累计收入
	if err := tx.Model(&wallet).
		Update("balance", gorm.Expr("balance - ?", incomeChange.Amount)).
		Update("total_income", gorm.Expr("total_income - ?", incomeChange.Amount)).
		Error; err != nil {
		return fmt.Errorf("更新钱包余额失败: %v", err)
	}

	// 记录退款变动
	refundChange := WalletChange{
		WalletID:      wallet.ID,
		Type:          WalletChangeTypeRefund,
		Amount:        -incomeChange.Amount, // 负数
		BalanceBefore: balanceBefore,
		BalanceAfter:  balanceBefore - incomeChange.Amount,
		FrozenUntil:   nil, // 退款不需要冻结期
		OrderID:       &orderID,
		ParentID:      &incomeChange.ID, // 关联原收入记录
		Remark:        fmt.Sprintf("订单退款，返现作废 - 原收入ID: %d", incomeChange.ID),
	}

	if err := tx.Create(&refundChange).Error; err != nil {
		return fmt.Errorf("记录退款变动失败: %v", err)
	}

	log.Infof(ctx, "订单退款处理成功: order_id=%d, wallet_id=%d, refund_amount=%d",
		orderID, wallet.ID, incomeChange.Amount)

	return nil
}

// ==================== 提现处理逻辑 ====================

// ValidateWithdrawAmount 验证提现金额是否合法
func ValidateWithdrawAmount(ctx context.Context, walletID uint64, amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("提现金额必须大于0")
	}

	// 计算实时可用余额
	availableBalance, err := CalculateAvailableBalance(ctx, walletID)
	if err != nil {
		return fmt.Errorf("计算可用余额失败: %v", err)
	}

	if amount > availableBalance {
		return fmt.Errorf("可用余额不足，当前可用余额: %d，申请提现: %d", availableBalance, amount)
	}

	return nil
}
