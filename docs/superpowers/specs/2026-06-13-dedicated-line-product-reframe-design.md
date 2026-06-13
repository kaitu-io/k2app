# 定制线路产品化 — Phase 1 家庭线路池 设计文档

> 2026-06-13。承接 Plan 5b（`2026-06-12-private-node-router-onboarding-design.md`）。把"路由器版"重定位为"**定制线路版**"，准入与 App tier 解耦，多条独立线路组成池由路由器自动选优，配额耗尽硬断到下月、需更多则再购实例。
>
> 本文档只覆盖 **Phase 1 家庭场景**。企业多 SSID（Plan 7）是独立子系统，单开 spec，明确排除在外。

---

## Goal

让任何**购买了 ≥1 条 active 独立线路（private node 订阅）**的用户，无论 App tier 是什么，都能铸造网关凭证并把自有 OpenWrt 路由器接入；多条线路组成一个池，路由器自动选最优、某条耗尽自动切换；线路配额由其 VPS 底座捆绑额度决定，耗尽即硬断到下个计费周期，需要更多容量时引导购买新实例。

## 背景与问题

**割裂（核心动机）**：现状 `checkDeviceLimitOrKick(isGateway=true)`（`logic_auth.go:492-539`）用 tier 派生的 `quota.MaxRouterDevice`（`tier.go:28-33`：lite/basic=0，family=1，business=3）做闸门。但产品意图是"**买了独立 VPS 线路就能用路由器版**"——准入应来自**线路所有权**，与 App tier 无关。当前 `api_gateway_credential.go:54-65` 其实已经另外查了 active 私有线，导致 **两个 entitlement 源打架**：一个 basic 档但持有线路的用户通过了线路检查，却被 `MaxRouterDevice==0` 拒（`ErrorPlanNoRouter` 402001）。这就是要拆的割裂。

**流量经济（已被底座结构解掉大半）**：所有节点底座（搬瓦工固定流量 / AWS Lightsail 捆绑额度，2TB 等）均为**捆绑额度制**，成本天然封顶在节点月租。一个 Netflix 重度用户最多烧光自己这条线的额度，**开途零额外成本**。因此"几乎不用担心流量"对正常用户诚实；只对极端重度用户需要一个**硬断兜底**动作。

## 锁定的设计决策

1. **路由器准入 = "持有 ≥1 条 active serviceable 独立线路"**，与 App tier 完全无关。
2. **一账号一路由器**：每账号路由器设备数上限固定为 **1**（想要第二台路由器 → 用第二个账号，不做"路由器位=线数"那套复杂逻辑）。这与 Plan 5b mint 的事务内 rotation（删旧 router 设备再建新）天然吻合——重复 mint 即替换，cap=1 自动维持。替换网关路径对 tier 派生 `MaxRouterDevice` 的依赖；App 设备配额 `MaxDevice` 不变。
3. **每条线配额 = 其底座捆绑额度**（已存于 `PrivateNodeSubscription.TrafficTotalBytes`）。耗尽阈值统一用现有 **95%**（与服务端 `slave_api_usage.go` verdict=stop 对齐），断点落在 Lightsail 超额计费之前。
4. **耗尽 → 硬断到下个计费周期**（服务端 `usage_reporter` + Center verdict 已实现）。`TrafficEpoch`/`TrafficResetAt` 重置后线路自动恢复入池。
5. **多线池（家庭）**：`/api/subs` 网关分支只返用户**未耗尽**的 active serviceable 线；路由器复用现有 `Subscription.Pick`（recommendScore + 被动 failover）选最优；耗尽的线从返回中剔除 → 路由器下次 refresh 自动切到次优，**无需重新粘贴凭证**。
6. **再购实例**：满了引导购买新线路（走现有 provisioning）；新 `PrivateNodeSubscription` 自动进 `/api/subs` → 路由器自动接管。**不做按-GB 加购**。

## 已存在、不要重建（探索坐实）

| 能力 | 位置 | 状态 |
|---|---|---|
| 网关跑完整持久订阅（Resolve+StartRefresh+Pick） | `gateway/gateway.go:255-269,356-364` | ✅ 与 daemon 一致 |
| 多隧道加权选优（recommendScore/Pick） | `config/subscription.go:295-411` | ✅ |
| 出站失败被动 failover | `engine.outboundReplaceLoop` → `subsMgr.NextURL` | ✅ |
| 服务端配额硬断（Option D） | `server/usage_reporter.go:135-183`，Center 下发 `epoch_hard_ceiling_bytes` | ✅ |
| `/api/subs` 网关分支严格只返 owner 自有 serviceable 线 | `api_subs.go:154-170` + `entitlement_resolver.go:14-61` | ✅ 零共享池泄漏 |
| 线路所有权模型 | `model_private_node.go:35-68`（UserID/OrderID/CloudInstanceID/SlaveNodeID/Status/TrafficTotalBytes/ExpiresAt） | ✅ |
| `IsServiceable(now)` = status∈(active,grace) ∧ now<ExpiresAt+7d | `model_private_node.go:70-80` | ✅ |
| 每线流量配额 + 用量 + 95% 耗尽判定 | `PrivateNodeSubscription.TrafficTotalBytes`，`CloudInstance.TrafficUsedBytes/Epoch`，`QuotaExhausted` | ✅ |
| 网关 `X-K2-Client: kaitu-router/...` 头 | `gateway/gateway.go:356-362` | ✅ Plan 5b |
| 80/95% 预警 worker | `worker_private_node_traffic_warning.go` | ✅ Plan 5b（仅文案待改） |

## 必做改动（Phase 1 真实 delta）

### A. 准入解耦（拆割裂）— Center
- 新增 helper：`HasActivePrivateLines(ctx, tx, userID, now) bool`（查 `PrivateNodeSubscription WHERE user_id=? AND status IN(active,grace)` 是否存在一条 `IsServiceable(now)`）。
- 改 `checkDeviceLimitOrKick(isGateway=true)`：网关路径**不再读** `quota.MaxRouterDevice`，改用线路所有权 + 固定 cap=1：
  - `!HasActivePrivateLines(...)` → `ErrorPlanNoRouter`（语义改为"无 active 线路"，与 App tier 无关）。
  - `gatewayDeviceCount >= 1` → `ErrorRouterDeviceLimit`（每账号至多 1 台路由器；但 mint 的 rotation 会先删旧设备，正常重复 mint 不触发，仅防御并发）。
- 统一 entitlement 源：`api_gateway_credential.go:54-65` 的独立检查与 `checkDeviceLimitOrKick` 收敛为**同一个 helper**，消除双源打架。
- `MaxRouterDevice` 字段保留（`User.MarshalJSON` 仍输出，前端兼容），但网关准入不再依赖它。

### B. 耗尽线剔出 /api/subs（耗尽自动 failover）— Center
- 在 `ResolveGatewayPrivateTunnels`（`entitlement_resolver.go`）对每条 sub 取其 `CloudInstance`，若 `TrafficTotalBytes>0 && TrafficUsedBytes*100 >= TrafficTotalBytes*95` 则**跳过**该线。
- 与服务端 95% 断点对齐：线路被服务端停止 accepting 的同时从订阅消失，路由器 Pick 不再选它。
- 全部线耗尽 → items 为空 → 现有 402 分支（`api_subs.go:164-165`）→ 路由器知道无可用线。

### C. 硬断兜底文案 — Center
- `worker_private_node_traffic_warning.go` 的 80/95% 邮件话术改为"**用完将暂停至下个计费周期，需要更多容量请购买新线路**"（开途，全角标点，禁裸 Kaitu）。
- 95% 邮件附"再购买线路"入口/引导。

### D. 面板与购买引导 — webapp
- 专属线路面板（`AddRouterCard` / 专属节点管理页）：每条线展示 **配额 + 已用 + 重置日期 + 耗尽态**（数据已在 `api_user_private_node` 的 `QuotaExhausted`/traffic 字段）。
- 耗尽态 UI + "**再购买线路**" CTA（复用现有购买 provisioning 入口）。

### E.（可选增强，可延后）网关 probe 主动 failover
- `gateway/gateway.go:114` 的 `scoreSrc` 当前硬编码 `nil`。接一个 `probe.Registry` 可让网关从"被动 failover（出站失败后切）"升级到"主动健康探测切线"。**Phase 1 不阻塞**，列为后续增强。

## 数据模型变更

**无需新增字段。** 配额/用量/所有权/serviceable 判定全部复用现有字段。仅新增一个查询 helper（`countActivePrivateLines`）。这点与探索结论一致：CloudInstance/PrivateNodeSubscription 已有全部所需列。

> 注：探索另提的"per-line device-count 绑定（Device→sub_id）"是**企业 SSID 绑定**才需要的（设备死绑某条线）。**家庭池模式下设备不绑线**，故明确排除出 Phase 1。

## 明确排除（Phase 2 / Plan 7）

- 企业多 SSID：SSID 感知路由、线路↔SSID 1:1 绑定（`ssid_uk_0`/`ssid_uk_1`）、OpenWrt VLAN/接口映射、后台绑定面板。
- 企业超额策略（放行+计费，与家庭硬断相反）。
- Device→PrivateNodeSubscription 绑定。
- 网关 probe.Registry 接线（列为本 Phase 可选增强，默认延后）。

## 测试策略

- **Center 单测/集成**（真 dev MySQL）：
  - `HasActivePrivateLines`：0 线 → false；1 active 线 → true；仅 expired/suspended 线 → false；grace 内 → true（边界）。
  - `checkDeviceLimitOrKick(isGateway)`：basic 档 + 1 线 → 通过；basic 档 + 0 线 → `ErrorPlanNoRouter`；已挂 1 路由器再挂第 2 台（绕过 rotation 的并发路径）→ `ErrorRouterDeviceLimit`；family 档但 0 线 → 拒（验证彻底脱钩 tier）。
  - `ResolveGatewayPrivateTunnels`：1 耗尽 + 1 健康 → 只返健康；全耗尽 → 空（→402）；全健康 → 全返。
- **webapp vitest**：面板耗尽态渲染 + "再购买线路" CTA + 配额展示。
- **真机 smoke（deferred，与 Plan 5b/#8/#12 三连批量）**：2 条线 → 跑满第 1 条 → 路由器自动切第 2 条；basic 档持线用户铸造凭证成功。

## 部署注记

- Center 改动纯逻辑 + 文案，**无迁移**（不加列）。
- 与 Plan 5b 未推的 commit 同车发布。
- 灰度：先确认线上有 ≥1 个 basic/lite 档 + 持 active 线的 canary 账号验证准入解耦（避免 family-only 假绿）。

## 风险与信心

- 代码层信心：**8.5/10**（机器已存在，delta 小且每处有 file:line 锚点）。
- 业务问题信心（"basic 档持线用户能用上路由器 + 多线自动切"）：**桌面验证可达 8/10；无真机 smoke 封顶 6-7/10**（按发布信心框架，功能改动）。
- 最大不确定：耗尽线剔出订阅后，路由器 refresh 周期内的切换延迟体验（被动 failover 依赖出站失败触发）——真机才能定。
