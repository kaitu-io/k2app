package center

import (
	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// TierWithPlans is a Tier descriptor enriched with the plans that belong to that
// tier. Returned by GET /api/tiers.
type TierWithPlans struct {
	TierInfo
	Plans []Plan `json:"plans"`
}

// buildTierInfos assembles the TierWithPlans slice without populating the Plans
// field. Pure (no DB), testable in isolation. GetTiers wraps this and adds the
// active plans for each tier.
func buildTierInfos() []TierWithPlans {
	all := AllTiers()
	out := make([]TierWithPlans, 0, len(all))
	for _, t := range all {
		out = append(out, TierWithPlans{TierInfo: t, Plans: nil})
	}
	return out
}

// GetTiers returns all 4 tiers (lite/basic/family/business) ordered by rank,
// each carrying the active plans purchasable at that tier. Public endpoint, no
// auth required.
func GetTiers(c *gin.Context) {
	out := buildTierInfos()

	names := make([]string, len(out))
	for i := range out {
		names[i] = out[i].Name
	}

	var plans []Plan
	if err := db.Get().
		Where("tier IN ? AND is_active = ?", names, true).
		Find(&plans).Error; err != nil {
		log.Errorf(c, "GetTiers: failed to load plans: %v", err)
		Error(c, ErrorSystemError, "failed to load plans")
		return
	}

	byTier := make(map[string][]Plan, len(out))
	for _, p := range plans {
		byTier[p.Tier] = append(byTier[p.Tier], p)
	}

	for i := range out {
		bucket := byTier[out[i].Name]
		if bucket == nil {
			bucket = []Plan{} // serialize as `[]` not `null`
		}
		out[i].Plans = bucket
	}

	Success(c, &gin.H{"tiers": out})
}
