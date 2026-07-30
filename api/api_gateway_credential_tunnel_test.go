package center

import (
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestGatewayCredential_MintsTunnelToken：mint 出的 k2subs URL password 必须
// 是可通过 validateTunnelToken 的 tunnel token（不再是 24h access token）。
func TestGatewayCredential_MintsTunnelToken(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)
	now := time.Now().Unix()

	user := CreateTestUser(t)
	user.ExpiredAt = now + 86400
	require.NoError(t, db.Get().Save(user).Error)
	// 路由器准入 = 持有 active 专属线路（checkDeviceLimitOrKick isGateway 分支）。
	seedGatewayPrivateLine(t, user.ID) // api_subs_test.go 既有 helper

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/user/gateway-credential", nil)
	c.Set("authContext", &authContext{UserID: user.ID, User: user})

	api_gateway_credential(c)

	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "mint 应成功: %s", resp.Message)

	data, err := ParseResponseData[map[string]string](w)
	require.NoError(t, err)
	raw := (*data)["url"]
	require.NotEmpty(t, raw)

	u, err := url.Parse(raw)
	require.NoError(t, err)
	require.Equal(t, "k2subs", u.Scheme)
	require.NotNil(t, u.User)
	udid := u.User.Username()
	tok, _ := u.User.Password()
	require.NotEmpty(t, tok)

	// password 必须是 tunnel token 且真实可校验。
	var claims TokenClaims
	_, _, err = jwt.NewParser().ParseUnverified(tok, &claims)
	require.NoError(t, err)
	require.Equal(t, TokenTypeTunnel, claims.Type, "gateway credential 必须内嵌 tunnel token")

	_, dev, err := validateTunnelToken(nil, tok)
	require.NoError(t, err)
	require.Equal(t, udid, dev.UDID)
	require.True(t, dev.IsGateway)
	require.NotZero(t, dev.TunnelIssueAt, "Device 创建即带 TunnelIssueAt 锚点")

	t.Cleanup(func() {
		db.Get().Unscoped().Where("user_id = ? AND is_gateway = true", user.ID).Delete(&Device{})
	})
}
