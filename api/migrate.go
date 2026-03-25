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

	if err := backfillLicenseKeyCodes(ctx); err != nil {
		log.Errorf(ctx, "license key backfill failed: %v", err)
		return err
	}

	log.Infof(ctx, "database migration completed successfully")
	return nil
}

func backfillLicenseKeyCodes(ctx context.Context) error {
	var keys []LicenseKey
	if err := db.Get().Where("code = '' OR code IS NULL").Find(&keys).Error; err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	log.Infof(ctx, "[MIGRATE] backfilling %d license keys with short codes", len(keys))
	for i := range keys {
		code, err := GenerateShortCode(ctx)
		if err != nil {
			return fmt.Errorf("failed to generate code for key %d: %w", keys[i].ID, err)
		}
		if err := db.Get().Model(&keys[i]).Update("code", code).Error; err != nil {
			return fmt.Errorf("failed to update key %d with code: %w", keys[i].ID, err)
		}
	}
	log.Infof(ctx, "[MIGRATE] backfilled %d license keys with short codes", len(keys))
	return nil
}
