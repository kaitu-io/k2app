package center

import (
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

func parseRouterFulfillmentID(c *gin.Context) (*RouterFulfillment, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid fulfillment id")
		return nil, false
	}
	var f RouterFulfillment
	if err := db.Get().First(&f, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, ErrorNotFound, "fulfillment not found")
			return nil, false
		}
		log.Errorf(c, "load router fulfillment %d: %v", id, err)
		Error(c, ErrorSystemError, "failed to load fulfillment")
		return nil, false
	}
	return &f, true
}

// userEmailByID best-effort 取用户邮箱（登录身份表），失败返回空串。
func userEmailByID(userID uint64) string {
	var u User
	if err := db.Get().Preload("LoginIdentifies").First(&u, userID).Error; err != nil {
		return ""
	}
	return getUserEmailFromIdentifies(&u)
}

// adminGatewayDeviceDTO 严格跟随 f.GatewayDeviceID（与 api_get_user_router / syncRouterFulfillment
// 的取法一致），不按 user_id+is_gateway 独立查——否则会和台账指向的设备对不上（轮换后旧台账
// 查到新设备）。GatewayDeviceID 为 nil 或对应设备已被删（rotation）时返回 nil。
func adminGatewayDeviceDTO(f *RouterFulfillment, now int64) *DataRouterDevice {
	if f.GatewayDeviceID == nil {
		return nil
	}
	var dev Device
	if err := db.Get().First(&dev, *f.GatewayDeviceID).Error; err != nil {
		return nil
	}
	return routerDeviceDTO(&dev, now)
}

func adminRouterFulfillmentDTO(c *gin.Context, f *RouterFulfillment, now int64) DataAdminRouterFulfillment {
	// CanMintCredential 镜像用户端 api_get_user_router 的准入门（HasActivePrivateLines），不是
	// 台账 stage 的派生值——两者可能分叉（例如线路过期但台账还没被 syncRouterFulfillment 推到 expired）。
	canMint, err := HasActivePrivateLines(c, db.Get(), f.UserID, now)
	if err != nil {
		log.Warnf(c, "check HasActivePrivateLines for user %d: %v", f.UserID, err)
		canMint = false
	}
	d := DataAdminRouterFulfillment{
		DataRouterFulfillment: DataRouterFulfillment{
			ID: f.ID, OrderID: f.OrderID, HardwareSKU: f.HardwareSKU, Stage: f.Stage, TrackingNo: f.TrackingNo,
			Carrier: f.Carrier, ShippedAt: f.ShippedAt, ActivatedAt: f.ActivatedAt, CredentialMinted: f.GatewayDeviceID != nil,
			CanMintCredential: canMint,
			CreatedAt:         f.CreatedAt,
		},
		UserID: f.UserID, Email: userEmailByID(f.UserID), SubID: f.SubID, Note: f.Note, UpdatedBy: f.UpdatedBy, UpdatedAt: f.UpdatedAt,
	}
	var order Order
	if err := db.Get().Select("id", "router_shipping").First(&order, f.OrderID).Error; err == nil {
		d.Shipping = order.GetRouterShipping()
	}
	var sub PrivateNodeSubscription
	if err := db.Get().First(&sub, f.SubID).Error; err == nil {
		line := buildPrivateNodeSubDTO(c, &sub, now)
		d.Line = &line
	}
	d.Device = adminGatewayDeviceDTO(f, now)
	return d
}

// api_admin_list_router_fulfillments 路由器订单主台账（按 stage 过滤，最新优先）。读路径顺带同步阶段。
func api_admin_list_router_fulfillments(c *gin.Context) {
	now := time.Now().Unix()
	query := db.Get().Model(&RouterFulfillment{})
	if stage := c.Query("stage"); stage != "" {
		query = query.Where("stage = ?", stage)
	}
	if uid := c.Query("userId"); uid != "" {
		query = query.Where("user_id = ?", uid)
	}
	pagination := PaginationFromRequest(c)
	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count router fulfillments: %v", err)
		Error(c, ErrorSystemError, "failed to count router fulfillments")
		return
	}
	var rows []RouterFulfillment
	if err := query.Order("id DESC").Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&rows).Error; err != nil {
		log.Errorf(c, "failed to list router fulfillments: %v", err)
		Error(c, ErrorSystemError, "failed to list router fulfillments")
		return
	}
	items := make([]DataAdminRouterFulfillment, 0, len(rows))
	for i := range rows {
		if err := syncRouterFulfillment(c, db.Get(), &rows[i], now); err != nil {
			log.Warnf(c, "sync router fulfillment %d on admin read: %v", rows[i].ID, err)
		}
		items = append(items, adminRouterFulfillmentDTO(c, &rows[i], now))
	}
	ListWithData(c, items, pagination)
}

// api_admin_update_router_stage 运营手动推进：仅 ready→shipped（成品，必须带快递单号）；任何阶段可改备注。
func api_admin_update_router_stage(c *gin.Context) {
	f, ok := parseRouterFulfillmentID(c)
	if !ok {
		return
	}
	var body AdminRouterStageRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	updates := map[string]any{"updated_by": adminActorTag(c)}
	if body.Note != "" {
		updates["note"] = body.Note
	}
	switch body.Stage {
	case "":
		// 只改备注
	case RouterStageShipped:
		if f.IsBYO() {
			Error(c, ErrorInvalidOperation, "自备路由器订单没有发货阶段")
			return
		}
		if f.Stage != RouterStageReady {
			Error(c, ErrorInvalidOperation, "只有「线路就绪」的订单可以标记发货")
			return
		}
		if body.TrackingNo == "" {
			Error(c, ErrorInvalidArgument, "发货必须填写快递单号")
			return
		}
		updates["stage"] = RouterStageShipped
		updates["tracking_no"] = body.TrackingNo
		updates["carrier"] = body.Carrier
		updates["shipped_at"] = time.Now().Unix()
	default:
		Error(c, ErrorInvalidOperation, "该阶段由系统自动推进，不能手动设置")
		return
	}
	if err := db.Get().Model(&RouterFulfillment{}).Where("id = ?", f.ID).Updates(updates).Error; err != nil {
		log.Errorf(c, "update router fulfillment %d: %v", f.ID, err)
		Error(c, ErrorSystemError, "update failed")
		return
	}
	log.Infof(c, "router fulfillment %d updated by %s: %+v", f.ID, updates["updated_by"], body)
	SuccessEmpty(c)
}

// api_admin_mint_router_credential 代客户铸造网关凭证（烧录进成品路由器用）。线路未就绪不能铸（准入门会拒）。
func api_admin_mint_router_credential(c *gin.Context) {
	f, ok := parseRouterFulfillmentID(c)
	if !ok {
		return
	}
	if f.Stage != RouterStageReady && f.Stage != RouterStageShipped && f.Stage != RouterStageOnline {
		Error(c, ErrorTooEarly, "线路尚未就绪，暂不能铸造凭证")
		return
	}
	// 只允许该用户 stage∈{ready,shipped,online} 中 id 最大的那一条台账铸造 —— 与
	// attachCredentialToFulfillment 的 newest-only 语义一致。否则凭证会挂到 :id 这条台账上，
	// 但 attachCredentialToFulfillment 只认"最新一条"，实际会挂到另一条台账，这里显示的
	// CredentialMinted/Device 就永远对不上自己刚触发的这次铸造。
	var newest RouterFulfillment
	if err := db.Get().Select("id").
		Where("user_id = ? AND stage IN ?", f.UserID, []string{RouterStageReady, RouterStageShipped, RouterStageOnline}).
		Order("id DESC").First(&newest).Error; err != nil {
		log.Errorf(c, "load newest router fulfillment for user %d: %v", f.UserID, err)
		Error(c, ErrorSystemError, "failed to verify fulfillment")
		return
	}
	if newest.ID != f.ID {
		Error(c, ErrorInvalidOperation, "该台账不是当前用户可关联凭证的最新一条")
		return
	}
	var user User
	if err := db.Get().First(&user, f.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "load user %d: %v", f.UserID, err)
		Error(c, ErrorSystemError, "failed to load user")
		return
	}
	url, deviceID, err := mintGatewayCredential(c.Request.Context(), &user)
	if err != nil {
		if _, isBiz := err.(rerr); isBiz {
			ErrorE(c, err)
			return
		}
		log.Errorf(c, "admin mint router credential for fulfillment %d: %v", f.ID, err)
		Error(c, ErrorSystemError, "mint failed")
		return
	}
	_ = db.Get().Model(&RouterFulfillment{}).Where("id = ?", f.ID).Update("updated_by", adminActorTag(c)).Error
	// 回读校验：确认凭证真的挂到了这条台账上（attachCredentialToFulfillment 是 newest-only，
	// 上面的检查已经把"不是最新"挡掉了，这里只是留一道信号，异常了要能在日志里看见）。
	var check RouterFulfillment
	if err := db.Get().Select("id", "gateway_device_id").First(&check, f.ID).Error; err != nil {
		log.Errorf(c, "reload router fulfillment %d after mint: %v", f.ID, err)
	} else if check.GatewayDeviceID == nil || *check.GatewayDeviceID != deviceID {
		log.Errorf(c, "router fulfillment %d gateway device mismatch after mint: got %v, want %d", f.ID, check.GatewayDeviceID, deviceID)
	}
	log.Infof(c, "admin %s minted router credential for fulfillment %d (device %d)", adminActorTag(c), f.ID, deviceID)
	Success(c, &gin.H{"url": url, "deviceId": deviceID})
}

// api_admin_list_private_node_subscriptions 线路订阅从表（排障用）。
func api_admin_list_private_node_subscriptions(c *gin.Context) {
	now := time.Now().Unix()
	query := db.Get().Model(&PrivateNodeSubscription{})
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	if uid := c.Query("userId"); uid != "" {
		query = query.Where("user_id = ?", uid)
	}
	pagination := PaginationFromRequest(c)
	if err := query.Count(&pagination.Total).Error; err != nil {
		Error(c, ErrorSystemError, "failed to count subscriptions")
		return
	}
	var subs []PrivateNodeSubscription
	if err := query.Order("id DESC").Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&subs).Error; err != nil {
		Error(c, ErrorSystemError, "failed to list subscriptions")
		return
	}
	items := make([]DataAdminPrivateNodeSubscription, 0, len(subs))
	for i := range subs {
		s := &subs[i]
		items = append(items, DataAdminPrivateNodeSubscription{
			DataPrivateNodeSubscription: buildPrivateNodeSubDTO(c, s, now),
			UserID:                      s.UserID, Email: userEmailByID(s.UserID), OrderID: s.OrderID, BoundIpv4: s.BoundIpv4,
		})
	}
	ListWithData(c, items, pagination)
}

// api_admin_list_router_devices 路由器设备从表（is_gateway=true）。
func api_admin_list_router_devices(c *gin.Context) {
	now := time.Now().Unix()
	query := db.Get().Model(&Device{}).Where("is_gateway = ?", true)
	if uid := c.Query("userId"); uid != "" {
		query = query.Where("user_id = ?", uid)
	}
	pagination := PaginationFromRequest(c)
	if err := query.Count(&pagination.Total).Error; err != nil {
		Error(c, ErrorSystemError, "failed to count router devices")
		return
	}
	var devs []Device
	if err := query.Order("token_last_used_at DESC").Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&devs).Error; err != nil {
		Error(c, ErrorSystemError, "failed to list router devices")
		return
	}
	items := make([]DataAdminRouterDevice, 0, len(devs))
	for i := range devs {
		items = append(items, DataAdminRouterDevice{
			DataRouterDevice: *routerDeviceDTO(&devs[i], now),
			ID:               devs[i].ID, UserID: devs[i].UserID, Email: userEmailByID(devs[i].UserID),
		})
	}
	ListWithData(c, items, pagination)
}
