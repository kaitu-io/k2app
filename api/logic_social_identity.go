package center

import (
	"context"
	"errors"
	"fmt"
	"strings"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 第三方身份（Google / Apple）与账号的匹配、绑定、解绑（spec §4）。
//
// 身份行就是 LoginIdentify：Type=provider、IndexID=secretHashIt(provider+":"+sub)、
// EncryptedValue=secretEncryptString(email)（无 email 存 ""）、Brand=请求品牌。
// 唯一索引 (type, index_id, brand) 保证一个 (provider, sub, brand) 只属于一个用户；
// "一个用户每种 provider 最多一个" 由写入事务内对 users 行加 FOR UPDATE 串行化保证。

var (
	errSocialEmailUnverified      = errors.New("social email unverified")
	errSocialIdentityTaken        = errors.New("social identity taken")
	errSocialEmailInUse           = errors.New("social email already in use by another user")
	errSocialProviderAlreadyBound = errors.New("social provider already bound")
)

// socialMaxAttempts 唯一键冲突后重走匹配的次数上限（并发首登时败者重查即可命中胜者的行）。
const socialMaxAttempts = 3

func socialIdentityIndexID(ctx context.Context, provider, sub string) string {
	return secretHashIt(ctx, []byte(provider+":"+sub))
}

func socialEmailIndexID(ctx context.Context, email string) string {
	return secretHashIt(ctx, []byte(normalizeSocialEmail(email)))
}

func normalizeSocialEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateSocialArgs(brand Brand, provider, sub string) error {
	if !brand.Valid() {
		return fmt.Errorf("invalid brand %q", brand)
	}
	if provider != SocialProviderGoogle && provider != SocialProviderApple {
		return fmt.Errorf("unknown social provider %q", provider)
	}
	if sub == "" {
		return fmt.Errorf("empty social sub")
	}
	return nil
}

// findSocialIdentity 第 1 步：按 (provider, hash(sub), brand) 查身份行。
func findSocialIdentity(ctx context.Context, tx *gorm.DB, brand Brand, provider, sub string) (*LoginIdentify, error) {
	var row LoginIdentify
	err := tx.Where("type = ? AND index_id = ? AND brand = ?", provider, socialIdentityIndexID(ctx, provider, sub), string(brand)).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// findEmailIdentity 按 (email, hash(email), brand) 查 email 身份行。
func findEmailIdentity(ctx context.Context, tx *gorm.DB, brand Brand, email string) (*LoginIdentify, error) {
	var row LoginIdentify
	err := tx.Where("type = ? AND index_id = ? AND brand = ?", "email", socialEmailIndexID(ctx, email), string(brand)).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// resolveSocialUser 按 spec §4 三步把已校验的第三方身份落到本品牌的一个用户上。
// 返回用户 id；created=true 表示本次调用新建了用户。
func resolveSocialUser(ctx context.Context, brand Brand, cl *socialClaims, lang ...string) (userID uint64, created bool, err error) {
	if cl == nil {
		return 0, false, fmt.Errorf("nil social claims")
	}
	if err := validateSocialArgs(brand, cl.Provider, cl.Sub); err != nil {
		return 0, false, err
	}
	for attempt := 0; attempt < socialMaxAttempts; attempt++ {
		userID, created, err = resolveSocialUserOnce(ctx, brand, cl, lang...)
		if err == nil || !isDuplicateKeyErr(err) {
			return userID, created, err
		}
		log.Infof(ctx, "social resolve: unique conflict for %s in %s, retrying (attempt %d)", cl.Provider, brand, attempt+1)
	}
	return 0, false, err
}

func resolveSocialUserOnce(ctx context.Context, brand Brand, cl *socialClaims, lang ...string) (uint64, bool, error) {
	tx := db.Get().WithContext(ctx)

	// 1. 身份已存在 → 该用户
	if row, err := findSocialIdentity(ctx, tx, brand, cl.Provider, cl.Sub); err == nil {
		return row.UserID, false, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, err
	}

	// 邮箱不可信 = 视为无邮箱，只能走第 1 步
	email := normalizeSocialEmail(cl.Email)
	if !cl.EmailTrusted || email == "" {
		return 0, false, errSocialEmailUnverified
	}

	// 2. 本品牌已有该邮箱的账号 → 该用户，并补写第三方身份行
	emailRow, err := findEmailIdentity(ctx, tx, brand, email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, err
	}
	if err == nil {
		uid, err := attachSocialIdentityToMatchedUser(ctx, brand, emailRow.UserID, cl)
		return uid, false, err
	}

	// 3. 新建用户（同时生成 email 身份行），再写第三方身份行
	user, created, err := findOrCreateUserByEmail(ctx, brand, email, lang...)
	if err != nil {
		return 0, false, err
	}
	if user == nil { // email 行指向已软删用户（孤儿行）
		return 0, false, fmt.Errorf("email identity points to missing user")
	}
	uid, err := attachSocialIdentityToMatchedUser(ctx, brand, user.ID, cl)
	return uid, created, err
}

// attachSocialIdentityToMatchedUser 匹配流程里给第 2/3 步命中的用户补写身份。
// 该用户已有另一个同 provider 身份 → 不覆盖，直接返回该用户；
// 身份行唯一键冲突 → 重查第 1 步，返回真正持有该身份的用户。
func attachSocialIdentityToMatchedUser(ctx context.Context, brand Brand, userID uint64, cl *socialClaims) (uint64, error) {
	err := writeSocialIdentity(ctx, brand, userID, cl)
	switch {
	case err == nil, errors.Is(err, errSocialProviderAlreadyBound):
		return userID, nil
	case errors.Is(err, errSocialIdentityTaken), isDuplicateKeyErr(err):
		row, ferr := findSocialIdentity(ctx, db.Get().WithContext(ctx), brand, cl.Provider, cl.Sub)
		if ferr != nil {
			return 0, err // 冲突却查不到：唯一键错误由上层重试；errSocialIdentityTaken 不重试，登录会拿到 409002
		}
		return row.UserID, nil
	default:
		return 0, err
	}
}

// bindSocialIdentity 已登录用户绑定第三方身份。已绑同一身份 → nil（幂等）。
//   - 身份已属他人 → errSocialIdentityTaken
//   - 本用户已绑另一个同 provider 身份 → errSocialProviderAlreadyBound
//   - 仅当本用户还没有 email 身份（绑定要写 email 行）时：邮箱可信且已是本品牌别的用户的
//     email 身份 → errSocialEmailInUse（→409001，不合并账号）；未被占用 → 同一事务写入 email 身份行
//   - 本用户已有 email 身份 → 不检查邮箱，只写第三方身份行
func bindSocialIdentity(ctx context.Context, brand Brand, userID uint64, cl *socialClaims) error {
	if cl == nil {
		return fmt.Errorf("nil social claims")
	}
	if err := validateSocialArgs(brand, cl.Provider, cl.Sub); err != nil {
		return err
	}
	var err error
	for attempt := 0; attempt < socialMaxAttempts; attempt++ {
		err = writeSocialIdentity(ctx, brand, userID, cl)
		if err == nil || !isDuplicateKeyErr(err) {
			return err
		}
		// 并发写入撞唯一键：重走一遍，按已提交的状态给出确定结果
	}
	return err
}

// writeSocialIdentity 在一个事务里（锁住 users 行）为 userID 写第三方身份行，
// 必要时一并写 email 身份行。冲突以哨兵错误返回；唯一键冲突原样返回（err 满足 isDuplicateKeyErr）。
func writeSocialIdentity(ctx context.Context, brand Brand, userID uint64, cl *socialClaims) error {
	email := normalizeSocialEmail(cl.Email)
	trusted := cl.EmailTrusted && email != ""
	indexID := socialIdentityIndexID(ctx, cl.Provider, cl.Sub)

	encEmail := ""
	if email != "" {
		var err error
		if encEmail, err = secretEncryptString(ctx, email); err != nil {
			return fmt.Errorf("encrypt social email: %w", err)
		}
	}

	return db.Get().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 锁用户行：同一用户的身份写入串行化（"每种 provider 最多一个" 无唯一索引兜底）。
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("brand = ?", string(brand)).
			Select("id").First(&user, userID).Error; err != nil {
			return err
		}

		// ① 身份已存在
		if row, err := findSocialIdentity(ctx, tx, brand, cl.Provider, cl.Sub); err == nil {
			if row.UserID == userID {
				return nil
			}
			return errSocialIdentityTaken
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		// 本用户已有另一个同 provider 身份
		var n int64
		if err := tx.Model(&LoginIdentify{}).
			Where("user_id = ? AND type = ? AND brand = ?", userID, cl.Provider, string(brand)).
			Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return errSocialProviderAlreadyBound
		}

		// ② 无 email 身份的用户 + 可信邮箱：绑定会写 email 行 → 邮箱属别人则拒绝，无主则一并写入。
		// 已有 email 身份的用户不写 email 行，邮箱归属无关紧要。
		if trusted {
			var emailCount int64
			if err := tx.Model(&LoginIdentify{}).
				Where("user_id = ? AND type = ? AND brand = ?", userID, "email", string(brand)).
				Count(&emailCount).Error; err != nil {
				return err
			}
			if emailCount == 0 {
				_, err := findEmailIdentity(ctx, tx, brand, email)
				switch {
				case err == nil:
					return errSocialEmailInUse
				case errors.Is(err, gorm.ErrRecordNotFound):
					if err := tx.Create(&LoginIdentify{
						UserID:         userID,
						Type:           "email",
						IndexID:        socialEmailIndexID(ctx, email),
						EncryptedValue: encEmail,
						Brand:          string(brand),
					}).Error; err != nil {
						return err
					}
				default:
					return err
				}
			}
		}

		return tx.Create(&LoginIdentify{
			UserID:         userID,
			Type:           cl.Provider,
			IndexID:        indexID,
			EncryptedValue: encEmail,
			Brand:          string(brand),
		}).Error
	})
}

// unbindSocialIdentity 物理删除本用户在本品牌的 provider 身份行并返回被删行（Apple 撤销用）。
// 未绑 → gorm.ErrRecordNotFound。
func unbindSocialIdentity(ctx context.Context, brand Brand, userID uint64, provider string) (*LoginIdentify, error) {
	if !brand.Valid() {
		return nil, fmt.Errorf("invalid brand %q", brand)
	}
	if provider != SocialProviderGoogle && provider != SocialProviderApple {
		return nil, fmt.Errorf("unknown social provider %q", provider)
	}
	var row LoginIdentify
	err := db.Get().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND type = ? AND brand = ?", userID, provider, string(brand)).
			First(&row).Error; err != nil {
			return err
		}
		return tx.Delete(&LoginIdentify{}, row.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// saveAppleRefreshToken 把换得的 Apple refresh token（经 secretEncryptString；目前该函数是直通，库中为明文，无 .p8 私钥不可用）与 client_id 存到
// (apple, hash("apple:"+sub), brand) 身份行上。行不存在 → gorm.ErrRecordNotFound。
func saveAppleRefreshToken(ctx context.Context, brand Brand, sub, clientID, refreshToken string) error {
	if err := validateSocialArgs(brand, SocialProviderApple, sub); err != nil {
		return err
	}
	enc, err := secretEncryptString(ctx, refreshToken)
	if err != nil {
		return fmt.Errorf("encrypt apple refresh token: %w", err)
	}
	tx := db.Get().WithContext(ctx)
	row, err := findSocialIdentity(ctx, tx, brand, SocialProviderApple, sub)
	if err != nil {
		return err
	}
	return tx.Model(&LoginIdentify{}).Where("id = ?", row.ID).Updates(map[string]any{
		"provider_client_id":     clientID,
		"provider_refresh_token": enc,
	}).Error
}
