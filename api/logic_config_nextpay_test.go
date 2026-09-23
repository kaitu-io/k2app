package center

import (
	"context"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestConfigNextpay_ReadyAndDefaults(t *testing.T) {
	testInitConfig()
	for _, k := range []string{"nextpay.access_key", "nextpay.webhook_secret", "nextpay.payment_method"} {
		orig := viper.Get(k)
		t.Cleanup(func() { viper.Set(k, orig) })
	}

	viper.Set("nextpay.access_key", "")
	viper.Set("nextpay.webhook_secret", "")
	viper.Set("nextpay.payment_method", "")
	cfg := configNextpay(context.Background())
	assert.False(t, cfg.Ready(), "缺 access_key / webhook_secret 必须 not ready")
	assert.Equal(t, "more", cfg.PaymentMethod, "payment_method 缺省 more")

	viper.Set("nextpay.access_key", "ak")
	viper.Set("nextpay.webhook_secret", "ws")
	viper.Set("nextpay.payment_method", "alipay")
	cfg = configNextpay(context.Background())
	assert.True(t, cfg.Ready())
	assert.Equal(t, "alipay", cfg.PaymentMethod)
}
