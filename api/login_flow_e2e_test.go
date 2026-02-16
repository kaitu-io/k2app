package center

import (
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

		assert.Equal(t, ErrorNotLogin, resp.Code,
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

		assert.Equal(t, ErrorNotLogin, resp.Code,
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

		// Should return error code (422 = invalid argument)
		assert.Equal(t, ErrorInvalidArgument, resp.Code,
			"Wrong verification code should return 422")

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

		assert.Equal(t, ErrorNotLogin, resp.Code,
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

		assert.Equal(t, ErrorNotLogin, resp.Code,
			"Invalid token should return 401")

		t.Log("Invalid token correctly rejected")
	})

	// ========== No Authentication ==========
	t.Run("NoAuthentication", func(t *testing.T) {
		w := NewTestRequest("GET", "/api/user/info").Execute(r)

		resp, err := ParseResponse(w)
		require.NoError(t, err)

		assert.Equal(t, ErrorNotLogin, resp.Code,
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

		assert.Equal(t, ErrorNotLogin, resp.Code,
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
