package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"context"

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
		// Usage analytics
		&StatAppOpen{},
		&StatConnection{},
		&StatK2sDownload{},
		// Admin audit log
		&AdminAuditLog{},
	)
	if err != nil {
		log.Errorf(ctx, "database migration failed: %v", err)
		return err
	}

	// Post-migration: convert access_key from plaintext to SHA-256 hash
	// Safe to re-run: only hashes values that aren't already 64-char hex
	log.Infof(ctx, "post-migration: hashing plaintext access keys...")
	db.Get().Exec("UPDATE users SET access_key = SHA2(access_key, 256) WHERE access_key IS NOT NULL AND access_key != '' AND LENGTH(access_key) < 64")
	db.Get().Exec("UPDATE users SET access_key = NULL WHERE access_key = ''")
	// Drop old non-unique index if it exists (GORM uniqueIndex will recreate)
	db.Get().Exec("DROP INDEX IF EXISTS idx_users_access_key ON users")

	log.Infof(ctx, "database migration completed successfully")
	return nil
}
