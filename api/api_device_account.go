package center

import (
	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// deviceAccountLimiter 每 IP 每分钟 20 次（spec §4.2：主要防库膨胀）。
var deviceAccountLimiter = newFunnelIPLimiter(20)

// DataDeviceAccountRequest 设备账号请求：按 udid 建号或重装找回（spec §4.2）。
type DataDeviceAccountRequest struct {
	UDID     string `json:"udid" binding:"required"` // 设备ID
	Remark   string `json:"remark"`                  // 设备备注
	Language string `json:"language"`                // 用户语言偏好（可选）
}

// api_device_account POST /api/auth/device-account
//
// 零身份设备账号：库里没有该 udid → 建 User（零身份）+ Device；
// 设备属零身份用户 → 签发新 token（重装回来）；属有身份用户 → 403005 请登录。
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
	// 与 api_login 相同：未知 client-class 早拒，header 缺失合法（兼容）。
	if rawHeader := c.GetHeader("X-K2-Client"); rawHeader != "" && parseClientHeader(rawHeader) == nil {
		log.Warnf(c, "device-account rejected: invalid client class header=%q remote=%s", rawHeader, c.ClientIP())
		Error(c, ErrorInvalidClientClass, "invalid client class token")
		return
	}
	brand := ReqBrand(c)
	if !brand.Config().DeviceAccount {
		Error(c, ErrorDeviceAccountDisabled, "device account not supported")
		return
	}

	res, err := deviceAccountSignIn(c, brand, req)
	if err != nil && isDuplicateKeyErr(err) {
		// 并发建号：devices.udid 唯一冲突，本事务已回滚；对方已建好 → 按"已存在"重走一次。
		log.Infof(c, "device-account udid %s created concurrently, retrying as existing", req.UDID)
		res, err = deviceAccountSignIn(c, brand, req)
	}
	if err != nil {
		log.Warnf(c, "device-account failed for udid %s: %v", req.UDID, err)
		ErrorE(c, err)
		return
	}

	if res.Created {
		ev := FunnelEvent{
			Brand:   string(brand),
			Surface: FunnelSurfaceApp,
			Event:   funnelEventDeviceAccountCreated,
			UserID:  res.UserID,
		}
		if info := parseClientHeader(c.GetHeader("X-K2-Client")); info != nil {
			ev.OS = normalizeFunnelOS(info.Platform)
			ev.AppVersion = funnelTruncate(info.Version, 32)
		}
		funnelEnqueue(ev)
	}
	log.Infof(c, "device-account ok: user %d udid %s created=%v", res.UserID, req.UDID, res.Created)
	Success(c, res.Auth)
}
