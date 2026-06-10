package center

import (
	"context"
	"testing"
	"time"

	db "github.com/wordgate/qtoolkit/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestEmitNodeProvisionJob proves the collapsed worker path: emitNodeProvisionJob
// gates the sub pending→provisioning and inserts exactly one queued NodeProvisionJob
// for an external agent to claim. Re-running is idempotent (one job row, no error).
func TestEmitNodeProvisionJob(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	ctx := context.Background()
	now := time.Now().Unix()
	stamp := time.Now().Format("20060102150405.000000")

	owner := User{UUID: "usr-pn-emit-" + stamp}
	require.NoError(t, db.Get().Create(&owner).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&owner) })

	plan := Plan{PID: "pn-emit-" + stamp, Kind: PlanKindPrivateNode, Month: 12}
	require.NoError(t, db.Get().Create(&plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&plan) })

	spec := PrivateNodePlanSpec{
		PlanID: plan.ID, Provider: "aws_lightsail", IPType: IPTypeNonResidential,
		AllowedRegions: `["japan"]`, ImageID: "ubuntu_22_04", BundleID: "nano_3_0",
		TrafficTotalBytes: 2 * 1024 * 1024 * 1024 * 1024,
	}
	require.NoError(t, db.Get().Create(&spec).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&spec) })

	order := Order{UUID: "ord-pn-emit-" + stamp, UserID: owner.ID, Meta: "{}"}
	require.NoError(t, db.Get().Create(&order).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&order) })

	sub, err := createPrivateNodeSubscription(ctx, db.Get(), &order, &plan, now)
	require.NoError(t, err)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub) })
	t.Cleanup(func() { db.Get().Unscoped().Where("sub_id = ?", sub.ID).Delete(&NodeProvisionJob{}) })

	// First emit: pending → provisioning, exactly one queued job.
	require.NoError(t, emitNodeProvisionJob(ctx, sub, &spec))

	var reloaded PrivateNodeSubscription
	require.NoError(t, db.Get().First(&reloaded, sub.ID).Error)
	assert.Equal(t, PNStatusProvisioning, reloaded.Status)

	var jobs []NodeProvisionJob
	require.NoError(t, db.Get().Where("sub_id = ?", sub.ID).Find(&jobs).Error)
	require.Len(t, jobs, 1, "exactly one provision job after first emit")
	assert.Equal(t, NPJStatusQueued, jobs[0].Status)
	assert.Equal(t, sub.ID, jobs[0].SubID)
	assert.Equal(t, spec.BundleID, jobs[0].BundleID)
	assert.Equal(t, spec.ImageID, jobs[0].ImageID)
	assert.Equal(t, "private", jobs[0].ComposeVariant)
	assert.Equal(t, sub.Region, jobs[0].Region)
	assert.Equal(t, sub.IPType, jobs[0].IPType)
	assert.Equal(t, sub.TrafficTotalBytes, jobs[0].TrafficTotalBytes)

	// Second emit on the same sub: idempotent, still exactly one job, no error.
	require.NoError(t, emitNodeProvisionJob(ctx, sub, &spec))

	var jobsAgain []NodeProvisionJob
	require.NoError(t, db.Get().Where("sub_id = ?", sub.ID).Find(&jobsAgain).Error)
	require.Len(t, jobsAgain, 1, "idempotent re-emit must NOT create a second job")
}
