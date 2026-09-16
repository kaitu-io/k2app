package center

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// buildPrivateNodeSubDTO 把一条线路映射为账户页 DTO（用量来自 NodeUsage 权威镜像，IP/Region 来自 CloudInstance）。
// 与 api_get_user_private_nodes 的逐行逻辑一致；抽出以便两个端点共用。
func buildPrivateNodeSubDTO(c *gin.Context, s *PrivateNodeSubscription, now int64) DataPrivateNodeSubscription {
	d := DataPrivateNodeSubscription{
		ID: s.ID, Status: s.Status, IsServiceable: s.IsServiceable(now), Region: s.Region, IPType: s.IPType,
		TrafficTotalBytes: s.TrafficTotalBytes, PurchasedAt: s.PurchasedAt, ExpiresAt: s.ExpiresAt,
		GraceUntil: s.GraceUntil, SuspendUntil: s.SuspendUntil,
	}
	var plan Plan
	if err := db.Get().Select("label").First(&plan, s.PlanID).Error; err == nil {
		d.PlanLabel = plan.Label
	} else {
		log.Warnf(c, "private sub %d references missing Plan %d: %v", s.ID, s.PlanID, err)
	}
	if s.BoundIpv4 != "" {
		var u NodeUsage
		if err := db.Get().Where("ipv4 = ?", s.BoundIpv4).First(&u).Error; err == nil {
			d.TrafficUsedBytes, d.QuotaResetAt, d.QuotaExhausted = u.UsedBytes, u.Epoch, isNodeOverQuota(&u)
		}
	}
	if s.CloudInstanceID != nil {
		var ci CloudInstance
		if err := db.Get().Select("ip_address", "region").First(&ci, *s.CloudInstanceID).Error; err == nil {
			d.Node = &DataPrivateNodeNode{IP: ci.IPAddress, Region: ci.Region}
		}
	}
	return d
}

// routerDeviceDTO 把一台网关设备映射为账户页/后台共用的 DTO；dev 为 nil 时返回 nil。
// 与 api_get_user_router / adminGatewayDeviceDTO / api_admin_list_router_devices 共用，避免三处各写一遍
// Online 判定（routerOnlineWindowSeconds）。
func routerDeviceDTO(dev *Device, now int64) *DataRouterDevice {
	if dev == nil {
		return nil
	}
	return &DataRouterDevice{
		UDID: dev.UDID, AppVersion: dev.AppVersion, AppArch: dev.AppArch, LastSeenAt: dev.TokenLastUsedAt,
		Online: dev.TokenLastUsedAt > 0 && now-dev.TokenLastUsedAt <= routerOnlineWindowSeconds,
	}
}

// api_get_user_router 账户页「我的路由器」聚合：最新台账 + 其线路 + 网关设备 + 可续费套餐。
// Route: GET /api/user/router (AuthRequired + EnforceDeviceClass)
func api_get_user_router(c *gin.Context) {
	userID := ReqUserID(c)
	now := time.Now().Unix()
	out := DataUserRouter{}

	var f RouterFulfillment
	err := db.Get().Where("user_id = ?", userID).Order("id DESC").First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		Success(c, &out)
		return
	}
	if err != nil {
		log.Errorf(c, "failed to load router fulfillment for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to load router fulfillment")
		return
	}
	out.HasRouter = true
	if err := syncRouterFulfillment(c, db.Get(), &f, now); err != nil {
		log.Warnf(c, "sync router fulfillment %d on read: %v", f.ID, err)
	}

	// CanMintCredential 镜像 POST /api/user/gateway-credential 的准入门
	// （checkDeviceLimitOrKick → HasActivePrivateLines），不是台账 stage 的派生值——
	// 两者可能分叉（例如线路过期但台账还没被 syncRouterFulfillment 推到 expired）。
	canMint, err := HasActivePrivateLines(c, db.Get(), userID, now)
	if err != nil {
		log.Warnf(c, "check HasActivePrivateLines for user %d: %v", userID, err)
		canMint = false
	}
	out.Fulfillment = &DataRouterFulfillment{
		ID: f.ID, OrderID: f.OrderID, HardwareSKU: f.HardwareSKU, Stage: f.Stage,
		TrackingNo: f.TrackingNo, Carrier: f.Carrier, ShippedAt: f.ShippedAt, ActivatedAt: f.ActivatedAt,
		CredentialMinted:  f.GatewayDeviceID != nil,
		CanMintCredential: canMint,
		CreatedAt:         f.CreatedAt,
	}

	var sub PrivateNodeSubscription
	if err := db.Get().First(&sub, f.SubID).Error; err == nil {
		line := buildPrivateNodeSubDTO(c, &sub, now)
		out.Line = &line
	}

	// Device 严格跟随 f.GatewayDeviceID（与 syncRouterFulfillment 的取法一致），不按
	// user_id+is_gateway 独立查——否则会和台账指向的设备对不上（轮换后旧台账查到新设备）。
	if f.GatewayDeviceID != nil {
		var dev Device
		if err := db.Get().First(&dev, *f.GatewayDeviceID).Error; err == nil {
			out.Device = routerDeviceDTO(&dev, now)
		}
	}

	var renew Plan
	if err := db.Get().Scopes(ScopeBrand(ReqBrand(c))).
		Where("product = ? AND hardware_sku = ? AND is_active = ?", ProductRouter, "", true).
		Order("id ASC").First(&renew).Error; err == nil {
		out.RenewPlanPID = renew.PID
	}
	Success(c, &out)
}
