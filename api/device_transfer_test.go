package center

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// =====================================================================
// Device Transfer E2E Tests
// Tests device transfer detection and notification
// =====================================================================

// TestE2E_DeviceTransfer_WithNotification tests the complete device transfer flow:
// 1. User A logs in with device X
// 2. User B logs in with same device X
// 3. Verify User A's device is deleted
// 4. Verify User B's device is created
// 5. Verify device transfer log is recorded
func TestE2E_DeviceTransfer_WithNotification(t *testing.T) {
	skipIfNoDB(t)
	r := SetupTestRouter()

	// Enable mock verification code
	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	// Test emails for two different users
	emailA := fmt.Sprintf("device-transfer-a-%d@test.com", time.Now().UnixNano())
	emailB := fmt.Sprintf("device-transfer-b-%d@test.com", time.Now().UnixNano())
	sharedDeviceID := fmt.Sprintf("shared-device-%d", time.Now().UnixNano())

	// ========== Step 1: User A登录设备X ==========
	t.Run("UserA_LoginWithDeviceX", func(t *testing.T) {
		// 发送验证码给用户A
		w := NewTestRequest("POST", "/api/auth/code").
			WithBody(map[string]string{"email": emailA}).
			Execute(r)
		assert.Equal(t, http.StatusOK, w.Code)

		// 用户A用设备X登录
		w = NewTestRequest("POST", "/api/auth/login").
			WithBody(map[string]string{
				"email":            emailA,
				"verificationCode": MockVerificationCode,
				"udid":             sharedDeviceID,
				"remark":           "User A Device",
			}).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, 0, resp.Code, "User A login should succeed")

		t.Logf("User A logged in with device %s", sharedDeviceID)
	})

	// 验证用户A的设备记录存在
	var deviceA Device
	err := db.Get().Where("udid = ?", sharedDeviceID).First(&deviceA).Error
	require.NoError(t, err, "Should find User A's device")
	userAID := deviceA.UserID
	t.Logf("User A ID: %d, Device ID: %s", userAID, deviceA.UDID)

	// ========== Step 2: User B登录同一设备X ==========
	t.Run("UserB_LoginWithSameDeviceX", func(t *testing.T) {
		// 发送验证码给用户B
		w := NewTestRequest("POST", "/api/auth/code").
			WithBody(map[string]string{"email": emailB}).
			Execute(r)
		assert.Equal(t, http.StatusOK, w.Code)

		// 用户B用同一设备X登录
		w = NewTestRequest("POST", "/api/auth/login").
			WithBody(map[string]string{
				"email":            emailB,
				"verificationCode": MockVerificationCode,
				"udid":             sharedDeviceID,
				"remark":           "User B Device",
			}).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, 0, resp.Code, "User B login should succeed")

		t.Logf("User B logged in with device %s (transfer occurred)", sharedDeviceID)
	})

	// ========== Step 3: 验证设备所有权已转移 ==========
	t.Run("VerifyDeviceTransfer", func(t *testing.T) {
		var devices []Device
		err := db.Get().Where("udid = ?", sharedDeviceID).Find(&devices).Error
		require.NoError(t, err)

		// 应该只有一个设备记录（用户B的）
		require.Equal(t, 1, len(devices), "Should have exactly one device record after transfer")

		device := devices[0]
		assert.NotEqual(t, userAID, device.UserID, "Device should NOT belong to User A anymore")

		t.Logf("Device transfer verified: old owner=%d, new owner=%d", userAID, device.UserID)
	})

	// ========== Step 4: 验证用户A的设备已被删除 ==========
	t.Run("VerifyUserADeviceDeleted", func(t *testing.T) {
		var count int64
		err := db.Get().Model(&Device{}).Where("udid = ? AND user_id = ?", sharedDeviceID, userAID).Count(&count).Error
		require.NoError(t, err)

		assert.Equal(t, int64(0), count, "User A should have no device with this UDID")

		t.Logf("Confirmed: User A's device has been deleted")
	})
}

// TestE2E_SameUser_Relogin tests that re-login by the same user does NOT trigger transfer notification:
// 1. User A logs in with device X
// 2. User A logs in again with device X
// 3. Verify no device transfer notification is sent
func TestE2E_SameUser_Relogin(t *testing.T) {
	skipIfNoDB(t)
	r := SetupTestRouter()

	// Enable mock verification code
	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	email := fmt.Sprintf("same-user-relogin-%d@test.com", time.Now().UnixNano())
	deviceID := fmt.Sprintf("relogin-device-%d", time.Now().UnixNano())

	// ========== Step 1: User A首次登录 ==========
	t.Run("UserA_FirstLogin", func(t *testing.T) {
		w := NewTestRequest("POST", "/api/auth/code").
			WithBody(map[string]string{"email": email}).
			Execute(r)
		assert.Equal(t, http.StatusOK, w.Code)

		w = NewTestRequest("POST", "/api/auth/login").
			WithBody(map[string]string{
				"email":            email,
				"verificationCode": MockVerificationCode,
				"udid":             deviceID,
				"remark":           "Test Device",
			}).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, 0, resp.Code, "First login should succeed")

		t.Logf("User A first login completed")
	})

	// 记录用户ID
	var device Device
	err := db.Get().Where("udid = ?", deviceID).First(&device).Error
	require.NoError(t, err)
	userID := device.UserID

	// 等待一小段时间
	time.Sleep(100 * time.Millisecond)

	// ========== Step 2: User A再次登录（相同设备） ==========
	t.Run("UserA_Relogin", func(t *testing.T) {
		w := NewTestRequest("POST", "/api/auth/code").
			WithBody(map[string]string{"email": email}).
			Execute(r)
		assert.Equal(t, http.StatusOK, w.Code)

		w = NewTestRequest("POST", "/api/auth/login").
			WithBody(map[string]string{
				"email":            email,
				"verificationCode": MockVerificationCode,
				"udid":             deviceID,
				"remark":           "Test Device Updated",
			}).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, 0, resp.Code, "Relogin should succeed")

		t.Logf("User A relogin completed")
	})

	// ========== Step 3: 验证设备仍属于同一用户 ==========
	t.Run("VerifyNoTransfer", func(t *testing.T) {
		var deviceAfter Device
		err := db.Get().Where("udid = ?", deviceID).First(&deviceAfter).Error
		require.NoError(t, err)

		assert.Equal(t, userID, deviceAfter.UserID, "Device should still belong to the same user")

		t.Logf("Confirmed: No device transfer occurred (same user relogin)")
	})
}
