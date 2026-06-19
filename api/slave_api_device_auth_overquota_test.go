package center

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestSlaveJWTAuth_PrivateNodeUsage 验证 device-check-auth 在专属节点不同流量使用率下的行为。
// Center 侧 95% 新连接闸门已移除：节点侧 sidecar enforcer 是流量掐断的单一权威。
// 用户在订阅有效期内，无论 CloudInstance 流量使用率如何，device-check-auth 均应放行。
func TestSlaveJWTAuth_PrivateNodeUsage(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()

	// 自愈：清理上次中断运行残留的固定 IPv4 节点（idx_slave_nodes_ipv4 唯一）。
	for _, ip := range []string{"10.99.0.20", "10.99.0.21", "10.99.0.22", "10.99.0.23", "10.99.0.24"} {
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
	}
	// 自愈：残留 CloudInstance（idx_provider_instance 唯一）。
	for _, iid := range []string{
		"i-overquota-ci-95", "i-overquota-ci-50",
		"i-overquota-ci-950", "i-overquota-ci-949", "i-overquota-ci-zerototal",
	} {
		db.Get().Unscoped().Where("instance_id = ?", iid).Delete(&CloudInstance{})
	}

	// buildPrivateNode 装一套 owner + private SlaveNode + active PrivateNodeSubscription
	// + CloudInstance（指定 used/total），返回 owner/device/token/node 供 handler 调用。
	buildPrivateNode := func(t *testing.T, ip, secret, instanceID string, used, total int64) (device *Device, token string, node *SlaveNode) {
		t.Helper()
		owner := CreateTestUser(t)
		owner.ExpiredAt = now + 86400 // 会员有效
		require.NoError(t, db.Get().Save(owner).Error)

		device = CreateTestDevice(t, owner.ID, "udid-"+instanceID)
		token = GenerateTestToken(owner.ID, device.UDID, time.Hour)

		ci := CloudInstance{
			Provider:          "ssh_standalone",
			AccountName:       "test-overquota",
			InstanceID:        instanceID,
			Name:              "private-" + instanceID,
			IPAddress:         ip,
			Region:            "hongkong",
			TrafficUsedBytes:  used,
			TrafficTotalBytes: total,
			TrafficResetAt:    now + 30*86400,
		}
		require.NoError(t, db.Get().Create(&ci).Error)

		n := SlaveNode{
			Ipv4:               ip,
			SecretToken:        secret,
			Country:            "HK",
			Region:             "hongkong",
			Name:               "private-hk-" + instanceID,
			Class:              NodeClassPrivate,
			PrivateOwnerUserID: &owner.ID,
		}
		require.NoError(t, db.Get().Create(&n).Error)

		sub := PrivateNodeSubscription{
			UserID: owner.ID, OrderID: ci.ID, Status: PNStatusActive, Region: "hongkong",
			IPType: IPTypeNonResidential, SlaveNodeID: &n.ID, CloudInstanceID: &ci.ID,
			PurchasedAt: now, ExpiresAt: now + 86400,
		}
		require.NoError(t, db.Get().Create(&sub).Error)
		// 列定向更新：SlaveNode.Meta 是 type:json 列，全量 Save 会写 meta='' 触发
		// MariaDB json_valid CHECK 失败。只更新 private_sub_id。
		require.NoError(t, db.Get().Model(&n).Update("private_sub_id", sub.ID).Error)
		n.PrivateSubID = &sub.ID

		t.Cleanup(func() {
			db.Get().Unscoped().Delete(&sub)
			db.Get().Unscoped().Delete(&n)
			db.Get().Unscoped().Delete(&ci)
		})
		return device, token, &n
	}

	// 以下所有场景：Center 侧无配额闸门，订阅有效 → 均应放行（ErrorNone）。
	// 节点侧 sidecar enforcer 负责在 100% 时掐断流量（单一权威）。

	t.Run("HighUsage96pct_Allowed", func(t *testing.T) {
		// used=960, total=1000 → 96%；Center 侧不再拒绝 → 放行。
		device, token, node := buildPrivateNode(t, "10.99.0.20", "secret-oq-95", "i-overquota-ci-95", 960, 1000)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/slave/device-check-auth", nil)
		c.Set("i_am_the_node", node)

		handleSlaveJWTAuth(c, device.UDID, token)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, ErrorNone, ErrorCode(resp.Code), "96%% 使用率应放行（Center 侧闸门已移除）: %s", resp.Message)
	})

	t.Run("UnderQuota50_Allowed", func(t *testing.T) {
		// used=500, total=1000 → 50% → 放行。
		device, token, node := buildPrivateNode(t, "10.99.0.21", "secret-oq-50", "i-overquota-ci-50", 500, 1000)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/slave/device-check-auth", nil)
		c.Set("i_am_the_node", node)

		handleSlaveJWTAuth(c, device.UDID, token)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, ErrorNone, ErrorCode(resp.Code), "50%% 使用率应放行: %s", resp.Message)
	})

	t.Run("ExactBoundary95pct_Allowed", func(t *testing.T) {
		// used=950, total=1000 → 95%；Center 侧不再拒绝 → 放行。
		device, token, node := buildPrivateNode(t, "10.99.0.22", "secret-oq-950", "i-overquota-ci-950", 950, 1000)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/slave/device-check-auth", nil)
		c.Set("i_am_the_node", node)

		handleSlaveJWTAuth(c, device.UDID, token)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, ErrorNone, ErrorCode(resp.Code), "950/1000 == 95%% 应放行（Center 侧闸门已移除）: %s", resp.Message)
	})

	t.Run("ExactBoundary949_Allowed", func(t *testing.T) {
		// used=949, total=1000 → < 95% → 放行。
		device, token, node := buildPrivateNode(t, "10.99.0.23", "secret-oq-949", "i-overquota-ci-949", 949, 1000)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/slave/device-check-auth", nil)
		c.Set("i_am_the_node", node)

		handleSlaveJWTAuth(c, device.UDID, token)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, ErrorNone, ErrorCode(resp.Code), "949/1000 < 95%% 应放行: %s", resp.Message)
	})

	t.Run("ZeroTotalNeverThrottles_Allowed", func(t *testing.T) {
		// TrafficTotalBytes=0（未配额）即便高用量也永不限流 → 放行。
		device, token, node := buildPrivateNode(t, "10.99.0.24", "secret-oq-zero", "i-overquota-ci-zerototal", 999999, 0)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/slave/device-check-auth", nil)
		c.Set("i_am_the_node", node)

		handleSlaveJWTAuth(c, device.UDID, token)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, ErrorNone, ErrorCode(resp.Code), "未配额（total=0）应放行: %s", resp.Message)
	})
}
