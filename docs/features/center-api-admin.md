# Feature: Center API Admin & Infrastructure

## Meta

| Field | Value |
|-------|-------|
| Feature | center-api-admin |
| Version | v1 |
| Status | implemented |
| Created | 2026-02-18 |
| Updated | 2026-02-18 |

## Overview

Center API (`api/`) 的管理后台和基础设施层，覆盖 Admin 面板路由 (`/app/*`)、Slave 节点通信 (`/slave/*`)、CSR 证书签发 (`/csr/*`)、多云 VPS 管理 (`cloudprovider/`)、Asynq 后台任务队列。核心设计原则：

1. **Flat package 架构** — 所有 handler、logic、model 位于 `center` 包根目录，通过文件命名约定区分职责（`api_admin_*.go`, `slave_api*.go`, `worker_*.go`）
2. **统一 Provider 接口** — 7 个云厂商通过 `cloudprovider.Provider` 接口统一管理，支持实例同步、换IP、创建、删除
3. **Asynq 任务驱动** — 所有重操作（云同步、ECH轮换、批量脚本、诊断）通过 Asynq 异步执行，cron 调度 + 手动触发双轨
4. **三层认证隔离** — Admin (`AdminRequired`)、Slave (`SlaveAuthRequired`)、CSR (公开) 三个路由组各自独立的认证策略

## Product Requirements

### Admin 面板路由组 (`/app/*`)

所有 `/app/*` 路由需要 `AdminRequired()` 中间件 — JWT 认证 + `IsAdmin=true` 检查。CORS 仅允许 `kaitu.io` 和 `localhost:3000`。

| 模块 | 路由 | 说明 |
|------|------|------|
| 隧道管理 | `GET/PUT/DELETE /app/tunnels` | 列表、更新、删除 VPN 隧道 |
| 物理节点管理 | `GET /app/nodes`, `GET /app/nodes/batch-matrix`, `PUT/DELETE /app/nodes/:ipv4` | 节点列表（含批量脚本关联矩阵）、更新、删除 |
| 套餐管理 | `CRUD /app/plans`, `POST /app/plans/:id/restore` | 订阅套餐 CRUD + 软删除恢复 |
| 用户管理 | `GET /app/users`, `GET /app/users/:uuid` | 用户列表（分页搜索）、用户详情 |
| 用户分销商状态 | `PUT /app/users/:uuid/retailer-status`, `PUT /app/users/:uuid/retailer-contacts` | 启用/停用分销商角色、更新联系方式 |
| 用户硬删除 | `POST /app/users/hard-delete` | 批量硬删除用户（不可恢复） |
| 用户成员管理 | `GET/POST/DELETE /app/users/:uuid/members` | 付费委托成员的增删查 |
| 分销商配置 | `PUT /app/users/:uuid/retailer-config` | 更新分销商等级、分成比例 |
| 用户会员时长 | `POST /app/users/:uuid/membership` | 手动为用户增加/扣减会员天数 |
| 用户邮箱 | `PUT /app/users/:uuid/email` | 管理员修改用户登录邮箱 |
| 用户设备 | `GET /app/users/:uuid/devices`, `POST .../test-token` | 查看设备列表、签发测试 token |
| 设备统计 | `GET /app/devices/statistics`, `GET /app/devices/active` | 设备整体统计、活跃设备列表 |
| 用户/订单统计 | `GET /app/users/statistics`, `GET /app/orders/statistics` | 用户增长、订单收入统计 |
| 分销商管理 | `GET /app/retailers`, `GET /app/retailers/todos`, `GET /app/retailers/:uuid` | 分销商列表、待跟进事项、详情 |
| 分销商备注 | `CRUD /app/retailers/:uuid/notes` | 沟通记录管理（支持跟进时间、指派人、逾期追踪） |
| 管理员列表 | `GET /app/admins` | 获取管理员用户列表（用于跟进人下拉选择） |
| 提现管理 | `GET /app/wallet/withdraws`, `POST .../approve`, `POST .../complete` | 提现审批流：列表 -> 批准 -> 完成（打款凭证） |
| 订单管理 | `GET /app/orders`, `GET /app/orders/:uuid` | 订单列表（分页）、订单详情 |
| 优惠活动 | `CRUD /app/campaigns` | 优惠码管理：折扣/优惠券两种类型，限时限量 |
| 活动统计 | `GET /app/campaigns/code/:code/stats\|orders\|funnel` | 按活动码查看统计、订单列表、转化漏斗 |
| EDM 邮件营销 | `CRUD /app/edm/templates`, `POST /app/edm/tasks`, `GET /app/edm/send-logs` | 多语言邮件模板 CRUD、发送任务入队、发送日志与统计 |
| EDM 翻译 | `POST /app/edm/templates/:id/translate/:language` | 自动翻译邮件模板到指定语言 |
| 云实例管理 | `GET/POST/DELETE /app/cloud/instances`, `POST .../change-ip`, `PUT .../traffic-config` | 多云 VPS 实例列表、同步、换IP、创建、删除、流量配置 |
| 云元数据 | `GET /app/cloud/accounts\|regions\|plans\|images` | 获取已配置账号、可用区域、套餐、镜像 |
| SSH Terminal | `GET /app/nodes/:ipv4/terminal` (WebSocket) | 浏览器 SSH 终端，通过 WebSocket 连接节点 |
| WebSocket Token | `GET /app/ws-token` | 获取短期 token 用于跨域 WebSocket 认证 |
| 批量脚本 | `CRUD /app/batch-scripts`, `POST .../test`, version history | 加密存储脚本模板、测试执行、版本历史与回滚 |
| 批量任务 | `CRUD /app/batch-tasks`, pause/resume/retry/schedule | 批量执行任务管理：立即/定时/cron、暂停恢复、重试 |
| 策略规则 | `CRUD /app/strategy/rules`, `PUT .../activate` | 版本化路由策略规则管理，同一时刻只有一个活跃版本 |
| Asynqmon | `/app/asynqmon` | Asynq 任务队列监控 Web UI（独立认证中间件，返回 HTML） |

### Slave 节点 API (`/slave/*`)

Slave 节点是运行 VPN 服务的物理服务器。通过 `SlaveAuthRequired()` 使用 HTTP Basic Auth（IPv4:NodeSecret）认证。

| 路由 | 认证 | 说明 |
|------|------|------|
| `PUT /slave/nodes/:ipv4` | 无（首次注册使用请求体中的 secretToken） | 节点注册/更新：提交国家、区域、名称、隧道配置。已存在节点验证 secretToken 后全量替换 |
| `PUT /slave/nodes/:ipv4/tunnels/:domain` | SlaveAuth | 添加/更新单个隧道：返回 SSL 证书 + 私钥 |
| `DELETE /slave/nodes/:ipv4/tunnels/:domain` | SlaveAuth | 删除隧道（幂等，不存在也返回成功） |
| `POST /slave/report/status` | SlaveAuth | 上报节点健康指标：CPU/内存/磁盘/连接数/带宽/丢包率/月度流量 |
| `POST /slave/device-check-auth` | SlaveAuth | 设备认证代理：节点转发客户端的 JWT token 或密码认证请求 |
| `GET /slave/accelerate-tunnels` | SlaveAuth | 获取全量隧道列表（用于加速/中继路由） |
| `GET /slave/resolve-domain` | SlaveAuth | DNS 式域名解析：精确匹配 + 通配符匹配 |
| `GET /slave/ech/keys` | SlaveAuth | 获取 ECH 密钥对（active + grace_period 状态的密钥） |
| `POST /slave/nodes/:ipv4/route-diagnosis` | SlaveAuth | 上报 inbound 路由诊断结果（carrier:province -> route_type） |
| `GET /slave/init-node.sh` | 公开 | 节点初始化脚本（`curl -fsSL .../init-node.sh \| sudo bash`） |
| `GET /slave/ssh-pubkey` | 公开 | 获取中心 SSH 公钥（节点初始化时添加到 authorized_keys） |
| `GET /slave/docker-compose.yml` | 公开 | 节点 Docker Compose 模板 |

### CSR 证书签发 (`/csr/*`)

为 sslip.io/nip.io IP 编码域名提供证书签发服务。公开 API，通过 challenge-response 验证域名所有权。

| 路由 | 说明 |
|------|------|
| `POST /csr/submit` | 提交 CSR：公钥 + 域名列表 -> 返回 requestID + challenge |
| `POST /csr/verify` | 验证 challenge + 签发证书：requestID + challenge response -> 返回签名证书 + 序列号 |

### 节点健康负载计算

`calculateServerLoad()` 实现三层负载评分算法（0-100分）：

1. **严重问题检测** — CPU>=90%/内存>=95%/磁盘>=95%/丢包>=10% 直接返回 90-95 分
2. **各维度负载计算** — CPU/内存/磁盘/丢包/带宽/连接数分别映射到 0-100
3. **综合计算** — `max(木桶短板 * 0.7, 加权平均)`，确保单一指标异常不被稀释

## Technical Decisions

### 1. Cloud Provider 统一接口

**决策**: 所有云厂商实现 `cloudprovider.Provider` 接口，通过 `NewProvider(config)` 工厂函数创建。

**接口方法**: `Name()`, `GetInstanceStatus()`, `ListInstances()`, `ChangeIP()`, `CreateInstance()`, `DeleteInstance()`, `ListRegions()`, `ListPlans()`, `ListImages()`

**支持的 Provider**:

| Provider 常量 | 文件 | 说明 |
|---------------|------|------|
| `aliyun_swas` | `aliyun_swas.go` | 阿里云（国内区域）轻量应用服务器 |
| `alibaba_swas` | `alibaba_swas.go` | 阿里云（国际区域）轻量应用服务器 |
| `aws_lightsail` | `aws_lightsail.go` | AWS Lightsail 多区域支持 |
| `tencent_lighthouse` | `tencent_lighthouse.go` | 腾讯云（国际区域）Lighthouse |
| `qcloud_lighthouse` | `tencent_lighthouse.go` | 腾讯云（国内区域）Lighthouse |
| `bandwagon` | `bandwagon.go` | BandwagonHost，VEID/APIKey 认证，支持多实例 |
| `ssh_standalone` | `ssh_standalone.go` | 纯 SSH 主机（无云 API），自动发现没有 CloudInstance 记录的 SlaveNode |

**Multi-region 模式**: AWS/Aliyun/Alibaba/Tencent 在不传 region 参数时自动创建 MultiRegion provider，遍历所有已知区域。

**统一 Region 注册表**: `region.go` 维护全局区域映射（40+ 区域），每个区域包含 slug、中英文名称、国家代码、各 provider 的原生区域 ID。

### 2. Asynq 任务驱动架构

**决策**: 所有耗时操作通过 Asynq 任务队列异步执行，`worker_integration.go` 的 `InitWorker()` 统一注册所有 handler 和 cron。

**任务类型**:

| 任务类型 | Worker 文件 | 调度方式 | 说明 |
|----------|-------------|----------|------|
| `edm:send` | `worker_integration.go` | 手动入队（立即/定时） | EDM 邮件批量发送 |
| `push:send` | `worker_integration.go` | 手动入队 | 推送通知发送 |
| `renewal:reminder` | `worker_renewal_reminder.go` | Cron: `30 2 * * *`（北京时间10:30） | 续费提醒（30/14/7/3 天梯度） |
| `retailer:followup` | `worker_retailer_followup.go` | Cron: `* * * * *`（每分钟） | 分销商跟进到期提醒（Slack 通知） |
| `ech:key_rotation` | `worker_ech.go` | 定时器（24h）+ 手动入队 | ECH 密钥轮换：active -> grace_period -> retired |
| `diagnosis:outbound` | `worker_diagnosis.go` | 手动入队 + 新节点自动触发 | 单 IP outbound 路由诊断（阿里云站点监控） |
| `diagnosis:all` | `worker_diagnosis.go` | Cron（可配置，默认每周） | 全节点批量诊断（拆分为 N 个 outbound 子任务） |
| `cloud:sync:all` | `worker_cloud.go` | Cron（可配置） | 同步所有云账号实例状态 |
| `cloud:change_ip` | `worker_cloud.go` | 手动入队（默认凌晨 2:00 UTC+8） | 云实例换 IP |
| `cloud:create` | `worker_cloud.go` | 手动入队 | 创建云实例 |
| `cloud:delete` | `worker_cloud.go` | 手动入队 | 删除云实例 |
| `slave:batch:exec` | `worker_batch.go` | 手动入队（立即/定时/cron） | 批量脚本执行（SSH 逐节点串行） |

**Asynqmon 面板**: 挂载于 `/app/asynqmon`，使用独立的 `asynqmonAuthMiddleware()`（返回 HTML 错误页，而非 JSON）。

### 3. Slave 认证机制

**决策**: Slave 节点使用 HTTP Basic Auth，用户名为节点 IPv4，密码为 NodeSecret。

**原因**:
- 节点注册时生成 `SecretToken`（64字符 hex），持久化保存
- Basic Auth 简单可靠，适合服务器间通信
- 首次注册 (`PUT /slave/nodes/:ipv4`) 不需要 SlaveAuth（使用请求体中的 secretToken 验证身份）
- 后续所有操作需要 `SlaveAuthRequired()` 中间件验证

**设备认证代理**: `POST /slave/device-check-auth` 支持双模式认证：
- JWT token 模式（k2wss 协议）：验证 token + UDID 匹配 + 会员有效期
- Password 模式（k2oc/RADIUS 协议）：UDID + 密码哈希验证

### 4. 批量脚本执行系统

**决策**: 脚本内容 AES-256-GCM 加密存储，执行通过 SSH 串行逐节点执行。

**组件**:
- `SlaveBatchScript`: 脚本模板（加密存储 + 版本历史）
- `SlaveBatchTask`: 执行任务（once/cron 两种调度，pending/running/paused/completed/failed 状态机）
- `SlaveBatchTaskResult`: 单节点执行结果（stdout/stderr/exit_code/error）
- `SlaveBatchScriptVersion`: 版本历史审计
- 支持 cron 调度（启动时从 DB 加载所有活跃 cron 任务）

### 5. ECH 密钥管理

**决策**: X25519 密钥对用于 ECH (Encrypted Client Hello)，24 小时轮换检查，grace period 过渡保证解密连续性。

**密钥生命周期**: `active` -> `grace_period`（到期后仍可解密）-> `retired`（完全退役）

**存储**: 私钥 + 公钥 + ECHConfig 均 AES-256-GCM 加密后 Base64 存储，ConfigID 1-255 循环分配。

### 6. 提现审批流

**决策**: 三步流程 — `pending` -> `approved` -> `completed`，支持多种提现渠道。

**支持渠道**:
- 加密货币：TRON / Polygon / BSC / Arbitrum（USDT/USDC）
- 传统支付：PayPal（USD）

**钱包系统**:
- `WalletChange` 记录收入（30天冻结期）、提现、退款
- 可用余额 = 总余额 - 冻结金额 - 待处理提现
- 乐观锁（`Version` 字段）防止并发问题

### 7. 分销商等级体系

**决策**: 4 级分销商体系，L1->L2 自动升级，L3/L4 需人工审核。

| 等级 | 名称 | 首单分成 | 续费分成 | 升级条件 |
|------|------|----------|----------|----------|
| L1 | 推荐者 | 20% | 0% | 默认等级 |
| L2 | 分销商 | 25% | 10% | 累计 10 个付费用户 |
| L3 | 优质分销商 | 30% | 20% | 累计 30 个付费用户 + 内容证明 |
| L4 | 合伙人 | 30% | 30% | 累计 100 个付费用户 + 内容证明 |

**沟通记录系统**: `RetailerNote` 模型支持跟进时间、指派人、逾期追踪、Slack 通知提醒。

### 8. 路由诊断系统

**决策**: 双向诊断 — outbound（Center 主动探测，通过阿里云站点监控）+ inbound（Slave 上报）。

**数据存储**: `IPRouteInfo` 按 IP + direction 唯一索引，RouteMatrix 为 JSON map（`carrier:province` -> `route_type`）。新节点注册时自动触发 outbound 诊断。

### 9. 中间件分层

| 中间件 | 适用路由 | 说明 |
|--------|----------|------|
| `AuthRequired()` | `/api/*` 需认证路由 | JWT 认证（Cookie/Bearer/AccessKey/Query 四种来源） |
| `AuthOptional()` | 混合路由 | 尝试认证但不拦截 |
| `ProRequired()` | `/api/tunnels`, `/api/relays` | 检查会员有效期 |
| `DeviceAuthRequired()` | 设备相关 API | 要求请求包含有效设备信息 |
| `AdminRequired()` | `/app/*` | 检查 `IsAdmin=true` |
| `RetailerRequired()` | `/api/retailer/*` | 检查 `IsRetailer=true` |
| `SlaveAuthRequired()` | `/slave/*` 需认证路由 | Basic Auth (IPv4:NodeSecret) |
| `asynqmonAuthMiddleware()` | `/app/asynqmon` | Admin 认证（返回 HTML 错误页） |
| `ApiCORSMiddleware()` | `/api/*` | CORS: localhost/RFC1918/capacitor:// |
| `CORSMiddleware()` | `/app/*` | CORS: kaitu.io + localhost:3000 |

## Key Files

### Admin 路由与 Handler

| 文件 | 说明 |
|------|------|
| `api/route.go` | 全部路由注册（`/api/*`, `/app/*`, `/slave/*`, `/csr/*`） |
| `api/middleware.go` | 所有中间件：Auth chain、Admin/Retailer/Slave guard、CORS |
| `api/api_admin_*.go` | Admin HTTP handler（隧道、节点、用户、订单、活动、EDM、云、批量脚本、策略） |
| `api/api_csr.go` | CSR 证书签发 handler |

### Slave 节点 API

| 文件 | 说明 |
|------|------|
| `api/slave_api.go` | 加速隧道列表、域名解析 |
| `api/slave_api_node.go` | 节点注册/更新、隧道 CRUD、路由诊断上报 |
| `api/slave_api_report.go` | 节点健康状态上报 + 负载计算算法 |
| `api/slave_api_device_auth.go` | 设备认证代理（JWT + Password 双模式） |

### Cloud Provider 层

| 文件 | 说明 |
|------|------|
| `api/cloudprovider/provider.go` | `Provider` 接口定义 + 通用类型（InstanceStatus, RegionInfo, PlanInfo, ImageInfo） |
| `api/cloudprovider/factory.go` | `NewProvider()` 工厂函数 |
| `api/cloudprovider/region.go` | 统一区域注册表（40+ 区域，7 provider 映射） |
| `api/cloudprovider/aliyun_swas.go` | 阿里云国内 SWAS |
| `api/cloudprovider/alibaba_swas.go` | 阿里云国际 SWAS |
| `api/cloudprovider/aws_lightsail.go` | AWS Lightsail |
| `api/cloudprovider/tencent_lighthouse.go` | 腾讯云 Lighthouse（国内 qcloud + 国际 tencent） |
| `api/cloudprovider/bandwagon.go` | BandwagonHost（支持多实例） |
| `api/cloudprovider/ssh_standalone.go` | SSH 纯主机（自动发现孤儿 SlaveNode） |
| `api/cloudprovider/traffic_stats.go` | vnstat 流量统计解析 |

### Background Workers

| 文件 | 说明 |
|------|------|
| `api/worker_integration.go` | `InitWorker()` 注册所有 handler + cron，EDM/Push 任务处理 |
| `api/worker_cloud.go` | 云实例同步、换IP、创建、删除（含 Slack 通知） |
| `api/worker_ech.go` | ECH 密钥轮换（24h 定时器 + 手动触发） |
| `api/worker_batch.go` | 批量脚本执行（串行 SSH + cron 任务加载） |
| `api/worker_diagnosis.go` | 路由诊断：单 IP outbound + 全节点批量 |
| `api/worker_renewal_reminder.go` | 续费提醒（30/14/7/3 天梯度，邮件发送 + 幂等性检查） |
| `api/worker_retailer_followup.go` | 分销商跟进提醒（Slack 通知到期/逾期任务） |

### 数据模型

| 文件 | 模型 |
|------|------|
| `api/model.go` | User, Device, LoginIdentify, Order, Plan, InviteCode, UserProHistory, Message, SlaveNode, SlaveTunnel, SlaveNodeLoad, SessionAcct, Secret, Campaign, EmailMarketingTemplate, EmailSendLog, ECHKey, StrategyRules, TelemetryEvent, TelemetryRateLimit, IPRouteInfo, CloudInstance, SlaveBatchScript, SlaveBatchTask, SlaveBatchTaskResult, SlaveBatchScriptVersion, CloudOperationLog |
| `api/model_wallet.go` | Wallet, WalletChange, WithdrawAccount, Withdraw |
| `api/model_push.go` | PushToken |
| `api/model_retailer.go` | RetailerNote, RetailerTodoItem, AdminRetailerListItem, AdminRetailerDetailData |

### 响应与类型

| 文件 | 说明 |
|------|------|
| `api/response.go` | `Success()`, `Error()`, `ListWithData()` + 错误码常量 |
| `api/type.go` | 请求/响应 DTO、角色位掩码 |

## Acceptance Criteria

### Admin 面板

- [x] `/app/*` 所有路由需 `AdminRequired()` 中间件，非管理员返回 403
- [x] CORS 仅允许 kaitu.io 域名和 localhost:3000
- [x] 隧道/节点/套餐支持完整 CRUD
- [x] 用户管理支持：列表搜索、详情查看、分销商状态切换、会员时长调整、邮箱修改、设备查看、硬删除
- [x] 成员管理支持：admin 代用户添加/移除付费委托成员
- [x] 分销商管理支持：列表（含待跟进统计）、详情、沟通记录 CRUD、跟进指派、逾期追踪
- [x] 提现审批三步流程：列表 -> approve -> complete（含打款凭证）
- [x] 订单管理：列表分页、详情查看、统计分析
- [x] 优惠活动 CRUD + 按活动码统计/订单/转化漏斗
- [x] EDM 系统：多语言模板 CRUD、自动翻译、异步发送任务、发送日志与统计
- [x] 云实例管理：列表同步、创建/删除/换IP、流量配置、多账号多区域
- [x] SSH Terminal WebSocket 连接到节点
- [x] 批量脚本：加密存储、版本历史、测试执行、cron 调度
- [x] 策略规则：版本化 CRUD + 激活/停用
- [x] 设备/用户/订单统计 API
- [x] Asynqmon 任务监控面板可访问

### Slave 节点 API

- [x] 节点注册使用 secretToken 验证（首次无需 Basic Auth）
- [x] 已存在节点全量替换（先删后建，事务保证一致性）
- [x] 隧道注册返回 SSL 证书 + 私钥
- [x] 健康上报计算负载评分并缓存失效
- [x] 设备认证代理支持 JWT + Password 双模式
- [x] 域名解析支持精确匹配和通配符匹配
- [x] ECH 密钥同步返回 active + grace_period 状态的密钥
- [x] Inbound 路由诊断上报 upsert 存储
- [x] 新节点注册自动触发 outbound 诊断
- [x] 公开脚本/公钥/docker-compose 端点无需认证

### CSR 证书签发

- [x] 公开 API，无需认证
- [x] 域名所有权通过 challenge-response 验证
- [x] 仅支持 sslip.io / nip.io IP 编码域名

### Cloud Provider

- [x] 7 个 provider 实现统一 `Provider` 接口
- [x] 不支持的操作返回 `NotSupportedError`
- [x] AWS/Aliyun/Alibaba/Tencent 支持多区域自动发现
- [x] Bandwagon 支持多实例配置
- [x] SSH Standalone 自动发现孤儿 SlaveNode
- [x] 统一 Region 注册表包含 40+ 区域映射
- [x] 云操作失败发送 Slack 通知

### Background Workers

- [x] 所有 worker 在 `InitWorker()` 中统一注册
- [x] 续费提醒：30/14/7/3 天梯度，幂等性检查防重发
- [x] 分销商跟进：每分钟检查到期/逾期任务，Slack 通知
- [x] ECH 轮换：24h 定时 + 启动时立即执行一次
- [x] 云同步：cron 定时 + 手动触发，孤儿实例自动标记删除
- [x] 路由诊断：单 IP + 全节点批量，Unique 防重复
- [x] 批量脚本：启动时加载 DB 中所有活跃 cron 任务
- [x] 云操作默认调度到凌晨 2:00 UTC+8 执行
