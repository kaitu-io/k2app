# 个人流量统计与高占用账号识别 — 设计规格

日期：2026-07-22（Rev 1 同日：上报通道由「搭 `/slave/usage` 便车」改为独立端点，理由见 §3.5）
状态：已获批（观测 + 告警；自动处置明确为 Phase 2，不在本 spec 范围）
涉及层：k2 submodule（`wire/` + `server/`）、Center API（`api/`）、Admin Dashboard（`web/src/app/(manager)/manager/`）

## 1. 背景与目标

共享池节点已有**节点级**流量计量（k2s 进程内 `ProxyHandler` rx/tx 原子计数 → `usage_reporter` 60s 级上报 `/slave/usage` → Center `NodeUsage` 纯记录），但没有**用户维度**——无法回答"哪个账号长期占用最多资源"。

**目标**：按账号统计真实转发字节数，管理端出流量排行与账号明细，月度累计超阈值时 Slack 告警。处置由人工决定。

**非目标**（明确排除）：
- 自动限速 / 自动拒绝连接（Phase 2，前置条件见 §8 风险）
- 按用户计费 / 对用户展示自身流量（数据模型不排斥未来支持，但本期不做任何用户面）
- 记录目标地址 / 域名 / SNI（隐私边界，见 §7）
- 改动节点级 epoch/cumulative 掐断链路（一行都不动）

## 2. 架构总览

```
k2s (每节点)
  ProxyHandler pipe 计量点 (与节点级 rxBytes/txBytes 同源)
    └─ per-device delta map  ──60s──►  deviceTrafficReporter (新, 独立 loop)
                                          │ POST /slave/device-traffic
                                          │ {boot_id, batch_seq, devices:[{udid,rx,tx}]}
                                          ▼
Center /slave/device-traffic (新端点; /slave/usage 节点级链路一行不动)
    ├─ 幂等检查 (DeviceTrafficCursor: boot_id, batch_seq)
    ├─ udid → user_id 解析 (Device 表, 每报一次 IN 批查)
    └─ DeviceTrafficDaily upsert 累加 (date/udid/node_ipv4 唯一)
          │
          ├─ GET /app/traffic/top-users      ┐
          ├─ GET /app/traffic/user/:id       ├─ admin 端点
          └─ worker_traffic_abuse (每小时)    ┘ 月度累计超阈值 → Slack
                    ▼
web /manager/usages「流量排行」tab + /manager/users 详情「流量」区块
```

## 3. k2s 侧设计（k2 submodule）

### 3.1 身份传递

- **QUIC**：`handleMetadataStream` 现已验证 `meta.UDID`（validator 链 → Center `/slave/device-check-auth`），但只把 `mode` 存进 `connContext`。加一个连接级共享的 `deviceIDHolder`（atomic），验证通过后写入 UDID。
- **TCP-WS 服务端**：metadata stream 的 UDID 是自报未验证的（auth 在 HTTP 层 `/k2v5/auth`，`/k2v5/tunnel` upgrade 不校验 ticket）。仍取 metadata UDID 存入 per-session holder 用于归因，**可信度差异如实记录**（见 §8）。
- **TCP-WS 客户端**：现在根本不发 metadata（服务端只是"protocol symmetry"接受）。本次补上：smux session 建立后发一条 metadata stream（镜像 QUIC 客户端 `sendMetadata`），失败不致命（TCP-WS 归因是 best-effort）。客户端与服务端同 repo 同 release，无部署错配；老客户端流量落 `""` 桶，随升级收敛。
- **传递到计量点**：stream conn（`quicStreamConn` / `smuxStreamConn`）持有 holder 引用并实现 `DeviceID() string`；UDP 会话 conn 同理（holder 挂在 `connUDPState` / smux session 上下文）。`ProxyHandler` 用**可选类型断言** `interface{ DeviceID() string }` 取身份——拿不到（老客户端 / raw smux）归入 `""` 空桶，节点级计数照常。**不改 `wire.Listener` 接口签名**，向后完全兼容。

### 3.2 计量

`ProxyHandler` 在现有 `rxBytes/txBytes` 旁增加 per-device 计数（mutex map，`Add(udid, rx, tx)`）。方向约定与节点级一致：rx = 客户端上行，tx = 目标下行。计数语义 = **待上报增量**，随批次取走清零，无常驻增长。

**入账时机与节点级计数完全同构**：TCP 在 `pipe` 的 `io.CopyBuffer` 返回处（= 连接方向结束时）一次性入账；UDP 会话在本地累加、会话结束时入账。长连接的字节在连接关闭当天入账——对自然月观测无碍（连接总会关），不为此引入增量包裹 writer。

### 3.3 批次上报与幂等（关键设计）

**上报的是增量（delta），不是累计。** 选 delta 的原因：cumulative 要求 k2s 持久化 per-device 状态（重启不丢、epoch 内 map 只增不减），Center 还要做差分；delta 模型 k2s 零持久状态，代价只是崩溃时丢 ≤1 个上报周期（60s）的增量——观测场景可接受。

**不可变批次 + (boot_id, batch_seq) 幂等**，防"Center 已入库但 ack 丢失 → 重发 → 双计"：

独立的 `deviceTrafficReporter`（新文件 `server/device_traffic_reporter.go`，固定 60s 周期，Basic auth 复用 `ipv4:NodeSecret`）：

```
reporter 状态:  bootID   进程启动时生成的随机 hex（每次重启必换）
               batchSeq 进程内单调递增
               pending  *deviceBatch (至多一个未确认批次, 内容不可变)

每周期 runOnce:
  if pending != nil:
      原样重发 pending (同 boot_id, 同 batch_seq, 同 devices)   # 不 merge 新增量
  else:
      snapshot = TakeDeviceTraffic()          # 原子取走 map 并清空
      if snapshot 为空: 本周期不发
      else: pending = {bootID, batchSeq++, snapshot}
  POST /slave/device-traffic {boot_id, batch_seq, ts, devices:[{udid,rx,tx}]}
  ack 成功 → pending = nil
  失败    → pending 保留；新流量继续累积在 live map，成为下一批
```

- 批次**不可变**是安全性的根：若允许把新增量 merge 进未确认批次并换号，旧批次已入库时重发合并批会被当新批接受 → 旧字节双计。
- Center 判据：`DeviceTrafficCursor`（按 ipv4 唯一）记 `boot_id` + `batch_seq`；收到同 boot_id 且 seq ≤ 已记录值 → 跳过入库直接 ack；否则入库并更新游标。重启换 boot_id 自然被接受，不存在"seq 归零后被误杀"。
- 单一 pending（而非 FIFO 队列）：积压时每周期消化一批，恢复速度足够，实现最小。
- **两边 JSON tag 必须逐字对齐**（既有契约约定，加契约测试钉住）。

### 3.4 为什么是新端点，不搭 `/slave/usage` 便车（Rev 1 核心变更）

原设计想让 devices 附在节点级上报里。落 plan 前重读代码发现 **k2 master 与 api master 在这个 seam 上已有潜伏矛盾**：

- k2 的进程内 `usageReporter`（Option D, commit 3335d91）期待 Center 返回 `verdict`，据此 `SetAccepting`；
- 而 api master 的 `/slave/usage` 已是**纯记录器**（node-authority 架构），只返回 `next_report_interval`——k2s 解出空 verdict 会 `SetAccepting(false)`，**节点停止接受新连接**；
- 且该 reporter 的节点级上报（epoch=0 / quota=0）会覆写 `NodeUsage.quota_total_bytes=0` 并触发 "Node Uncapped" 告警风暴。

当前车队 pin 在 Option D 之前的版本、reporter 未启用，故 bug 潜伏。但 per-device 上报需要给 k2s 配 `K2_USAGE_REPORT_URL`+`K2_NODE_SECRET`，会**顺带激活**这个坏 seam。因此：

1. per-device 上报走**独立 reporter + 独立端点**，与节点级链路零交集；
2. legacy `usageReporter` 的启动条件追加显式开关 `cfg.NodeCutoffReport`（env `K2_NODE_CUTOFF_REPORT`，默认 false）——在有 Center 侧 verdict 服务之前它对着当前 Center 就是坏的，不该被凭证的存在隐式激活（见 §3.5）。

### 3.5 legacy usageReporter 重新加门

`server.go` 启动逻辑改为：凭证齐备（`UsageReportURL`+`NodeSecret`）→ 启动 `deviceTrafficReporter`；**额外**满足 `NodeCutoffReport=true` 才启动 legacy `usageReporter`。`config.ServerConfig` 增加 `NodeCutoffReport bool`（yaml `node_cutoff_report` / env `K2_NODE_CUTOFF_REPORT`）。不改 legacy reporter 本体——它的 verdict seam 修复属于 node-authority 架构的后续，不在本 spec 范围，但在此如实记录。

## 4. Center 侧设计（`api/`）

### 4.1 数据模型（`model_device_traffic.go`）

```go
// DeviceTrafficDaily 按日累加的设备流量明细（事实源）
type DeviceTrafficDaily struct {
    ID        uint64
    Date      string // "2026-07-22"，按 Asia/Shanghai 截断（见 §7 口径）
    UDID      string `gorm:"column:udid;type:varchar(64)"`
    NodeIpv4  string `gorm:"type:varchar(15)"`
    UserID    uint   // 入库时解析；解析失败=0，仍记账
    RxBytes   int64
    TxBytes   int64
    // 唯一索引 (date, udid, node_ipv4)；辅助索引 (user_id, date)
}
```

幂等游标独立成表 `DeviceTrafficCursor { Ipv4 唯一, BootID, BatchSeq }`——不碰 `NodeUsage`（该表属于节点级计量模型，语义隔离；且 device 上报可能先于 sidecar 建行到达）。

量级：~20 节点 × 每节点日活数百设备 ≈ 万行/天。`worker_traffic_abuse` 顺带清理 >60 天的行（隐私政策承诺"流量用量统计保留 2 个月"）。

**月度视图是纯查询口径**：本月用量 = `SUM WHERE date BETWEEN 月初 AND 月末`（Asia/Shanghai）。月份翻转零动作、无清零故障面。如排行查询压力可见，再加 `user_traffic_monthly` 派生汇总表（worker 增量维护）——首版先不建，日表索引足够。

### 4.2 入库（新文件 `slave_api_device_traffic.go`，路由 `POST /slave/device-traffic` + `SlaveAuthRequired()`）

1. 幂等检查：读 `DeviceTrafficCursor` by ipv4；同 boot_id 且 `batch_seq ≤` 游标 → 跳过入库，仍返回成功 ack。
2. udid → user_id：一次 `WHERE udid IN (...)` 批查 `Device` 表（每节点每 60s 一次，量级无需缓存）；查不到 user_id=0 仍记账。
3. `clause.OnConflict` 批量累加 upsert（MySQL `rx_bytes = rx_bytes + VALUES(rx_bytes)`）。
4. upsert 游标（boot_id + batch_seq）。

### 4.3 查询端点（`api_admin_traffic.go`，`/app` admin 组）

- `GET /app/traffic/top-users?month=2026-07&limit=50` ——按 user SUM 排序：email、总量、rx/tx、活跃设备数、涉及节点数、占全网比例。`user_id=0` 桶单独一行标注「未识别设备」。
- `GET /app/traffic/user?uuid=<user-uuid>&month=2026-07` —— 每日曲线 + 按设备明细 + 按节点明细（admin 用户详情页以 uuid 为键，端点随之）。

### 4.4 告警 worker（`worker_traffic_abuse.go`，Asynq cron 每小时）

- 聚**当前自然月** per-user 累计，超过 `config.yml` 阈值（`traffic.abuse_monthly_gb`，viper 读取，缺省 100）→ Slack（复用 `sendCloudSlackNotification`）+ 用户警告邮件（EDM 模板 `traffic-abuse-warning`，公平使用提示，月度去重）。
- 去重：小表 `TrafficAbuseAlert { Month, UserID 联合唯一 }`，insert 成功才发通知——同账号同月只告警一次，DB 判据可测试、无 Redis 依赖。
- 顺带执行 §4.1 的 60 天保留清理。

## 5. Admin UI（`web/src/app/(manager)/manager/`）

管理端真实仪表盘在 `(manager)` 组（`(payload)/manager/` 下只有 Payload CMS，与本功能无关）。

- `/manager/usages` 新增**「流量排行」tab**：月份/区间选择器 + top-N 表格（email、总量、rx/tx、设备数、节点数、占比），行点击跳用户详情。
- `/manager/users` 详情页新增**「流量」区块**：每日柱状图（复用页面既有 BarChart 形态）+ 设备/节点明细表。
- `web/src/lib/api.ts` 增加两个 typed 方法，类型与 Center 端点对齐。

## 6. 测试策略

- **k2s**：per-device 计量并发测试（`-race`）；批次状态机单测（ack 丢失重发不双计、失败累积、重启换 boot_id）；`NodeUsageRequest` 序列化契约测试与 Center 端逐字段对齐。
- **api**：幂等跳过 / 累加 upsert / udid 解析缓存 / top-users 聚合 / 告警去重（mock DB + 集成，集成测试注意 `skipIfNoConfig` 0-SKIP 判据与新列手动 migrate）。
- **web**：api.ts 方法类型对齐 + 页面 vitest 渲染测试。
- **真机 smoke**（release confidence 门槛）：dev Center + 1 台 canary 节点跑新 k2s，真流量验证 devices 上报入库、断网重发不双计、admin 页面出数。

## 7. 口径与边界

- **自然月**：所有"月"均指 Asia/Shanghai (UTC+8) 自然月；`Date` 列按该时区截断，入库处代码注释写明。k2s / Center 均无月状态、无清零动作。
- **隐私**：只计字节数。不记录目标地址 / 域名 / SNI / 连接内容——抓资源占用不需要，也避免留存用户浏览记录。
- **覆盖范围**：共享池与专属节点都计（专属节点归属本就明确，顺带获得明细）；k2r gateway 模式的 udid 同样归因。空 udid 归入 `""` 桶，admin 显示为「未识别」。
- **与节点级计量的关系**：同一观测点、两套账本。节点级（cumulative + epoch + 云厂商账单锚）驱动成本掐断；用户级（delta + 自然月）驱动运营观测。互不依赖、互不影响。

## 8. 风险与已知限制

| 风险 | 影响 | 处理 |
|---|---|---|
| TCP-WS 会话 UDID 自报未验证 | 恶意合法用户可伪造他人 udid 转嫁归因 | 观测+人工场景可接受（处置前人工核实）。**Phase 2 自动处置的硬前置：TCP-WS upgrade 带 ticket 校验，metadata UDID 与 ticket.DeviceID 交叉验证** |
| k2s 崩溃丢最后一个周期增量 | ≤60s 流量缺账 | 观测场景可接受，不补 |
| 空 udid 桶（老客户端 / raw smux） | 部分流量无法归因 | admin 单列展示；随客户端升级自然收敛 |
| `Device` 表 udid 缺失（设备未注册） | user_id=0 | 仍记账，admin「未识别」桶可见，不静默丢弃 |
| 上报体积增大 | 每节点每 60s 多 ~几百条 × ~40B | 量级无害；无流量周期不发 |
| legacy Option D reporter 对纯记录器 Center 是坏 seam（空 verdict → 停节点 + 踩 quota） | 若被隐式激活会停掉共享节点 | 本次重新加门（§3.5, `K2_NODE_CUTOFF_REPORT` 默认关）；seam 本体修复留给 node-authority 后续工作 |

## 9. 部署顺序（硬性）

1. **deploy-api**：AutoMigrate 建 `device_traffic_dailies` + `device_traffic_cursors` + `traffic_abuse_alerts`；新端点上线。
2. **k2s 新版**：走既有 fleet pin 流程（canary 1 台 → 批量，`.env K2_VERSION` 显式升级）。同批给 k2s 容器补 env：`K2_USAGE_REPORT_URL` + `K2_NODE_SECRET`（compose 里 sidecar 已有同名值，透传即可）。**不设 `K2_NODE_CUTOFF_REPORT`**（保持 legacy reporter 关闭）。
3. **web**：admin UI 上线。

每步独立可回滚；k2s 回滚只是 device 上报消失，Center 与 UI 优雅降级为无新数据。

## 10. 里程碑外（Phase 2 预留）

- 自动处置（per-device verdict 下发）：前置 = TCP-WS 强归因（§8）+ 白名单/申诉机制。
- `user_traffic_monthly` 派生汇总表：排行查询变慢时再加。
- 用户面流量展示：`DeviceTrafficDaily` 已具备数据基础。
