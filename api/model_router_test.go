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

func TestAdvanceRouterFulfillment_ExpiredRecoveredHardwareShipped(t *testing.T) {
	f, sub := routerFixture(RouterStageExpired, "redmi-ax6s")
	f.ShippedAt = 4000
	f.CredentialMintedAt = 3000
	sub.Status = PNStatusActive
	dev := &Device{IsGateway: true, TokenLastUsedAt: 5000}
	// 已发货过的硬件路由器：回收后不能落到 ready 等运维再标一次发货，同次推进直接回 online
	assert.True(t, advanceRouterFulfillment(f, sub, dev, 6000))
	assert.Equal(t, RouterStageOnline, f.Stage)
	assert.EqualValues(t, 5000, f.ActivatedAt)
}

func TestAdvanceRouterFulfillment_ExpiredRecoveredBYOOnline(t *testing.T) {
	f, sub := routerFixture(RouterStageExpired, "")
	f.CredentialMintedAt = 3000
	sub.Status = PNStatusActive
	dev := &Device{IsGateway: true, TokenLastUsedAt: 5000}
	assert.True(t, advanceRouterFulfillment(f, sub, dev, 6000))
	assert.Equal(t, RouterStageOnline, f.Stage)
	assert.EqualValues(t, 5000, f.ActivatedAt)
}

func TestAdvanceRouterFulfillment_GraceIsNotExpired(t *testing.T) {
	f, sub := routerFixture(RouterStageOnline, "")
	sub.Status = PNStatusGrace
	assert.False(t, advanceRouterFulfillment(f, sub, nil, 2000))
	assert.Equal(t, RouterStageOnline, f.Stage)
}

// 成品：技术员铸凭证后在仓库跑 k2r setup（设备活动晚于铸造），运营随后标发货 —— 发货前的活动不能让
// 台账跳 online；只有晚于发货时刻的活动才算。自备仍只比铸造时刻。
func TestAdvanceRouterFulfillment_HardwareOnlineThresholdIsMaxMintShipped(t *testing.T) {
	f, sub := routerFixture(RouterStageShipped, "redmi-ax6s")
	sub.Status = PNStatusActive
	f.CredentialMintedAt, f.ShippedAt = 3000, 5000
	dev := &Device{IsGateway: true, TokenLastUsedAt: 4000}
	assert.False(t, advanceRouterFulfillment(f, sub, dev, 6000), "activity between mint and ship is warehouse setup, not online")
	assert.Equal(t, RouterStageShipped, f.Stage)

	dev.TokenLastUsedAt = 5000
	assert.False(t, advanceRouterFulfillment(f, sub, dev, 6000), "must be strictly after ShippedAt")

	dev.TokenLastUsedAt = 5001
	assert.True(t, advanceRouterFulfillment(f, sub, dev, 6000))
	assert.Equal(t, RouterStageOnline, f.Stage)

	// BYO：门槛仍是铸造时刻（即使脏数据带了 ShippedAt 也不参与）
	b, bsub := routerFixture(RouterStageReady, "")
	bsub.Status = PNStatusActive
	b.CredentialMintedAt, b.ShippedAt = 3000, 5000
	assert.True(t, advanceRouterFulfillment(b, bsub, &Device{IsGateway: true, TokenLastUsedAt: 4000}, 6000))
	assert.Equal(t, RouterStageOnline, b.Stage)
}

// 续费继承的成品台账带 ShippedAt：线路激活时跳过 ready 直接 shipped；普通成品新单（ShippedAt==0）仍停 ready。
func TestAdvanceRouterFulfillment_InheritedShippedSkipsReady(t *testing.T) {
	f, sub := routerFixture(RouterStageProvisioning, "redmi-ax6s")
	f.ShippedAt, f.CredentialMintedAt = 4000, 3000
	sub.Status = PNStatusActive
	assert.True(t, advanceRouterFulfillment(f, sub, nil, 6000))
	assert.Equal(t, RouterStageShipped, f.Stage)

	g, gsub := routerFixture(RouterStagePaid, "redmi-ax6s")
	g.ShippedAt, g.CredentialMintedAt = 4000, 3000
	gsub.Status = PNStatusActive
	assert.True(t, advanceRouterFulfillment(g, gsub, &Device{IsGateway: true, TokenLastUsedAt: 4500}, 6000))
	assert.Equal(t, RouterStageOnline, g.Stage, "cascades to online when the device was seen after max(mint, shipped)")

	n, nsub := routerFixture(RouterStageProvisioning, "redmi-ax6s")
	nsub.Status = PNStatusActive
	assert.True(t, advanceRouterFulfillment(n, nsub, nil, 6000))
	assert.Equal(t, RouterStageReady, n.Stage, "fresh hardware order must wait for manual shipping")
}
