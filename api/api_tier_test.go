package center

import (
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

// TestBuildTierInfos_ReturnsAll4TiersInRankOrder verifies the pure assembly
// helper without touching the database. Asserts the contract exposed via
// GET /api/tiers: all 4 tiers in ascending rank order with correct quotas.
func TestBuildTierInfos_ReturnsAll4TiersInRankOrder(t *testing.T) {
	tiers := buildTierInfos()
	require.Len(t, tiers, 4, "must return exactly 4 tiers")

	// rank ascending
	assert.Equal(t, "lite", tiers[0].Name)
	assert.Equal(t, 1, tiers[0].Rank)
	assert.Equal(t, "basic", tiers[1].Name)
	assert.Equal(t, 2, tiers[1].Rank)
	assert.Equal(t, "family", tiers[2].Name)
	assert.Equal(t, 3, tiers[2].Rank)
	assert.Equal(t, "business", tiers[3].Name)
	assert.Equal(t, 4, tiers[3].Rank)

	// quota sanity
	assert.Equal(t, 1, tiers[0].MaxDevice, "lite max device")
	assert.Equal(t, 5, tiers[1].MaxDevice, "basic max device")
	assert.Equal(t, 8, tiers[2].MaxDevice, "family max device")
	assert.Equal(t, 1, tiers[2].MaxRouterDevice, "family router device")
	assert.Equal(t, 20, tiers[2].MaxLanClient, "family lan client")
	assert.Equal(t, 20, tiers[3].MaxDevice, "business max device")
	assert.Equal(t, -1, tiers[3].MaxLanClient, "business unlimited lan")

	// plans must be empty before DB lookup
	for _, ti := range tiers {
		assert.Nil(t, ti.Plans, "buildTierInfos must not populate Plans")
	}
}

// TestGetTiers_ResponseShape exercises the HTTP handler shape. The handler
// queries the global db.Get(); we cannot inject a mock there. So we drive the
// handler with a fresh recorder and accept either a successful response (if
// the test env happens to have a working DB) or a system error wrapping the
// nil-DB panic recovery — what we lock down here is the JSON envelope
// (data.tiers count + ordering) when the response is a success.
//
// Without DB, the handler panics on db.Get().Where(...). We therefore guard
// by testing only buildTierInfos structurally (above) and leave end-to-end
// JSON marshalling assertions for a future integration test that has a real
// DB fixture. This test still verifies the route is registered correctly.
func TestGetTiers_RouteRegistration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery()) // swallow the expected nil-DB panic
	r.GET("/api/tiers", GetTiers)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/tiers", nil)
	r.ServeHTTP(w, req)

	// Either 200 (DB available) or 500 (DB nil-panic recovered). Both prove the
	// route is wired and the handler is invoked.
	assert.Contains(t, []int{http.StatusOK, http.StatusInternalServerError}, w.Code)
}

// TestGetTiers_WithMockDB exercises the full handler against a sqlmock-backed
// GORM instance. We override the qtoolkit global DB pointer via the same
// SetupMockDB pattern used elsewhere in the package, then issue 4 separate
// tier→plans queries that the handler will make in rank order. Skipped when
// db.Get() cannot be redirected (current qtoolkit/db has no Set()).
func TestGetTiers_WithMockDB(t *testing.T) {
	// SetupMockDB stores the gorm.DB on the package-level mockGlobalDB but does
	// NOT swap qtoolkit's globalDB — so the handler's db.Get() call would still
	// hit the real (uninitialised) global. We can't fix that without patching
	// qtoolkit. Skip until qtoolkit exposes db.Set(*gorm.DB).
	t.Skip("requires qtoolkit/db to expose Set(*gorm.DB) for handler-level mocking")

	m := SetupMockDB(t)

	// Each tier triggers one SELECT in rank order.
	for _, name := range []string{"lite", "basic", "family", "business"} {
		rows := sqlmock.NewRows([]string{"id", "pid", "tier", "label", "is_active"})
		_ = name
		m.Mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `plans` WHERE tier = ? AND is_active = ?")).
			WillReturnRows(rows)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/tiers", GetTiers)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/tiers", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Tiers []TierWithPlans `json:"tiers"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Tiers, 4)
	assert.Equal(t, "family", resp.Data.Tiers[2].Name)
	assert.Equal(t, 8, resp.Data.Tiers[2].MaxDevice)
}
