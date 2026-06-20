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

	// NodeUsage NodeID→Ipv4 re-key (one-time, idempotent). The table is a
	// disposable mirror — every node repopulates it within one report interval
	// from its durable traffic.state — so drop+recreate beats backfilling. Guard
	// fires only before the ipv4 column exists, so reruns/fresh DBs are no-ops.
	if database := db.Get(); database != nil {
		if database.Migrator().HasTable(&NodeUsage{}) && !database.Migrator().HasColumn(&NodeUsage{}, "ipv4") {
			if err := database.Migrator().DropTable(&NodeUsage{}); err != nil {
				log.Errorf(ctx, "failed to drop node_usages for ipv4 re-key: %v", err)
				return err
			}
			log.Infof(ctx, "dropped node_usages for NodeID→Ipv4 re-key (mirror repopulates from nodes)")
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
		&NodeUsage{},
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
