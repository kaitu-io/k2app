package center

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// userHasIdentity 判定用户是否有登录身份（login_identifies 至少一行）。
// 零身份 = 设备账号；这是唯一判定入口，别处不要手写计数。
func userHasIdentity(tx *gorm.DB, userID uint64) (bool, error) {
	var n int64
	if err := tx.Model(&LoginIdentify{}).Where("user_id = ?", userID).Limit(1).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

const (
	funnelEventDeviceAccountCreated = "device_account_created"
	funnelEventIdentityBound        = "identity_bound"
)

// recordIdentityBound 零身份用户补上第一个登录身份后记 identity_bound（spec §4.7 后置观察）。
// 「是不是第一个」由调用方在写身份行前用 userHasIdentity 判定（写完之后已无从区分）；
// 只在那次判定为零身份、且写入已提交后调用。user 只需 ID 与 Brand。
func recordIdentityBound(ctx context.Context, user *User) {
	if user == nil || user.ID == 0 {
		return
	}
	funnelEnqueue(FunnelEvent{
		Brand:   user.Brand,
		Surface: FunnelSurfaceApp,
		Event:   funnelEventIdentityBound,
		UserID:  user.ID,
	})
	log.Infof(ctx, "identity bound: user %d got first login identity", user.ID)
}

type deviceAccountResult struct {
	Auth    *DataAuthResult
	UserID  uint64
	Created bool
}

// deviceAccountSignIn 按 udid 建零身份用户 + 设备（spec §4.2）。设备行已存在时**不凭 udid 签发
// token**（udid 不是秘密）：属有身份用户 → 403005 请登录；属设备账号 → 403006 要凭证（重装靠
// Keychain 里的 token，重置 / 换手机靠 deviceAccountRestore）。udid 唯一冲突（并发建号）原样返回。
func deviceAccountSignIn(c *gin.Context, brand Brand, req DataDeviceAccountRequest) (*deviceAccountResult, error) {
	res := &deviceAccountResult{}
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		var device Device
		findErr := tx.Where("udid = ?", req.UDID).First(&device).Error
		if findErr != nil && !util.DbIsNotFoundErr(findErr) {
			return findErr
		}
		if findErr == nil {
			has, err := userHasIdentity(tx, device.UserID)
			if err != nil {
				return err
			}
			if has {
				return e(ErrorDeviceAccountLoginRequired, "device belongs to an account, login required")
			}
			return e(ErrorDeviceAccountProofRequired, "device belongs to a device account, proof required")
		}

		cc := CountryFromGinContext(c)
		user := User{
			UUID:                generateId("user"),
			ExpiredAt:           0,
			Language:            detectUserLanguage(c, req.Language, "", c.GetHeader("Accept-Language")),
			RegistrationCountry: cc,
			CurrentCountry:      cc,
			Brand:               string(brand),
			IsActivated:         BoolPtr(true),
			ActivatedAt:         time.Now().Unix(),
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		auth, issueAt, err := generateTokens(c, user.ID, req.UDID, user.Roles)
		if err != nil {
			return err
		}
		device = Device{
			UDID:            req.UDID,
			Remark:          req.Remark,
			UserID:          user.ID,
			TokenIssueAt:    issueAt.Unix(),
			TokenLastUsedAt: time.Now().Unix(),
		}
		createDeviceWithAppInfo(c, &device)
		if err := tx.Create(&device).Error; err != nil {
			return err
		}
		res.Auth, res.UserID, res.Created = auth, user.ID, true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// deviceAccountRestore 凭 Apple 交易把零身份设备账号搬到 udid 这台设备（spec §4.2.1）。
// 调用方已验过 JWS、并向 Apple 复核了交易（info）。主人 = 这条订阅链订阅行上的用户；
// 搬是移动不是共享：主人的其他设备行全删（零身份上限 1）。所有拒绝都不写库。
func deviceAccountRestore(c *gin.Context, brand Brand, udid, remark string, info *appstore.TransactionInfo) (*deviceAccountResult, error) {
	res := &deviceAccountResult{}
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		var sub Subscription
		if err := tx.Where(&Subscription{Provider: SubscriptionProviderApple, ProviderSubscriptionID: info.OriginalTransactionId}).
			First(&sub).Error; err != nil {
			if util.DbIsNotFoundErr(err) {
				log.Warnf(c, "device-account restore: no subscription row for otx %s", info.OriginalTransactionId)
				return e(ErrorAppleSubscriptionOwnedElsewhere, "subscription owner not found")
			}
			return err
		}
		var owner User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&owner, sub.UserID).Error; err != nil {
			return err
		}
		if owner.Brand != string(brand) {
			log.Warnf(c, "device-account restore: owner %d brand %q != request brand %q", owner.ID, owner.Brand, brand)
			return e(ErrorBrandMismatch, "account belongs to a different brand")
		}
		if isUserBlocked(&owner) {
			log.Warnf(c, "device-account restore: owner %d is blocked", owner.ID)
			return e(ErrorForbidden, "account blocked")
		}
		has, err := userHasIdentity(tx, owner.ID)
		if err != nil {
			return err
		}
		if has {
			return e(ErrorAppleSubscriptionOwnedElsewhere, "subscription belongs to an account with a login identity")
		}

		var device Device
		findErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("udid = ?", udid).First(&device).Error
		if findErr != nil && !util.DbIsNotFoundErr(findErr) {
			return findErr
		}
		exists := findErr == nil
		if exists && device.UserID != owner.ID {
			if ok, err := deviceAccountIsDisposable(tx, device.UserID); err != nil {
				return err
			} else if !ok {
				log.Warnf(c, "device-account restore: udid %s held by user %d which is not an empty device account", udid, device.UserID)
				return e(ErrorAppleSubscriptionOwnedElsewhere, "device holds another account")
			}
		}

		auth, issueAt, err := generateTokens(c, owner.ID, udid, owner.Roles)
		if err != nil {
			return err
		}
		now := time.Now().Unix()
		if exists {
			updates := map[string]any{"user_id": owner.ID, "token_issue_at": issueAt.Unix(), "token_last_used_at": now}
			// app info 照 refreshDeviceAppInfo：is_gateway 建行时锁定，不改。
			if ci := parseClientHeader(c.GetHeader("X-K2-Client")); ci != nil {
				updates["app_version"] = ci.Version
				updates["app_platform"] = ci.Platform
				updates["app_arch"] = ci.Arch
				updates["os_version"] = ci.OSVersion
				updates["device_model"] = ci.DeviceModel
			}
			if err := tx.Model(&Device{}).Where("id = ?", device.ID).Updates(updates).Error; err != nil {
				return err
			}
		} else {
			device = Device{UDID: udid, Remark: remark, UserID: owner.ID, TokenIssueAt: issueAt.Unix(), TokenLastUsedAt: now}
			createDeviceWithAppInfo(c, &device)
			if err := tx.Create(&device).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("user_id = ? AND udid <> ?", owner.ID, udid).Delete(&Device{}).Error; err != nil {
			return err
		}
		res.Auth, res.UserID, res.Created = auth, owner.ID, !exists
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// deviceAccountIsDisposable 设备上现有的账号能否被恢复覆盖：零身份、没有任何订阅行、没有会员期——
// 即重置手机后 App 自动建的空账号。有任何可丢的东西都不能覆盖。
func deviceAccountIsDisposable(tx *gorm.DB, userID uint64) (bool, error) {
	has, err := userHasIdentity(tx, userID)
	if err != nil || has {
		return false, err
	}
	var subs int64
	if err := tx.Model(&Subscription{}).Where("user_id = ?", userID).Limit(1).Count(&subs).Error; err != nil {
		return false, err
	}
	if subs > 0 {
		return false, nil
	}
	var u User
	if err := tx.Select("id", "expired_at").First(&u, userID).Error; err != nil {
		return false, err
	}
	return u.ExpiredAt <= time.Now().Unix(), nil
}
