package center

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestSlaveNodeUpsert_PrivateClaim 直接驱动 api_slave_node_upsert handler，验证
// 专属节点的注册侧认领与 class 保全（spec §7.4）：
//   - A: 携带有效 ProvisionClaimToken 注册 → 节点 class 置 private + 归属回填，
//     订阅置 active 并回填 SlaveNodeID。
//   - B: 同 ipv4 + 同 secretToken 无 claim 重注册（sidecar 重启 / connect-url
//     再注册）→ class 仍为 private，归属保全（sidecar 不发 Class，缺此逻辑会被
//     重置成 shared）。
//   - C: 全新 ipv4 无 claim → class 非 private（shared 或 ""），不报错。
func TestSlaveNodeUpsert_PrivateClaim(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()

	// 自愈：清理上次中断运行残留的固定 IPv4 节点（共享 dev MySQL 上
	// idx_slave_nodes_ipv4 是唯一索引）。
	ips := []string{"10.99.21.1", "10.99.21.3"}
	for _, ip := range ips {
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
	}

	// driveUpsert 用 gin 测试 context 调用 handler，模拟 PUT /slave/nodes/:ipv4。
	driveUpsert := func(t *testing.T, ip string, req SlaveNodeUpsertRequest) *TestResponse {
		t.Helper()
		body, err := json.Marshal(req)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "ipv4", Value: ip}}
		c.Request = httptest.NewRequest("PUT", "/slave/nodes/"+ip, bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")

		api_slave_node_upsert(c)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		return resp
	}

	t.Run("A_ClaimActivates", func(t *testing.T) {
		owner := CreateTestUser(t)

		claimToken := "claim-tok-A-" + generateId("c")
		sub := PrivateNodeSubscription{
			UserID: owner.ID, Status: PNStatusProvisioning, Region: "hongkong",
			IPType: IPTypeNonResidential, PurchasedAt: now, ExpiresAt: now + 86400,
			ProvisionClaimToken: claimToken,
		}
		require.NoError(t, db.Get().Create(&sub).Error)

		ip := "10.99.21.1"
		t.Cleanup(func() {
			db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
			db.Get().Unscoped().Delete(&sub)
		})

		resp := driveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "HK", Name: "private-claim-A", SecretToken: "secret-A",
			PrivateClaim: claimToken,
		})
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "注册应成功: %s", resp.Message)

		// 重新加载节点：应已置 private + 归属回填。
		var node SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&node).Error)
		require.Equal(t, NodeClassPrivate, node.Class, "应认领为 private")
		require.NotNil(t, node.PrivateOwnerUserID)
		require.Equal(t, owner.ID, *node.PrivateOwnerUserID)
		require.NotNil(t, node.PrivateSubID)
		require.Equal(t, sub.ID, *node.PrivateSubID)

		// 重新加载订阅：应已激活 + 回填 SlaveNodeID。
		var reloadedSub PrivateNodeSubscription
		require.NoError(t, db.Get().First(&reloadedSub, sub.ID).Error)
		require.Equal(t, PNStatusActive, reloadedSub.Status, "订阅应激活")
		require.NotNil(t, reloadedSub.SlaveNodeID)
		require.Equal(t, node.ID, *reloadedSub.SlaveNodeID)
	})

	t.Run("B_PreserveOnReRegister", func(t *testing.T) {
		owner := CreateTestUser(t)

		claimToken := "claim-tok-B-" + generateId("c")
		sub := PrivateNodeSubscription{
			UserID: owner.ID, Status: PNStatusProvisioning, Region: "hongkong",
			IPType: IPTypeNonResidential, PurchasedAt: now, ExpiresAt: now + 86400,
			ProvisionClaimToken: claimToken,
		}
		require.NoError(t, db.Get().Create(&sub).Error)

		ip := "10.99.21.1" // 复用 A 的 IP（A 已在 Cleanup 中清理，子测试串行）
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
		t.Cleanup(func() {
			db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
			db.Get().Unscoped().Delete(&sub)
		})

		// 首次注册 + claim → private。
		resp := driveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "HK", Name: "private-claim-B", SecretToken: "secret-B",
			PrivateClaim: claimToken,
		})
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "首次注册应成功: %s", resp.Message)

		var first SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&first).Error)
		require.Equal(t, NodeClassPrivate, first.Class)

		// 模拟 sidecar 重启：同 ipv4 + 同 secretToken，不带 claim。
		resp = driveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "HK", Name: "private-claim-B", SecretToken: "secret-B",
		})
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "重注册应成功: %s", resp.Message)

		// 重新加载：class 应保全为 private，归属保全。
		var node SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&node).Error)
		require.Equal(t, NodeClassPrivate, node.Class, "重注册后应仍为 private（class 保全）")
		require.NotNil(t, node.PrivateOwnerUserID)
		require.Equal(t, owner.ID, *node.PrivateOwnerUserID, "归属应保全")
		require.NotNil(t, node.PrivateSubID)
		require.Equal(t, sub.ID, *node.PrivateSubID, "PrivateSubID 应保全")
	})

	t.Run("C_NoClaimStaysShared", func(t *testing.T) {
		ip := "10.99.21.3"
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
		t.Cleanup(func() {
			db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
		})

		resp := driveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "US", Name: "shared-node-C", SecretToken: "secret-C",
		})
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "无 claim 注册应成功: %s", resp.Message)

		var node SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&node).Error)
		require.NotEqual(t, NodeClassPrivate, node.Class, "无 claim 不应是 private")
		require.Nil(t, node.PrivateOwnerUserID)
		require.Nil(t, node.PrivateSubID)
	})
}
