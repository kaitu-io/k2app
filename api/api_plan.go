package center

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
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
		q := plan.Quota()
		dp := DataPlan{
			PID:             plan.PID,
			Tier:            plan.Tier,
			Label:           plan.Label,
			Price:           plan.Price,
			OriginPrice:     plan.OriginPrice,
			Month:           plan.Month,
			Highlight:       plan.Highlight != nil && *plan.Highlight,
			IsActive:        plan.IsActive != nil && *plan.IsActive,
			MaxDevice:       q.MaxDevice,
			MaxRouterDevice: q.MaxRouterDevice,
			MaxLanClient:    q.MaxLanClient,
			AppleProductID:  plan.AppleProductID,
			Kind:            plan.Kind,
		}
		if plan.Kind == PlanKindPrivateNode {
			var spec PrivateNodePlanSpec
			if err := db.Get().Where(&PrivateNodePlanSpec{PlanID: plan.ID}).First(&spec).Error; err == nil {
				var regions []string
				_ = json.Unmarshal([]byte(spec.AllowedRegions), &regions)
				dp.PrivateNode = &DataPrivateNodePlanSpec{
					Provider:          spec.Provider,
					IPType:            spec.IPType,
					AllowedRegions:    regions,
					TrafficTotalBytes: spec.TrafficTotalBytes,
				}
			} else {
				log.Warnf(c, "private_node plan %s (id=%d) has no PrivateNodePlanSpec: %v", plan.PID, plan.ID, err)
			}
		}
		items = append(items, dp)
	}
	log.Infof(c, "successfully loaded %d plans", len(items))
	ItemsAll(c, items)
}
