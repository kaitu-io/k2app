package center

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestHandleSlaveJWTAuth_ModeGate 验证 spec §5.4 的设备类别交叉校验：
//   - App 设备（IsGateway=false）自称 mode=gateway → 403（危险方向，拦）
//   - mode=""（普通客户端与老客户端默认）→ 放行
//   - 路由器设备（IsGateway=true）自称 gateway → 放行
//
// 断言只锚定本门（403 与否）；正向用例额外确认全程走通（共享节点 + 有效会员）。
// 注意：本测试必须单跑与全量各跑一次（center 包共享 viper/redis 全局状态）。
func TestHandleSlaveJWTAuth_ModeGate(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	now := time.Now().Unix()

	// 自愈：清理上次中断运行残留的固定 IPv4 节点（idx_slave_nodes_ipv4 唯一索引）。
	db.Get().Unscoped().Where("ipv4 = ?", "10.99.0.30").Delete(&SlaveNode{})
	node := SlaveNode{
		Ipv4: "10.99.0.30", SecretToken: "secret-mode-gate",
		Country: "HK", Region: "hongkong", Name: "shared-mode-gate",
		Class: NodeClassShared,
	}
	require.NoError(t, db.Get().Create(&node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })

	newCtx := func() (*httptest.ResponseRecorder, *gin.Context) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/slave/device-check-auth", nil)
		c.Set("i_am_the_node", &node)
		return w, c
	}
	newMember := func(udid string) (*Device, string) {
		user := CreateTestUser(t)
		user.ExpiredAt = now + 86400
		require.NoError(t, db.Get().Save(user).Error)
		device := CreateTestDevice(t, user.ID, udid) // IsGateway=false by default
		token := GenerateTestToken(user.ID, device.UDID, time.Hour)
		return device, token
	}

	t.Run("AppDeviceClaimingGateway_403", func(t *testing.T) {
		device, token := newMember("udid-mode-app-claims-gw")
		w, c := newCtx()
		handleSlaveJWTAuth(c, device.UDID, token, "gateway")
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.EqualValues(t, ErrorForbidden, ErrorCode(resp.Code),
			"App 设备自称 gateway 必须 403（危险方向）: %s", resp.Message)
	})

	t.Run("EmptyMode_PassesGate", func(t *testing.T) {
		device, token := newMember("udid-mode-empty")
		w, c := newCtx()
		handleSlaveJWTAuth(c, device.UDID, token, "")
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.NotEqualValues(t, ErrorForbidden, ErrorCode(resp.Code), "mode=\"\" 不得触发本门")
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "有效会员+共享节点应全程走通: %s", resp.Message)
	})

	t.Run("GatewayDeviceClaimingGateway_PassesGate", func(t *testing.T) {
		device, token := newMember("udid-mode-real-gw")
		require.NoError(t, db.Get().Model(&Device{}).Where("udid = ?", device.UDID).
			Update("is_gateway", true).Error)
		w, c := newCtx()
		handleSlaveJWTAuth(c, device.UDID, token, "gateway")
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.NotEqualValues(t, ErrorForbidden, ErrorCode(resp.Code),
			"真路由器自称 gateway 不得被拦: %s", resp.Message)
	})
}
