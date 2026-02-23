package center

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/asynq"
	"github.com/wordgate/qtoolkit/github/issue"
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
	r.POST("/webhook/wordgate", log.MiddlewareRequestLog(true), MiddleRecovery(), api_wordgate_webhook)

	// 任务队列触发接口（公开接口，无需认证，供外部 crontab 调用）
	r.GET("/cron/execute", log.MiddlewareRequestLog(true), MiddleRecovery(), api_execute_cron_tasks)

	// 节点相关路由
	api := r.Group("/api")
	log.Debugf(ctx, "registering /api group")

	api.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), ApiCORSMiddleware())
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
			// 刷新 token
			auth.POST("/refresh", api_refresh_token)
			// 设备登出
			auth.POST("/logout", AuthRequired(), api_logout)
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
			invite.GET("/my-codes", AuthRequired(), api_my_inviteCodes)
			// 获取我的最新邀请码
			invite.GET("/my-codes/latest", AuthRequired(), api_my_latest_invite_code)
			// 创建我的邀请码
			invite.POST("/my-codes", AuthRequired(), api_create_my_invite_code)
			// 更新我的邀请码备注
			invite.PUT("/my-codes/:code/remark", AuthRequired(), api_update_my_invite_code_remark)
			// 获取邀请码分享链接
			invite.GET("/my-codes/:code/share-link", AuthRequired(), api_get_share_link)
			// 获取我邀请的用户列表
			invite.GET("/my-users", AuthRequired(), api_my_invite_users)
		}

		// Get tunnel list
		api.GET("/tunnels", AuthRequired(), ProRequired(), DeviceAuthRequired(), api_k2_tunnels)
		api.GET("/tunnels/:protocol", AuthRequired(), ProRequired(), DeviceAuthRequired(), api_k2_tunnels)
		// Get relay list (nodes with has_relay=true)
		api.GET("/relays", AuthRequired(), ProRequired(), DeviceAuthRequired(), api_k2_relays)
		// Get plans
		api.GET("/plans", api_get_plans)
		// 获取 CA 证书（公开接口，CA 证书是公开信息）
		api.GET("/ca", api_ca_get)
		// 获取应用配置
		api.GET("/app/config", api_get_app_config)
		// 获取 ECH 配置（公开接口，无需认证）
		api.GET("/ech/config", api_fetch_ech_config)

		user := api.Group("/user")
		log.Debugf(ctx, "registering /api/user group")
		{
			// 获取用户信息
			user.GET("", AuthRequired(), api_get_user_info)
			user.GET("/info", AuthRequired(), api_get_user_info)
			// 删除设备
			user.DELETE("/devices/:uuid", AuthRequired(), api_delete_device)
			// 更新设备备注
			user.PUT("/devices/:uuid/remark", AuthRequired(), api_update_device_remark)
			// 获取设备列表
			user.GET("/devices", AuthRequired(), api_get_devices)
			// 创建订单
			user.POST("/orders", AuthRequired(), api_create_order)
			// 获取授权变更历史
			user.GET("/pro-histories", AuthRequired(), api_get_pro_histories)
			// 发送绑定邮箱验证码
			user.POST("/email/send-bind-verification", AuthRequired(), api_send_bind_email_verification)
			// 修改邮箱
			user.POST("/email/update-email", AuthRequired(), api_update_login_email)
			// 成员管理
			user.GET("/members", AuthRequired(), api_member_list)
			user.POST("/members", AuthRequired(), api_member_add)
			user.DELETE("/members/:userUUID", AuthRequired(), api_member_remove)
			// 代付人管理
			user.GET("/delegate", AuthRequired(), api_get_delegate)
			user.DELETE("/delegate", AuthRequired(), api_reject_delegate)
			// 自我删除账号
			user.DELETE("/delete-account", AuthRequired(), api_delete_user_account)
			// 获取AccessKey
			user.GET("/access-key", AuthRequired(), api_get_access_key)
			// 重新生成AccessKey
			user.POST("/access-key/regenerate", AuthRequired(), api_regenerate_access_key)
			// 更新用户语言偏好
			user.PUT("/language", AuthRequired(), api_update_user_language)
			// 创建工单
			user.POST("/ticket", AuthRequired(), api_create_ticket)
			// 设置/更新密码
			user.POST("/password", AuthRequired(), api_set_password)
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
		strategy.Use(AuthRequired(), DeviceAuthRequired())
		{
			// Get latest rules configuration
			strategy.GET("/rules", api_strategy_get_rules)
		}

		// Telemetry routes (requires device auth)
		telemetry := api.Group("/telemetry")
		log.Debugf(ctx, "registering /api/telemetry group")
		telemetry.Use(AuthRequired(), DeviceAuthRequired())
		{
			// Submit batch telemetry events
			telemetry.POST("/batch", api_strategy_telemetry_batch)
		}

		// Route diagnosis routes (requires device auth)
		diagnosis := api.Group("/diagnosis")
		log.Debugf(ctx, "registering /api/diagnosis group")
		diagnosis.Use(AuthRequired(), DeviceAuthRequired())
		{
			// Get outbound route for a specific node
			diagnosis.GET("/outbound-route", api_outbound_route)
		}

	}

	admin := r.Group("/app")
	log.Debugf(ctx, "registering /app group")
	admin.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), CORSMiddleware(), AdminRequired())
	{
		// 隧道管理
		admin.GET("/tunnels", api_admin_list_tunnels)
		admin.PUT("/tunnels/:id", api_admin_update_tunnel)
		admin.DELETE("/tunnels/:id", api_admin_delete_tunnel)

		// 物理节点管理
		admin.GET("/nodes", api_admin_list_nodes)
		admin.GET("/nodes/batch-matrix", api_admin_nodes_batch_matrix)
		admin.PUT("/nodes/:ipv4", api_admin_update_node)
		admin.DELETE("/nodes/:ipv4", api_admin_delete_node)

		// 套餐管理
		admin.GET("/plans", api_admin_list_plans)
		admin.POST("/plans", api_admin_create_plan)
		admin.PUT("/plans/:id", api_admin_update_plan)
		admin.DELETE("/plans/:id", api_admin_delete_plan)
		admin.POST("/plans/:id/restore", api_admin_restore_plan)

		// 用户管理
		admin.GET("/users", api_admin_list_users)
		admin.GET("/users/:uuid", api_admin_get_user_detail)
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
		// 用户设备管理
		admin.GET("/users/:uuid/devices", api_admin_get_user_devices)
		admin.POST("/users/:uuid/devices/:udid/test-token", api_admin_issue_test_token)

		// Device statistics
		admin.GET("/devices/statistics", api_admin_get_device_statistics)
		admin.GET("/devices/active", api_admin_get_active_devices)

		// User and Order statistics
		admin.GET("/users/statistics", api_admin_get_user_statistics)
		admin.GET("/orders/statistics", api_admin_get_order_statistics)

		// 分销商管理
		admin.GET("/retailers", api_admin_list_retailers)
		admin.GET("/retailers/todos", api_admin_list_retailer_todos)
		admin.GET("/retailers/:uuid", api_admin_get_retailer_detail)
		admin.PUT("/retailers/:uuid/level", api_admin_update_retailer_config) // 更新级别（复用用户页的函数）
		admin.PUT("/retailers/:uuid/notes", api_admin_update_retailer_notes)  // 更新备注
		admin.POST("/retailers/:uuid/notes", api_admin_create_retailer_note)
		admin.GET("/retailers/:uuid/notes", api_admin_list_retailer_notes)
		admin.PUT("/retailers/:uuid/notes/:noteId", api_admin_update_retailer_note)
		admin.DELETE("/retailers/:uuid/notes/:noteId", api_admin_delete_retailer_note)

		// 管理员用户列表（用于跟进人选择）
		admin.GET("/admins", api_admin_list_admin_users)

		// 钱包和提现管理
		admin.GET("/wallet/withdraws", api_admin_list_withdraw_requests)
		admin.POST("/wallet/withdraws/:id/approve", api_admin_approve_withdraw)
		admin.POST("/wallet/withdraws/:id/complete", api_admin_complete_withdraw)

		// 订单管理
		admin.GET("/orders", api_admin_list_orders)
		admin.GET("/orders/:uuid", api_admin_get_order_detail)

		// 优惠活动管理
		admin.GET("/campaigns", api_admin_list_campaigns)
		admin.GET("/campaigns/:id", api_admin_get_campaign)
		admin.POST("/campaigns", api_admin_create_campaign)
		admin.PUT("/campaigns/:id", api_admin_update_campaign)
		admin.DELETE("/campaigns/:id", api_admin_delete_campaign)

		// Campaign统计分析路由 - 使用code前缀避免冲突
		admin.GET("/campaigns/code/:code/stats", api_admin_get_campaign_stats)
		admin.GET("/campaigns/code/:code/orders", api_admin_get_campaign_orders)
		admin.GET("/campaigns/code/:code/funnel", api_admin_get_campaign_funnel)

		// EDM邮件营销管理
		edm := admin.Group("/edm")
		{
			// 邮件模板管理
			edm.GET("/templates", api_admin_list_email_templates)
			edm.POST("/templates", api_admin_create_email_template)
			edm.PUT("/templates/:id", api_admin_update_email_template)
			edm.DELETE("/templates/:id", api_admin_delete_email_template)
			edm.POST("/templates/:id/translate/:language", api_admin_translate_email_template) // 自动翻译模板

			// EDM发送任务管理（基于 Asynq）
			edm.POST("/tasks", api_admin_create_edm_task)               // 创建EDM任务（入队到Asynq）
			edm.POST("/preview-targets", api_admin_preview_edm_targets) // 预览目标用户

			// 邮件发送日志管理
			edm.GET("/send-logs", api_admin_list_email_send_logs)           // 获取发送日志列表（分页，默认100条/页）
			edm.GET("/send-logs/stats", api_admin_get_email_send_log_stats) // 获取发送统计

			// 注意：任务监控请使用 asynqmon UI (/app/asynqmon)
		}

		// Cloud instance management
		admin.GET("/cloud/instances", api_admin_list_cloud_instances)
		admin.POST("/cloud/instances/sync", api_admin_sync_all_cloud_instances)
		admin.GET("/cloud/instances/:id", api_admin_get_cloud_instance)
		admin.POST("/cloud/instances/:id/change-ip", api_admin_change_ip_cloud_instance)
		admin.PUT("/cloud/instances/:id/traffic-config", api_admin_update_traffic_config)
		admin.POST("/cloud/instances", api_admin_create_cloud_instance)
		admin.DELETE("/cloud/instances/:id", api_admin_delete_cloud_instance)
		admin.GET("/cloud/accounts", api_admin_list_cloud_accounts)
		admin.GET("/cloud/regions", api_admin_list_cloud_regions)
		admin.GET("/cloud/plans", api_admin_list_cloud_plans)
		admin.GET("/cloud/images", api_admin_list_cloud_images)

		// SSH Terminal (WebSocket)
		admin.GET("/nodes/:ipv4/terminal", api_admin_ssh_terminal)

		// WebSocket authentication token
		// Used by frontend to get a short-lived token for cross-domain WebSocket connections
		admin.GET("/ws-token", api_get_ws_token)

		// Batch script execution management
		admin.POST("/batch-scripts", api_admin_batch_scripts_create)
		admin.GET("/batch-scripts", api_admin_batch_scripts_list)
		admin.GET("/batch-scripts/:id", api_admin_batch_scripts_detail)
		admin.PUT("/batch-scripts/:id", api_admin_batch_scripts_update)
		admin.DELETE("/batch-scripts/:id", api_admin_batch_scripts_delete)
		admin.GET("/batch-scripts/:id/versions", api_admin_batch_scripts_versions)
		admin.GET("/batch-scripts/:id/versions/:version", api_admin_batch_scripts_version_detail)
		admin.POST("/batch-scripts/:id/versions/:version/restore", api_admin_batch_scripts_version_restore)
		admin.POST("/batch-scripts/:id/test", api_admin_batch_scripts_test)

		admin.POST("/batch-tasks", api_admin_batch_tasks_create)
		admin.GET("/batch-tasks", api_admin_batch_tasks_list)
		admin.GET("/batch-tasks/scheduled", api_admin_batch_tasks_scheduled)
		admin.GET("/batch-tasks/:id", api_admin_batch_tasks_detail)
		admin.PUT("/batch-tasks/:id/pause", api_admin_batch_tasks_pause)
		admin.PUT("/batch-tasks/:id/resume", api_admin_batch_tasks_resume)
		admin.POST("/batch-tasks/:id/retry", api_admin_batch_tasks_retry)
		admin.PUT("/batch-tasks/:id/schedule", api_admin_batch_tasks_schedule_update)
		admin.DELETE("/batch-tasks/:id/schedule", api_admin_batch_tasks_schedule_delete)
		admin.DELETE("/batch-tasks/:id", api_admin_batch_tasks_delete)

		// Strategy rules management
		strategy := admin.Group("/strategy")
		{
			strategy.GET("/rules", api_admin_strategy_list)           // List all versions
			strategy.POST("/rules", api_admin_strategy_create)        // Create new version
			strategy.GET("/rules/:version", api_admin_strategy_get)   // Get specific version
			strategy.PUT("/rules/:version/activate", api_admin_strategy_activate) // Activate version
			strategy.DELETE("/rules/:version", api_admin_strategy_delete)         // Delete version
		}

	}

	// GitHub Issues routes (requires authentication)
	// Uses qtoolkit/github/issue RegisterRoutes with middleware to set X-App-User-ID header
	issues := api.Group("/issues")
	log.Debugf(ctx, "registering /api/issues group")
	issues.Use(AuthRequired(), setAppUserIDHeader())
	issue.RegisterRoutes(issues)

	// 节点管理路由（需要节点认证）
	slaveManage := r.Group("/slave")
	log.Debugf(ctx, "registering /slave group")
	slaveManage.Use(log.MiddlewareRequestLog(true), MiddleRecovery())
	{
		// 物理节点管理（新版API - RESTful）
		slaveManage.PUT("/nodes/:ipv4", api_slave_node_upsert)                                                // 注册/更新物理节点
		slaveManage.PUT("/nodes/:ipv4/tunnels/:domain", SlaveAuthRequired(), api_slave_node_upsert_tunnel)    // 添加/更新隧道
		slaveManage.DELETE("/nodes/:ipv4/tunnels/:domain", SlaveAuthRequired(), api_slave_node_delete_tunnel) // 删除隧道
		slaveManage.DELETE("/nodes/:ipv4", SlaveAuthRequired(), api_slave_node_unregister)                 // 节点自注销（graceful shutdown）

		// 节点状态上报
		slaveManage.POST("/report/status", SlaveAuthRequired(), api_slave_report_status)

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
