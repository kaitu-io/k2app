package center

import (
	"context"

	db "github.com/wordgate/qtoolkit/db"

	"github.com/wordgate/qtoolkit/log"
)

// Migrate 执行数据库迁移
func Migrate() error {
	ctx := context.Background()
	log.Infof(ctx, "start migrating database...")
	err := db.Get().AutoMigrate(
		&Plan{},
		&User{},
		&LoginIdentify{},
		&Device{},
		&InviteCode{},
		&Order{},
		&UserProHistory{},
		&Message{},
		&Secret{},
		&SlaveNode{},
		&SlaveTunnel{},
		&SlaveNodeLoad{},
		&SessionAcct{},
		&Campaign{},
		&LicenseKeyBatch{},
		&LicenseKey{},
		&RetailerConfig{},
		&RetailerLevelHistory{},
		&EmailMarketingTemplate{},
		&Wallet{},
		&WalletChange{},
		&WithdrawAccount{},
		&Withdraw{},
		&EmailSendLog{},
		// 推送通知系统
		&PushToken{},
		// ECH 密钥管理
		&ECHKey{},
		// 分销商沟通记录
		&RetailerNote{},
		// Strategy system
		&StrategyRules{},
		&TelemetryEvent{},
		&TelemetryRateLimit{},
		// Route diagnosis
		&IPRouteInfo{},
		// Cloud instance management
		&CloudInstance{},
		// Device log & feedback ticket
		&DeviceLog{},
		&FeedbackTicket{},
		&TicketReply{},
		// Connection quality ratings
		&ConnectionRating{},
		// Usage analytics
		&StatAppOpen{},
		&StatConnection{},
		&StatK2sDownload{},
		// Admin audit log
		&AdminAuditLog{},
		// Admin approval system
		&AdminApproval{},
		// Survey system
		&SurveyResponse{},
		// Announcement system
		&Announcement{},
	)
	if err != nil {
		log.Errorf(ctx, "database migration failed: %v", err)
		return err
	}

	// === 2026-04-20: Tier rename + 单一事实源迁移 ===
	// 注：GORM AutoMigrate 不会修改已有列的 DEFAULT，必须手动 ALTER。
	// 这些操作均为幂等（重复执行无副作用）。
	log.Infof(ctx, "[migrate] running tier rename migrations...")

	// (1) 修改默认值 'pro' → 'basic'
	if err := db.Get().Exec("ALTER TABLE plans MODIFY COLUMN tier VARCHAR(30) NOT NULL DEFAULT 'basic'").Error; err != nil {
		log.Errorf(ctx, "[migrate] alter plans.tier default failed: %v", err)
		return err
	}
	if err := db.Get().Exec("ALTER TABLE users MODIFY COLUMN tier VARCHAR(30) NOT NULL DEFAULT 'basic'").Error; err != nil {
		log.Errorf(ctx, "[migrate] alter users.tier default failed: %v", err)
		return err
	}

	// (2) 回填存量数据：pro/空/NULL → basic
	if err := db.Get().Exec("UPDATE plans SET tier='basic' WHERE tier IN ('pro', '') OR tier IS NULL").Error; err != nil {
		log.Errorf(ctx, "[migrate] backfill plans.tier failed: %v", err)
		return err
	}
	if err := db.Get().Exec("UPDATE users SET tier='basic' WHERE tier IN ('pro', '') OR tier IS NULL").Error; err != nil {
		log.Errorf(ctx, "[migrate] backfill users.tier failed: %v", err)
		return err
	}

	log.Infof(ctx, "[migrate] tier rename migrations completed")

	// Clean up unused legacy license keys without a batch (test data from pre-batch era)
	db.Get().Where("batch_id = 0 AND is_used = false").Delete(&LicenseKey{})

	log.Infof(ctx, "database migration completed successfully")
	return nil
}
