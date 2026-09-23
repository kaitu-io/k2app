package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrder_SetOrderCheckout_PreservesPlanAndSetsPayUrl(t *testing.T) {
	o := &Order{}
	require.NoError(t, o.SetOrderMeta(&Plan{PID: "p1", Label: "L"}, nil, nil, true))

	require.NoError(t, o.SetOrderCheckout("https://www.kaitu.io/api/orders/u1/pay", "https://checkout.stripe.com/c/pay/cs_1", 1700000000))

	assert.Equal(t, "https://www.kaitu.io/api/orders/u1/pay", o.GetPayUrl(), "代付邮件读 payUrl，必须是耐久链接")
	url, at := o.GetCheckout()
	assert.Equal(t, "https://checkout.stripe.com/c/pay/cs_1", url)
	assert.Equal(t, int64(1700000000), at)
	plan, err := o.GetPlan()
	require.NoError(t, err)
	assert.Equal(t, "p1", plan.PID, "plan 必须保留")
	assert.True(t, o.GetForMyself())
}

func TestOrder_GetCheckout_EmptyMeta(t *testing.T) {
	url, at := (&Order{}).GetCheckout()
	assert.Equal(t, "", url)
	assert.Equal(t, int64(0), at)
}
