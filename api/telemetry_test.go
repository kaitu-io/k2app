package center

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRuleMissTestRouter creates a minimal gin router with only the
// rule-miss endpoint. Mirrors the pattern used in api_stats_test.go:
// no global config, no mock DB — the handler drops records in Phase 1.
func setupRuleMissTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	telemetry := r.Group("/api/telemetry")
	{
		telemetry.POST("/rule_miss", api_telemetry_rule_miss)
	}
	return r
}

// resetRuleMissLimiter wipes the package-level limiter so tests don't
// interfere with each other (each test re-uses the same IP "192.0.2.1").
func resetRuleMissLimiter(t *testing.T) {
	t.Helper()
	ruleMissRateLimiter.mu.Lock()
	ruleMissRateLimiter.buckets = map[string]*ruleMissBucket{}
	ruleMissRateLimiter.mu.Unlock()
}

func validRecord(i int) RuleMissRecord {
	return RuleMissRecord{
		Hash16:     fmt.Sprintf("%016x", i),
		Country:    "US",
		WeekBucket: "2026-W15",
		Protocol:   "tcp",
	}
}

func postRuleMiss(t *testing.T, router *gin.Engine, body any) *httptest.ResponseRecorder {
	t.Helper()
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/telemetry/rule_miss", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeResp(t *testing.T, w *httptest.ResponseRecorder) Response[DataAny] {
	t.Helper()
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestRuleMiss_ValidBatch(t *testing.T) {
	resetRuleMissLimiter(t)
	router := setupRuleMissTestRouter()

	batch := RuleMissBatch{
		SchemaVersion: 1,
		ClientVersion: "0.4.3",
		RulesVersion:  "2026-04",
		SaltDay:       "2026-04-11",
		Records: []RuleMissRecord{
			validRecord(1),
			validRecord(2),
		},
	}
	w := postRuleMiss(t, router, batch)
	assert.Equal(t, http.StatusOK, w.Code)
	resp := decodeResp(t, w)
	assert.Equal(t, ErrorNone, resp.Code)
}

func TestRuleMiss_EmptyRecords(t *testing.T) {
	resetRuleMissLimiter(t)
	router := setupRuleMissTestRouter()

	batch := RuleMissBatch{
		SchemaVersion: 1,
		ClientVersion: "0.4.3",
		SaltDay:       "2026-04-11",
		Records:       nil,
	}
	w := postRuleMiss(t, router, batch)
	resp := decodeResp(t, w)
	assert.Equal(t, ErrorNone, resp.Code, "empty records is a valid no-op")
}

func TestRuleMiss_TooManyRecords(t *testing.T) {
	resetRuleMissLimiter(t)
	router := setupRuleMissTestRouter()

	records := make([]RuleMissRecord, ruleMissMaxRecords+1)
	for i := range records {
		records[i] = validRecord(i)
	}
	batch := RuleMissBatch{
		SchemaVersion: 1,
		ClientVersion: "0.4.3",
		SaltDay:       "2026-04-11",
		Records:       records,
	}
	w := postRuleMiss(t, router, batch)
	resp := decodeResp(t, w)
	assert.Equal(t, ErrorInvalidArgument, resp.Code)
}

func TestRuleMiss_BadSchemaVersion(t *testing.T) {
	resetRuleMissLimiter(t)
	router := setupRuleMissTestRouter()

	batch := RuleMissBatch{
		SchemaVersion: 99,
		Records:       []RuleMissRecord{validRecord(1)},
	}
	w := postRuleMiss(t, router, batch)
	resp := decodeResp(t, w)
	assert.Equal(t, ErrorInvalidArgument, resp.Code)
}

func TestRuleMiss_InvalidHash(t *testing.T) {
	resetRuleMissLimiter(t)
	router := setupRuleMissTestRouter()

	bad := validRecord(1)
	bad.Hash16 = "not-hex"
	batch := RuleMissBatch{
		SchemaVersion: 1,
		Records:       []RuleMissRecord{bad},
	}
	w := postRuleMiss(t, router, batch)
	resp := decodeResp(t, w)
	assert.Equal(t, ErrorInvalidArgument, resp.Code)
}

func TestRuleMiss_InvalidCountry(t *testing.T) {
	resetRuleMissLimiter(t)
	router := setupRuleMissTestRouter()

	bad := validRecord(1)
	bad.Country = "USA"
	batch := RuleMissBatch{
		SchemaVersion: 1,
		Records:       []RuleMissRecord{bad},
	}
	w := postRuleMiss(t, router, batch)
	resp := decodeResp(t, w)
	assert.Equal(t, ErrorInvalidArgument, resp.Code)
}

func TestRuleMiss_InvalidWeekBucket(t *testing.T) {
	resetRuleMissLimiter(t)
	router := setupRuleMissTestRouter()

	bad := validRecord(1)
	bad.WeekBucket = "2026-04-11"
	batch := RuleMissBatch{
		SchemaVersion: 1,
		Records:       []RuleMissRecord{bad},
	}
	w := postRuleMiss(t, router, batch)
	resp := decodeResp(t, w)
	assert.Equal(t, ErrorInvalidArgument, resp.Code)
}

func TestRuleMiss_RevealedRejectedInPhase1(t *testing.T) {
	resetRuleMissLimiter(t)
	router := setupRuleMissTestRouter()

	bad := validRecord(1)
	bad.Revealed = "example.com"
	batch := RuleMissBatch{
		SchemaVersion: 1,
		Records:       []RuleMissRecord{bad},
	}
	w := postRuleMiss(t, router, batch)
	resp := decodeResp(t, w)
	assert.Equal(t, ErrorInvalidArgument, resp.Code)
}

// TestRuleMiss_NoAuthRequired verifies that the endpoint accepts
// requests with no Authorization header, no device headers, and no
// cookies — it is explicitly anonymous.
func TestRuleMiss_NoAuthRequired(t *testing.T) {
	resetRuleMissLimiter(t)
	router := setupRuleMissTestRouter()

	batch := RuleMissBatch{
		SchemaVersion: 1,
		Records:       []RuleMissRecord{validRecord(1)},
	}
	bodyBytes, _ := json.Marshal(batch)
	req := httptest.NewRequest(http.MethodPost, "/api/telemetry/rule_miss", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	// Intentionally: no Authorization, no X-Device-*, no cookies.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	resp := decodeResp(t, w)
	assert.Equal(t, ErrorNone, resp.Code)
}

// TestRuleMiss_RateLimit fires 11 requests from the same client IP
// within the 1-minute window and verifies the 11th is rate limited.
func TestRuleMiss_RateLimit(t *testing.T) {
	resetRuleMissLimiter(t)
	router := setupRuleMissTestRouter()

	batch := RuleMissBatch{
		SchemaVersion: 1,
		Records:       []RuleMissRecord{validRecord(1)},
	}
	bodyBytes, _ := json.Marshal(batch)

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/telemetry/rule_miss", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		resp := decodeResp(t, w)
		require.Equalf(t, ErrorNone, resp.Code, "request %d should succeed", i+1)
	}

	// 11th request — should be rate limited.
	req := httptest.NewRequest(http.MethodPost, "/api/telemetry/rule_miss", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	resp := decodeResp(t, w)
	assert.Equal(t, ErrorTooManyRequests, resp.Code)
}
