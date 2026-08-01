package center

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestHandleJWTAuth_RejectsTunnelToken：tunnel token 不得通过 handleJWTAuth。
// 关键在于构造出漏洞前置——把 Device.TokenIssueAt 设成与 tunnel token 的 issueAt
// 相等，这样没有 type 门时 device 分支的 TokenIssueAt 比对会放行（网关路径的
// 必然形态）。有门时必须在比对之前就 return nil。锚定 handleJWTAuth 这道门本身
// （返回 nil），AuthRequired 把 nil 翻成 401 是下游，不在断言范围内。
func TestHandleJWTAuth_RejectsTunnelToken(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	user := CreateTestUser(t)
	device := CreateTestDevice(t, user.ID, "udid-jwttype-"+time.Now().Format("150405.000000"))
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("token_issue_at", now).Error)

	// issueAt = device.TokenIssueAt → 无门时 device 分支比对通过（复现漏洞）。
	tunnelTok, err := generateTunnelToken(nil, user.ID, device.UDID, user.Roles, now)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/user/info", nil)

	got := handleJWTAuth(c, tunnelTok)
	require.Nil(t, got, "tunnel token 不得通过 handleJWTAuth（默认拒绝非 access type）")

	// 正向对照：同设备的 access token 仍必须通过（门不误伤存量）。
	access := GenerateTestToken(user.ID, device.UDID, time.Hour)
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("token_issue_at", tokenIssueAtOf(t, access)).Error)
	ok := handleJWTAuth(c, access)
	require.NotNil(t, ok, "access token 必须仍然通过")
}
