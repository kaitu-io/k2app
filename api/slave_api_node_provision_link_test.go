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

// TestSelfRegister_LinksCloudInstanceAndCompletesJob 驱动真实的节点自注册 handler
// (api_slave_node_upsert)，验证带 claim 的私有节点自注册激活订阅后，两步 best-effort：
//   - 按 IP 匹配 CloudInstance，回填 sub.cloud_instance_id
//   - 把对应 provision NodeOperation 翻 done
//
// 注册响应本身不应被这两步影响（best-effort），且节点应被置 private。
func TestSelfRegister_LinksCloudInstanceAndCompletesJob(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()
	const fixedIP = "10.99.8.5"
	const claimToken = "claim-link-test"

	// 自愈：清理上一轮残留（共享 dev MySQL，idx_slave_nodes_ipv4 唯一）。
	db.Get().Unscoped().Where("ipv4 = ?", fixedIP).Delete(&SlaveNode{})
	db.Get().Unscoped().Where("ip_address = ?", fixedIP).Delete(&CloudInstance{})

	owner := CreateTestUser(t)

	sub := PrivateNodeSubscription{
		UserID:              owner.ID,
		OrderID:             owner.ID, // 一单一 sub（uniqueIndex），借 user.ID 当唯一订单号
		Status:              PNStatusProvisioning,
		Region:              "hongkong",
		IPType:              IPTypeNonResidential,
		TrafficTotalBytes:   2 << 40,
		PurchasedAt:         now,
		ExpiresAt:           now + 86400,
		ProvisionClaimToken: claimToken,
	}
	require.NoError(t, db.Get().Create(&sub).Error)

	// 预清理同 sub_id 的残留 op。
	db.Get().Unscoped().Where("sub_id = ?", sub.ID).Delete(&NodeOperation{})

	job := NodeOperation{
		Action:    NodeOpProvision,
		SubID:     sub.ID,
		Status:    NodeOpClaimed,
		CreatedBy: "system:order",
		Params: mustJSON(ProvisionParams{
			Region:            "hongkong",
			TrafficTotalBytes: 2 << 40,
			IPType:            IPTypeNonResidential,
		}),
	}
	require.NoError(t, db.Get().Create(&job).Error)

	ci := CloudInstance{
		Provider:    "ssh_standalone",
		AccountName: "test-account",
		InstanceID:  "test-instance-link-" + fixedIP,
		Name:        "test-private-node",
		IPAddress:   fixedIP,
		Region:      "hongkong",
	}
	require.NoError(t, db.Get().Create(&ci).Error)

	var node SlaveNode

	t.Cleanup(func() {
		db.Get().Unscoped().Where("ipv4 = ?", fixedIP).Delete(&SlaveNode{})
		db.Get().Unscoped().Delete(&ci)
		db.Get().Unscoped().Delete(&job)
		db.Get().Unscoped().Delete(&sub)
		db.Get().Unscoped().Delete(owner)
	})

	// 驱动真实 handler：PUT /slave/nodes/:ipv4，body 带 secretToken + privateClaim。
	body, err := json.Marshal(SlaveNodeUpsertRequest{
		Country:      "HK",
		Region:       "hongkong",
		Name:         "private-hk-selfreg",
		SecretToken:  "secret-selfreg-link",
		PrivateClaim: claimToken,
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("PUT", "/slave/nodes/"+fixedIP, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "ipv4", Value: fixedIP}}

	api_slave_node_upsert(c)

	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.Equal(t, ErrorNone, ErrorCode(resp.Code), "注册应成功（best-effort 步骤不应阻塞）: %s", resp.Message)

	// 节点应被置 private。
	require.NoError(t, db.Get().Where("ipv4 = ?", fixedIP).First(&node).Error)
	require.Equal(t, NodeClassPrivate, node.Class, "节点应被认领为 private")

	// sub 应 active 且回填 cloud_instance_id。
	var reloadedSub PrivateNodeSubscription
	require.NoError(t, db.Get().Where("id = ?", sub.ID).First(&reloadedSub).Error)
	require.Equal(t, PNStatusActive, reloadedSub.Status, "订阅应被激活")
	require.NotNil(t, reloadedSub.CloudInstanceID, "应回填 cloud_instance_id")
	require.Equal(t, ci.ID, *reloadedSub.CloudInstanceID, "cloud_instance_id 应指向匹配 IP 的 CloudInstance")

	// op 应翻 done。
	var reloadedJob NodeOperation
	require.NoError(t, db.Get().Where("id = ?", job.ID).First(&reloadedJob).Error)
	require.Equal(t, NodeOpDone, reloadedJob.Status, "provision operation 应翻 done")
}

// TestProvisionLink_WritesSoldQuotaToCloudInstance 验证 Part 1：私有节点自注册认领时，把
// 卖出配额 (sub.TrafficTotalBytes) 写到链接的 CloudInstance.TrafficTotalBytes 并初始化计费
// 周期 (traffic_reset_at)。预置 CloudInstance 带 provider bundle (6TB) + resetAt=0，证明认领
// 用卖出配额 (2TB) 覆盖 bundle 并初始化 reset。
func TestProvisionLink_WritesSoldQuotaToCloudInstance(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()
	const fixedIP = "10.99.8.21"
	const claimToken = "claim-sold-quota-test"
	const soldQuota = int64(2) << 40 // 2TB 卖出配额
	const bundle = int64(6) << 40    // 6TB provider bundle（预置在 CloudInstance）

	db.Get().Unscoped().Where("ipv4 = ?", fixedIP).Delete(&SlaveNode{})
	db.Get().Unscoped().Where("ip_address = ?", fixedIP).Delete(&CloudInstance{})

	owner := CreateTestUser(t)

	sub := PrivateNodeSubscription{
		UserID:              owner.ID,
		OrderID:             owner.ID,
		Status:              PNStatusProvisioning,
		Region:              "hongkong",
		IPType:              IPTypeNonResidential,
		TrafficTotalBytes:   soldQuota,
		PurchasedAt:         now,
		ExpiresAt:           now + 86400,
		ProvisionClaimToken: claimToken,
	}
	require.NoError(t, db.Get().Create(&sub).Error)

	// 预置 CloudInstance：带 provider bundle 6TB、resetAt=0（未初始化周期）。
	ci := CloudInstance{
		Provider:          "aws_lightsail",
		AccountName:       "test-account",
		InstanceID:        "test-sold-quota-" + fixedIP,
		Name:              "test-private-node",
		IPAddress:         fixedIP,
		Region:            "hongkong",
		TrafficTotalBytes: bundle,
		TrafficResetAt:    0,
	}
	require.NoError(t, db.Get().Create(&ci).Error)

	t.Cleanup(func() {
		db.Get().Unscoped().Where("ipv4 = ?", fixedIP).Delete(&SlaveNode{})
		db.Get().Unscoped().Delete(&ci)
		db.Get().Unscoped().Delete(&sub)
		db.Get().Unscoped().Delete(owner)
	})

	resp := driveSlaveUpsert(t, fixedIP, SlaveNodeUpsertRequest{
		Country: "HK", Region: "hongkong", Name: "sold-quota-node",
		SecretToken: "secret-sold-quota", PrivateClaim: claimToken,
	})
	require.Equal(t, ErrorNone, ErrorCode(resp.Code), "注册应成功: %s", resp.Message)

	// sub 应 active 且回填 cloud_instance_id。
	var reloadedSub PrivateNodeSubscription
	require.NoError(t, db.Get().First(&reloadedSub, sub.ID).Error)
	require.Equal(t, PNStatusActive, reloadedSub.Status, "订阅应被激活")
	require.NotNil(t, reloadedSub.CloudInstanceID, "应回填 cloud_instance_id")
	require.Equal(t, ci.ID, *reloadedSub.CloudInstanceID)

	// CloudInstance 的卖出配额应被写为 sub.TrafficTotalBytes（覆盖 bundle），周期被初始化。
	var reloadedCI CloudInstance
	require.NoError(t, db.Get().First(&reloadedCI, ci.ID).Error)
	require.Equal(t, soldQuota, reloadedCI.TrafficTotalBytes, "CloudInstance 应写卖出配额而非 provider bundle")
	require.Greater(t, reloadedCI.TrafficResetAt, int64(0), "计费周期应被初始化")
}

// driveSlaveUpsert 复用：用 gin 测试 context 调 handler（PUT /slave/nodes/:ipv4）。
func driveSlaveUpsert(t *testing.T, ip string, req SlaveNodeUpsertRequest) *TestResponse {
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

// TestSelfRegister_ClaimSecurity 验证原子认领 CAS 的三条安全不变量：
//   - failed 态订阅不被注册侧 activation 复活（resurrection guard）。
//   - active 态订阅不可被持 token 的不同 IP 重认领（MITM/owner 劫持 guard）。
//   - 认领成功即置空 token，杜绝重放；二次同 token 注册为幂等 no-op。
func TestSelfRegister_ClaimSecurity(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()

	t.Run("FailedSubNotResurrected", func(t *testing.T) {
		const ip = "10.99.8.11"
		const claimToken = "claim-failed-resurrect"
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})

		owner := CreateTestUser(t)
		sub := PrivateNodeSubscription{
			UserID: owner.ID, OrderID: owner.ID, Status: PNStatusFailed,
			Region: "hongkong", IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40,
			PurchasedAt: now, ExpiresAt: now + 86400, ProvisionClaimToken: claimToken,
		}
		require.NoError(t, db.Get().Create(&sub).Error)

		db.Get().Unscoped().Where("sub_id = ?", sub.ID).Delete(&NodeOperation{})
		job := NodeOperation{
			Action: NodeOpProvision, SubID: sub.ID, Status: NodeOpFailed, CreatedBy: "system:order",
			Params: mustJSON(ProvisionParams{
				Region:            "hongkong",
				TrafficTotalBytes: 2 << 40, IPType: IPTypeNonResidential,
			}),
		}
		require.NoError(t, db.Get().Create(&job).Error)

		t.Cleanup(func() {
			db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
			db.Get().Unscoped().Delete(&job)
			db.Get().Unscoped().Delete(&sub)
			db.Get().Unscoped().Delete(owner)
		})

		resp := driveSlaveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "HK", Region: "hongkong", Name: "failed-resurrect",
			SecretToken: "secret-failed", PrivateClaim: claimToken,
		})
		require.Equal(t, ErrorNone, ErrorCode(resp.Code), "注册仍应成功（claim best-effort）: %s", resp.Message)

		// sub 必须仍 failed（不被复活）。
		var reloadedSub PrivateNodeSubscription
		require.NoError(t, db.Get().First(&reloadedSub, sub.ID).Error)
		require.Equal(t, PNStatusFailed, reloadedSub.Status, "failed 订阅不应被注册侧复活")

		// 节点必须仍非 private。
		var node SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&node).Error)
		require.NotEqual(t, NodeClassPrivate, node.Class, "failed claim 不应把节点置 private")

		// op 不应被翻 done。
		var reloadedJob NodeOperation
		require.NoError(t, db.Get().First(&reloadedJob, job.ID).Error)
		require.NotEqual(t, NodeOpDone, reloadedJob.Status, "failed claim 不应翻 operation done")
	})

	t.Run("ActiveSubNotReClaimable", func(t *testing.T) {
		const ipA = "10.99.8.12"
		const ipB = "10.99.8.13"
		const claimToken = "claim-active-mitm"
		db.Get().Unscoped().Where("ipv4 IN ?", []string{ipA, ipB}).Delete(&SlaveNode{})

		owner := CreateTestUser(t)

		// 已存在的合法 node A。
		nodeA := SlaveNode{
			Ipv4: ipA, Country: "HK", Name: "legit-node-A", SecretToken: "secret-A",
			Class: NodeClassPrivate, PrivateOwnerUserID: &owner.ID,
		}
		require.NoError(t, db.Get().Create(&nodeA).Error)

		// active 订阅，已绑定 node A，token 仍非空（模拟泄漏/未清）。
		sub := PrivateNodeSubscription{
			UserID: owner.ID, OrderID: owner.ID, Status: PNStatusActive,
			Region: "hongkong", IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40,
			PurchasedAt: now, ExpiresAt: now + 86400, ProvisionClaimToken: claimToken,
			SlaveNodeID: &nodeA.ID,
		}
		require.NoError(t, db.Get().Create(&sub).Error)
		// 回填 node A 的 private_sub_id。
		require.NoError(t, db.Get().Model(&SlaveNode{}).Where("id = ?", nodeA.ID).
			Update("private_sub_id", sub.ID).Error)

		t.Cleanup(func() {
			db.Get().Unscoped().Where("ipv4 IN ?", []string{ipA, ipB}).Delete(&SlaveNode{})
			db.Get().Unscoped().Delete(&sub)
			db.Get().Unscoped().Delete(owner)
		})

		// 攻击者用同 token 注册不同 IP 的 node B。
		resp := driveSlaveUpsert(t, ipB, SlaveNodeUpsertRequest{
			Country: "HK", Region: "hongkong", Name: "attacker-node-B",
			SecretToken: "secret-B", PrivateClaim: claimToken,
		})
		require.Equal(t, ErrorNone, ErrorCode(resp.Code), "注册不报错（防探测）: %s", resp.Message)

		// sub 仍指向 node A（未被重定向到 node B）。
		var reloadedSub PrivateNodeSubscription
		require.NoError(t, db.Get().First(&reloadedSub, sub.ID).Error)
		require.NotNil(t, reloadedSub.SlaveNodeID)
		require.Equal(t, nodeA.ID, *reloadedSub.SlaveNodeID, "active 订阅不应被重指向攻击者节点")

		// node B 仍非 private。
		var nodeB SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", ipB).First(&nodeB).Error)
		require.NotEqual(t, NodeClassPrivate, nodeB.Class, "攻击者节点不应被认领为 private")
	})

	t.Run("TokenInvalidatedAfterClaim", func(t *testing.T) {
		const ip = "10.99.8.14"
		const claimToken = "claim-invalidate-once"
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})

		owner := CreateTestUser(t)
		sub := PrivateNodeSubscription{
			UserID: owner.ID, OrderID: owner.ID, Status: PNStatusProvisioning,
			Region: "hongkong", IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40,
			PurchasedAt: now, ExpiresAt: now + 86400, ProvisionClaimToken: claimToken,
		}
		require.NoError(t, db.Get().Create(&sub).Error)

		t.Cleanup(func() {
			db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
			db.Get().Unscoped().Delete(&sub)
			db.Get().Unscoped().Delete(owner)
		})

		// 首次：claim 成功，sub active。
		resp := driveSlaveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "HK", Region: "hongkong", Name: "invalidate-token",
			SecretToken: "secret-inv", PrivateClaim: claimToken,
		})
		require.Equal(t, ErrorNone, ErrorCode(resp.Code), "首次注册应成功: %s", resp.Message)

		var afterClaim PrivateNodeSubscription
		require.NoError(t, db.Get().First(&afterClaim, sub.ID).Error)
		require.Equal(t, PNStatusActive, afterClaim.Status, "首次认领应激活订阅")
		require.Equal(t, "", afterClaim.ProvisionClaimToken, "认领成功后 token 必须被置空（防重放）")

		// 二次：同 token 同节点再注册 → 幂等 no-op，不报错，sub 仍 active。
		resp = driveSlaveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "HK", Region: "hongkong", Name: "invalidate-token",
			SecretToken: "secret-inv", PrivateClaim: claimToken,
		})
		require.Equal(t, ErrorNone, ErrorCode(resp.Code), "二次注册应成功（幂等）: %s", resp.Message)

		var afterReplay PrivateNodeSubscription
		require.NoError(t, db.Get().First(&afterReplay, sub.ID).Error)
		require.Equal(t, PNStatusActive, afterReplay.Status, "二次注册 sub 仍 active")
	})
}
