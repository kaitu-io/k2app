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

func postOrder(t *testing.T, r *gin.Engine, userID uint64, body map[string]any) (code float64, raw string) {
	t.Helper()
	token := GenerateTestToken(userID, "", time.Hour)
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req, _ := http.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var env struct {
		Code float64 `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	return env.Code, w.Body.String()
}

func TestCreateOrder_RouterHardwareRequiresShipping(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	r := orderRegionTestRouter()
	plan := seedRouterPlan(t, "router-test-hw", "redmi-ax6s", 39900)
	user := CreateTestUser(t)

	// preview 不强制收货信息（页面先报价再填地址）
	code, _ := postOrder(t, r, user.ID, map[string]any{"preview": true, "plan": plan.PID, "region": "japan"})
	assert.EqualValues(t, 0, code)

	// 真实下单缺收货信息 → 422
	code, _ = postOrder(t, r, user.ID, map[string]any{"preview": false, "plan": plan.PID, "region": "japan"})
	assert.EqualValues(t, ErrorInvalidArgument, code)
	assert.Nil(t, latestOrderForUser(t, user.ID), "rejected order must not persist")

	// 带收货信息：订单行落库并带 shipping + region（WordGate 不可达无妨，行在 wordgate 之前建）
	postOrder(t, r, user.ID, map[string]any{"preview": false, "plan": plan.PID, "region": "japan",
		"shipping": map[string]any{"name": "张三", "phone": "13800000000", "address": "上海市…"}})
	o := latestOrderForUser(t, user.ID)
	require.NotNil(t, o)
	t.Cleanup(func() { db.Get().Unscoped().Delete(o) })
	assert.Equal(t, "japan", o.PrivateNodeRegion)
	s := o.GetRouterShipping()
	require.NotNil(t, s)
	assert.Equal(t, "张三", s.Name)
	assert.Equal(t, "13800000000", s.Phone)
}

func TestCreateOrder_RouterServiceIgnoresShippingAndSkipsTierGate(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	r := orderRegionTestRouter()
	plan := seedRouterPlan(t, "router-test-svc", "", 29900)
	// 老客户：已首单、tier=family。App 续费会被 TierMismatch 拦；路由器版与 tier 无关，必须放行。
	user := CreateTestUser(t)
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", user.ID).
		Updates(map[string]any{"is_first_order_done": true, "tier": TierFamily}).Error)

	postOrder(t, r, user.ID, map[string]any{"preview": false, "plan": plan.PID, "region": "japan"})
	o := latestOrderForUser(t, user.ID)
	require.NotNil(t, o, "service plan must not require shipping and must bypass tier gate")
	t.Cleanup(func() { db.Get().Unscoped().Delete(o) })
	assert.Nil(t, o.GetRouterShipping())
}

func TestCreateOrder_RouterBadRegionRejected(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	r := orderRegionTestRouter()
	plan := seedRouterPlan(t, "router-test-region", "", 29900)
	user := CreateTestUser(t)
	code, _ := postOrder(t, r, user.ID, map[string]any{"preview": true, "plan": plan.PID, "region": "mars"})
	assert.EqualValues(t, ErrorInvalidArgument, code)
}
