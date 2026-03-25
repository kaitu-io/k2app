package center

import (
	"context"
	"fmt"

	db "github.com/wordgate/qtoolkit/db"

	"github.com/wordgate/qtoolkit/log"
)

// Migrate 执行数据库迁移
func Migrate() error {
	ctx := context.Background()
	log.Infof(ctx, "start migrating database...")
	// Pre-migration: add code column as nullable + backfill BEFORE AutoMigrate
	// adds NOT NULL + uniqueIndex. This ensures existing rows get codes first.
	if err := preMigrateLicenseKeyCodes(ctx); err != nil {
		log.Errorf(ctx, "license key pre-migration failed: %v", err)
		return err
	}

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
		// Admin approval system
		&AdminApproval{},
	)
	if err != nil {
		log.Errorf(ctx, "database migration failed: %v", err)
		return err
	}

	log.Infof(ctx, "database migration completed successfully")
	return nil
}

// preMigrateLicenseKeyCodes adds code column (nullable) and backfills existing rows
// BEFORE AutoMigrate adds NOT NULL + uniqueIndex constraints.
// Safe to run multiple times — no-ops if column already has data.
func preMigrateLicenseKeyCodes(ctx context.Context) error {
	d := db.Get()

	// Check if license_keys table exists
	if !d.Migrator().HasTable("license_keys") {
		return nil // fresh install, AutoMigrate will create everything
	}

	// Add code column as nullable if it doesn't exist yet
	if !d.Migrator().HasColumn(&LicenseKey{}, "code") {
		log.Infof(ctx, "[MIGRATE] adding code column to license_keys")
		if err := d.Exec("ALTER TABLE license_keys ADD COLUMN code VARCHAR(8)").Error; err != nil {
			return fmt.Errorf("failed to add code column: %w", err)
		}
	}

	// Backfill any rows missing a code
	var count int64
	if err := d.Raw("SELECT COUNT(*) FROM license_keys WHERE code IS NULL OR code = ''").Scan(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}

	log.Infof(ctx, "[MIGRATE] backfilling %d license keys with short codes", count)
	var keys []LicenseKey
	if err := d.Where("code = '' OR code IS NULL").Find(&keys).Error; err != nil {
		return err
	}
	for i := range keys {
		code, err := GenerateShortCode(ctx)
		if err != nil {
			return fmt.Errorf("failed to generate code for key %d: %w", keys[i].ID, err)
		}
		if err := d.Model(&keys[i]).Update("code", code).Error; err != nil {
			return fmt.Errorf("failed to update key %d with code: %w", keys[i].ID, err)
		}
	}
	log.Infof(ctx, "[MIGRATE] backfilled %d license keys with short codes", count)
	return nil
}
