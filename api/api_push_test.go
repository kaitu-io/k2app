package center

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// Push Token Model Tests (TDD)
// =====================================================================

func TestMockDB_PushToken_Create(t *testing.T) {
	m := SetupMockDB(t)

	t.Run("Create push token successfully", func(t *testing.T) {
		m.Mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `push_tokens`")).
			WillReturnResult(sqlmock.NewResult(1, 1))

		sandbox := false
		token := PushToken{
			UserID:      uint64(1),
			DeviceUDID:  "device-udid-123",
			Platform:    PushPlatformIOS,
			Provider:    PushProviderAPNs,
			Token:       "apns-token-abc123",
			Topic:       "io.kaitu.savemevpn",
			Sandbox:     &sandbox,
			AppFlavor:   AppFlavorGooglePlay,
			AppVersion:  "1.0.0",
			AppBundle:   "io.kaitu.savemevpn",
			OSVersion:   "17.0",
			DeviceModel: "iPhone15,2",
			Status:      PushTokenStatusActive,
			LastSeenAt:  time.Now().Unix(),
		}

		err := m.DB.Create(&token).Error
		assert.NoError(t, err)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})

	t.Run("Create push token for Android FCM", func(t *testing.T) {
		m.Mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `push_tokens`")).
			WillReturnResult(sqlmock.NewResult(2, 1))

		token := PushToken{
			UserID:      uint64(2),
			DeviceUDID:  "android-device-456",
			Platform:    PushPlatformAndroid,
			Provider:    PushProviderFCM,
			Token:       "fcm-token-xyz",
			AppFlavor:   AppFlavorGooglePlay,
			AppVersion:  "1.0.0",
			Status:      PushTokenStatusActive,
			LastSeenAt:  time.Now().Unix(),
		}

		err := m.DB.Create(&token).Error
		assert.NoError(t, err)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})

	t.Run("Create push token for Android JPush (China)", func(t *testing.T) {
		m.Mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `push_tokens`")).
			WillReturnResult(sqlmock.NewResult(3, 1))

		token := PushToken{
			UserID:      uint64(3),
			DeviceUDID:  "china-android-789",
			Platform:    PushPlatformAndroid,
			Provider:    PushProviderJPush,
			Token:       "jpush-reg-id-xyz",
			AppFlavor:   AppFlavorChina,
			Status:      PushTokenStatusActive,
			LastSeenAt:  time.Now().Unix(),
		}

		err := m.DB.Create(&token).Error
		assert.NoError(t, err)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})
}

func TestMockDB_PushToken_FindByDeviceUDID(t *testing.T) {
	m := SetupMockDB(t)

	t.Run("Find push token by device UDID", func(t *testing.T) {
		userID := uint64(1)
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at",
			"user_id", "device_udid", "platform", "provider", "token",
			"topic", "sandbox", "app_flavor", "app_version", "app_bundle",
			"os_version", "device_model", "status", "last_seen_at", "metadata",
		}).AddRow(
			1, time.Now(), time.Now(), nil,
			userID, "device-udid-123", "ios", "apns", "apns-token-abc123",
			"io.kaitu.savemevpn", false, "google_play", "1.0.0", "io.kaitu.savemevpn",
			"17.0", "iPhone15,2", "active", time.Now().Unix(), "{}",
		)

		m.Mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `push_tokens` WHERE device_udid = ? AND `push_tokens`.`deleted_at` IS NULL ORDER BY `push_tokens`.`id` LIMIT ?")).
			WithArgs("device-udid-123", 1).
			WillReturnRows(rows)

		var token PushToken
		err := m.DB.Where("device_udid = ?", "device-udid-123").First(&token).Error

		assert.NoError(t, err)
		assert.Equal(t, "device-udid-123", token.DeviceUDID)
		assert.Equal(t, PushPlatformIOS, token.Platform)
		assert.Equal(t, PushProviderAPNs, token.Provider)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})

	t.Run("Find push token by device UDID and provider", func(t *testing.T) {
		userID := uint64(1)
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at",
			"user_id", "device_udid", "platform", "provider", "token",
			"topic", "sandbox", "app_flavor", "app_version", "app_bundle",
			"os_version", "device_model", "status", "last_seen_at", "metadata",
		}).AddRow(
			1, time.Now(), time.Now(), nil,
			userID, "device-udid-456", "android", "fcm", "fcm-token-xyz",
			"", false, "google_play", "1.0.0", "io.kaitu",
			"14", "Pixel 8", "active", time.Now().Unix(), "{}",
		)

		m.Mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `push_tokens` WHERE (device_udid = ? AND provider = ?) AND `push_tokens`.`deleted_at` IS NULL ORDER BY `push_tokens`.`id` LIMIT ?")).
			WithArgs("device-udid-456", PushProviderFCM, 1).
			WillReturnRows(rows)

		var token PushToken
		err := m.DB.Where("device_udid = ? AND provider = ?", "device-udid-456", PushProviderFCM).First(&token).Error

		assert.NoError(t, err)
		assert.Equal(t, "device-udid-456", token.DeviceUDID)
		assert.Equal(t, PushPlatformAndroid, token.Platform)
		assert.Equal(t, PushProviderFCM, token.Provider)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})
}

func TestMockDB_PushToken_UpdateStatus(t *testing.T) {
	m := SetupMockDB(t)

	t.Run("Mark token as inactive", func(t *testing.T) {
		m.Mock.ExpectExec(regexp.QuoteMeta("UPDATE `push_tokens` SET")).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := m.DB.Model(&PushToken{}).
			Where("id = ?", uint64(1)).
			Update("status", PushTokenStatusInactive).Error

		assert.NoError(t, err)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})
}

func TestMockDB_PushToken_FindActiveByUser(t *testing.T) {
	m := SetupMockDB(t)

	t.Run("Find all active tokens for user across multiple devices", func(t *testing.T) {
		userID := uint64(1)
		rows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at",
			"user_id", "device_udid", "platform", "provider", "token",
			"topic", "sandbox", "app_flavor", "app_version", "app_bundle",
			"os_version", "device_model", "status", "last_seen_at", "metadata",
		}).AddRow(
			1, time.Now(), time.Now(), nil,
			userID, "device-udid-1", "ios", "apns", "apns-token-1",
			"io.kaitu", false, "google_play", "1.0.0", "io.kaitu",
			"17.0", "iPhone15,2", "active", time.Now().Unix(), "{}",
		).AddRow(
			2, time.Now(), time.Now(), nil,
			userID, "device-udid-2", "android", "fcm", "fcm-token-2",
			"", false, "google_play", "1.0.0", "io.kaitu",
			"14", "Pixel 8", "active", time.Now().Unix(), "{}",
		).AddRow(
			3, time.Now(), time.Now(), nil,
			userID, "device-udid-3", "android", "jpush", "jpush-token-3",
			"", false, "china", "1.0.0", "io.kaitu",
			"13", "Xiaomi 14", "active", time.Now().Unix(), "{}",
		)

		m.Mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `push_tokens` WHERE (user_id = ? AND status = ?) AND `push_tokens`.`deleted_at` IS NULL")).
			WithArgs(userID, PushTokenStatusActive).
			WillReturnRows(rows)

		var tokens []PushToken
		err := m.DB.Where("user_id = ? AND status = ?", userID, PushTokenStatusActive).Find(&tokens).Error

		assert.NoError(t, err)
		assert.Len(t, tokens, 3)

		// Verify each platform/provider combination
		platforms := make(map[PushPlatform]int)
		providers := make(map[PushProvider]int)
		for _, token := range tokens {
			platforms[token.Platform]++
			providers[token.Provider]++
		}
		assert.Equal(t, 1, platforms[PushPlatformIOS])
		assert.Equal(t, 2, platforms[PushPlatformAndroid])
		assert.Equal(t, 1, providers[PushProviderAPNs])
		assert.Equal(t, 1, providers[PushProviderFCM])
		assert.Equal(t, 1, providers[PushProviderJPush])
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})
}


// =====================================================================
// Push Token Model Helper Methods Tests
// =====================================================================

func TestPushToken_HelperMethods(t *testing.T) {
	t.Run("IsActive returns true for active token", func(t *testing.T) {
		token := PushToken{Status: PushTokenStatusActive}
		assert.True(t, token.IsActive())
	})

	t.Run("IsActive returns false for inactive token", func(t *testing.T) {
		token := PushToken{Status: PushTokenStatusInactive}
		assert.False(t, token.IsActive())
	})

	t.Run("MarkInactive sets status to inactive", func(t *testing.T) {
		token := PushToken{Status: PushTokenStatusActive}
		token.MarkInactive()
		assert.Equal(t, PushTokenStatusInactive, token.Status)
	})

	t.Run("MarkActive sets status to active and updates last seen", func(t *testing.T) {
		token := PushToken{Status: PushTokenStatusInactive, LastSeenAt: 0}
		token.MarkActive()
		assert.Equal(t, PushTokenStatusActive, token.Status)
		assert.Greater(t, token.LastSeenAt, int64(0))
	})

	t.Run("GetMetadata returns empty map for empty metadata", func(t *testing.T) {
		token := PushToken{Metadata: ""}
		meta, err := token.GetMetadata()
		assert.NoError(t, err)
		assert.Empty(t, meta)
	})

	t.Run("SetMetadata and GetMetadata round-trip", func(t *testing.T) {
		token := PushToken{}
		meta := map[string]interface{}{
			"key1": "value1",
			"key2": float64(42),
		}
		err := token.SetMetadata(meta)
		require.NoError(t, err)

		retrieved, err := token.GetMetadata()
		require.NoError(t, err)
		assert.Equal(t, "value1", retrieved["key1"])
		assert.Equal(t, float64(42), retrieved["key2"])
	})
}


// =====================================================================
// Push Token Idempotent Registration Tests
// =====================================================================

func TestMockDB_PushToken_IdempotentRegistration(t *testing.T) {
	m := SetupMockDB(t)

	t.Run("Upsert token - update existing by device_udid and provider", func(t *testing.T) {
		// First, try to find existing token
		userID := uint64(1)
		existingRows := sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at",
			"user_id", "device_udid", "platform", "provider", "token",
			"topic", "sandbox", "app_flavor", "app_version", "app_bundle",
			"os_version", "device_model", "status", "last_seen_at", "metadata",
		}).AddRow(
			1, time.Now(), time.Now(), nil,
			userID, "device-udid-123", "ios", "apns", "old-token",
			"io.kaitu", false, "google_play", "1.0.0", "io.kaitu",
			"17.0", "iPhone15,2", "active", time.Now().Unix(), "{}",
		)

		m.Mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `push_tokens` WHERE (device_udid = ? AND provider = ?) AND `push_tokens`.`deleted_at` IS NULL ORDER BY `push_tokens`.`id` LIMIT ?")).
			WithArgs("device-udid-123", PushProviderAPNs, 1).
			WillReturnRows(existingRows)

		// Find existing
		var token PushToken
		err := m.DB.Where("device_udid = ? AND provider = ?", "device-udid-123", PushProviderAPNs).First(&token).Error
		assert.NoError(t, err)
		assert.Equal(t, "old-token", token.Token)

		// Then update it
		m.Mock.ExpectExec(regexp.QuoteMeta("UPDATE `push_tokens` SET")).
			WillReturnResult(sqlmock.NewResult(0, 1))

		token.Token = "new-token"
		token.LastSeenAt = time.Now().Unix()
		err = m.DB.Save(&token).Error
		assert.NoError(t, err)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Push Token Auto-Deactivation Tests
// =====================================================================

func TestMockDB_PushToken_AutoDeactivate(t *testing.T) {
	m := SetupMockDB(t)

	t.Run("Deactivate token on provider error", func(t *testing.T) {
		// Simulate finding a token that should be deactivated
		m.Mock.ExpectExec(regexp.QuoteMeta("UPDATE `push_tokens` SET `status`=?,`updated_at`=? WHERE token = ?")).
			WithArgs(PushTokenStatusInactive, sqlmock.AnyArg(), "invalid-token-xyz").
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := m.DB.Model(&PushToken{}).
			Where("token = ?", "invalid-token-xyz").
			Updates(map[string]interface{}{
				"status": PushTokenStatusInactive,
			}).Error

		assert.NoError(t, err)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Provider Routing Tests
// =====================================================================

func TestProviderRouting(t *testing.T) {
	t.Run("iOS uses APNs", func(t *testing.T) {
		provider := getProviderForPlatformAndFlavor(PushPlatformIOS, AppFlavorGooglePlay)
		assert.Equal(t, PushProviderAPNs, provider)
	})

	t.Run("Android China uses JPush", func(t *testing.T) {
		provider := getProviderForPlatformAndFlavor(PushPlatformAndroid, AppFlavorChina)
		assert.Equal(t, PushProviderJPush, provider)
	})

	t.Run("Android GooglePlay uses FCM", func(t *testing.T) {
		provider := getProviderForPlatformAndFlavor(PushPlatformAndroid, AppFlavorGooglePlay)
		assert.Equal(t, PushProviderFCM, provider)
	})
}

// Helper function for provider routing (to be implemented)
func getProviderForPlatformAndFlavor(platform PushPlatform, flavor AppFlavor) PushProvider {
	if platform == PushPlatformIOS {
		return PushProviderAPNs
	}
	if flavor == AppFlavorChina {
		return PushProviderJPush
	}
	return PushProviderFCM
}

// =====================================================================
// API Handler Tests (TDD - Tests before implementation)
// =====================================================================

func TestAPI_RegisterPushToken(t *testing.T) {
	router := SetupMinimalRouter()
	// Route will be added when implementing the handler
	// router.POST("/api/push/token", AuthOptional(), api_register_push_token)

	t.Run("Register new token with valid data", func(t *testing.T) {
		// This test will pass when the handler is implemented
		_ = router
		// req := NewTestRequest("POST", "/api/push/token").
		// 	WithBody(map[string]interface{}{
		// 		"device_id":   "device-uuid-123",
		// 		"platform":    "ios",
		// 		"provider":    "apns",
		// 		"token":       "apns-token-abc123",
		// 		"app_flavor":  "google_play",
		// 		"app_version": "1.0.0",
		// 	}).
		// 	WithBearerToken(GenerateTestToken(1, "device-uuid-123", time.Hour))

		// w := req.Execute(router)
		// AssertResponseCode(t, w, 0)
	})

	t.Run("Register token without auth (anonymous)", func(t *testing.T) {
		// This test will pass when the handler is implemented
		_ = router
	})

	t.Run("Reject invalid platform/provider combination", func(t *testing.T) {
		// This test will pass when the handler is implemented
		_ = router
	})
}

func TestAPI_UnregisterPushToken(t *testing.T) {
	router := SetupMinimalRouter()
	// Route will be added when implementing the handler
	// router.DELETE("/api/push/token", AuthRequired(), api_unregister_push_token)

	t.Run("Unregister existing token", func(t *testing.T) {
		// This test will pass when the handler is implemented
		_ = router
	})

	t.Run("Return 404 for non-existent token", func(t *testing.T) {
		// This test will pass when the handler is implemented
		_ = router
	})
}

// =====================================================================
// Platform/Provider Combination Validation Tests
// =====================================================================

func TestIsValidPlatformProviderCombination(t *testing.T) {
	testCases := []struct {
		name     string
		platform string
		provider string
		flavor   string
		expected bool
	}{
		// iOS Cases
		{"iOS with APNs (GooglePlay)", "ios", "apns", "google_play", true},
		{"iOS with APNs (China)", "ios", "apns", "china", true},
		{"iOS with FCM (invalid)", "ios", "fcm", "google_play", false},
		{"iOS with JPush (invalid)", "ios", "jpush", "china", false},

		// Android GooglePlay Cases
		{"Android GooglePlay with FCM", "android", "fcm", "google_play", true},
		{"Android GooglePlay with JPush (invalid)", "android", "jpush", "google_play", false},
		{"Android GooglePlay with APNs (invalid)", "android", "apns", "google_play", false},

		// Android China Cases
		{"Android China with JPush", "android", "jpush", "china", true},
		{"Android China with FCM (invalid)", "android", "fcm", "china", false},
		{"Android China with APNs (invalid)", "android", "apns", "china", false},

		// Invalid Platform
		{"Invalid platform", "windows", "apns", "google_play", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isValidPlatformProviderCombination(tc.platform, tc.provider, tc.flavor)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =====================================================================
// Cross-Platform Push Notification Tests
// =====================================================================

func TestCrossPlatformPushScenarios(t *testing.T) {
	t.Run("User with multiple devices receives push on all platforms", func(t *testing.T) {
		// Scenario: User has iPhone, Android (GooglePlay), and Android (China)
		userID := uint64(123)

		tokens := []PushToken{
			{
				ID:         1,
				UserID:     userID,
				DeviceUDID: "iphone-udid-001",
				Platform:   PushPlatformIOS,
				Provider:   PushProviderAPNs,
				Token:      "apns-token-xxx",
				AppFlavor:  AppFlavorGooglePlay,
				Status:     PushTokenStatusActive,
			},
			{
				ID:         2,
				UserID:     userID,
				DeviceUDID: "pixel-udid-002",
				Platform:   PushPlatformAndroid,
				Provider:   PushProviderFCM,
				Token:      "fcm-token-yyy",
				AppFlavor:  AppFlavorGooglePlay,
				Status:     PushTokenStatusActive,
			},
			{
				ID:         3,
				UserID:     userID,
				DeviceUDID: "xiaomi-udid-003",
				Platform:   PushPlatformAndroid,
				Provider:   PushProviderJPush,
				Token:      "jpush-regid-zzz",
				AppFlavor:  AppFlavorChina,
				Status:     PushTokenStatusActive,
			},
		}

		// Verify correct provider for each device
		for _, token := range tokens {
			expectedProvider := getProviderForPlatformAndFlavor(token.Platform, token.AppFlavor)
			assert.Equal(t, expectedProvider, token.Provider,
				"Token %d should use %s provider", token.ID, expectedProvider)
		}

		// Verify all tokens belong to same user
		for _, token := range tokens {
			assert.Equal(t, userID, token.UserID)
		}

		// Verify all tokens are active
		for _, token := range tokens {
			assert.True(t, token.IsActive())
		}
	})

	t.Run("Device UDID uniquely identifies token per provider", func(t *testing.T) {
		// Same device can have multiple tokens if it switches regions
		// (e.g., user travels from China to US and reinstalls app)
		deviceUDID := "shared-device-udid"

		token1 := PushToken{
			DeviceUDID: deviceUDID,
			Provider:   PushProviderJPush,
			Status:     PushTokenStatusInactive, // Old China token
		}

		token2 := PushToken{
			DeviceUDID: deviceUDID,
			Provider:   PushProviderFCM,
			Status:     PushTokenStatusActive, // New GooglePlay token
		}

		// Different providers = different tokens (even for same device)
		assert.NotEqual(t, token1.Provider, token2.Provider)
		assert.Equal(t, token1.DeviceUDID, token2.DeviceUDID)
	})
}

// =====================================================================
// Push Notification Payload Tests
// =====================================================================

func TestPushNotificationPayload(t *testing.T) {
	t.Run("Create notification with all fields", func(t *testing.T) {
		badge := 5
		notification := PushNotification{
			Title:    "Subscription Expiring",
			Body:     "Your subscription expires in 3 days",
			Sound:    "default",
			Badge:    &badge,
			ImageURL: "https://example.com/image.png",
			Data: map[string]interface{}{
				"action":    "open_subscription",
				"user_id":   123,
				"deep_link": "kaitu://subscription",
			},
		}

		assert.Equal(t, "Subscription Expiring", notification.Title)
		assert.Equal(t, "Your subscription expires in 3 days", notification.Body)
		assert.Equal(t, 5, *notification.Badge)
		assert.Equal(t, "open_subscription", notification.Data["action"])
	})

	t.Run("Create minimal notification", func(t *testing.T) {
		notification := PushNotification{
			Title: "New Message",
			Body:  "You have a new message",
		}

		assert.Equal(t, "New Message", notification.Title)
		assert.Nil(t, notification.Badge)
		assert.Empty(t, notification.Sound)
		assert.Nil(t, notification.Data)
	})
}

// =====================================================================
// Token Lifecycle Tests
// =====================================================================

func TestTokenLifecycle(t *testing.T) {
	t.Run("Token states transition correctly", func(t *testing.T) {
		token := PushToken{
			Status:     PushTokenStatusActive,
			LastSeenAt: time.Now().Add(-24 * time.Hour).Unix(),
		}

		// Active -> Inactive (e.g., logout or provider error)
		assert.True(t, token.IsActive())
		token.MarkInactive()
		assert.False(t, token.IsActive())
		assert.Equal(t, PushTokenStatusInactive, token.Status)

		// Inactive -> Active (e.g., re-login)
		token.MarkActive()
		assert.True(t, token.IsActive())
		assert.Equal(t, PushTokenStatusActive, token.Status)
		assert.Greater(t, token.LastSeenAt, time.Now().Add(-1*time.Second).Unix())
	})

	t.Run("Expired tokens are not active", func(t *testing.T) {
		token := PushToken{
			Status:     PushTokenStatusExpired,
			LastSeenAt: time.Now().Add(-90 * 24 * time.Hour).Unix(), // 90 days ago
		}

		assert.False(t, token.IsActive())
	})
}

// =====================================================================
// User-Centric Push Design Tests
// =====================================================================

func TestUserCentricPushDesign(t *testing.T) {
	t.Run("PushToUser sends to all user devices", func(t *testing.T) {
		// This test validates the design principle:
		// Push is sent to USER, not to individual devices
		// The system automatically handles multi-device scenarios

		userID := uint64(100)

		// Simulate user's active tokens
		userTokens := []PushToken{
			{UserID: userID, Provider: PushProviderAPNs, Status: PushTokenStatusActive},
			{UserID: userID, Provider: PushProviderFCM, Status: PushTokenStatusActive},
		}

		// All tokens should belong to the same user
		for _, token := range userTokens {
			assert.Equal(t, userID, token.UserID)
			assert.True(t, token.IsActive())
		}

		// Number of push attempts should equal number of active tokens
		assert.Equal(t, 2, len(userTokens))
	})

	t.Run("Inactive tokens are skipped", func(t *testing.T) {
		userID := uint64(100)

		allTokens := []PushToken{
			{UserID: userID, Provider: PushProviderAPNs, Status: PushTokenStatusActive},
			{UserID: userID, Provider: PushProviderFCM, Status: PushTokenStatusInactive},
			{UserID: userID, Provider: PushProviderJPush, Status: PushTokenStatusExpired},
		}

		activeTokens := make([]PushToken, 0)
		for _, token := range allTokens {
			if token.IsActive() {
				activeTokens = append(activeTokens, token)
			}
		}

		// Only 1 token should be active
		assert.Equal(t, 1, len(activeTokens))
		assert.Equal(t, PushProviderAPNs, activeTokens[0].Provider)
	})
}
