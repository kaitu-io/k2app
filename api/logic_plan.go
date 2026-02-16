package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"context"

	"github.com/wordgate/qtoolkit/log"
)

// getPlanByID 根据套餐ID获取套餐信息
func getPlanByPID(ctx context.Context, pid string) *Plan {
	log.Debugf(ctx, "getting plan by pid: %s", pid)
	var plan Plan
	if err := db.Get().Where("pid = ? AND is_active = ?", pid, true).First(&plan).Error; err != nil {
		log.Warnf(ctx, "plan with pid %s not found: %v", pid, err)
		return nil
	}
	return &plan
}
