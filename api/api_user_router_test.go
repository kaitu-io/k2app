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
