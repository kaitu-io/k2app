package center

import (
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
)

// UserStatisticsResponse contains aggregated user statistics
type UserStatisticsResponse struct {
	// Total counts
	TotalUsers int64 `json:"totalUsers"` // Total registered users
	PaidUsers  int64 `json:"paidUsers"`  // Users who have completed at least one order
	FreeUsers  int64 `json:"freeUsers"`  // Users who have not paid

	// Active subscription counts
	ActivePro   int64 `json:"activePro"`   // Users with valid pro subscription (expired_at > now)
	ExpiredPro  int64 `json:"expiredPro"`  // Users with expired pro subscription
	NeverHadPro int64 `json:"neverHadPro"` // Users who never had pro (expired_at = 0)

	// Retailer stats
	TotalRetailers int64 `json:"totalRetailers"` // Users who are retailers

	// Growth stats (new users)
	New24h int64 `json:"new24h"` // New users in last 24 hours
	New7d  int64 `json:"new7d"`  // New users in last 7 days
	New30d int64 `json:"new30d"` // New users in last 30 days

	// Registration breakdown by time periods
	ByRegistrationPeriod []PeriodCount `json:"byRegistrationPeriod"`
}

// OrderStatisticsResponse contains aggregated order statistics
type OrderStatisticsResponse struct {
	// Total counts
	TotalOrders int64 `json:"totalOrders"` // Total orders created
	PaidOrders  int64 `json:"paidOrders"`  // Paid orders
	UnpaidOrders int64 `json:"unpaidOrders"` // Unpaid/pending orders

	// Revenue stats (in cents)
	TotalRevenue int64 `json:"totalRevenue"` // Total revenue from paid orders
	Revenue24h   int64 `json:"revenue24h"`   // Revenue in last 24 hours
	Revenue7d    int64 `json:"revenue7d"`    // Revenue in last 7 days
	Revenue30d   int64 `json:"revenue30d"`   // Revenue in last 30 days

	// Order counts by time
	Orders24h int64 `json:"orders24h"` // Paid orders in last 24 hours
	Orders7d  int64 `json:"orders7d"`  // Paid orders in last 7 days
	Orders30d int64 `json:"orders30d"` // Paid orders in last 30 days

	// Conversion rate
	ConversionRate float64 `json:"conversionRate"` // Paid orders / Total orders

	// Average order value (in cents)
	AverageOrderValue int64 `json:"averageOrderValue"` // Average paid order value

	// Revenue by period (for chart)
	RevenueByPeriod []RevenuePeriod `json:"revenueByPeriod"`
}

// PeriodCount represents count for a specific time period
type PeriodCount struct {
	Period string `json:"period"` // e.g., "2024-01", "2024-01-15"
	Count  int64  `json:"count"`
}

// RevenuePeriod represents revenue for a specific time period
type RevenuePeriod struct {
	Period  string `json:"period"`  // e.g., "2024-01-15"
	Revenue int64  `json:"revenue"` // Revenue in cents
	Orders  int64  `json:"orders"`  // Number of paid orders
}

// api_admin_get_user_statistics returns aggregated user statistics
// GET /app/users/statistics
func api_admin_get_user_statistics(c *gin.Context) {
	var result UserStatisticsResponse
	now := time.Now()

	// Get total user count
	db.Get().Model(&User{}).Count(&result.TotalUsers)

	// Get paid users count (users who have completed first order)
	db.Get().Model(&User{}).Where("is_first_order_done = ?", true).Count(&result.PaidUsers)
	result.FreeUsers = result.TotalUsers - result.PaidUsers

	// Get active pro users (expired_at > now)
	nowUnix := now.Unix()
	db.Get().Model(&User{}).Where("expired_at > ?", nowUnix).Count(&result.ActivePro)

	// Get expired pro users (expired_at > 0 AND expired_at <= now)
	db.Get().Model(&User{}).Where("expired_at > 0 AND expired_at <= ?", nowUnix).Count(&result.ExpiredPro)

	// Get users who never had pro (expired_at = 0 or NULL)
	db.Get().Model(&User{}).Where("expired_at = 0 OR expired_at IS NULL").Count(&result.NeverHadPro)

	// Get retailer count
	db.Get().Model(&User{}).Where("is_retailer = ?", true).Count(&result.TotalRetailers)

	// Get new user counts for different periods
	h24Ago := now.Add(-24 * time.Hour)
	d7Ago := now.Add(-7 * 24 * time.Hour)
	d30Ago := now.Add(-30 * 24 * time.Hour)

	db.Get().Model(&User{}).Where("created_at >= ?", h24Ago).Count(&result.New24h)
	db.Get().Model(&User{}).Where("created_at >= ?", d7Ago).Count(&result.New7d)
	db.Get().Model(&User{}).Where("created_at >= ?", d30Ago).Count(&result.New30d)

	// Get registration breakdown by month (last 6 months)
	type monthResult struct {
		Month string
		Count int64
	}
	var monthCounts []monthResult
	sixMonthsAgo := now.AddDate(0, -6, 0)
	db.Get().Model(&User{}).
		Select("DATE_FORMAT(created_at, '%Y-%m') as month, COUNT(*) as count").
		Where("created_at >= ?", sixMonthsAgo).
		Group("DATE_FORMAT(created_at, '%Y-%m')").
		Order("month ASC").
		Find(&monthCounts)

	for _, mc := range monthCounts {
		result.ByRegistrationPeriod = append(result.ByRegistrationPeriod, PeriodCount{
			Period: mc.Month,
			Count:  mc.Count,
		})
	}

	Success(c, &result)
}

// api_admin_get_order_statistics returns aggregated order statistics
// GET /app/orders/statistics
func api_admin_get_order_statistics(c *gin.Context) {
	var result OrderStatisticsResponse
	now := time.Now()

	// Get total order count
	db.Get().Model(&Order{}).Count(&result.TotalOrders)

	// Get paid orders count
	db.Get().Model(&Order{}).Where("is_paid = ?", true).Count(&result.PaidOrders)
	result.UnpaidOrders = result.TotalOrders - result.PaidOrders

	// Calculate conversion rate
	if result.TotalOrders > 0 {
		result.ConversionRate = float64(result.PaidOrders) / float64(result.TotalOrders) * 100
	}

	// Get total revenue from paid orders
	var totalRevenue struct {
		Sum int64
	}
	db.Get().Model(&Order{}).
		Select("COALESCE(SUM(pay_amount), 0) as sum").
		Where("is_paid = ?", true).
		Scan(&totalRevenue)
	result.TotalRevenue = totalRevenue.Sum

	// Get revenue and order counts for time periods
	h24Ago := now.Add(-24 * time.Hour)
	d7Ago := now.Add(-7 * 24 * time.Hour)
	d30Ago := now.Add(-30 * 24 * time.Hour)

	// 24 hours
	var stats24h struct {
		Revenue int64
		Count   int64
	}
	db.Get().Model(&Order{}).
		Select("COALESCE(SUM(pay_amount), 0) as revenue, COUNT(*) as count").
		Where("is_paid = ? AND paid_at >= ?", true, h24Ago).
		Scan(&stats24h)
	result.Revenue24h = stats24h.Revenue
	result.Orders24h = stats24h.Count

	// 7 days
	var stats7d struct {
		Revenue int64
		Count   int64
	}
	db.Get().Model(&Order{}).
		Select("COALESCE(SUM(pay_amount), 0) as revenue, COUNT(*) as count").
		Where("is_paid = ? AND paid_at >= ?", true, d7Ago).
		Scan(&stats7d)
	result.Revenue7d = stats7d.Revenue
	result.Orders7d = stats7d.Count

	// 30 days
	var stats30d struct {
		Revenue int64
		Count   int64
	}
	db.Get().Model(&Order{}).
		Select("COALESCE(SUM(pay_amount), 0) as revenue, COUNT(*) as count").
		Where("is_paid = ? AND paid_at >= ?", true, d30Ago).
		Scan(&stats30d)
	result.Revenue30d = stats30d.Revenue
	result.Orders30d = stats30d.Count

	// Calculate average order value
	if result.PaidOrders > 0 {
		result.AverageOrderValue = result.TotalRevenue / result.PaidOrders
	}

	// Get revenue by day (last 30 days)
	type dailyResult struct {
		Day     string
		Revenue int64
		Count   int64
	}
	var dailyCounts []dailyResult
	db.Get().Model(&Order{}).
		Select("DATE_FORMAT(paid_at, '%Y-%m-%d') as day, COALESCE(SUM(pay_amount), 0) as revenue, COUNT(*) as count").
		Where("is_paid = ? AND paid_at >= ?", true, d30Ago).
		Group("DATE_FORMAT(paid_at, '%Y-%m-%d')").
		Order("day ASC").
		Find(&dailyCounts)

	for _, dc := range dailyCounts {
		result.RevenueByPeriod = append(result.RevenueByPeriod, RevenuePeriod{
			Period:  dc.Day,
			Revenue: dc.Revenue,
			Orders:  dc.Count,
		})
	}

	Success(c, &result)
}
