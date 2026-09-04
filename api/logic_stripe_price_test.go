package center

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v82"
)

// stubStripeGetPrice 替换 SDK 调用点并计数；测试结束还原 + 清缓存。
func stubStripeGetPrice(t *testing.T, fn func(key, id string) (*stripe.Price, error)) *int {
	t.Helper()
	calls := 0
	orig := stripeGetPrice
	stripeGetPrice = func(key, id string) (*stripe.Price, error) {
		calls++
		return fn(key, id)
	}
	resetStripePriceCache()
	t.Cleanup(func() {
		stripeGetPrice = orig
		resetStripePriceCache()
	})
	return &calls
}

func usdPriceFixture() *stripe.Price {
	return &stripe.Price{
		ID:         "price_test_1y",
		Currency:   stripe.CurrencyUSD,
		UnitAmount: 7900,
		CurrencyOptions: map[string]*stripe.PriceCurrencyOptions{
			"gbp": {UnitAmount: 7900},
			"eur": {UnitAmount: 8900},
		},
	}
}

func TestStripePriceCurrencyAmounts(t *testing.T) {
	ctx := context.Background()

	t.Run("maps primary currency plus every currency option", func(t *testing.T) {
		calls := stubStripeGetPrice(t, func(key, id string) (*stripe.Price, error) {
			assert.Equal(t, "sk_test_x", key)
			assert.Equal(t, "price_test_1y", id)
			return usdPriceFixture(), nil
		})
		got, err := stripePriceCurrencyAmounts(ctx, "sk_test_x", "price_test_1y")
		require.NoError(t, err)
		assert.Equal(t, map[string]int64{"usd": 7900, "gbp": 7900, "eur": 8900}, got)
		assert.Equal(t, 1, *calls)
	})

	t.Run("second call within TTL is served from cache", func(t *testing.T) {
		calls := stubStripeGetPrice(t, func(string, string) (*stripe.Price, error) { return usdPriceFixture(), nil })
		_, err := stripePriceCurrencyAmounts(ctx, "k", "price_test_1y")
		require.NoError(t, err)
		got, err := stripePriceCurrencyAmounts(ctx, "k", "price_test_1y")
		require.NoError(t, err)
		assert.Equal(t, int64(8900), got["eur"])
		assert.Equal(t, 1, *calls, "cache hit must not call Stripe again")
	})

	t.Run("SDK error propagates and is not cached", func(t *testing.T) {
		calls := stubStripeGetPrice(t, func(string, string) (*stripe.Price, error) { return nil, errors.New("boom") })
		_, err := stripePriceCurrencyAmounts(ctx, "k", "price_bad")
		require.Error(t, err)
		_, err = stripePriceCurrencyAmounts(ctx, "k", "price_bad")
		require.Error(t, err)
		assert.Equal(t, 2, *calls, "errors must not be cached")
	})

	t.Run("price without fixed unit_amount is rejected", func(t *testing.T) {
		stubStripeGetPrice(t, func(string, string) (*stripe.Price, error) {
			return &stripe.Price{ID: "price_tiered", Currency: stripe.CurrencyUSD, UnitAmount: 0}, nil
		})
		_, err := stripePriceCurrencyAmounts(ctx, "k", "price_tiered")
		require.Error(t, err)
	})
}

func TestBuildPlanDTOCurrencyPrices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	t.Run("stripe plan carries currencyPrices when config is ready", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_x", "whsec_x")
		stubStripeGetPrice(t, func(string, string) (*stripe.Price, error) { return usdPriceFixture(), nil })
		dp := buildPlanDTO(c, Plan{PID: "overleap-basic-1y", Price: 7900, Month: 12, Product: ProductApp,
			Brand: string(BrandOverleap), StripePriceID: "price_test_1y"})
		assert.Equal(t, map[string]int64{"usd": 7900, "gbp": 7900, "eur": 8900}, dp.CurrencyPrices)
		assert.Equal(t, uint64(7900), dp.Price, "Plan.Price stays the USD display fallback")
	})

	t.Run("plan without stripe price id never touches Stripe", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_x", "whsec_x")
		calls := stubStripeGetPrice(t, func(string, string) (*stripe.Price, error) { return usdPriceFixture(), nil })
		dp := buildPlanDTO(c, Plan{PID: "2y", Price: 9490, Month: 24, Product: ProductApp, Brand: string(BrandKaitu)})
		assert.Nil(t, dp.CurrencyPrices)
		assert.Equal(t, 0, *calls)
	})

	t.Run("stripe failure is fail-open: field omitted, price kept", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_x", "whsec_x")
		stubStripeGetPrice(t, func(string, string) (*stripe.Price, error) { return nil, errors.New("down") })
		dp := buildPlanDTO(c, Plan{PID: "overleap-basic-1m", Price: 1199, Month: 1, Product: ProductApp,
			Brand: string(BrandOverleap), StripePriceID: "price_test_1m"})
		assert.Nil(t, dp.CurrencyPrices)
		assert.Equal(t, uint64(1199), dp.Price)
	})

	t.Run("missing stripe config skips the lookup", func(t *testing.T) {
		setStripeTestConfig(t, "", "")
		calls := stubStripeGetPrice(t, func(string, string) (*stripe.Price, error) { return usdPriceFixture(), nil })
		dp := buildPlanDTO(c, Plan{PID: "overleap-basic-1y", Price: 7900, Month: 12, Product: ProductApp,
			Brand: string(BrandOverleap), StripePriceID: "price_test_1y"})
		assert.Nil(t, dp.CurrencyPrices)
		assert.Equal(t, 0, *calls)
	})
}
