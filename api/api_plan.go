package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// api_get_plans 获取套餐列表
//
func api_get_plans(c *gin.Context) {
	log.Infof(c, "request to get plans")
	log.Debugf(c, "loading plans from database")
	var items []DataPlan

	var plans []Plan
	err := db.Get().Where("is_active = ?", true).Find(&plans).Error
	if err != nil {
		log.Errorf(c, "failed to load plans from database: %v", err)
		Error(c, ErrorSystemError, "failed to load plans")
		return
	}
	for _, plan := range plans {
		items = append(items, DataPlan{
			PID:         plan.PID,
			Label:       plan.Label,
			Price:       plan.Price,
			OriginPrice: plan.OriginPrice,
			Month:       plan.Month,
			Highlight:   plan.Highlight != nil && *plan.Highlight,
			IsActive:    plan.IsActive != nil && *plan.IsActive,
		})
	}
	log.Infof(c, "successfully loaded %d plans", len(items))
	ItemsAll(c, items)
}
