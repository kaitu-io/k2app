package center

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	if body.Stage != RouterStageShipped {
		if err := db.Get().Model(&RouterFulfillment{}).Where("id = ?", f.ID).Updates(updates).Error; err != nil {
			log.Errorf(c, "update router fulfillment %d: %v", f.ID, err)
			Error(c, ErrorSystemError, "update failed")
			return
		}
		log.Infof(c, "router fulfillment %d updated by %s: %+v", f.ID, updates["updated_by"], body)
		SuccessEmpty(c)
		return
	}

	// 发货 = 服务期起点。成品客户在付款→开机→烧录→寄出→在途这段时间里拿不到任何服务，线路
	// 建立时按付款日算的 expires_at 只是暂定值；发货这一刻把它重设为「发货日 + 套餐月数」
	// （只延不缩）。预售单（付款离发货可能隔两个月）与发售后的正常单走同一条规则。
	// 续费单不会经过这里：applyRouterOrder 让续费台账继承旧硬件台账的 ShippedAt 后由
	// advanceRouterFulfillment 自动跳到 shipped，服务期由 extendPrivateLine 叠加。
	shippedAt := updates["shipped_at"].(int64)
	txErr := db.Get().Transaction(func(tx *gorm.DB) error {
		// 条件更新：读到 ready 到写入之间可能被并发推进（另一次发货 / 同步），只从 ready 发货。
		res := tx.Model(&RouterFulfillment{}).Where("id = ? AND stage = ?", f.ID, RouterStageReady).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return e(ErrorInvalidOperation, "只有「线路就绪」的订单可以标记发货")
		}
		return resetLineExpiryOnShip(c, tx, f, shippedAt)
	})
	if txErr != nil {
		if _, isBiz := txErr.(rerr); isBiz {
			ErrorE(c, txErr)
			return
		}
		log.Errorf(c, "ship router fulfillment %d: %v", f.ID, txErr)
		Error(c, ErrorSystemError, "update failed")
		return
	}
	log.Infof(c, "router fulfillment %d updated by %s: %+v", f.ID, updates["updated_by"], body)
	SuccessEmpty(c)
}

// resetLineExpiryOnShip 发货时把台账所指线路的到期日重设为 shippedAt + 套餐月数（只延不缩）。
// 套餐行找不到直接报错回滚发货：不知道服务期长度就不能宣布服务开始，静默按 12 个月猜会在
// 套餐改期限时悄悄给错日期。
func resetLineExpiryOnShip(ctx context.Context, tx *gorm.DB, f *RouterFulfillment, shippedAt int64) error {
	var sub PrivateNodeSubscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, f.SubID).Error; err != nil {
		return fmt.Errorf("load line %d of fulfillment %d: %w", f.SubID, f.ID, err)
	}
	var plan Plan
	if err := tx.Select("id", "month").First(&plan, sub.PlanID).Error; err != nil {
		return fmt.Errorf("load plan %d of line %d: %w", sub.PlanID, sub.ID, err)
	}
	if plan.Month <= 0 {
		return fmt.Errorf("plan %d of line %d has non-positive month %d", plan.ID, sub.ID, plan.Month)
	}
	newExpiry := time.Unix(shippedAt, 0).AddDate(0, plan.Month, 0).Unix()
	if newExpiry <= sub.ExpiresAt {
		return nil
	}
	if err := tx.Model(&PrivateNodeSubscription{}).Where("id = ?", sub.ID).Update("expires_at", newExpiry).Error; err != nil {
		return fmt.Errorf("reset expiry of line %d on ship: %w", sub.ID, err)
	}
	log.Infof(ctx, "router fulfillment %d shipped: line %d expiry %d -> %d (%d months from ship)", f.ID, sub.ID, sub.ExpiresAt, newExpiry, plan.Month)
	return nil
}

// api_admin_mint_router_credential 代客户铸造网关凭证（烧录进成品路由器用）。线路未就绪不能铸（准入门会拒）。
func api_admin_mint_router_credential(c *gin.Context) {
	f, ok := parseRouterFulfillmentID(c)
	if !ok {
		return
	}
	if f.Stage == RouterStageExpired {
		Error(c, ErrorInvalidOperation, "该台账的线路已过期，续费后才能铸造凭证")
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
		items = append(items, adminPrivateNodeSubDTO(c, &subs[i], now))
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

// api_admin_router_stats 路由器版后台看板统计（stage 分布 / 卡住 / 在线 / 即将到期）。
// 统计前对 stage ≠ expired 的全部台账（含 online —— 线路停机/回收后 online 行要转 expired，否则
// 流失的路由器一直算在线）逐条 syncRouterFulfillment，保证数字与列表页的 stage 一致（同步失败只记
// 日志，不影响统计的其余口径）；路由器台账量级在百级，逐条同步可接受。
func api_admin_router_stats(c *gin.Context) {
	now := time.Now().Unix()

	var pending []RouterFulfillment
	if err := db.Get().Where("stage <> ?", RouterStageExpired).Find(&pending).Error; err != nil {
		log.Errorf(c, "load router fulfillments for stats sync: %v", err)
		Error(c, ErrorSystemError, "failed to load router fulfillments")
		return
	}
	for i := range pending {
		if err := syncRouterFulfillment(c, db.Get(), &pending[i], now); err != nil {
			log.Warnf(c, "sync router fulfillment %d for stats: %v", pending[i].ID, err)
		}
	}

	stats := DataAdminRouterStats{StageCounts: map[string]int64{
		RouterStagePaid: 0, RouterStageProvisioning: 0, RouterStageReady: 0,
		RouterStageShipped: 0, RouterStageOnline: 0, RouterStageExpired: 0,
	}}
	var counts []struct {
		Stage string
		N     int64
	}
	if err := db.Get().Model(&RouterFulfillment{}).Select("stage, count(*) as n").Group("stage").Scan(&counts).Error; err != nil {
		log.Errorf(c, "count router fulfillments by stage: %v", err)
		Error(c, ErrorSystemError, "failed to count router fulfillments")
		return
	}
	for _, row := range counts {
		stats.StageCounts[row.Stage] = row.N
	}

	if err := db.Get().Model(&RouterFulfillment{}).
		Where("stage IN ? AND updated_at < ?", []string{RouterStagePaid, RouterStageProvisioning, RouterStageReady}, now-48*3600).
		Count(&stats.Stuck).Error; err != nil {
		log.Errorf(c, "count stuck router fulfillments: %v", err)
		Error(c, ErrorSystemError, "failed to count stuck fulfillments")
		return
	}

	if err := db.Get().Model(&Device{}).
		Where("is_gateway = ? AND token_last_used_at >= ?", true, now-routerOnlineWindowSeconds).
		Count(&stats.OnlineRouters).Error; err != nil {
		log.Errorf(c, "count online routers: %v", err)
		Error(c, ErrorSystemError, "failed to count online routers")
		return
	}

	subQuery := db.Get().Model(&RouterFulfillment{}).Select("sub_id")
	if err := db.Get().Model(&PrivateNodeSubscription{}).
		Where("status = ? AND expires_at BETWEEN ? AND ? AND id IN (?)", PNStatusActive, now, now+30*86400, subQuery).
		Count(&stats.ExpiringSoon).Error; err != nil {
		log.Errorf(c, "count expiring router lines: %v", err)
		Error(c, ErrorSystemError, "failed to count expiring lines")
		return
	}

	Success(c, &stats)
}

// api_admin_extend_private_line 线路手工延期（补偿 / 客服）：复用 extendPrivateLine 的叠加/回正语义，
// 同事务同步挂在这条线路上的路由器台账（与续费分支 applyRouterOrder 同语义）。
func api_admin_extend_private_line(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid subscription id")
		return
	}
	var body AdminExtendLineRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request")
		return
	}

	now := time.Now().Unix()
	var sub PrivateNodeSubscription
	txErr := db.Get().Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return e(ErrorNotFound, "subscription not found")
			}
			return err
		}
		renewable := false
		for _, s := range renewableLineStatuses {
			if sub.Status == s {
				renewable = true
				break
			}
		}
		if !renewable {
			return e(ErrorInvalidOperation, "该线路已回收或开通失败，不能延期")
		}
		if err := extendPrivateLine(c, tx, &sub, body.Months, now); err != nil {
			return err
		}
		var fulfillments []RouterFulfillment
		if err := tx.Where("sub_id = ?", sub.ID).Find(&fulfillments).Error; err != nil {
			return err
		}
		for i := range fulfillments {
			if err := syncRouterFulfillment(c, tx, &fulfillments[i], now); err != nil {
				return err
			}
		}
		return nil
	})
	if txErr != nil {
		if _, ok := txErr.(rerr); ok {
			ErrorE(c, txErr)
			return
		}
		log.Errorf(c, "admin extend private line %d: %v", id, txErr)
		Error(c, ErrorSystemError, "extend failed")
		return
	}

	// extendPrivateLine 只回写传入指针的 ExpiresAt（status/grace_until/suspend_until 只落库，不回填
	// 调用方持有的 sub），所以响应 DTO 必须重新加载，否则 grace/suspended 线路延期后响应体里的
	// status/isServiceable/graceUntil/suspendUntil 仍是延期前的旧值（哪怕库里已经写成 active）。
	var reloaded PrivateNodeSubscription
	if err := db.Get().First(&reloaded, sub.ID).Error; err != nil {
		log.Errorf(c, "reload private node subscription %d after extend: %v", sub.ID, err)
		Error(c, ErrorSystemError, "extend failed")
		return
	}
	actor := adminActorTag(c)
	log.Infof(c, "admin %s extended line %d by %d months: %s", actor, reloaded.ID, body.Months, body.Reason)
	// sub 仍持有延期前的 status（extendPrivateLine 不回写 status）：此前已停机的线路节点不会自动开机，
	// 后台延期对话框承诺了「系统会发 Slack 提醒」，这里兑现。
	restartHint := ""
	if sub.Status == PNStatusSuspended {
		restartHint = "该线路此前已停机，需人工开机。"
	}
	sendCloudSlackNotification(c.Request.Context(), "Router Edition — Manual Extend",
		fmt.Sprintf("管理员 %s 手工延期线路 %d，%d 个月，原因：%s（新到期日 %s）。%s",
			actor, reloaded.ID, body.Months, body.Reason, time.Unix(reloaded.ExpiresAt, 0).Format("2006-01-02"), restartHint))
	dto := adminPrivateNodeSubDTO(c, &reloaded, now)
	Success(c, &dto)
}
