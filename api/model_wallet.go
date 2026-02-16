package center

import (
	"time"

	"gorm.io/gorm"
)

// ==================== 钱包系统 ====================

// Wallet 用户钱包模型
type Wallet struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deletedAt,omitempty"`

	// 用户关联
	UserID uint64 `gorm:"uniqueIndex;not null" json:"userId"`
	User   *User  `gorm:"foreignKey:UserID" json:"user,omitempty"`

	// 余额（美分）
	Balance        int64 `gorm:"not null;default:0" json:"balance"`        // 总余额
	TotalIncome    int64 `gorm:"not null;default:0" json:"totalIncome"`    // 累计收入
	TotalWithdrawn int64 `gorm:"not null;default:0" json:"totalWithdrawn"` // 累计提现

	// 以下字段通过实时计算获得，不存储在数据库中
	AvailableBalance int64 `gorm:"-" json:"availableBalance"` // 可用余额（实时计算）
	FrozenBalance    int64 `gorm:"-" json:"frozenBalance"`    // 冻结余额（实时计算）

	// 并发控制
	Version int64 `gorm:"not null;default:0" json:"version"` // 乐观锁
}

// WalletChangeType 钱包变动类型
type WalletChangeType string

const (
	WalletChangeTypeIncome   WalletChangeType = "income"   // 收入（返现），有冻结期
	WalletChangeTypeWithdraw WalletChangeType = "withdraw" // 提现（扣款），无冻结期
	WalletChangeTypeRefund   WalletChangeType = "refund"   // 退款（订单退款，返现作废）
)

// WalletChange 钱包变动记录（替代 RetailerCashback）
type WalletChange struct {
	ID        uint64    `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`

	// 钱包关联
	WalletID uint64  `gorm:"not null;index:idx_wallet_type" json:"walletId"`
	Wallet   *Wallet `gorm:"foreignKey:WalletID" json:"wallet,omitempty"`

	// 变动信息
	Type   WalletChangeType `gorm:"type:varchar(30);not null;index:idx_wallet_type;uniqueIndex:idx_type_order" json:"type"` // 变动类型
	Amount int64            `gorm:"not null" json:"amount"`                                                                   // 变动金额（美分，正数=增加，负数=减少）

	// 余额快照
	BalanceBefore int64 `gorm:"not null" json:"balanceBefore"` // 变动前总余额
	BalanceAfter  int64 `gorm:"not null" json:"balanceAfter"`  // 变动后总余额

	// 冻结期（仅 income 类型使用，用于计算冻结余额）
	// income: frozen_until = now + 30 days (冻结期)
	// withdraw: frozen_until = NULL (不冻结)
	// refund: frozen_until = NULL (不冻结)
	FrozenUntil *time.Time `gorm:"index:idx_frozen_until" json:"frozenUntil,omitempty"` // 冻结到期时间

	// 关联信息
	OrderID     *uint64 `gorm:"index:idx_order;uniqueIndex:idx_type_order" json:"orderId,omitempty"`   // 订单ID（income/refund 类型）
	WithdrawID  *uint64 `gorm:"index:idx_withdraw" json:"withdrawId,omitempty"`                        // 提现ID（withdraw 类型）
	ParentID    *uint64 `gorm:"index:idx_parent" json:"parentId,omitempty"`                            // 父记录ID（refund 关联 income）
	Remark      string  `gorm:"type:varchar(500)" json:"remark,omitempty"`                             // 备注
	OperatorID  *uint64 `json:"operatorId,omitempty"`                                                  // 操作员ID（人工调整）
}

// TableName 指定表名
func (WalletChange) TableName() string {
	return "wallet_changes"
}

// BeforeCreate GORM hook
func (wc *WalletChange) BeforeCreate(tx *gorm.DB) error {
	// 为 type + order_id 创建唯一索引，防止重复返现
	// 注意：这个索引只对 income 和 refund 类型有效（它们有 order_id）
	// withdraw 类型没有 order_id，所以不会冲突
	return nil
}

// ==================== 提现账户 ====================

// WithdrawAccountType 提现渠道类型
// 设计说明：渠道类型决定地址格式验证和手续费
type WithdrawAccountType string

const (
	// 加密货币渠道（按网络区分）
	WithdrawAccountTypeTron     WithdrawAccountType = "tron"     // TRON 网络 - 手续费最低，推荐
	WithdrawAccountTypePolygon  WithdrawAccountType = "polygon"  // Polygon 网络 - 手续费低
	WithdrawAccountTypeBSC      WithdrawAccountType = "bsc"      // BSC 网络 - 手续费低
	WithdrawAccountTypeArbitrum WithdrawAccountType = "arbitrum" // Arbitrum 网络 - 手续费低

	// 传统支付渠道
	WithdrawAccountTypePayPal WithdrawAccountType = "paypal" // PayPal - 传统支付方式
)

// Currency 币种（打款时需要知道具体币种）
type Currency string

const (
	CurrencyUSDT Currency = "usdt" // USDT - 加密货币稳定币
	CurrencyUSDC Currency = "usdc" // USDC - 加密货币稳定币
	CurrencyUSD  Currency = "usd"  // USD - 美元（PayPal 默认）
)

// IsBlockchainAccount 判断是否为区块链账户（加密货币）
func (t WithdrawAccountType) IsBlockchainAccount() bool {
	switch t {
	case WithdrawAccountTypeTron, WithdrawAccountTypePolygon, WithdrawAccountTypeBSC, WithdrawAccountTypeArbitrum:
		return true
	default:
		return false
	}
}

// GetExplorerURL 获取区块链浏览器地址（用于查看钱包地址）
func (t WithdrawAccountType) GetExplorerURL(address string) string {
	switch t {
	case WithdrawAccountTypeTron:
		return "https://tronscan.org/#/address/" + address
	case WithdrawAccountTypePolygon:
		return "https://polygonscan.com/address/" + address
	case WithdrawAccountTypeBSC:
		return "https://bscscan.com/address/" + address
	case WithdrawAccountTypeArbitrum:
		return "https://arbiscan.io/address/" + address
	default:
		return ""
	}
}

// GetTxExplorerURL 获取交易浏览器地址（用于查看交易）
func (t WithdrawAccountType) GetTxExplorerURL(txHash string) string {
	switch t {
	case WithdrawAccountTypeTron:
		return "https://tronscan.org/#/transaction/" + txHash
	case WithdrawAccountTypePolygon:
		return "https://polygonscan.com/tx/" + txHash
	case WithdrawAccountTypeBSC:
		return "https://bscscan.com/tx/" + txHash
	case WithdrawAccountTypeArbitrum:
		return "https://arbiscan.io/tx/" + txHash
	case WithdrawAccountTypePayPal:
		return "https://www.paypal.com/activity/payment/" + txHash
	default:
		return ""
	}
}

// WithdrawAccount 提现账户
// 统一存储各类提现账户：加密货币钱包地址、PayPal 邮箱等
type WithdrawAccount struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deletedAt,omitempty"`

	// 用户关联
	UserID uint64 `gorm:"not null;index:idx_user_type" json:"userId"`
	User   *User  `gorm:"foreignKey:UserID" json:"user,omitempty"`

	// 账户信息
	// AccountType: 渠道类型（tron/polygon/bsc/arbitrum/paypal）
	// AccountID: 收款标识（加密货币=钱包地址，PayPal=邮箱地址）
	AccountType WithdrawAccountType `gorm:"type:varchar(20);not null;index:idx_user_type" json:"accountType"`
	AccountID   string              `gorm:"type:varchar(100);not null;uniqueIndex:idx_unique_account" json:"accountId"`

	// 币种：usdt/usdc（加密货币）或 usd（PayPal）
	// 管理员打款时需要知道具体币种
	Currency Currency `gorm:"type:varchar(10);not null" json:"currency"`

	// 用户自定义标签（如"主账户"、"公司账户"）
	Label string `gorm:"type:varchar(50)" json:"label,omitempty"`

	// 默认账户（每个用户只能有一个默认账户）
	IsDefault *bool `gorm:"default:false;index" json:"isDefault"`

	// 使用统计
	WithdrawCount int64      `gorm:"default:0" json:"withdrawCount"`
	LastUsedAt    *time.Time `json:"lastUsedAt,omitempty"`
}

// TableName 指定表名
func (WithdrawAccount) TableName() string {
	return "withdraw_accounts"
}

// ==================== 提现申请 ====================

// WithdrawStatus 提现状态
type WithdrawStatus string

const (
	WithdrawStatusPending    WithdrawStatus = "pending"    // 待审核
	WithdrawStatusApproved   WithdrawStatus = "approved"   // 已批准
	WithdrawStatusProcessing WithdrawStatus = "processing" // 打款中
	WithdrawStatusCompleted  WithdrawStatus = "completed"  // 已完成
	WithdrawStatusRejected   WithdrawStatus = "rejected"   // 已拒绝
	WithdrawStatusCancelled  WithdrawStatus = "cancelled"  // 已取消
)

// Withdraw 提现申请
type Withdraw struct {
	ID        uint64    `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// 用户和钱包
	UserID   uint64  `gorm:"not null;index:idx_user_status" json:"userId"`
	WalletID uint64  `gorm:"not null;index" json:"walletId"`
	User     *User   `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Wallet   *Wallet `gorm:"foreignKey:WalletID" json:"wallet,omitempty"`

	// 金额信息（单位：美分）
	Amount    int64 `gorm:"not null" json:"amount"`    // 申请提现金额
	FeeAmount int64 `gorm:"not null;default:0" json:"feeAmount"` // 手续费
	NetAmount int64 `gorm:"not null" json:"netAmount"` // 实际到账金额 = Amount - FeeAmount

	// 提现账户（快照，防止账户被删除后丢失信息）
	WithdrawAccountID uint64              `gorm:"not null;index" json:"withdrawAccountId"`
	WithdrawAccount   *WithdrawAccount    `gorm:"foreignKey:WithdrawAccountID" json:"withdrawAccount,omitempty"`
	AccountType       WithdrawAccountType `gorm:"type:varchar(20);not null" json:"accountType"` // 渠道类型快照
	AccountID         string              `gorm:"type:varchar(100);not null" json:"accountId"`  // 收款标识快照（钱包地址/邮箱）
	Currency          Currency            `gorm:"type:varchar(10);not null" json:"currency"`    // 币种快照

	// 状态流程：pending -> approved/rejected -> processing -> completed
	Status WithdrawStatus `gorm:"type:varchar(20);not null;default:'pending';index:idx_user_status" json:"status"`

	// 处理信息（审批 + 打款）
	ProcessedBy *uint64    `gorm:"index" json:"processedBy,omitempty"` // 处理人（审批 + 打款）
	ProcessedAt *time.Time `json:"processedAt,omitempty"`              // 完成时间
	Processor   *User      `gorm:"foreignKey:ProcessedBy" json:"processor,omitempty"`

	// 打款凭证
	// 加密货币：区块链交易哈希（如 0x...）
	// PayPal：PayPal 交易 ID
	TxHash        string `gorm:"type:varchar(100)" json:"txHash,omitempty"`        // 交易凭证
	TxExplorerURL string `gorm:"type:varchar(255)" json:"txExplorerUrl,omitempty"` // 查看链接（自动生成）

	// 拒绝原因
	RejectReason string `gorm:"type:varchar(500)" json:"rejectReason,omitempty"`

	// 备注（用户备注 + 管理员备注）
	Remark string `gorm:"type:varchar(1000)" json:"remark,omitempty"`
}
