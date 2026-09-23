package center

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/wordgate-sdk"
)

// ORPHAN-PAYMENT 哨兵：渠道报告"已付"，本地却查不到对应订单 —— 钱收了、没记录。
//
// 改这两条测试前先想清楚：在此之前 **两条渠道都只写一行 log.Errorf**，nextpay 那侧还
// 直接 ack 丢弃。也就是说这种事故此前不会惊动任何人。哨兵存在的意义就是让它惊动人。
//
// nextpay 侧的同型用例在 api_nextpay_webhook_test.go
// （TestNextpayWebhook_UnknownOrder_AlertsAndAcks），跟它的 HTTP 辅助函数放在一起。

func TestWordgateWebhook_OrphanPayment_Alerts(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	tags := captureAnomaly(t)

	paidAt := time.Now()
	event := &wordgate.WebhookEventData{
		EventType: wordgate.WebhookEventOrderPaid,
		Data: wordgate.WebhookOrderPaidData{
			WordgateOrderNo: "wg-no-such-order-" + time.Now().Format("150405.000000"),
			Amount:          4900,
			Currency:        "USD",
			IsPaid:          true,
			PaidAt:          &paidAt,
		},
		Timestamp: paidAt.Unix(),
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	// 沿用既有语义：返回 error 让 wordgate 重投（本次不改重投行为，只补告警）。
	err := handleWordgateOrderPaidEvent(c, event)
	assert.Error(t, err, "查不到订单必须返回 error，触发渠道重投")
	assert.Equal(t, []string{"ORPHAN-PAYMENT"}, *tags)
}

func TestWordgateWebhook_OrphanButUnpaid_DoesNotAlert(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	tags := captureAnomaly(t)

	// isPaid=false 的事件即使匹配不到订单也不是事故：没有钱被收走。
	event := &wordgate.WebhookEventData{
		EventType: wordgate.WebhookEventOrderPaid,
		Data: wordgate.WebhookOrderPaidData{
			WordgateOrderNo: "wg-no-such-order-unpaid",
			Amount:          4900,
			Currency:        "USD",
			IsPaid:          false,
		},
		Timestamp: time.Now().Unix(),
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	assert.Error(t, handleWordgateOrderPaidEvent(c, event))
	assert.Empty(t, *tags, "未收钱的 not-found 不应告警")
}
