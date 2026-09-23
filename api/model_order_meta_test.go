package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrderMetaPayUrl(t *testing.T) {
	o := &Order{}
	err := o.SetOrderMeta(&Plan{PID: "pro-1y", Label: "1 年 Pro"}, nil, []string{"uuid-a", "uuid-b"}, false)
	require.NoError(t, err)
	require.Equal(t, "", o.GetPayUrl())

	err = o.SetOrderCheckout("https://www.kaitu.io/api/orders/x/pay", "https://checkout.stripe.com/c/pay/cs_123", 1700000000)
	require.NoError(t, err)
	assert.Equal(t, "https://www.kaitu.io/api/orders/x/pay", o.GetPayUrl())

	// Other fields survive second marshal
	p, err := o.GetPlan()
	require.NoError(t, err)
	assert.Equal(t, "pro-1y", p.PID)
	assert.False(t, o.GetForMyself())
	assert.Equal(t, []string{"uuid-a", "uuid-b"}, o.GetForUsers())
}

func TestOrderMetaSetCheckoutOnEmpty(t *testing.T) {
	o := &Order{}
	err := o.SetOrderCheckout("https://www.kaitu.io/api/orders/x/pay", "https://checkout.stripe.com/c/pay/x", 1)
	require.NoError(t, err)
	assert.Equal(t, "https://www.kaitu.io/api/orders/x/pay", o.GetPayUrl())
	url, at := o.GetCheckout()
	assert.Equal(t, "https://checkout.stripe.com/c/pay/x", url)
	assert.Equal(t, int64(1), at)
}
