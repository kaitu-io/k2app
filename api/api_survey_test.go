package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSurveyRouter creates a test router with injected auth context.
// For handler-level tests that do NOT touch db.Get(), this is sufficient.
func setupSurveyRouter(t *testing.T, user *User) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: user.ID, User: user})
		c.Next()
	})
	r.POST("/api/survey/submit", api_survey_submit)
	r.GET("/api/survey/status", api_survey_status)
	return r
}

// =====================================================================
// Survey Status — validation (no DB)
// =====================================================================

func TestSurveyStatus_MissingSurveyKey(t *testing.T) {
	user := &User{ID: 1}
	r := setupSurveyRouter(t, user)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/survey/status", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorInvalidArgument, resp.Code)
}

// =====================================================================
// Survey Status — DB operations (mock DB, tested directly)
// =====================================================================

func TestSurveyStatus_NotSubmitted(t *testing.T) {
	m := SetupMockDB(t)

	m.Mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM `survey_responses`")).
		WithArgs(uint64(1), "active_2026q1").
		WillReturnRows(sqlmock.NewRows([]string{"count(*)"}).AddRow(0))

	var count int64
	err := m.DB.Model(&SurveyResponse{}).
		Where("user_id = ? AND survey_key = ?", uint64(1), "active_2026q1").
		Count(&count).Error

	assert.NoError(t, err)
	assert.Equal(t, int64(0), count)
	assert.False(t, count > 0)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

func TestSurveyStatus_AlreadySubmitted(t *testing.T) {
	m := SetupMockDB(t)

	m.Mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM `survey_responses`")).
		WithArgs(uint64(1), "active_2026q1").
		WillReturnRows(sqlmock.NewRows([]string{"count(*)"}).AddRow(1))

	var count int64
	err := m.DB.Model(&SurveyResponse{}).
		Where("user_id = ? AND survey_key = ?", uint64(1), "active_2026q1").
		Count(&count).Error

	assert.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.True(t, count > 0)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

// =====================================================================
// Survey Submit — validation (no DB)
// =====================================================================

func TestSurveySubmit_InvalidSurveyKey(t *testing.T) {
	user := &User{ID: 1}
	r := setupSurveyRouter(t, user)

	body, _ := json.Marshal(SurveySubmitRequest{
		SurveyKey: "nonexistent_survey",
		Answers:   json.RawMessage(`{"q1":"test"}`),
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/survey/submit", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorInvalidOperation, resp.Code)
}

func TestSurveySubmit_InvalidAnswersJSON(t *testing.T) {
	user := &User{ID: 1}
	r := setupSurveyRouter(t, user)

	body, _ := json.Marshal(map[string]any{
		"survey_key": "active_2026q1",
		"answers":    "not an object",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/survey/submit", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorInvalidArgument, resp.Code)
}

func TestSurveySubmit_MissingRequiredFields(t *testing.T) {
	user := &User{ID: 1}
	r := setupSurveyRouter(t, user)

	t.Run("missing survey_key", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"answers": map[string]any{"q1": "test"},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/survey/submit", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp Response[DataAny]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, ErrorInvalidArgument, resp.Code)
	})

	t.Run("missing answers", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"survey_key": "active_2026q1",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/survey/submit", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp Response[DataAny]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, ErrorInvalidArgument, resp.Code)
	})

	t.Run("empty body", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/survey/submit", bytes.NewBuffer([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp Response[DataAny]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, ErrorInvalidArgument, resp.Code)
	})
}

// =====================================================================
// Survey Submit — DB operations (mock DB, tested directly)
// =====================================================================

func TestSurveySubmit_DuplicateCheck(t *testing.T) {
	m := SetupMockDB(t)

	t.Run("existing response found means duplicate", func(t *testing.T) {
		m.Mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `survey_responses`")).
			WithArgs(uint64(1), "active_2026q1", 1).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "created_at", "user_id", "survey_key", "answers", "ip_address", "reward_days",
			}).AddRow(1, nil, 1, "active_2026q1", `{"q1":"a"}`, "127.0.0.1", 30))

		var existing SurveyResponse
		err := m.DB.Where("user_id = ? AND survey_key = ?", uint64(1), "active_2026q1").
			First(&existing).Error

		assert.NoError(t, err)
		assert.Equal(t, uint64(1), existing.ID)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})
}

func TestSurveySubmit_CreateResponse(t *testing.T) {
	m := SetupMockDB(t)

	t.Run("insert survey response", func(t *testing.T) {
		m.Mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `survey_responses`")).
			WillReturnResult(sqlmock.NewResult(1, 1))

		response := &SurveyResponse{
			UserID:     1,
			SurveyKey:  "active_2026q1",
			Answers:    `{"q1":"answer1"}`,
			IPAddress:  "192.168.1.1",
			RewardDays: 30,
		}

		err := m.DB.Create(response).Error
		assert.NoError(t, err)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})
}

// =====================================================================
// Active surveys map validation
// =====================================================================

func TestActiveSurveys_RewardDays(t *testing.T) {
	t.Run("active_2026q1 exists with 30 days", func(t *testing.T) {
		days, ok := activeSurveys["active_2026q1"]
		assert.True(t, ok)
		assert.Equal(t, 30, days)
	})

	t.Run("nonexistent survey returns false", func(t *testing.T) {
		_, ok := activeSurveys["nonexistent"]
		assert.False(t, ok)
	})
}
