# Feature: Center API — Core Routes

## Meta

| Field | Value |
|-------|-------|
| Feature | center-api-core |
| Version | v1 |
| Status | implemented |
| Created | 2026-02-18 |
| Updated | 2026-02-18 |

## Overview

Center API 是 Kaitu VPN 的中心后端服务，提供认证、用户管理、订阅支付、隧道分发、邀请系统、分销商系统、钱包提现、推送通知等完整业务功能。基于 Go + Gin + GORM 构建，采用扁平包结构（所有文件在 `api/` 根目录），通过文件命名约定（而非目录结构）来组织代码。

核心设计原则：
- **HTTP 200 恒定**：所有业务响应 HTTP status 始终 200，错误状态通过 JSON `code` 字段传递
- **CORS 分裂**：`/api/*` 客户端路由仅允许本地/LAN 来源；`/app/*` 管理路由允许 kaitu.io 域名
- **四级认证**：Cookie > X-Access-Key > Bearer Token > URL Query Token，按优先级依次尝试
- **角色位掩码**：uint64 存储 64 个角色位，JWT 中携带 `roles` 字段

## Product Requirements

### 1. 认证系统（Auth）

用户通过邮箱 OTP 验证码或密码登录，获取 JWT token 对。支持两种模式：

- **设备登录**（Desktop/Mobile）：`/api/auth/login` + `/api/auth/login/password` — 绑定设备 UDID，返回 access_token + refresh_token
- **Web 登录**：`/api/auth/web-login` — 无设备绑定，通过 HttpOnly Cookie + CSRF Token 认证，sliding expiration（2 个月有效期，剩余不足 7 天时自动续期）

登录流程：
1. 发送验证码 `POST /api/auth/code` — 自动创建不存在的用户（注册 + 登录统一）
2. 验证码登录 `POST /api/auth/login` — 验证 OTP + 绑定设备 + 处理邀请码 + 激活账号
3. 密码登录 `POST /api/auth/login/password` — bcrypt 密码验证 + 失败锁定机制
4. 刷新令牌 `POST /api/auth/refresh` — 支持 Cookie 和 Body 两种传递方式
5. 登出 `POST /api/auth/logout` — 设备认证删除设备记录，Web 认证清除 Cookie

设备管理策略：每用户最多 `MaxDevice` 台设备（默认 5），超限时自动踢除最久未使用的设备，并发送邮件通知。设备转移（UDID 从 A 用户转到 B 用户）也会通知原所有者。

### 2. 隧道分发（Tunnels & Relays）

- `GET /api/tunnels` — 获取全部隧道列表（排除 k2oc 协议），Legacy API 强制返回 `k2wss` 协议
- `GET /api/tunnels/:protocol` — 新 API，返回真实协议值
- `GET /api/relays` — 获取中继节点列表（`has_relay=true` 的隧道）

三重中间件保护：`AuthRequired() + ProRequired() + DeviceAuthRequired()`

隧道响应中包含：
- 节点负载信息（CPU、流量使用率、带宽使用率）— 通过 batch query 避免 N+1
- 云实例计费信息（流量配额、计费周期）— 通过 IP 地址关联 CloudInstance
- ECH 配置（Base64 编码 ECHConfigList）— 用于 K2v4 TLS 加密

### 3. 用户管理（User）

- 用户信息：`GET /api/user/info` — 支持设备认证和 Web 认证两种模式
- 设备管理：列表、删除、更新备注
- 邮箱变更：发送验证码 → 验证 → 更新（检查邮箱唯一性）
- 成员管理：代付模式（DelegateID），支持添加/移除成员
- 代付人管理：查看/拒绝代付关系
- 账号删除：软删除
- AccessKey 管理：生成/重新生成 API 密钥
- 语言偏好：BCP 47 标准语言标签
- 密码设置：bcrypt 哈希，强度校验
- 工单创建：提交 GitHub Issues

### 4. 订阅与支付（Orders & Plans）

- `GET /api/plans` — 公开接口，获取可用套餐列表
- `POST /api/user/orders` — 创建订单，支持：
  - 预览模式（`preview: true`）：计算价格但不保存
  - 为自己和/或多个成员购买（`forMyself` + `forUserUUIDs`）
  - 优惠码（Campaign）：折扣（discount）和优惠券（coupon）两种类型
  - 通过 Wordgate SDK 创建第三方支付订单
- `GET /api/user/pro-histories` — Pro 版变更历史（购买、邀请奖励、系统发放）

Webhook 回调：`POST /webhook/wordgate` — 支付成功后回调，HTTP status 直接返回（非 200 恒定模式，因上游依赖 HTTP status 进行重试）

### 5. 邀请系统（Invite）

四层接口：
- `GET /api/invite/code` — 公开查询邀请码信息
- `GET/POST /api/invite/my-codes` — 我的邀请码 CRUD（创建、列表、更新备注）
- `GET /api/invite/my-codes/latest` — 获取最新邀请码（不存在时自动创建）
- `GET /api/invite/my-users` — 我邀请的用户列表

统计维度：注册人数、购买人数、购买奖励天数（通过 UserProHistory 聚合）

邀请奖励机制：
- 被邀请人首次登录时设置邀请码（仅未激活用户可设置）
- 自邀请检测：不允许使用自己的邀请码
- 下载奖励、购买奖励异步处理

### 6. 分销商系统（Retailer）

四级分销商体系：

| 等级 | 名称 | 首单分成 | 续费分成 | 升级条件 |
|------|------|---------|---------|---------|
| L1 | 推荐者 | 20% | 0% | 默认 |
| L2 | 分销商 | 25% | 10% | 10 个付费用户 |
| L3 | 优质分销商 | 30% | 20% | 30 个付费用户 + 内容证明 |
| L4 | 合伙人 | 30% | 30% | 100 个付费用户 + 内容证明 |

- `GET /api/retailer/level` — 当前等级信息
- `GET /api/retailer/stats` — 统计数据含升级进度百分比

L1 → L2 可自动升级，更高等级需管理员审核。

### 7. 钱包与提现（Wallet）

- `GET /api/wallet` — 钱包信息（余额实时计算：总余额、可用余额、冻结余额）
- `GET /api/wallet/changes` — 变动记录（支持类型过滤）
- 提现账户管理：CRUD + 设置默认
- 提现申请：`POST /api/wallet/withdraws` — 事务中扣减余额 + 记录变动

支持的提现渠道：

| 渠道 | 币种 | 手续费 |
|------|------|-------|
| TRON (TRC-20) | USDT/USDC | 固定 $1 |
| Polygon | USDT/USDC | 固定 $0.50 |
| BSC (BEP-20) | USDT/USDC | 固定 $0.50 |
| Arbitrum | USDT/USDC | 固定 $0.50 |
| PayPal | USD | 3%（最低 $0.30） |

### 8. 推送通知（Push）

- `POST /api/push/token` — 注册推送令牌（从 JWT 获取用户和设备信息）
- `DELETE /api/push/token` — 解绑推送令牌（幂等：未找到也返回成功）

平台/渠道组合验证：
- iOS → APNs only
- Android China → JPush only
- Android Google Play → FCM only

### 9. 策略系统（Strategy & Telemetry）

- `GET /api/strategy/rules` — 获取路由策略规则（支持 ETag 条件请求，304 缓存）
- `POST /api/telemetry/batch` — 批量上报遥测事件（每设备每小时 1000 条限制，INSERT IGNORE 幂等）

### 10. 路由诊断（Diagnosis）

- `GET /api/diagnosis/outbound-route` — 获取指定节点的出站路由信息

### 11. 公开配置接口

- `GET /api/app/config` — 前端应用配置
- `GET /api/ech/config` — ECH (Encrypted Client Hello) 配置
- `GET /api/ca` — CA 证书（公开信息）
- `GET /api/issues/*` — GitHub Issues 代理（需认证）

### 12–14: 节点管理、证书签发、管理后台

> 详见 [center-api-admin.md](center-api-admin.md) — 覆盖 Slave API（`/slave/*`）、CSR（`/csr/*`）、管理后台（`/app/*`）完整路由。

## Technical Decisions

### TD-1: 扁平包结构

**决定**：所有 handler、logic、model 在 `center` 包根目录，通过文件命名约定组织。

**原因**：Go 包级别的可见性天然支持内部访问。扁平结构避免了循环依赖和过深的目录嵌套，对于单一服务来说足够清晰。

### TD-2: HTTP 200 恒定响应

**决定**：所有业务端点返回 HTTP 200，错误状态放在 JSON `code` 字段。

**原因**：前端统一处理逻辑，避免 HTTP status 层面的歧义（如 404 是资源不存在还是路由不存在）。

**例外**：Webhook 回调（`api_webhook.go`）直接返回 HTTP status，因为支付提供商依赖 HTTP status 进行重试逻辑。Asynqmon UI 返回 HTML。

### TD-3: CORS 分裂策略

**决定**：`ApiCORSMiddleware` 用于 `/api/*`，仅允许 localhost、回环地址、RFC 1918 私有 IP、`capacitor://localhost`。`CORSMiddleware` 用于 `/app/*`，允许 kaitu.io 域名和 localhost:3000。

**原因**：客户端通过 Tauri IPC 或 Capacitor 插件调用 API（不经 CORS），只有本地开发和移动 WebView 需要跨域。管理后台部署在 kaitu.io，需要跨域直连（特别是 WebSocket）。

### TD-4: 四级认证优先级

**决定**：认证上下文 `getAuthContext()` 按以下优先级解析：
1. HttpOnly Cookie（Web 端，需 CSRF 验证）
2. X-Access-Key 头部（分销商 API 密钥）
3. Authorization: Bearer Token（Desktop/Mobile）
4. URL 查询参数 `?token=`（WebSocket 跨域）

**原因**：Cookie 优先确保 Web 端安全性（HttpOnly 防 XSS）。X-Access-Key 提供无状态 API 调用能力。Bearer Token 是标准移动端方案。URL 参数是 WebSocket 的唯一选择（无法携带跨域 Cookie）。

### TD-5: 设备与 Token 绑定

**决定**：每次登录创建新设备记录并生成 token，token 中嵌入 `TokenIssueAt` 时间戳。认证时校验 `device.TokenIssueAt == claims.TokenIssueAt`，不匹配则认证失败。

**原因**：实现「一设备一 token」语义 — 同一 UDID 重新登录后旧 token 自动失效，无需维护 token 黑名单。

### TD-6: 邮箱加密存储

**决定**：用户邮箱不明文存储。`LoginIdentify` 表中 `IndexID` 存储 HMAC 哈希（用于查询），`EncryptedValue` 存储 AES 加密值（用于恢复原文）。

**原因**：即使数据库泄露，攻击者也无法直接获取用户邮箱。HMAC 哈希支持精确匹配查询，AES 加密支持向用户展示和发送邮件。

### TD-7: 角色位掩码

**决定**：用 `uint64` 位掩码存储角色，而非关联表。当前定义 4 个角色（user=1, cms_admin=2, cms_editor=4, super=8）。

**原因**：角色数量少且变动不频繁，位运算高效且不需要额外 JOIN。JWT 中直接携带 `roles` 字段，中间件无需查库。

### TD-8: 订单 Meta JSON

**决定**：订单的 Plan、forUserUUIDs、forMyself 等扩展信息存储在 `Meta` JSON 字段中。Campaign 通过外键 `CampaignCode` 单独关联。

**原因**：Plan 信息在订单创建时快照，避免后续套餐变更影响历史订单。JSON 字段灵活且不需要额外表。

### TD-9: 遥测幂等与限流

**决定**：遥测事件通过 `event_id` 唯一索引实现幂等（INSERT IGNORE），通过 `TelemetryRateLimit` 表实现每设备每小时 1000 条限制（原子计数器 `gorm.Expr`）。

**原因**：客户端可能重试上传，幂等避免重复计数。限流防止恶意客户端或 bug 导致的数据库膨胀。

### TD-10: 节点认证 Basic Auth

**决定**：Slave API 使用 HTTP Basic Auth（`IPv4:NodeSecret`），而非 JWT。

**原因**：节点是长期运行的服务，不需要 token 过期和刷新机制。Basic Auth 简单可靠，IPv4 作为标识符天然唯一。

## Key Files

### 路由与框架

| 文件 | 职责 |
|------|------|
| `api/route.go` | 全部路由注册，4 个路由组（`/api`, `/app`, `/slave`, `/csr`） |
| `api/middleware.go` | 认证中间件链、CORS 中间件、Recovery 中间件、角色守卫 |
| `api/response.go` | 响应助手（`Success`, `Error`, `List`, `ItemsAll`）、错误码常量、分页 |
| `api/type.go` | 请求/响应 DTO、角色位掩码、资源类型定义 |

### 认证

| 文件 | 职责 |
|------|------|
| `api/api_auth.go` | 登录（OTP + 密码）、刷新 token、登出、Web 登录、Cookie 管理、WebSocket Token |
| `api/logic_auth.go` | Token 生成/验证、验证码存储/校验、用户查找/创建 |
| `api/logic_password.go` | 密码强度校验、失败锁定、bcrypt 操作 |
| `api/logic_secret.go` | 邮箱 HMAC 哈希 + AES 加密/解密 |

### 隧道与节点

| 文件 | 职责 |
|------|------|
| `api/api_tunnel.go` | 隧道列表 API（含 ECH 配置、云实例数据、负载详情） |
| `api/api_relay.go` | 中继节点列表 API |
| `api/logic_tunnel_health.go` | 节点负载详情计算（batch query） |
| `api/logic_node_load.go` | 节点负载聚合 |
| `api/logic_ech.go` | ECH 密钥管理（生成、轮换、加解密） |

### 用户与设备

| 文件 | 职责 |
|------|------|
| `api/api_user.go` | 用户信息、邮箱变更、账号删除、AccessKey、语言偏好、密码设置 |
| `api/api_device.go` | 设备列表、删除、备注更新 |
| `api/api_member.go` | 成员管理（添加/移除）、代付人管理 |
| `api/logic_user.go` | 用户业务逻辑 |
| `api/logic_member.go` | 成员权限检查 |

### 订阅与支付

| 文件 | 职责 |
|------|------|
| `api/api_order.go` | 创建订单（预览 + 实际）、Pro 历史 |
| `api/api_plan.go` | 套餐列表 |
| `api/api_webhook.go` | 支付回调（Wordgate） |
| `api/logic_order.go` | 订单完成后的会员时长计算、邀请奖励处理 |
| `api/logic_plan.go` | 套餐查询 |
| `api/logic_campaign.go` | 优惠活动匹配与应用 |

### 邀请系统

| 文件 | 职责 |
|------|------|
| `api/api_my_invite_code.go` | 我的邀请码 CRUD + 统计 |
| `api/api_my_invite_user.go` | 我邀请的用户列表 |
| `api/api_invited_code.go` | 公开查询邀请码 |
| `api/api_share_link.go` | 邀请码分享链接 |
| `api/logic_invite.go` | 邀请奖励发放逻辑 |

### 分销商

| 文件 | 职责 |
|------|------|
| `api/api_retailer.go` | 分销商等级信息、统计数据 |
| `api/logic_retailer.go` | 分销商配置管理、等级升降级 |
| `api/model_retailer.go` | RetailerConfig、RetailerNote 模型 |

### 钱包

| 文件 | 职责 |
|------|------|
| `api/api_wallet.go` | 钱包信息、变动记录、提现账户管理、提现申请 |
| `api/logic_wallet.go` | 余额计算、提现验证、佣金处理 |
| `api/model_wallet.go` | Wallet、WalletChange、WithdrawAccount、Withdraw 模型 |

### 推送

| 文件 | 职责 |
|------|------|
| `api/api_push.go` | 推送令牌注册/解绑 |
| `api/logic_push.go` | 推送发送逻辑 |
| `api/model_push.go` | PushToken 模型 |

### 策略与遥测

| 文件 | 职责 |
|------|------|
| `api/api_strategy.go` | 策略规则获取、遥测事件批量上报 |

### 节点管理、证书、管理后台

> Key files 详见 [center-api-admin.md](center-api-admin.md)。

### 数据模型

| 文件 | 职责 |
|------|------|
| `api/model.go` | 核心模型：User, Device, LoginIdentify, Order, Plan, InviteCode, SlaveNode, SlaveTunnel, Campaign, ECHKey, StrategyRules, TelemetryEvent, CloudInstance, BatchScript/Task |
| `api/model_wallet.go` | 钱包相关模型 |
| `api/model_push.go` | 推送令牌模型 |
| `api/model_retailer.go` | 分销商相关模型 |


### 后台任务

| 文件 | 职责 |
|------|------|
| `api/worker_integration.go` | 注册所有 Asynq handler + cron 调度 |
| `api/worker_cloud.go` | 云实例同步/换IP/创建/删除 |
| `api/worker_ech.go` | ECH 密钥轮换 |
| `api/worker_batch.go` | 批量脚本执行 |
| `api/worker_diagnosis.go` | 路由诊断聚合 |
| `api/worker_renewal_reminder.go` | 续费提醒 |
| `api/worker_retailer_followup.go` | 分销商跟进通知 |
| `api/handler_edm.go` | EDM 邮件发送 handler |

## Acceptance Criteria

### 路由架构

- [x] `/api/*` 使用 `ApiCORSMiddleware`（localhost / loopback / RFC1918 / capacitor://）
- [x] `/app/*` 使用 `CORSMiddleware`（kaitu.io / localhost:3000）+ `AdminRequired()`
- [x] `/slave/*` 使用 `SlaveAuthRequired()`（Basic Auth: IPv4:NodeSecret）
- [x] `/csr/*` 无认证（域名验证通过 challenge-response）
- [x] `/app/asynqmon` 使用 `asynqmonAuthMiddleware()`（返回 HTML 错误页）
- [x] OPTIONS preflight 由 CORS 中间件处理，返回 204
- [x] Recovery 中间件捕获 panic，Slack 报警

### 认证系统

- [x] 邮箱 OTP：`/api/auth/code` 统一处理登录和注册（不存在用户自动创建）
- [x] 密码登录：bcrypt 验证 + 失败计数锁定 + 通用错误信息防枚举
- [x] Web Cookie：HttpOnly + Secure + SameSite=Lax + CSRF Token + Sliding Expiration
- [x] 认证优先级：Cookie > X-Access-Key > Bearer > URL Query
- [x] Token 绑定：`device.TokenIssueAt == claims.TokenIssueAt` 校验
- [x] 设备限制：超限踢除最久未用设备 + 邮件通知
- [x] 设备转移：检测 UDID 用户变更 + 通知原所有者
- [x] WebSocket Token：短期（5 分钟）JWT，用于 WS 握手

### 响应格式

- [x] HTTP 200 恒定（业务端点）
- [x] 单对象：`Success(c, data)` → `{code: 0, data: {...}}`
- [x] 列表：`List(c, items, pagination)` → `{code: 0, data: {items: [...], pagination: {...}}}`
- [x] 空成功：`SuccessEmpty(c)` → `{code: 0, data: {}}`
- [x] 错误：`Error(c, ErrorCode, "message")` → `{code: 422, message: "..."}`
- [x] 分页：1-based，默认 pageSize=10，最大 100

### 隧道分发

- [x] 排除 k2oc 协议隧道
- [x] 非管理员用户过滤测试节点（`is_test=false`）
- [x] Legacy API (`/tunnels`) 强制返回 `k2wss` 协议
- [x] New API (`/tunnels/:protocol`) 返回真实协议
- [x] 响应包含节点负载（CPU、流量、带宽）
- [x] 响应包含云实例数据（流量配额、计费周期）
- [x] 响应包含 ECH 配置（Base64 ECHConfigList）
- [x] Batch query 避免 N+1 问题

### 用户管理

- [x] 邮箱加密存储（HMAC 索引 + AES 加密值）
- [x] 成员代付模式（DelegateID 关联）
- [x] 账号软删除（`gorm.DeletedAt`）
- [x] AccessKey 生成/重新生成
- [x] 语言偏好 BCP 47 标准化
- [x] 密码：bcrypt 哈希 + 强度校验 + 失败锁定

### 邀请系统

- [x] 邀请码编码：`util.NumEncoder`（6 位，seed 固定不可变）
- [x] 自邀请检测
- [x] 仅未激活用户可设置邀请码
- [x] 统计：注册人数、购买人数、购买奖励天数
- [x] 批量查询优化（按 InvitedByCodeID 分组聚合）

### 分销商

- [x] 四级等级体系（L1-L4）
- [x] L1 → L2 自动升级
- [x] 升级进度百分比计算
- [x] 首单/续费双维度分成

### 钱包与提现

- [x] 余额实时计算（总余额、可用余额、冻结余额）
- [x] 提现事务：扣减余额 + 记录变动（原子操作）
- [x] 5 种提现渠道（TRON/Polygon/BSC/Arbitrum/PayPal）
- [x] 地址格式校验（TRON: T开头34位, EVM: 0x开头42位, PayPal: email）
- [x] 手续费计算（固定费 + PayPal 百分比费）
- [x] 第一个账户自动设为默认

### 推送通知

- [x] 平台/渠道组合验证
- [x] 每设备每通道一个令牌（upsert 语义）
- [x] 解绑幂等（未找到也返回成功）

### 策略与遥测

- [x] 策略规则支持 ETag 缓存（304 Not Modified）
- [x] 遥测限流：每设备每小时 1000 条（原子计数器）
- [x] 遥测幂等：event_id 唯一索引 + INSERT IGNORE

### 节点管理 & 管理后台

> AC 详见 [center-api-admin.md](center-api-admin.md)。
