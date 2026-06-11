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

// TestGetPlans_IncludesKindAndPrivateNodeSpec verifies that GET /api/plans emits
// the plan `kind` and, for private_node plans, the purchase-visible
// PrivateNodePlanSpec (provider/ipType/allowedRegions/trafficTotalBytes).
func TestGetPlans_IncludesKindAndPrivateNodeSpec(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/plans", api_get_plans)

	// seed a private_node plan + spec
	plan := Plan{PID: "test-pn-1m", Label: "专属节点测试", Price: 9900, Month: 1,
		Tier: "basic", Kind: PlanKindPrivateNode, IsActive: BoolPtr(true), Highlight: BoolPtr(false)}
	require.NoError(t, db.Get().Create(&plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&plan) })

	spec := PrivateNodePlanSpec{PlanID: plan.ID, Provider: "aws_lightsail", IPType: IPTypeNonResidential,
		AllowedRegions: `["us-east-1","ap-northeast-1"]`, ImageID: "img-x", BundleID: "nano_2_0", TrafficTotalBytes: 2 << 40}
	require.NoError(t, db.Get().Create(&spec).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&spec) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/plans", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)

	var found map[string]any
	for _, it := range resp.Data.Items {
		if it["pid"] == "test-pn-1m" {
			found = it
		}
	}
	require.NotNil(t, found, "seeded private node plan must appear")
	require.Equal(t, "private_node", found["kind"])
	pn, ok := found["privateNode"].(map[string]any)
	require.True(t, ok, "privateNode spec must be present for private_node plans")
	require.Equal(t, "non_residential", pn["ipType"])
	regions, ok := pn["allowedRegions"].([]any)
	require.True(t, ok)
	require.Len(t, regions, 2)
}
