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

// loadTiersWithPlans builds the tier list and attaches matching plans from
// the DB. includeInactive=false (public) returns only active plans;
// includeInactive=true (admin) returns all plans regardless of is_active.
func loadTiersWithPlans(c *gin.Context, includeInactive bool) ([]TierWithPlans, error) {
	out := buildTierInfos()

	names := make([]string, len(out))
	for i := range out {
		names[i] = out[i].Name
	}

	q := db.Get().Where("tier IN ?", names)
	if !includeInactive {
		q = q.Where("is_active = ?", true)
		// Tiers are an app-product concept. private_node plans reuse tier names
		// but are a separate product — never surface them on the public endpoint.
		q = q.Where("product = ?", ProductApp)
		// 公开端点按请求品牌隔离；admin 路径（includeInactive=true）保持跨品牌视角。
		q = q.Scopes(ScopeBrand(ReqBrand(c)))
	}

	var plans []Plan
	if err := q.Find(&plans).Error; err != nil {
		return nil, err
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

	return out, nil
}

// GetTiers returns all 4 tiers (lite/basic/family/business) ordered by rank,
// each carrying the active plans purchasable at that tier. Public endpoint, no
// auth required.
func GetTiers(c *gin.Context) {
	out, err := loadTiersWithPlans(c, false)
	if err != nil {
		log.Errorf(c, "GetTiers: failed to load plans: %v", err)
		Error(c, ErrorSystemError, "failed to load plans")
		return
	}
	Success(c, &gin.H{"tiers": out})
}

// GetAdminTiers returns all 4 tiers with ALL plans, including inactive ones.
// Admin-only — auth is enforced at the route group via AdminRequired()
// middleware (see route.go admin := r.Group("/app")).
func GetAdminTiers(c *gin.Context) {
	out, err := loadTiersWithPlans(c, true)
	if err != nil {
		log.Errorf(c, "GetAdminTiers: failed to load plans: %v", err)
		Error(c, ErrorSystemError, "failed to load plans")
		return
	}
	Success(c, &gin.H{"tiers": out})
}
