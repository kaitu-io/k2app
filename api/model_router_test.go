package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func routerFixture(stage, sku string) (*RouterFulfillment, *PrivateNodeSubscription) {
	f := &RouterFulfillment{ID: 1, OrderID: 10, UserID: 7, SubID: 3, HardwareSKU: sku, Stage: stage, CreatedAt: 1000}
	sub := &PrivateNodeSubscription{ID: 3, UserID: 7, Status: PNStatusPending, ExpiresAt: 9_000_000}
	return f, sub
}

func TestAdvanceRouterFulfillment_PaidToProvisioningToReady(t *testing.T) {
	f, sub := routerFixture(RouterStagePaid, "redmi-ax6s")
	assert.False(t, advanceRouterFulfillment(f, sub, nil, 2000), "pending sub: stay paid")
	assert.Equal(t, RouterStagePaid, f.Stage)

	sub.Status = PNStatusProvisioning
	assert.True(t, advanceRouterFulfillment(f, sub, nil, 2000))
	assert.Equal(t, RouterStageProvisioning, f.Stage)

	sub.Status = PNStatusActive
	assert.True(t, advanceRouterFulfillment(f, sub, nil, 2000))
	assert.Equal(t, RouterStageReady, f.Stage)
}

func TestAdvanceRouterFulfillment_PaidJumpsToReadyWhenAlreadyActive(t *testing.T) {
	f, sub := routerFixture(RouterStagePaid, "redmi-ax6s")
	sub.Status = PNStatusActive
	assert.True(t, advanceRouterFulfillment(f, sub, nil, 2000))
	assert.Equal(t, RouterStageReady, f.Stage)
}

func TestAdvanceRouterFulfillment_HardwareNeedsShippedBeforeOnline(t *testing.T) {
	f, sub := routerFixture(RouterStageReady, "redmi-ax6s")
	sub.Status = PNStatusActive
	f.CredentialMintedAt = 3000
	dev := &Device{IsGateway: true, TokenLastUsedAt: 5000}
	// 成品：未标记发货前，即使设备已上线也不跳 online（运维还没寄出，说明是烧录测试）
	assert.False(t, advanceRouterFulfillment(f, sub, dev, 6000))
	assert.Equal(t, RouterStageReady, f.Stage)

	f.Stage, f.ShippedAt = RouterStageShipped, 4000
	assert.True(t, advanceRouterFulfillment(f, sub, dev, 6000))
	assert.Equal(t, RouterStageOnline, f.Stage)
	assert.EqualValues(t, 5000, f.ActivatedAt)
}

func TestAdvanceRouterFulfillment_BYOReadyToOnlineAfterMint(t *testing.T) {
	f, sub := routerFixture(RouterStageReady, "")
	sub.Status = PNStatusActive
	f.CredentialMintedAt = 3000
	// 设备最近活动 == 铸造时刻（创建即写入），还不算上线
	dev := &Device{IsGateway: true, TokenLastUsedAt: 3000}
	assert.False(t, advanceRouterFulfillment(f, sub, dev, 4000))
	dev.TokenLastUsedAt = 3600
	assert.True(t, advanceRouterFulfillment(f, sub, dev, 4000))
	assert.Equal(t, RouterStageOnline, f.Stage)
}

func TestAdvanceRouterFulfillment_ExpiredAndRecovered(t *testing.T) {
	f, sub := routerFixture(RouterStageOnline, "redmi-ax6s")
	sub.Status = PNStatusSuspended
	assert.True(t, advanceRouterFulfillment(f, sub, nil, 2000))
	assert.Equal(t, RouterStageExpired, f.Stage)

	// 续费后 sub 回到 active（sweep 回收），台账回到 ready，等设备再次上线
	sub.Status = PNStatusActive
	assert.True(t, advanceRouterFulfillment(f, sub, nil, 2000))
	assert.Equal(t, RouterStageReady, f.Stage)
}

func TestAdvanceRouterFulfillment_GraceIsNotExpired(t *testing.T) {
	f, sub := routerFixture(RouterStageOnline, "")
	sub.Status = PNStatusGrace
	assert.False(t, advanceRouterFulfillment(f, sub, nil, 2000))
	assert.Equal(t, RouterStageOnline, f.Stage)
}
