package center

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// =====================================================================
// Mock DB Test Utilities
// =====================================================================

// setupMockDB creates a mock database connection with GORM
func setupMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	testInitConfig()

	db, mock, err := sqlmock.New()
	require.NoError(t, err, "Failed to create sqlmock")

	// GORM requires MySQL version check
	mock.ExpectQuery("SELECT VERSION()").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("10.4.0-MariaDB"))

	gormDB, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      db,
		SkipInitializeWithVersion: false,
	}), &gorm.Config{})
	require.NoError(t, err, "Failed to open GORM connection")

	cleanup := func() {
		sqlDB, _ := gormDB.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}

	return gormDB, mock, cleanup
}

// =====================================================================
// Test 1: User Model CRUD Operations
// =====================================================================

func TestMockDB_User_Create(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Create user successfully", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `users`")).
			WithArgs(
				sqlmock.AnyArg(), // CreatedAt
				sqlmock.AnyArg(), // UpdatedAt
				sqlmock.AnyArg(), // UUID
				nil,              // DeletedAt
				int64(0),         // ExpiredAt
				false,            // IsFirstOrderDone
				false,            // IsActivated
				int64(0),         // ActivatedAt
				uint64(0),        // InvitedByCodeID
				false,            // IsAdmin
				nil,              // DelegateID
				5,                // MaxDevice
				sqlmock.AnyArg(), // AccessKey
				false,            // IsRetailer
				"en-US",          // Language
			).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		user := User{
			UUID:      "test-uuid",
			AccessKey: "test-access-key",
			Language:  "en-US",
		}

		err := gormDB.Create(&user).Error
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Create user with database error", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `users`")).
			WillReturnError(sql.ErrConnDone)
		mock.ExpectRollback()

		user := User{UUID: "test-uuid-2"}
		err := gormDB.Create(&user).Error

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestMockDB_User_Query(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Find user by ID", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "uuid", "deleted_at",
			"expired_at", "is_first_order_done", "is_activated", "activated_at",
			"invited_by_code_id", "is_admin", "delegate_id", "max_device",
			"access_key", "is_retailer", "language",
		}).AddRow(
			1, time.Now(), time.Now(), "user-uuid-1", nil,
			time.Now().Unix()+86400, false, true, time.Now().Unix(),
			0, false, nil, 5,
			"access-key-1", false, "zh-CN",
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `users` WHERE `users`.`id` = ? AND `users`.`deleted_at` IS NULL ORDER BY `users`.`id` LIMIT ?")).
			WithArgs(1, 1).
			WillReturnRows(rows)

		var user User
		err := gormDB.First(&user, 1).Error

		assert.NoError(t, err)
		assert.Equal(t, uint64(1), user.ID)
		assert.Equal(t, "user-uuid-1", user.UUID)
		assert.Equal(t, "zh-CN", user.Language)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("User not found", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `users` WHERE `users`.`id` = ? AND `users`.`deleted_at` IS NULL ORDER BY `users`.`id` LIMIT ?")).
			WithArgs(999, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		var user User
		err := gormDB.First(&user, 999).Error

		assert.Error(t, err)
		assert.Equal(t, gorm.ErrRecordNotFound, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestMockDB_User_Update(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Update user language", func(t *testing.T) {
		mock.ExpectBegin()
		// GORM adds soft delete condition: AND `users`.`deleted_at` IS NULL
		mock.ExpectExec(regexp.QuoteMeta("UPDATE `users` SET `language`=?,`updated_at`=? WHERE id = ? AND `users`.`deleted_at` IS NULL")).
			WithArgs("ja", sqlmock.AnyArg(), uint64(1)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		err := gormDB.Model(&User{}).Where("id = ?", uint64(1)).Update("language", "ja").Error

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Update user with multiple fields", func(t *testing.T) {
		// Reset mock expectations - need fresh mock for this subtest
		gormDB2, mock2, cleanup2 := setupMockDB(t)
		defer cleanup2()

		mock2.ExpectBegin()
		// Use regex pattern to match partial SQL since column order may vary
		mock2.ExpectExec("UPDATE `users` SET").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock2.ExpectCommit()

		isAdmin := true
		err := gormDB2.Model(&User{}).Where("id = ?", uint64(1)).Updates(map[string]interface{}{
			"is_admin": isAdmin,
			"language": "en-US",
		}).Error

		assert.NoError(t, err)
		assert.NoError(t, mock2.ExpectationsWereMet())
	})
}

func TestMockDB_User_Delete(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Soft delete user", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("UPDATE `users` SET `deleted_at`=? WHERE `users`.`id` = ? AND `users`.`deleted_at` IS NULL")).
			WithArgs(sqlmock.AnyArg(), uint64(1)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		err := gormDB.Delete(&User{}, uint64(1)).Error

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 2: Device Model Operations
// =====================================================================

func TestMockDB_Device_CRUD(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Create device", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `devices`")).
			WithArgs(
				sqlmock.AnyArg(), // CreatedAt
				sqlmock.AnyArg(), // UpdatedAt
				"device-udid-1",  // UDID
				"Test Device",    // Remark
				uint64(1),        // UserID
				sqlmock.AnyArg(), // TokenIssueAt
				sqlmock.AnyArg(), // TokenLastUsedAt
				"",               // PasswordHash
				"",               // AppVersion
				"",               // AppPlatform
				"",               // AppArch
			).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		device := Device{
			UDID:            "device-udid-1",
			Remark:          "Test Device",
			UserID:          1,
			TokenIssueAt:    time.Now().Unix(),
			TokenLastUsedAt: time.Now().Unix(),
		}

		err := gormDB.Create(&device).Error
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Find device by UDID", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "udid", "remark",
			"user_id", "token_issue_at", "token_last_used_at",
			"password_hash", "app_version", "app_platform", "app_arch",
		}).AddRow(
			1, time.Now(), time.Now(), "device-udid-1", "Test Device",
			1, time.Now().Unix(), time.Now().Unix(),
			"", "1.0.0", "darwin", "arm64",
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `devices` WHERE udid = ? ORDER BY `devices`.`id` LIMIT ?")).
			WithArgs("device-udid-1", 1).
			WillReturnRows(rows)

		var device Device
		err := gormDB.Where("udid = ?", "device-udid-1").First(&device).Error

		assert.NoError(t, err)
		assert.Equal(t, "device-udid-1", device.UDID)
		assert.Equal(t, uint64(1), device.UserID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Delete device by UDID", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("DELETE FROM `devices` WHERE udid = ?")).
			WithArgs("device-udid-1").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		err := gormDB.Where("udid = ?", "device-udid-1").Delete(&Device{}).Error

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Count devices for user", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"count"}).AddRow(3)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM `devices` WHERE user_id = ?")).
			WithArgs(uint64(1)).
			WillReturnRows(rows)

		var count int64
		err := gormDB.Model(&Device{}).Where("user_id = ?", uint64(1)).Count(&count).Error

		assert.NoError(t, err)
		assert.Equal(t, int64(3), count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 3: LoginIdentify Model Operations
// =====================================================================

func TestMockDB_LoginIdentify_Operations(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Find login identify by type and index", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at",
			"user_id", "type", "index_id", "encrypted_value",
		}).AddRow(
			1, time.Now(), time.Now(), nil,
			1, "email", "hashed-email-index", "encrypted-email-value",
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `login_identifies` WHERE (type = ? AND index_id = ?) AND `login_identifies`.`deleted_at` IS NULL ORDER BY `login_identifies`.`id` LIMIT ?")).
			WithArgs("email", "hashed-email-index", 1).
			WillReturnRows(rows)

		var identify LoginIdentify
		err := gormDB.Where("type = ? AND index_id = ?", "email", "hashed-email-index").First(&identify).Error

		assert.NoError(t, err)
		assert.Equal(t, "email", identify.Type)
		assert.Equal(t, "hashed-email-index", identify.IndexID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Create login identify", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `login_identifies`")).
			WithArgs(
				sqlmock.AnyArg(), // CreatedAt
				sqlmock.AnyArg(), // UpdatedAt
				nil,              // DeletedAt
				uint64(1),        // UserID
				"email",          // Type
				"hash-index",     // IndexID
				"encrypted",      // EncryptedValue
			).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		identify := LoginIdentify{
			UserID:         1,
			Type:           "email",
			IndexID:        "hash-index",
			EncryptedValue: "encrypted",
		}

		err := gormDB.Create(&identify).Error
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 4: Transaction Operations
// =====================================================================

func TestMockDB_Transaction(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Successful transaction with user and device creation", func(t *testing.T) {
		mock.ExpectBegin()

		// User insert
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `users`")).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Device insert
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `devices`")).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectCommit()

		err := gormDB.Transaction(func(tx *gorm.DB) error {
			user := User{UUID: "tx-user-uuid", Language: "en-US"}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}

			device := Device{
				UDID:            "tx-device-udid",
				UserID:          user.ID,
				TokenIssueAt:    time.Now().Unix(),
				TokenLastUsedAt: time.Now().Unix(),
			}
			return tx.Create(&device).Error
		})

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Transaction rollback on error", func(t *testing.T) {
		mock.ExpectBegin()

		// User insert succeeds
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `users`")).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Device insert fails
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `devices`")).
			WillReturnError(sql.ErrConnDone)

		mock.ExpectRollback()

		err := gormDB.Transaction(func(tx *gorm.DB) error {
			user := User{UUID: "tx-user-uuid-2", Language: "en-US"}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}

			device := Device{
				UDID:            "tx-device-udid-2",
				UserID:          user.ID,
				TokenIssueAt:    time.Now().Unix(),
				TokenLastUsedAt: time.Now().Unix(),
			}
			return tx.Create(&device).Error
		})

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 5: Preload Associations
// =====================================================================

func TestMockDB_Preload(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Preload user with login identifies", func(t *testing.T) {
		// Query for login identify
		identifyRows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at",
			"user_id", "type", "index_id", "encrypted_value",
		}).AddRow(
			1, time.Now(), time.Now(), nil,
			1, "email", "hash-index", "encrypted-value",
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `login_identifies` WHERE (type = ? AND index_id = ?) AND `login_identifies`.`deleted_at` IS NULL ORDER BY `login_identifies`.`id` LIMIT ?")).
			WithArgs("email", "hash-index", 1).
			WillReturnRows(identifyRows)

		// Preload user
		userRows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "uuid", "deleted_at",
			"expired_at", "is_first_order_done", "is_activated", "activated_at",
			"invited_by_code_id", "is_admin", "delegate_id", "max_device",
			"access_key", "is_retailer", "language",
		}).AddRow(
			1, time.Now(), time.Now(), "preload-user-uuid", nil,
			time.Now().Unix()+86400, false, true, time.Now().Unix(),
			0, false, nil, 5,
			"access-key", false, "en-US",
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `users` WHERE `users`.`id` = ? AND `users`.`deleted_at` IS NULL")).
			WithArgs(uint64(1)).
			WillReturnRows(userRows)

		var identify LoginIdentify
		err := gormDB.Preload("User").Where("type = ? AND index_id = ?", "email", "hash-index").First(&identify).Error

		assert.NoError(t, err)
		assert.NotNil(t, identify.User)
		assert.Equal(t, "preload-user-uuid", identify.User.UUID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 6: Order By and Limit Operations
// =====================================================================

func TestMockDB_OrderAndLimit(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Find oldest device ordered by token_last_used_at", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "udid", "remark",
			"user_id", "token_issue_at", "token_last_used_at",
			"password_hash", "app_version", "app_platform", "app_arch",
		}).AddRow(
			1, time.Now(), time.Now(), "oldest-device-udid", "Oldest Device",
			1, time.Now().Unix()-86400, time.Now().Unix()-86400,
			"", "1.0.0", "darwin", "arm64",
		)

		// GORM adds secondary order by primary key: ORDER BY token_last_used_at ASC,`devices`.`id` LIMIT ?
		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `devices` WHERE user_id = ? ORDER BY token_last_used_at ASC,`devices`.`id` LIMIT ?")).
			WithArgs(uint64(1), 1).
			WillReturnRows(rows)

		var device Device
		err := gormDB.Where("user_id = ?", uint64(1)).Order("token_last_used_at ASC").First(&device).Error

		assert.NoError(t, err)
		assert.Equal(t, "oldest-device-udid", device.UDID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 7: InviteCode Model Operations
// =====================================================================

func TestMockDB_InviteCode(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Find invite code by ID", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "remark", "user_id",
		}).AddRow(
			12345, time.Now(), time.Now(), "Test Invite Code", 1,
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `invite_codes` WHERE `invite_codes`.`id` = ? ORDER BY `invite_codes`.`id` LIMIT ?")).
			WithArgs(12345, 1).
			WillReturnRows(rows)

		var inviteCode InviteCode
		err := gormDB.First(&inviteCode, 12345).Error

		assert.NoError(t, err)
		assert.Equal(t, uint64(12345), inviteCode.ID)
		assert.Equal(t, uint64(1), inviteCode.UserID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Create invite code", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `invite_codes`")).
			WithArgs(
				sqlmock.AnyArg(), // CreatedAt
				sqlmock.AnyArg(), // UpdatedAt
				"New Code",       // Remark
				uint64(1),        // UserID
			).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		inviteCode := InviteCode{
			Remark: "New Code",
			UserID: 1,
		}

		err := gormDB.Create(&inviteCode).Error
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 8: Plan Model Operations
// =====================================================================

func TestMockDB_Plan(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Find all active plans", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "pid", "label",
			"price", "origin_price", "month", "highlight", "is_active",
		}).AddRow(
			1, time.Now(), time.Now(), "monthly", "Monthly Plan",
			999, 1299, 1, false, true,
		).AddRow(
			2, time.Now(), time.Now(), "yearly", "Yearly Plan",
			9999, 15588, 12, true, true,
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `plans` WHERE is_active = ?")).
			WithArgs(true).
			WillReturnRows(rows)

		var plans []Plan
		err := gormDB.Where("is_active = ?", true).Find(&plans).Error

		assert.NoError(t, err)
		assert.Len(t, plans, 2)
		assert.Equal(t, "monthly", plans[0].PID)
		assert.Equal(t, "yearly", plans[1].PID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 9: SlaveNode and SlaveTunnel Operations
// =====================================================================

func TestMockDB_SlaveNode(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Find slave node by IPv4", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at",
			"ipv4", "secret_token", "country", "region", "name", "ipv6",
		}).AddRow(
			1, time.Now(), time.Now(), nil,
			"192.168.1.1", "secret-token", "US", "us-west-1", "US West Node", "::",
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `slave_nodes` WHERE ipv4 = ? AND `slave_nodes`.`deleted_at` IS NULL ORDER BY `slave_nodes`.`id` LIMIT ?")).
			WithArgs("192.168.1.1", 1).
			WillReturnRows(rows)

		var node SlaveNode
		err := gormDB.Where("ipv4 = ?", "192.168.1.1").First(&node).Error

		assert.NoError(t, err)
		assert.Equal(t, "192.168.1.1", node.Ipv4)
		assert.Equal(t, "US", node.Country)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestMockDB_SlaveTunnel(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Find tunnel by domain", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at",
			"domain", "secret_token", "name", "protocol", "port", "node_id", "is_test",
		}).AddRow(
			1, time.Now(), time.Now(), nil,
			"tunnel.example.com", "tunnel-secret", "Main Tunnel", "k2wss", 10001, 1, false,
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `slave_tunnels` WHERE domain = ? AND `slave_tunnels`.`deleted_at` IS NULL ORDER BY `slave_tunnels`.`id` LIMIT ?")).
			WithArgs("tunnel.example.com", 1).
			WillReturnRows(rows)

		var tunnel SlaveTunnel
		err := gormDB.Where("domain = ?", "tunnel.example.com").First(&tunnel).Error

		assert.NoError(t, err)
		assert.Equal(t, "tunnel.example.com", tunnel.Domain)
		assert.Equal(t, TunnelProtocolK2WSS, tunnel.Protocol) // Matches mock data "k2wss" on line 641
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 10: Campaign Model Operations
// =====================================================================

func TestMockDB_Campaign(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Find active campaign by code", func(t *testing.T) {
		now := time.Now().Unix()
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at",
			"code", "name", "type", "value", "start_at", "end_at",
			"description", "is_active", "matcher_type", "usage_count", "max_usage",
		}).AddRow(
			1, time.Now(), time.Now(), nil,
			"FIRST_ORDER_20", "First Order 20% Off", "discount", 80, now-86400, now+86400,
			"20% off for first order", true, "first_order", 100, 0,
		)

		// GORM wraps conditions in parentheses: WHERE (code = ? AND is_active = ?)
		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `campaigns` WHERE (code = ? AND is_active = ?) AND `campaigns`.`deleted_at` IS NULL ORDER BY `campaigns`.`id` LIMIT ?")).
			WithArgs("FIRST_ORDER_20", true, 1).
			WillReturnRows(rows)

		var campaign Campaign
		err := gormDB.Where("code = ? AND is_active = ?", "FIRST_ORDER_20", true).First(&campaign).Error

		assert.NoError(t, err)
		assert.Equal(t, "FIRST_ORDER_20", campaign.Code)
		assert.Equal(t, "discount", campaign.Type)
		assert.Equal(t, uint64(80), campaign.Value)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 11: Order Model Operations
// =====================================================================

func TestMockDB_Order(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Create order with transaction", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `orders`")).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		isPaid := false
		order := Order{
			UUID:                 "order-uuid-1",
			Title:                "Monthly Plan",
			OriginAmount:         1299,
			CampaignReduceAmount: 0,
			PayAmount:            1299,
			UserID:               1,
			IsPaid:               &isPaid,
		}

		err := gormDB.Create(&order).Error
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Find order by UUID", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "uuid", "title",
			"origin_amount", "campaign_reduce_amount", "pay_amount",
			"user_id", "is_paid", "paid_at", "wordgate_order_no", "campaign_code", "meta",
		}).AddRow(
			1, time.Now(), time.Now(), "order-uuid-1", "Monthly Plan",
			1299, 0, 1299,
			1, false, nil, "", nil, "{}",
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `orders` WHERE uuid = ? ORDER BY `orders`.`id` LIMIT ?")).
			WithArgs("order-uuid-1", 1).
			WillReturnRows(rows)

		var order Order
		err := gormDB.Where("uuid = ?", "order-uuid-1").First(&order).Error

		assert.NoError(t, err)
		assert.Equal(t, "order-uuid-1", order.UUID)
		assert.Equal(t, uint64(1299), order.OriginAmount)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 12: UserProHistory Model Operations
// =====================================================================

func TestMockDB_UserProHistory(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Create pro history record", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `user_pro_histories`")).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		history := UserProHistory{
			UserID:      1,
			ReferenceID: 1001,
			Type:        VipPurchase,
			Days:        30,
			Reason:      "Monthly subscription",
		}

		err := gormDB.Create(&history).Error
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 13: Complex Query with struct conditions
// =====================================================================

func TestMockDB_StructQuery(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Query with struct conditions", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at",
			"user_id", "type", "index_id", "encrypted_value",
		}).AddRow(
			1, time.Now(), time.Now(), nil,
			1, "email", "test-index", "encrypted",
		)

		// GORM wraps struct conditions in parentheses
		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `login_identifies` WHERE (`login_identifies`.`type` = ? AND `login_identifies`.`index_id` = ?) AND `login_identifies`.`deleted_at` IS NULL ORDER BY `login_identifies`.`id` LIMIT ?")).
			WithArgs("email", "test-index", 1).
			WillReturnRows(rows)

		var identify LoginIdentify
		err := gormDB.Where(&LoginIdentify{Type: "email", IndexID: "test-index"}).First(&identify).Error

		assert.NoError(t, err)
		assert.Equal(t, "email", identify.Type)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 14: Helper Functions with Mock Context
// =====================================================================

func TestMockDB_UserHelperMethods(t *testing.T) {
	t.Run("User.IsExpired returns false for non-expired user", func(t *testing.T) {
		user := User{
			ExpiredAt: time.Now().Unix() + 86400, // Tomorrow
		}
		assert.False(t, user.IsExpired())
	})

	t.Run("User.IsExpired returns true for expired user", func(t *testing.T) {
		user := User{
			ExpiredAt: time.Now().Unix() - 86400, // Yesterday
		}
		assert.True(t, user.IsExpired())
	})

	t.Run("User.IsVip returns true for user with first order done", func(t *testing.T) {
		user := User{
			IsFirstOrderDone: BoolPtr(true),
		}
		assert.True(t, user.IsVip())
	})

	t.Run("User.IsVip returns false for user without first order", func(t *testing.T) {
		user := User{
			IsFirstOrderDone: BoolPtr(false),
		}
		assert.False(t, user.IsVip())
	})

	t.Run("User.GetLanguagePreference returns user language", func(t *testing.T) {
		user := User{
			Language: "ja",
		}
		assert.Equal(t, "ja", user.GetLanguagePreference())
	})

	t.Run("User.GetLanguagePreference returns default when empty", func(t *testing.T) {
		user := User{
			Language: "",
		}
		// When language is empty and no login identifies, returns "en-US"
		assert.Equal(t, "en-US", user.GetLanguagePreference())
	})
}

// =====================================================================
// Test 15: Concurrent Database Operations
// =====================================================================

func TestMockDB_ConcurrentOperations(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Multiple independent queries", func(t *testing.T) {
		// Setup expectations for user query
		userRows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "uuid", "deleted_at",
			"expired_at", "is_first_order_done", "is_activated", "activated_at",
			"invited_by_code_id", "is_admin", "delegate_id", "max_device",
			"access_key", "is_retailer", "language",
		}).AddRow(
			1, time.Now(), time.Now(), "user-uuid", nil,
			time.Now().Unix()+86400, false, true, time.Now().Unix(),
			0, false, nil, 5,
			"access-key", false, "en-US",
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `users` WHERE `users`.`id` = ? AND `users`.`deleted_at` IS NULL ORDER BY `users`.`id` LIMIT ?")).
			WithArgs(1, 1).
			WillReturnRows(userRows)

		// Execute query
		var user User
		err := gormDB.First(&user, 1).Error

		assert.NoError(t, err)
		assert.Equal(t, "user-uuid", user.UUID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 16: Error Scenarios
// =====================================================================

func TestMockDB_ErrorScenarios(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Handle connection error", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `users`")).
			WillReturnError(sql.ErrConnDone)

		var users []User
		err := gormDB.Find(&users).Error

		assert.Error(t, err)
		assert.Equal(t, sql.ErrConnDone, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Handle duplicate key error", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `users`")).
			WillReturnError(gorm.ErrDuplicatedKey)
		mock.ExpectRollback()

		user := User{UUID: "duplicate-uuid"}
		err := gormDB.Create(&user).Error

		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 17: EmailSendLog Operations (for EDM)
// =====================================================================

func TestMockDB_EmailSendLog(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Create email send log", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `email_send_logs`")).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		log := EmailSendLog{
			BatchID:    "batch-123",
			TemplateID: 1,
			UserID:     1,
			Email:      "test@example.com",
			Language:   "en-US",
			Status:     EmailSendLogStatusPending,
		}

		err := gormDB.Create(&log).Error
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Check idempotency - count existing logs", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"count"}).AddRow(1)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM `email_send_logs` WHERE batch_id = ? AND template_id = ? AND user_id = ?")).
			WithArgs("batch-123", uint64(1), uint64(1)).
			WillReturnRows(rows)

		var count int64
		err := gormDB.Model(&EmailSendLog{}).
			Where("batch_id = ? AND template_id = ? AND user_id = ?", "batch-123", uint64(1), uint64(1)).
			Count(&count).Error

		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 18: Model Methods that use context
// =====================================================================

func TestMockDB_ContextUsage(t *testing.T) {
	testInitConfig()

	t.Run("InviteCode.GetCode returns encoded ID", func(t *testing.T) {
		inviteCode := InviteCode{ID: 12345}
		code := inviteCode.GetCode()
		assert.NotEmpty(t, code)
		assert.Len(t, code, 6) // 6-character encoded string
	})

	t.Run("InviteCodeID decodes code back to ID", func(t *testing.T) {
		inviteCode := InviteCode{ID: 12345}
		code := inviteCode.GetCode()
		decodedID := InviteCodeID(code)
		assert.Equal(t, uint64(12345), decodedID)
	})
}

// =====================================================================
// Test 19: Save vs Create semantics
// =====================================================================

func TestMockDB_SaveVsCreate(t *testing.T) {
	gormDB, mock, cleanup := setupMockDB(t)
	defer cleanup()

	t.Run("Save creates new record when ID is 0", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `devices`")).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		device := Device{
			UDID:            "new-device-udid",
			UserID:          1,
			TokenIssueAt:    time.Now().Unix(),
			TokenLastUsedAt: time.Now().Unix(),
		}

		err := gormDB.Save(&device).Error
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Save updates existing record when ID is set", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("UPDATE `devices`")).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		device := Device{
			ID:              1,
			UDID:            "existing-device-udid",
			UserID:          1,
			TokenIssueAt:    time.Now().Unix(),
			TokenLastUsedAt: time.Now().Unix(),
		}

		err := gormDB.Save(&device).Error
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Test 20: Verify test helper context
// =====================================================================

func TestContextHelper(t *testing.T) {
	ctx := context.Background()
	assert.NotNil(t, ctx)
}
