package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func planRouterTestRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BrandResolver())
	r.GET("/api/plans", api_get_plans)
	r.GET("/api/products/:product/plans", api_get_product_plans)
	return r
}

func seedRouterPlan(t *testing.T, pid, sku string, price uint64) *Plan {
	t.Helper()
	plan := &Plan{PID: pid, Label: "开途路由器版测试", Price: price, OriginPrice: price, Month: 12,
		Tier: TierBasic, Product: ProductRouter, HardwareSKU: sku,
		IsActive: BoolPtr(true), Highlight: BoolPtr(false), Brand: "kaitu"}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(plan) })
	spec := &PrivateNodePlanSpec{PlanID: plan.ID, IPType: IPTypeNonResidential,
		AllowedRegions: `["japan","singapore"]`, TrafficTotalBytes: 2 << 40}
	require.NoError(t, db.Get().Create(spec).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(spec) })
	return plan
}

func getPlansJSON(t *testing.T, r *gin.Engine, path string) (code float64, items []map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var env struct {
		Code float64 `json:"code"`
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	return env.Code, env.Data.Items
}

func TestProductPlans_Router(t *testing.T) {
	skipIfNoConfig(t)
	r := planRouterTestRouter()
	plan := seedRouterPlan(t, "router-test-std", "redmi-ax6s", 39900)

	code, items := getPlansJSON(t, r, "/api/products/router/plans")
	require.EqualValues(t, 0, code)
	var found map[string]any
	for _, it := range items {
		if it["pid"] == plan.PID {
			found = it
		}
	}
	require.NotNil(t, found, "router plan must be listed under /api/products/router/plans")
	assert.Equal(t, "router", found["product"])
	assert.Equal(t, "redmi-ax6s", found["hardwareSku"])
	pn, _ := found["privateNode"].(map[string]any)
	require.NotNil(t, pn, "router plan must carry its internal line spec (regions)")
	assert.ElementsMatch(t, []any{"japan", "singapore"}, pn["allowedRegions"])

	// 老端点冻结为 app：路由器版套餐绝不泄漏进去
	_, legacy := getPlansJSON(t, r, "/api/plans")
	for _, it := range legacy {
		assert.NotEqual(t, plan.PID, it["pid"], "/api/plans must stay app-only")
	}
}

func TestProductPlans_UnknownProductRejected(t *testing.T) {
	skipIfNoConfig(t)
	r := planRouterTestRouter()
	code, _ := getPlansJSON(t, r, "/api/products/bogus/plans")
	assert.EqualValues(t, ErrorInvalidArgument, code)
}

func TestIsLineProduct(t *testing.T) {
	assert.True(t, isLineProduct(ProductPrivateNode))
	assert.True(t, isLineProduct(ProductRouter))
	assert.False(t, isLineProduct(ProductApp))
	assert.False(t, isLineProduct(""))
}
