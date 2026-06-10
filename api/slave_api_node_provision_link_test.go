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
//   - 把对应 NodeProvisionJob 翻 succeeded
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

	// 预清理同 sub_id 的残留 job（sub_id uniqueIndex）。
	db.Get().Unscoped().Where("sub_id = ?", sub.ID).Delete(&NodeProvisionJob{})

	job := NodeProvisionJob{
		SubID:             sub.ID,
		Status:            NPJStatusClaimed,
		Region:            "hongkong",
		ComposeVariant:    "private",
		TrafficTotalBytes: 2 << 40,
		IPType:            IPTypeNonResidential,
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

	// job 应翻 succeeded。
	var reloadedJob NodeProvisionJob
	require.NoError(t, db.Get().Where("id = ?", job.ID).First(&reloadedJob).Error)
	require.Equal(t, NPJStatusSucceeded, reloadedJob.Status, "provision job 应翻 succeeded")
}
