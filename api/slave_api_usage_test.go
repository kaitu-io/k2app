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

// TestSlaveUsageHeartbeat 直接驱动 api_slave_node_report_usage handler（通过
// c.Set("i_am_the_node", ...) 注入节点上下文，与既有 slave 测试一致），验证：
//   - 累计 max 归并（抗乱序/重复/丢包）
//   - 95% 阈值整数算术裁决（serve / stop）
//   - epoch 身份门控（节点 epoch 落后则不采纳其累计值）
//   - 到期 lazy reset（bump epoch + 清零 used + 前推 TrafficResetAt）
func TestSlaveUsageHeartbeat(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()

	// driveUsage 用 gin 测试 context 调用 handler，模拟已通过 SlaveAuthRequired 的
	// POST /slave/usage（节点上下文已注入）。
	driveUsage := func(t *testing.T, node *SlaveNode, req NodeUsageRequest) *NodeUsageResponse {
		t.Helper()
		body, err := json.Marshal(req)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/slave/usage", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("i_am_the_node", node)

		api_slave_node_report_usage(c)

		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, ErrorNone, ErrorCode(resp.Code), "usage 心跳应成功: %s", resp.Message)

		data, err := ParseResponseData[NodeUsageResponse](w)
		require.NoError(t, err)
		return data
	}

	t.Run("MeterAndVerdict", func(t *testing.T) {
		ip := "203.0.113.61"
		// 自愈：清理上次中断残留。
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
		db.Get().Unscoped().Where("ip_address = ?", ip).Delete(&CloudInstance{})

		node := SlaveNode{
			Ipv4: ip, SecretToken: "usage-test-secret",
			Country: "US", Region: "us-west", Name: "usage-test-node",
		}
		require.NoError(t, db.Get().Create(&node).Error)

		ci := CloudInstance{
			Provider: "ssh_standalone", AccountName: "usage-test", InstanceID: "usage-ci-" + ip,
			Region: "us-west", IPAddress: ip,
			TrafficTotalBytes: 1000, TrafficUsedBytes: 0, TrafficEpoch: 0,
			TrafficResetAt: now + 30*86400,
		}
		require.NoError(t, db.Get().Create(&ci).Error)

		t.Cleanup(func() {
			db.Get().Unscoped().Delete(&node)
			db.Get().Unscoped().Delete(&ci)
		})

		// Case 1: 累计 500（epoch 0）→ serve, used=500。
		r := driveUsage(t, &node, NodeUsageRequest{EpochID: 0, CumulativeBytes: 500, Seq: 1, Ts: now})
		require.Equal(t, "serve", r.Verdict)
		require.Equal(t, int64(500), r.QuotaUsed)
		require.Equal(t, int64(1000), r.QuotaTotal)
		require.Equal(t, int64(0), r.EpochID)

		// Case 2: 累计 960（>=95% of 1000）→ stop。
		r = driveUsage(t, &node, NodeUsageRequest{EpochID: 0, CumulativeBytes: 960, Seq: 2, Ts: now})
		require.Equal(t, "stop", r.Verdict)
		require.Equal(t, int64(960), r.QuotaUsed)

		// Case 3: 重复/乱序发累计 800（< 已存 960）→ max 不回退，used 仍 960。
		r = driveUsage(t, &node, NodeUsageRequest{EpochID: 0, CumulativeBytes: 800, Seq: 3, Ts: now})
		require.Equal(t, int64(960), r.QuotaUsed, "max 归并不应回退")
		require.Equal(t, "stop", r.Verdict)

		// Case 4: 节点 epoch=99（≠当前 0）发累计 5 → 不采纳，used 仍 960，响应 epoch=0。
		r = driveUsage(t, &node, NodeUsageRequest{EpochID: 99, CumulativeBytes: 5, Seq: 4, Ts: now})
		require.Equal(t, int64(960), r.QuotaUsed, "epoch 不符不应采纳其累计值")
		require.Equal(t, int64(0), r.EpochID, "应回当前权威 epoch")
	})

	t.Run("LazyEpochResetOnExpiry", func(t *testing.T) {
		ip := "203.0.113.62"
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
		db.Get().Unscoped().Where("ip_address = ?", ip).Delete(&CloudInstance{})

		node := SlaveNode{
			Ipv4: ip, SecretToken: "usage-reset-secret",
			Country: "US", Region: "us-west", Name: "usage-reset-node",
		}
		require.NoError(t, db.Get().Create(&node).Error)

		// 已过期行：TrafficResetAt=now-1，且已用满 800（epoch 0）。
		ci := CloudInstance{
			Provider: "ssh_standalone", AccountName: "usage-test", InstanceID: "usage-ci-" + ip,
			Region: "us-west", IPAddress: ip,
			TrafficTotalBytes: 1000, TrafficUsedBytes: 800, TrafficEpoch: 0,
			TrafficResetAt: now - 1,
		}
		require.NoError(t, db.Get().Create(&ci).Error)

		t.Cleanup(func() {
			db.Get().Unscoped().Delete(&node)
			db.Get().Unscoped().Delete(&ci)
		})

		// 心跳触发 reset：epoch 0→1、used 清零；本次带新 epoch=1 累计 200 被采纳。
		r := driveUsage(t, &node, NodeUsageRequest{EpochID: 1, CumulativeBytes: 200, Seq: 1, Ts: now})
		require.Equal(t, int64(1), r.EpochID, "应 bump 到新 epoch")
		require.Equal(t, int64(200), r.QuotaUsed, "新 epoch 累计应被采纳（清零后续计）")
		require.Equal(t, "serve", r.Verdict, "新周期 20% 应放行")

		// 落库校验：reset 已持久化、TrafficResetAt 前推。
		var reloaded CloudInstance
		require.NoError(t, db.Get().First(&reloaded, ci.ID).Error)
		require.Equal(t, int64(1), reloaded.TrafficEpoch)
		require.Equal(t, int64(200), reloaded.TrafficUsedBytes)
		require.Greater(t, reloaded.TrafficResetAt, now, "TrafficResetAt 应前推到未来")
	})

	t.Run("NoCloudInstanceServes", func(t *testing.T) {
		ip := "203.0.113.63"
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
		db.Get().Unscoped().Where("ip_address = ?", ip).Delete(&CloudInstance{})

		node := SlaveNode{
			Ipv4: ip, SecretToken: "usage-noci-secret",
			Country: "US", Region: "us-west", Name: "usage-noci-node",
		}
		require.NoError(t, db.Get().Create(&node).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })

		// 无 CloudInstance（共享节点 / 未 sync）→ 不计量，serve。
		r := driveUsage(t, &node, NodeUsageRequest{EpochID: 0, CumulativeBytes: 9999, Seq: 1, Ts: now})
		require.Equal(t, "serve", r.Verdict)
	})
}
