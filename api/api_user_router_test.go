package center

import (
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

func userRouterTestRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/api/user/router", AuthRequired(), EnforceDeviceClass(), api_get_user_router)
	return r
}

func getUserRouter(t *testing.T, r *gin.Engine, userID uint64) DataUserRouter {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, "/api/user/router", nil)
	req.Header.Set("Authorization", "Bearer "+GenerateTestToken(userID, "", time.Hour))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var env struct {
		Code float64        `json:"code"`
		Data DataUserRouter `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	require.EqualValues(t, 0, env.Code)
	return env.Data
}

func TestGetUserRouter_NoOrder(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	got := getUserRouter(t, userRouterTestRouter(), user.ID)
	assert.False(t, got.HasRouter)
	assert.Nil(t, got.Fulfillment)
}

func TestGetUserRouter_ReadyLineAndOnlineDevice(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	sub, f := seedReadyRouterFulfillment(t, user) // 来自 Task 6 测试文件
	plan := seedRouterPlan(t, "router-user-renew", "", 29900)
	now := time.Now().Unix()
	// 台账仍是 ready（自备），铸造凭证后设备 10 分钟前拉过订阅 → 读路径应把阶段同步到 online
	dev := &Device{UDID: newRouterUDID(), UserID: user.ID, IsGateway: true, AppPlatform: "router",
		AppVersion: "0.4.10", AppArch: "arm64", TokenIssueAt: now - 3600, TokenLastUsedAt: now - 600, TunnelIssueAt: now - 3600}
	require.NoError(t, db.Get().Create(dev).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(dev) })
	require.NoError(t, db.Get().Model(f).Updates(map[string]any{"gateway_device_id": dev.ID, "credential_minted_at": now - 3600}).Error)

	got := getUserRouter(t, userRouterTestRouter(), user.ID)
	assert.True(t, got.HasRouter)
	require.NotNil(t, got.Fulfillment)
	assert.Equal(t, RouterStageOnline, got.Fulfillment.Stage)
	assert.True(t, got.Fulfillment.CredentialMinted)
	assert.True(t, got.Fulfillment.CanMintCredential)
	require.NotNil(t, got.Line)
	assert.Equal(t, sub.ID, got.Line.ID)
	assert.True(t, got.Line.IsServiceable)
	require.NotNil(t, got.Device)
	assert.True(t, got.Device.Online)
	assert.Equal(t, "0.4.10", got.Device.AppVersion)
	// dev 库可能已存在真实的 router-svc-1y（更早的 id），只断言"有可续费套餐"而不锚定本测试种的那条
	assert.NotEmpty(t, got.RenewPlanPID)
	_ = plan
}

func TestGetUserRouter_OwnerIsolation(t *testing.T) {
	skipIfNoConfig(t)
	owner := CreateTestUser(t)
	other := CreateTestUser(t)
	seedReadyRouterFulfillment(t, owner)
	got := getUserRouter(t, userRouterTestRouter(), other.ID)
	assert.False(t, got.HasRouter)
}

// Device 必须严格跟随台账的 GatewayDeviceID，不按 user_id+is_gateway 独立查——
// 否则台账与设备各取各的，读路径可能报告一台与台账对不上的设备。
func TestGetUserRouter_DeviceFollowsFulfillment(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	_, f := seedReadyRouterFulfillment(t, user)
	now := time.Now().Unix()

	dev := &Device{UDID: newRouterUDID(), UserID: user.ID, IsGateway: true, AppPlatform: "router",
		TokenIssueAt: now - 7200, TokenLastUsedAt: now - 7200, TunnelIssueAt: now - 7200}
	require.NoError(t, db.Get().Create(dev).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(dev) })
	require.NoError(t, db.Get().Model(f).Updates(map[string]any{"gateway_device_id": dev.ID, "credential_minted_at": now - 7300}).Error)

	r := userRouterTestRouter()
	got := getUserRouter(t, r, user.ID)
	require.NotNil(t, got.Device)
	assert.False(t, got.Device.Online)
	assert.EqualValues(t, now-7200, got.Device.LastSeenAt)

	// 台账指向一个不存在的设备 ID（轮换后旧设备已删）→ device 必须为 nil，不能兜底猜别的设备。
	require.NoError(t, db.Get().Model(f).Update("gateway_device_id", uint64(999999999)).Error)
	got2 := getUserRouter(t, r, user.ID)
	assert.Nil(t, got2.Device)
}

// CanMintCredential 必须镜像 POST /api/user/gateway-credential 的真实准入门
// （HasActivePrivateLines），不能只看台账 stage——两者可能分叉。
func TestGetUserRouter_CanMintMirrorsGate(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	sub, _ := seedReadyRouterFulfillment(t, user)
	r := userRouterTestRouter()

	require.NoError(t, db.Get().Model(sub).Update("status", PNStatusDeprovisioned).Error)
	got := getUserRouter(t, r, user.ID)
	require.NotNil(t, got.Fulfillment)
	assert.False(t, got.Fulfillment.CanMintCredential)

	require.NoError(t, db.Get().Model(sub).Update("status", PNStatusActive).Error)
	got2 := getUserRouter(t, r, user.ID)
	require.NotNil(t, got2.Fulfillment)
	assert.True(t, got2.Fulfillment.CanMintCredential)
}
