package center

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// api_get_wallet 获取用户钱包信息
func api_get_wallet(c *gin.Context) {
	userID := ReqUserID(c)

	// 使用 logic 层获取钱包并实时计算余额
	wallet, err := GetWalletWithBalances(c, userID)
	if err != nil {
		log.Errorf(c, "获取钱包失败: %v", err)
		Error(c, 500, err.Error())
		return
	}

	Success(c, wallet)
}

// api_get_wallet_changes 获取钱包变动记录
func api_get_wallet_changes(c *gin.Context) {
	userID := ReqUserID(c)

	// 查找或创建钱包
	wallet, err := GetOrCreateWallet(c, userID)
	if err != nil {
		log.Errorf(c, "获取钱包失败: %v", err)
		Error(c, 500, "获取钱包失败")
		return
	}

	// 分页参数
	pagination := PaginationFromRequest(c)

	// 查询条件
	query := db.Get().Where(&WalletChange{WalletID: wallet.ID})

	// 类型过滤
	if changeType := c.Query("type"); changeType != "" {
		query = query.Where(&WalletChange{Type: WalletChangeType(changeType)})
	}

	// 查询总数
	var total int64
	if err := query.Model(&WalletChange{}).Count(&total).Error; err != nil {
		Error(c, 500, "查询失败")
		return
	}

	// 查询记录
	var changes []WalletChange
	if err := query.Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&changes).Error; err != nil {
		Error(c, 500, "查询失败")
		return
	}

	pagination.Total = total
	List(c, changes, pagination)
}

// api_get_withdraw_accounts 获取提现账户列表
func api_get_withdraw_accounts(c *gin.Context) {
	userID := ReqUserID(c)

	var accounts []WithdrawAccount
	if err := db.Get().Where(&WithdrawAccount{UserID: userID}).
		Order("is_default DESC, created_at DESC").
		Find(&accounts).Error; err != nil {
		Error(c, 500, "查询提现账户失败")
		return
	}

	Success(c, &accounts)
}

// api_create_withdraw_account 添加提现账户
func api_create_withdraw_account(c *gin.Context) {
	userID := ReqUserID(c)

	var req CreateWithdrawAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, 422, fmt.Sprintf("参数错误: %v", err))
		return
	}

	// 验证渠道类型
	validAccountTypes := map[WithdrawAccountType]bool{
		WithdrawAccountTypeTron:     true,
		WithdrawAccountTypePolygon:  true,
		WithdrawAccountTypeBSC:      true,
		WithdrawAccountTypeArbitrum: true,
		WithdrawAccountTypePayPal:   true,
	}
	if !validAccountTypes[req.AccountType] {
		Error(c, 422, "不支持的渠道类型")
		return
	}

	// 处理币种
	var currency Currency
	if req.AccountType == WithdrawAccountTypePayPal {
		// PayPal 强制使用 USD
		currency = CurrencyUSD
	} else {
		// 加密货币验证币种
		if req.Currency != CurrencyUSDT && req.Currency != CurrencyUSDC {
			Error(c, 422, "加密货币仅支持 USDT 或 USDC")
			return
		}
		currency = req.Currency
	}

	// 验证收款标识格式
	if err := validateAccountID(req.AccountType, req.AccountID); err != nil {
		Error(c, 422, err.Error())
		return
	}

	// 检查是否已存在（同渠道+同地址）
	var existingAccount WithdrawAccount
	if err := db.Get().Where(&WithdrawAccount{
		UserID:      userID,
		AccountType: req.AccountType,
		AccountID:   req.AccountID,
	}).First(&existingAccount).Error; err == nil {
		Error(c, 422, "该提现账户已存在")
		return
	}

	// 如果是第一个账户，自动设为默认
	var accountCount int64
	db.Get().Model(&WithdrawAccount{}).Where(&WithdrawAccount{UserID: userID}).Count(&accountCount)
	isDefault := accountCount == 0

	account := WithdrawAccount{
		UserID:      userID,
		AccountType: req.AccountType,
		AccountID:   req.AccountID,
		Currency:    currency,
		Label:       req.Label,
		IsDefault:   BoolPtr(isDefault),
	}

	if err := db.Get().Create(&account).Error; err != nil {
		log.Errorf(c, "创建提现账户失败: %v", err)
		Error(c, 500, "创建提现账户失败")
		return
	}

	Success(c, &account)
}

// validateAccountID 验证收款标识格式
func validateAccountID(accountType WithdrawAccountType, accountID string) error {
	if accountID == "" {
		return fmt.Errorf("收款标识不能为空")
	}

	switch accountType {
	case WithdrawAccountTypeTron:
		// TRON 地址: T 开头，34 字符
		if len(accountID) != 34 || accountID[0] != 'T' {
			return fmt.Errorf("TRON 钱包地址格式不正确，应以 T 开头，34 位字符")
		}
	case WithdrawAccountTypePolygon, WithdrawAccountTypeBSC, WithdrawAccountTypeArbitrum:
		// EVM 兼容地址: 0x 开头，42 字符
		if len(accountID) != 42 || accountID[:2] != "0x" {
			return fmt.Errorf("钱包地址格式不正确，应以 0x 开头，42 位字符")
		}
	case WithdrawAccountTypePayPal:
		// PayPal 邮箱验证（简单验证）
		if !isValidEmail(accountID) {
			return fmt.Errorf("PayPal 邮箱格式不正确")
		}
	}

	return nil
}

// isValidEmail 简单邮箱验证
func isValidEmail(email string) bool {
	// 简单验证：包含 @ 且 @ 后有 .
	atIndex := -1
	for i, c := range email {
		if c == '@' {
			atIndex = i
			break
		}
	}
	if atIndex <= 0 || atIndex >= len(email)-1 {
		return false
	}
	// @ 后面要有 .
	for i := atIndex + 1; i < len(email); i++ {
		if email[i] == '.' && i > atIndex+1 && i < len(email)-1 {
			return true
		}
	}
	return false
}

// api_set_default_withdraw_account 设置默认提现账户
func api_set_default_withdraw_account(c *gin.Context) {
	userID := ReqUserID(c)
	accountID := c.Param("id")

	// 查找账户
	var account WithdrawAccount
	if err := db.Get().Where(&WithdrawAccount{UserID: userID}).
		First(&account, accountID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, 404, "提现账户不存在")
		} else {
			Error(c, 500, "查询提现账户失败")
		}
		return
	}

	// 使用事务更新
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		// 取消其他默认账户
		var emptyAccount WithdrawAccount
		emptyAccount.IsDefault = BoolPtr(false)
		if err := tx.Model(&WithdrawAccount{}).
			Where(&WithdrawAccount{UserID: userID}).
			Updates(&emptyAccount).Error; err != nil {
			return err
		}

		// 设置新的默认账户
		account.IsDefault = BoolPtr(true)
		if err := tx.Model(&account).
			Select("IsDefault").
			Updates(&account).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		log.Errorf(c, "设置默认提现账户失败: %v", err)
		Error(c, 500, "设置默认提现账户失败")
		return
	}

	Success(c, &gin.H{})
}

// api_delete_withdraw_account 删除提现账户
func api_delete_withdraw_account(c *gin.Context) {
	userID := ReqUserID(c)
	accountID := c.Param("id")

	// 查找账户
	var account WithdrawAccount
	if err := db.Get().Where(&WithdrawAccount{UserID: userID}).
		First(&account, accountID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, 404, "提现账户不存在")
		} else {
			Error(c, 500, "查询提现账户失败")
		}
		return
	}

	// 软删除
	if err := db.Get().Delete(&account).Error; err != nil {
		log.Errorf(c, "删除提现账户失败: %v", err)
		Error(c, 500, "删除提现账户失败")
		return
	}

	Success(c, &gin.H{})
}

// api_get_withdraw_requests 获取提现申请列表
func api_get_withdraw_requests(c *gin.Context) {
	userID := ReqUserID(c)

	// 分页参数
	pagination := PaginationFromRequest(c)

	// 查询条件
	query := db.Get().Where(&Withdraw{UserID: userID})

	// 状态过滤
	if status := c.Query("status"); status != "" {
		query = query.Where(&Withdraw{Status: WithdrawStatus(status)})
	}

	// 查询总数
	var total int64
	if err := query.Model(&Withdraw{}).Count(&total).Error; err != nil {
		Error(c, 500, "查询失败")
		return
	}

	// 查询记录
	var withdraws []Withdraw
	if err := query.Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Preload("WithdrawAccount").
		Find(&withdraws).Error; err != nil {
		Error(c, 500, "查询失败")
		return
	}

	pagination.Total = total
	List(c, withdraws, pagination)
}

// api_create_withdraw_request 创建提现申请
func api_create_withdraw_request(c *gin.Context) {
	userID := ReqUserID(c)

	var req CreateWithdrawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, 422, fmt.Sprintf("参数错误: %v", err))
		return
	}

	// 确保钱包存在（自动创建）
	wallet, err := GetOrCreateWallet(c, userID)
	if err != nil {
		log.Errorf(c, "获取钱包失败: %v", err)
		Error(c, 500, "获取钱包失败")
		return
	}

	// 使用事务处理
	var withdraw Withdraw
	err = db.Get().Transaction(func(tx *gorm.DB) error {
		// 重新查询钱包（在事务中）
		var walletInTx Wallet
		if err := tx.First(&walletInTx, wallet.ID).Error; err != nil {
			return fmt.Errorf("查询钱包失败")
		}

		// 验证提现金额（实时计算可用余额）
		if err := ValidateWithdrawAmount(c, walletInTx.ID, req.Amount); err != nil {
			return err
		}

		// 查找提现账户
		var account WithdrawAccount
		if err := tx.Where(&WithdrawAccount{UserID: userID}).
			First(&account, req.WithdrawAccountID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("提现账户不存在")
			}
			return fmt.Errorf("查询提现账户失败")
		}

		// 计算手续费
		feeAmount := calculateWithdrawFee(account.AccountType, req.Amount)
		netAmount := req.Amount - feeAmount

		// 创建提现申请（含手续费）
		withdraw = Withdraw{
			UserID:            userID,
			WalletID:          walletInTx.ID,
			Amount:            req.Amount,
			FeeAmount:         feeAmount,
			NetAmount:         netAmount,
			WithdrawAccountID: account.ID,
			AccountType:       account.AccountType,
			AccountID:         account.AccountID,
			Currency:          account.Currency,
			Status:            WithdrawStatusPending,
			Remark:            req.UserRemark,
		}

		if err := tx.Create(&withdraw).Error; err != nil {
			return fmt.Errorf("创建提现申请失败")
		}

		// 记录变动前余额
		balanceBefore := walletInTx.Balance

		// 扣减余额（立即扣款）
		if err := tx.Model(&walletInTx).
			Update("balance", gorm.Expr("balance - ?", req.Amount)).
			Error; err != nil {
			return fmt.Errorf("扣减余额失败")
		}

		// 记录钱包变动
		change := WalletChange{
			WalletID:      walletInTx.ID,
			Type:          WalletChangeTypeWithdraw,
			Amount:        -req.Amount, // 负数
			BalanceBefore: balanceBefore,
			BalanceAfter:  balanceBefore - req.Amount,
			FrozenUntil:   nil, // 提现不需要冻结期
			WithdrawID:    &withdraw.ID,
			Remark:        fmt.Sprintf("提现 - %s %s", account.AccountType, account.Currency),
		}

		if err := tx.Create(&change).Error; err != nil {
			return fmt.Errorf("记录钱包变动失败")
		}

		// 更新账户使用统计
		now := time.Now()
		if err := tx.Model(&account).
			Update("withdraw_count", gorm.Expr("withdraw_count + 1")).
			Update("last_used_at", now).
			Error; err != nil {
			return fmt.Errorf("更新账户统计失败")
		}

		return nil
	})

	if err != nil {
		log.Errorf(c, "创建提现申请失败: %v", err)
		Error(c, 500, err.Error())
		return
	}

	Success(c, &withdraw)
}

// calculateWithdrawFee 计算提现手续费（美分）
// 可以根据不同渠道设置不同费率
func calculateWithdrawFee(accountType WithdrawAccountType, amount int64) int64 {
	switch accountType {
	case WithdrawAccountTypeTron:
		// TRON: 固定 1 USD
		return 100
	case WithdrawAccountTypePolygon, WithdrawAccountTypeBSC, WithdrawAccountTypeArbitrum:
		// 其他链: 固定 0.5 USD
		return 50
	case WithdrawAccountTypePayPal:
		// PayPal: 2.9% + $0.30（简化为 3%，最低 $0.30）
		fee := amount * 3 / 100
		if fee < 30 {
			fee = 30
		}
		return fee
	default:
		return 0
	}
}
