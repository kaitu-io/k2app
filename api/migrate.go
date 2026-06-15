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

	// Legacy cleanup: LoginIdentify previously used gorm.DeletedAt; soft-deleted
	// rows occupied the (type, index_id) unique slot forever and blocked email
	// reuse after account deletion (prod incident 2026-05-02). The DeletedAt
	// field has been removed; physically purge any leftover soft-deleted rows
	// before AutoMigrate so they don't reappear as "active" rows under the new
	// query semantics. Idempotent — empty tables return RowsAffected=0.
	if database := db.Get(); database != nil {
		result := database.Exec("DELETE FROM login_identifies WHERE deleted_at IS NOT NULL")
		if result.Error != nil {
			log.Errorf(ctx, "failed to purge soft-deleted login_identifies: %v", result.Error)
			return result.Error
		}
		if result.RowsAffected > 0 {
			log.Infof(ctx, "purged %d legacy soft-deleted login_identifies rows", result.RowsAffected)
		}
	}

	// Plan discriminator rename: kind → product. AutoMigrate cannot rename
	// columns, so do it explicitly here (idempotent). The legacy value
	// "shared_subscription" is remapped to "app". Index rename is best-effort —
	// AutoMigrate recreates idx_plans_product from the model tag if absent.
	if database := db.Get(); database != nil {
		m := database.Migrator()
		if m.HasColumn(&Plan{}, "kind") && !m.HasColumn(&Plan{}, "product") {
			if err := m.RenameColumn(&Plan{}, "kind", "Product"); err != nil {
				log.Errorf(ctx, "failed to rename plans.kind→product: %v", err)
				return err
			}
		}
		if m.HasIndex(&Plan{}, "idx_plans_kind") && !m.HasIndex(&Plan{}, "idx_plans_product") {
			_ = m.RenameIndex(&Plan{}, "idx_plans_kind", "idx_plans_product")
		}
		// Remap the legacy value only when the column already exists. On a fresh
		// DB the plans table is created by AutoMigrate below (with default 'app'),
		// so this UPDATE must be skipped to avoid "table doesn't exist".
		if m.HasColumn(&Plan{}, "product") {
			if r := database.Exec("UPDATE plans SET product = 'app' WHERE product = 'shared_subscription'"); r.Error != nil {
				log.Errorf(ctx, "failed to remap plan product value: %v", r.Error)
				return r.Error
			}
		}
	}

	err := db.Get().AutoMigrate(
		&Plan{},
		&User{},
		&LoginIdentify{},
		&Device{},
		&InviteCode{},
		&Order{},
		&UserProHistory{},
		&Subscription{},
		&SubscriptionCredit{},
		&Message{},
		&Secret{},
		&SlaveNode{},
		&SlaveTunnel{},
		&SlaveNodeLoad{},
		&PrivateNodeSubscription{},
		&PrivateNodePlanSpec{},
		&NodeOperation{},
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

	log.Infof(ctx, "database migration completed successfully")
	return nil
}
