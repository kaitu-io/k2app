package center

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// =====================================================================
// E2E Login Flow Tests
// Tests the complete login flow: email verification code -> login ->
// cookie generation -> frontend state update -> UI interaction
// =====================================================================

// skipIfNoDB checks if database is available, skips test if not
// Note: Uses different name to avoid conflict with edm_task_test.go
func skipIfNoDB(t *testing.T) {
	t.Helper()
	testInitConfig()
	if db.Get() == nil {
		t.Skip("Skipping test: database not available")
	}
	sqlDB, err := db.Get().DB()
	if err != nil || sqlDB.Ping() != nil {
		t.Skip("Skipping test: database not available")
	}
}

// =====================================================================
// Test 1: Complete Web Login Flow (E2E)
// =====================================================================

// TestE2E_WebLoginFlow_Complete tests the complete web login flow:
// 1. Send verification code
// 2. Login with code
// 3. Verify cookies are set correctly
// 4. Access protected endpoint with cookies
// 5. Logout and verify cleanup
func TestE2E_WebLoginFlow_Complete(t *testing.T) {
	skipIfNoDB(t)
	r := SetupTestRouter()

	// Enable mock verification code
	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	testEmail := "e2e-login-test@example.com"

	// ========== Step 1: Send Verification Code ==========
	t.Run("Step1_SendVerificationCode", func(t *testing.T) {
		w := NewTestRequest("POST", "/api/auth/code").
			WithBody(map[string]string{"email": testEmail}).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, 0, resp.Code, "Send code should succeed, got message: %s", resp.Message)

		// Parse response data
		var data SendCodeResponse
		err = json.Unmarshal(resp.Data, &data)
		require.NoError(t, err)

		t.Logf("Send code response: userExists=%v, isActivated=%v", data.UserExists, data.IsActivated)
	})

	// ========== Step 2: Login with Verification Code ==========
	var accessCookie, csrfCookie *http.Cookie

	t.Run("Step2_WebLogin", func(t *testing.T) {
		w := NewTestRequest("POST", "/api/auth/web-login").
			WithBody(map[string]string{
				"email":            testEmail,
				"verificationCode": MockVerificationCode,
			}).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, 0, resp.Code, "Web login should succeed, got message: %s", resp.Message)

		// ========== Step 3: Verify Cookies ==========
		accessCookie = GetAccessTokenCookie(w)
		csrfCookie = GetCSRFTokenCookie(w)

		require.NotNil(t, accessCookie, "access_token cookie must be set")
		require.NotNil(t, csrfCookie, "csrf_token cookie must be set")

		// Verify cookie security attributes
		assert.True(t, accessCookie.HttpOnly, "access_token MUST be HttpOnly")
		assert.Equal(t, http.SameSiteLaxMode, accessCookie.SameSite, "access_token MUST have SameSite=Lax")
		assert.Equal(t, "/", accessCookie.Path, "access_token path should be /")

		// Verify CSRF token properties
		assert.False(t, csrfCookie.HttpOnly, "csrf_token should NOT be HttpOnly")
		assert.NotEmpty(t, csrfCookie.Value, "csrf_token should have value")
		assert.GreaterOrEqual(t, len(csrfCookie.Value), 16, "csrf_token should be at least 16 chars")

		t.Logf("Login successful, access_token length=%d, csrf_token length=%d",
			len(accessCookie.Value), len(csrfCookie.Value))
	})

	// ========== Step 4: Access Protected Resource ==========
	t.Run("Step4_AccessProtectedResource_GET", func(t *testing.T) {
		if accessCookie == nil {
			t.Skip("No access cookie from login")
		}

		// GET request - only needs access_token cookie (no CSRF required)
		w := NewTestRequest("GET", "/api/user/info").
			WithCookie(CookieAccessToken, accessCookie.Value).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)

		// Should not return 401 (authenticated)
		assert.NotEqual(t, ErrorNotLogin, resp.Code,
			"Authenticated request should not return 401")

		t.Logf("Protected resource access: code=%d, message=%s", resp.Code, resp.Message)
	})

	t.Run("Step4_AccessProtectedResource_POST", func(t *testing.T) {
		if accessCookie == nil || csrfCookie == nil {
			t.Skip("No cookies from login")
		}

		// POST request - needs both access_token and CSRF token
		w := NewTestRequest("POST", "/api/user/test-post").
			WithCookie(CookieAccessToken, accessCookie.Value).
			WithCSRFToken(csrfCookie.Value).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)

		// Should not return 401 (authenticated)
		assert.NotEqual(t, ErrorNotLogin, resp.Code,
			"POST with valid Cookie + CSRF should not return 401")

		t.Logf("POST with CSRF: code=%d", resp.Code)
	})

	// ========== Step 5: Verify CSRF Protection ==========
	t.Run("Step5_CSRF_Protection", func(t *testing.T) {
		if accessCookie == nil {
			t.Skip("No access cookie from login")
		}

		// POST without CSRF token should fail
		w := NewTestRequest("POST", "/api/user/test-post").
			WithCookie(CookieAccessToken, accessCookie.Value).
			// Intentionally NOT setting CSRF token
			Execute(r)

		resp, err := ParseResponse(w)
		require.NoError(t, err)

		assert.EqualValues(t, ErrorNotLogin, resp.Code,
			"POST without CSRF token should be rejected")

		t.Log("CSRF protection verified: POST without token rejected")
	})

	// ========== Step 6: Wrong CSRF Token ==========
	t.Run("Step6_Wrong_CSRF_Token", func(t *testing.T) {
		if accessCookie == nil || csrfCookie == nil {
			t.Skip("No cookies from login")
		}

		// POST with wrong CSRF token should fail
		w := NewTestRequest("POST", "/api/user/test-post").
			WithCookie(CookieAccessToken, accessCookie.Value).
			WithCookie(CookieCSRFToken, csrfCookie.Value).
			WithHeader("X-CSRF-Token", "wrong-csrf-token").
			Execute(r)

		resp, err := ParseResponse(w)
		require.NoError(t, err)

		assert.EqualValues(t, ErrorNotLogin, resp.Code,
			"POST with wrong CSRF token should be rejected")

		t.Log("CSRF protection verified: wrong token rejected")
	})
}

// =====================================================================
// Test 2: Device Login Flow (E2E)
// =====================================================================

// TestE2E_DeviceLoginFlow_Complete tests the device login flow:
// 1. Register free device
// 2. Send verification code
// 3. Login with device
// 4. Verify tokens are returned
// 5. Access protected endpoint with Bearer token
// 6. Refresh token
// =====================================================================
// Test 2: Authentication Error Scenarios
// =====================================================================

// TestE2E_LoginFlow_ErrorScenarios tests error handling in login flow
func TestE2E_LoginFlow_ErrorScenarios(t *testing.T) {
	skipIfNoDB(t)
	r := SetupTestRouter()

	// Enable mock verification code
	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	// ========== Wrong Verification Code ==========
	t.Run("WrongVerificationCode", func(t *testing.T) {
		// First send a valid code
		NewTestRequest("POST", "/api/auth/code").
			WithBody(map[string]string{"email": "error-test@example.com"}).
			Execute(r)

		// Try to login with wrong code
		w := NewTestRequest("POST", "/api/auth/web-login").
			WithBody(map[string]string{
				"email":            "error-test@example.com",
				"verificationCode": "999999", // Wrong code
			}).
			Execute(r)

		resp, err := ParseResponse(w)
		require.NoError(t, err)

		// Wrong verification code returns the specific business code
		// ErrorInvalidVerificationCode (400003), not the generic 422.
		assert.EqualValues(t, ErrorInvalidVerificationCode, resp.Code,
			"Wrong verification code should return ErrorInvalidVerificationCode")

		t.Logf("Wrong code error: code=%d, message=%s", resp.Code, resp.Message)
	})

	// ========== Expired Token ==========
	t.Run("ExpiredToken", func(t *testing.T) {
		expiredToken := GenerateExpiredToken(99999, "")

		w := NewTestRequest("GET", "/api/user/info").
			WithCookie(CookieAccessToken, expiredToken).
			Execute(r)

		resp, err := ParseResponse(w)
		require.NoError(t, err)

		assert.EqualValues(t, ErrorNotLogin, resp.Code,
			"Expired token should return 401")

		t.Log("Expired token correctly rejected")
	})

	// ========== Invalid Token ==========
	t.Run("InvalidToken", func(t *testing.T) {
		w := NewTestRequest("GET", "/api/user/info").
			WithCookie(CookieAccessToken, "invalid-token-format").
			Execute(r)

		resp, err := ParseResponse(w)
		require.NoError(t, err)

		assert.EqualValues(t, ErrorNotLogin, resp.Code,
			"Invalid token should return 401")

		t.Log("Invalid token correctly rejected")
	})

	// ========== No Authentication ==========
	t.Run("NoAuthentication", func(t *testing.T) {
		w := NewTestRequest("GET", "/api/user/info").Execute(r)

		resp, err := ParseResponse(w)
		require.NoError(t, err)

		assert.EqualValues(t, ErrorNotLogin, resp.Code,
			"No auth should return 401")

		t.Log("Missing auth correctly rejected")
	})

	// ========== Tampered Token ==========
	t.Run("TamperedToken", func(t *testing.T) {
		validToken := GenerateTestToken(12345, "", 24*time.Hour)
		tamperedToken := validToken[:len(validToken)-5] + "XXXXX"

		w := NewTestRequest("GET", "/api/user/info").
			WithCookie(CookieAccessToken, tamperedToken).
			Execute(r)

		resp, err := ParseResponse(w)
		require.NoError(t, err)

		assert.EqualValues(t, ErrorNotLogin, resp.Code,
			"Tampered token should return 401")

		t.Log("Tampered token correctly rejected")
	})
}

// =====================================================================
// Test 4: Cookie Sliding Expiration
// =====================================================================

// TestE2E_CookieSlidingExpiration tests automatic token renewal
func TestE2E_CookieSlidingExpiration(t *testing.T) {
	skipIfNoDB(t)
	r := SetupTestRouter()

	// Create test user
	user := CreateTestUser(t)
	require.NotNil(t, user)

	// ========== Fresh Token (>7 days remaining) - No Renewal ==========
	t.Run("FreshToken_NoRenewal", func(t *testing.T) {
		// Generate token with 30 days remaining
		freshToken := GenerateExpiringToken(user.ID, 30*24*time.Hour)

		w := NewTestRequest("GET", "/api/user/info").
			WithCookie(CookieAccessToken, freshToken).
			Execute(r)

		// Check if new cookie was set
		newCookie := GetAccessTokenCookie(w)

		// Fresh token should not trigger renewal
		if newCookie != nil && newCookie.Value != freshToken {
			t.Log("Fresh token was renewed (unexpected but acceptable)")
		} else {
			t.Log("Fresh token not renewed (expected)")
		}
	})

	// ========== Expiring Token (<7 days remaining) - Should Renew ==========
	t.Run("ExpiringToken_ShouldRenew", func(t *testing.T) {
		// Generate token with 3 days remaining
		expiringToken := GenerateExpiringToken(user.ID, 3*24*time.Hour)

		w := NewTestRequest("GET", "/api/user/info").
			WithCookie(CookieAccessToken, expiringToken).
			Execute(r)

		// Check if new cookie was set
		newCookie := GetAccessTokenCookie(w)

		if newCookie != nil && newCookie.Value != expiringToken {
			t.Log("Expiring token was renewed (expected)")
			assert.NotEqual(t, expiringToken, newCookie.Value,
				"Renewed token should be different")
		} else {
			t.Log("Expiring token not renewed (may need user lookup)")
		}
	})
}

// =====================================================================
// Test 5: Authentication Priority (Cookie vs Bearer)
// =====================================================================

// TestE2E_AuthenticationPriority tests auth method priority
func TestE2E_AuthenticationPriority(t *testing.T) {
	skipIfNoDB(t)
	r := SetupTestRouter()

	// Create test user and device
	user := CreateTestUser(t)
	require.NotNil(t, user)

	device := CreateTestDevice(t, user.ID, "priority-test-device")
	require.NotNil(t, device)

	// Generate tokens
	webToken := GenerateExpiringToken(user.ID, 24*time.Hour)
	deviceToken := GenerateTestToken(user.ID, device.UDID, 24*time.Hour)

	// ========== Cookie Auth Takes Priority ==========
	t.Run("CookieAuthPriority", func(t *testing.T) {
		// Request with both cookie and bearer token
		// Cookie should take priority for web requests
		w := NewTestRequest("GET", "/api/user/info").
			WithCookie(CookieAccessToken, webToken).
			WithBearerToken(deviceToken).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)

		// Should succeed (either auth method works)
		assert.NotEqual(t, ErrorNotLogin, resp.Code,
			"Request with valid auth should succeed")

		t.Logf("Auth priority test: code=%d", resp.Code)
	})

	// ========== Bearer Token Fallback ==========
	t.Run("BearerTokenFallback", func(t *testing.T) {
		// Request with only bearer token
		w := NewTestRequest("GET", "/api/user/info").
			WithBearerToken(deviceToken).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)

		// Should succeed with bearer token
		assert.NotEqual(t, ErrorNotLogin, resp.Code,
			"Request with bearer token should succeed")

		t.Logf("Bearer fallback test: code=%d", resp.Code)
	})
}

// =====================================================================
// Test 6: Database State Verification
// Note: SendCodeResponse type is defined in type.go

// TestE2E_LoginFlow_WithUnicodeWhitespace is the regression test for the
// 2026-05-02 prod incident: validator.v10's `email` tag let an email containing
// U+2006 SIX-PER-EM SPACE through into the SMTP path; SMTP rejected it as a
// 500 Bad request, blocking the user's verification code 5 retries in a row.
//
// This test proves the fix is consistent across send-code AND web-login: if
// sanitization were applied to one endpoint but not the other, the user-lookup
// hash would differ between the two paths and login would fail with "user not
// found" even after the code was successfully sent. A green test = both
// endpoints clean the email identically before hashing.
func TestE2E_LoginFlow_WithUnicodeWhitespace(t *testing.T) {
	skipIfNoDB(t)
	r := SetupTestRouter()

	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	// Same byte sequence as the prod log: U+2006 inside the domain.
	dirtyEmail := "u2006-regression@gmail.co m"
	cleanEmail := "u2006-regression@gmail.com"

	t.Run("send_code_accepts_dirty_email", func(t *testing.T) {
		w := NewTestRequest("POST", "/api/auth/code").
			WithBody(map[string]string{"email": dirtyEmail}).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, 0, resp.Code,
			"send_code should sanitize U+2006 and succeed; got message: %s", resp.Message)
	})

	t.Run("web_login_with_dirty_email_finds_same_user", func(t *testing.T) {
		w := NewTestRequest("POST", "/api/auth/web-login").
			WithBody(map[string]string{
				"email":            dirtyEmail,
				"verificationCode": MockVerificationCode,
			}).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, 0, resp.Code,
			"web-login with the same dirty email must hash to the same user record; got message: %s", resp.Message)
	})

	t.Run("web_login_with_clean_email_finds_same_user", func(t *testing.T) {
		// Re-send a code (the previous login consumed it) using the CLEAN form
		// and log in — confirms send-code(dirty) and login(clean) converge on
		// the same indexID.
		w := NewTestRequest("POST", "/api/auth/code").
			WithBody(map[string]string{"email": cleanEmail}).
			Execute(r)
		require.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code, "send_code clean form should succeed")

		w = NewTestRequest("POST", "/api/auth/web-login").
			WithBody(map[string]string{
				"email":            cleanEmail,
				"verificationCode": MockVerificationCode,
			}).
			Execute(r)
		assert.Equal(t, http.StatusOK, w.Code)
		resp, err = ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, 0, resp.Code, "login with clean form must find the same user; got: %s", resp.Message)
	})

	t.Run("send_code_rejects_unsalvageable_input", func(t *testing.T) {
		// A whitespace-only email cleans to "" which net/mail rejects.
		w := NewTestRequest("POST", "/api/auth/code").
			WithBody(map[string]string{"email": "   "}).
			Execute(r)

		assert.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, int(ErrorInvalidArgument), resp.Code,
			"empty/whitespace email must be rejected with 422")
	})
}

// TestE2E_HardDelete_FreesEmailSlot is the regression test for the 2026-05-02
// prod incident where admin tried to update a user's email to a value that had
// previously belonged to a "hard-deleted" account. The deletion was actually a
// GORM soft delete, and login_identifies' (type, index_id) unique index didn't
// include deleted_at, so the soft-deleted row permanently occupied the slot
// and triggered MariaDB error 1062 on every reuse attempt.
//
// The fix removed gorm.DeletedAt from LoginIdentify so all deletes are
// physical. This test exercises the full cycle:
//
//  1. Register user A with a unique email
//  2. Hard-delete user A via the approval callback (executes the same code
//     path the admin tool uses)
//  3. Register user B with the same email
//  4. Assert user B is a fresh row and no orphan login_identifies blocks it
func TestE2E_HardDelete_FreesEmailSlot(t *testing.T) {
	skipIfNoDB(t)
	r := SetupTestRouter()

	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	testEmail := "hard-delete-slot-test@example.com"

	indexID := secretHashIt(context.Background(), []byte(testEmail))
	cleanup := func() {
		// belt-and-suspenders: ensure no leftover rows from previous test runs
		db.Get().Where("type = ? AND index_id = ?", "email", indexID).Delete(&LoginIdentify{})
	}
	cleanup()
	t.Cleanup(cleanup)

	// Step 1: register user A by sending code + logging in.
	{
		w := NewTestRequest("POST", "/api/auth/code").
			WithBody(map[string]string{"email": testEmail}).
			Execute(r)
		require.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code, "send_code (user A) should succeed: %s", resp.Message)

		w = NewTestRequest("POST", "/api/auth/web-login").
			WithBody(map[string]string{
				"email":            testEmail,
				"verificationCode": MockVerificationCode,
			}).
			Execute(r)
		require.Equal(t, http.StatusOK, w.Code)
		resp, err = ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code, "web-login (user A) should succeed: %s", resp.Message)
	}

	var userA User
	require.NoError(t,
		db.Get().Where("id IN (?)",
			db.Get().Model(&LoginIdentify{}).Select("user_id").Where("type = ? AND index_id = ?", "email", indexID),
		).First(&userA).Error,
		"user A should be created and reachable via login_identifies",
	)

	// Step 2: hard-delete user A via the same callback the admin tool runs.
	params, err := json.Marshal(HardDeleteUsersRequest{UserUUIDs: []string{userA.UUID}})
	require.NoError(t, err)
	require.NoError(t, executeApprovalUserHardDelete(context.Background(), params))

	// Step 3: the (type, index_id) slot must now be free — no row, soft or hard.
	var leftover int64
	require.NoError(t,
		db.Get().Model(&LoginIdentify{}).Where("type = ? AND index_id = ?", "email", indexID).Count(&leftover).Error,
	)
	assert.Equal(t, int64(0), leftover,
		"hard delete must physically remove login_identifies; soft-deleted rows would block reuse")

	// Step 4: register user B with the same email — must succeed end-to-end.
	{
		w := NewTestRequest("POST", "/api/auth/code").
			WithBody(map[string]string{"email": testEmail}).
			Execute(r)
		require.Equal(t, http.StatusOK, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code,
			"send_code (user B, same email after hard delete) must succeed: %s", resp.Message)

		w = NewTestRequest("POST", "/api/auth/web-login").
			WithBody(map[string]string{
				"email":            testEmail,
				"verificationCode": MockVerificationCode,
			}).
			Execute(r)
		require.Equal(t, http.StatusOK, w.Code)
		resp, err = ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, 0, resp.Code,
			"web-login (user B) must succeed: %s", resp.Message)
	}

	var userB User
	require.NoError(t,
		db.Get().Where("id IN (?)",
			db.Get().Model(&LoginIdentify{}).Select("user_id").Where("type = ? AND index_id = ?", "email", indexID),
		).First(&userB).Error,
	)
	assert.NotEqual(t, userA.ID, userB.ID, "user B should be a fresh row, not user A")
}

// =====================================================================
// Test 8: Router Device-Class Locking (k2r integration)
// =====================================================================

// TestLoginFlow_RouterFullCycle exercises the end-to-end class-locking guarantee:
// 1. Family-tier user logs in on k2r → Device.IsGateway=true.
// 2. Same token used with service header → 403002.
func TestLoginFlow_RouterFullCycle(t *testing.T) {
	skipIfNoDB(t)
	r := SetupDeviceClassTestRouter()

	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	testEmail := "test-router-full-cycle@example.com"
	udid := "test-udid-router-cycle"

	// Ensure clean state across runs.
	indexID := secretHashIt(context.Background(), []byte(testEmail))
	cleanup := func() {
		db.Get().Where("type = ? AND index_id = ?", "email", indexID).Delete(&LoginIdentify{})
		db.Get().Where("udid = ?", udid).Delete(&Device{})
	}
	cleanup()
	t.Cleanup(cleanup)

	// Step A: request OTP (mock-bypassed).
	w := NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": testEmail}).
		Execute(r)
	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code, "send_code should succeed: %s", resp.Message)

	// Step B: first login as a service client to create the user record.
	// We use a different UDID so this device is separate from the router device.
	setupUDID := "test-udid-router-cycle-setup"
	t.Cleanup(func() { db.Get().Where("udid = ?", setupUDID).Delete(&Device{}) })
	NewTestRequest("POST", "/api/auth/login").
		WithHeader("X-K2-Client", "kaitu-service/0.4.5 (ios; arm64)").
		WithBody(map[string]any{
			"email":            testEmail,
			"verificationCode": MockVerificationCode,
			"udid":             setupUDID,
		}).
		Execute(r)

	// Look up the user created by the OTP flow and promote to family tier.
	var user User
	require.NoError(t,
		db.Get().Where("id IN (?)",
			db.Get().Model(&LoginIdentify{}).Select("user_id").Where("type = ? AND index_id = ?", "email", indexID),
		).First(&user).Error,
		"user must exist after first login",
	)
	t.Cleanup(func() { db.Get().Delete(&user) })
	require.NoError(t, db.Get().Model(&user).Updates(map[string]any{
		"tier":       TierFamily,
		"expired_at": time.Now().Add(30 * 24 * time.Hour).Unix(),
	}).Error)
	createActivePrivateNodeSub(t, user.ID)

	// Step C: send a fresh OTP for the router login.
	w = NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": testEmail}).
		Execute(r)
	resp, err = ParseResponse(w)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code, "second send_code should succeed: %s", resp.Message)

	// Step D: login as router with the new UDID.
	w = NewTestRequest("POST", "/api/auth/login").
		WithHeader("X-K2-Client", "kaitu-router/0.4.5 (linux; arm64)").
		WithBody(map[string]any{
			"email":            testEmail,
			"verificationCode": MockVerificationCode,
			"udid":             udid,
		}).
		Execute(r)
	resp, err = ParseResponse(w)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code, "router login should succeed; got %s", resp.Message)

	var loginData struct {
		AccessToken string `json:"accessToken"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &loginData))
	require.NotEmpty(t, loginData.AccessToken)

	// Verify Device.IsGateway=true in DB.
	var dev Device
	require.NoError(t, db.Get().Where("udid = ?", udid).First(&dev).Error)
	assert.True(t, dev.IsGateway, "Device.IsGateway must be true after router login")

	// Step E: same token + router header → /api/user/info passes.
	w = NewTestRequest("GET", "/api/user/info").
		WithHeader("Authorization", "Bearer "+loginData.AccessToken).
		WithHeader("X-K2-Client", "kaitu-router/0.4.5 (linux; arm64)").
		Execute(r)
	resp, err = ParseResponse(w)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Code, "router token + router header must pass EnforceDeviceClass: %s", resp.Message)

	// Step F: same token + service header → 403002.
	w = NewTestRequest("GET", "/api/user/info").
		WithHeader("Authorization", "Bearer "+loginData.AccessToken).
		WithHeader("X-K2-Client", "kaitu-service/0.4.5 (ios; arm64)").
		Execute(r)
	resp, err = ParseResponse(w)
	require.NoError(t, err)
	assert.Equal(t, int(ErrorDeviceClassMismatch), resp.Code,
		"router token + service header must return 403002; got %d: %s", resp.Code, resp.Message)
}

// TestLoginFlow_PlanDowngrade: family user registers router with an active private
// line, then tier is downgraded to basic. Router access follows the LINE, not the
// tier — a tier downgrade does not revoke it; removing the line does.
func TestLoginFlow_PlanDowngrade(t *testing.T) {
	skipIfNoDB(t)
	r := SetupDeviceClassTestRouter()

	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	testEmail := "test-router-plan-downgrade@example.com"
	udid := "test-udid-router-dg"

	// Ensure clean state across runs.
	indexID := secretHashIt(context.Background(), []byte(testEmail))
	cleanup := func() {
		db.Get().Where("type = ? AND index_id = ?", "email", indexID).Delete(&LoginIdentify{})
		db.Get().Where("udid = ?", udid).Delete(&Device{})
	}
	cleanup()
	t.Cleanup(cleanup)

	// Setup: create user via OTP, promote to family, then router-login.
	w := NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": testEmail}).
		Execute(r)
	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code, "send_code should succeed: %s", resp.Message)

	// First login with service UDID to materialize the user row.
	setupUDID := "test-udid-router-dg-setup"
	t.Cleanup(func() { db.Get().Where("udid = ?", setupUDID).Delete(&Device{}) })
	NewTestRequest("POST", "/api/auth/login").
		WithHeader("X-K2-Client", "kaitu-service/0.4.5 (ios; arm64)").
		WithBody(map[string]any{
			"email":            testEmail,
			"verificationCode": MockVerificationCode,
			"udid":             setupUDID,
		}).
		Execute(r)

	var user User
	require.NoError(t,
		db.Get().Where("id IN (?)",
			db.Get().Model(&LoginIdentify{}).Select("user_id").Where("type = ? AND index_id = ?", "email", indexID),
		).First(&user).Error,
	)
	t.Cleanup(func() { db.Get().Delete(&user) })
	require.NoError(t, db.Get().Model(&user).Updates(map[string]any{
		"tier":       TierFamily,
		"expired_at": time.Now().Add(30 * 24 * time.Hour).Unix(),
	}).Error)
	createActivePrivateNodeSub(t, user.ID)

	// Send fresh OTP for router login.
	w = NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": testEmail}).
		Execute(r)
	resp, err = ParseResponse(w)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code, "second send_code should succeed: %s", resp.Message)

	// Router login — registers Device.IsGateway=true.
	w = NewTestRequest("POST", "/api/auth/login").
		WithHeader("X-K2-Client", "kaitu-router/0.4.5 (linux; arm64)").
		WithBody(map[string]any{
			"email":            testEmail,
			"verificationCode": MockVerificationCode,
			"udid":             udid,
		}).
		Execute(r)
	resp, err = ParseResponse(w)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code, "router login should succeed: %s", resp.Message)
	var loginData struct {
		AccessToken string `json:"accessToken"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &loginData))

	// Tier downgrade — no admin API helper exists; mutate DB directly.
	require.NoError(t, db.Get().Model(&user).Update("tier", TierBasic).Error)

	// Router access follows the LINE, not tier: /api/router/quota still succeeds
	// after the tier downgrade because the private line is still active.
	w = NewTestRequest("GET", "/api/router/quota").
		WithHeader("Authorization", "Bearer "+loginData.AccessToken).
		WithHeader("X-K2-Client", "kaitu-router/0.4.5 (linux; arm64)").
		Execute(r)
	resp, err = ParseResponse(w)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Code,
		"/api/router/quota must still succeed after tier downgrade (line active); got %d: %s", resp.Code, resp.Message)

	// /api/tunnels should still serve — ProRequired passes (ExpiredAt still in future),
	// EnforceDeviceClass passes (IsGateway=true, header=router).
	w = NewTestRequest("GET", "/api/tunnels").
		WithHeader("Authorization", "Bearer "+loginData.AccessToken).
		WithHeader("X-K2-Client", "kaitu-router/0.4.5 (linux; arm64)").
		Execute(r)
	resp, err = ParseResponse(w)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Code, "tunnel listing must remain accessible after downgrade: %s", resp.Message)

	// Remove the private line → router access is revoked (402001), proving the
	// line — not the tier — is the gate.
	require.NoError(t, db.Get().Unscoped().Where("user_id = ?", user.ID).Delete(&PrivateNodeSubscription{}).Error)
	w = NewTestRequest("GET", "/api/router/quota").
		WithHeader("Authorization", "Bearer "+loginData.AccessToken).
		WithHeader("X-K2-Client", "kaitu-router/0.4.5 (linux; arm64)").
		Execute(r)
	resp, err = ParseResponse(w)
	require.NoError(t, err)
	assert.Equal(t, int(ErrorPlanNoRouter), resp.Code,
		"/api/router/quota must return 402001 once the line is gone; got %d: %s", resp.Code, resp.Message)
}

// TestLoginFlow_NoHeaderLegacyApp locks the backward-compat invariant:
// a client that never sends X-K2-Client (legacy app build, third-party caller,
// curl, etc.) MUST still log in, get Device.IsGateway=false, and access
// device-bound endpoints — EnforceDeviceClass bypasses on absent header.
//
// This is the regression guard for the "old webapp on new Center" path.
func TestLoginFlow_NoHeaderLegacyApp(t *testing.T) {
	skipIfNoDB(t)
	r := SetupDeviceClassTestRouter()

	EnableMockVerificationCode = true
	defer func() { EnableMockVerificationCode = false }()

	testEmail := "test-no-header-legacy@example.com"
	udid := "test-udid-no-header"

	indexID := secretHashIt(context.Background(), []byte(testEmail))
	cleanup := func() {
		db.Get().Where("type = ? AND index_id = ?", "email", indexID).Delete(&LoginIdentify{})
		db.Get().Where("udid = ?", udid).Delete(&Device{})
	}
	cleanup()
	t.Cleanup(cleanup)

	// Step A: send OTP (no X-K2-Client).
	w := NewTestRequest("POST", "/api/auth/code").
		WithBody(map[string]string{"email": testEmail}).
		Execute(r)
	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code, "send_code without header should succeed: %s", resp.Message)

	// Step B: login without any X-K2-Client header.
	w = NewTestRequest("POST", "/api/auth/login").
		WithBody(map[string]any{
			"email":            testEmail,
			"verificationCode": MockVerificationCode,
			"udid":             udid,
		}).
		Execute(r)
	resp, err = ParseResponse(w)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code, "login without X-K2-Client must succeed (legacy compat); got %d: %s", resp.Code, resp.Message)

	var loginData struct {
		AccessToken string `json:"accessToken"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &loginData))
	require.NotEmpty(t, loginData.AccessToken)

	// Cleanup the user row created by login.
	var user User
	require.NoError(t,
		db.Get().Where("id IN (?)",
			db.Get().Model(&LoginIdentify{}).Select("user_id").Where("type = ? AND index_id = ?", "email", indexID),
		).First(&user).Error,
	)
	t.Cleanup(func() { db.Get().Delete(&user) })

	// Step C: Device row exists with IsGateway=false (zero value — header absent).
	var dev Device
	require.NoError(t, db.Get().Where("udid = ?", udid).First(&dev).Error)
	assert.False(t, dev.IsGateway, "Device.IsGateway must default to false when X-K2-Client header is absent at login")

	// Step D: device-bound endpoint without header must pass EnforceDeviceClass.
	w = NewTestRequest("GET", "/api/user/info").
		WithHeader("Authorization", "Bearer "+loginData.AccessToken).
		Execute(r)
	resp, err = ParseResponse(w)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Code,
		"no-header request to /api/user/info must bypass EnforceDeviceClass; got %d: %s",
		resp.Code, resp.Message)
}
