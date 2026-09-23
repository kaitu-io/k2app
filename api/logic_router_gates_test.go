package center

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 路由器版下单门的"产品归属"回归测试。
//
// 背景：这三条门（userHasRouter / userCanBuyRouterService / findRenewablePrivateLine）曾以
// "该用户有任意一条可续专属线路"为判据，于是把**定制线路**（Product=private_node，pn-* 套餐）
// 客户误判成路由器车主，造成两个方向的错判，都在本地 UAT 里实测复现过：
//   ① 定制线路客户能买 router-svc-1y（便宜的续费套餐），并真的把定制线路延了一年；
//   ② 同一个客户想买真路由器（router-std-1y）反而被拒，理由是"该账户已有开途路由器"。
// 判据已收敛为"线路挂了 RouterFulfillment"（routerLineIDs）—— 台账与线路由 applyRouterOrder
// 在同一事务建出，所以台账存在性等价于"这条线路是路由器版的"。

// gateSeedSeq 给 seed 出来的行分配唯一 order_id —— private_node_subscriptions.order_id 与
// router_fulfillments.order_id 都是 uniqueIndex，多条 seed 共用 0 会撞唯一键。
var gateSeedSeq atomic.Uint64

// gatePID 生成 <=30 字符的唯一套餐标识（plans.pid 是 varchar(30) uniqueIndex）。
func gatePID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%1_000_000_000)
}

// seedLine 落一条线路；withFulfillment=true 时同时挂一条路由器台账（= 路由器版线路）。
func seedLine(t *testing.T, userID uint64, planID uint64, status string, expiresAt int64, withFulfillment bool, sku string) *PrivateNodeSubscription {
	t.Helper()
	seq := 800000000 + gateSeedSeq.Add(1)
	sub := &PrivateNodeSubscription{UserID: userID, PlanID: planID, Status: status, OrderID: seq,
		Region: "japan", IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40,
		ProvisionClaimToken: generateId("gate-claim"),
		PurchasedAt:         time.Now().Unix(), ExpiresAt: expiresAt}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub) })
	if withFulfillment {
		f := &RouterFulfillment{OrderID: seq, UserID: userID, SubID: sub.ID,
			HardwareSKU: sku, Stage: RouterStageReady, UpdatedBy: "test"}
		require.NoError(t, db.Get().Create(f).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(f) })
	}
	return sub
}

// TestRouterGates_PrivateNodeCustomerIsNotRouterOwner 定制线路客户不得被当成路由器车主。
func TestRouterGates_PrivateNodeCustomerIsNotRouterOwner(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	user := CreateTestUser(t)
	pnPlan := &Plan{PID: gatePID("pn-gate"), Label: "定制线路", Price: 59900, OriginPrice: 59900,
		Month: 12, Tier: TierBasic, Product: ProductPrivateNode, IsActive: BoolPtr(false),
		Highlight: BoolPtr(false), Brand: "kaitu"}
	require.NoError(t, db.Get().Create(pnPlan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(pnPlan) })

	now := time.Now().Unix()
	// 活跃的定制线路，**没有**任何路由器台账。
	seedLine(t, user.ID, pnPlan.ID, PNStatusActive, now+300*86400, false, "")

	has, err := userHasRouter(context.Background(), db.Get(), user.ID, now)
	require.NoError(t, err)
	assert.False(t, has, "定制线路客户不是路由器车主：否则他买真路由器会被『已有开途路由器』挡掉")

	canSvc, err := userCanBuyRouterService(context.Background(), db.Get(), user.ID, now)
	require.NoError(t, err)
	assert.False(t, canSvc, "定制线路客户不能买路由器续费套餐：那会用 $299 续掉一条 $599 的线路")

	line, err := findRenewablePrivateLine(db.Get(), user.ID)
	require.NoError(t, err)
	assert.Nil(t, line, "续费套餐绝不能把定制线路当成可续目标")
}

// TestRouterGates_RouterOwnerRecognized 路由器车主（线路 + 台账）必须被认出来。
func TestRouterGates_RouterOwnerRecognized(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	user := CreateTestUser(t)
	plan := seedRouterPlan(t, gatePID("rt-gate-hw"), "redmi-ax6s", 39900)
	now := time.Now().Unix()
	sub := seedLine(t, user.ID, plan.ID, PNStatusActive, now+200*86400, true, "redmi-ax6s")

	has, err := userHasRouter(context.Background(), db.Get(), user.ID, now)
	require.NoError(t, err)
	assert.True(t, has, "有台账的活跃线路 = 路由器车主")

	canSvc, err := userCanBuyRouterService(context.Background(), db.Get(), user.ID, now)
	require.NoError(t, err)
	assert.True(t, canSvc, "路由器车主可以买续费套餐")

	line, err := findRenewablePrivateLine(db.Get(), user.ID)
	require.NoError(t, err)
	require.NotNil(t, line)
	assert.Equal(t, sub.ID, line.ID)
}

// TestRouterGates_RenewalPicksRouterLineNotLaterPrivateNodeLine 同时持有两条线路时，
// 续费必须延**路由器那条**，即使定制线路到期更晚（旧实现按 expires_at DESC 会选错）。
func TestRouterGates_RenewalPicksRouterLineNotLaterPrivateNodeLine(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	user := CreateTestUser(t)
	rtPlan := seedRouterPlan(t, gatePID("rt-gate-mix"), "redmi-ax6s", 39900)
	pnPlan := &Plan{PID: gatePID("pn-gate-mix"), Label: "定制线路", Price: 59900, OriginPrice: 59900,
		Month: 12, Tier: TierBasic, Product: ProductPrivateNode, IsActive: BoolPtr(false),
		Highlight: BoolPtr(false), Brand: "kaitu"}
	require.NoError(t, db.Get().Create(pnPlan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(pnPlan) })

	now := time.Now().Unix()
	routerLine := seedLine(t, user.ID, rtPlan.ID, PNStatusActive, now+100*86400, true, "redmi-ax6s")
	pnLine := seedLine(t, user.ID, pnPlan.ID, PNStatusActive, now+900*86400, false, "") // 到期更晚

	line, err := findRenewablePrivateLine(db.Get(), user.ID)
	require.NoError(t, err)
	require.NotNil(t, line)
	assert.Equal(t, routerLine.ID, line.ID,
		"必须延路由器线路 %d，而不是到期更晚的定制线路 %d", routerLine.ID, pnLine.ID)
}
