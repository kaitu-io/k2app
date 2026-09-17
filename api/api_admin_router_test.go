package center

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 与 nodeOperationTestRouter 同法：裸 gin，不挂 admin 中间件，直接驱动 handler。
func adminRouterTestRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/app/router/fulfillments", api_admin_list_router_fulfillments)
	r.POST("/app/router/fulfillments/:id/stage", api_admin_update_router_stage)
	r.POST("/app/router/fulfillments/:id/credential", api_admin_mint_router_credential)
	r.GET("/app/private-node-subscriptions", api_admin_list_private_node_subscriptions)
	r.GET("/app/router-devices", api_admin_list_router_devices)
	r.GET("/app/router/stats", api_admin_router_stats)
	r.POST("/app/private-node-subscriptions/:id/extend", api_admin_extend_private_line)
	return r
}

func adminCall(t *testing.T, r *gin.Engine, method, path string, body any) (code float64, data map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	return parseJobResponse(t, w.Body.Bytes())
}

func TestAdminRouterFulfillments_ListAndShip(t *testing.T) {
	skipIfNoConfig(t)
	r := adminRouterTestRouter()
	user := CreateTestUser(t)
	_, f := seedReadyRouterFulfillment(t, user)
	require.NoError(t, db.Get().Model(f).Update("hardware_sku", "redmi-ax6s").Error)

	code, data := adminCall(t, r, http.MethodGet, "/app/router/fulfillments?stage=ready&pageSize=50", nil)
	require.EqualValues(t, 0, code)
	items, _ := data["items"].([]any)
	var row map[string]any
	for _, it := range items {
		m := it.(map[string]any)
		if uint64(m["id"].(float64)) == f.ID {
			row = m
		}
	}
	require.NotNil(t, row, "seeded fulfillment must be listed under stage=ready")
	assert.Equal(t, "redmi-ax6s", row["hardwareSku"])
	assert.NotNil(t, row["line"], "row must embed its line")

	// 缺快递单号不能发货
	code, _ = adminCall(t, r, http.MethodPost, "/app/router/fulfillments/"+strconv.FormatUint(f.ID, 10)+"/stage",
		AdminRouterStageRequest{Stage: RouterStageShipped})
	assert.EqualValues(t, ErrorInvalidArgument, code)

	code, _ = adminCall(t, r, http.MethodPost, "/app/router/fulfillments/"+strconv.FormatUint(f.ID, 10)+"/stage",
		AdminRouterStageRequest{Stage: RouterStageShipped, TrackingNo: "SF123", Carrier: "顺丰"})
	require.EqualValues(t, 0, code)
	var got RouterFulfillment
	require.NoError(t, db.Get().First(&got, f.ID).Error)
	assert.Equal(t, RouterStageShipped, got.Stage)
	assert.Equal(t, "SF123", got.TrackingNo)
	assert.NotZero(t, got.ShippedAt)

	// 已发货不能再次发货
	code, _ = adminCall(t, r, http.MethodPost, "/app/router/fulfillments/"+strconv.FormatUint(f.ID, 10)+"/stage",
		AdminRouterStageRequest{Stage: RouterStageShipped, TrackingNo: "SF456"})
	assert.EqualValues(t, ErrorInvalidOperation, code)
}

func TestAdminRouterFulfillments_MintCredential(t *testing.T) {
	skipIfNoConfig(t)
	r := adminRouterTestRouter()
	user := CreateTestUser(t)
	_, f := seedReadyRouterFulfillment(t, user)

	code, data := adminCall(t, r, http.MethodPost, "/app/router/fulfillments/"+strconv.FormatUint(f.ID, 10)+"/credential", nil)
	require.EqualValues(t, 0, code)
	url, _ := data["url"].(string)
	assert.Contains(t, url, "k2subs://router-")
	var got RouterFulfillment
	require.NoError(t, db.Get().First(&got, f.ID).Error)
	require.NotNil(t, got.GatewayDeviceID)

	// 线路未就绪（paid）不能铸造
	require.NoError(t, db.Get().Model(&got).Update("stage", RouterStagePaid).Error)
	code, _ = adminCall(t, r, http.MethodPost, "/app/router/fulfillments/"+strconv.FormatUint(f.ID, 10)+"/credential", nil)
	assert.EqualValues(t, ErrorTooEarly, code)
}

func TestAdminRouterFulfillments_MintCredential_RejectsNonNewest(t *testing.T) {
	skipIfNoConfig(t)
	r := adminRouterTestRouter()
	user := CreateTestUser(t)
	_, older := seedReadyRouterFulfillment(t, user)

	// 手工建第二条台账（不能复用 seedReadyRouterFulfillment：它对同一用户固定用
	// 800000+userID 做 OrderID/SubID，两条会撞唯一索引）——同一用户名下更新的一条 ready 台账。
	now := time.Now().Unix()
	sub2 := &PrivateNodeSubscription{UserID: user.ID, PlanID: 1, OrderID: 900000 + user.ID, Region: "japan",
		IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40, Status: PNStatusActive,
		PurchasedAt: now, ExpiresAt: now + 300*86400}
	require.NoError(t, db.Get().Create(sub2).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub2) })
	newer := &RouterFulfillment{OrderID: sub2.OrderID, UserID: user.ID, SubID: sub2.ID, Stage: RouterStageReady}
	require.NoError(t, db.Get().Create(newer).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(newer) })

	// 旧的那条（id 更小）不是当前可关联凭证的最新台账 → 拒绝
	code, _ := adminCall(t, r, http.MethodPost, "/app/router/fulfillments/"+strconv.FormatUint(older.ID, 10)+"/credential", nil)
	assert.EqualValues(t, ErrorInvalidOperation, code)

	// 最新那条（id 更大）仍可正常铸造
	code, data := adminCall(t, r, http.MethodPost, "/app/router/fulfillments/"+strconv.FormatUint(newer.ID, 10)+"/credential", nil)
	require.EqualValues(t, 0, code)
	url, _ := data["url"].(string)
	assert.Contains(t, url, "k2subs://router-")
}

func TestAdminRouterFulfillments_ByoCannotShip(t *testing.T) {
	skipIfNoConfig(t)
	r := adminRouterTestRouter()
	user := CreateTestUser(t)
	_, f := seedReadyRouterFulfillment(t, user)
	// seedReadyRouterFulfillment 默认 HardwareSKU=="" (自备路由器)，本测试不改 SKU。

	code, _ := adminCall(t, r, http.MethodPost, "/app/router/fulfillments/"+strconv.FormatUint(f.ID, 10)+"/stage",
		AdminRouterStageRequest{Stage: RouterStageShipped, TrackingNo: "SF999"})
	assert.EqualValues(t, ErrorInvalidOperation, code)

	var got RouterFulfillment
	require.NoError(t, db.Get().First(&got, f.ID).Error)
	assert.Equal(t, RouterStageReady, got.Stage, "自备台账阶段不应被 shipped 请求改动")
}

func TestAdminPrivateNodeSubscriptions_List(t *testing.T) {
	skipIfNoConfig(t)
	r := adminRouterTestRouter()
	user := CreateTestUser(t)
	sub, _ := seedReadyRouterFulfillment(t, user)
	code, data := adminCall(t, r, http.MethodGet, "/app/private-node-subscriptions?userId="+strconv.FormatUint(user.ID, 10), nil)
	require.EqualValues(t, 0, code)
	items, _ := data["items"].([]any)
	require.Len(t, items, 1)
	row := items[0].(map[string]any)
	assert.EqualValues(t, sub.ID, row["id"])
	assert.EqualValues(t, user.ID, row["userId"])
}

func TestAdminRouterDevices_List(t *testing.T) {
	skipIfNoConfig(t)
	r := adminRouterTestRouter()
	user := CreateTestUser(t)
	dev := &Device{UDID: newRouterUDID(), UserID: user.ID, IsGateway: true, AppPlatform: "router",
		AppVersion: "0.4.10", TokenIssueAt: 1, TokenLastUsedAt: 1, TunnelIssueAt: 1}
	require.NoError(t, db.Get().Create(dev).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(dev) })
	code, data := adminCall(t, r, http.MethodGet, "/app/router-devices?pageSize=100", nil)
	require.EqualValues(t, 0, code)
	items, _ := data["items"].([]any)
	found := false
	for _, it := range items {
		if it.(map[string]any)["udid"] == dev.UDID {
			found = true
		}
	}
	assert.True(t, found)
}

// 端到端：成品路由器在仓库铸凭证 + 跑 k2r setup（/api/subs 记下活动）→ 运营标发货 → 台账必须仍是
// shipped（此前的活动是仓库测试，不是客户上线）→ 发货之后再有活动才 online。
func TestRouterFulfillment_HardwareWarehouseActivityNotOnlineAfterShip(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	r := adminRouterTestRouter()
	user := CreateTestUser(t)
	_, f := seedReadyRouterFulfillment(t, user)
	require.NoError(t, db.Get().Model(f).Update("hardware_sku", "redmi-ax6s").Error)

	_, devID, err := mintGatewayCredential(ctx, user)
	require.NoError(t, err)
	// 铸造把 CredentialMintedAt 与设备 TokenLastUsedAt 都写成"现在"；整体回拨到过去，给后续
	// touch / 发货留出严格递增的时间线（发货端点把 shipped_at 写成真实的 now）。
	now := time.Now().Unix()
	mintAt := now - 1000
	require.NoError(t, db.Get().Model(&RouterFulfillment{}).Where("id = ?", f.ID).Update("credential_minted_at", mintAt).Error)
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", devID).Update("token_last_used_at", mintAt).Error)
	var dev Device
	require.NoError(t, db.Get().First(&dev, devID).Error)

	// 仓库里跑 k2r setup：活动晚于铸造
	touchGatewayDeviceSeen(ctx, &dev, now-500)

	reload := func() RouterFulfillment {
		var got RouterFulfillment
		require.NoError(t, db.Get().First(&got, f.ID).Error)
		require.NoError(t, syncRouterFulfillment(ctx, db.Get(), &got, time.Now().Unix()))
		var persisted RouterFulfillment
		require.NoError(t, db.Get().First(&persisted, f.ID).Error)
		return persisted
	}
	assert.Equal(t, RouterStageReady, reload().Stage, "hardware must not go online before shipping")

	code, _ := adminCall(t, r, http.MethodPost, "/app/router/fulfillments/"+strconv.FormatUint(f.ID, 10)+"/stage",
		AdminRouterStageRequest{Stage: RouterStageShipped, TrackingNo: "SF-E2E", Carrier: "顺丰"})
	require.EqualValues(t, 0, code)

	got := reload()
	require.Greater(t, got.ShippedAt, now-500)
	assert.Equal(t, RouterStageShipped, got.Stage, "warehouse activity before ShippedAt must not flip the ledger to online")

	// 客户收货后路由器拉订阅：活动晚于发货时刻 → online
	touchGatewayDeviceSeen(ctx, &dev, got.ShippedAt+10)
	got = reload()
	assert.Equal(t, RouterStageOnline, got.Stage)
	assert.Equal(t, got.ShippedAt+10, got.ActivatedAt)
}

// 已过期台账代铸：返回 ErrorInvalidOperation（线路过期），与"尚未就绪"的 ErrorTooEarly 区分。
func TestAdminRouterFulfillments_MintCredential_ExpiredIsInvalidOperation(t *testing.T) {
	skipIfNoConfig(t)
	r := adminRouterTestRouter()
	user := CreateTestUser(t)
	_, f := seedReadyRouterFulfillment(t, user)
	require.NoError(t, db.Get().Model(f).Update("stage", RouterStageExpired).Error)

	code, _ := adminCall(t, r, http.MethodPost, "/app/router/fulfillments/"+strconv.FormatUint(f.ID, 10)+"/credential", nil)
	assert.EqualValues(t, ErrorInvalidOperation, code)
}

func TestAdminRouterStats_CountsAndStuck(t *testing.T) {
	skipIfNoConfig(t)
	r := adminRouterTestRouter()

	code, before := adminCall(t, r, http.MethodGet, "/app/router/stats", nil)
	require.EqualValues(t, 0, code)
	counts := before["stageCounts"].(map[string]any)
	for _, s := range []string{RouterStagePaid, RouterStageProvisioning, RouterStageReady, RouterStageShipped, RouterStageOnline, RouterStageExpired} {
		_, ok := counts[s]
		assert.True(t, ok, "stageCounts must contain %s", s)
	}
	readyBefore := counts[RouterStageReady].(float64)
	stuckBefore := before["stuck"].(float64)

	user := CreateTestUser(t)
	_, f := seedReadyRouterFulfillment(t, user)

	_, after := adminCall(t, r, http.MethodGet, "/app/router/stats", nil)
	assert.Equal(t, readyBefore+1, after["stageCounts"].(map[string]any)[RouterStageReady].(float64))
	assert.Equal(t, stuckBefore, after["stuck"].(float64), "fresh row is not stuck")

	// 把 updated_at 拨回 49 小时前（UpdateColumn 不触发 autoUpdateTime）
	require.NoError(t, db.Get().Model(f).UpdateColumn("updated_at", time.Now().Unix()-49*3600).Error)
	_, stuck := adminCall(t, r, http.MethodGet, "/app/router/stats", nil)
	assert.Equal(t, stuckBefore+1, stuck["stuck"].(float64))
}

func TestAdminExtendPrivateLine(t *testing.T) {
	skipIfNoConfig(t)
	r := adminRouterTestRouter()
	user := CreateTestUser(t)
	sub, _ := seedReadyRouterFulfillment(t, user)
	oldExpiry := sub.ExpiresAt

	// 参数校验
	code, _ := adminCall(t, r, http.MethodPost, "/app/private-node-subscriptions/"+strconv.FormatUint(sub.ID, 10)+"/extend",
		AdminExtendLineRequest{Months: 0, Reason: "x"})
	assert.EqualValues(t, ErrorInvalidArgument, code)
	code, _ = adminCall(t, r, http.MethodPost, "/app/private-node-subscriptions/"+strconv.FormatUint(sub.ID, 10)+"/extend",
		AdminExtendLineRequest{Months: 1})
	assert.EqualValues(t, ErrorInvalidArgument, code)

	// 正常延期 1 个月
	code, data := adminCall(t, r, http.MethodPost, "/app/private-node-subscriptions/"+strconv.FormatUint(sub.ID, 10)+"/extend",
		AdminExtendLineRequest{Months: 1, Reason: "工单 #1 补偿"})
	require.EqualValues(t, 0, code)
	var got PrivateNodeSubscription
	require.NoError(t, db.Get().First(&got, sub.ID).Error)
	assert.Greater(t, got.ExpiresAt, oldExpiry)
	assert.EqualValues(t, got.ExpiresAt, data["expiresAt"].(float64))

	// grace 线路延期后回到 active
	require.NoError(t, db.Get().Model(&got).Updates(map[string]any{"status": PNStatusGrace, "expires_at": time.Now().Unix() - 86400}).Error)
	code, _ = adminCall(t, r, http.MethodPost, "/app/private-node-subscriptions/"+strconv.FormatUint(sub.ID, 10)+"/extend",
		AdminExtendLineRequest{Months: 1, Reason: "x"})
	require.EqualValues(t, 0, code)
	require.NoError(t, db.Get().First(&got, sub.ID).Error)
	assert.Equal(t, PNStatusActive, got.Status)

	// deprovisioned 不能延期
	require.NoError(t, db.Get().Model(&got).Update("status", PNStatusDeprovisioned).Error)
	code, _ = adminCall(t, r, http.MethodPost, "/app/private-node-subscriptions/"+strconv.FormatUint(sub.ID, 10)+"/extend",
		AdminExtendLineRequest{Months: 1, Reason: "x"})
	assert.EqualValues(t, ErrorInvalidOperation, code)

	// 不存在
	code, _ = adminCall(t, r, http.MethodPost, "/app/private-node-subscriptions/999999999/extend",
		AdminExtendLineRequest{Months: 1, Reason: "x"})
	assert.EqualValues(t, ErrorNotFound, code)
}
