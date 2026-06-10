package center

import (
	"context"
	"strings"
	"testing"
	"time"

	db "github.com/wordgate/qtoolkit/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleProvisionTimeoutSweep verifies that a subscription stuck in
// provisioning past the timeout cutoff is marked failed, while a fresh
// provisioning subscription is left untouched.
func TestHandleProvisionTimeoutSweep(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()

	// Stale sub: provisioning + old updated_at → should be marked failed.
	stale := PrivateNodeSubscription{
		UserID:      900001,
		PlanID:      900001,
		OrderID:     900001,
		Region:      "us-east-1",
		IPType:      IPTypeNonResidential,
		Status:      PNStatusProvisioning,
		PurchasedAt: now,
		ExpiresAt:   now + 86400,
	}
	require.NoError(t, db.Get().Create(&stale).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&PrivateNodeSubscription{}, stale.ID)
	})

	// Fresh sub: provisioning + recent updated_at → should NOT be touched.
	fresh := PrivateNodeSubscription{
		UserID:      900002,
		PlanID:      900002,
		OrderID:     900002,
		Region:      "us-east-1",
		IPType:      IPTypeNonResidential,
		Status:      PNStatusProvisioning,
		PurchasedAt: now,
		ExpiresAt:   now + 86400,
	}
	require.NoError(t, db.Get().Create(&fresh).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&PrivateNodeSubscription{}, fresh.ID)
	})

	// Force the stale sub's updated_at older than the cutoff (autoUpdateTime
	// would otherwise stamp it "now" on Create).
	require.NoError(t, db.Get().Model(&PrivateNodeSubscription{}).
		Where("id = ?", stale.ID).
		UpdateColumn("updated_at", now-provisionTimeoutSeconds-100).Error)

	require.NoError(t, handleProvisionTimeoutSweep(context.Background(), nil))

	var reloadedStale PrivateNodeSubscription
	require.NoError(t, db.Get().First(&reloadedStale, stale.ID).Error)
	assert.Equal(t, PNStatusFailed, reloadedStale.Status)
	assert.True(t, strings.Contains(reloadedStale.LastProvisionError, "timed out"),
		"expected timeout error, got %q", reloadedStale.LastProvisionError)

	var reloadedFresh PrivateNodeSubscription
	require.NoError(t, db.Get().First(&reloadedFresh, fresh.ID).Error)
	assert.Equal(t, PNStatusProvisioning, reloadedFresh.Status, "fresh sub must remain provisioning")
}

// TestProvisionTimeoutSweep_FailsOrphanJob verifies the sweep also fails the
// orphan NodeProvisionJob when it times out the owning subscription — otherwise
// the job stays claimable and an agent burns a VPS on a dead sub.
func TestProvisionTimeoutSweep_FailsOrphanJob(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()

	// Pre-purge any leftover rows for our sentinel order id (idempotent reruns).
	db.Get().Unscoped().Where("order_id = ?", 900003).Delete(&PrivateNodeSubscription{})

	sub := PrivateNodeSubscription{
		UserID:      900003,
		PlanID:      900003,
		OrderID:     900003,
		Region:      "us-east-1",
		IPType:      IPTypeNonResidential,
		Status:      PNStatusProvisioning,
		PurchasedAt: now,
		ExpiresAt:   now + 86400,
	}
	require.NoError(t, db.Get().Create(&sub).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&PrivateNodeSubscription{}, sub.ID)
	})

	job := NodeProvisionJob{
		SubID:             sub.ID,
		Status:            NPJStatusQueued,
		Region:            sub.Region,
		BundleID:          "nano_3_0",
		ImageID:           "ubuntu_22_04",
		ComposeVariant:    "private",
		TrafficTotalBytes: sub.TrafficTotalBytes,
		IPType:            sub.IPType,
	}
	require.NoError(t, db.Get().Create(&job).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&NodeProvisionJob{}, job.ID)
	})

	// Force updated_at older than the cutoff AFTER create (autoUpdateTime fights us).
	require.NoError(t, db.Get().Model(&PrivateNodeSubscription{}).
		Where("id = ?", sub.ID).
		UpdateColumn("updated_at", now-provisionTimeoutSeconds-100).Error)

	require.NoError(t, handleProvisionTimeoutSweep(context.Background(), nil))

	var reloadedSub PrivateNodeSubscription
	require.NoError(t, db.Get().First(&reloadedSub, sub.ID).Error)
	assert.Equal(t, PNStatusFailed, reloadedSub.Status, "stale sub must be failed")

	var reloadedJob NodeProvisionJob
	require.NoError(t, db.Get().First(&reloadedJob, job.ID).Error)
	assert.Equal(t, NPJStatusFailed, reloadedJob.Status, "orphan job must be failed")
	assert.True(t, strings.Contains(reloadedJob.LastError, "timed out"),
		"expected timeout error on job, got %q", reloadedJob.LastError)
}
