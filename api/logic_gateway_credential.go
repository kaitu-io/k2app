package center

import (
	"context"
	"errors"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// mintGatewayCredential 为用户铸造 k2subs:// 网关凭证（轮换语义：先删该用户既有 router 设备）。
// 准入由 checkDeviceLimitOrKick(isGateway=true) 决定：持有 ≥1 条 serviceable 线路即可，一账号一路由器。
// 业务拒绝以 rerr 返回（ErrorPlanNoRouter / ErrorRouterDeviceLimit），调用方用 ErrorE 透传。
// 同一事务内把新设备挂到用户的路由器版台账（若有），铸造时刻是"上线"判定基准。
//
// Phase 0（spec §4.2）：不再签 access/refresh 对（refresh 曾被直接丢弃 —— P1 的根因）。
// 改签 90 天 tunnel token，锚点 = 设备创建时刻。
func mintGatewayCredential(ctx context.Context, user *User) (string, uint64, error) {
	udid := newRouterUDID()
	var tunnelToken string
	var deviceID uint64
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		// Rotation: drop any existing router devices so the limit check counts
		// only the device we are about to mint.
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
		// 选行前先把该用户非 online 的台账按线路实时状态同步：存储的 stage 可能陈旧（线路过期时
		// 读路径存了 expired，续费后还没人读过）。不同步的话 attach 按陈旧 stage 找不到行，而旧设备
		// 上面已被轮换删掉 —— 之后行恢复，GatewayDeviceID 却指向已删设备，永远到不了 online。
		if err := syncUserRouterFulfillments(ctx, tx, user.ID, now.Unix()); err != nil {
			return err
		}
		return attachCredentialToFulfillment(tx, user.ID, dev.ID, now.Unix())
	})
	if err != nil {
		return "", 0, err
	}
	return injectSubsCreds(gatewayCredentialBase(), udid, tunnelToken), deviceID, nil
}

// syncUserRouterFulfillments 对用户所有 stage≠online 的台账调 syncRouterFulfillment（事务内，错误上抛；
// 线路行不存在的孤儿台账跳过）。
func syncUserRouterFulfillments(ctx context.Context, tx *gorm.DB, userID uint64, now int64) error {
	var rows []RouterFulfillment
	if err := tx.Where("user_id = ? AND stage <> ?", userID, RouterStageOnline).Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		err := syncRouterFulfillment(ctx, tx, &rows[i], now)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 台账指向的线路不存在（孤儿数据）：无从同步，也不该挡住铸凭证。
			log.Warnf(ctx, "router fulfillment %d: line %d not found, skip sync", rows[i].ID, rows[i].SubID)
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// attachCredentialToFulfillment 把网关设备挂到用户**最新一条**未结的路由器版台账（ready/shipped/online）。
// 只关联一条，不批量更新：一用户可能有多条台账（每次买硬件套餐 applyRouterOrder 都建新台账），
// 全部盖同一个设备 ID 会让没接过网的旧台账也被 syncRouterFulfillment 推到 online。
// 最新一条（id 最大）= 正在开通的那台；旧台账保留旧设备 ID —— rotation 会删掉旧设备，
// 其 syncRouterFulfillment 之后查不到对应设备（GatewayDeviceID 指向的设备已不存在），
// 停在原里程碑，不会被误判上线。无台账（纯专属线路用户）为 no-op。
func attachCredentialToFulfillment(tx *gorm.DB, userID, deviceID uint64, now int64) error {
	var f RouterFulfillment
	err := tx.Where("user_id = ? AND stage IN ?", userID, []string{RouterStageReady, RouterStageShipped, RouterStageOnline}).
		Order("id DESC").First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return tx.Model(&f).Updates(map[string]any{"gateway_device_id": deviceID, "credential_minted_at": now}).Error
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
