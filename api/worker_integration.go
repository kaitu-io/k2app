package center

import (
	"context"
	"fmt"
	"time"

	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/wordgate/qtoolkit/asynq"
	"github.com/wordgate/qtoolkit/log"
)

// =====================================================================
// Worker 集成 (基于 qtoolkit/asynq)
// =====================================================================

// 任务类型常量
const (
	TaskTypePushSend               = "push:send"
	TaskTypeTemplatedEmailSend     = "edm:send-templated"
	TaskTypePrivateNodeTrafficWarn = "private_node:traffic-warn"
)

// TemplatedEmailTaskPayload 通用邮件发送任务载荷
type TemplatedEmailTaskPayload struct {
	Request SendEmailsRequest `json:"request"`
}

// InitWorker 初始化 Worker
// 注册任务处理函数
func InitWorker() {
	initAPNs()
	// GeoIP: qtoolkit/geoip is lazy-initialized on first Country() call.
	// No explicit init needed.

	asynq.Handle(TaskTypePushSend, handlePushTask)
	asynq.Handle(TaskTypePushBatch, handlePushBatchTask)
	asynq.Handle(TaskTypeTemplatedEmailSend, handleTemplatedEmailTask)
	asynq.Handle(TaskTypeRenewalReminder, handleRenewalReminderTask)
	asynq.Handle(TaskTypeTrialRecall, handleTrialRecallTask)
	asynq.Handle(TaskTypeAbandonedOrderHourly, handleAbandonedOrderHourlyTask)
	asynq.Handle(TaskTypeAbandonedOrderDaily, handleAbandonedOrderDailyTask)
	asynq.Handle(TaskTypeRetailerFollowup, handleRetailerFollowupTask)
	asynq.Handle(TaskTypeTicketNotify, handleTicketNotify)
	asynq.Handle(TaskTypeProvisionPrivateNode, handleProvisionPrivateNode)
	asynq.Handle(TaskTypeProvisionTimeoutSweep, handleProvisionTimeoutSweep)
	asynq.Handle(TaskTypePrivateNodeLifecycleSweep, handlePrivateNodeLifecycleSweep)
	asynq.Handle(TaskTypePrivateNodeTrafficWarn, handlePrivateNodeTrafficWarn)
	asynq.Handle(TaskTypeTrafficAbuseCheck, handleTrafficAbuseCheck)
	asynq.Handle(TaskTypeStatsRetentionCleanup, handleStatsRetentionCleanup)
	asynq.Handle(TaskTypeSubscriptionReconcile, handleSubscriptionReconcileTask)
	asynq.Handle(TaskTypeNodeCapacityRefresh, handleNodeCapacityRefresh)

	// 注册续费提醒 Cron 任务
	// 每天北京时间 10:30 执行（UTC 02:30）
	// Cron 格式: 分 时 日 月 周
	// Unique(25h) 防止多实例重复入队同一任务
	asynq.Cron("30 2 * * *", TaskTypeRenewalReminder, nil, hibikenAsynq.Unique(25*time.Hour))

	// 试用未转化召回：每小时整点；每人按自己的试用结束钟点到点发送，北京 23:00~07:59 的运行空转（worker_trial_recall.go）。
	// 注意 cron 按服务器本地时区解释（qtoolkit 用 time.Local，center 为 +08），整点任务不受影响。
	asynq.Cron("0 * * * *", TaskTypeTrialRecall, nil, hibikenAsynq.Unique(55*time.Minute))

	// 注册未支付订单召回 Cron 任务
	// 每小时运行：1h 即时提醒
	asynq.Cron("0 * * * *", TaskTypeAbandonedOrderHourly, nil, hibikenAsynq.Unique(2*time.Hour))
	// 每天北京时间 11:00 执行（UTC 03:00）处理 1d/3d/7d/14d/30d
	asynq.Cron("0 3 * * *", TaskTypeAbandonedOrderDaily, nil, hibikenAsynq.Unique(25*time.Hour))

	// 注册分销商跟进提醒 Cron 任务
	// 每分钟检查一次，发送到期的 Slack 提醒
	// Unique(2min) 防止重复执行
	asynq.Cron("* * * * *", TaskTypeRetailerFollowup, nil, hibikenAsynq.Unique(2*time.Minute))

	// 注册专属节点开通超时清扫 Cron 任务
	// 每 10 分钟扫描卡在 provisioning 超时（节点始终未自注册）的订阅，置 failed
	// Unique(11min) 防止多实例重复入队
	asynq.Cron("*/10 * * * *", TaskTypeProvisionTimeoutSweep, nil, hibikenAsynq.Unique(11*time.Minute))

	// 注册专属节点生命周期推进 Cron 任务
	// 每天北京时间 03:00（UTC 19:00 次日）推进 active→grace→suspended→deprovisioned
	// 标签 + 续费回收。服务可用性以 IsServiceable 时间戳为权威，此 cron 只重贴标签。
	// Unique(25h) 防止多实例重复入队
	asynq.Cron("0 19 * * *", TaskTypePrivateNodeLifecycleSweep, nil, hibikenAsynq.Unique(25*time.Hour))

	// 注册专属线路流量预警 Cron 任务
	// 每 30 分钟扫描 active 专属线路,跨 80%/95% 阈值各发一封,按 TrafficEpoch 去重
	// Unique(31min) 防止多实例重复入队
	asynq.Cron("*/30 * * * *", TaskTypePrivateNodeTrafficWarn, nil, hibikenAsynq.Unique(31*time.Minute))

	// 注册流量滥用检查 Cron 任务
	// 每小时第 5 分扫描自然月(CST)累计超阈值(traffic.abuse_monthly_gb,默认 100GB)的用户,
	// Slack 告警按 (month,user) 去重 + 用户警告邮件,并顺带清理超过 60 天保留期的 DeviceTrafficDaily 明细
	// Unique(2h) 防止多实例重复入队
	asynq.Cron("5 * * * *", TaskTypeTrafficAbuseCheck, nil, hibikenAsynq.Unique(2*time.Hour))

	// 注册时序统计表保留期清理 Cron 任务
	// 每日 04:20 分批清扫 slave_node_loads(>30d) 与 stat_*/connection_ratings(>120d),
	// 这些表此前无任何清理、无限增长(slave_node_loads 曾达 160 万行/297MB)
	// Unique(25h) 防止多实例重复入队
	asynq.Cron("20 4 * * *", TaskTypeStatsRetentionCleanup, nil, hibikenAsynq.Unique(25*time.Hour))

	// 每日 04:50 用近 14 天指标样本重拟合各节点吞吐容量 M（node_usages.capacity_*）
	asynq.Cron("50 4 * * *", TaskTypeNodeCapacityRefresh, nil, hibikenAsynq.Unique(25*time.Hour))

	// 注册订阅对账 Cron 任务(spec 2026-08-22):每日 03:40 UTC,兜底漏 webhook。
	// 扫标活跃且周期临期/已过的订阅行,向 provider 核实真相并收敛 status/period/权益。
	// Unique(25h) 防止多实例重复入队
	asynq.Cron("40 3 * * *", TaskTypeSubscriptionReconcile, nil, hibikenAsynq.Unique(25*time.Hour))

	// 注册 ECH 相关的 worker
	RegisterECHWorker()

	// 注册云同步 worker
	RegisterCloudWorker()

	// 注册客服聊天 worker：每分钟 Slack 镜像兜底、每 10 分钟关闭空闲会话
	RegisterChatWorker()

	// 审批执行 handler
	asynq.Handle(TaskTypeApprovalExecute, ExecuteApproval)

	// 审批 callback 注册
	registerApprovalCallbacks()

	log.Infof(context.Background(), "[WORKER] Task handlers registered (renewal 10:30, abandoned hourly + 11:00 Beijing time)")
}

// registerApprovalCallbacks 注册全部审批回调（InitWorker 调用；抽出来便于测试断言注册存在）。
// SubmitApproval 对未注册的 action 直接报错，漏注册 = 该后台操作整个不可用。
func registerApprovalCallbacks() {
	RegisterApprovalCallback("edm_send", executeApprovalEDMSend)
	RegisterApprovalCallback("push_send", executeApprovalPushSend)
	RegisterApprovalCallback("campaign_create", executeApprovalCampaignCreate)
	RegisterApprovalCallback("campaign_update", executeApprovalCampaignUpdate)
	RegisterApprovalCallback("campaign_delete", executeApprovalCampaignDelete)
	RegisterApprovalCallback("license_key_batch_create", executeApprovalLicenseKeyBatchCreate)
	RegisterApprovalCallback("license_key_batch_invalidate", executeApprovalLicenseKeyBatchInvalidate)
	RegisterApprovalCallback("user_hard_delete", executeApprovalUserHardDelete)
	RegisterApprovalCallback("plan_update", executeApprovalPlanUpdate)
	RegisterApprovalCallback("plan_delete", executeApprovalPlanDelete)
	RegisterApprovalCallback("withdraw_approve", executeApprovalWithdrawApprove)
	RegisterApprovalCallback("withdraw_complete", executeApprovalWithdrawComplete)
	RegisterApprovalCallback("order_refund", executeApprovalOrderRefund)
	RegisterApprovalCallback("stripe_withdrawal", executeApprovalStripeWithdrawal)
}

// RunWorker 启动 Worker 服务（阻塞）
func RunWorker() error {
	InitWorker()
	log.Infof(context.Background(), "[WORKER] Starting worker service...")
	return asynq.Run()
}

// =====================================================================
// 任务入队 API
// =====================================================================

// edmTaskEnqueue EDM 发送任务的入队接缝（整批任务与节奏发送的单封任务共用）；测试替换它，
// 免得写进 config.yml 的 asynq.redis_addr（真实 dev Redis）。
var edmTaskEnqueue = asynq.Enqueue

// EnqueueTemplatedEmailTask 入队通用邮件发送任务
func EnqueueTemplatedEmailTask(ctx context.Context, req *SendEmailsRequest) (string, error) {
	payload := TemplatedEmailTaskPayload{Request: *req}

	info, err := edmTaskEnqueue(TaskTypeTemplatedEmailSend, payload)
	if err != nil {
		return "", fmt.Errorf("enqueue task failed: %w", err)
	}

	log.Infof(ctx, "[EMAIL-SEND] Task enqueued: batchId=%s, items=%d", req.BatchID, len(req.Items))
	return info.ID, nil
}

// handleTemplatedEmailTask 处理通用邮件发送任务
func handleTemplatedEmailTask(ctx context.Context, payload []byte) error {
	var p TemplatedEmailTaskPayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload failed: %w", err)
	}

	log.Infof(ctx, "[EMAIL-SEND] Processing async batch=%s, items=%d", p.Request.BatchID, len(p.Request.Items))

	result, err := SendTemplatedEmails(ctx, &p.Request)
	if err != nil {
		log.Errorf(ctx, "[EMAIL-SEND] Batch %s failed: %v", p.Request.BatchID, err)
		return err
	}

	log.Infof(ctx, "[EMAIL-SEND] Batch %s completed: sent=%d, failed=%d, skipped=%d",
		result.BatchID, result.Sent, result.Failed, result.Skipped)
	return nil
}

// =====================================================================
// 推送任务
// =====================================================================
