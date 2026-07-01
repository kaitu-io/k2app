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
	db "github.com/wordgate/qtoolkit/db"
)

// orderRegionTestRouter wires POST /api/user/orders behind real auth, mirroring
// the production route (AuthRequired + EnforceDeviceClass). Used by the
// private-node region tests.
func orderRegionTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	user := r.Group("/api/user", AuthRequired(), EnforceDeviceClass())
	user.POST("/orders", api_create_order)
	return r
}

// seedPrivateNodePlan seeds a private_node plan + spec with the given allowed
// regions JSON, registering cleanup. Returns the created plan.
func seedPrivateNodePlan(t *testing.T, pid, allowedRegionsJSON string) *Plan {
	t.Helper()
	plan := &Plan{PID: pid, Label: "专属节点测试", Price: 9900, Month: 1,
		Tier: TierBasic, Product: ProductPrivateNode, IsActive: BoolPtr(true), Highlight: BoolPtr(false)}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(plan) })

	spec := &PrivateNodePlanSpec{PlanID: plan.ID, IPType: IPTypeNonResidential,
		AllowedRegions: allowedRegionsJSON, TrafficTotalBytes: 2 << 40}
	require.NoError(t, db.Get().Create(spec).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(spec) })
	return plan
}

// latestOrderForUser returns the most recently created order row for a user, or
// nil if none exists. Used to assert persistence independent of the handler's
// final HTTP response (the order row is created before the wordgate txn).
func latestOrderForUser(t *testing.T, userID uint64) *Order {
	t.Helper()
	var o Order
	err := db.Get().Where(&Order{UserID: userID}).Order("id DESC").First(&o).Error
	if err != nil {
		return nil
	}
	return &o
}

// TestCreateOrder_PrivateNodeRegion covers region accept/validate/persist at
// order creation for private_node plans.
func TestCreateOrder_PrivateNodeRegion(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	r := orderRegionTestRouter()

	post := func(t *testing.T, userID uint64, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		token := GenerateTestToken(userID, "", time.Hour)
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)
		req, _ := http.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// Case 1: valid region ∈ allowedRegions → order persists PrivateNodeRegion.
	// The order row is created (db.Get().Create) before the wordgate txn, so the
	// persisted region is asserted directly from the DB regardless of whether the
	// downstream wordgate call succeeds in the test environment.
	t.Run("valid region persists", func(t *testing.T) {
		user := CreateTestUser(t)
		plan := seedPrivateNodePlan(t, "test-pn-region-ok", `["us-east-1","ap-northeast-1"]`)
		t.Cleanup(func() { db.Get().Unscoped().Where(&Order{UserID: user.ID}).Delete(&Order{}) })

		w := post(t, user.ID, map[string]any{"plan": plan.PID, "region": "ap-northeast-1"})
		require.Equal(t, http.StatusOK, w.Code)

		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		// Must NOT be a validation rejection.
		assert.NotEqual(t, float64(ErrorInvalidArgument), resp["code"],
			"valid region must not be rejected (got code=%v message=%v)", resp["code"], resp["message"])

		order := latestOrderForUser(t, user.ID)
		require.NotNil(t, order, "order row must be persisted before the wordgate txn")
		assert.Equal(t, "ap-northeast-1", order.PrivateNodeRegion,
			"selected region must persist on the Order row")
	})

	// Case 2: invalid region ∉ allowedRegions → ErrorInvalidArgument, no order.
	t.Run("invalid region rejected, no order", func(t *testing.T) {
		user := CreateTestUser(t)
		plan := seedPrivateNodePlan(t, "test-pn-region-bad", `["us-east-1","ap-northeast-1"]`)

		w := post(t, user.ID, map[string]any{"plan": plan.PID, "region": "eu-west-1"})
		require.Equal(t, http.StatusOK, w.Code)

		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, float64(ErrorInvalidArgument), resp["code"],
			"region not in allowed list must return ErrorInvalidArgument")

		order := latestOrderForUser(t, user.ID)
		assert.Nil(t, order, "no order may be created when region validation fails")
	})

	// Case 3: empty region → allowed (provision-time fallback); persists empty.
	t.Run("empty region allowed, persists empty", func(t *testing.T) {
		user := CreateTestUser(t)
		plan := seedPrivateNodePlan(t, "test-pn-region-empty", `["us-east-1","ap-northeast-1"]`)
		t.Cleanup(func() { db.Get().Unscoped().Where(&Order{UserID: user.ID}).Delete(&Order{}) })

		w := post(t, user.ID, map[string]any{"plan": plan.PID, "region": ""})
		require.Equal(t, http.StatusOK, w.Code)

		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.NotEqual(t, float64(ErrorInvalidArgument), resp["code"],
			"empty region must not be rejected (got code=%v message=%v)", resp["code"], resp["message"])

		order := latestOrderForUser(t, user.ID)
		require.NotNil(t, order, "order row must be persisted")
		assert.Equal(t, "", order.PrivateNodeRegion, "empty region persists empty")
	})

	// Case 4: shared plan with a region value → ignored, order created normally.
	t.Run("shared plan ignores region", func(t *testing.T) {
		user := CreateTestUser(t)
		sharedPlan := &Plan{PID: "test-shared-region", Label: "共享订阅测试", Price: 1900, Month: 1,
			Tier: TierBasic, Product: ProductApp, IsActive: BoolPtr(true), Highlight: BoolPtr(false)}
		require.NoError(t, db.Get().Create(sharedPlan).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(sharedPlan) })
		t.Cleanup(func() { db.Get().Unscoped().Where(&Order{UserID: user.ID}).Delete(&Order{}) })

		w := post(t, user.ID, map[string]any{"plan": sharedPlan.PID, "region": "ap-northeast-1"})
		require.Equal(t, http.StatusOK, w.Code)

		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.NotEqual(t, float64(ErrorInvalidArgument), resp["code"],
			"shared plan must ignore region, never reject (got code=%v message=%v)", resp["code"], resp["message"])

		order := latestOrderForUser(t, user.ID)
		require.NotNil(t, order, "shared plan order must be created")
		assert.Equal(t, "", order.PrivateNodeRegion,
			"shared plan must not persist a region even when one is sent")
	})
}
