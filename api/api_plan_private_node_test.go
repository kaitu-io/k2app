package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// planTestFixture seeds one private_node plan (with spec) and one app plan,
// registering cleanup. Returns nothing — callers query by the fixed PIDs below.
const (
	testPNPlanPID  = "test-pn-1m"
	testAppPlanPID = "test-app-1m"
)

func seedPlanFixtures(t *testing.T) {
	t.Helper()
	plan := Plan{PID: testPNPlanPID, Label: "专属节点测试", Price: 9900, Month: 1,
		Tier: "basic", Product: ProductPrivateNode, IsActive: BoolPtr(true), Highlight: BoolPtr(false)}
	require.NoError(t, db.Get().Create(&plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&plan) })

	spec := PrivateNodePlanSpec{PlanID: plan.ID, IPType: IPTypeNonResidential,
		AllowedRegions: `["us-east-1","ap-northeast-1"]`, TrafficTotalBytes: 2 << 40}
	require.NoError(t, db.Get().Create(&spec).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&spec) })

	appPlan := Plan{PID: testAppPlanPID, Label: "App 订阅测试", Price: 1900, Month: 1,
		Tier: "basic", Product: ProductApp, IsActive: BoolPtr(true), Highlight: BoolPtr(false)}
	require.NoError(t, db.Get().Create(&appPlan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&appPlan) })
}

type planListResp struct {
	Code int `json:"code"`
	Data struct {
		Items []map[string]any `json:"items"`
	} `json:"data"`
}

func doPlanRequest(t *testing.T, r *gin.Engine, path string) planListResp {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", path, nil)
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
	var resp planListResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func findPlan(items []map[string]any, pid string) map[string]any {
	for _, it := range items {
		if it["pid"] == pid {
			return it
		}
	}
	return nil
}

// TestGetPlans_ExcludesPrivateNode verifies the legacy/frozen GET /api/plans
// returns ONLY app plans — the seeded private_node plan must NOT leak to old
// product-unaware clients, while the seeded app plan IS present and carries
// product=app with no privateNode block.
func TestGetPlans_ExcludesPrivateNode(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/plans", api_get_plans)

	seedPlanFixtures(t)

	resp := doPlanRequest(t, r, "/api/plans")
	require.Equal(t, 0, resp.Code)

	require.Nil(t, findPlan(resp.Data.Items, testPNPlanPID),
		"private_node plan must NOT appear in legacy /api/plans")

	foundApp := findPlan(resp.Data.Items, testAppPlanPID)
	require.NotNil(t, foundApp, "seeded app plan must appear")
	require.Equal(t, "app", foundApp["product"])
	require.Nil(t, foundApp["privateNode"], "app plan must not emit a privateNode block")

	// Every returned item must be an app plan with no privateNode block.
	for _, it := range resp.Data.Items {
		require.Equal(t, "app", it["product"],
			"legacy /api/plans must only return app plans, got %v for %v", it["product"], it["pid"])
		require.Nil(t, it["privateNode"], "app plan %v must not emit a privateNode block", it["pid"])
	}
}

// TestGetProductPlans_PrivateNode verifies GET /api/products/private_node/plans
// returns the private_node plan WITH its full PrivateNodePlanSpec, and excludes app plans.
func TestGetProductPlans_PrivateNode(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/products/:product/plans", api_get_product_plans)

	seedPlanFixtures(t)

	resp := doPlanRequest(t, r, "/api/products/private_node/plans")
	require.Equal(t, 0, resp.Code)

	found := findPlan(resp.Data.Items, testPNPlanPID)
	require.NotNil(t, found, "seeded private node plan must appear")
	require.Equal(t, "private_node", found["product"])
	pn, ok := found["privateNode"].(map[string]any)
	require.True(t, ok, "privateNode spec must be present for private_node plans")
	require.Equal(t, "non_residential", pn["ipType"])
	regions, ok := pn["allowedRegions"].([]any)
	require.True(t, ok)
	require.Len(t, regions, 2)
	require.EqualValues(t, 2<<40, pn["trafficTotalBytes"])

	// No app plan should leak into the private_node product list.
	require.Nil(t, findPlan(resp.Data.Items, testAppPlanPID),
		"app plan must NOT appear in private_node product list")
	for _, it := range resp.Data.Items {
		require.Equal(t, "private_node", it["product"],
			"private_node product list must only return private_node plans, got %v for %v", it["product"], it["pid"])
	}
}

// TestGetProductPlans_App verifies GET /api/products/app/plans returns app plans
// and excludes private_node plans.
func TestGetProductPlans_App(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/products/:product/plans", api_get_product_plans)

	seedPlanFixtures(t)

	resp := doPlanRequest(t, r, "/api/products/app/plans")
	require.Equal(t, 0, resp.Code)

	foundApp := findPlan(resp.Data.Items, testAppPlanPID)
	require.NotNil(t, foundApp, "seeded app plan must appear")
	require.Equal(t, "app", foundApp["product"])

	require.Nil(t, findPlan(resp.Data.Items, testPNPlanPID),
		"private_node plan must NOT appear in app product list")
	for _, it := range resp.Data.Items {
		require.Equal(t, "app", it["product"],
			"app product list must only return app plans, got %v for %v", it["product"], it["pid"])
	}
}

// TestGetProductPlans_InvalidProduct verifies an unknown :product returns ErrorInvalidArgument.
func TestGetProductPlans_InvalidProduct(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/products/:product/plans", api_get_product_plans)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/products/bogus/plans", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, int(ErrorInvalidArgument), resp.Code)
}
