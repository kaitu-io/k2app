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

// setupStatsTestRouter creates a minimal gin router with only stats routes.
// Does NOT use SetupRouter() (requires full config/asynq init) or
// SetupMockDB() (mock doesn't bridge to db.Get() used by handlers).
// Both test cases below return before any DB call, so no DB setup needed.
func setupStatsTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	stats := r.Group("/api/stats")
	{
		stats.POST("/events", api_stats_ingest)
	}
	return r
}

func TestStatsIngest_EmptyRequest(t *testing.T) {
	router := setupStatsTestRouter()

	body := `{"app_opens":[],"connections":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/stats/events", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorNone, resp.Code)
}

func TestStatsIngest_TooManyEvents(t *testing.T) {
	router := setupStatsTestRouter()

	// Create 101 app_open events — exceeds maxEventsPerRequest (100)
	events := make([]StatsAppOpenEvent, 101)
	for i := range events {
		events[i] = StatsAppOpenEvent{
			DeviceHash: "abc123",
			OS:         "macos",
			AppVersion: "0.4.0",
			CreatedAt:  time.Now(),
		}
	}
	reqBody := StatsEventRequest{AppOpens: events}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/stats/events", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorInvalidArgument, resp.Code)
}
