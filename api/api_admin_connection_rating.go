package center

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_admin_connection_rating_statistics returns connection quality stats.
// GET /app/connection-ratings/statistics?period=7d|30d|90d
func api_admin_connection_rating_statistics(c *gin.Context) {
	period := c.DefaultQuery("period", "7d")
	var days int
	switch period {
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	default:
		Error(c, ErrorInvalidArgument, "period must be 7d, 30d, or 90d")
		return
	}

	since := time.Now().AddDate(0, 0, -days)
	d := db.Get()

	var result ConnectionRatingStatisticsResponse

	// Summary
	if err := d.Model(&ConnectionRating{}).Where("created_at >= ?", since).Count(&result.Summary.Total).Error; err != nil {
		log.Errorf(c, "connection_rating_statistics: count total failed: %v", err)
		Error(c, ErrorSystemError, "failed to query statistics")
		return
	}
	if err := d.Model(&ConnectionRating{}).Where("created_at >= ? AND rating = ?", since, "good").Count(&result.Summary.Good).Error; err != nil {
		log.Errorf(c, "connection_rating_statistics: count good failed: %v", err)
		Error(c, ErrorSystemError, "failed to query statistics")
		return
	}
	result.Summary.Bad = result.Summary.Total - result.Summary.Good
	if result.Summary.Total > 0 {
		result.Summary.GoodRate = float64(result.Summary.Good) / float64(result.Summary.Total)
	}

	// Trend (daily)
	type trendRow struct {
		Date  string
		Total int64
		Good  int64
	}
	var trendRows []trendRow
	if err := d.Model(&ConnectionRating{}).
		Select("DATE(created_at) as date, COUNT(*) as total, SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) as good").
		Where("created_at >= ?", since).
		Group("DATE(created_at)").
		Order("date ASC").
		Find(&trendRows).Error; err != nil {
		log.Errorf(c, "connection_rating_statistics: trend query failed: %v", err)
		Error(c, ErrorSystemError, "failed to query statistics")
		return
	}

	result.Trend = make([]RatingTrendItem, len(trendRows))
	for i, r := range trendRows {
		bad := r.Total - r.Good
		var goodRate float64
		if r.Total > 0 {
			goodRate = float64(r.Good) / float64(r.Total)
		}
		result.Trend[i] = RatingTrendItem{
			Date: r.Date, Total: r.Total, Good: r.Good, Bad: bad, GoodRate: goodRate,
		}
	}

	// By server
	type serverRow struct {
		Domain  string
		Name    string
		Country string
		Total   int64
		Good    int64
	}
	var serverRows []serverRow
	if err := d.Model(&ConnectionRating{}).
		Select("server_domain as domain, MAX(server_name) as name, MAX(server_country) as country, COUNT(*) as total, SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) as good").
		Where("created_at >= ? AND server_domain != ''", since).
		Group("server_domain").
		Order("(SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) * 1.0 / COUNT(*)) ASC, total DESC").
		Limit(50).
		Find(&serverRows).Error; err != nil {
		log.Errorf(c, "connection_rating_statistics: by-server query failed: %v", err)
		Error(c, ErrorSystemError, "failed to query statistics")
		return
	}

	result.ByServer = make([]RatingByServer, len(serverRows))
	for i, r := range serverRows {
		var goodRate float64
		if r.Total > 0 {
			goodRate = float64(r.Good) / float64(r.Total)
		}
		result.ByServer[i] = RatingByServer{
			Domain: r.Domain, Name: r.Name, Country: r.Country,
			Total: r.Total, Good: r.Good, Bad: r.Total - r.Good, GoodRate: goodRate,
		}
	}

	// By ISP
	type ispRow struct {
		ISP     string
		Country string
		Total   int64
		Good    int64
	}
	var ispRows []ispRow
	if err := d.Model(&ConnectionRating{}).
		Select("isp, MAX(user_country) as country, COUNT(*) as total, SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) as good").
		Where("created_at >= ? AND isp != ''", since).
		Group("isp").
		Order("(SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) * 1.0 / COUNT(*)) ASC, total DESC").
		Limit(50).
		Find(&ispRows).Error; err != nil {
		log.Errorf(c, "connection_rating_statistics: by-isp query failed: %v", err)
		Error(c, ErrorSystemError, "failed to query statistics")
		return
	}

	result.ByISP = make([]RatingByISP, len(ispRows))
	for i, r := range ispRows {
		var goodRate float64
		if r.Total > 0 {
			goodRate = float64(r.Good) / float64(r.Total)
		}
		result.ByISP[i] = RatingByISP{
			ISP: r.ISP, Country: r.Country, Total: r.Total, Good: r.Good, Bad: r.Total - r.Good, GoodRate: goodRate,
		}
	}

	// By platform (os + app_version)
	type platformRow struct {
		OS         string
		AppVersion string
		Total      int64
		Good       int64
	}
	var platformRows []platformRow
	if err := d.Model(&ConnectionRating{}).
		Select("os, app_version, COUNT(*) as total, SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) as good").
		Where("created_at >= ? AND os != ''", since).
		Group("os, app_version").
		Order("(SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) * 1.0 / COUNT(*)) ASC, total DESC").
		Limit(30).
		Find(&platformRows).Error; err != nil {
		log.Errorf(c, "connection_rating_statistics: by-platform query failed: %v", err)
		Error(c, ErrorSystemError, "failed to query statistics")
		return
	}

	result.ByPlatform = make([]RatingByPlatform, len(platformRows))
	for i, r := range platformRows {
		var goodRate float64
		if r.Total > 0 {
			goodRate = float64(r.Good) / float64(r.Total)
		}
		result.ByPlatform[i] = RatingByPlatform{
			OS: r.OS, AppVersion: r.AppVersion, Total: r.Total, Good: r.Good, Bad: r.Total - r.Good, GoodRate: goodRate,
		}
	}

	// By user (top 50 worst good rate, minimum 3 ratings)
	type userRow struct {
		UserID uint64
		Total  int64
		Good   int64
	}
	var userRows []userRow
	if err := d.Model(&ConnectionRating{}).
		Select("user_id, COUNT(*) as total, SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) as good").
		Where("created_at >= ?", since).
		Group("user_id").
		Having("COUNT(*) >= 3").
		Order("(SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) * 1.0 / COUNT(*)) ASC, COUNT(*) DESC").
		Limit(50).
		Find(&userRows).Error; err != nil {
		log.Errorf(c, "connection_rating_statistics: by-user query failed: %v", err)
		Error(c, ErrorSystemError, "failed to query statistics")
		return
	}

	// Batch-load email identities for all user IDs (avoids N+1)
	userIDs := make([]uint64, len(userRows))
	for i, r := range userRows {
		userIDs[i] = r.UserID
	}
	var identifies []LoginIdentify
	if len(userIDs) > 0 {
		if err := d.Where("user_id IN ? AND type = ?", userIDs, "email").Find(&identifies).Error; err != nil {
			log.Errorf(c, "connection_rating_statistics: user identity query failed: %v", err)
			Error(c, ErrorSystemError, "failed to query statistics")
			return
		}
	}
	identifyMap := make(map[uint64]*LoginIdentify, len(identifies))
	for i := range identifies {
		identifyMap[uint64(identifies[i].UserID)] = &identifies[i]
	}

	result.ByUser = make([]RatingByUser, len(userRows))
	for i, r := range userRows {
		var goodRate float64
		if r.Total > 0 {
			goodRate = float64(r.Good) / float64(r.Total)
		}
		email := fmt.Sprintf("user#%d", r.UserID)
		if identify, ok := identifyMap[r.UserID]; ok {
			if dec, err := secretDecryptString(c, identify.EncryptedValue); err == nil {
				email = dec
			}
		}
		result.ByUser[i] = RatingByUser{
			UserID: r.UserID, Email: email,
			Total: r.Total, Good: r.Good, Bad: r.Total - r.Good, GoodRate: goodRate,
		}
	}

	log.Infof(c, "api_admin_connection_rating_statistics: period=%s total=%d goodRate=%.2f",
		period, result.Summary.Total, result.Summary.GoodRate)
	Success(c, &result)
}
