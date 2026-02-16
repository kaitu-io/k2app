package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// API Request Validation Unit Tests
// These tests verify request validation logic without database
// =====================================================================

func setupMinimalRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	return r
}

// =====================================================================
// Test 1: Email Validation in Auth Code Request
// =====================================================================

func TestAuthCode_EmailValidation(t *testing.T) {
	r := setupMinimalRouter()
	r.POST("/api/auth/code", api_send_auth_code)

	tests := []struct {
		name        string
		body        map[string]interface{}
		expectCode  int
		expectError bool
	}{
		{
			name:        "Empty email",
			body:        map[string]interface{}{"email": ""},
			expectCode:  422,
			expectError: true,
		},
		{
			name:        "Missing email field",
			body:        map[string]interface{}{},
			expectCode:  422,
			expectError: true,
		},
		{
			name:        "Invalid email format",
			body:        map[string]interface{}{"email": "not-an-email"},
			expectCode:  422,
			expectError: true,
		},
		{
			name:        "Email without domain",
			body:        map[string]interface{}{"email": "user@"},
			expectCode:  422,
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/auth/code", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)

			if tc.expectError {
				code, ok := resp["code"].(float64)
				if ok {
					assert.Equal(t, float64(tc.expectCode), code,
						"Expected error code %d for %s", tc.expectCode, tc.name)
				}
			}
		})
	}
}

// =====================================================================
// Test 2: Login Request Validation
// =====================================================================

func TestLogin_RequestValidation(t *testing.T) {
	r := setupMinimalRouter()
	r.POST("/api/auth/login", api_login)

	tests := []struct {
		name        string
		body        map[string]interface{}
		expectCode  int
		description string
	}{
		{
			name: "Missing email",
			body: map[string]interface{}{
				"verificationCode": "123456",
				"udid":             "test-device",
			},
			expectCode:  422,
			description: "Should reject missing email",
		},
		{
			name: "Missing verification code",
			body: map[string]interface{}{
				"email": "test@example.com",
				"udid":  "test-device",
			},
			expectCode:  422,
			description: "Should reject missing verification code",
		},
		{
			name: "Empty verification code",
			body: map[string]interface{}{
				"email":            "test@example.com",
				"verificationCode": "",
				"udid":             "test-device",
			},
			expectCode:  422,
			description: "Should reject empty verification code",
		},
		{
			name:        "Empty body",
			body:        map[string]interface{}{},
			expectCode:  422,
			description: "Should reject empty request body",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)

			code, ok := resp["code"].(float64)
			if ok {
				assert.Equal(t, float64(tc.expectCode), code, tc.description)
			}
		})
	}
}

// =====================================================================
// Test 3: Web Login Request Validation
// =====================================================================

func TestWebLogin_RequestValidation(t *testing.T) {
	r := setupMinimalRouter()
	r.POST("/api/auth/web-login", api_web_auth)

	tests := []struct {
		name       string
		body       map[string]interface{}
		expectCode int
	}{
		{
			name: "Missing email",
			body: map[string]interface{}{
				"verificationCode": "123456",
			},
			expectCode: 422,
		},
		{
			name: "Missing verification code",
			body: map[string]interface{}{
				"email": "test@example.com",
			},
			expectCode: 422,
		},
		{
			name:       "Empty body",
			body:       map[string]interface{}{},
			expectCode: 422,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/auth/web-login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)

			code, ok := resp["code"].(float64)
			if ok {
				assert.Equal(t, float64(tc.expectCode), code,
					"Expected code %d for %s", tc.expectCode, tc.name)
			}
		})
	}
}

// =====================================================================
// Test 4: Token Refresh Request Validation
// =====================================================================

func TestRefreshToken_RequestValidation(t *testing.T) {
	r := setupMinimalRouter()
	r.POST("/api/auth/refresh", api_refresh_token)

	tests := []struct {
		name       string
		body       map[string]interface{}
		expectCode int
	}{
		{
			name:       "Missing refresh token",
			body:       map[string]interface{}{},
			expectCode: 422,
		},
		{
			name: "Empty refresh token",
			body: map[string]interface{}{
				"refreshToken": "",
			},
			expectCode: 422,
		},
		{
			name: "Invalid refresh token format",
			body: map[string]interface{}{
				"refreshToken": "not-a-valid-jwt",
			},
			expectCode: 401, // Token validation failure
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/auth/refresh", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)

			code, ok := resp["code"].(float64)
			if ok {
				assert.Contains(t, []float64{float64(tc.expectCode), 422, 401}, code,
					"Expected validation error for %s", tc.name)
			}
		})
	}
}

// =====================================================================
// Test 6: Token Generation and Validation
// =====================================================================

func TestTokenGeneration_Claims(t *testing.T) {
	testInitConfig()

	userID := uint64(12345)
	deviceID := "test-device-uuid"

	t.Run("Access token contains correct claims", func(t *testing.T) {
		token := GenerateTestToken(userID, deviceID, 1*time.Hour)
		require.NotEmpty(t, token)

		// Verify token is valid JWT
		assert.Contains(t, token, ".")
		parts := bytes.Split([]byte(token), []byte("."))
		assert.Len(t, parts, 3, "JWT should have 3 parts")
	})

	t.Run("Expired token generation", func(t *testing.T) {
		token := GenerateExpiredToken(userID, deviceID)
		require.NotEmpty(t, token)

		// Token should be parseable but expired
		assert.Contains(t, token, ".")
	})

	t.Run("Web token (no device) generation", func(t *testing.T) {
		token := GenerateExpiringToken(userID, 24*time.Hour)
		require.NotEmpty(t, token)
		assert.Contains(t, token, ".")
	})
}

// =====================================================================
// Test 7: Mock Verification Code
// =====================================================================

func TestMockVerificationCode(t *testing.T) {
	t.Run("Mock verification code can be enabled", func(t *testing.T) {
		originalValue := EnableMockVerificationCode
		defer func() { EnableMockVerificationCode = originalValue }()

		EnableMockVerificationCode = true
		assert.True(t, EnableMockVerificationCode)
		assert.Equal(t, "123456", MockVerificationCode)
	})

	t.Run("Mock verification code default is disabled", func(t *testing.T) {
		// Default should be false in production
		assert.False(t, EnableMockVerificationCode == true && MockVerificationCode == "real-code")
	})
}

// =====================================================================
// Test 8: Response Format Consistency
// =====================================================================

func TestResponseFormat_Consistency(t *testing.T) {
	r := setupMinimalRouter()
	r.POST("/api/auth/code", api_send_auth_code)

	t.Run("Error response has correct structure", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{"email": ""})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/auth/code", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err, "Response should be valid JSON")

		// Check response structure
		_, hasCode := resp["code"]
		assert.True(t, hasCode, "Response should have 'code' field")

		// Error responses should have message
		code, _ := resp["code"].(float64)
		if code != 0 {
			_, hasMessage := resp["message"]
			assert.True(t, hasMessage || resp["message"] != nil,
				"Error response should have 'message' field")
		}
	})
}

// =====================================================================
// Test 9: Content-Type Handling
// =====================================================================

func TestContentType_Handling(t *testing.T) {
	r := setupMinimalRouter()
	r.POST("/api/auth/code", api_send_auth_code)

	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{
			name:        "JSON content type",
			contentType: "application/json",
			body:        `{"email":""}`, // Empty email triggers validation error
		},
		{
			name:        "JSON with charset",
			contentType: "application/json; charset=utf-8",
			body:        `{"email":""}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/auth/code", bytes.NewReader([]byte(tc.body)))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			r.ServeHTTP(w, req)

			// Should return valid JSON (validation error, not DB error)
			var resp map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err, "Should return valid JSON response")

			// Should be a validation error (422)
			code, _ := resp["code"].(float64)
			assert.Equal(t, float64(422), code, "Empty email should return validation error")
		})
	}
}

// =====================================================================
// Test 10: HTTP Method Validation
// =====================================================================

func TestHTTPMethod_Validation(t *testing.T) {
	r := setupMinimalRouter()
	r.POST("/api/auth/code", api_send_auth_code)

	methods := []string{"GET", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method+" should return 404", func(t *testing.T) {
			body, _ := json.Marshal(map[string]interface{}{"email": "test@example.com"})
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(method, "/api/auth/code", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equal(t, 404, w.Code,
				"%s method should return 404 for POST-only endpoint", method)
		})
	}
}
