package center

import (
	"context"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// mintGatewayCredential 为用户铸造 k2subs:// 网关凭证（轮换语义：先删该用户既有 router 设备）。
// 准入由 checkDeviceLimitOrKick(isGateway=true) 决定：持有 ≥1 条 serviceable 线路即可，一账号一路由器。
// 业务拒绝以 rerr 返回（ErrorPlanNoRouter / ErrorRouterDeviceLimit），调用方用 ErrorE 透传。
// 同一事务内把新设备挂到用户的路由器版台账（若有），铸造时刻是"上线"判定基准。
func mintGatewayCredential(ctx context.Context, user *User) (string, uint64, error) {
	udid := newRouterUDID()
	var tunnelToken string
	var deviceID uint64
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND is_gateway = ?", user.ID, true).Delete(&Device{}).Error; err != nil {
			return err
		}
		if err := checkDeviceLimitOrKick(ctx, tx, user, true); err != nil {
			return err
		}
		now := time.Now()
		tok, err := generateTunnelToken(ctx, user.ID, udid, user.Roles, now.Unix())
		if err != nil {
			return err
		}
		tunnelToken = tok
		dev := &Device{
			UDID: udid, UserID: user.ID, IsGateway: true, AppPlatform: "router",
			TokenIssueAt: now.Unix(), TokenLastUsedAt: now.Unix(), TunnelIssueAt: now.Unix(),
		}
		if err := tx.Create(dev).Error; err != nil {
			return err
		}
		deviceID = dev.ID
		return attachCredentialToFulfillment(tx, user.ID, dev.ID, now.Unix())
	})
	if err != nil {
		return "", 0, err
	}
	return injectSubsCreds(gatewayCredentialBase(), udid, tunnelToken), deviceID, nil
}

// attachCredentialToFulfillment 把网关设备挂到用户未结的路由器版台账（ready/shipped/online）。无台账（纯专属线路用户）为 no-op。
func attachCredentialToFulfillment(tx *gorm.DB, userID, deviceID uint64, now int64) error {
	return tx.Model(&RouterFulfillment{}).
		Where("user_id = ? AND stage IN ?", userID, []string{RouterStageReady, RouterStageShipped, RouterStageOnline}).
		Updates(map[string]any{"gateway_device_id": deviceID, "credential_minted_at": now}).Error
}

// touchGatewayDeviceSeen 记录路由器最近一次拉取订阅的时刻（/api/subs 是 k2r 唯一的 Center 触点，默认每 30 分钟）。
// 只对网关设备写；失败只记日志。
func touchGatewayDeviceSeen(ctx context.Context, dev *Device, now int64) {
	if dev == nil || !dev.IsGateway {
		return
	}
	if err := db.Get().Model(&Device{}).Where("id = ?", dev.ID).Update("token_last_used_at", now).Error; err != nil {
		log.Warnf(ctx, "touch gateway device %d last-seen: %v", dev.ID, err)
	}
}
