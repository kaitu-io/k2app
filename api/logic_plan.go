package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"context"

	"github.com/wordgate/qtoolkit/log"
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
