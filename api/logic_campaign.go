package center

import (
	"context"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

const (
	CampaignTypeDiscount = "discount"
	CampaignTypeCoupon   = "coupon"
)

// getCampaignByCode 根据优惠码获取优惠活动信息（从数据库查询）
func getCampaignByCode(ctx context.Context, code string) *Campaign {
	log.Debugf(ctx, "getting campaign by code: %s", code)

	var campaign Campaign
	if err := db.Get().Where(&Campaign{Code: code, IsActive: BoolPtr(true)}).First(&campaign).Error; err != nil {
		log.Warnf(ctx, "campaign code not found: %s", code)
		return nil
	}

	// 检查活动是否过期
	now := time.Now().Unix()
	if now < campaign.StartAt || now > campaign.EndAt {
		log.Warnf(ctx, "campaign %s is not in valid time range", code)
		return nil
	}

	// 检查使用次数限制
	if campaign.MaxUsage > 0 && campaign.UsageCount >= campaign.MaxUsage {
		log.Warnf(ctx, "campaign %s has reached usage limit", code)
		return nil
	}

	return &campaign
}

// getCampaignMatcher 根据匹配器类型返回对应的匹配函数
func getCampaignMatcher(matcherType string) func(ctx context.Context, user *User, order *Order) bool {
	switch matcherType {
	case "first_order":
		return func(ctx context.Context, user *User, order *Order) bool {
			return user.IsFirstOrderDone != nil && *user.IsFirstOrderDone
		}
	case "vip":
		return func(ctx context.Context, user *User, order *Order) bool {
			// VIP定义：只要完成过首单就算VIP（不管是否过期）
			isVip := user.IsVip()
			log.Debugf(ctx, "checking VIP campaign for user %d, isVip: %v", user.ID, isVip)
			return isVip
		}
	case "all":
		return func(ctx context.Context, user *User, order *Order) bool {
			log.Debugf(ctx, "campaign matches all users")
			return true
		}
	default:
		return func(ctx context.Context, user *User, order *Order) bool {
			log.Warnf(ctx, "unknown matcher type: %s", matcherType)
			return false
		}
	}
}

// incrementCampaignUsage 增加活动使用次数
func incrementCampaignUsage(ctx context.Context, code string) error {
	log.Debugf(ctx, "incrementing usage for campaign: %s", code)

	result := db.Get().Model(&Campaign{}).Where(&Campaign{Code: code}).Update("usage_count", gorm.Expr("usage_count + 1"))
	if result.Error != nil {
		log.Errorf(ctx, "failed to increment campaign usage: %v", result.Error)
		return result.Error
	}

	log.Infof(ctx, "successfully incremented usage for campaign: %s", code)
	return nil
}

// matchCampaign 检查活动是否匹配用户
func matchCampaign(ctx context.Context, campaign *Campaign, user *User, order *Order) bool {
	log.Debugf(ctx, "matching campaign %s for user %d", campaign.Code, user.ID)

	if campaign.EndAt < time.Now().Unix() {
		log.Warnf(ctx, "campaign %s has expired", campaign.Code)
		return false
	}

	// 根据匹配器类型获取匹配函数
	matcher := getCampaignMatcher(campaign.MatcherType)
	if matcher == nil {
		log.Warnf(ctx, "campaign %s has invalid matcher type: %s", campaign.Code, campaign.MatcherType)
		return false
	}

	return matcher(ctx, user, order)
}

// applyCampaign 应用活动到订单
func applyCampaign(ctx context.Context, campaign *Campaign, order *Order) (uint64, error) {
	log.Debugf(ctx, "applying campaign %s to order %s", campaign.Code, order.UUID)

	var newAmount uint64
	var err error

	switch campaign.Type {
	case CampaignTypeDiscount:
		// 折扣类型，Value 表示百分比（如 80 表示 8 折）
		if campaign.Value == 0 || campaign.Value > 100 {
			log.Warnf(ctx, "invalid discount value %d for campaign %s", campaign.Value, campaign.Code)
			return order.OriginAmount, nil
		}
		newAmount = order.OriginAmount * campaign.Value / 100
		log.Infof(ctx, "applied discount campaign %s, original amount: %d, new amount: %d", campaign.Code, order.OriginAmount, newAmount)

	case CampaignTypeCoupon:
		// 优惠券类型，Value 表示优惠金额（美分）
		if campaign.Value >= order.OriginAmount {
			log.Infof(ctx, "coupon value %d is greater than or equal to origin amount %d for campaign %s, price becomes 0", campaign.Value, order.OriginAmount, campaign.Code)
			newAmount = 0 // 优惠金额大于等于原价时，价格为0
		} else {
			newAmount = order.OriginAmount - campaign.Value
			log.Infof(ctx, "applied coupon campaign %s, original amount: %d, new amount: %d", campaign.Code, order.OriginAmount, newAmount)
		}

	default:
		log.Warnf(ctx, "unknown campaign type %s for campaign %s", campaign.Type, campaign.Code)
		return order.OriginAmount, nil
	}

	// 增加活动使用次数
	if err = incrementCampaignUsage(ctx, campaign.Code); err != nil {
		log.Warnf(ctx, "failed to increment campaign usage, but continue with order processing: %v", err)
		// 不因为统计失败而阻止订单处理
	}

	return newAmount, nil
}

// CampaignStats Campaign统计数据
type CampaignStats struct {
	Code            string  `json:"code"`
	TotalUsed       int64   `json:"totalUsed"`       // 总使用次数
	PaidOrders      int64   `json:"paidOrders"`      // 已支付订单数
	TotalDiscount   uint64  `json:"totalDiscount"`   // 总折扣金额
	TotalRevenue    uint64  `json:"totalRevenue"`    // 总产生收入
	ConversionRate  float64 `json:"conversionRate"`  // 转化率
	UniqueUsers     int64   `json:"uniqueUsers"`     // 独立用户数
	AvgDiscountPerOrder uint64 `json:"avgDiscountPerOrder"` // 平均每单折扣
}

// getCampaignStats 获取Campaign统计数据（实时查询）
func getCampaignStats(ctx context.Context, campaignCode string) (*CampaignStats, error) {
	log.Debugf(ctx, "calculating stats for campaign: %s", campaignCode)

	stats := &CampaignStats{Code: campaignCode}

	// 一次查询获取所有统计数据
	var result struct {
		TotalUsed     int64  `json:"total_used"`
		PaidOrders    int64  `json:"paid_orders"`
		TotalDiscount uint64 `json:"total_discount"`
		TotalRevenue  uint64 `json:"total_revenue"`
		UniqueUsers   int64  `json:"unique_users"`
	}

	err := db.Get().Model(&Order{}).
		Select(`
			COUNT(*) as total_used,
			COUNT(CASE WHEN is_paid = true THEN 1 END) as paid_orders,
			COALESCE(SUM(CASE WHEN is_paid = true THEN campaign_reduce_amount END), 0) as total_discount,
			COALESCE(SUM(CASE WHEN is_paid = true THEN pay_amount END), 0) as total_revenue,
			COUNT(DISTINCT user_id) as unique_users
		`).
		Where("campaign_code = ?", campaignCode).
		Scan(&result).Error

	if err != nil {
		log.Errorf(ctx, "failed to get campaign stats: %v", err)
		return nil, err
	}

	stats.TotalUsed = result.TotalUsed
	stats.PaidOrders = result.PaidOrders
	stats.TotalDiscount = result.TotalDiscount
	stats.TotalRevenue = result.TotalRevenue
	stats.UniqueUsers = result.UniqueUsers

	// 计算转化率
	if stats.TotalUsed > 0 {
		stats.ConversionRate = float64(stats.PaidOrders) / float64(stats.TotalUsed)
	}

	// 计算平均每单折扣
	if stats.PaidOrders > 0 {
		stats.AvgDiscountPerOrder = stats.TotalDiscount / uint64(stats.PaidOrders)
	}

	log.Debugf(ctx, "campaign stats calculated: total=%d, paid=%d, conversion=%.2f",
		stats.TotalUsed, stats.PaidOrders, stats.ConversionRate)

	return stats, nil
}

// getCampaignOrders 获取使用特定Campaign的订单列表
func getCampaignOrders(ctx context.Context, campaignCode string, pagination *Pagination) ([]Order, error) {
	log.Debugf(ctx, "getting orders for campaign: %s", campaignCode)

	var orders []Order
	query := db.Get().Model(&Order{}).Where("campaign_code = ?", campaignCode).Preload("User").Preload("Campaign")

	// 统计总数
	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(ctx, "failed to count campaign orders: %v", err)
		return nil, err
	}

	// 分页查询
	if err := query.Offset(pagination.Offset()).Limit(pagination.PageSize).
		Order("created_at DESC").Find(&orders).Error; err != nil {
		log.Errorf(ctx, "failed to get campaign orders: %v", err)
		return nil, err
	}

	log.Infof(ctx, "found %d orders for campaign %s", len(orders), campaignCode)
	return orders, nil
}

// getCampaignFunnel 获取Campaign转化漏斗数据
func getCampaignFunnel(ctx context.Context, campaignCode string) (map[string]int64, error) {
	log.Debugf(ctx, "calculating funnel for campaign: %s", campaignCode)

	var result struct {
		Applied int64 `json:"applied"`
		Paid    int64 `json:"paid"`
	}

	err := db.Get().Model(&Order{}).
		Select("COUNT(*) as applied, COUNT(CASE WHEN is_paid = true THEN 1 END) as paid").
		Where("campaign_code = ?", campaignCode).
		Scan(&result).Error

	if err != nil {
		log.Errorf(ctx, "failed to get campaign funnel: %v", err)
		return nil, err
	}

	funnel := map[string]int64{
		"applied":   result.Applied,
		"paid":      result.Paid,
		"abandoned": result.Applied - result.Paid,
	}

	log.Debugf(ctx, "campaign funnel: applied=%d, paid=%d, abandoned=%d",
		funnel["applied"], funnel["paid"], funnel["abandoned"])

	return funnel, nil
}
