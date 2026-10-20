package center

import (
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
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

const funnelEventDeviceAccountCreated = "device_account_created"

// deviceAccountBeforeReissueWrite 测试接缝：重签校验事务提交后、写 token 前调用。生产恒为 nil。
var deviceAccountBeforeReissueWrite func()

type deviceAccountResult struct {
	Auth    *DataAuthResult
	UserID  uint64
	Created bool
}

// deviceAccountSignIn 按 udid 建号（事务内）或给零身份用户重签 token（事务内校验、事务外写）（spec §4.2）。
// udid 唯一冲突（并发建号）原样返回，由调用方重走一次。
func deviceAccountSignIn(c *gin.Context, brand Brand, req DataDeviceAccountRequest) (*deviceAccountResult, error) {
	res := &deviceAccountResult{}
	var reissueDeviceID uint64
	var reissueAt int64
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		var device Device
		findErr := tx.Where("udid = ?", req.UDID).First(&device).Error
		if findErr != nil && !util.DbIsNotFoundErr(findErr) {
			return findErr
		}

		if findErr != nil { // 无设备行 → 建零身份用户 + 设备
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
		}

		// 设备行已存在：只有零身份、同品牌、未封禁的用户可以免登录拿 token。
		var user User
		if err := tx.First(&user, device.UserID).Error; err != nil {
			return err
		}
		has, err := userHasIdentity(tx, user.ID)
		if err != nil {
			return err
		}
		if has {
			return e(ErrorDeviceAccountLoginRequired, "device belongs to an account, login required")
		}
		if user.Brand != string(brand) {
			log.Warnf(c, "device-account rejected: user %d brand %q does not match request brand %q", user.ID, user.Brand, brand)
			return e(ErrorBrandMismatch, "account belongs to a different brand")
		}
		if isUserBlocked(&user) {
			log.Warnf(c, "device-account rejected: user %d is blocked", user.ID)
			return e(ErrorForbidden, "account blocked")
		}
		auth, issueAt, err := generateTokens(c, user.ID, req.UDID, user.Roles)
		if err != nil {
			return err
		}
		res.Auth, res.UserID, reissueDeviceID = auth, user.ID, device.ID
		reissueAt = issueAt.Unix()
		return nil
	})
	if err != nil {
		return nil, err
	}
	if reissueDeviceID != 0 {
		// 重签的写放在只读校验事务之外：事务内先做了快照读，MariaDB 的
		// innodb_snapshot_isolation 下，并发重签已改过这一行时事务内 UPDATE 会报
		// 1020 Record has changed since last read。单条自动提交 UPDATE 无此问题。
		updates := map[string]any{
			"token_issue_at":     reissueAt,
			"token_last_used_at": time.Now().Unix(),
		}
		// app info 照 refreshDeviceAppInfo：is_gateway 建行时锁定，不改。
		if info := parseClientHeader(c.GetHeader("X-K2-Client")); info != nil {
			updates["app_version"] = info.Version
			updates["app_platform"] = info.Platform
			updates["app_arch"] = info.Arch
			updates["os_version"] = info.OSVersion
			updates["device_model"] = info.DeviceModel
		}
		if deviceAccountBeforeReissueWrite != nil {
			deviceAccountBeforeReissueWrite()
		}
		// 带 user_id 条件：校验后设备若已被并发登录转给别人，不得覆盖新主人的 token 锚点。
		upd := db.Get().Model(&Device{}).Where("id = ? AND user_id = ?", reissueDeviceID, res.UserID).Updates(updates)
		if upd.Error != nil {
			return nil, upd.Error
		}
		if upd.RowsAffected == 0 {
			// MySQL 对"值没变"的行也报 0 行（如同一时刻的并发重签），所以 0 行不等于易主：再确认归属。
			var owned int64
			if err := db.Get().Model(&Device{}).Where("id = ? AND user_id = ?", reissueDeviceID, res.UserID).Count(&owned).Error; err != nil {
				return nil, err
			}
			if owned > 0 {
				return res, nil
			}
			log.Warnf(c, "device-account reissue: device %d no longer owned by user %d", reissueDeviceID, res.UserID)
			return nil, e(ErrorDeviceAccountLoginRequired, "device belongs to an account, login required")
		}
	}
	return res, nil
}
