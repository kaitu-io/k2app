package center

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/asynq"
	"github.com/wordgate/qtoolkit/chatwoot"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
)

// SetupRouter 设置路由
func SetupRouter() *gin.Engine {
	ctx := context.Background()
	log.Infof(ctx, "setting up router...")
	if util.IsDev() {
		gin.SetMode(gin.DebugMode)
		log.Infof(ctx, "gin mode: debug")
	} else {
		gin.SetMode(gin.ReleaseMode)
		log.Infof(ctx, "gin mode: release")
	}
	r := gin.New()

	// 版本信息（公开接口）
	r.GET("/version", log.MiddlewareRequestLog(true), MiddleRecovery(), api_get_version)
	r.GET("/healthy", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})
	// Webhook 相关路由
	r.POST("/webhook/wordgate", log.MiddlewareRequestLog(true), MiddleRecovery(), BrandResolver(), api_wordgate_webhook)
	// Apple App Store Server Notifications V2（自动续订订阅续费/退款/撤销）
	r.POST("/webhook/appstore", log.MiddlewareRequestLog(true), MiddleRecovery(), BrandResolver(), api_apple_webhook)
	// Stripe webhook（overleap 官网 Checkout 的入账通道：invoice.paid 单一入账事件）
	r.POST("/webhook/stripe", log.MiddlewareRequestLog(true), MiddleRecovery(), BrandResolver(), api_stripe_webhook)

	// Chatwoot → FastGPT AI bridge
	chatwootWebhook := r.Group("/webhook")
	chatwootWebhook.Use(log.MiddlewareRequestLog(true), MiddleRecovery())
	chatwoot.Mount(chatwootWebhook, "/chatwoot", handleChatwootEvent)

	// 任务队列触发接口（公开接口，无需认证，供外部 crontab 调用）
	r.GET("/cron/execute", log.MiddlewareRequestLog(true), MiddleRecovery(), api_execute_cron_tasks)

	// 节点相关路由
	api := r.Group("/api")
	log.Debugf(ctx, "registering /api group")

	api.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), BrandResolver(), ApiCORSMiddleware())
	// Preflight: ApiCORSMiddleware handles OPTIONS and aborts with 204
	api.OPTIONS("/*path", func(c *gin.Context) {})
	{
		// 认证相关路由
		auth := api.Group("/auth")
		log.Debugf(ctx, "registering /api/auth group")
		{
			// 获取验证码（统一处理登录和注册）
			auth.POST("/code", api_send_auth_code)
			// @deprecated 旧端点，保留用于向后兼容，将在未来版本中移除
			auth.POST("/login-code", api_send_auth_code)    // 废弃：请使用 /code
			auth.POST("/register-code", api_send_auth_code) // 废弃：请使用 /code
			// 用户登录
			auth.POST("/login", api_login)
			// Password login (with device binding)
			auth.POST("/login/password", api_password_login)
			// Web用户登录（无设备绑定）
			auth.POST("/web-login", api_web_auth)
			// Web 密码登录（无设备绑定，cookie 认证）
			auth.POST("/web-login/password", api_web_password_login)
			// 刷新 token
			auth.POST("/refresh", api_refresh_token)
			// 设备登出
			auth.POST("/logout", AuthRequired(), api_logout)
			// OTT exchange — public endpoint, no auth required
			auth.GET("/ott/exchange", api_exchange_ott)
			// 设备udid认证（已废弃，保留用于向后兼容，总是返回 403）
			auth.POST("/auth-with-device", api_auth_with_device)
		}

		// 邀请码相关路由
		invite := api.Group("/invite")
		log.Debugf(ctx, "registering /api/invite group")
		{
			// 获取邀请码信息
			invite.GET("/code", api_get_invite_code)
			// 获取我的邀请码列表
			invite.GET("/my-codes", AuthRequired(), EnforceDeviceClass(), api_my_inviteCodes)
			// 获取我的最新邀请码
			invite.GET("/my-codes/latest", AuthRequired(), EnforceDeviceClass(), api_my_latest_invite_code)
			// 创建我的邀请码
			invite.POST("/my-codes", AuthRequired(), EnforceDeviceClass(), api_create_my_invite_code)
			// 更新我的邀请码备注
			invite.PUT("/my-codes/:code/remark", AuthRequired(), EnforceDeviceClass(), api_update_my_invite_code_remark)
			// 获取邀请码分享链接
			invite.GET("/my-codes/:code/share-link", AuthRequired(), EnforceDeviceClass(), api_get_share_link)
			// 获取我邀请的用户列表
			invite.GET("/my-users", AuthRequired(), EnforceDeviceClass(), api_my_invite_users)
		}

		// Get tunnel list
		api.GET("/tunnels", AuthRequired(), EnforceDeviceClass(), ProRequired(), DeviceAuthRequired(), api_k2_tunnels)
		api.GET("/tunnels/:protocol", AuthRequired(), EnforceDeviceClass(), ProRequired(), DeviceAuthRequired(), api_k2_tunnels)
		// v20260717: clean tunnels endpoint — protocol="k2s", ipType, serverUrl=k2v5://, no echConfigList
		v20260717 := api.Group("/v20260717")
		v20260717.GET("/tunnels", AuthRequired(), EnforceDeviceClass(), ProRequired(), DeviceAuthRequired(), api_v20260717_tunnels)
		// k2subs subscription endpoint — Basic Auth (udid:token) validated inside handler,
		// because k2 engine sends Basic Auth, not the Bearer token the middleware chain expects.
		api.GET("/subs", api_subs)
		// Get relay list (nodes with has_relay=true)
		api.GET("/relays", AuthRequired(), EnforceDeviceClass(), ProRequired(), DeviceAuthRequired(), api_k2_relays)
		// Get plans (legacy, frozen — app-only)
		api.GET("/plans", api_get_plans)
		// Get plans by product line (new, product-aware)
		api.GET("/products/:product/plans", api_get_product_plans)
		// Get tiers (public — returns all 4 tiers with their active plans)
		api.GET("/tiers", GetTiers)
		// GeoIP country detection (anonymous, no auth)
		api.GET("/geo", api_get_geo)
		// 获取 CA 证书（公开接口，CA 证书是公开信息）
		api.GET("/ca", api_ca_get)
		// 获取应用配置
		api.GET("/app/config", api_get_app_config)
		// 获取 ECH 配置（公开接口，无需认证）
		api.GET("/ech/config", api_fetch_ech_config)
		// Antiblock relay seed — healthy shared-node list for CDN publish + cold-start bootstrap.
		// Auth is an in-handler shared secret (config.yml antiblock.seed_key); real HTTP codes (503/401)
		// are returned intentionally so CI (curl -fsS) fails loudly. See handleAntiblockSeed.
		api.GET("/antiblock/seed", handleAntiblockSeed)
		// 获取礼品卡/授权码信息（公开接口，无需认证）
		api.GET("/license-keys/code/:code", api_get_license_key)
		// 预览授权码折扣信息（需登录，检查当前用户是否符合使用条件）
		api.POST("/license-keys/code/:code/redeem", AuthRequired(), api_redeem_license_key)

		user := api.Group("/user")
		log.Debugf(ctx, "registering /api/user group")
		{
			// 获取用户信息
			user.GET("", AuthRequired(), EnforceDeviceClass(), api_get_user_info)
			user.GET("/info", AuthRequired(), EnforceDeviceClass(), api_get_user_info)
			// 删除设备
			user.DELETE("/devices/:uuid", AuthRequired(), EnforceDeviceClass(), api_delete_device)
			// 更新设备备注
			user.PUT("/devices/:uuid/remark", AuthRequired(), EnforceDeviceClass(), api_update_device_remark)
			// 获取设备列表
			user.GET("/devices", AuthRequired(), EnforceDeviceClass(), api_get_devices)
			// 创建订单
			user.POST("/orders", AuthRequired(), EnforceDeviceClass(), api_create_order)
			// iOS StoreKit IAP：客户端购买完成后上报 transactionId，服务端复核入账
			user.POST("/apple-iap/verify", AuthRequired(), EnforceDeviceClass(), api_apple_iap_verify)
			// Stripe Checkout（overleap 官网购买）：创建 Checkout Session，返回跳转 URL
			user.POST("/stripe/checkout", AuthRequired(), EnforceDeviceClass(), api_stripe_checkout)
			// Stripe Billing Portal（订阅管理/取消面）
			user.POST("/stripe/portal", AuthRequired(), EnforceDeviceClass(), api_stripe_portal)
			// 通知代付人付款（给当前用户的 delegate 发支付邀请邮件）
			user.POST("/orders/:uuid/notify-delegate", AuthRequired(), EnforceDeviceClass(), api_order_notify_delegate)
			// 专属节点订阅只读列表（owner-scoped）
			user.GET("/private-nodes", AuthRequired(), EnforceDeviceClass(), api_get_user_private_nodes)
			// 铸造专属线路路由器凭证（k2subs:// URL）。调用方是普通 app/web 设备，
			// 故不加 EnforceDeviceClass；铸造的是 router 设备（is_gateway=true）。
			user.POST("/gateway-credential", AuthRequired(), api_gateway_credential)
			// 路由器控制密钥（账号级，同 gateway-credential 不加 EnforceDeviceClass）
			user.POST("/router-control-key", AuthRequired(), api_router_control_key)
			user.POST("/router-control-key/reset", AuthRequired(), api_router_control_key_reset)
			// 获取授权变更历史
			user.GET("/pro-histories", AuthRequired(), EnforceDeviceClass(), api_get_pro_histories)
			// 发送绑定邮箱验证码
			user.POST("/email/send-bind-verification", AuthRequired(), EnforceDeviceClass(), api_send_bind_email_verification)
			// 修改邮箱
			user.POST("/email/update-email", AuthRequired(), EnforceDeviceClass(), api_update_login_email)
			// 成员管理
			user.GET("/members", AuthRequired(), EnforceDeviceClass(), api_member_list)
			user.POST("/members", AuthRequired(), EnforceDeviceClass(), api_member_add)
			user.DELETE("/members/:userUUID", AuthRequired(), EnforceDeviceClass(), api_member_remove)
			// 代付人管理
			user.GET("/delegate", AuthRequired(), EnforceDeviceClass(), api_get_delegate)
			user.PUT("/delegate", AuthRequired(), EnforceDeviceClass(), api_put_delegate)
			user.DELETE("/delegate", AuthRequired(), EnforceDeviceClass(), api_delete_delegate)
			// 自我删除账号
			user.DELETE("/delete-account", AuthRequired(), EnforceDeviceClass(), api_delete_user_account)
			// Access key 自助端点已移除 — 统一通过 admin API 管理
			// 更新用户语言偏好
			user.PUT("/language", AuthRequired(), EnforceDeviceClass(), api_update_user_language)
			// 更新 beta channel 订阅状态
			user.PUT("/beta-channel", AuthRequired(), EnforceDeviceClass(), api_update_user_beta_channel)
			// 创建工单
			user.POST("/ticket", AuthOptional(), api_create_ticket)
			// 注册设备日志元数据（S3 上传后调用）
			user.POST("/device-log", AuthOptional(), api_register_device_log)
			// 日志上传后通知（Slack）
			user.POST("/feedback-notify", AuthRequired(), EnforceDeviceClass(), api_feedback_notify)
			// 工单对话
			user.GET("/tickets", AuthRequired(), EnforceDeviceClass(), api_user_list_tickets)
			user.GET("/tickets/unread", AuthRequired(), EnforceDeviceClass(), api_user_tickets_unread)
			user.GET("/tickets/:id", AuthRequired(), EnforceDeviceClass(), api_user_ticket_detail)
			user.POST("/tickets/:id/reply", AuthRequired(), EnforceDeviceClass(), api_user_ticket_reply)
			// 设置/更新密码
			user.POST("/password", AuthRequired(), EnforceDeviceClass(), api_set_password)
			// OTT 签发 — webapp → web auth handoff
			user.POST("/ott", AuthRequired(), EnforceDeviceClass(), api_issue_ott)
			// 连接质量评分
			user.POST("/connection-rating", AuthRequired(), EnforceDeviceClass(), api_create_connection_rating)
		}

		// 分销商管理
		retailer := api.Group("/retailer")
		log.Debugf(ctx, "registering /api/retailer group")
		{
			// 获取分销商等级信息
			retailer.GET("/level", AuthRequired(), RetailerRequired(), api_get_retailer_level)
			// 获取分销商统计数据（包含升级进度）
			retailer.GET("/stats", AuthRequired(), RetailerRequired(), api_get_retailer_stats)
		}

		// 钱包管理
		wallet := api.Group("/wallet")
		log.Debugf(ctx, "registering /api/wallet group")
		{
			// 获取钱包信息（余额实时计算）
			wallet.GET("", AuthRequired(), api_get_wallet)
			// 获取钱包变动记录
			wallet.GET("/changes", AuthRequired(), api_get_wallet_changes)
			// 提现账户管理
			wallet.GET("/withdraw-accounts", AuthRequired(), api_get_withdraw_accounts)
			wallet.POST("/withdraw-accounts", AuthRequired(), api_create_withdraw_account)
			wallet.PUT("/withdraw-accounts/:id/set-default", AuthRequired(), api_set_default_withdraw_account)
			wallet.DELETE("/withdraw-accounts/:id", AuthRequired(), api_delete_withdraw_account)
			// 提现申请
			wallet.GET("/withdraws", AuthRequired(), api_get_withdraw_requests)
			wallet.POST("/withdraws", AuthRequired(), api_create_withdraw_request)
		}

		// 推送通知管理（必须登录：从 JWT 获取用户和设备信息）
		push := api.Group("/push")
		log.Debugf(ctx, "registering /api/push group")
		push.Use(AuthRequired())
		{
			// 注册推送令牌
			push.POST("/token", api_register_push_token)
			// 解绑推送令牌
			push.DELETE("/token", api_unregister_push_token)
		}

		// Strategy system routes (requires device auth)
		strategy := api.Group("/strategy")
		log.Debugf(ctx, "registering /api/strategy group")
		strategy.Use(AuthRequired(), EnforceDeviceClass(), DeviceAuthRequired())
		{
			// Get latest rules configuration
			strategy.GET("/rules", api_strategy_get_rules)
		}

		// Router-only endpoints — gated on an active 专属线路 (any app tier; private
		// lines run on an independent clock, so shared membership is not required).
		router := api.Group("/router")
		log.Debugf(ctx, "registering /api/router group")
		router.Use(AuthRequired(), EnforceDeviceClass(), RouterRequired())
		{
			router.GET("/quota", api_router_quota)
		}

		// Telemetry routes
		telemetry := api.Group("/telemetry")
		log.Debugf(ctx, "registering /api/telemetry group")
		{
			// Rule-miss telemetry (Phase 1): anonymous, unauthenticated,
			// drop-on-receive. Rate limited by source IP inside the
			// handler. See api/telemetry.go for schema + design notes.
			telemetry.POST("/rule_miss", api_telemetry_rule_miss)

			// Submit batch telemetry events (requires device auth)
			authed := telemetry.Group("")
			authed.Use(AuthRequired(), EnforceDeviceClass(), DeviceAuthRequired())
			authed.POST("/batch", api_strategy_telemetry_batch)
		}

		// Pairing beacon / discover (Plan 5b) — BYO router LAN discovery scoped
		// by public source IP. Beacon is unauthenticated (an unconfigured k2r
		// router has no credentials yet); discover requires a logged-in user.
		// No credentials cross the public-IP boundary — see api_pair_beacon.go.
		pair := api.Group("/pair")
		log.Debugf(ctx, "registering /api/pair group")
		{
			pair.POST("/beacon", api_pair_beacon)
			pair.GET("/discover", AuthRequired(), api_pair_discover)
		}

		// Route diagnosis routes (requires device auth)
		diagnosis := api.Group("/diagnosis")
		log.Debugf(ctx, "registering /api/diagnosis group")
		diagnosis.Use(AuthRequired(), EnforceDeviceClass(), DeviceAuthRequired())
		{
			// Get outbound route for a specific node
			diagnosis.GET("/outbound-route", api_outbound_route)
		}

		// Usage analytics (no auth)
		stats := api.Group("/stats")
		log.Debugf(ctx, "registering /api/stats group")
		{
			stats.POST("/events", api_stats_ingest)
			stats.POST("/k2s-download", api_stats_k2s_download)
		}

		// 问卷调查
		survey := api.Group("/survey")
		{
			survey.POST("/submit", AuthRequired(), api_survey_submit)
			survey.GET("/status", AuthRequired(), api_survey_status)
		}

	}

	admin := r.Group("/app")
	log.Debugf(ctx, "registering /app group")
	admin.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), BrandResolver(), CORSMiddleware(), AdminRequired())
	{
		// 套餐管理
		admin.GET("/plans", api_admin_list_plans)
		admin.POST("/plans", api_admin_create_plan)
		admin.PUT("/plans/:id", api_admin_update_plan)
		admin.DELETE("/plans/:id", api_admin_delete_plan)
		admin.POST("/plans/:id/restore", api_admin_restore_plan)
		// Tier 列表（含 inactive plans，用于后台 plan 管理 UI）
		admin.GET("/tiers", GetAdminTiers)

		// 用户管理
		admin.PUT("/users/:uuid/tier", api_admin_change_user_tier)
		admin.PUT("/users/:uuid/retailer-status", api_admin_update_user_retailer_status)
		admin.PUT("/users/:uuid/retailer-contacts", api_admin_update_retailer_contacts)
		// 用户硬删除（批量）
		admin.POST("/users/hard-delete", api_admin_hard_delete_users)
		// 用户成员管理
		admin.GET("/users/:uuid/members", api_admin_member_list)
		admin.POST("/users/:uuid/members", api_admin_member_add)
		admin.DELETE("/users/:uuid/members/:memberUUID", api_admin_member_remove)
		// 分销商配置管理
		admin.PUT("/users/:uuid/retailer-config", api_admin_update_retailer_config)
		// 用户会员时长管理
		admin.POST("/users/:uuid/membership", api_admin_add_user_membership)
		// 用户邮箱管理
		admin.PUT("/users/:uuid/email", api_admin_update_user_email)
		// 用户密码管理（管理员代为重置）
		admin.POST("/users/:uuid/password", api_admin_set_user_password)
		// 用户角色管理（仅超级管理员）
		admin.PUT("/users/:uuid/roles", api_admin_set_user_roles)
		admin.POST("/users/:uuid/devices/:udid/test-token", api_admin_issue_test_token)
		admin.POST("/users/:uuid/access-key", api_admin_generate_access_key)
		admin.DELETE("/users/:uuid/access-key", api_admin_revoke_access_key)
		// 用户封禁管理（管理员直接执行，无需审批）
		admin.POST("/users/:uuid/block", api_admin_block_user)
		admin.POST("/users/:uuid/unblock", api_admin_unblock_user)

		// Device statistics
		admin.GET("/devices/statistics", api_admin_get_device_statistics)
		admin.GET("/devices/active", api_admin_get_active_devices)

		// User and Order statistics
		admin.GET("/users/statistics", api_admin_get_user_statistics)
		admin.GET("/orders/statistics", api_admin_get_order_statistics)
		admin.GET("/connection-ratings/statistics", api_admin_connection_rating_statistics)

		// 分销商管理 — 已移至 opsAdmin 组（RoleMarketing）

		// 管理员用户列表（用于跟进人选择）
		admin.GET("/admins", api_admin_list_admin_users)

		// 钱包和提现管理
		admin.GET("/wallet/withdraws", api_admin_list_withdraw_requests)
		admin.POST("/wallet/withdraws/:id/approve", api_admin_approve_withdraw)
		admin.POST("/wallet/withdraws/:id/complete", api_admin_complete_withdraw)
		admin.POST("/orders/:uuid/refund", api_admin_refund_order)

		// 订单管理
		// 订单管理 — GET 已移至 opsAdmin 组（Support + Marketing 可读）

		// 优惠活动管理 — 已移至 opsAdmin 组（RoleMarketing）

		// LicenseKey admin management — 已移至 opsAdmin 组（RoleMarketing）

		// 公告管理 — 已移至 opsAdmin 组（RoleMarketing）

		// EDM邮件营销管理 — 已移至 opsAdmin 组（RoleMarketing）

		// Usage analytics overview
		admin.GET("/stats/overview", api_admin_usage_overview)

		// User traffic accounting: monthly ranking + per-user detail
		admin.GET("/traffic/top-users", api_admin_traffic_top_users)
		admin.GET("/traffic/user", api_admin_traffic_user)

		// 问卷调查统计
		admin.GET("/surveys/stats", api_admin_survey_stats)

		// Strategy rules management
		strategy := admin.Group("/strategy")
		{
			strategy.GET("/rules", api_admin_strategy_list)                       // List all versions
			strategy.POST("/rules", api_admin_strategy_create)                    // Create new version
			strategy.GET("/rules/:version", api_admin_strategy_get)               // Get specific version
			strategy.PUT("/rules/:version/activate", api_admin_strategy_activate) // Activate version
			strategy.DELETE("/rules/:version", api_admin_strategy_delete)         // Delete version
		}

	}

	// 审批管理路由
	// list/detail/cancel: AuthRequired（角色用户可看自己的）
	// approve/reject: AdminRequired（仅 is_admin 可审批）
	approvalRoutes := r.Group("/app/approvals")
	approvalRoutes.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), BrandResolver(), CORSMiddleware(), AuthRequired())
	{
		approvalRoutes.GET("", api_admin_list_approvals)
		approvalRoutes.GET("/:id", api_admin_get_approval)
		approvalRoutes.POST("/:id/cancel", api_admin_cancel_approval)
	}
	approvalAdmin := r.Group("/app/approvals")
	approvalAdmin.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), BrandResolver(), CORSMiddleware(), AdminRequired())
	{
		approvalAdmin.POST("/:id/approve", api_admin_approve_approval)
		approvalAdmin.POST("/:id/reject", api_admin_reject_approval)
	}

	// opsAdmin 运维权限路由组：不需要超级管理员，通过角色位掩码控制访问
	// 超级管理员（IsAdmin=true）经由 RoleRequired 内部 bypass 直接通过
	opsAdmin := r.Group("/app")
	log.Debugf(ctx, "registering /app opsAdmin group")
	opsAdmin.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), BrandResolver(), CORSMiddleware(), AuthRequired())
	{
		// No role restriction — every authenticated user can see their own permissions
		opsAdmin.GET("/my-permissions", api_admin_my_permissions)

		viewOrEdit := RoleDevopsViewer | RoleDevopsEditor
		allOpsRoles := RoleDevopsViewer | RoleDevopsEditor | RoleSupport

		// 隧道管理
		opsAdmin.GET("/tunnels", RoleRequired(viewOrEdit), api_admin_list_tunnels)
		opsAdmin.PUT("/tunnels/:id", RoleRequired(RoleDevopsEditor), api_admin_update_tunnel)
		opsAdmin.DELETE("/tunnels/:id", RoleRequired(RoleDevopsEditor), api_admin_delete_tunnel)

		// 物理节点管理
		opsAdmin.GET("/nodes", RoleRequired(viewOrEdit), api_admin_list_nodes)
		opsAdmin.PUT("/nodes/:ipv4", RoleRequired(RoleDevopsEditor), api_admin_update_node)
		opsAdmin.DELETE("/nodes/:ipv4", RoleRequired(RoleDevopsEditor), api_admin_delete_node)

		// 云实例（只读）
		opsAdmin.GET("/cloud/instances", RoleRequired(viewOrEdit), api_admin_list_cloud_instances)
		opsAdmin.GET("/cloud/instances/:id", RoleRequired(viewOrEdit), api_admin_get_cloud_instance)
		opsAdmin.GET("/cloud/accounts", RoleRequired(viewOrEdit), api_admin_list_cloud_accounts)
		opsAdmin.GET("/cloud/regions", RoleRequired(viewOrEdit), api_admin_list_cloud_regions)
		opsAdmin.GET("/cloud/plans", RoleRequired(viewOrEdit), api_admin_list_cloud_plans)
		opsAdmin.GET("/cloud/images", RoleRequired(viewOrEdit), api_admin_list_cloud_images)

		// 云实例（读写）
		opsAdmin.POST("/cloud/instances/sync", RoleRequired(RoleDevopsEditor), api_admin_sync_all_cloud_instances)
		opsAdmin.POST("/cloud/instances/:id/change-ip", RoleRequired(RoleDevopsEditor), api_admin_change_ip_cloud_instance)
		opsAdmin.PUT("/cloud/instances/:id/traffic-config", RoleRequired(RoleDevopsEditor), api_admin_update_traffic_config)
		opsAdmin.POST("/cloud/instances", RoleRequired(RoleDevopsEditor), api_admin_create_cloud_instance)
		opsAdmin.DELETE("/cloud/instances/:id", RoleRequired(RoleDevopsEditor), api_admin_delete_cloud_instance)

		// 专属节点运维任务队列（外部 AI agent / 运维消费）
		opsAdmin.GET("/node-operations", RoleRequired(viewOrEdit), adminListNodeOperations)
		opsAdmin.POST("/node-operations", RoleRequired(RoleDevopsEditor), adminCreateNodeOperation)
		opsAdmin.POST("/node-operations/:id/claim", RoleRequired(RoleDevopsEditor), adminClaimNodeOperation)
		opsAdmin.POST("/node-operations/:id/update", RoleRequired(RoleDevopsEditor), adminUpdateNodeOperation)

		// 企业路由器（多槽多线路）
		opsAdmin.GET("/enterprise/customers", RoleRequired(viewOrEdit), api_admin_list_enterprise_customers)
		opsAdmin.POST("/enterprise/customers", RoleRequired(RoleDevopsEditor), api_admin_create_enterprise_customer)
		opsAdmin.PUT("/enterprise/customers/:id", RoleRequired(RoleDevopsEditor), api_admin_update_enterprise_customer)
		opsAdmin.GET("/enterprise/customers/:id/lines", RoleRequired(viewOrEdit), api_admin_list_enterprise_lines)
		opsAdmin.POST("/enterprise/lines", RoleRequired(RoleDevopsEditor), api_admin_create_enterprise_line)
		opsAdmin.PUT("/enterprise/lines/:id", RoleRequired(RoleDevopsEditor), api_admin_update_enterprise_line)
		opsAdmin.DELETE("/enterprise/lines/:id", RoleRequired(RoleDevopsEditor), api_admin_delete_enterprise_line)
		opsAdmin.GET("/enterprise/bindings", RoleRequired(viewOrEdit), api_admin_list_enterprise_bindings)
		opsAdmin.PUT("/enterprise/bindings", RoleRequired(RoleDevopsEditor), api_admin_upsert_enterprise_binding)
		opsAdmin.DELETE("/enterprise/bindings/:id", RoleRequired(RoleDevopsEditor), api_admin_delete_enterprise_binding)

		// 用户查看（只读）— DevOps + Support + Marketing 均可访问
		readRoles := viewOrEdit | RoleSupport | RoleMarketing
		opsAdmin.GET("/users", RoleRequired(readRoles), api_admin_list_users)
		opsAdmin.GET("/users/:uuid", RoleRequired(readRoles), api_admin_get_user_detail)
		opsAdmin.GET("/users/:uuid/devices", RoleRequired(readRoles), api_admin_get_user_devices)

		// 订单查看（只读）— Support + Marketing 可访问
		opsAdmin.GET("/orders", RoleRequired(readRoles), api_admin_list_orders)
		opsAdmin.GET("/orders/:uuid", RoleRequired(readRoles), api_admin_get_order_detail)

		// 设备日志 + 工单
		opsAdmin.GET("/device-logs", RoleRequired(allOpsRoles), api_admin_list_device_logs)
		opsAdmin.GET("/feedback-tickets", RoleRequired(allOpsRoles), api_admin_list_feedback_tickets)
		opsAdmin.PUT("/feedback-tickets/:id/resolve", RoleRequired(RoleSupport), api_admin_resolve_feedback_ticket)
		opsAdmin.PUT("/feedback-tickets/:id/close", RoleRequired(RoleSupport), api_admin_close_feedback_ticket)
		opsAdmin.POST("/feedback-tickets/:id/reply", RoleRequired(RoleSupport), api_admin_reply_ticket)
		opsAdmin.GET("/feedback-tickets/:id/replies", RoleRequired(allOpsRoles), api_admin_list_ticket_replies)

		// 分销商管理（Marketing 角色）
		opsAdmin.GET("/retailers", RoleRequired(RoleMarketing), api_admin_list_retailers)
		opsAdmin.GET("/retailers/todos", RoleRequired(RoleMarketing), api_admin_list_retailer_todos)
		opsAdmin.GET("/retailers/:uuid", RoleRequired(RoleMarketing), api_admin_get_retailer_detail)
		opsAdmin.PUT("/retailers/:uuid/level", RoleRequired(RoleMarketing), api_admin_update_retailer_config)
		opsAdmin.PUT("/retailers/:uuid/notes", RoleRequired(RoleMarketing), api_admin_update_retailer_notes)
		opsAdmin.POST("/retailers/:uuid/notes", RoleRequired(RoleMarketing), api_admin_create_retailer_note)
		opsAdmin.GET("/retailers/:uuid/notes", RoleRequired(RoleMarketing), api_admin_list_retailer_notes)
		opsAdmin.PUT("/retailers/:uuid/notes/:noteId", RoleRequired(RoleMarketing), api_admin_update_retailer_note)
		opsAdmin.DELETE("/retailers/:uuid/notes/:noteId", RoleRequired(RoleMarketing), api_admin_delete_retailer_note)

		// EDM 邮件营销管理（Marketing 角色）
		// 公告管理（Marketing 角色）
		opsAdmin.GET("/announcements", RoleRequired(RoleMarketing), api_admin_list_announcements)
		opsAdmin.POST("/announcements", RoleRequired(RoleMarketing), api_admin_create_announcement)
		opsAdmin.PUT("/announcements/:id", RoleRequired(RoleMarketing), api_admin_update_announcement)
		opsAdmin.DELETE("/announcements/:id", RoleRequired(RoleMarketing), api_admin_delete_announcement)
		opsAdmin.POST("/announcements/:id/activate", RoleRequired(RoleMarketing), api_admin_activate_announcement)
		opsAdmin.POST("/announcements/:id/deactivate", RoleRequired(RoleMarketing), api_admin_deactivate_announcement)

		edmOps := opsAdmin.Group("/edm")
		{
			edmOps.GET("/templates", RoleRequired(RoleMarketing), api_admin_list_email_templates)
			edmOps.POST("/templates", RoleRequired(RoleMarketing), api_admin_create_email_template)
			edmOps.PUT("/templates/:id", RoleRequired(RoleMarketing), api_admin_update_email_template)
			edmOps.DELETE("/templates/:id", RoleRequired(RoleMarketing), api_admin_delete_email_template)
			edmOps.POST("/templates/:id/translate/:language", RoleRequired(RoleMarketing), api_admin_translate_email_template)
			edmOps.POST("/send", RoleRequired(RoleMarketing), api_admin_send_templated_emails)
			edmOps.GET("/send-logs", RoleRequired(RoleMarketing), api_admin_list_email_send_logs)
			edmOps.GET("/send-logs/stats", RoleRequired(RoleMarketing), api_admin_get_email_send_log_stats)
		}

		// 优惠活动管理（审批流程已覆盖，RoleMarketing 可操作）
		opsAdmin.GET("/campaigns", RoleRequired(RoleMarketing), api_admin_list_campaigns)
		opsAdmin.GET("/campaigns/:id", RoleRequired(RoleMarketing), api_admin_get_campaign)
		opsAdmin.POST("/campaigns", RoleRequired(RoleMarketing), api_admin_create_campaign)
		opsAdmin.PUT("/campaigns/:id", RoleRequired(RoleMarketing), api_admin_update_campaign)
		opsAdmin.DELETE("/campaigns/:id", RoleRequired(RoleMarketing), api_admin_delete_campaign)
		opsAdmin.GET("/campaigns/code/:code/stats", RoleRequired(RoleMarketing), api_admin_get_campaign_stats)
		opsAdmin.GET("/campaigns/code/:code/orders", RoleRequired(RoleMarketing), api_admin_get_campaign_orders)
		opsAdmin.GET("/campaigns/code/:code/funnel", RoleRequired(RoleMarketing), api_admin_get_campaign_funnel)

		// 授权码批次管理
		opsAdmin.GET("/license-key-batches/stats", RoleRequired(RoleMarketing), api_admin_license_key_batch_stats)
		opsAdmin.GET("/license-key-batches/stats/by-source", RoleRequired(RoleMarketing), api_admin_license_key_batch_stats_by_source)
		opsAdmin.GET("/license-key-batches/stats/trend", RoleRequired(RoleMarketing), api_admin_license_key_batch_stats_trend)
		opsAdmin.POST("/license-key-batches", RoleRequired(RoleMarketing), api_admin_create_license_key_batch)
		opsAdmin.GET("/license-key-batches", RoleRequired(RoleMarketing), api_admin_list_license_key_batches)
		opsAdmin.GET("/license-key-batches/:id", RoleRequired(RoleMarketing), api_admin_get_license_key_batch)
		opsAdmin.GET("/license-key-batches/:id/keys", RoleRequired(RoleMarketing), api_admin_list_license_key_batch_keys)
		opsAdmin.DELETE("/license-key-batches/:id", RoleRequired(RoleMarketing), api_admin_delete_license_key_batch)

		// LicenseKey 管理（RoleMarketing 可操作）
		opsAdmin.GET("/license-keys", RoleRequired(RoleMarketing), api_admin_list_license_keys)
		opsAdmin.DELETE("/license-keys/:id", RoleRequired(RoleMarketing), api_admin_delete_license_key)
	}

	// 节点管理路由（需要节点认证）
	slaveManage := r.Group("/slave")
	log.Debugf(ctx, "registering /slave group")
	slaveManage.Use(log.MiddlewareRequestLog(true), MiddleRecovery())
	{
		// 物理节点管理（新版API - RESTful）
		slaveManage.PUT("/nodes/:ipv4", api_slave_node_upsert)                                                // 注册/更新物理节点
		slaveManage.PUT("/nodes/:ipv4/tunnels/:domain", SlaveAuthRequired(), api_slave_node_upsert_tunnel)    // 添加/更新隧道
		slaveManage.DELETE("/nodes/:ipv4/tunnels/:domain", SlaveAuthRequired(), api_slave_node_delete_tunnel) // 删除隧道
		slaveManage.DELETE("/nodes/:ipv4", SlaveAuthRequired(), api_slave_node_unregister)                    // 节点自注销（graceful shutdown）

		// 节点状态上报
		slaveManage.POST("/report/status", SlaveAuthRequired(), api_slave_report_status)

		// 流量计量心跳（节点上报累计流量，Center 回 serve/stop 裁决 + epoch 身份）
		slaveManage.POST("/usage", SlaveAuthRequired(), api_slave_node_report_usage)

		// 设备流量增量上报（用户级流量记账）
		slaveManage.POST("/device-traffic", SlaveAuthRequired(), api_slave_device_traffic)

		// 设备认证（自动识别 JWT token 或密码）
		slaveManage.POST("/device-check-auth", SlaveAuthRequired(), api_slave_device_check_auth)

		// 获取加速节点列表
		slaveManage.GET("/accelerate-tunnels", SlaveAuthRequired(), api_slave_accelerate_tunnels)

		// 域名解析（DNS 式单域名查询）
		slaveManage.GET("/resolve-domain", SlaveAuthRequired(), api_slave_resolve_domain)

		// ECH 密钥同步（内部接口，需要 SlaveAuthRequired）
		slaveManage.GET("/ech/keys", SlaveAuthRequired(), api_slave_fetch_ech_keys)

		// Route diagnosis - Slave reports inbound diagnosis results
		slaveManage.POST("/nodes/:ipv4/route-diagnosis", SlaveAuthRequired(), api_slave_report_route_diagnosis)
	}

	// CSR (Certificate Signing Request) for sslip.io/nip.io IP-encoded domains
	// Public API - domain verification is done via challenge-response
	csr := r.Group("/csr")
	log.Debugf(ctx, "registering /csr group")
	csr.Use(log.MiddlewareRequestLog(true), MiddleRecovery())
	{
		csr.POST("/submit", api_csr_submit)
		csr.POST("/verify", api_csr_verify)
	}

	// 注册任务处理器并挂载监控面板
	// 路径: /app/asynqmon - 与其他管理接口保持一致（/app/* 前缀）
	// 注意: asynqmon 需要单独的路由组，不能直接应用 AdminRequired()
	// 因为它的静态资源（HTML/CSS/JS）会被中间件拦截返回 JSON 错误
	InitWorker()
	asynqmonGroup := r.Group("/app")
	asynqmonGroup.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), asynqmonAuthMiddleware())
	asynq.Mount(asynqmonGroup, "/asynqmon")

	log.Infof(ctx, "router setup completed")
	return r
}
