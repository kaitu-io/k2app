package center

import (
	"context"

	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// 路由器版发货台账阶段。持久化的是"运营视角的推进点"；自动阶段由 advanceRouterFulfillment
// 从订阅状态 / 网关设备活动推导，运营只手动做 ready→shipped。
const (
	RouterStagePaid         = "paid"         // 已付款，线路尚未入队
	RouterStageProvisioning = "provisioning" // 线路开机中（sub=provisioning）
	RouterStageReady        = "ready"        // 线路就绪（sub=active）：成品待烧录/寄出，自备可生成安装命令
	RouterStageShipped      = "shipped"      // 成品已寄出（仅 HardwareSKU != ""）
	RouterStageOnline       = "online"       // 路由器已连上 Center（网关设备在铸造凭证后有活动）
	RouterStageExpired      = "expired"      // 线路已停服（sub=suspended/deprovisioned/failed）
)

// RouterShipping 收货信息（成品路由器订单）。存于 Order.RouterShipping JSON 列。
type RouterShipping struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
}

// RouterFulfillment 路由器版订单的发货 / 上线台账。一单一行（OrderID 唯一）。
// 线路本身是 PrivateNodeSubscription（SubID），本表只记"这台路由器走到哪一步"。
type RouterFulfillment struct {
	ID        uint64 `gorm:"primarykey" json:"id"`
	CreatedAt int64  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt int64  `gorm:"autoUpdateTime" json:"updatedAt"`

	OrderID uint64 `gorm:"uniqueIndex;not null" json:"orderId"`
	UserID  uint64 `gorm:"not null;index" json:"userId"`
	SubID   uint64 `gorm:"not null;index" json:"subId"` // → private_node_subscriptions.id

	HardwareSKU string `gorm:"column:hardware_sku;type:varchar(40);not null;default:''" json:"hardwareSku"` // 空 = 自备路由器
	Stage       string `gorm:"type:varchar(20);not null;index" json:"stage"`

	TrackingNo string `gorm:"type:varchar(64);not null;default:''" json:"trackingNo"`
	Carrier    string `gorm:"type:varchar(32);not null;default:''" json:"carrier"`
	ShippedAt  int64  `gorm:"not null;default:0" json:"shippedAt"`

	// 网关凭证：谁铸的不重要，铸造时刻是"上线"判定的基准（设备活动 > 此刻 才算真上线）。
	GatewayDeviceID    *uint64 `gorm:"index" json:"gatewayDeviceId,omitempty"`
	CredentialMintedAt int64   `gorm:"not null;default:0" json:"credentialMintedAt"`
	ActivatedAt        int64   `gorm:"not null;default:0" json:"activatedAt"`

	Note      string `gorm:"type:text" json:"note"`
	UpdatedBy string `gorm:"type:varchar(64);not null;default:''" json:"updatedBy"`
}

// IsBYO 自备路由器（不发硬件）。
func (f *RouterFulfillment) IsBYO() bool { return f.HardwareSKU == "" }

// advanceRouterFulfillment 按订阅状态与网关设备活动推进阶段（纯函数，返回是否有变化）。
// 规则：
//   - paid → provisioning（sub=provisioning）→ ready（sub=active）；paid 可直接跳 ready。
//   - ready → online：自备路由器在铸造凭证后设备有活动即上线；成品必须先 shipped。
//   - shipped → online：设备活动 > 铸造时刻。
//   - 任意非终态 → expired：sub 进入 suspended/deprovisioned/failed。grace 仍视为在服务。
//   - expired → ready：sub 回到 active（续费回收），等待设备再次上线。
func advanceRouterFulfillment(f *RouterFulfillment, sub *PrivateNodeSubscription, dev *Device, now int64) bool {
	before := f.Stage
	switch sub.Status {
	case PNStatusSuspended, PNStatusDeprovisioned, PNStatusFailed:
		f.Stage = RouterStageExpired
		return f.Stage != before
	}
	if f.Stage == RouterStageExpired && sub.Status == PNStatusActive {
		f.Stage = RouterStageReady
	}
	if f.Stage == RouterStagePaid && sub.Status == PNStatusProvisioning {
		f.Stage = RouterStageProvisioning
	}
	if (f.Stage == RouterStagePaid || f.Stage == RouterStageProvisioning) && sub.Status == PNStatusActive {
		f.Stage = RouterStageReady
	}
	canGoOnline := (f.Stage == RouterStageReady && f.IsBYO()) || f.Stage == RouterStageShipped
	if canGoOnline && dev != nil && dev.IsGateway && f.CredentialMintedAt > 0 && dev.TokenLastUsedAt > f.CredentialMintedAt {
		f.Stage = RouterStageOnline
		f.ActivatedAt = dev.TokenLastUsedAt
	}
	_ = now
	return f.Stage != before
}

// syncRouterFulfillment 加载订阅与用户网关设备，推进阶段并落库（幂等，可在读路径调用）。
func syncRouterFulfillment(ctx context.Context, tx *gorm.DB, f *RouterFulfillment, now int64) error {
	var sub PrivateNodeSubscription
	if err := tx.First(&sub, f.SubID).Error; err != nil {
		return err
	}
	var dev Device
	var devPtr *Device
	if err := tx.Where("user_id = ? AND is_gateway = ?", f.UserID, true).Order("id DESC").First(&dev).Error; err == nil {
		devPtr = &dev
	}
	if !advanceRouterFulfillment(f, &sub, devPtr, now) {
		return nil
	}
	log.Infof(ctx, "router fulfillment %d (order %d) advanced to %s", f.ID, f.OrderID, f.Stage)
	return tx.Model(&RouterFulfillment{}).Where("id = ?", f.ID).
		Updates(map[string]any{"stage": f.Stage, "activated_at": f.ActivatedAt}).Error
}
