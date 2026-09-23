package center

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 与 setNextpayReady 写入的 nextpay.webhook_secret 一致。
const nextpayTestWebhookSecret = "test-ws"

// nextpaySig 按 qtoolkit/nextpay 的 "t=<unix>,v1=<hex HMAC-SHA256(secret, t.body)>" 签名。
func nextpaySig(body []byte) string {
	t := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(nextpayTestWebhookSecret))
	mac.Write([]byte(fmt.Sprintf("%d.", t)))
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", t, hex.EncodeToString(mac.Sum(nil)))
}

func nextpayWebhookRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/webhook/nextpay", BrandResolver(), api_nextpay_webhook)
	return r
}

func postNextpayWebhook(t *testing.T, body []byte, sig string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook/nextpay", strings.NewReader(string(body)))
	req.Header.Set("X-NextPay-Signature", sig)
	nextpayWebhookRouter().ServeHTTP(w, req)
	return w
}

func orderPaidPayload(evtID, nextpayOrderID, objectID string, amount uint64, currency string) []byte {
	return []byte(fmt.Sprintf(`{"id":%q,"type":"order.paid","timestamp":%d,"data":{"orderId":%q,"userId":"u","amount":%d,"currency":%q,"status":"paid","productName":"一年","objectId":%q,"isSubscription":false,"paidAt":%d}}`,
		evtID, time.Now().Unix(), nextpayOrderID, amount, currency, objectID, time.Now().Unix()))
}

// captureAnomaly 替换告警 seam，收集 tag。
func captureAnomaly(t *testing.T) *[]string {
	t.Helper()
	orig := alertPaymentAnomaly
	t.Cleanup(func() { alertPaymentAnomaly = orig })
	tags := []string{}
	alertPaymentAnomaly = func(ctx context.Context, tag, format string, args ...any) { tags = append(tags, tag) }
	return &tags
}

func TestNextpayWebhook_BadSignature_400(t *testing.T) {
	setNextpayReady(t)
	w := postNextpayWebhook(t, orderPaidPayload("evt_1", "np-1", "o-1", 4990, "usd"), "t=1,v1=deadbeef")
	assert.Equal(t, 400, w.Code)
}

func TestNextpayWebhook_NotConfigured_503(t *testing.T) {
	testInitConfig()
	for _, k := range []string{"nextpay.access_key", "nextpay.webhook_secret"} {
		orig := viper.Get(k)
		t.Cleanup(func() { viper.Set(k, orig) })
		viper.Set(k, "")
	}
	body := orderPaidPayload("evt_0", "np-1", "o-1", 4990, "usd")
	w := postNextpayWebhook(t, body, nextpaySig(body))
	assert.Equal(t, 503, w.Code)
}

func TestNextpayWebhook_OrderPaid_Credits(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	setNextpayReady(t)
	plan := nextpayTestPlan(t)
	user := CreateTestUser(t)
	o := newUnpaidNextpayOrder(t, user, plan)
	o.NextpayOrderID = "np-1"
	require.NoError(t, db.Get().Save(o).Error)
	tags := captureAnomaly(t)

	body := orderPaidPayload("evt_1", "np-1", o.UUID, plan.Price, "usd")
	w := postNextpayWebhook(t, body, nextpaySig(body))
	require.Equal(t, 200, w.Code, w.Body.String())

	var saved Order
	require.NoError(t, db.Get().Where("uuid = ?", o.UUID).First(&saved).Error)
	assert.True(t, saved.IsPaid != nil && *saved.IsPaid, "必须经 MarkOrderAsPaid 入账")
	assert.Empty(t, *tags)

	// 重投同一事件：200 且不再告警、不重复入账（MarkOrderAsPaid 幂等）
	w = postNextpayWebhook(t, body, nextpaySig(body))
	assert.Equal(t, 200, w.Code)
	assert.Empty(t, *tags)
}

func TestNextpayWebhook_AmountMismatch_AlertsAnd5xx(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	setNextpayReady(t)
	plan := nextpayTestPlan(t)
	user := CreateTestUser(t)
	o := newUnpaidNextpayOrder(t, user, plan)
	tags := captureAnomaly(t)

	body := orderPaidPayload("evt_2", "np-1", o.UUID, plan.Price-1, "usd")
	w := postNextpayWebhook(t, body, nextpaySig(body))
	assert.Equal(t, 500, w.Code)
	assert.Equal(t, []string{"PAYMENT-AMOUNT-MISMATCH"}, *tags)

	var saved Order
	require.NoError(t, db.Get().Where("uuid = ?", o.UUID).First(&saved).Error)
	assert.False(t, saved.IsPaid != nil && *saved.IsPaid, "金额错配绝不入账")
}

func TestNextpayWebhook_DoublePay_AlertsAndAcks(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	setNextpayReady(t)
	plan := nextpayTestPlan(t)
	user := CreateTestUser(t)
	o := newUnpaidNextpayOrder(t, user, plan)
	require.NoError(t, db.Get().Model(o).Updates(map[string]any{"is_paid": true, "nextpay_order_id": "np-1"}).Error)
	tags := captureAnomaly(t)

	body := orderPaidPayload("evt_3", "np-2", o.UUID, plan.Price, "usd")
	w := postNextpayWebhook(t, body, nextpaySig(body))
	assert.Equal(t, 200, w.Code, "已付订单被另一个 NextPay 订单再次付款：告警但 ack")
	assert.Equal(t, []string{"DOUBLE-PAY"}, *tags)
}

func TestNextpayWebhook_UnknownOrder_Acks(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	setNextpayReady(t)
	tags := captureAnomaly(t)
	body := orderPaidPayload("evt_4", "np-9", "no-such-order", 100, "usd")
	w := postNextpayWebhook(t, body, nextpaySig(body))
	assert.Equal(t, 200, w.Code, "永久异常 ack，避免 15 次重试打空")
	assert.Empty(t, *tags)
}

func TestNextpayWebhook_OtherEvent_Acks(t *testing.T) {
	setNextpayReady(t)
	body := []byte(`{"id":"evt_5","type":"order.expired","timestamp":1,"data":{"orderId":"np-1","objectId":"o-1","status":"expired"}}`)
	w := postNextpayWebhook(t, body, nextpaySig(body))
	assert.Equal(t, 200, w.Code)
}

// 两条分支的交点：NextPay 入账必须走路由器版续费分支（applyRouterOrder → extendPrivateLine），
// 且续费副作用经 OrderPostCommit 延后到提交后——签名若退回旧的 *[]uint64 本测试编译不过。
func TestNextpayWebhook_RouterRenewal_ExtendsLine(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	setNextpayReady(t)
	user := CreateTestUser(t)
	plan := seedRouterPlan(t, "router-np-renew", "", 29900)
	now := time.Now().Unix()
	// 既有的路由器版线路必须连带台账：台账是"这条线路属于路由器版"的唯一判据（routerLineIDs），
	// 只建线路等于造了一条定制线路，续费不会认它（见 TestRouterGates_*）。
	existing := &PrivateNodeSubscription{UserID: user.ID, PlanID: plan.ID, OrderID: 780000 + user.ID,
		Region: "japan", IPType: IPTypeNonResidential, TrafficTotalBytes: 2 << 40,
		Status: PNStatusGrace, PurchasedAt: now - 400*86400, ExpiresAt: now - 3*86400, GraceUntil: now + 4*86400}
	require.NoError(t, db.Get().Create(existing).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(existing) })
	existingFul := &RouterFulfillment{OrderID: existing.OrderID, UserID: user.ID, SubID: existing.ID,
		HardwareSKU: "redmi-ax6s", Stage: RouterStageShipped, ShippedAt: now - 390*86400}
	require.NoError(t, db.Get().Create(existingFul).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(existingFul) })
	o := newUnpaidNextpayOrder(t, user, plan)
	o.NextpayOrderID = "np-rt-1"
	require.NoError(t, db.Get().Save(o).Error)
	tags := captureAnomaly(t)

	body := orderPaidPayload("evt_rt_1", "np-rt-1", o.UUID, plan.Price, "usd")
	w := postNextpayWebhook(t, body, nextpaySig(body))
	require.Equal(t, 200, w.Code, w.Body.String())
	assert.Empty(t, *tags)

	var saved Order
	require.NoError(t, db.Get().Where("uuid = ?", o.UUID).First(&saved).Error)
	assert.True(t, saved.IsPaid != nil && *saved.IsPaid)

	var subs []PrivateNodeSubscription
	require.NoError(t, db.Get().Where("user_id = ?", user.ID).Find(&subs).Error)
	require.Len(t, subs, 1, "renewal must extend the existing line, not create a second one")
	assert.Equal(t, PNStatusActive, subs[0].Status)
	assert.GreaterOrEqual(t, subs[0].ExpiresAt, time.Unix(now, 0).AddDate(0, 12, 0).Unix()-5)

	var cnt int64
	db.Get().Model(&RouterFulfillment{}).Where("order_id = ?", o.ID).Count(&cnt)
	assert.EqualValues(t, 0, cnt, "renewal creates no fulfillment row")
}
