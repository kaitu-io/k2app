package center

import (
	"context"
	"testing"
	"time"

	"github.com/kaitu-io/k2app/api/cloudprovider"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestUpsertCloudInstance_PrivateSkipsTrafficFields 验证 provider 周期同步对私有
// (专属线路) 实例跳过 traffic 字段：卖出配额 / 自计量用量 / Center epoch 周期为权威，
// provider 报的 VPS bundle 绝不可覆盖它们；但 IP/name/region/last_synced 仍正常更新。
func TestUpsertCloudInstance_PrivateSkipsTrafficFields(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	const (
		soldQuota = int64(2) << 40 // 2TB 卖出配额
		usedBytes = int64(500) << 30
		bundle    = int64(6) << 40 // 6TB VPS bundle
	)
	resetAt := time.Now().Unix() + 12345
	instanceID := "test-private-skip-" + time.Now().Format("20060102150405.000000")

	ci := CloudInstance{
		Provider:          "aws_lightsail",
		AccountName:       "test-account",
		InstanceID:        instanceID,
		Name:              "old-name",
		IPAddress:         "10.50.0.1",
		Region:            "ap-northeast-1",
		TrafficUsedBytes:  usedBytes,
		TrafficTotalBytes: soldQuota,
		TrafficResetAt:    resetAt,
	}
	require.NoError(t, db.Get().Create(&ci).Error)

	owner := CreateTestUser(t)
	ciID := ci.ID
	sub := PrivateNodeSubscription{
		UserID:            owner.ID,
		OrderID:           owner.ID,
		Status:            PNStatusActive,
		Region:            "hongkong",
		IPType:            IPTypeNonResidential,
		TrafficTotalBytes: soldQuota,
		PurchasedAt:       time.Now().Unix(),
		ExpiresAt:         time.Now().Unix() + 86400,
		CloudInstanceID:   &ciID,
	}
	require.NoError(t, db.Get().Create(&sub).Error)

	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&sub)
		db.Get().Unscoped().Delete(&ci)
		db.Get().Unscoped().Delete(owner)
	})

	status := &cloudprovider.InstanceStatus{
		InstanceID:        instanceID,
		Name:              "new-name",
		IPAddress:         "10.50.0.99",
		Region:            "us-east-1",
		TrafficUsedBytes:  0,
		TrafficTotalBytes: bundle,
		TrafficResetAt:    time.Unix(resetAt+99999, 0),
	}

	account := CloudInstanceAccount{Name: "test-account", Provider: "aws_lightsail"}
	require.NoError(t, upsertCloudInstance(context.Background(), account, status))

	var reloaded CloudInstance
	require.NoError(t, db.Get().Unscoped().First(&reloaded, ci.ID).Error)

	// traffic 字段必须被跳过（保持卖出配额 / 自计量用量 / Center 周期）。
	require.Equal(t, soldQuota, reloaded.TrafficTotalBytes, "卖出配额不应被 provider bundle 覆盖")
	require.Equal(t, usedBytes, reloaded.TrafficUsedBytes, "自计量用量不应被 provider 清零")
	require.Equal(t, resetAt, reloaded.TrafficResetAt, "Center epoch 周期不应被 provider 覆盖")

	// 非 traffic 字段仍正常更新。
	require.Equal(t, "new-name", reloaded.Name, "name 应被 provider 更新")
	require.Equal(t, "10.50.0.99", reloaded.IPAddress, "IP 应被 provider 更新")
	require.Equal(t, "us-east-1", reloaded.Region, "region 应被 provider 更新")
	require.NotZero(t, reloaded.LastSyncedAt, "last_synced_at 应被更新")
}

// TestUpsertCloudInstance_SharedOverwritesTrafficFields 回归守卫：非私有实例（无指向它的
// PrivateNodeSubscription）的 traffic 字段仍按 provider 报告覆盖，跳过逻辑只限私有实例。
func TestUpsertCloudInstance_SharedOverwritesTrafficFields(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	const bundle = int64(6) << 40
	instanceID := "test-shared-overwrite-" + time.Now().Format("20060102150405.000000")

	ci := CloudInstance{
		Provider:          "aws_lightsail",
		AccountName:       "test-account",
		InstanceID:        instanceID,
		Name:              "old-name",
		IPAddress:         "10.51.0.1",
		Region:            "ap-northeast-1",
		TrafficUsedBytes:  int64(100) << 30,
		TrafficTotalBytes: int64(1) << 40,
		TrafficResetAt:    time.Now().Unix(),
	}
	require.NoError(t, db.Get().Create(&ci).Error)

	t.Cleanup(func() { db.Get().Unscoped().Delete(&ci) })

	newReset := time.Now().Unix() + 55555
	status := &cloudprovider.InstanceStatus{
		InstanceID:        instanceID,
		Name:              "new-name",
		IPAddress:         "10.51.0.2",
		Region:            "us-east-1",
		TrafficUsedBytes:  int64(2) << 40,
		TrafficTotalBytes: bundle,
		TrafficResetAt:    time.Unix(newReset, 0),
	}

	account := CloudInstanceAccount{Name: "test-account", Provider: "aws_lightsail"}
	require.NoError(t, upsertCloudInstance(context.Background(), account, status))

	var reloaded CloudInstance
	require.NoError(t, db.Get().Unscoped().First(&reloaded, ci.ID).Error)

	require.Equal(t, bundle, reloaded.TrafficTotalBytes, "共享实例 traffic_total 应被 provider 覆盖")
	require.Equal(t, int64(2)<<40, reloaded.TrafficUsedBytes, "共享实例 traffic_used 应被 provider 覆盖")
	require.Equal(t, newReset, reloaded.TrafficResetAt, "共享实例 traffic_reset 应被 provider 覆盖")
}
