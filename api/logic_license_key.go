package center

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/xid"
	gormdb "gorm.io/gorm"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

const licenseKeyTTLDays = 30

// IsExpired reports whether the key has passed its expiry time.
func (k *LicenseKey) IsExpired() bool {
	return k.ExpiresAt < time.Now().Unix()
}

// MatchLicenseKey checks whether user is eligible to redeem this key.
func MatchLicenseKey(key *LicenseKey, user *User) bool {
	switch key.RecipientMatcher {
	case "all":
		return true
	case "never_paid":
		return user.IsFirstOrderDone == nil || !*user.IsFirstOrderDone
	default:
		return false
	}
}

// RedeemLicenseKey validates, consumes, and grants plan access to the user.
// Runs inside a DB transaction.
func RedeemLicenseKey(ctx context.Context, uuid string, userID uint64) (*LicenseKey, *UserProHistory, error) {
	var history *UserProHistory
	var key *LicenseKey

	err := db.Get().Transaction(func(tx *gormdb.DB) error {
		// 1. Load key
		var k LicenseKey
		if err := tx.Where("uuid = ?", uuid).First(&k).Error; err != nil {
			return ErrLicenseKeyNotFound
		}
		if k.IsUsed {
			return ErrLicenseKeyUsed
		}
		if k.IsExpired() {
			return ErrLicenseKeyExpired
		}

		// 2. Anti-abuse: one key per user ever
		var existingCount int64
		if err := tx.Model(&LicenseKey{}).Where("used_by_user_id = ?", userID).Count(&existingCount).Error; err != nil {
			return err
		}
		if existingCount > 0 {
			return ErrLicenseKeyUsed
		}

		// 3. Load user
		var user User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}

		// 4. Check eligibility
		if !MatchLicenseKey(&k, &user) {
			return ErrLicenseKeyNotMatch
		}

		// 5. Atomic consume
		result := tx.Model(&LicenseKey{}).
			Where("uuid = ? AND is_used = false", uuid).
			Updates(map[string]any{
				"is_used":           true,
				"used_by_user_id":   userID,
				"used_at":           time.Now(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrLicenseKeyUsed
		}

		// 6. Grant plan days
		reason := fmt.Sprintf("礼物码兑换 - %s", k.UUID)
		h, err := addProExpiredDays(ctx, tx, &user, VipSystemGrant, k.ID, k.PlanDays, reason)
		if err != nil {
			return err
		}

		k.IsUsed = true
		k.UsedByUserID = &userID
		key = &k
		history = h
		return nil
	})

	return key, history, err
}

// GetLicenseKeyByUUID fetches a key by its UUID (includes soft-deleted check via GORM).
func GetLicenseKeyByUUID(ctx context.Context, uuid string) (*LicenseKey, error) {
	var key LicenseKey
	if err := db.Get().Where("uuid = ?", uuid).First(&key).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

// ConsumeLicenseKey atomically marks a key as used.
// Uses conditional UPDATE to prevent concurrent double-redemption.
func ConsumeLicenseKey(ctx context.Context, tx *gormdb.DB, uuid string, userID uint64) (*LicenseKey, error) {
	var key LicenseKey
	if err := tx.Where("uuid = ?", uuid).First(&key).Error; err != nil {
		return nil, ErrLicenseKeyNotFound
	}
	if key.IsUsed {
		return nil, ErrLicenseKeyUsed
	}
	if key.IsExpired() {
		return nil, ErrLicenseKeyExpired
	}

	// 同一用户只能使用一个 LicenseKey
	var existingUseCount int64
	if err := tx.Model(&LicenseKey{}).Where("used_by_user_id = ?", userID).Count(&existingUseCount).Error; err != nil {
		return nil, err
	}
	if existingUseCount > 0 {
		return nil, ErrLicenseKeyNotMatch
	}

	now := time.Now()
	result := tx.Model(&LicenseKey{}).
		Where("uuid = ? AND is_used = false", uuid).
		Updates(map[string]any{
			"is_used":           true,
			"used_by_user_id":   userID,
			"used_at":           now,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrLicenseKeyUsed
	}

	key.IsUsed = true
	key.UsedByUserID = &userID
	key.UsedAt = &now
	return &key, nil
}

// GenerateLicenseKeysForCampaign generates N keys per eligible user and inserts them in batches.
func GenerateLicenseKeysForCampaign(ctx context.Context, campaign *Campaign) (int64, error) {
	if !campaign.IsShareable || campaign.SharesPerUser <= 0 {
		return 0, fmt.Errorf("campaign is not shareable or sharesPerUser is 0")
	}

	users, err := queryEligibleUsers(ctx, campaign)
	if err != nil {
		return 0, fmt.Errorf("query eligible users: %w", err)
	}
	if len(users) == 0 {
		log.Infof(ctx, "[LICENSE_KEY] campaign=%d no eligible users found", campaign.ID)
		return 0, nil
	}

	expiresAt := time.Now().AddDate(0, 0, licenseKeyTTLDays).Unix()
	var keys []LicenseKey
	for _, user := range users {
		userID := user.ID
		campaignID := campaign.ID
		for i := int64(0); i < campaign.SharesPerUser; i++ {
			keys = append(keys, LicenseKey{
				UUID:             xid.New().String(),
				PlanDays:         licenseKeyTTLDays,
				RecipientMatcher: "never_paid",
				ExpiresAt:        expiresAt,
				CampaignID:       &campaignID,
				CreatedByUserID:  &userID,
			})
		}
	}

	batchSize := 100
	for i := 0; i < len(keys); i += batchSize {
		end := min(i+batchSize, len(keys))
		if err := db.Get().CreateInBatches(keys[i:end], batchSize).Error; err != nil {
			return int64(i), fmt.Errorf("batch insert at offset %d: %w", i, err)
		}
	}

	log.Infof(ctx, "[LICENSE_KEY] campaign=%d generated=%d keys for %d users",
		campaign.ID, len(keys), len(users))
	return int64(len(keys)), nil
}

// CountEligibleUsers returns how many users qualify for the campaign (for dryRun preview).
func CountEligibleUsers(ctx context.Context, campaign *Campaign) (int64, error) {
	users, err := queryEligibleUsers(ctx, campaign)
	if err != nil {
		return 0, err
	}
	return int64(len(users)), nil
}

// queryEligibleUsers pages through all users and filters by campaign matcher.
func queryEligibleUsers(ctx context.Context, campaign *Campaign) ([]User, error) {
	matcher := getCampaignMatcherWithDB(db.Get(), campaign.MatcherType, campaign.MatcherParams)
	if matcher == nil {
		return nil, fmt.Errorf("unknown matcherType: %s", campaign.MatcherType)
	}

	var result []User
	page := 0
	pageSize := 500
	for {
		var batch []User
		if err := db.Get().Offset(page * pageSize).Limit(pageSize).Find(&batch).Error; err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		for i := range batch {
			if matcher(ctx, &batch[i], nil) {
				result = append(result, batch[i])
			}
		}
		if len(batch) < pageSize {
			break
		}
		page++
	}
	return result, nil
}
