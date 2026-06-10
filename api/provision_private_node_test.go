package center

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	db "github.com/wordgate/qtoolkit/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kaitu-io/k2app/api/cloudprovider"
)

// fakeProvider implements cloudprovider.Provider for provision tests.
// It records the UserData passed to CreateInstance and reports a running
// instance with an IP from GetInstanceStatus.
type fakeProvider struct {
	name         string
	instanceID   string
	ipAddress    string
	lastUserData string
	created      bool
	createCount  int
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*cloudprovider.InstanceStatus, error) {
	if !f.created {
		return nil, fmt.Errorf("not found")
	}
	return &cloudprovider.InstanceStatus{
		InstanceID: f.instanceID,
		Name:       instanceID,
		IPAddress:  f.ipAddress,
		State:      "running",
	}, nil
}

func (f *fakeProvider) ListInstances(ctx context.Context) ([]*cloudprovider.InstanceStatus, error) {
	return nil, nil
}

func (f *fakeProvider) ChangeIP(ctx context.Context, instanceID string, opts cloudprovider.ChangeIPOptions) (*cloudprovider.OperationResult, error) {
	return nil, &cloudprovider.NotSupportedError{Provider: f.name, Operation: "ChangeIP"}
}

func (f *fakeProvider) CreateInstance(ctx context.Context, opts cloudprovider.CreateInstanceOptions) (*cloudprovider.OperationResult, error) {
	f.lastUserData = opts.UserData
	f.created = true
	f.createCount++
	return &cloudprovider.OperationResult{Success: true, Data: map[string]interface{}{"instance_id": f.instanceID}}, nil
}

func (f *fakeProvider) DeleteInstance(ctx context.Context, instanceID string) (*cloudprovider.OperationResult, error) {
	return &cloudprovider.OperationResult{Success: true}, nil
}

func (f *fakeProvider) ListRegions(ctx context.Context) ([]cloudprovider.RegionInfo, error) {
	return nil, nil
}

func (f *fakeProvider) ListPlans(ctx context.Context, region string) ([]cloudprovider.PlanInfo, error) {
	return nil, nil
}

func (f *fakeProvider) ListImages(ctx context.Context, region string) ([]cloudprovider.ImageInfo, error) {
	return nil, nil
}

func TestRenderProvisionUserData(t *testing.T) {
	ud := renderProvisionUserData(provisionParams{
		NodeSecret: "node-secret-xyz",
		ClaimToken: "claim-abc",
		CenterURL:  "https://k2.example.com",
		Domain:     "*.1-2-3-4.sslip.io",
	})
	require.NotEmpty(t, ud)
	assert.Contains(t, ud, "K2_NODE_SECRET=node-secret-xyz")
	assert.Contains(t, ud, "K2_PRIVATE_CLAIM=claim-abc")
	assert.Contains(t, ud, "K2_CENTER_URL=https://k2.example.com")
	assert.Contains(t, ud, "K2_DOMAIN=*.1-2-3-4.sslip.io")
	assert.True(t, strings.HasPrefix(ud, "#!") || strings.HasPrefix(ud, "#cloud-config"),
		"user-data must be a runnable bootstrap")
}

func TestCreatePrivateNodeSubscription(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	now := time.Now().Unix()

	owner := User{UUID: "usr-pn-create-" + time.Now().Format("20060102150405.000000")}
	require.NoError(t, db.Get().Create(&owner).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&owner) })

	plan := Plan{PID: "pn-test-" + time.Now().Format("150405.000000"), Kind: PlanKindPrivateNode, Month: 12}
	require.NoError(t, db.Get().Create(&plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&plan) })

	spec := PrivateNodePlanSpec{
		PlanID: plan.ID, Provider: "aws_lightsail", IPType: IPTypeNonResidential,
		AllowedRegions: `["japan"]`, ImageID: "ubuntu_22_04", BundleID: "nano_3_0",
		TrafficTotalBytes: 2 * 1024 * 1024 * 1024 * 1024,
	}
	require.NoError(t, db.Get().Create(&spec).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&spec) })

	order := Order{UUID: "ord-pn-" + time.Now().Format("150405.000000"), UserID: owner.ID, Meta: "{}"}
	require.NoError(t, db.Get().Create(&order).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&order) })

	sub, err := createPrivateNodeSubscription(context.Background(), db.Get(), &order, &plan, now)
	require.NoError(t, err)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub) })

	assert.Equal(t, PNStatusPending, sub.Status)
	assert.Equal(t, owner.ID, sub.UserID)
	assert.Equal(t, order.ID, sub.OrderID)
	assert.Equal(t, IPTypeNonResidential, sub.IPType)
	assert.Equal(t, "japan", sub.Region)
	assert.Equal(t, spec.TrafficTotalBytes, sub.TrafficTotalBytes)
	assert.NotEmpty(t, sub.ProvisionClaimToken)
	assert.Greater(t, sub.ExpiresAt, now)
}

func TestProvisionPrivateNode_HappyPath(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ctx := context.Background()
	now := time.Now().Unix()
	stamp := time.Now().Format("20060102150405.000000")

	owner := User{UUID: "usr-pn-prov-" + stamp}
	require.NoError(t, db.Get().Create(&owner).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&owner) })

	plan := Plan{PID: "pn-prov-" + stamp, Kind: PlanKindPrivateNode, Month: 12}
	require.NoError(t, db.Get().Create(&plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&plan) })

	spec := PrivateNodePlanSpec{
		PlanID: plan.ID, Provider: "aws_lightsail", IPType: IPTypeNonResidential,
		AllowedRegions: `["japan"]`, ImageID: "ubuntu_22_04", BundleID: "nano_3_0",
		TrafficTotalBytes: 2 * 1024 * 1024 * 1024 * 1024,
	}
	require.NoError(t, db.Get().Create(&spec).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&spec) })

	order := Order{UUID: "ord-pn-prov-" + stamp, UserID: owner.ID, Meta: "{}"}
	require.NoError(t, db.Get().Create(&order).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&order) })

	sub, err := createPrivateNodeSubscription(ctx, db.Get(), &order, &plan, now)
	require.NoError(t, err)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub) })

	account := CloudInstanceAccount{Name: "aws_lightsail", Provider: "aws_lightsail"}
	instID := "pn-prov-inst-" + stamp
	fake := &fakeProvider{name: "aws_lightsail", instanceID: instID, ipAddress: "203.0.113.7"}
	t.Cleanup(func() {
		db.Get().Unscoped().Where("provider = ? AND instance_id = ?", account.Provider, instID).Delete(&CloudInstance{})
	})

	require.NoError(t, provisionPrivateNode(ctx, sub, &spec, account, fake))

	// status stays provisioning (NOT active) — node self-registers later
	var reloaded PrivateNodeSubscription
	require.NoError(t, db.Get().First(&reloaded, sub.ID).Error)
	assert.Equal(t, PNStatusProvisioning, reloaded.Status)
	require.NotNil(t, reloaded.CloudInstanceID)

	var ci CloudInstance
	require.NoError(t, db.Get().Where("provider = ? AND instance_id = ?", account.Provider, instID).First(&ci).Error)
	assert.Equal(t, spec.TrafficTotalBytes, ci.TrafficTotalBytes)
	assert.Equal(t, *reloaded.CloudInstanceID, ci.ID)

	assert.Contains(t, fake.lastUserData, sub.ProvisionClaimToken)
	assert.Contains(t, fake.lastUserData, "K2_NODE_SECRET=")
}

// TestProvisionPrivateNode_RetryIdempotent proves that re-running provision on a
// sub already in `provisioning` (Asynq retry after a partial failure) resumes
// instead of stranding the sub or double-creating the VPS.
func TestProvisionPrivateNode_RetryIdempotent(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ctx := context.Background()
	now := time.Now().Unix()
	stamp := time.Now().Format("20060102150405.000000")

	owner := User{UUID: "usr-pn-retry-" + stamp}
	require.NoError(t, db.Get().Create(&owner).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&owner) })

	plan := Plan{PID: "pn-retry-" + stamp, Kind: PlanKindPrivateNode, Month: 12}
	require.NoError(t, db.Get().Create(&plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&plan) })

	spec := PrivateNodePlanSpec{
		PlanID: plan.ID, Provider: "aws_lightsail", IPType: IPTypeNonResidential,
		AllowedRegions: `["japan"]`, ImageID: "ubuntu_22_04", BundleID: "nano_3_0",
		TrafficTotalBytes: 2 * 1024 * 1024 * 1024 * 1024,
	}
	require.NoError(t, db.Get().Create(&spec).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&spec) })

	order := Order{UUID: "ord-pn-retry-" + stamp, UserID: owner.ID, Meta: "{}"}
	require.NoError(t, db.Get().Create(&order).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&order) })

	sub, err := createPrivateNodeSubscription(ctx, db.Get(), &order, &plan, now)
	require.NoError(t, err)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub) })

	account := CloudInstanceAccount{Name: "aws_lightsail", Provider: "aws_lightsail"}
	instID := "pn-retry-inst-" + stamp
	fake := &fakeProvider{name: "aws_lightsail", instanceID: instID, ipAddress: "203.0.113.9"}
	t.Cleanup(func() {
		db.Get().Unscoped().Where("provider = ? AND instance_id = ?", account.Provider, instID).Delete(&CloudInstance{})
	})

	// First run: pending → provisioning, instance created exactly once.
	require.NoError(t, provisionPrivateNode(ctx, sub, &spec, account, fake))
	assert.Equal(t, 1, fake.createCount, "first run must create exactly one instance")

	// Second run on the SAME sub (now provisioning): must resume, not double-create.
	require.NoError(t, provisionPrivateNode(ctx, sub, &spec, account, fake))
	assert.Equal(t, 1, fake.createCount, "retry must NOT create a second instance")

	var reloaded PrivateNodeSubscription
	require.NoError(t, db.Get().First(&reloaded, sub.ID).Error)
	assert.Equal(t, PNStatusProvisioning, reloaded.Status)
	require.NotNil(t, reloaded.CloudInstanceID, "retry must still link the cloud instance")
}
