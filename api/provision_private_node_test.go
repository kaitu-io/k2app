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
