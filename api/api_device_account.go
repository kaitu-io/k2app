package center

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/appstore"
	"github.com/wordgate/qtoolkit/log"
)

// deviceAccountLimiter 每 IP 每分钟 20 次（spec §4.2：主要防库膨胀）。
var deviceAccountLimiter = newFunnelIPLimiter(20)

// DataDeviceAccountRequest 设备账号请求：按 udid 建号（spec §4.2）。
type DataDeviceAccountRequest struct {
	UDID     string `json:"udid" binding:"required"` // 设备ID
	Remark   string `json:"remark"`                  // 设备备注
	Language string `json:"language"`                // 用户语言偏好（可选）
}

// api_device_account POST /api/auth/device-account
//
// 零身份设备账号：库里没有该 udid → 建 User（零身份）+ Device；
// 设备已存在 → 不签 token：属有身份用户 403005 请登录，属设备账号 403006 要凭证。
// 响应与 api_login 同形（DataAuthResult）。
func api_device_account(c *gin.Context) {
	if !deviceAccountLimiter.Allow(c.ClientIP()) {
		Error(c, ErrorTooManyRequests, "too many requests")
		return
	}
	var req DataDeviceAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid device-account request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	brand, ok := deviceAccountGate(c)
	if !ok {
		return
	}

	res, err := deviceAccountSignIn(c, brand, req)
	if err != nil && isDuplicateKeyErr(err) {
		// 并发建号：devices.udid 唯一冲突，本事务已回滚，对方已建好——这时它就是「已存在的设备账号」，
		// 不凭 udid 给 token（客户端单飞建号，正常不会走到）。
		log.Warnf(c, "device-account udid %s created concurrently", req.UDID)
		err = e(ErrorDeviceAccountProofRequired, "device belongs to a device account, proof required")
	}
	if err != nil {
		log.Warnf(c, "device-account failed for udid %s: %v", req.UDID, err)
		ErrorE(c, err)
		return
	}

	// app 面的 device_hash 就是 udid（webapp services/stats.ts getDeviceHash），据此把建号前的匿名事件并到该用户。
	ev := FunnelEvent{
		Brand:   string(brand),
		Surface: FunnelSurfaceApp,
		Event:   funnelEventDeviceAccountCreated,
		UserID:  res.UserID,
		Source:  "create",
	}
	if validFunnelDeviceHash(req.UDID) {
		ev.AnonID = req.UDID
	}
	if info := parseClientHeader(c.GetHeader("X-K2-Client")); info != nil {
		ev.OS = normalizeFunnelOS(info.Platform)
		ev.AppVersion = funnelTruncate(info.Version, 32)
	}
	funnelEnqueue(ev)
	if ev.AnonID != "" {
		linkFunnelIdentity(c, "did", req.UDID, res.UserID, brand)
	}
	log.Infof(c, "device-account created: user %d udid %s", res.UserID, req.UDID)
	Success(c, res.Auth)
}

// deviceAccountGate 建号与恢复共用的前置门：client-class、路由器、品牌开关。拒绝时已写响应。
func deviceAccountGate(c *gin.Context) (Brand, bool) {
	// 与 api_login 相同：未知 client-class 早拒，header 缺失合法（兼容）。
	if rawHeader := c.GetHeader("X-K2-Client"); rawHeader != "" && parseClientHeader(rawHeader) == nil {
		log.Warnf(c, "device-account rejected: invalid client class header=%q remote=%s", rawHeader, c.ClientIP())
		Error(c, ErrorInvalidClientClass, "invalid client class token")
		return "", false
	}
	// 设备账号只给终端 App；路由器（网关）不能免登录建号。
	if parseClientHeader(c.GetHeader("X-K2-Client")).IsGateway() {
		log.Warnf(c, "device-account rejected: gateway client remote=%s", c.ClientIP())
		Error(c, ErrorInvalidClientClass, "device account not available for router clients")
		return "", false
	}
	brand := ReqBrand(c)
	if !brand.Config().DeviceAccount {
		Error(c, ErrorDeviceAccountDisabled, "device account not supported")
		return "", false
	}
	return brand, true
}

// DataDeviceAccountRestoreRequest 凭 Apple 交易恢复设备账号（spec §4.2.1）。
type DataDeviceAccountRestoreRequest struct {
	UDID              string `json:"udid" binding:"required"`              // 设备ID
	SignedTransaction string `json:"signedTransaction" binding:"required"` // StoreKit Transaction.jwsRepresentation
	Remark            string `json:"remark"`                               // 设备备注
}

// api_device_account_restore POST /api/auth/device-account/restore
//
// 重置 / 换手机后，凭这台设备上 Apple ID 的签名交易，把交易所属的零身份设备账号搬到 udid 这台设备，
// 返回该账号的 token（与 api_login 同形）。交易属有身份账号、或设备上已有别的有内容的账号 → 409004。
func api_device_account_restore(c *gin.Context) {
	if !deviceAccountLimiter.Allow(c.ClientIP()) {
		Error(c, ErrorTooManyRequests, "too many requests")
		return
	}
	var req DataDeviceAccountRestoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid device-account restore request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	brand, ok := deviceAccountGate(c)
	if !ok {
		return
	}
	bundleID := appleBundleIDForBrand(brand)
	if bundleID == "" {
		log.Errorf(c, "device-account restore: no apple bundle id configured for brand %s", brand)
		Error(c, ErrorSystemError, "apple not configured")
		return
	}

	signed, err := verifyAppleSignedTransaction(req.SignedTransaction)
	if err != nil {
		log.Warnf(c, "device-account restore rejected for udid %s: %v", req.UDID, err)
		Error(c, ErrorInvalidArgument, "invalid signed transaction")
		return
	}
	// 签名只证明持有；交易状态以 Apple Server API 复核为准（同 verify）。
	info, err := fetchAppleTransaction(c, bundleID, signed.TransactionID)
	if err != nil {
		log.Errorf(c, "device-account restore: apple lookup txn %s failed: %v", signed.TransactionID, err)
		Error(c, ErrorSystemError, "apple verification failed")
		return
	}
	if info.BundleId != bundleID || info.OriginalTransactionId != signed.OriginalTransactionID {
		log.Warnf(c, "device-account restore: txn %s mismatch (bundle %s/%s, otx %s/%s)", signed.TransactionID,
			info.BundleId, bundleID, info.OriginalTransactionId, signed.OriginalTransactionID)
		Error(c, ErrorInvalidArgument, "invalid signed transaction")
		return
	}
	if info.InAppOwnershipType == appstore.OwnershipType_FAMILY_SHARED {
		log.Warnf(c, "device-account restore: txn %s is family-shared", signed.TransactionID)
		Error(c, ErrorAppleSubscriptionOwnedElsewhere, "family-shared purchase")
		return
	}

	res, err := deviceAccountRestore(c, brand, req.UDID, req.Remark, info)
	if err != nil {
		log.Warnf(c, "device-account restore failed for udid %s txn %s: %v", req.UDID, signed.TransactionID, err)
		ErrorE(c, err)
		return
	}
	log.Infof(c, "device-account restored: user %d moved to udid %s via txn %s (otx %s)",
		res.UserID, req.UDID, signed.TransactionID, info.OriginalTransactionId)

	// 追上期间的续订：对主人补跑一次这笔交易的入账（已入账则去重；失败不影响恢复，对账 worker 兜底）。
	if err := verifyAndGrantTransaction(c, res.UserID, signed.TransactionID); err != nil && !errors.Is(err, errAppleTxnUpgraded) {
		log.Warnf(c, "device-account restore: post-restore credit for user %d txn %s: %v", res.UserID, signed.TransactionID, err)
	}
	Success(c, res.Auth)
}
