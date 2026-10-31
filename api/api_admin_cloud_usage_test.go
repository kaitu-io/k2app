package center

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"

	"github.com/kaitu-io/k2app/api/cloudprovider"
)

func seedCloudRow(t *testing.T, provider, ip string) *CloudInstance {
	t.Helper()
	ci := &CloudInstance{
		Provider:          provider,
		AccountName:       "usage-test",
		InstanceID:        generateId("ci"),
		Name:              "usage-test",
		IPAddress:         ip,
		Region:            "test",
		TrafficUsedBytes:  100 << 30,
		TrafficTotalBytes: 200 << 30,
	}
	require.NoError(t, db.Get().Create(ci).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(ci) })
	return ci
}

func seedLinkedNode(t *testing.T, ip string, used, quota int64) {
	t.Helper()
	node := &SlaveNode{Ipv4: ip, Name: "usage-test-" + ip}
	require.NoError(t, db.Get().Create(node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(node) })

	usage := &NodeUsage{NodeID: node.ID, Ipv4: ip, Epoch: time.Now().Unix() + 86400, UsedBytes: used, QuotaTotalBytes: quota, LastReportAt: time.Now().Unix()}
	require.NoError(t, db.Get().Create(usage).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(usage) })
}

func cloudItemByID(items []map[string]any, id uint64) map[string]any {
	for _, it := range items {
		if it["id"] == float64(id) {
			return it
		}
	}
	return nil
}

func TestAdminListCloudInstances_ShowsBothUsagesAndHidesCoveredPlaceholder(t *testing.T) {
	skipIfNoConfig(t)
	coveredIP := fmt.Sprintf("10.77.%d.1", time.Now().UnixNano()%250)
	standaloneIP := fmt.Sprintf("10.78.%d.1", time.Now().UnixNano()%250)
	unlinkedIP := fmt.Sprintf("10.79.%d.1", time.Now().UnixNano()%250)

	provider := seedCloudRow(t, cloudprovider.ProviderAWSLightsail, coveredIP)
	placeholder := seedCloudRow(t, cloudprovider.ProviderSSHStandalone, coveredIP)
	standalone := seedCloudRow(t, cloudprovider.ProviderSSHStandalone, standaloneIP)
	unlinked := seedCloudRow(t, cloudprovider.ProviderBandwagon, unlinkedIP)
	seedLinkedNode(t, coveredIP, 50<<30, 100<<30)
	seedLinkedNode(t, standaloneIP, 1849<<30, 1850<<30)

	items := adminListItems(t, "/app/cloud/instances", api_admin_list_cloud_instances, "pageSize=100")

	assert.Nil(t, cloudItemByID(items, placeholder.ID), "有厂商行的 IP 不再显示占位行")

	row := cloudItemByID(items, provider.ID)
	require.NotNil(t, row)
	assert.Equal(t, float64(100), row["traffic_used_gb"], "厂商数据保持不变")
	meter := row["meter"].(map[string]any)
	assert.Equal(t, float64(50), meter["used_gb"])
	assert.Equal(t, float64(100), meter["total_gb"])
	assert.Equal(t, 0.5, meter["ratio"])

	row = cloudItemByID(items, standalone.ID)
	require.NotNil(t, row, "没有厂商行的节点保留，页面靠它显示自测用量")
	assert.Equal(t, float64(1849), row["meter"].(map[string]any)["used_gb"])
	assert.Less(t, row["time_ratio"], float64(1), "占位行的时间进度跟节点周期")

	row = cloudItemByID(items, unlinked.ID)
	require.NotNil(t, row)
	assert.NotContains(t, row, "meter", "未关联节点不带自测数据")
}

type fakePoolProvider struct {
	pool *cloudprovider.AccountPool
	err  error
}

func (p *fakePoolProvider) AccountPool(context.Context) (*cloudprovider.AccountPool, error) {
	return p.pool, p.err
}

func TestSyncAccountPool_SavesFiguresAndKeepsThemOnFailure(t *testing.T) {
	skipIfNoConfig(t)
	account := CloudInstanceAccount{Provider: cloudprovider.ProviderVultr, Name: "pool-" + generateId("t")}
	t.Cleanup(func() { db.Get().Where("account_name = ?", account.Name).Delete(&CloudAccountUsage{}) })
	resetAt := time.Now().Add(24 * time.Hour)

	syncAccountPool(context.Background(), account, &fakePoolProvider{pool: &cloudprovider.AccountPool{UsedBytes: 10 << 30, TotalBytes: 100 << 30, ResetAt: resetAt}})
	syncAccountPool(context.Background(), account, &fakePoolProvider{pool: &cloudprovider.AccountPool{UsedBytes: 20 << 30, TotalBytes: 100 << 30, ResetAt: resetAt}})

	var usage CloudAccountUsage
	require.NoError(t, db.Get().Where("account_name = ?", account.Name).First(&usage).Error)
	assert.Equal(t, int64(20)<<30, usage.UsedBytes, "同一账号更新同一行")

	syncAccountPool(context.Background(), account, &fakePoolProvider{err: errors.New("vultr down")})

	require.NoError(t, db.Get().Where("account_name = ?", account.Name).First(&usage).Error)
	assert.Equal(t, int64(20)<<30, usage.UsedBytes, "失败时保留上次数据")
	assert.Equal(t, "vultr down", usage.SyncError)

	syncAccountPool(context.Background(), account, &fakePoolProvider{pool: &cloudprovider.AccountPool{UsedBytes: 30 << 30, TotalBytes: 100 << 30, ResetAt: resetAt}})
	require.NoError(t, db.Get().Where("account_name = ?", account.Name).First(&usage).Error)
	assert.Empty(t, usage.SyncError, "恢复后清掉错误")

	items := adminListItems(t, "/app/cloud/account-usages", api_admin_list_cloud_account_usages, "")
	var found bool
	for _, it := range items {
		if it["account_name"] == account.Name {
			found = true
			assert.Equal(t, float64(30), it["used_gb"])
			assert.Equal(t, 0.3, it["ratio"])
		}
	}
	assert.True(t, found)
}
