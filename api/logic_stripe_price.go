package center

import (
	"context"
	"fmt"
	"sync"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/price"
	"github.com/wordgate/qtoolkit/log"
)

// Stripe Price 的多币种价中继（DataPlan.currencyPrices）。
//
// 定价真相在 Stripe Price 对象：主币 unit_amount + currency_options（Checkout 按客户属地
// 自动在这些币种里选）。客户端要按 locale 展示"你大概会付多少"，就得拿到这张表——
// 由本文件从 Stripe 取一次、进程内缓存 1h，失败只记日志（fail-open：字段省略，
// 客户端回落 Plan.Price 的美元价）。kaitu plan 没有 StripePriceID，永远不走这里。

// stripeGetPrice 是 SDK 调用的测试替换点（同 stripeNewCheckoutSession 的包级 var 模式）。
// key 逐调用传入，不写 stripe.Key 包级全局。
var stripeGetPrice = func(key, priceID string) (*stripe.Price, error) {
	return price.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}.Get(priceID, &stripe.PriceParams{
		Expand: []*string{stripe.String("currency_options")},
	})
}

const stripePriceCacheTTL = time.Hour

type stripePriceCacheEntry struct {
	amounts   map[string]int64
	expiresAt time.Time
}

var stripePriceCache sync.Map // priceID → stripePriceCacheEntry

// stripePriceCurrencyAmounts 返回 priceID 的 {币种(小写) → 最小单位金额}，含主币与全部
// currency_options。缓存 1h。Price 无 unit_amount（分级/按量计价）视为错误——本产品只用
// 固定价，出现即配置错。
func stripePriceCurrencyAmounts(ctx context.Context, key, priceID string) (map[string]int64, error) {
	if v, ok := stripePriceCache.Load(priceID); ok {
		if e := v.(stripePriceCacheEntry); time.Now().Before(e.expiresAt) {
			return e.amounts, nil
		}
	}
	p, err := stripeGetPrice(key, priceID)
	if err != nil {
		return nil, fmt.Errorf("get stripe price %s: %w", priceID, err)
	}
	if p == nil || p.Currency == "" || p.UnitAmount <= 0 {
		return nil, fmt.Errorf("stripe price %s has no fixed unit_amount", priceID)
	}
	amounts := map[string]int64{string(p.Currency): p.UnitAmount}
	for cur, opt := range p.CurrencyOptions {
		if opt != nil && opt.UnitAmount > 0 {
			amounts[cur] = opt.UnitAmount
		}
	}
	stripePriceCache.Store(priceID, stripePriceCacheEntry{amounts: amounts, expiresAt: time.Now().Add(stripePriceCacheTTL)})
	log.Debugf(ctx, "[Stripe] cached currency amounts for price %s: %v", priceID, amounts)
	return amounts, nil
}

// resetStripePriceCache 清空缓存（测试用）。
func resetStripePriceCache() {
	stripePriceCache.Range(func(k, _ any) bool {
		stripePriceCache.Delete(k)
		return true
	})
}
