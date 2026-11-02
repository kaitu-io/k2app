# 节点实时分布与验收视图

日期：2026-11-02　状态：David 已要求直接实施

## 意图

客户端自动分配节点出过大问题。需要直接看到每个节点的设备数和负载，用来验收客户端策略。David 的验收标准：**不要出现有节点没有连接、同时有节点爆了的分布**，不追求每台数量相同。

## 目标 / 非目标 / 反目标

- 目标：节点部署和节点管理合并成一页；节点列表显示实时设备数和负载；MCP 返回同样的数据。
- 非目标：用户维度的页面、作弊判定、踢设备（见 `2026-11-02-device-control-foundation-design.md`）；改动 k2s；改动选择策略。
- 反目标：观测写入影响计费账本；把离线、流量用尽或已下架节点算成分配失败；把未知负载当成 0；存客户端 IP 或访问目标。

## 方案

| 方案 | 取舍 | 结论 |
|---|---|---|
| A. 从 k2s 每 60 秒的设备流量批次推算活跃设备 | 不用升级节点，发 Center 即生效；不含完全没流量的挂机设备 | **选定** |
| B. k2s 暴露真实在线数，sidecar 上报 | 数字最准；要全车队升级 k2s 和 sidecar，验收要等 | 不选，时机记录在设备控制档案里 |
| C. 先 A 后 B，预留字段 | 多一个空字段 | 不选，字段等真用到再加 |

选 A 的理由：验收看的是“有人用却没分到”和“人太多”，都是有流量的设备。

`Connections` 字段全车队为 0，本次不用，也不修。

## 设计

**活跃设备定义**：近 5 分钟内在该节点有流量的不同 `udid`。

**存储**：`node_device_presences`，5 分钟一个时间桶，键 `(bucket, node_ipv4, udid, session_id)`，带收发字节、`user_id`、`last_seen_at`。保留 7 天，与隐私政策 §4.2 一致，由 `statsRetentionTargets` 清理。`last_seen_at` 用 Center 时钟，所以 5 分钟窗口是精确滚动的，不是 5–10 分钟。

**写入**：`ingestDeviceTraffic` 的账本事务提交之后尽力写入。失败只丢一个样本，不回滚账本，也不触发 k2s 重发。重发批次被游标拦下，不会重复计入。空 `udid` 不是设备，跳过。

**状态**（`classifyNodeStatus`，按序判断）：

1. `private`：专属节点
2. `offline`：沿用 `isNodeOffline`
3. `hidden`：对两个品牌都不可见
4. `exhausted`：沿用 `isNodeOverQuota`
5. `overloaded`：10 分钟内的压力 ≥ 0.9
6. `idle`：活跃设备为 0
7. `normal`

只有 5–7 参与均衡计算。压力 1.0 表示 CPU 到 80% 上限或吞吐到容量 M。

**判定**（全局和每个国家各一个）：

- `imbalanced`：同时存在空闲和过载节点，这是验收失败。
- `skewed`：有空闲节点，同时别的节点有设备。sidecar 升级前没有压力数据，这是能给出的最强信号。
- `ok`：其余情况，包括夜间全部空闲。

**接口**（`viewOrEdit`）：

- `GET /app/nodes/distribution?brand=`：全部节点，不分页。
- `GET /app/nodes/distribution/trend?hours=&brand=&country=`：已完成的时间桶，1–168 小时，默认 24；只算共享节点；节点范围取当前快照。

**页面**：`/manager/nodes` 是合并页，`/manager/cloud` 重定向到它，创建实例页保留。一行一台机器，按 IPv4 关联，保留没有云实例的节点（如 `bwh`）和尚未注册的实例；品牌视图下不显示未注册实例。实时数据 30 秒刷新，云实例不自动刷新。

**MCP**：`kaitu-center` 新增 `get_node_distribution`、`get_node_distribution_trend`，权限组 `nodes`。

## 已知局限

- k2s 没流量时不发批次，所以 `idle` 无法区分“真没人”和“设备上报坏了”。
- CPU、steal、带宽、压力要等 sidecar 升级到 metrics v1。未升级的节点显示“sidecar 待升级”，不会被判过载，判定最高到 `skewed`。
- 设备计数按节点去重，同一设备在两个节点算两次，趋势里的 `activeDevices` 衡量分配，不是在线用户数。
- 趋势 UI 本次未做，只有 MCP；页面显示每节点 24 小时峰值。

## 部署

需要 `RUN_MIGRATE=1 make deploy-api` 建表，然后发布 `website` 和 `kaitu-center` MCP。Center 发版后约 5 分钟才有实时数，24 小时峰值和趋势随数据积累增长。
