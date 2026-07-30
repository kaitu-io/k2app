package center

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// slaveAuthTestNode 建一个共享节点并注入 gin context（SlaveAuthRequired 的替身）。
func slaveAuthTestNode(t *testing.T, ipv4 string) *SlaveNode {
	t.Helper()
	db.Get().Unscoped().Where("ipv4 = ?", ipv4).Delete(&SlaveNode{}) // 自愈残留
	node := &SlaveNode{
		Ipv4: ipv4, SecretToken: "s-" + ipv4, Country: "JP", Region: "japan",
		Name: "tunnel-auth-" + ipv4, Class: NodeClassShared,
	}
	require.NoError(t, db.Get().Create(node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(node) })
	return node
}

func driveDeviceCheckAuth(t *testing.T, node *SlaveNode, udid, token string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/slave/device-check-auth", nil)
	c.Set("i_am_the_node", node)
	handleSlaveJWTAuth(c, udid, token, "")
	return w
}

// TestSlaveDeviceCheckAuth_TunnelToken：tunnel token 通过（过渡期新路径）。
func TestSlaveDeviceCheckAuth_TunnelToken(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)
	now := time.Now().Unix()

	user := CreateTestUser(t)
	user.ExpiredAt = now + 86400
	require.NoError(t, db.Get().Save(user).Error)
	device := CreateTestDevice(t, user.ID, "udid-slave-tt-"+time.Now().Format("150405.000000"))
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("tunnel_issue_at", now).Error)
	device.TunnelIssueAt = now
	node := slaveAuthTestNode(t, "10.99.20.1")

	t.Run("TunnelToken_200", func(t *testing.T) {
		w := driveDeviceCheckAuth(t, node, device.UDID, generateTestTunnelToken(t, device, time.Hour))
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "tunnel token 应通过: %s", resp.Message)
		data, err := ParseResponseData[SlaveDeviceCheckAuthResult](w)
		require.NoError(t, err)
		require.Equal(t, device.UDID, data.UDID)
		require.Equal(t, user.ExpiredAt, data.ServiceExpiredAt)
	})

	t.Run("AccessToken_Still200_Transition", func(t *testing.T) {
		token := GenerateTestToken(user.ID, device.UDID, time.Hour)
		require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
			Update("token_issue_at", tokenIssueAtOf(t, token)).Error)
		w := driveDeviceCheckAuth(t, node, device.UDID, token)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "过渡期 access token 必须仍然通过: %s", resp.Message)
	})

	t.Run("TunnelToken_AnchorBumped_401", func(t *testing.T) {
		tok := generateTestTunnelToken(t, device, time.Hour)
		require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
			Update("tunnel_issue_at", now+1).Error)
		t.Cleanup(func() {
			db.Get().Model(&Device{}).Where("id = ?", device.ID).Update("tunnel_issue_at", now)
		})
		w := driveDeviceCheckAuth(t, node, device.UDID, tok)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.EqualValues(t, ErrorNotLogin, ErrorCode(resp.Code), "锚点递增后应 401")
	})
}

// TestSlaveDeviceCheckAuth_BlockedUser403：封禁门（本 Phase 新增，之前这条
// 路径从未执行过封禁检查——只锚定 403，不锚定下游）。
func TestSlaveDeviceCheckAuth_BlockedUser403(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)
	now := time.Now().Unix()

	user := CreateTestUser(t)
	user.ExpiredAt = now + 86400
	blocked := true
	user.IsBlocked = &blocked
	require.NoError(t, db.Get().Save(user).Error)

	device := CreateTestDevice(t, user.ID, "udid-slave-blk-"+time.Now().Format("150405.000000"))
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("tunnel_issue_at", now).Error)
	device.TunnelIssueAt = now
	node := slaveAuthTestNode(t, "10.99.20.2")

	w := driveDeviceCheckAuth(t, node, device.UDID, generateTestTunnelToken(t, device, time.Hour))
	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.EqualValues(t, ErrorForbidden, ErrorCode(resp.Code), "封禁用户必须 403，got %d: %s", resp.Code, resp.Message)
}

// TestSlaveDeviceCheckAuthRequest_ModeFieldBinds：Mode 字段进请求体（Phase 1
// 的 mode 校验消费它；本 Phase 只绑定不校验）。
func TestSlaveDeviceCheckAuthRequest_ModeFieldBinds(t *testing.T) {
	var req SlaveDeviceCheckAuthRequest
	req.Mode = "gateway" // 编译层面锁定字段存在
	require.Equal(t, "gateway", req.Mode)
}
