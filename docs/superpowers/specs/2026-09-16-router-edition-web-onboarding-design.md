# 开途路由器版：网站开通入口 + 后台台账 设计

> 决策日期 2026-09-16。承接 `2026-06-13-dedicated-line-product-reframe-design.md`（专属线路 Phase 1）与 `2026-06-12-private-node-router-onboarding-design.md`（Plan 5b）。本文把两者的产品面**倒过来**：用户购买的商品是「路由器版」，专属线路降级为内部实现，不再单独售卖。

## 1. 结论与拍板

| 决策点 | 结论（用户拍板 2026-09-16） |
|---|---|
| 商品 | **一个商品：开途路由器版** = 一台刷好 k2r 的二手经典路由器 + 一年服务费 |
| 专属线路 | 不再面向用户售卖。生产库 `pn-*` 6 个 SKU 保持 `is_active=false`，只作内部开机规格 |
| IP 类型 | **暂不接家庭 IP**，只用数据中心线路 |
| 流量 | 统一一档（内部规格 `pn-dc-2t`：每月 2TB，国内直连不计） |
| 硬件 | 一款经典可刷机路由器，**二手采购，泰国刷机后发货** |
| 定价 | **按需求定价，不按成本**：首年 $399（含路由器），续费 $299/年；结账页不拆硬件明细 |
| 自备路由器 | 保留为次要路径（只收服务年费），入口是一个链接，不抢主路径 |
| 准入 | 沿用 Phase 1：持有 ≥1 条 active 内部线路即可用路由器；一账号一路由器 |

### 1.1 为什么倒过来

- 路由器版的买点是「全家所有设备零配置联网，电视 / 游戏机 / 音箱都能用」，与 App 版是两个人群、两条产品线。
- 独享出口是让路由器 always-on 在成本上成立的手段（固定流量 VPS + 95% 硬断流）。用户不需要理解「线路」，也不会为它单独掏钱。
- 网站上目前**没有任何可走通的路由器版开通路径**（见 §2），而技术链路已在 2026-09-16 于出厂 OpenWrt 25.12 实测跑通（安装命令带凭证，8 秒装完即连通）。

## 2. 现状（2026-09-16 核实）

| 事实 | 出处 |
|---|---|
| 网站 `/purchase` 只卖 App 订阅，无产品切换 | `web/src/app/[locale]/purchase/PurchaseClient.tsx` 调 `/api/plans`（已冻结 product=app） |
| 生产库 6 个 `product=private_node` SKU 全部停用；生产路由器设备 0 台 | `list_admin_plans` / `device_statistics` |
| `/routers` 是「5 款自购机型导购 + 刷机教程 + 成品预售联系客服」，无价格无下单 | `web/src/app/[locale]/routers/` |
| `/routers` 第 3、4 步文案失真：面板 `:9000`、「智能选服」Tab 粘贴链接 | 生产 k2r 是 `-tags nowebapp` headless 构建，没有面板；真实路径是 `wget -qO- https://kaitu.io/i/k2r \| sh -s '<k2subs URL>'` |
| App 内 webapp 已有 `/purchase?product=private_node`、`/private-node` 管理页、AddRouterCard 铸凭证 | Plan 5 / 5b，将随本设计收口 |
| 后台仅有 节点部署 / 节点运维 / 节点管理 / 企业路由器；无线路订阅台账、无路由器设备台账、无发货流程 | `web/src/components/manager-sidebar.tsx`；api 无 admin 读 `PrivateNodeSubscription` 端点 |
| 网关凭证只能用户本人铸造（`POST /api/user/gateway-credential`） | `api/api_gateway_credential.go` |
| 线路续费写 `ExpiresAt` 的路径未实现 | 2026-06 记忆，实现前须复核 `worker_private_node_lifecycle.go` 与 `applyOrderToBuyer` |

## 3. 商品与定价

### 3.1 硬件 SKU（建议）

推荐 **红米 AX6S（RB03）**：OpenWrt 社区保有量第一、刷机资料最全、二手良品 ¥150 上下、MT7622 arm64（k2r `linux-arm64` 直接跑，实测 400–500 Mbps）、WiFi 6 AX1800、千兆 WAN/LAN。备选小米 AX3000T（RA82，CPU 略强、WiFi 6 AX3000、二手 ¥200 上下），若采购价接近可直接换成它，产品面不变。

采购 / 刷机 / 发货全在泰国完成。发货前完成三件事：刷 OpenWrt（或 ImmortalWrt）、装 k2r、**用该客户账号的网关凭证跑 `k2r setup`**。用户收货插上网线即上线。

### 3.2 价格结构（按需求定价，不按成本）

用户拍板：**价格按需求与价值定，不按成本倒推**（销售成本本身很高）。因此：

- 结账页**不拆硬件明细**，只有一个首年价「含开途路由器一台」，避免用户拿二手机行情对比。
- 续费价单独定，低于首年价，作为留存杠杆；差额在页面上表述为「续费优惠」而非「不含硬件」。
- 页面锚点：对比 App 版「每人每年约 $40、5 台设备」与路由器版「全家不限设备、独享出口、开箱即用」；对比自己折腾（买机 + 刷机 + 找机场）的时间与风险。

| 项 | 首年 | 续费 |
|---|---|---|
| 开途路由器版（含路由器、刷机、发货、独享出口 2TB/月） | **$399**（定稿 2026-09-16） | **$299**（定稿 2026-09-16） |

两个价格落到 Plan 表：

- `router-std-1y`：`product=router`，`month=12`，`price=39900`，`label=开途路由器版·首年（含路由器）`，`hardware_sku=redmi-ax6s`（仅供后台发货与台账，不用于展示拆价）。
- `router-svc-1y`：`product=router`，`month=12`，`price=29900`，`label=开途路由器版·续费一年`，`hardware_sku=""`。既是续费商品，也是「自备路由器」路径的商品。

`Plan` 只新增 `hardware_sku` 一个字段，不加 `hardware_price`。

两者内部都映射到 `PrivateNodePlanSpec`（复用 `pn-dc-2t` 的规格：`ip_type=non_residential`、`traffic_total_bytes=2TB`、`allowed_regions`）。**不新增 SKU 矩阵**。

## 4. 网站信息架构与开通流程

### 4.1 入口

1. `/routers` 改造为**产品页**：首屏（全家联通）、价格卡（首年含路由器 / 次年起服务费）、「怎么用」三步图（收货、插网线、完成）、FAQ、CTA「立即购买」直达 `/purchase/router`。现有机型导购 + 刷机教程 + 成品预售卡整体移到 `/routers/diy`，只给自备路由器用户看，并修正 `:9000` / 「智能选服」等失真文案。
2. 新增结账页 `/purchase/router`（独立路由，不用 `?product=`；营销投放与 SEO 更干净，embed 模式也能直达）。复用现有三步骨架（登录 / 选择 / 支付）与 WordGate 下单链路。
3. `/purchase` 顶部加分段切换「App 版 | 路由器版」，App 版页底加一行「电视、游戏机也想用？看看路由器版」。头部导航「路由器」指向 `/routers`。
4. 品牌门：整套页面 kaitu-only（`Brand.features.routers` 已有），overleap 404。

### 4.2 结账页 `/purchase/router`

| 步 | 内容 | 默认 |
|---|---|---|
| 1 登录 | 复用邮箱验证码 | |
| 2 选择 | **默认选中「开途路由器（红米 AX6S，已刷好，开箱即用）」**；下方一行链接「我已有 OpenWrt 路由器，只买服务」切到 `router-svc-1y`。地区下拉（默认日本，可改，值来自 `allowed_regions`） | `router-std-1y` + 日本 |
| 3 确认支付 | 一行「开途路由器版·首年（含路由器）$399」；下方一句「次年起续费 $299/年」。收货信息：姓名、手机、地址（成品才收集） | |

无流量档、无 IP 类型选择。活动码 / 代付沿用现有能力。

### 4.3 付款后

**成品路径**（`router-std-1y`）：
1. 付款成功：页面进入「开通中」，发欢迎邮件（含「我的路由器」链接）。
2. 后台阶段推进（§6）：线路开机（现有节点运维队列）→ 线路就绪 → 运维**代客户铸造网关凭证**并写入路由器 → 填快递单、标记已发货。
3. 用户收货插网线，k2r 自动上线（`k2r setup` 已持久化凭证，重启自动重连已实测），账户页显示「路由器已在线」。

**自备路径**（`router-svc-1y`）：
1. 付款成功、线路开机完成后，账户页显示一条**个性化安装命令**（`wget -qO- https://kaitu.io/i/k2r | sh -s '<k2subs://…>'`），凭证已嵌入。
2. 用户在路由器 SSH 执行，装完即连通。
3. 账户页轮询路由器设备最近心跳，检测到上线即显示完成。

### 4.4 账户页 `/account/router`（我的路由器）

- 状态卡：路由器在线 / 离线（按 `Device.TokenLastUsedAt` 或 `/api/subs` 最近拉取判定，阈值 10 分钟）、k2r 版本、公网出口地区。
- 用量卡：本月已用 / 2TB、重置日；≥80% 黄、≥95% 红并解释「本月已用完，下月 X 日恢复」（复用 `quotaExhausted` / `quotaResetAt`）。
- 订阅卡：到期日、「续费一年」按钮（下单 `router-svc-1y`）。
- 成品用户：发货进度（阶段 + 快递单号）。自备用户：安装命令 + 「重新生成凭证」（重复 mint 即替换，cap=1 天然维持）。
- 侧栏「账户」下新增入口，仅在用户持有路由器版订单时显示。

### 4.5 收口

- App 内 webapp 的「购买专属线路」CTA、`/purchase?product=private_node` 购买面、Account 的「专属线路」入口全部隐藏（保留路由，防旧链接 404，但不再暴露）。`/private-node` 管理页改名「我的路由器」，与 §4.4 同一套文案。
- 站点所有中文文案用「开途」，不出现 Kaitu 裸词。

## 5. 数据模型与后端

### 5.1 新增 / 变更

| 对象 | 变更 |
|---|---|
| `Plan` | `Product` 新增枚举 `router`；新增 `HardwareSKU string`（仅后台发货用）。`/api/products/router/plans` 走现有 allow-list 端点 |
| `PrivateNodePlanSpec` | 不变；`router-*` Plan 各挂一条 spec（与 `pn-dc-2t` 同规格） |
| `Order` | 新增独立列 `RouterShipping *string`（JSON `{name,phone,address}`，成品套餐真实下单时写入；`nil` = SQL NULL，**不进** `Order.Meta`——`SetOrderPayUrl` 用固定字段集重 marshal `Meta`，会静默丢弃塞进去的额外键）。硬件 SKU 不落 `Order`，由 `Plan.HardwareSKU` 直接复制进 `RouterFulfillment.HardwareSKU` |
| **新表 `router_fulfillments`** | `id, order_id(unique), user_id, sub_id(→private_node_subscriptions), hardware_sku, stage, tracking_no, carrier, gateway_device_id(→devices), shipped_at, activated_at, note, updated_by` |
| `PrivateNodeSubscription` | 不变；`ProductRouter` 订单与 `ProductPrivateNode` 共用 `createPrivateNodeSubscription` |

`stage` 枚举：`paid` → `provisioning`（线路开机中）→ `ready`（线路就绪，待烧录）→ `shipped` → `online` → `expired`。自备路径跳过 `shipped`，`ready` 即向用户展示安装命令。

### 5.2 订单分叉

`applyOrderToBuyer`（`api/logic_member.go`）在现有 `plan.Product == ProductPrivateNode` 分支旁增加 `ProductRouter`：同样建内部线路订阅并入开机队列；额外在同一事务内建 `router_fulfillments(stage=paid)`。续费商品 `router-svc-1y`：若用户已有 active/grace 线路订阅则**延期**该订阅 `ExpiresAt += 12 个月`（补上尚未实现的续费路径，须先复核 `worker_private_node_lifecycle.go` 的回收逻辑），否则按新购处理。

### 5.3 新增端点

| 端点 | 用途 |
|---|---|
| `GET /api/user/router` | 账户页聚合：fulfillment（含 `credentialMinted` / `canMintCredential`，不返回安装命令本身）+ 线路订阅 DTO（复用 `/api/user/private-nodes` 的字段）+ 路由器设备（在线判定、版本）。安装命令由网站用 `POST /api/user/gateway-credential` 返回的 `url` 在**客户端**拼成 `wget -qO- <本站>/i/k2r | sh -s '<url>'`——Center 不存凭证、不拼装安装命令 |
| `POST /app/router/fulfillments/:id/credential` | admin 代客户铸造网关凭证（复用 `api_gateway_credential` 内部函数，`RoleDevopsEditor`），返回 k2subs URL 并写 `gateway_device_id` |
| `GET /app/router/fulfillments`、`POST /app/router/fulfillments/:id/stage` | 台账列表（分页、按 stage 过滤）与阶段推进（填快递单） |
| `GET /app/private-node-subscriptions` | 线路订阅从表（分页、按状态 / 用户过滤） |
| `GET /app/router-devices` | `is_gateway=true` 设备从表 |

线路开机、自注册、计量、断流、80/95% 预警全部沿用现有实现，不改。

## 6. 后台管理

侧栏新增分组「路由器版」：

| 页面 | 每行 | 关键列 | 操作 |
|---|---|---|---|
| **路由器订单**（主台账） | 一条 `router_fulfillments` | 客户邮箱、机型、阶段、下单时间、收货信息、快递单、线路状态、本月用量、到期日、路由器在线 | 推进阶段、代铸凭证（显示 k2subs URL 供烧录）、填快递单、备注 |
| 线路订阅（从表） | 一条 `PrivateNodeSubscription` | 用户、状态、地区、用量、到期、绑定节点 IP | 查看、手工延期、重新开通、停用 |
| 路由器设备（从表） | 一台 `Device(is_gateway)` | 用户、UDID、版本、架构、最近心跳、绑定线路 | 吊销并重铸凭证 |

看板卡片（放在「路由器订单」页顶）：各阶段数量、卡在某阶段 >48h 的订单、在线路由器数、30 天内到期数。

线路开机继续走「节点运维」队列（`NodeOperation`），主台账的 `provisioning → ready` 由订阅状态变 active 自动推进，运维无需双录。

## 7. 分期

| 期 | 内容 | 门 |
|---|---|---|
| P0 数据与商品 | `Product=router`、Plan 加 `hardware_sku`、两条 Plan + spec、`router_fulfillments` 迁移、订单分叉、续费延期 | Center 集成测（真 dev MySQL） |
| P1 网站 | `/routers` 产品页、`/routers/diy`、`/purchase/router`、`/account/router`、`/purchase` 切换、文案修正、webapp 收口 | 真浏览器走查（本地 Center + web dev），brand-guard |
| P2 后台 | 三张台账 + 看板 + 代铸凭证 | admin 真浏览器走查 |
| P3 上线 | `git push origin main:website`、`make deploy-api` + migrate、打 `v*-k2r` 发新版安装脚本与二进制（CDN 仍是 0.4.4） | 首台真机：泰国刷好一台 AX6S，用真实订单走完 paid→online |

## 8. 待定与风险

- 定价已定稿：首年 $399（含路由器）、续费 $299/年；价位理由见 §3.2。采购价以泰国二手行情为准，不影响售价。
- 二手硬件的质保口径（建议「一年内故障换新」）与泰国发中国大陆的物流、清关方式，属运营决策，不在本设计。
- 6 月的记忆称续费写 `ExpiresAt` 未实现，P0 首个任务是复核并补齐；缺它第二年必断服务。
- 一账号一路由器：买第二台需第二个账号，结账页要写明。
- 真机 smoke 仍是唯一无法在桌面闭合的门（与 Plan 5b 一致）。
