节点运维简化：定时重启移到 sidecar 本地，Batch 系统删除 cron/pause/resume，保留即时执行

Scrum 结论：定时重启是节点本地职责，不应由 Center 远程编排。Batch 系统保留 ad-hoc 价值，删除 YAGNI 调度层。

Action:
1. 定时重启移到 sidecar（Go goroutine，2:00-5:00 随机时间窗口）
2. 删除 SlaveBatchTask.ScheduleType="cron" 相关代码（CronExpr, IsEnabled, RegisterBatchCronTasks）
3. 删除 pause/resume 功能
4. 保留即时执行（once）+ 结果记录
5. 状态机简化为 pending→running→completed/failed
