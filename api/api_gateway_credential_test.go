package center

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// gatewayCredentialTestUser creates a router-capable (TierFamily) user. Cleanup
// of the user and any minted gateway devices is registered on t.
func gatewayCredentialTestUser(t *testing.T) *User {
	t.Helper()
	user := &User{
		UUID: "gwcred-user-" + time.Now().Format("150405.000000000"),
		Tier: TierFamily, // arbitrary; tier no longer gates routers — an active private line does
	}
	require.NoError(t, db.Get().Create(user).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("user_id = ?", user.ID).Delete(&Device{})
		db.Get().Unscoped().Delete(user)
	})
	return user
}

// createActivePrivateNodeSub seeds an active PrivateNodeSubscription for userID.
func createActivePrivateNodeSub(t *testing.T, userID uint64) {
	t.Helper()
	now := time.Now().Unix()
	sub := &PrivateNodeSubscription{
		UserID:            userID,
		PlanID:            1,
		OrderID:           userID*1000 + uint64(now%1000) + 1,
		Region:            "jp",
		IPType:            IPTypeNonResidential,
		TrafficTotalBytes: 2 << 40,
		Status:            PNStatusActive,
		PurchasedAt:       now,
		ExpiresAt:         now + 86400,
	}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub) })
}

// gatewayCredentialContext builds a gin context with the given user injected as
// the authenticated principal (bypassing the JWT middleware chain).
func gatewayCredentialContext(t *testing.T, user *User) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/gateway-credential", nil)
	c.Set("authContext", &authContext{
		UserID: user.ID,
		User:   user,
	})
	return c, w
}

func TestGatewayCredentialActiveSubReturnsK2subsURL(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	user := gatewayCredentialTestUser(t)
	createActivePrivateNodeSub(t, user.ID)

	c, w := gatewayCredentialContext(t, user)
	api_gateway_credential(c)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	_, data := parseJobResponse(t, w.Body.Bytes())
	require.NotNil(t, data, "body=%s", w.Body.String())
	url, _ := data["url"].(string)
	if !strings.HasPrefix(url, "k2subs://") || !strings.Contains(url, "@") {
		t.Fatalf("bad url: %s", url)
	}
	if !strings.Contains(url, "/api/subs") {
		t.Fatalf("url missing /api/subs: %s", url)
	}
}

func TestGatewayCredentialNoSubRejected(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	user := gatewayCredentialTestUser(t)
	c, w := gatewayCredentialContext(t, user)
	api_gateway_credential(c)

	code, _ := parseJobResponse(t, w.Body.Bytes())
	require.Equal(t, float64(ErrorPlanNoRouter), code, "无线应被 ErrorPlanNoRouter 拒; body=%s", w.Body.String())
}

// 拆割裂后：basic 档（旧 MaxRouterDevice=0）但持有 active 专属线，必须放行。
func TestGatewayCredentialBasicTierWithLineAllowed(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	user := &User{
		UUID: "gwcred-basic-" + time.Now().Format("150405.000000000"),
		Tier: TierBasic,
	}
	require.NoError(t, db.Get().Create(user).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("user_id = ?", user.ID).Delete(&Device{})
		db.Get().Unscoped().Delete(user)
	})
	createActivePrivateNodeSub(t, user.ID)

	c, w := gatewayCredentialContext(t, user)
	api_gateway_credential(c)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	code, data := parseJobResponse(t, w.Body.Bytes())
	require.Equal(t, float64(0), code, "basic 档持线应放行; body=%s", w.Body.String())
	require.NotNil(t, data)
	url, _ := data["url"].(string)
	if !strings.HasPrefix(url, "k2subs://") {
		t.Fatalf("bad url: %s", url)
	}
	var got int64
	db.Get().Model(&Device{}).Where("user_id = ? AND is_gateway = true", user.ID).Count(&got)
	require.Equal(t, int64(1), got, "应铸造 1 台路由器设备")
}

// 纯 tier 不再授予路由器：family 档但无任何 active 专属线 → ErrorPlanNoRouter。
func TestGatewayCredentialFamilyTierNoLineRejected(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	user := gatewayCredentialTestUser(t) // TierFamily，但故意不建线
	c, w := gatewayCredentialContext(t, user)
	api_gateway_credential(c)

	code, _ := parseJobResponse(t, w.Body.Bytes())
	require.Equal(t, float64(ErrorPlanNoRouter), code, "family 无线应被拒; body=%s", w.Body.String())

	var got int64
	db.Get().Model(&Device{}).Where("user_id = ? AND is_gateway = true", user.ID).Count(&got)
	require.Equal(t, int64(0), got, "拒绝时不应建路由器设备")
}

func TestGatewayCredentialDoesNotKickAppDevice(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	user := gatewayCredentialTestUser(t)
	createActivePrivateNodeSub(t, user.ID)

	appDev := &Device{
		UDID:            "gwcred-app-" + time.Now().Format("150405.000000000"),
		UserID:          user.ID,
		IsGateway:       false,
		TokenIssueAt:    1,
		TokenLastUsedAt: 1,
	}
	require.NoError(t, db.Get().Create(appDev).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(appDev) })

	c, w := gatewayCredentialContext(t, user)
	api_gateway_credential(c)

	code, _ := parseJobResponse(t, w.Body.Bytes())
	require.Equal(t, float64(0), code, "mint failed; body=%s", w.Body.String())

	var got int64
	db.Get().Model(&Device{}).Where("udid = ?", appDev.UDID).Count(&got)
	if got != 1 {
		t.Fatalf("app device was kicked; want 1 got %d", got)
	}
}
