package center

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func seedReadyRouterFulfillment(t *testing.T, user *User) (*PrivateNodeSubscription, *RouterFulfillment) {
	t.Helper()
	now := time.Now().Unix()
	sub := &PrivateNodeSubscription{UserID: user.ID, PlanID: 1, OrderID: 800000 + user.ID, Region: "japan",
		IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40, Status: PNStatusActive,
		PurchasedAt: now, ExpiresAt: now + 300*86400}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub) })
	f := &RouterFulfillment{OrderID: sub.OrderID, UserID: user.ID, SubID: sub.ID, Stage: RouterStageReady}
	require.NoError(t, db.Get().Create(f).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Delete(f)
		db.Get().Unscoped().Where("user_id = ? AND is_gateway = ?", user.ID, true).Delete(&Device{})
	})
	return sub, f
}

func TestMintGatewayCredential_AttachesToFulfillment(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	_, f := seedReadyRouterFulfillment(t, user)

	url, devID, err := mintGatewayCredential(context.Background(), user)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(url, "k2subs://router-"), url)
	assert.NotZero(t, devID)

	var got RouterFulfillment
	require.NoError(t, db.Get().First(&got, f.ID).Error)
	require.NotNil(t, got.GatewayDeviceID)
	assert.Equal(t, devID, *got.GatewayDeviceID)
	assert.NotZero(t, got.CredentialMintedAt)

	// 重复铸造 = 轮换：旧设备删除，台账指向新设备
	url2, devID2, err := mintGatewayCredential(context.Background(), user)
	require.NoError(t, err)
	assert.NotEqual(t, url, url2)
	var cnt int64
	db.Get().Model(&Device{}).Where("user_id = ? AND is_gateway = ?", user.ID, true).Count(&cnt)
	assert.EqualValues(t, 1, cnt)
	require.NoError(t, db.Get().First(&got, f.ID).Error)
	assert.Equal(t, devID2, *got.GatewayDeviceID)
}

func TestMintGatewayCredential_NoLineRejected(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	_, _, err := mintGatewayCredential(context.Background(), user)
	require.Error(t, err)
	re, isBiz := err.(rerr)
	require.True(t, isBiz, "no active line must surface as a business error, got %v", err)
	assert.Equal(t, ErrorPlanNoRouter, re.code)
}

// 一用户可能有多条台账（每次买硬件套餐都建新台账）；attachCredentialToFulfillment 只能关联
// 最新一条，否则旧台账（没接过网）会被 syncRouterFulfillment 误判上线。
func TestAttachCredentialToFulfillment_NewestOnly(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	_, older := seedReadyRouterFulfillment(t, user)

	now := time.Now().Unix()
	sub2 := &PrivateNodeSubscription{UserID: user.ID, PlanID: 1, OrderID: 810000 + user.ID, Region: "japan",
		IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40, Status: PNStatusActive,
		PurchasedAt: now, ExpiresAt: now + 300*86400}
	require.NoError(t, db.Get().Create(sub2).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub2) })
	newer := &RouterFulfillment{OrderID: sub2.OrderID, UserID: user.ID, SubID: sub2.ID, Stage: RouterStageReady}
	require.NoError(t, db.Get().Create(newer).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(newer) })
	require.Greater(t, newer.ID, older.ID)

	const devID uint64 = 424242
	require.NoError(t, attachCredentialToFulfillment(db.Get(), user.ID, devID, now))

	var gotNewer, gotOlder RouterFulfillment
	require.NoError(t, db.Get().First(&gotNewer, newer.ID).Error)
	require.NoError(t, db.Get().First(&gotOlder, older.ID).Error)

	require.NotNil(t, gotNewer.GatewayDeviceID)
	assert.EqualValues(t, devID, *gotNewer.GatewayDeviceID)
	assert.Equal(t, now, gotNewer.CredentialMintedAt)

	assert.Nil(t, gotOlder.GatewayDeviceID)
	assert.Zero(t, gotOlder.CredentialMintedAt)
}

func TestTouchGatewayDeviceSeen(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	dev := &Device{UDID: newRouterUDID(), UserID: user.ID, IsGateway: true, AppPlatform: "router",
		TokenIssueAt: 1000, TokenLastUsedAt: 1000, TunnelIssueAt: 1000}
	require.NoError(t, db.Get().Create(dev).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(dev) })

	touchGatewayDeviceSeen(context.Background(), dev, 5000)
	var got Device
	require.NoError(t, db.Get().First(&got, dev.ID).Error)
	assert.EqualValues(t, 5000, got.TokenLastUsedAt)

	// 非网关设备不写（App 设备的 last-used 语义由别处维护）
	app := &Device{UDID: "app-" + newRouterUDID(), UserID: user.ID, IsGateway: false, TokenIssueAt: 1000, TokenLastUsedAt: 1000}
	require.NoError(t, db.Get().Create(app).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(app) })
	touchGatewayDeviceSeen(context.Background(), app, 5000)
	// Fresh destination var: reusing `got` (its primary key already set from the
	// query above) would make GORM AND-in the stale id alongside app.ID.
	var gotApp Device
	require.NoError(t, db.Get().First(&gotApp, app.ID).Error)
	assert.EqualValues(t, 1000, gotApp.TokenLastUsedAt)
}
