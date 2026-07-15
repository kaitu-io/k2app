package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPaymentChannelGate(t *testing.T) {
	assert.True(t, Brand("kaitu").Config().AllowsPayment(PayChannelWordgate))
	assert.True(t, Brand("kaitu").Config().AllowsPayment(PayChannelAppleIAP))
	// kaitu 用户永远碰不到 stripe（跨品牌渠道隔离硬边界）
	assert.False(t, Brand("kaitu").Config().AllowsPayment(PayChannelStripe))

	// Phase 6: overleap 开通 stripe（官网 Checkout）；apple_iap/google_play 随 App 上架再填
	assert.True(t, Brand("overleap").Config().AllowsPayment(PayChannelStripe))
	assert.False(t, Brand("overleap").Config().AllowsPayment(PayChannelWordgate))
	assert.False(t, Brand("overleap").Config().AllowsPayment(PayChannelAppleIAP))
	assert.False(t, Brand("overleap").Config().AllowsPayment(PayChannelGooglePlay))
}
