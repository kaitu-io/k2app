package center

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/rs/xid"
	gormdb "gorm.io/gorm"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// CreateLicenseKeyBatch creates a batch record and generates all keys in a single transaction.
func CreateLicenseKeyBatch(ctx context.Context, req *CreateLicenseKeyBatchRequest, adminUserID uint64) (*LicenseKeyBatch, error) {
	expiresAt := time.Now().AddDate(0, 0, req.ExpiresInDays).Unix()

	batch := LicenseKeyBatch{
		Name:             req.Name,
		SourceTag:        req.SourceTag,
		RecipientMatcher: req.RecipientMatcher,
		PlanDays:         req.PlanDays,
		Quantity:         req.Quantity,
		ExpiresAt:        expiresAt,
		Note:             req.Note,
		CreatedByUserID:  adminUserID,
	}

	err := db.Get().Transaction(func(tx *gormdb.DB) error {
		if err := tx.Create(&batch).Error; err != nil {
			return fmt.Errorf("create batch: %w", err)
		}

		keys := make([]LicenseKey, 0, batch.Quantity)
		for i := 0; i < batch.Quantity; i++ {
			code, err := GenerateShortCode(ctx)
			if err != nil {
				return fmt.Errorf("generate code for key %d: %w", i+1, err)
			}
			keys = append(keys, LicenseKey{
				UUID:      xid.New().String(),
				Code:      code,
				BatchID:   batch.ID,
				PlanDays:  batch.PlanDays,
				ExpiresAt: batch.ExpiresAt,
			})
		}

		batchSize := 100
		for i := 0; i < len(keys); i += batchSize {
			end := min(i+batchSize, len(keys))
			if err := tx.CreateInBatches(keys[i:end], batchSize).Error; err != nil {
				return fmt.Errorf("batch insert at offset %d: %w", i, err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	log.Infof(ctx, "[LICENSE_KEY_BATCH] created batch=%d name=%q keys=%d", batch.ID, batch.Name, batch.Quantity)
	return &batch, nil
}


// batchKeyCount is used for grouped COUNT queries.
type batchKeyCount struct {
	BatchID uint64 `gorm:"column:batch_id"`
	Count   int64  `gorm:"column:count"`
}

// batchBaseCounts returns redeemed and expired counts for a single batch.
func batchBaseCounts(batchID uint64) (redeemed, expired int64) {
	now := time.Now().Unix()
	db.Get().Model(&LicenseKey{}).Where("batch_id = ? AND is_used = true", batchID).Count(&redeemed)
	db.Get().Model(&LicenseKey{}).Where("batch_id = ? AND is_used = false AND expires_at < ?", batchID, now).Count(&expired)
	return
}

// batchBaseCountsBatch returns redeemed and expired counts for multiple batches in 2 queries.
func batchBaseCountsBatch(batchIDs []uint64) (redeemedMap, expiredMap map[uint64]int64) {
	redeemedMap = make(map[uint64]int64)
	expiredMap = make(map[uint64]int64)
	if len(batchIDs) == 0 {
		return
	}

	var redeemed []batchKeyCount
	db.Get().Model(&LicenseKey{}).
		Select("batch_id, COUNT(*) as count").
		Where("batch_id IN ? AND is_used = true", batchIDs).
		Group("batch_id").
		Scan(&redeemed)
	for _, r := range redeemed {
		redeemedMap[r.BatchID] = r.Count
	}

	now := time.Now().Unix()
	var expired []batchKeyCount
	db.Get().Model(&LicenseKey{}).
		Select("batch_id, COUNT(*) as count").
		Where("batch_id IN ? AND is_used = false AND expires_at < ?", batchIDs, now).
		Group("batch_id").
		Scan(&expired)
	for _, e := range expired {
		expiredMap[e.BatchID] = e.Count
	}
	return
}

// redeemInfo holds the minimum data needed for conversion calculation.
type redeemInfo struct {
	UsedByUserID uint64    `gorm:"column:used_by_user_id"`
	UsedAt       time.Time `gorm:"column:used_at"`
}

// calcConversion computes converted users and revenue from a set of redeemed keys.
// Uses two GORM struct queries + Go-side matching (no raw SQL).
func calcConversion(redeems []redeemInfo) (convertedUsers int64, revenue uint64) {
	if len(redeems) == 0 {
		return 0, 0
	}

	userIDs := make([]uint64, 0, len(redeems))
	userRedeemTime := make(map[uint64]time.Time, len(redeems))
	for _, r := range redeems {
		userIDs = append(userIDs, r.UsedByUserID)
		userRedeemTime[r.UsedByUserID] = r.UsedAt
	}

	var orders []Order
	db.Get().Where("user_id IN ? AND is_paid = true", userIDs).Find(&orders)

	convertedSet := make(map[uint64]bool)
	for _, o := range orders {
		redeemTime, ok := userRedeemTime[o.UserID]
		if ok && o.CreatedAt.After(redeemTime) {
			convertedSet[o.UserID] = true
			revenue += o.PayAmount
		}
	}
	return int64(len(convertedSet)), revenue
}

// GetBatchStats computes redemption + conversion stats for a single batch.
func GetBatchStats(ctx context.Context, batchID uint64) (*BatchStatsResponse, error) {
	var batch LicenseKeyBatch
	if err := db.Get().First(&batch, batchID).Error; err != nil {
		return nil, err
	}

	redeemed, expired := batchBaseCounts(batchID)

	var redeems []redeemInfo
	db.Get().Model(&LicenseKey{}).
		Select("used_by_user_id, used_at").
		Where("batch_id = ? AND is_used = true AND used_by_user_id IS NOT NULL", batchID).
		Scan(&redeems)

	convertedUsers, totalRevenue := calcConversion(redeems)

	total := int64(batch.Quantity)
	redeemRate := float64(0)
	if total > 0 {
		redeemRate = float64(redeemed) / float64(total)
	}
	conversionRate := float64(0)
	if redeemed > 0 {
		conversionRate = float64(convertedUsers) / float64(redeemed)
	}

	return &BatchStatsResponse{
		BatchID:        batch.ID,
		Name:           batch.Name,
		SourceTag:      batch.SourceTag,
		TotalKeys:      total,
		Redeemed:       redeemed,
		Expired:        expired,
		RedeemRate:     redeemRate,
		ConvertedUsers: convertedUsers,
		ConversionRate: conversionRate,
		Revenue:        totalRevenue,
	}, nil
}

// GetAllBatchStats computes stats for all non-deleted batches.
func GetAllBatchStats(ctx context.Context) ([]BatchStatsResponse, error) {
	var batches []LicenseKeyBatch
	if err := db.Get().Order("created_at DESC").Find(&batches).Error; err != nil {
		return nil, err
	}

	results := make([]BatchStatsResponse, 0, len(batches))
	for _, b := range batches {
		stats, err := GetBatchStats(ctx, b.ID)
		if err != nil {
			log.Warnf(ctx, "[LICENSE_KEY_BATCH] failed to get stats for batch %d: %v", b.ID, err)
			continue
		}
		results = append(results, *stats)
	}
	return results, nil
}

// GetBatchStatsBySource aggregates stats by source_tag using GORM + Go aggregation.
func GetBatchStatsBySource(ctx context.Context) ([]BatchStatsBySourceResponse, error) {
	var batches []LicenseKeyBatch
	if err := db.Get().Order("source_tag").Find(&batches).Error; err != nil {
		return nil, err
	}

	type sourceGroup struct {
		totalKeys int64
		batchIDs  []uint64
	}
	groups := make(map[string]*sourceGroup)
	for _, b := range batches {
		g, ok := groups[b.SourceTag]
		if !ok {
			g = &sourceGroup{}
			groups[b.SourceTag] = g
		}
		g.totalKeys += int64(b.Quantity)
		g.batchIDs = append(g.batchIDs, b.ID)
	}

	results := make([]BatchStatsBySourceResponse, 0, len(groups))
	for tag, g := range groups {
		var redeemed int64
		db.Get().Model(&LicenseKey{}).Where("batch_id IN ? AND is_used = true", g.batchIDs).Count(&redeemed)

		var redeems []redeemInfo
		db.Get().Model(&LicenseKey{}).
			Select("used_by_user_id, used_at").
			Where("batch_id IN ? AND is_used = true AND used_by_user_id IS NOT NULL", g.batchIDs).
			Scan(&redeems)

		convertedUsers, rev := calcConversion(redeems)

		redeemRate := float64(0)
		if g.totalKeys > 0 {
			redeemRate = float64(redeemed) / float64(g.totalKeys)
		}
		convRate := float64(0)
		if redeemed > 0 {
			convRate = float64(convertedUsers) / float64(redeemed)
		}

		results = append(results, BatchStatsBySourceResponse{
			SourceTag:      tag,
			TotalKeys:      g.totalKeys,
			Redeemed:       redeemed,
			RedeemRate:     redeemRate,
			ConvertedUsers: convertedUsers,
			ConversionRate: convRate,
			Revenue:        rev,
		})
	}
	return results, nil
}

// GetBatchStatsTrend returns daily redemption counts for the last N days.
func GetBatchStatsTrend(ctx context.Context, days int) ([]BatchStatsTrendResponse, error) {
	if days <= 0 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days)

	var redeems []redeemInfo
	db.Get().Model(&LicenseKey{}).
		Select("used_by_user_id, used_at").
		Where("is_used = true AND used_at >= ? AND batch_id > 0", since).
		Scan(&redeems)

	// Group by date in Go
	type dayData struct {
		redeemed int64
		userIDs  map[uint64]time.Time
	}
	dayMap := make(map[string]*dayData)
	for _, r := range redeems {
		date := r.UsedAt.Format("2006-01-02")
		d, ok := dayMap[date]
		if !ok {
			d = &dayData{userIDs: make(map[uint64]time.Time)}
			dayMap[date] = d
		}
		d.redeemed++
		d.userIDs[r.UsedByUserID] = r.UsedAt
	}

	// Collect all unique user IDs for batch order query
	allUserIDs := make(map[uint64]time.Time)
	for _, r := range redeems {
		allUserIDs[r.UsedByUserID] = r.UsedAt
	}

	userIDSlice := make([]uint64, 0, len(allUserIDs))
	for uid := range allUserIDs {
		userIDSlice = append(userIDSlice, uid)
	}

	paidUsers := make(map[uint64]bool)
	if len(userIDSlice) > 0 {
		var orders []Order
		db.Get().Where("user_id IN ? AND is_paid = true", userIDSlice).Find(&orders)
		for _, o := range orders {
			redeemTime, ok := allUserIDs[o.UserID]
			if ok && o.CreatedAt.After(redeemTime) {
				paidUsers[o.UserID] = true
			}
		}
	}

	results := make([]BatchStatsTrendResponse, 0, len(dayMap))
	for date, d := range dayMap {
		var converted int64
		for uid := range d.userIDs {
			if paidUsers[uid] {
				converted++
			}
		}
		results = append(results, BatchStatsTrendResponse{
			Date:           date,
			Redeemed:       d.redeemed,
			ConvertedUsers: converted,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Date < results[j].Date
	})

	return results, nil
}
