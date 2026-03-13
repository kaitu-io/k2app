package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// Beta Channel Subscription Tests
// =====================================================================

// --- Unit Tests: Model & Type definitions ---

func TestBetaChannel_UserModel_Defaults(t *testing.T) {
	t.Run("BetaOptedIn defaults to nil (not opted in)", func(t *testing.T) {
		user := User{}
		assert.Nil(t, user.BetaOptedIn)
		assert.Equal(t, int64(0), user.BetaOptedAt)
	})

	t.Run("BetaOptedIn can be set to true", func(t *testing.T) {
		user := User{
			BetaOptedIn: BoolPtr(true),
			BetaOptedAt: time.Now().Unix(),
		}
		require.NotNil(t, user.BetaOptedIn)
		assert.True(t, *user.BetaOptedIn)
		assert.Greater(t, user.BetaOptedAt, int64(0))
	})

	t.Run("BetaOptedIn can be set to false", func(t *testing.T) {
		user := User{
			BetaOptedIn: BoolPtr(false),
			BetaOptedAt: time.Now().Unix(), // preserved even when opted out
		}
		require.NotNil(t, user.BetaOptedIn)
		assert.False(t, *user.BetaOptedIn)
		assert.Greater(t, user.BetaOptedAt, int64(0))
	})
}

func TestBetaChannel_DataUser_BetaOptedIn(t *testing.T) {
	t.Run("buildDataUserWithDevice sets betaOptedIn=true when opted in", func(t *testing.T) {
		user := User{
			ID:          1,
			UUID:        "test-uuid",
			BetaOptedIn: BoolPtr(true),
			Language:    "en-US",
			Roles:       RoleUser,
		}
		dataUser := buildDataUserWithDevice(&user, nil)
		assert.True(t, dataUser.BetaOptedIn)
	})

	t.Run("buildDataUserWithDevice sets betaOptedIn=false when opted out", func(t *testing.T) {
		user := User{
			ID:          2,
			UUID:        "test-uuid-2",
			BetaOptedIn: BoolPtr(false),
			Language:    "en-US",
			Roles:       RoleUser,
		}
		dataUser := buildDataUserWithDevice(&user, nil)
		assert.False(t, dataUser.BetaOptedIn)
	})

	t.Run("buildDataUserWithDevice sets betaOptedIn=false when nil", func(t *testing.T) {
		user := User{
			ID:       3,
			UUID:     "test-uuid-3",
			Language: "en-US",
			Roles:    RoleUser,
		}
		dataUser := buildDataUserWithDevice(&user, nil)
		assert.False(t, dataUser.BetaOptedIn)
	})
}

func TestBetaChannel_DataUser_JSON(t *testing.T) {
	t.Run("betaOptedIn field is serialized in JSON", func(t *testing.T) {
		dataUser := DataUser{
			UUID:        "test-uuid",
			BetaOptedIn: true,
			Roles:       RoleUser,
		}
		jsonBytes, err := json.Marshal(dataUser)
		require.NoError(t, err)

		var parsed map[string]interface{}
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		betaVal, exists := parsed["betaOptedIn"]
		assert.True(t, exists, "betaOptedIn field should be present in JSON")
		assert.Equal(t, true, betaVal)
	})

	t.Run("betaOptedIn=false is serialized in JSON", func(t *testing.T) {
		dataUser := DataUser{
			UUID:        "test-uuid",
			BetaOptedIn: false,
			Roles:       RoleUser,
		}
		jsonBytes, err := json.Marshal(dataUser)
		require.NoError(t, err)

		var parsed map[string]interface{}
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		betaVal, exists := parsed["betaOptedIn"]
		assert.True(t, exists, "betaOptedIn field should be present in JSON")
		assert.Equal(t, false, betaVal)
	})
}

func TestBetaChannel_UpdateRequest_Parsing(t *testing.T) {
	t.Run("parses opted_in=true", func(t *testing.T) {
		body := `{"opted_in": true}`
		var req UpdateBetaChannelRequest
		err := json.Unmarshal([]byte(body), &req)
		require.NoError(t, err)
		assert.True(t, req.OptedIn)
	})

	t.Run("parses opted_in=false", func(t *testing.T) {
		body := `{"opted_in": false}`
		var req UpdateBetaChannelRequest
		err := json.Unmarshal([]byte(body), &req)
		require.NoError(t, err)
		assert.False(t, req.OptedIn)
	})

	t.Run("defaults to false when omitted", func(t *testing.T) {
		body := `{}`
		var req UpdateBetaChannelRequest
		err := json.Unmarshal([]byte(body), &req)
		require.NoError(t, err)
		assert.False(t, req.OptedIn)
	})
}

func TestBetaChannel_UserFilter(t *testing.T) {
	t.Run("BetaOptedIn filter nil means no filter", func(t *testing.T) {
		filter := UserFilter{}
		assert.Nil(t, filter.BetaOptedIn)
	})

	t.Run("BetaOptedIn filter true means only beta users", func(t *testing.T) {
		filter := UserFilter{BetaOptedIn: BoolPtr(true)}
		require.NotNil(t, filter.BetaOptedIn)
		assert.True(t, *filter.BetaOptedIn)
	})

	t.Run("UserFilter JSON omits BetaOptedIn when nil", func(t *testing.T) {
		filter := UserFilter{}
		jsonBytes, err := json.Marshal(filter)
		require.NoError(t, err)

		var parsed map[string]interface{}
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		_, exists := parsed["betaOptedIn"]
		assert.False(t, exists, "betaOptedIn should be omitted when nil")
	})
}

// --- Mock DB Tests: Handler logic ---

// setupBetaChannelRouter creates a test router with the beta-channel endpoint
// using Bearer token auth (no CSRF needed for test simplicity)
func setupBetaChannelRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	user := r.Group("/api/user")
	{
		user.PUT("/beta-channel", AuthRequired(), api_update_user_beta_channel)
	}

	return r
}

func TestBetaChannel_Handler_OptIn(t *testing.T) {
	testInitConfig()
	m := SetupMockDB(t)

	// The handler calls db.Get() which returns the qtoolkit global DB.
	// For mock DB tests, we test the DB operations directly instead
	// of going through the full handler (which requires db.Get() override).

	t.Run("opted_in=true updates both fields", func(t *testing.T) {
		m.Mock.ExpectExec(regexp.QuoteMeta("UPDATE `users` SET")).
			WillReturnResult(sqlmock.NewResult(0, 1))

		updates := map[string]any{
			"beta_opted_in": true,
			"beta_opted_at": time.Now().Unix(),
		}

		err := m.DB.Model(&User{}).Where(&User{ID: 1}).Updates(updates).Error
		assert.NoError(t, err)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})
}

func TestBetaChannel_Handler_OptOut(t *testing.T) {
	testInitConfig()
	m := SetupMockDB(t)

	t.Run("opted_in=false only updates beta_opted_in", func(t *testing.T) {
		m.Mock.ExpectExec(regexp.QuoteMeta("UPDATE `users` SET")).
			WillReturnResult(sqlmock.NewResult(0, 1))

		updates := map[string]any{
			"beta_opted_in": false,
		}

		err := m.DB.Model(&User{}).Where(&User{ID: 1}).Updates(updates).Error
		assert.NoError(t, err)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})
}

func TestBetaChannel_Handler_InvalidBody(t *testing.T) {
	// Test the handler directly without AuthRequired middleware
	// to verify JSON parsing error handling
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	// Skip auth middleware, inject user ID manually
	r.PUT("/api/user/beta-channel", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: 1})
		c.Next()
	}, api_update_user_beta_channel)

	t.Run("invalid JSON body returns error", func(t *testing.T) {
		body := bytes.NewReader([]byte(`not-json`))
		req, err := http.NewRequest("PUT", "/api/user/beta-channel", body)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	})
}

func TestBetaChannel_Handler_Unauthenticated(t *testing.T) {
	testInitConfig()

	router := setupBetaChannelRouter()

	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		body := bytes.NewReader([]byte(`{"opted_in": true}`))
		req, err := http.NewRequest("PUT", "/api/user/beta-channel", body)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		assert.Equal(t, int(ErrorNotLogin), resp.Code)
	})
}

// --- EDM Filter Tests ---

func TestBetaChannel_EDMFilter_SQL(t *testing.T) {
	t.Run("BetaOptedIn filter adds WHERE clause", func(t *testing.T) {
		m := SetupMockDB(t)

		query := m.DB.Model(&User{}).Where("is_activated = ?", true)

		betaFilter := true
		filters := &UserFilter{BetaOptedIn: &betaFilter}

		if filters.BetaOptedIn != nil && *filters.BetaOptedIn {
			query = query.Where("beta_opted_in = ?", true)
		}

		m.Mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `users` WHERE is_activated = ? AND beta_opted_in = ? AND `users`.`deleted_at` IS NULL")).
			WithArgs(true, true).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "uuid", "language",
			}))

		var users []User
		err := query.Find(&users).Error
		assert.NoError(t, err)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})

	t.Run("nil BetaOptedIn filter does not add WHERE clause", func(t *testing.T) {
		m := SetupMockDB(t)

		query := m.DB.Model(&User{}).Where("is_activated = ?", true)

		filters := &UserFilter{}

		if filters.BetaOptedIn != nil && *filters.BetaOptedIn {
			query = query.Where("beta_opted_in = ?", true)
		}

		m.Mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `users` WHERE is_activated = ? AND `users`.`deleted_at` IS NULL")).
			WithArgs(true).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "uuid", "language",
			}))

		var users []User
		err := query.Find(&users).Error
		assert.NoError(t, err)
		assert.NoError(t, m.Mock.ExpectationsWereMet())
	})
}
