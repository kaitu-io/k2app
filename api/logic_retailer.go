package center

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// ==================== CLI 命令支持函数 ====================

// RetailerInfo 分销商信息（CLI 输出用）
type RetailerInfo struct {
	UserID            uint64
	Email             string
	IsRetailer        bool
	Level             int
	LevelName         string
	FirstOrderPercent int
	RenewalPercent    int
	PaidUserCount     int
}

// GetRetailerInfoByEmail 通过邮箱获取分销商信息
func GetRetailerInfoByEmail(ctx context.Context, email string) (*RetailerInfo, error) {
	email = strings.ToLower(email)
	indexID := secretHashIt(ctx, []byte(email))

	// 查找用户
	var identify LoginIdentify
	if err := db.Get().Where("type = ? AND index_id = ?", "email", indexID).
		Preload("User").First(&identify).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("user with email %s not found", email)
		}
		return nil, fmt.Errorf("failed to find user: %v", err)
	}

	user := identify.User
	info := &RetailerInfo{
		UserID:     user.ID,
		Email:      email,
		IsRetailer: user.IsRetailer != nil && *user.IsRetailer,
	}

	// 如果是分销商，获取配置
	if info.IsRetailer {
		var config RetailerConfig
		if err := db.Get().Where(&RetailerConfig{UserID: user.ID}).First(&config).Error; err == nil {
			info.Level = config.Level
			info.LevelName = config.GetLevelInfo().Name
			info.FirstOrderPercent = config.FirstOrderPercent
			info.RenewalPercent = config.RenewalPercent
			info.PaidUserCount = config.PaidUserCount
		} else {
			// 没有配置，使用默认 L1
			info.Level = RetailerLevelReferrer
			info.LevelName = RetailerLevelConfig[RetailerLevelReferrer].Name
			info.FirstOrderPercent = RetailerLevelConfig[RetailerLevelReferrer].FirstOrderPct
			info.RenewalPercent = RetailerLevelConfig[RetailerLevelReferrer].RenewalPct
		}
	}

	return info, nil
}

// SetRetailerLevelByEmail 通过邮箱设置分销商等级
// 如果用户不存在会创建用户，如果不是分销商会先设置为分销商
func SetRetailerLevelByEmail(ctx context.Context, email string, level int) error {
	// 验证等级有效性
	_, ok := RetailerLevelConfig[level]
	if !ok {
		return fmt.Errorf("invalid level: %d (must be 1-4)", level)
	}

	// 1. 查找或创建用户（复用现有逻辑）
	user, err := FindOrCreateUserByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("failed to find or create user: %v", err)
	}

	// 2. 确保用户是分销商（复用现有逻辑）
	if user.IsRetailer == nil || !*user.IsRetailer {
		if err := SetUserRetailerStatus(ctx, email, true); err != nil {
			return fmt.Errorf("failed to set retailer status: %v", err)
		}
	}

	// 3. 获取或创建分销商配置并设置等级
	config, err := GetOrCreateRetailerConfig(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("failed to get retailer config: %v", err)
	}

	// 4. 更新等级
	if config.Level != level {
		return UpgradeRetailerLevel(ctx, config.ID, level, "manual_set_via_cli", nil)
	}

	return nil
}

// ListRetailers 列出所有分销商
// level: 0 表示全部，1-4 表示特定等级
func ListRetailers(ctx context.Context, level int) ([]RetailerInfo, error) {
	var configs []RetailerConfig

	query := db.Get().Preload("User").Preload("User.LoginIdentifies")
	if level > 0 {
		query = query.Where("level = ?", level)
	}

	if err := query.Find(&configs).Error; err != nil {
		return nil, fmt.Errorf("failed to list retailers: %v", err)
	}

	var results []RetailerInfo
	for _, config := range configs {
		if config.User == nil {
			continue
		}

		// 获取邮箱
		email := ""
		for _, identify := range config.User.LoginIdentifies {
			if identify.Type == "email" {
				decrypted, err := secretDecryptString(ctx, identify.EncryptedValue)
				if err == nil {
					email = decrypted
				}
				break
			}
		}

		results = append(results, RetailerInfo{
			UserID:            config.UserID,
			Email:             email,
			IsRetailer:        true,
			Level:             config.Level,
			LevelName:         config.GetLevelInfo().Name,
			FirstOrderPercent: config.FirstOrderPercent,
			RenewalPercent:    config.RenewalPercent,
			PaidUserCount:     config.PaidUserCount,
		})
	}

	return results, nil
}

// ==================== 分销商配置管理 ====================

// GetOrCreateRetailerConfig 获取或创建分销商配置（非事务版本）
// 如果配置不存在，则自动创建一个默认配置（L1等级）
func GetOrCreateRetailerConfig(ctx context.Context, userID uint64) (*RetailerConfig, error) {
	return getOrCreateRetailerConfigInTx(ctx, db.Get(), userID)
}

// getOrCreateRetailerConfigInTx 获取或创建分销商配置（事务版本）
func getOrCreateRetailerConfigInTx(ctx context.Context, tx *gorm.DB, userID uint64) (*RetailerConfig, error) {
	var config RetailerConfig

	// 1. 尝试查询现有配置
	err := tx.Where(&RetailerConfig{UserID: userID}).First(&config).Error
	if err == nil {
		// 配置存在，直接返回
		log.Debugf(ctx, "[GetOrCreateRetailerConfig] found existing config for user %d: level=%d, firstOrder=%d%%, renewal=%d%%",
			userID, config.Level, config.FirstOrderPercent, config.RenewalPercent)
		return &config, nil
	}

	// 2. 如果不是 "记录不存在" 错误，返回错误
	if err != gorm.ErrRecordNotFound {
		log.Errorf(ctx, "[GetOrCreateRetailerConfig] failed to query config for user %d: %v", userID, err)
		return nil, err
	}

	// 3. 记录不存在，创建 L1 等级的默认配置
	log.Infof(ctx, "[GetOrCreateRetailerConfig] creating L1 config for user %d", userID)

	// L1 等级配置
	l1Config := RetailerLevelConfig[RetailerLevelReferrer]

	config = RetailerConfig{
		UserID:            userID,
		Level:             RetailerLevelReferrer,
		PaidUserCount:     0,
		FirstOrderPercent: l1Config.FirstOrderPct,
		RenewalPercent:    l1Config.RenewalPct,
		// 兼容旧字段
		CashbackPercent: l1Config.FirstOrderPct,
		CashbackRule:    "first_order",
	}

	if err := tx.Create(&config).Error; err != nil {
		log.Errorf(ctx, "[GetOrCreateRetailerConfig] failed to create config for user %d: %v", userID, err)
		return nil, err
	}

	log.Infof(ctx, "[GetOrCreateRetailerConfig] created L1 config for user %d: firstOrder=%d%%, renewal=%d%%",
		userID, config.FirstOrderPercent, config.RenewalPercent)
	return &config, nil
}

// ==================== 分销商等级升级 ====================

// incrementPaidUserCountInTx 增加付费用户计数，并检查自动升级
func incrementPaidUserCountInTx(ctx context.Context, tx *gorm.DB, retailerConfigID uint64) error {
	// 1. 增加计数
	if err := tx.Model(&RetailerConfig{}).
		Where("id = ?", retailerConfigID).
		Update("paid_user_count", gorm.Expr("paid_user_count + 1")).Error; err != nil {
		return fmt.Errorf("增加付费用户计数失败: %v", err)
	}

	// 2. 重新查询配置
	var config RetailerConfig
	if err := tx.First(&config, retailerConfigID).Error; err != nil {
		return fmt.Errorf("查询分销商配置失败: %v", err)
	}

	// 3. 检查自动升级（仅 L1→L2）
	if config.CanAutoUpgrade() {
		log.Infof(ctx, "[IncrementPaidUserCount] retailer %d reached %d users, auto upgrading to L2",
			retailerConfigID, config.PaidUserCount)
		return upgradeRetailerLevelInTx(ctx, tx, &config, RetailerLevelRetailer, "auto_upgrade", nil)
	}

	return nil
}

// UpgradeRetailerLevel 升级分销商等级（管理员手动操作）
func UpgradeRetailerLevel(ctx context.Context, retailerConfigID uint64, newLevel int, reason string, adminID *uint64) error {
	return db.Get().Transaction(func(tx *gorm.DB) error {
		var config RetailerConfig
		if err := tx.First(&config, retailerConfigID).Error; err != nil {
			return fmt.Errorf("查询分销商配置失败: %v", err)
		}
		return upgradeRetailerLevelInTx(ctx, tx, &config, newLevel, reason, adminID)
	})
}

// upgradeRetailerLevelInTx 在事务中执行等级升级
func upgradeRetailerLevelInTx(ctx context.Context, tx *gorm.DB, config *RetailerConfig, newLevel int, reason string, adminID *uint64) error {
	// 验证等级有效性
	levelInfo, ok := RetailerLevelConfig[newLevel]
	if !ok {
		return fmt.Errorf("无效的等级: %d", newLevel)
	}

	oldLevel := config.Level

	// 如果等级相同，跳过
	if oldLevel == newLevel {
		log.Debugf(ctx, "[upgradeRetailerLevelInTx] retailer %d already at level %d, skipping", config.ID, newLevel)
		return nil
	}

	// 1. 更新分销商配置
	if err := tx.Model(config).Updates(map[string]interface{}{
		"level":               newLevel,
		"first_order_percent": levelInfo.FirstOrderPct,
		"renewal_percent":     levelInfo.RenewalPct,
		// 同步更新旧字段以保持兼容
		"cashback_percent": levelInfo.FirstOrderPct,
	}).Error; err != nil {
		return fmt.Errorf("更新分销商等级失败: %v", err)
	}

	// 2. 记录等级变更历史
	history := RetailerLevelHistory{
		RetailerConfigID: config.ID,
		OldLevel:         oldLevel,
		NewLevel:         newLevel,
		Reason:           reason,
		AdminID:          adminID,
	}
	if err := tx.Create(&history).Error; err != nil {
		return fmt.Errorf("记录等级变更历史失败: %v", err)
	}

	log.Infof(ctx, "[upgradeRetailerLevelInTx] retailer %d upgraded from L%d to L%d (%s), firstOrder=%d%%, renewal=%d%%",
		config.ID, oldLevel, newLevel, reason, levelInfo.FirstOrderPct, levelInfo.RenewalPct)

	// 3. TODO: 发送等级变更邮件通知
	// go SendLevelChangeEmail(ctx, config.UserID, oldLevel, newLevel)

	return nil
}

// ==================== 数据转换辅助函数 ====================

// ToDataRetailerConfig 将 RetailerConfig 转换为 API 返回的 DataRetailerConfig
// 注意：此版本不解密联系方式，如需解密请使用 ToDataRetailerConfigWithContext
func ToDataRetailerConfig(config *RetailerConfig) *DataRetailerConfig {
	return ToDataRetailerConfigWithContext(context.TODO(), config)
}

// ToDataRetailerConfigWithContext 将 RetailerConfig 转换为 API 返回的 DataRetailerConfig
// 带 context 版本，支持解密联系方式
func ToDataRetailerConfigWithContext(ctx context.Context, config *RetailerConfig) *DataRetailerConfig {
	if config == nil {
		return nil
	}

	levelInfo := config.GetLevelInfo()
	data := &DataRetailerConfig{
		Level:             config.Level,
		LevelName:         levelInfo.Name,
		FirstOrderPercent: config.FirstOrderPercent,
		RenewalPercent:    config.RenewalPercent,
		PaidUserCount:     config.PaidUserCount,
		ContentProof:      config.ContentProof,
	}

	// 内容审核时间
	if config.ContentVerifiedAt != nil {
		ts := config.ContentVerifiedAt.Unix()
		data.ContentVerifiedAt = &ts
	}

	// 解密联系方式
	if config.Contacts != "" && ctx != nil {
		decrypted, err := secretDecryptString(ctx, config.Contacts)
		if err == nil && decrypted != "" {
			var contacts []ContactInfo
			if jsonErr := json.Unmarshal([]byte(decrypted), &contacts); jsonErr == nil {
				data.Contacts = contacts
			}
		}
	}

	// 计算升级进度
	nextLevelInfo := config.GetNextLevelInfo()
	if nextLevelInfo != nil {
		nextLevel := config.Level + 1
		data.NextLevel = &nextLevel
		data.NextLevelName = nextLevelInfo.Name
		data.NextLevelRequirement = &nextLevelInfo.RequiredUsers
		data.NeedContentProof = nextLevelInfo.NeedContentProof

		// 计算进度百分比
		if nextLevelInfo.RequiredUsers > 0 {
			data.ProgressPercent = config.PaidUserCount * 100 / nextLevelInfo.RequiredUsers
			if data.ProgressPercent > 100 {
				data.ProgressPercent = 100
			}
		}
	}

	return data
}

// ==================== 查询辅助函数 ====================

// isUserFirstPaidOrderInTx 检查是否为该用户在该分销商下的首个已支付订单（事务内）
// 用于判断应该使用首单分成还是续费分成
func isUserFirstPaidOrderInTx(tx *gorm.DB, userID uint64, retailerID uint64, currentOrderID uint64) bool {
	var count int64
	tx.Model(&Order{}).
		Where("user_id = ? AND retailer_id = ? AND is_paid = ? AND id < ?", userID, retailerID, true, currentOrderID).
		Count(&count)
	return count == 0 // 如果之前没有已支付订单，则当前订单是首单
}

// GetRetailerLevelHistory 获取分销商等级变更历史
func GetRetailerLevelHistory(ctx context.Context, retailerConfigID uint64) ([]RetailerLevelHistory, error) {
	var history []RetailerLevelHistory
	err := db.Get().Where(&RetailerLevelHistory{RetailerConfigID: retailerConfigID}).
		Order("created_at DESC").
		Find(&history).Error
	return history, err
}

// ==================== 分销商分成计算 ====================

// processRetailerCashbackInTx 处理订单的分销商分成
// 支持 L 级别分成体系：首单使用 FirstOrderPercent，续费使用 RenewalPercent
// 此函数应在订单支付事务中调用，确保原子性
// 分销商关系通过邀请码链条确定：User.InvitedByCodeID → InviteCode.UserID → User(IsRetailer=true)
func processRetailerCashbackInTx(ctx context.Context, tx *gorm.DB, orderID uint64) error {
	// 1. 查询订单
	var order Order
	if err := tx.First(&order, orderID).Error; err != nil {
		return fmt.Errorf("查询订单失败: %v", err)
	}

	// 检查订单是否已支付
	if order.IsPaid == nil || !*order.IsPaid {
		return fmt.Errorf("订单未支付，无法返现")
	}

	// 2. 通过邀请码链条查找分销商
	// User.InvitedByCodeID → InviteCode.UserID → User(IsRetailer=true)
	var user User
	if err := tx.Preload("InvitedByCode.User").First(&user, order.UserID).Error; err != nil {
		log.Errorf(ctx, "[ProcessRetailerCashback] 查询用户失败: %v", err)
		return nil
	}

	// 检查用户是否有邀请码
	if user.InvitedByCode == nil || user.InvitedByCode.User == nil {
		log.Infof(ctx, "[ProcessRetailerCashback] 订单 %d 用户没有邀请码，跳过返现", orderID)
		return nil
	}

	// 检查邀请码所有者是否为分销商
	inviter := user.InvitedByCode.User
	if inviter.IsRetailer == nil || !*inviter.IsRetailer {
		log.Infof(ctx, "[ProcessRetailerCashback] 订单 %d 邀请人不是分销商，跳过返现", orderID)
		return nil
	}

	retailerID := inviter.ID

	// 3. 获取或创建分销商配置（使用事务版本）
	retailerConfig, err := getOrCreateRetailerConfigInTx(ctx, tx, retailerID)
	if err != nil {
		return fmt.Errorf("获取分销商配置失败: %v", err)
	}

	// 4. 判断是首单还是续费，选择对应的分成比例
	isFirstOrder := isUserFirstPaidOrderInTx(tx, order.UserID, retailerID, orderID)

	var cashbackPercent int
	var orderType string
	if isFirstOrder {
		cashbackPercent = retailerConfig.FirstOrderPercent
		orderType = "首单"

		// 首单时增加分销商的付费用户计数（同步执行，保证事务一致性）
		if err := incrementPaidUserCountInTx(ctx, tx, retailerConfig.ID); err != nil {
			log.Errorf(ctx, "[ProcessRetailerCashback] 增加付费用户计数失败: %v", err)
			// 计数失败不影响主流程，仅记录错误
		}
	} else {
		cashbackPercent = retailerConfig.RenewalPercent
		orderType = "续费"
	}

	// 5. 如果分成比例为0，跳过（L1等级续费分成为0）
	if cashbackPercent <= 0 {
		log.Infof(ctx, "[ProcessRetailerCashback] 订单 %d (%s) 分成比例为0%%，跳过返现", orderID, orderType)
		return nil
	}

	// 6. 计算返现金额（返现基数 = 实际支付金额）
	cashbackAmount := int64(order.PayAmount) * int64(cashbackPercent) / 100

	if cashbackAmount <= 0 {
		log.Infof(ctx, "[ProcessRetailerCashback] 订单 %d 返现金额为0，跳过", orderID)
		return nil
	}

	// 7. 发放返现到钱包（数据库唯一索引 idx_type_order 防止重复）
	freezeDays := 30 // 冻结期 30 天
	remark := fmt.Sprintf("%s返现 - 订单ID: %s, L%d等级, 比例: %d%%",
		orderType, order.UUID, retailerConfig.Level, cashbackPercent)

	if err := addCashbackIncomeInTx(ctx, tx, retailerID, orderID, cashbackAmount, freezeDays, remark); err != nil {
		return fmt.Errorf("发放返现到钱包失败: %v", err)
	}

	log.Infof(ctx, "[ProcessRetailerCashback] 订单返现成功: order_id=%d, retailer_id=%d, type=%s, level=L%d, amount=%d, percent=%d%%",
		orderID, retailerID, orderType, retailerConfig.Level, cashbackAmount, cashbackPercent)

	return nil
}
