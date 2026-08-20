package center

import (
	"testing"
	"time"

	db "github.com/wordgate/qtoolkit/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kaitu-io/k2app/api/cloudprovider"
)

// currentMonthEndUTC is the calendar-month boundary the AWS correction gate
// keys on (Lightsail resets allowances on the 1st, UTC).
func currentMonthEndUTC() int64 {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC).Unix()
}

// seedCloudInstance inserts an aws_lightsail CloudInstance row (unless provider
// overridden) keyed to ip, cleaned up with the test.
func seedCloudInstance(t *testing.T, ip, provider, instanceID string, used int64, resetAt, syncedAt int64) *CloudInstance {
	t.Helper()
	db.Get().Unscoped().Where("provider = ? AND instance_id = ?", provider, instanceID).Delete(&CloudInstance{})
	ci := &CloudInstance{
		Provider: provider, AccountName: "test-acc", InstanceID: instanceID,
		Name: instanceID, IPAddress: ip, Region: "ap-southeast-2",
		TrafficUsedBytes: used, TrafficTotalBytes: 1024 << 30,
		TrafficResetAt: resetAt, LastSyncedAt: syncedAt,
	}
	require.NoError(t, db.Get().Create(ci).Error)
	t.Cleanup(func() { db.Get().Unscoped().Where("id = ?", ci.ID).Delete(&CloudInstance{}) })
	return ci
}

// TestAwsAuthoritativeUsedBytes_Gates walks every gate of the correction: only
// a fresh, same-cycle, shared-pool aws_lightsail figure ABOVE the self-report
// is returned; anything else yields 0 (no correction, fail-open).
func TestAwsAuthoritativeUsedBytes_Gates(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	monthEnd := currentMonthEndUTC()
	now := time.Now().Unix()
	const gib = int64(1) << 30

	mkReq := func(epoch, used int64) NodeUsageRequest {
		return NodeUsageRequest{EpochID: epoch, CumulativeBytes: used}
	}

	t.Run("happy path: provider figure above self-report", func(t *testing.T) {
		ip := "203.0.113.10"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, "auth-happy", 800*gib, monthEnd, now)
		assert.Equal(t, 800*gib, awsAuthoritativeUsedBytes(ip, mkReq(monthEnd, 400*gib)))
	})

	t.Run("node epoch not the current calendar month", func(t *testing.T) {
		ip := "203.0.113.11"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, "auth-epoch", 800*gib, monthEnd, now)
		assert.Zero(t, awsAuthoritativeUsedBytes(ip, mkReq(monthEnd+86400, 400*gib)))
	})

	t.Run("synced figure from a previous cycle", func(t *testing.T) {
		ip := "203.0.113.12"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, "auth-cycle", 800*gib, monthEnd-30*86400, now)
		assert.Zero(t, awsAuthoritativeUsedBytes(ip, mkReq(monthEnd, 400*gib)))
	})

	t.Run("stale sync", func(t *testing.T) {
		ip := "203.0.113.13"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, "auth-stale", 800*gib, monthEnd, now-3*3600)
		assert.Zero(t, awsAuthoritativeUsedBytes(ip, mkReq(monthEnd, 400*gib)))
	})

	t.Run("not an AWS instance", func(t *testing.T) {
		ip := "203.0.113.14"
		seedCloudInstance(t, ip, cloudprovider.ProviderBandwagon, "auth-bwg", 800*gib, monthEnd, now)
		assert.Zero(t, awsAuthoritativeUsedBytes(ip, mkReq(monthEnd, 400*gib)))
	})

	t.Run("provider figure at or below self-report", func(t *testing.T) {
		ip := "203.0.113.15"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, "auth-lower", 300*gib, monthEnd, now)
		assert.Zero(t, awsAuthoritativeUsedBytes(ip, mkReq(monthEnd, 400*gib)))
	})

	t.Run("dedicated-line instance is excluded", func(t *testing.T) {
		ip := "203.0.113.16"
		ci := seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, "auth-private", 800*gib, monthEnd, now)
		pns := &PrivateNodeSubscription{
			UserID: 999999, PlanID: 1, OrderID: uint64(time.Now().UnixNano()),
			CloudInstanceID: &ci.ID, Region: "au", IPType: "non_residential",
			TrafficTotalBytes: 2 << 40, Status: PNStatusActive,
			PurchasedAt: now, ExpiresAt: now + 86400,
		}
		require.NoError(t, db.Get().Create(pns).Error)
		t.Cleanup(func() { db.Get().Unscoped().Where("id = ?", pns.ID).Delete(&PrivateNodeSubscription{}) })
		assert.Zero(t, awsAuthoritativeUsedBytes(ip, mkReq(monthEnd, 400*gib)))
	})
}

// TestReportUsage_CarriesAuthoritativeCorrection: end-to-end through the
// handler — the response carries the provider figure and node_usages is raised
// to it (never lowered).
func TestReportUsage_CarriesAuthoritativeCorrection(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	monthEnd := currentMonthEndUTC()
	now := time.Now().Unix()
	const gib = int64(1) << 30
	ip := "203.0.113.20"

	node := seedSlaveNodeForUsageTest(t, ip)
	db.Get().Where("ipv4 = ?", ip).Delete(&NodeUsage{})
	t.Cleanup(func() { db.Get().Where("ipv4 = ?", ip).Delete(&NodeUsage{}) })
	seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, "auth-e2e", 900*gib, monthEnd, now)

	resp := recordUsage(t, node, NodeUsageRequest{
		EpochID: monthEnd, CumulativeBytes: 400 * gib, QuotaTotalBytes: 1024 * gib, Seq: 1,
	})
	assert.Equal(t, 900*gib, resp.AuthoritativeUsedBytes, "response carries the provider figure")

	var u NodeUsage
	require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&u).Error)
	assert.Equal(t, 900*gib, u.UsedBytes, "record raised to the authoritative figure")

	// A later, lower self-report must not lower the record, and the correction
	// keeps being offered while the node meter lags.
	resp = recordUsage(t, node, NodeUsageRequest{
		EpochID: monthEnd, CumulativeBytes: 401 * gib, QuotaTotalBytes: 1024 * gib, Seq: 2,
	})
	assert.Equal(t, 900*gib, resp.AuthoritativeUsedBytes)
	require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&u).Error)
	assert.Equal(t, 900*gib, u.UsedBytes)
}

// TestReportUsage_NoCorrectionForPlainNode: a node with no CloudInstance row
// gets the plain ack (regression guard: correction path must not disturb the
// ordinary flow).
func TestReportUsage_NoCorrectionForPlainNode(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	ip := "203.0.113.21"
	node := seedSlaveNodeForUsageTest(t, ip)
	db.Get().Where("ipv4 = ?", ip).Delete(&NodeUsage{})
	t.Cleanup(func() { db.Get().Where("ipv4 = ?", ip).Delete(&NodeUsage{}) })

	resp := recordUsage(t, node, NodeUsageRequest{
		EpochID: currentMonthEndUTC(), CumulativeBytes: 5 << 30, QuotaTotalBytes: 2 << 40, Seq: 1,
	})
	assert.Zero(t, resp.AuthoritativeUsedBytes)
	var u NodeUsage
	require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&u).Error)
	assert.Equal(t, int64(5<<30), u.UsedBytes)
}
