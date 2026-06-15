package center

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// buildPlanDTO 把单个 Plan 映射为 DataPlan，并为 private_node 套餐填充专属节点规格。
// 被 api_get_plans 与 api_get_product_plans 共用，避免重复。
func buildPlanDTO(c *gin.Context, plan Plan) DataPlan {
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
		Product:         plan.Product,
	}
	if plan.Product == ProductPrivateNode {
		var spec PrivateNodePlanSpec
		if err := db.Get().Where(&PrivateNodePlanSpec{PlanID: plan.ID}).First(&spec).Error; err == nil {
			var regions []string
			_ = json.Unmarshal([]byte(spec.AllowedRegions), &regions)
			if regions == nil {
				regions = []string{}
			}
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
	return dp
}

// api_get_plans 获取 App 套餐列表（已冻结）。
//
// 历史端点：老客户端无产品意识，因此这里硬过滤 product=app，
// 避免 private_node（专属线路）套餐泄漏给它们。新客户端走
// /api/products/:product/plans。
func api_get_plans(c *gin.Context) {
	log.Infof(c, "request to get plans (app-only, legacy)")
	var plans []Plan
	err := db.Get().Where("is_active = ?", true).Where("product = ?", ProductApp).Find(&plans).Error
	if err != nil {
		log.Errorf(c, "failed to load plans from database: %v", err)
		Error(c, ErrorSystemError, "failed to load plans")
		return
	}
	var items []DataPlan
	for _, plan := range plans {
		items = append(items, buildPlanDTO(c, plan))
	}
	log.Infof(c, "successfully loaded %d app plans", len(items))
	ItemsAll(c, items)
}

// api_get_product_plans 按产品线获取套餐列表。
//
// :product 必须为 ProductApp 或 ProductPrivateNode，否则返回 ErrorInvalidArgument。
func api_get_product_plans(c *gin.Context) {
	product := c.Param("product")
	if product != ProductApp && product != ProductPrivateNode {
		log.Warnf(c, "request for unknown product: %s", product)
		Error(c, ErrorInvalidArgument, "unknown product")
		return
	}
	log.Infof(c, "request to get plans for product %s", product)
	var plans []Plan
	err := db.Get().Where("is_active = ?", true).Where("product = ?", product).Find(&plans).Error
	if err != nil {
		log.Errorf(c, "failed to load plans from database: %v", err)
		Error(c, ErrorSystemError, "failed to load plans")
		return
	}
	var items []DataPlan
	for _, plan := range plans {
		items = append(items, buildPlanDTO(c, plan))
	}
	log.Infof(c, "successfully loaded %d plans for product %s", len(items), product)
	ItemsAll(c, items)
}
