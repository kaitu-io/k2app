package center

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
	db "github.com/wordgate/qtoolkit/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kaitu-io/k2app/api/cloudprovider"
)

// stopRecordingProvider is a minimal Provider + InstanceStopper that records
// StopInstance calls (and can be scripted to fail).
type stopRecordingProvider struct {
	mu       sync.Mutex
	stops    []string
	stopFail bool
}

func (p *stopRecordingProvider) Name() string { return cloudprovider.ProviderAWSLightsail }
func (p *stopRecordingProvider) GetInstanceStatus(context.Context, string) (*cloudprovider.InstanceStatus, error) {
	return nil, nil
}
func (p *stopRecordingProvider) ListInstances(context.Context) ([]*cloudprovider.InstanceStatus, error) {
	return nil, nil
}
func (p *stopRecordingProvider) ChangeIP(context.Context, string, cloudprovider.ChangeIPOptions) (*cloudprovider.OperationResult, error) {
	return nil, nil
}
func (p *stopRecordingProvider) CreateInstance(context.Context, cloudprovider.CreateInstanceOptions) (*cloudprovider.OperationResult, error) {
	return nil, nil
}
func (p *stopRecordingProvider) DeleteInstance(context.Context, string) (*cloudprovider.OperationResult, error) {
	return nil, nil
}
func (p *stopRecordingProvider) ListRegions(context.Context) ([]cloudprovider.RegionInfo, error) {
	return nil, nil
}
func (p *stopRecordingProvider) ListPlans(context.Context, string) ([]cloudprovider.PlanInfo, error) {
	return nil, nil
}
func (p *stopRecordingProvider) ListImages(context.Context, string) ([]cloudprovider.ImageInfo, error) {
	return nil, nil
}
func (p *stopRecordingProvider) StopInstance(_ context.Context, id string) (*cloudprovider.OperationResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopFail {
		return nil, assert.AnError
	}
	p.stops = append(p.stops, id)
	return &cloudprovider.OperationResult{Success: true}, nil
}
func (p *stopRecordingProvider) stopCount() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.stops) }

// awsStatus builds the synced InstanceStatus the sync loop hands the checker.
func awsStatus(instanceID, ip string, used, total int64, resetAt int64) *cloudprovider.InstanceStatus {
	return &cloudprovider.InstanceStatus{
		InstanceID: instanceID, Name: instanceID, IPAddress: ip,
		Region: "ap-southeast-2", State: "running",
		TrafficUsedBytes: used, TrafficTotalBytes: total,
		TrafficResetAt: time.Unix(resetAt, 0),
	}
}

func loadCloudInstance(t *testing.T, instanceID string) CloudInstance {
	t.Helper()
	var ci CloudInstance
	require.NoError(t, db.Get().
		Where("provider = ? AND instance_id = ?", cloudprovider.ProviderAWSLightsail, instanceID).
		First(&ci).Error)
	return ci
}

// seedReportingNodeUsage keeps the reconcile leg quiet: a fresh self-report in
// line with the provider figure.
func seedReportingNodeUsage(t *testing.T, ip string, used int64) {
	t.Helper()
	db.Get().Where("ipv4 = ?", ip).Delete(&NodeUsage{})
	require.NoError(t, db.Get().Create(&NodeUsage{
		Ipv4: ip, NodeID: 1, Epoch: 1, UsedBytes: used, QuotaTotalBytes: 1024 << 30,
		LastReportAt: time.Now().Unix(),
	}).Error)
	t.Cleanup(func() { db.Get().Where("ipv4 = ?", ip).Delete(&NodeUsage{}) })
}

func TestCheckAWSInstanceOverage_ThresholdsAndAutostop(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	viper.Set("cloud_instance.aws_overage_autostop", true)
	t.Cleanup(func() { viper.Set("cloud_instance.aws_overage_autostop", true) })

	const gib = int64(1) << 30
	monthEnd := currentMonthEndUTC()
	now := time.Now().Unix()
	ctx := context.Background()

	t.Run("80 percent warns once", func(t *testing.T) {
		p := &stopRecordingProvider{}
		ip, id := "203.0.113.30", "ovr-80"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, id, 850*gib, monthEnd, now)
		seedReportingNodeUsage(t, ip, 850*gib)

		st := awsStatus(id, ip, 850*gib, 1024*gib, monthEnd)
		checkAWSInstanceOverage(ctx, p, st)
		ci := loadCloudInstance(t, id)
		assert.Equal(t, monthEnd, ci.OverageWarn80SentResetAt, "80% warn marked for this cycle")
		assert.Zero(t, ci.OverageWarn95SentResetAt)
		assert.Zero(t, p.stopCount())

		checkAWSInstanceOverage(ctx, p, st) // second sync: dedup
		assert.Equal(t, monthEnd, loadCloudInstance(t, id).OverageWarn80SentResetAt)
	})

	t.Run("over 100 percent stops the instance once", func(t *testing.T) {
		p := &stopRecordingProvider{}
		ip, id := "203.0.113.31", "ovr-100"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, id, 1100*gib, monthEnd, now)
		seedReportingNodeUsage(t, ip, 1100*gib)

		st := awsStatus(id, ip, 1100*gib, 1024*gib, monthEnd)
		checkAWSInstanceOverage(ctx, p, st)
		assert.Equal(t, 1, p.stopCount(), "StopInstance fired")
		assert.Equal(t, monthEnd, loadCloudInstance(t, id).OverageStopSentResetAt)

		checkAWSInstanceOverage(ctx, p, st) // dedup: no second stop
		assert.Equal(t, 1, p.stopCount())
	})

	t.Run("stop failure is retried next sync", func(t *testing.T) {
		p := &stopRecordingProvider{stopFail: true}
		ip, id := "203.0.113.32", "ovr-fail"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, id, 1100*gib, monthEnd, now)
		seedReportingNodeUsage(t, ip, 1100*gib)

		st := awsStatus(id, ip, 1100*gib, 1024*gib, monthEnd)
		checkAWSInstanceOverage(ctx, p, st)
		assert.Zero(t, loadCloudInstance(t, id).OverageStopSentResetAt, "failed stop must NOT mark the dedup field")

		p.stopFail = false
		checkAWSInstanceOverage(ctx, p, st)
		assert.Equal(t, 1, p.stopCount())
		assert.Equal(t, monthEnd, loadCloudInstance(t, id).OverageStopSentResetAt)
	})

	t.Run("autostop disabled alerts without stopping", func(t *testing.T) {
		viper.Set("cloud_instance.aws_overage_autostop", false)
		t.Cleanup(func() { viper.Set("cloud_instance.aws_overage_autostop", true) })

		p := &stopRecordingProvider{}
		ip, id := "203.0.113.33", "ovr-off"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, id, 1100*gib, monthEnd, now)
		seedReportingNodeUsage(t, ip, 1100*gib)

		checkAWSInstanceOverage(ctx, p, awsStatus(id, ip, 1100*gib, 1024*gib, monthEnd))
		assert.Zero(t, p.stopCount())
		assert.Equal(t, monthEnd, loadCloudInstance(t, id).OverageStopSentResetAt, "alert still deduped per cycle")
	})

	t.Run("stopped instance is skipped entirely", func(t *testing.T) {
		p := &stopRecordingProvider{}
		ip, id := "203.0.113.34", "ovr-stopped"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, id, 1100*gib, monthEnd, now)

		st := awsStatus(id, ip, 1100*gib, 1024*gib, monthEnd)
		st.State = "stopped"
		checkAWSInstanceOverage(ctx, p, st)
		ci := loadCloudInstance(t, id)
		assert.Zero(t, p.stopCount())
		assert.Zero(t, ci.OverageStopSentResetAt)
		assert.Zero(t, ci.ReconcileAlertSentResetAt, "no reconcile noise for a parked instance")
	})
}

func TestReconcileAWSNodeUsage(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	const gib = int64(1) << 30
	monthEnd := currentMonthEndUTC()
	now := time.Now().Unix()
	ctx := context.Background()
	p := &stopRecordingProvider{}

	t.Run("missing node row alerts once", func(t *testing.T) {
		ip, id := "203.0.113.40", "rec-missing"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, id, 100*gib, monthEnd, now)
		db.Get().Where("ipv4 = ?", ip).Delete(&NodeUsage{})

		st := awsStatus(id, ip, 100*gib, 1024*gib, monthEnd)
		checkAWSInstanceOverage(ctx, p, st)
		assert.Equal(t, monthEnd, loadCloudInstance(t, id).ReconcileAlertSentResetAt)

		checkAWSInstanceOverage(ctx, p, st) // dedup
		assert.Equal(t, monthEnd, loadCloudInstance(t, id).ReconcileAlertSentResetAt)
	})

	t.Run("silent node alerts", func(t *testing.T) {
		ip, id := "203.0.113.41", "rec-silent"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, id, 100*gib, monthEnd, now)
		db.Get().Where("ipv4 = ?", ip).Delete(&NodeUsage{})
		require.NoError(t, db.Get().Create(&NodeUsage{
			Ipv4: ip, NodeID: 1, Epoch: 1, UsedBytes: 100 * gib, QuotaTotalBytes: 1024 * gib,
			LastReportAt: now - 3600, // an hour of silence
		}).Error)
		t.Cleanup(func() { db.Get().Where("ipv4 = ?", ip).Delete(&NodeUsage{}) })

		checkAWSInstanceOverage(ctx, p, awsStatus(id, ip, 100*gib, 1024*gib, monthEnd))
		assert.Equal(t, monthEnd, loadCloudInstance(t, id).ReconcileAlertSentResetAt)
	})

	t.Run("metering drift alerts", func(t *testing.T) {
		ip, id := "203.0.113.42", "rec-drift"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, id, 500*gib, monthEnd, now)
		seedReportingNodeUsage(t, ip, 300*gib) // provider 500 vs node 300: >10% and >20GiB

		checkAWSInstanceOverage(ctx, p, awsStatus(id, ip, 500*gib, 1024*gib, monthEnd))
		assert.Equal(t, monthEnd, loadCloudInstance(t, id).ReconcileAlertSentResetAt)
	})

	t.Run("healthy metering stays quiet", func(t *testing.T) {
		ip, id := "203.0.113.43", "rec-ok"
		seedCloudInstance(t, ip, cloudprovider.ProviderAWSLightsail, id, 500*gib, monthEnd, now)
		seedReportingNodeUsage(t, ip, 495*gib) // within margins

		checkAWSInstanceOverage(ctx, p, awsStatus(id, ip, 500*gib, 1024*gib, monthEnd))
		assert.Zero(t, loadCloudInstance(t, id).ReconcileAlertSentResetAt)
	})
}
