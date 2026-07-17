package center

import (
	"context"
	"fmt"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// getPlanByPID 根据套餐PID获取套餐信息，按 brand 隔离——跨品牌 PID 必须解析为 nil，
// 否则用户可通过猜测/枚举对方品牌的 plan PID 绕过品牌边界完成真实下单（见
// api_order.go 调用点：payment-channel 门只挡 overleap 用户，kaitu 用户会直接穿透）。
func getPlanByPID(ctx context.Context, pid string, brand Brand) *Plan {
	log.Debugf(ctx, "getting plan by pid: %s (brand=%s)", pid, brand)
	var plan Plan
	if err := db.Get().Scopes(ScopeBrand(brand)).Where("pid = ? AND is_active = ?", pid, true).First(&plan).Error; err != nil {
		log.Warnf(ctx, "plan with pid %s not found for brand %s: %v", pid, brand, err)
		return nil
	}
	return &plan
}

// planByPIDForCredit 入账路径的 plan 查找：品牌隔离但不过滤 is_active——
// ops 下架 plan 只停新售，存量 Stripe 订阅的续费 invoice 仍须入账，
// 否则 credit 拒绝 → webhook 500 → Stripe 重试风暴（用户付了钱没到账）。
// 必须传入事务 tx（入账全程单事务）。
func planByPIDForCredit(ctx context.Context, tx *gorm.DB, pid string, brand Brand) *Plan {
	var plan Plan
	if err := tx.Scopes(ScopeBrand(brand)).Where("pid = ?", pid).First(&plan).Error; err != nil {
		log.Warnf(ctx, "plan pid %s not found for brand %s (credit path): %v", pid, brand, err)
		return nil
	}
	return &plan
}

// planByStripePriceID 按 Stripe Price ID 查套餐（GetActiveSubscriptions tier 反查用）。
// stripe 是 overleap 专属渠道，出生即带品牌过滤——planByAppleProductID 也现已等同品牌隔离（Phase A）。
func planByStripePriceID(ctx context.Context, tx *gorm.DB, priceID string) (*Plan, error) {
	if priceID == "" {
		return nil, fmt.Errorf("empty stripe price id")
	}
	var plan Plan
	if err := tx.Scopes(ScopeBrand(BrandOverleap)).Where("stripe_price_id = ?", priceID).First(&plan).Error; err != nil {
		return nil, fmt.Errorf("no plan for stripe price %s: %w", priceID, err)
	}
	return &plan, nil
}
