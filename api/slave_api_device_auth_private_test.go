package center

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestHandleSlaveJWTAuth_PrivateNode 直接驱动 handler（带 gin 测试 context +
// 真 token/device），验证专属节点的授权接线与错误映射：
//   - 主人 + active 订阅 → 200，serviceExpiredAt = 订阅时钟（非 user.ExpiredAt）
//   - 陌生人（非主人）→ 402（AuthorizeNodeAccess 拒绝）
//   - 专属节点 PrivateSubID 为 nil → 500（数据完整性问题，不伪装成会员过期）
//
// 共享节点路径的 402-on-expiry 由 entitlement_authorize_test.go +
// AuthorizeNodeAccess 的 shared 分支 (ExpiredAt > now) 覆盖。
func TestHandleSlaveJWTAuth_PrivateNode(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()

	t.Run("OwnerWithActiveSub_200_PrivateClock", func(t *testing.T) {
		owner := CreateTestUser(t)
		// 故意让 user.ExpiredAt 与订阅 ExpiresAt 不同，以分辨返回的是哪条时钟。
		owner.ExpiredAt = now + 10
		require.NoError(t, db.Get().Save(owner).Error)

		device := CreateTestDevice(t, owner.ID, "udid-owner-private")
		token := GenerateTestToken(owner.ID, device.UDID, time.Hour)

		node := SlaveNode{
			Ipv4:               "10.99.0.10",
			SecretToken:        "secret-private-owner",
			Country:            "HK",
			Region:             "hongkong",
			Name:               "private-hk-owner",
			Class:              NodeClassPrivate,
			PrivateOwnerUserID: &owner.ID,
		}
		require.NoError(t, db.Get().Create(&node).Error)

		subExpires := now + 86400
		sub := PrivateNodeSubscription{
			UserID: owner.ID, Status: PNStatusActive, Region: "hongkong",
			IPType: IPTypeNonResidential, SlaveNodeID: &node.ID,
			PurchasedAt: now, ExpiresAt: subExpires,
		}
		require.NoError(t, db.Get().Create(&sub).Error)
		node.PrivateSubID = &sub.ID
		require.NoError(t, db.Get().Save(&node).Error)

		t.Cleanup(func() {
			db.Get().Unscoped().Delete(&sub)
			db.Get().Unscoped().Delete(&node)
		})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/slave/device-check-auth", nil)
		c.Set("i_am_the_node", &node)

		handleSlaveJWTAuth(c, device.UDID, token)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, ErrorNone, ErrorCode(resp.Code), "owner+active 应成功: %s", resp.Message)

		data, err := ParseResponseData[SlaveDeviceCheckAuthResult](w)
		require.NoError(t, err)
		require.Equal(t, subExpires, data.ServiceExpiredAt, "应返回专属订阅时钟而非 user.ExpiredAt")
		require.NotEqual(t, owner.ExpiredAt, data.ServiceExpiredAt, "不应是 user.ExpiredAt")
	})

	t.Run("Stranger_402", func(t *testing.T) {
		// 节点主人是 owner，但 token 属于另一个 user → 非主人，应拒绝。
		owner := CreateTestUser(t)
		stranger := CreateTestUser(t)
		stranger.ExpiredAt = now + 86400 // 即便会员有效也应拒绝
		require.NoError(t, db.Get().Save(stranger).Error)

		device := CreateTestDevice(t, stranger.ID, "udid-stranger-private")
		token := GenerateTestToken(stranger.ID, device.UDID, time.Hour)

		node := SlaveNode{
			Ipv4:               "10.99.0.11",
			SecretToken:        "secret-private-stranger",
			Country:            "HK",
			Region:             "hongkong",
			Name:               "private-hk-stranger",
			Class:              NodeClassPrivate,
			PrivateOwnerUserID: &owner.ID,
		}
		require.NoError(t, db.Get().Create(&node).Error)

		sub := PrivateNodeSubscription{
			UserID: owner.ID, Status: PNStatusActive, Region: "hongkong",
			IPType: IPTypeNonResidential, SlaveNodeID: &node.ID,
			PurchasedAt: now, ExpiresAt: now + 86400,
		}
		require.NoError(t, db.Get().Create(&sub).Error)
		node.PrivateSubID = &sub.ID
		require.NoError(t, db.Get().Save(&node).Error)

		t.Cleanup(func() {
			db.Get().Unscoped().Delete(&sub)
			db.Get().Unscoped().Delete(&node)
		})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/slave/device-check-auth", nil)
		c.Set("i_am_the_node", &node)

		handleSlaveJWTAuth(c, device.UDID, token)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, ErrorPaymentRequired, ErrorCode(resp.Code), "非主人应 402")
	})

	t.Run("NilPrivateSubID_500", func(t *testing.T) {
		owner := CreateTestUser(t)
		owner.ExpiredAt = now + 86400
		require.NoError(t, db.Get().Save(owner).Error)

		device := CreateTestDevice(t, owner.ID, "udid-owner-nilsub")
		token := GenerateTestToken(owner.ID, device.UDID, time.Hour)

		// 专属节点但 PrivateSubID 为 nil（数据完整性问题）。
		node := SlaveNode{
			Ipv4:               "10.99.0.12",
			SecretToken:        "secret-private-nilsub",
			Country:            "HK",
			Region:             "hongkong",
			Name:               "private-hk-nilsub",
			Class:              NodeClassPrivate,
			PrivateOwnerUserID: &owner.ID,
			PrivateSubID:       nil,
		}
		require.NoError(t, db.Get().Create(&node).Error)

		t.Cleanup(func() {
			db.Get().Unscoped().Delete(&node)
		})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/slave/device-check-auth", nil)
		c.Set("i_am_the_node", &node)

		handleSlaveJWTAuth(c, device.UDID, token)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, ErrorSystemError, ErrorCode(resp.Code), "nil PrivateSubID 应 500（非 402）")
	})
}
