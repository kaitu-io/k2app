package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPaymentChannelGate(t *testing.T) {
	assert.True(t, Brand("kaitu").Config().AllowsPayment(PayChannelWordgate))
	assert.True(t, Brand("kaitu").Config().AllowsPayment(PayChannelAppleIAP))
	// Phase 1 阶段 overleap 一切渠道关闭；Phase 6 填充后更新此断言
	assert.False(t, Brand("overleap").Config().AllowsPayment(PayChannelWordgate))
	assert.False(t, Brand("overleap").Config().AllowsPayment(PayChannelAppleIAP))
	assert.False(t, Brand("overleap").Config().AllowsPayment(PayChannelStripe))
}
