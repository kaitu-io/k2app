package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func postOrder(t *testing.T, r *gin.Engine, userID uint64, body map[string]any) (code float64, raw string) {
	t.Helper()
	token := GenerateTestToken(userID, "", time.Hour)
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req, _ := http.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var env struct {
		Code float64 `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	return env.Code, w.Body.String()
}

func TestCreateOrder_RouterHardwareRequiresShipping(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	r := orderRegionTestRouter()
	plan := seedRouterPlan(t, "router-test-hw", "redmi-ax6s", 39900)
	user := CreateTestUser(t)

	// preview 不强制收货信息（页面先报价再填地址）
	code, _ := postOrder(t, r, user.ID, map[string]any{"preview": true, "plan": plan.PID, "region": "japan"})
	assert.EqualValues(t, 0, code)

	// 真实下单缺收货信息 → 422
	code, _ = postOrder(t, r, user.ID, map[string]any{"preview": false, "plan": plan.PID, "region": "japan"})
	assert.EqualValues(t, ErrorInvalidArgument, code)
	assert.Nil(t, latestOrderForUser(t, user.ID), "rejected order must not persist")

	// 带收货信息：订单行落库并带 shipping + region（WordGate 不可达无妨，行在 wordgate 之前建）
	postOrder(t, r, user.ID, map[string]any{"preview": false, "plan": plan.PID, "region": "japan",
		"shipping": map[string]any{"name": "张三", "phone": "13800000000", "address": "上海市…"}})
	o := latestOrderForUser(t, user.ID)
	require.NotNil(t, o)
	t.Cleanup(func() { db.Get().Unscoped().Delete(o) })
	assert.Equal(t, "japan", o.PrivateNodeRegion)
	s := o.GetRouterShipping()
	require.NotNil(t, s)
	assert.Equal(t, "张三", s.Name)
	assert.Equal(t, "13800000000", s.Phone)
}

func TestCreateOrder_RouterServiceIgnoresShippingAndSkipsTierGate(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	r := orderRegionTestRouter()
	plan := seedRouterPlan(t, "router-test-svc", "", 29900)
	// 老客户：已首单、tier=family。App 续费会被 TierMismatch 拦；路由器版与 tier 无关，必须放行。
	user := CreateTestUser(t)
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", user.ID).
		Updates(map[string]any{"is_first_order_done": true, "tier": TierFamily}).Error)
	// 服务套餐仅续费：给该用户一条可续的**路由器版**线路，让它过服务套餐门（门本身见
	// TestCreateOrder_RouterServiceRenewalOnly）。线路必须连带台账 —— 台账是"这条线路属于路由器版"
	// 的唯一判据（routerLineIDs），只建线路等于造了一条定制线路，过不了门。
	now := time.Now().Unix()
	line := &PrivateNodeSubscription{UserID: user.ID, PlanID: plan.ID, OrderID: 710000 + user.ID,
		Region: "japan", IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40, Status: PNStatusActive,
		PurchasedAt: now - 10*86400, ExpiresAt: now + 300*86400}
	require.NoError(t, db.Get().Create(line).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(line) })
	lineFul := &RouterFulfillment{OrderID: line.OrderID, UserID: user.ID, SubID: line.ID,
		HardwareSKU: "redmi-ax6s", Stage: RouterStageOnline, ShippedAt: now - 9*86400}
	require.NoError(t, db.Get().Create(lineFul).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(lineFul) })

	postOrder(t, r, user.ID, map[string]any{"preview": false, "plan": plan.PID, "region": "japan"})
	o := latestOrderForUser(t, user.ID)
	require.NotNil(t, o, "service plan must not require shipping and must bypass tier gate")
	t.Cleanup(func() { db.Get().Unscoped().Delete(o) })
	assert.Nil(t, o.GetRouterShipping())

	// 服务套餐即使请求里带了 shipping，也不落库（HardwareSKU=="" 时收货信息门完全不生效）。
	postOrder(t, r, user.ID, map[string]any{"preview": false, "plan": plan.PID, "region": "japan",
		"shipping": map[string]any{"name": "李四", "phone": "13900000000", "address": "北京市…"}})
	o2 := latestOrderForUser(t, user.ID)
	require.NotNil(t, o2)
	t.Cleanup(func() { db.Get().Unscoped().Delete(o2) })
	assert.Nil(t, o2.GetRouterShipping(), "service plan must ignore shipping in the request body, never persist it")
}

// TestOrderRouterShipping_SaveRoundTrip covers the Save() path specifically — api_create_order's
// wordgate flow calls tx.Save(order) (full-struct UPDATE, Selects("*")) after wordgate order
// creation, not just Create(). *string is required so a nil shipping still serializes to SQL
// NULL under Save, not the empty string that trips MariaDB/MySQL's json_valid CHECK on a
// type:json column.
func TestOrderRouterShipping_SaveRoundTrip(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	user := CreateTestUser(t)

	// Case 1: no shipping (RouterShipping nil) — Create then Save must both succeed, and the
	// round-tripped row must report no shipping.
	// Meta populated via SetOrderMeta like production (api_create_order always does this before
	// Create): Meta is also a type:json column with no default, an empty "" would trip the same
	// json_valid CHECK — orthogonal to what this test covers, so avoid it rather than test it.
	o := &Order{
		UUID: generateId("ord"), Title: "路由器版服务测试", OriginAmount: 29900, PayAmount: 29900,
		UserID: user.ID,
	}
	require.NoError(t, o.SetOrderMeta(nil, nil, nil, true))
	require.NoError(t, db.Get().Create(o).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(o) })
	require.NoError(t, db.Get().Save(o).Error, "Save must not trip the json_valid CHECK on a nil RouterShipping")

	var reloaded Order
	require.NoError(t, db.Get().First(&reloaded, o.ID).Error)
	assert.Nil(t, reloaded.GetRouterShipping())

	// Case 2: with shipping — Create then Save must both succeed and round-trip the JSON.
	shipping := RouterShipping{Name: "王五", Phone: "13700000000", Address: "广州市…"}
	b, err := json.Marshal(shipping)
	require.NoError(t, err)
	shippingJSON := string(b)
	o2 := &Order{
		UUID: generateId("ord"), Title: "路由器版硬件测试", OriginAmount: 39900, PayAmount: 39900,
		UserID: user.ID, RouterShipping: &shippingJSON,
	}
	require.NoError(t, o2.SetOrderMeta(nil, nil, nil, true))
	require.NoError(t, db.Get().Create(o2).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(o2) })
	require.NoError(t, db.Get().Save(o2).Error, "Save must not trip the json_valid CHECK on a populated RouterShipping")

	var reloaded2 Order
	require.NoError(t, db.Get().First(&reloaded2, o2.ID).Error)
	s := reloaded2.GetRouterShipping()
	require.NotNil(t, s)
	assert.Equal(t, "王五", s.Name)
	assert.Equal(t, "13700000000", s.Phone)
	assert.Equal(t, "广州市…", s.Address)
}

// 一户一台：真实下单硬件套餐时，已有未过期台账或可续线路 → ErrorInvalidOperation；预览放行；
// 只剩 expired 台账且无可续线路可以再买；服务套餐不受此门影响（它有自己的仅续费门）。
func TestCreateOrder_RouterHardwareOnePerAccount(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	r := orderRegionTestRouter()
	hw := seedRouterPlan(t, "router-test-one-hw", "redmi-ax6s", 39900)
	svc := seedRouterPlan(t, "router-test-one-svc", "", 29900)
	shipping := map[string]any{"name": "张三", "phone": "13800000000", "address": "上海市…"}
	hwOrder := map[string]any{"preview": false, "plan": hw.PID, "region": "japan", "shipping": shipping}
	now := time.Now().Unix()

	mkLine := func(t *testing.T, userID uint64, status string, orderID uint64) *PrivateNodeSubscription {
		t.Helper()
		s := &PrivateNodeSubscription{UserID: userID, PlanID: hw.ID, OrderID: orderID, Region: "japan",
			IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40, Status: status,
			PurchasedAt: now - 10*86400, ExpiresAt: now + 300*86400}
		require.NoError(t, db.Get().Create(s).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(s) })
		return s
	}
	cleanupOrders := func(t *testing.T, userID uint64) {
		t.Cleanup(func() { db.Get().Unscoped().Where("user_id = ?", userID).Delete(&Order{}) })
	}

	t.Run("ready fulfillment blocks hardware order", func(t *testing.T) {
		user := CreateTestUser(t)
		cleanupOrders(t, user.ID)
		_, _ = seedReadyRouterFulfillment(t, user)

		code, _ := postOrder(t, r, user.ID, hwOrder)
		assert.EqualValues(t, ErrorInvalidOperation, code)
		assert.Nil(t, latestOrderForUser(t, user.ID), "rejected order must not persist")

		// 预览照常报价
		code, _ = postOrder(t, r, user.ID, map[string]any{"preview": true, "plan": hw.PID, "region": "japan"})
		assert.EqualValues(t, 0, code)

		// 服务套餐不受此门影响
		code, _ = postOrder(t, r, user.ID, map[string]any{"preview": false, "plan": svc.PID, "region": "japan"})
		assert.NotEqualValues(t, ErrorInvalidOperation, code)
		assert.NotNil(t, latestOrderForUser(t, user.ID), "service plan order must persist")
	})

	t.Run("paid fulfillment on a not-yet-renewable pending line blocks hardware order", func(t *testing.T) {
		// 隔离台账门：线路 pending 不在可续集合内，只能靠 stage≠expired 的台账拦下
		user := CreateTestUser(t)
		cleanupOrders(t, user.ID)
		pending := mkLine(t, user.ID, PNStatusPending, 770000+user.ID)
		f := &RouterFulfillment{OrderID: pending.OrderID, UserID: user.ID, SubID: pending.ID,
			HardwareSKU: "redmi-ax6s", Stage: RouterStagePaid}
		require.NoError(t, db.Get().Create(f).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(f) })

		code, _ := postOrder(t, r, user.ID, hwOrder)
		assert.EqualValues(t, ErrorInvalidOperation, code)
		assert.Nil(t, latestOrderForUser(t, user.ID))
	})

	// 无台账的可续线路 = 定制线路（Product=private_node），不是路由器 —— 绝不能用"已有路由器"
	// 把这位客户挡在路由器版之外。本地 UAT 实测过这个误判：定制线路客户买 router-std-1y 被拒，
	// 提示"该账户已有开途路由器"。判据是 routerLineIDs（线路是否挂台账），不是"有没有专属线路"。
	t.Run("renewable line without ledger row (定制线路) does not block hardware order", func(t *testing.T) {
		user := CreateTestUser(t)
		cleanupOrders(t, user.ID)
		mkLine(t, user.ID, PNStatusActive, 760000+user.ID)
		code, _ := postOrder(t, r, user.ID, hwOrder)
		// 与兄弟子测试同一口径：只断言"这道门没触发"。本测试路由不接 NextPay，订单落库后
		// 建 checkout 会失败（ErrorSystemError），断言 code==0 会锚到与本门无关的下游形态上。
		assert.NotEqualValues(t, ErrorInvalidOperation, code, "定制线路客户必须能买开途路由器版")
		assert.NotNil(t, latestOrderForUser(t, user.ID), "订单必须落库")
	})

	t.Run("renewable line with ledger row blocks hardware order", func(t *testing.T) {
		user := CreateTestUser(t)
		cleanupOrders(t, user.ID)
		live := mkLine(t, user.ID, PNStatusActive, 765000+user.ID)
		f := &RouterFulfillment{OrderID: live.OrderID, UserID: user.ID, SubID: live.ID,
			HardwareSKU: "redmi-ax6s", Stage: RouterStageOnline, ShippedAt: now - 5*86400}
		require.NoError(t, db.Get().Create(f).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(f) })
		code, _ := postOrder(t, r, user.ID, hwOrder)
		assert.EqualValues(t, ErrorInvalidOperation, code)
		assert.Nil(t, latestOrderForUser(t, user.ID))
	})

	t.Run("only expired fulfillment and no renewable line may buy hardware", func(t *testing.T) {
		user := CreateTestUser(t)
		cleanupOrders(t, user.ID)
		dead := mkLine(t, user.ID, PNStatusDeprovisioned, 750000+user.ID)
		f := &RouterFulfillment{OrderID: dead.OrderID, UserID: user.ID, SubID: dead.ID,
			HardwareSKU: "redmi-ax6s", Stage: RouterStageExpired, ShippedAt: now - 400*86400}
		require.NoError(t, db.Get().Create(f).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(f) })

		code, _ := postOrder(t, r, user.ID, hwOrder)
		assert.NotEqualValues(t, ErrorInvalidOperation, code)
		assert.NotNil(t, latestOrderForUser(t, user.ID), "hardware order must persist")
	})

	t.Run("stale non-expired stage over a deprovisioned line does not block", func(t *testing.T) {
		user := CreateTestUser(t)
		cleanupOrders(t, user.ID)
		dead := mkLine(t, user.ID, PNStatusDeprovisioned, 760000+user.ID)
		f := &RouterFulfillment{OrderID: dead.OrderID, UserID: user.ID, SubID: dead.ID,
			HardwareSKU: "redmi-ax6s", Stage: RouterStageOnline, ShippedAt: now - 400*86400}
		require.NoError(t, db.Get().Create(f).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(f) })

		code, _ := postOrder(t, r, user.ID, hwOrder)
		assert.NotEqualValues(t, ErrorInvalidOperation, code)
		assert.NotNil(t, latestOrderForUser(t, user.ID))
	})
}

// 服务套餐仅续费：真实下单时名下既没有未过期台账 / 可续线路，也没有任何成品台账 → ErrorInvalidOperation；
// 有可续线路、或只剩 expired 的成品台账（线路已回收）→ 放行；预览不受影响。
func TestCreateOrder_RouterServiceRenewalOnly(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	r := orderRegionTestRouter()
	svc := seedRouterPlan(t, "router-test-renew-only-svc", "", 29900)
	svcOrder := map[string]any{"preview": false, "plan": svc.PID, "region": "japan"}
	now := time.Now().Unix()

	mkLine := func(t *testing.T, userID uint64, status string, orderID uint64) *PrivateNodeSubscription {
		t.Helper()
		s := &PrivateNodeSubscription{UserID: userID, PlanID: svc.ID, OrderID: orderID, Region: "japan",
			IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40, Status: status,
			PurchasedAt: now - 400*86400, ExpiresAt: now + 300*86400}
		require.NoError(t, db.Get().Create(s).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(s) })
		return s
	}
	mkFulfillment := func(t *testing.T, userID uint64, sub *PrivateNodeSubscription, sku, stage string) {
		t.Helper()
		f := &RouterFulfillment{OrderID: sub.OrderID, UserID: userID, SubID: sub.ID,
			HardwareSKU: sku, Stage: stage, ShippedAt: now - 390*86400}
		require.NoError(t, db.Get().Create(f).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(f) })
	}
	cleanupOrders := func(t *testing.T, userID uint64) {
		t.Cleanup(func() { db.Get().Unscoped().Where("user_id = ?", userID).Delete(&Order{}) })
	}

	t.Run("no fulfillment and no line: rejected, preview still quotes", func(t *testing.T) {
		user := CreateTestUser(t)
		cleanupOrders(t, user.ID)

		code, _ := postOrder(t, r, user.ID, svcOrder)
		assert.EqualValues(t, ErrorInvalidOperation, code)
		assert.Nil(t, latestOrderForUser(t, user.ID), "rejected service order must not persist")

		code, _ = postOrder(t, r, user.ID, map[string]any{"preview": true, "plan": svc.PID, "region": "japan"})
		assert.EqualValues(t, 0, code)
	})

	t.Run("active router line (with ledger row): allowed", func(t *testing.T) {
		user := CreateTestUser(t)
		cleanupOrders(t, user.ID)
		live := mkLine(t, user.ID, PNStatusActive, 745000+user.ID)
		mkFulfillment(t, user.ID, live, "redmi-ax6s", RouterStageOnline)

		code, _ := postOrder(t, r, user.ID, svcOrder)
		assert.NotEqualValues(t, ErrorInvalidOperation, code)
		assert.NotNil(t, latestOrderForUser(t, user.ID), "renewal order must persist")
	})

	// 定制线路客户（活跃线路但没有任何路由器台账）不得买路由器续费套餐：那会用 $299 的服务费
	// 续掉一条 $599 的定制线路。本地 UAT 实测复现过这条资损。
	t.Run("active line without ledger row (定制线路): rejected", func(t *testing.T) {
		user := CreateTestUser(t)
		cleanupOrders(t, user.ID)
		mkLine(t, user.ID, PNStatusActive, 746000+user.ID)

		code, _ := postOrder(t, r, user.ID, svcOrder)
		assert.EqualValues(t, ErrorInvalidOperation, code)
		assert.Nil(t, latestOrderForUser(t, user.ID), "定制线路不得成为路由器续费的目标")
	})

	t.Run("only expired hardware fulfillment on a deprovisioned line: allowed", func(t *testing.T) {
		user := CreateTestUser(t)
		cleanupOrders(t, user.ID)
		dead := mkLine(t, user.ID, PNStatusDeprovisioned, 740000+user.ID)
		mkFulfillment(t, user.ID, dead, "redmi-ax6s", RouterStageExpired)

		code, _ := postOrder(t, r, user.ID, svcOrder)
		assert.NotEqualValues(t, ErrorInvalidOperation, code)
		assert.NotNil(t, latestOrderForUser(t, user.ID), "hardware customer's service order must persist")
	})

	t.Run("only expired self-supplied fulfillment on a deprovisioned line: rejected", func(t *testing.T) {
		user := CreateTestUser(t)
		cleanupOrders(t, user.ID)
		dead := mkLine(t, user.ID, PNStatusDeprovisioned, 730000+user.ID)
		mkFulfillment(t, user.ID, dead, "", RouterStageExpired)

		code, _ := postOrder(t, r, user.ID, svcOrder)
		assert.EqualValues(t, ErrorInvalidOperation, code)
		assert.Nil(t, latestOrderForUser(t, user.ID))
	})
}

func TestCreateOrder_RouterBadRegionRejected(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	r := orderRegionTestRouter()
	plan := seedRouterPlan(t, "router-test-region", "", 29900)
	user := CreateTestUser(t)
	code, _ := postOrder(t, r, user.ID, map[string]any{"preview": true, "plan": plan.PID, "region": "mars"})
	assert.EqualValues(t, ErrorInvalidArgument, code)
}
