# Background Workers (Asynq) — 完整清单

> 内容移自 `api/CLAUDE.md`（2026-10 精简）。找某个定时任务 / Asynq handler 在哪个文件、加新 worker 前读。接线规则留在 `api/CLAUDE.md` "Workers" 段。

| Worker | Purpose |
|--------|---------|
| `worker_integration.go` | `InitWorker()` — `asynq.Handle(TaskType…, handle…)` for every handler, `asynq.Cron` schedules, approval-callback registry |
| `worker_cloud.go` | Cloud instance sync, change-IP, create, delete |
| `worker_cloud_overage.go` | AWS Lightsail overage guard, tail step of `syncAccount` per instance (three layers, provider-reported usage is authoritative) |
| `worker_ech.go` | ECH key rotation |
| `worker_renewal_reminder.go` | Membership renewal reminders + winback campaigns (daily 02:30) |
| `worker_abandoned_order.go` | Unpaid-order recall emails (hourly + daily) |
| `worker_retailer_followup.go` | Retailer follow-up notifications (every minute) |
| `worker_ticket_notify.go` | Aggregates un-notified admin ticket replies → email |
| `worker_private_node.go` | Private-node provisioning task + provisioning-timeout sweep (30 min tolerance, every 10 min) |
| `worker_private_node_lifecycle.go` | Daily private-node subscription lifecycle labels, grace→suspended cutover, renewal reclaim (`IsServiceable` timestamps stay authoritative) |
| `worker_private_node_traffic_warning.go` | Tiered private-line traffic warnings, per-tier dedupe (every 30 min) |
| `worker_subscription_reconcile.go` | Daily Apple/Stripe subscription reconcile — the fallback when webhooks are lost (spec 2026-08-22) |
| `worker_traffic_abuse.go` | 每小时聚合当月 per-user 流量，超阈值（`traffic.abuse_monthly_gb`，缺省 100GB）Slack 告警 + 用户警告邮件（模板 `traffic-abuse-warning`，月度去重）+ 60 天保留清理（隐私政策承诺 2 个月） |
| `worker_chat.go` | 客服聊天：`RegisterChatWorker()`（由 `InitWorker()` 调用）注册 `chatCronSpecs` —— 每分钟 `chat:slack-sweep`（补镜像实时路径漏发的消息 / 状态卡 / 归档，超 10 分钟未镜像告警），每 10 分钟 `chat:close-idle`（AI 处理中 24h、人工处理中 72h 无消息自动关闭并归档 Slack 频道） |
| `worker_stats_retention.go` | 每日分批清扫无界时序表：`slave_node_loads` >30d（读取端只取每节点最新一条）、`stat_*`/`connection_ratings` >120d（admin 报表最大回看 90d）；stat 表按 `reported_at`（服务端权威，`created_at` 是客户端时钟含脏数据） |
| `handler_edm.go` | `createEmailSendLog` / `updateEmailSendLogStatus` helpers — not an Asynq handler despite the prefix |

Asynqmon UI available at `/app/asynqmon` (admin auth required).
