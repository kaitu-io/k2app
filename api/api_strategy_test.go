package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// =====================================================================
// Strategy Rules API Tests (TDD)
// =====================================================================

func setupStrategyTestRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	api := r.Group("/api")
	{
		strategy := api.Group("/strategy")
		{
			strategy.GET("/rules", api_strategy_get_rules)
		}
	}

	return r
}

func TestGetStrategyRules_NoActiveRules(t *testing.T) {
	skipIfNoConfig(t)
	r := setupStrategyTestRouter()

	req, _ := http.NewRequest("GET", "/api/strategy/rules", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response[StrategyRulesResponse]
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, ErrorNone, resp.Code)

	// When no active rules, should return default rules
	assert.NotNil(t, resp.Data)
	assert.Equal(t, "default", resp.Data.Version)
	assert.Equal(t, "\"default\"", resp.Data.ETag)
	assert.NotNil(t, resp.Data.Rules)
	assert.NotNil(t, resp.Data.Protocols)
	assert.NotNil(t, resp.Data.Default)
}

func TestGetStrategyRules_ResponseStructure(t *testing.T) {
	skipIfNoConfig(t)
	r := setupStrategyTestRouter()

	t.Run("Response contains required fields", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/strategy/rules", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var rawResp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &rawResp)
		assert.NoError(t, err)

		// Check response structure
		assert.Contains(t, rawResp, "code")
		assert.Contains(t, rawResp, "data")

		// Check data structure
		if data, ok := rawResp["data"].(map[string]interface{}); ok {
			assert.Contains(t, data, "version")
			assert.Contains(t, data, "updatedAt")
			assert.Contains(t, data, "etag")
			assert.Contains(t, data, "rules")
			assert.Contains(t, data, "protocols")
			assert.Contains(t, data, "default")
		} else {
			t.Error("Expected data to be a map")
		}
	})
}

func TestGetStrategyRules_DefaultRulesContent(t *testing.T) {
	skipIfNoConfig(t)
	r := setupStrategyTestRouter()

	req, _ := http.NewRequest("GET", "/api/strategy/rules", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp Response[StrategyRulesResponse]
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)

	// Verify default rules contain expected protocol chain
	if resp.Data != nil && resp.Data.Default != nil {
		protocolChain, ok := resp.Data.Default["protocol_chain"]
		if ok {
			chain, isSlice := protocolChain.([]interface{})
			if isSlice {
				assert.Contains(t, chain, "k2:quic_bbr")
				assert.Contains(t, chain, "k2:tcp_ws")
			}
		}

		// Verify default timeout
		timeout, ok := resp.Data.Default["timeout_ms"]
		if ok {
			assert.Equal(t, float64(5000), timeout)
		}
	}
}

func TestGetStrategyRules_HTTPMethod(t *testing.T) {
	skipIfNoConfig(t)
	r := setupStrategyTestRouter()

	methods := []string{"POST", "PUT", "DELETE", "PATCH"}
	for _, method := range methods {
		t.Run(method+" should return 404", func(t *testing.T) {
			req, _ := http.NewRequest(method, "/api/strategy/rules", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, 404, w.Code,
				"%s method should return 404 for GET-only endpoint", method)
		})
	}
}

func TestGetStrategyRules_ETagCaching(t *testing.T) {
	skipIfNoConfig(t)
	r := setupStrategyTestRouter()

	t.Run("Response includes ETag header for default rules", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/strategy/rules", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Default rules should have ETag in response body
		var resp Response[StrategyRulesResponse]
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, "\"default\"", resp.Data.ETag)
	})

	t.Run("If-None-Match with non-matching ETag returns full response", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/strategy/rules", nil)
		req.Header.Set("If-None-Match", "\"wrong-etag\"")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return full response when ETag doesn't match
		assert.Equal(t, http.StatusOK, w.Code)

		var resp Response[StrategyRulesResponse]
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, ErrorNone, resp.Code)
		assert.NotNil(t, resp.Data)
	})
}

// =====================================================================
// Telemetry Batch API Tests (TDD)
// =====================================================================

func setupTelemetryTestRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	api := r.Group("/api")
	{
		telemetry := api.Group("/telemetry")
		{
			telemetry.POST("/batch", api_strategy_telemetry_batch)
		}
	}

	return r
}

func TestTelemetryBatch_InvalidRequest(t *testing.T) {
	skipIfNoConfig(t)
	r := setupTelemetryTestRouter()

	t.Run("Missing required fields returns error", func(t *testing.T) {
		body := `{}`
		req, _ := http.NewRequest("POST", "/api/telemetry/batch", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		// Should return validation error code
		assert.NotEqual(t, float64(0), resp["code"])
	})

	t.Run("Invalid event type returns error", func(t *testing.T) {
		body := `{
			"deviceId": "test-device",
			"appVersion": "1.0.0",
			"events": [
				{
					"eventId": "evt-001",
					"timestamp": 1737187200000,
					"eventType": "invalid_type"
				}
			]
		}`
		req, _ := http.NewRequest("POST", "/api/telemetry/batch", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		// Should return validation error
		assert.NotEqual(t, float64(0), resp["code"])
	})
}

func TestTelemetryBatch_DeviceNotFound(t *testing.T) {
	skipIfNoConfig(t)
	r := setupTelemetryTestRouter()

	body := `{
		"deviceId": "nonexistent-device-12345",
		"appVersion": "1.0.0",
		"events": [
			{
				"eventId": "evt-001",
				"timestamp": 1737187200000,
				"eventType": "connection"
			}
		]
	}`

	req, _ := http.NewRequest("POST", "/api/telemetry/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	// Should return not found error for unknown device
	assert.NotEqual(t, float64(0), resp["code"])
}

func TestTelemetryBatch_ResponseStructure(t *testing.T) {
	skipIfNoConfig(t)
	r := setupTelemetryTestRouter()

	// Even with device not found, we test the response parsing capability
	body := `{
		"deviceId": "test-device",
		"appVersion": "1.0.0",
		"events": []
	}`

	req, _ := http.NewRequest("POST", "/api/telemetry/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	// Response should have code and message at minimum
	assert.Contains(t, resp, "code")
}

func TestTelemetryBatch_HTTPMethod(t *testing.T) {
	skipIfNoConfig(t)
	r := setupTelemetryTestRouter()

	methods := []string{"GET", "PUT", "DELETE", "PATCH"}
	for _, method := range methods {
		t.Run(method+" should return 404", func(t *testing.T) {
			req, _ := http.NewRequest(method, "/api/telemetry/batch", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, 404, w.Code,
				"%s method should return 404 for POST-only endpoint", method)
		})
	}
}
