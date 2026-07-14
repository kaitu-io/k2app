# 双品牌拆分设计：开途 / Overleap

*Status: Approved 2026-07-14（rev 2：后端品牌工作合并进 Phase 1 一次做完 + 最佳实践补充）*

## 背景与目标

把当前单一产品真正拆分为两个独立品牌：**开途/Kaitu**（中国市场）和 **Overleap**（海外市场）。目标排序：品牌独立 > 弹性大 > 维护成本低。

选定方案：**单代码库品牌参数化**——一套代码出两个品牌产物，品牌是数据（brand 注册表 + feature gates），不是分支。web 层同代码库但**双独立部署**。

本设计取代 `docs/marketing/brand-naming-strategy.md` 中「跨语境用 Kaitu by Overleap 衔接」的规则：**两个品牌从此互不感知**（零互跳、零互链、各自 SEO）。该文档需同步更新。

## 已确认的关键决策

| 决策点 | 结论 |
|---|---|
| 用户账号 | **完全独立的用户池**。同一邮箱在两边是两个账号，互不相通 |
| 数据隔离 | **同一 API 进程 + brand 字段分区**（共享服务器降成本） |
| 定价/支付 | 两品牌完全独立。开途 = WordGate + StoreKit IAP（不动）；Overleap = Stripe（官网）+ StoreKit IAP + Play Billing |
| 节点池 | **共享物理节点，按品牌可见性标签分配** |
| UI 独立度 | 同一代码库 + 品牌主题化 + feature gates 功能分叉；营销页面 per-brand 独立设计 |
| web 拆分 | 同代码库，**双独立部署**（两个 Amplify app，各自域名/发布节奏，互不感知） |
| 首发平台 | 网站 + 桌面（macOS/Windows）+ iOS 全球区 App Store + Google Play |
| 存量用户 | **全部归开途**（迁移时全量 `brand='kaitu'`），Overleap 从零开始 |
| 法律实体 | **Overleap LLC**（Stripe 收单主体）；App 发布沿用现有 Apple/Google 账号 |
| 客服 | support@overleap.io（邮箱已就绪） |

## 1. API：品牌分区（Phase 1 一次做完整个后端）

**范围原则**：后端数据层 + API 层的全部 brand 改造集中在 Phase 1 一次性完成——包括 users/配置表 schema、节点可见性字段、支付品牌路由骨架、邮件/文案参数化。Phase 1 交付后 API 即是完整的双品牌形态（Overleap 侧数据为空但功能完备），后续 Phase 只做前端接入、渠道集成和运营铺量，不再回头改后端。

### 品牌解析

新增 `api/brand.go` 品牌注册表（仿 `web/src/lib/brands.ts` 模型）：

- `Brand = kaitu | overleap`
- 每品牌配置：host 集合、CORS 白名单、OTT 重定向白名单、AppConfig baseURL、发件人地址、邮件签名、支付渠道集合。
- 中间件按 `请求 host → X-K2-Brand header（客户端 build 时烘焙）→ 默认 kaitu` 解析出 brand 放进 request context。

### 数据模型

- `users` 加 `brand` 列（`default 'kaitu'`，索引）。brand 是用户出生属性，注册时确定，终身不变。
- 面向用户的配置表同样加 `brand` 列：`plans`（定价体系天然分开——Overleap plans 是独立美元价格行）、`campaigns`、`announcements`、`license_key_batches`、`edm_templates`（EDM 模板按品牌隔离，Overleap 走英文模板）。
- `nodes`/`tunnels` 加品牌可见性字段（`brands` 集合，默认 `['kaitu']`）——**schema 与过滤逻辑属 Phase 1**，节点打标铺量是后续运营动作。
- 订单/订阅/设备/工单通过 user 继承品牌，不重复存；查询时 join user 取 brand。
- 迁移：所有存量行 `brand='kaitu'`，零迁移风险。

### 强制隔离

- 登录/注册校验 `user.brand == request.brand`，不匹配返回 403（开途账号登不进 Overleap，反之亦然）。
- 数据访问层提供 `ScopeBrand(ctx)` GORM helper，所有面向用户的列表查询必须经过它。
- 配套跨品牌泄漏测试：overleap 用户请求 kaitu 资源断言 404/403，反向同理。
- **Admin 边界明确化**：admin/`manager` 路由是唯一合法的跨品牌视角——不走 ScopeBrand，改为显式 `brand` 筛选参数（默认显示全部并标注品牌列）。哪些路由属于 admin 白名单在实施计划里逐条列出，白名单之外的路由缺 ScopeBrand 视为 bug。
- **MCP/运营工具跟随**：`mcp/`（k2 用户端）按客户端品牌传 `X-K2-Brand`；`tools/kaitu-center`（admin 端）的 `lookup_user`/工单/统计类工具输出加 brand 字段——工单回复语言与品牌词由 user.brand 决定（开途中文、Overleap 英文；中文语境禁用 Kaitu 裸词的既定规则继续生效）。

### 现有硬编码点参数化（摸底已定位）

- `api/middleware.go` `CORSMiddleware()` 硬编码 kaitu.io → 按 brand 取白名单。
- `api/api_auth_ott.go` OTT 重定向只允许 `*.kaitu.io` → 按 brand 取白名单（否则 overleap.io 登录被拒）。
- `api/api_app_config.go` / `api/logic_config.go` 默认 baseURL → 按 brand 返回。
- 邮件模板（Go 字符串常量，含「开途团队」签名 + kaitu.io 链接，如 `api/email_templates_private_node.go`）→ 品牌变量化；Overleap 需要一套英文模板 + `@overleap.io` 发件域（SES 域名验证是基建前置）。
- 其他散点：`worker_retailer_followup.go`、`logic_approval.go`、`api_share_link.go`、`api_ticket.go`、`api_member.go`。

## 2. 支付：按品牌路由

- **品牌路由骨架属 Phase 1**：支付渠道注册表按 `user.brand` 分发（渠道集合挂在品牌注册表上），WordGate 在 Phase 1 即被锁定为 kaitu-only。Stripe/Play Billing 的具体渠道接入在 Phase 6，接入时只是往 overleap 的渠道集合里填实现，不改路由层。
- 开途用户永远看不到 Stripe，Overleap 用户永远碰不到 WordGate。
- **开途**：不动（WordGate + 现有 StoreKit IAP 链路）。
- **Overleap** 三渠道，统一进现有 neutral `subscriptions` 表（复用 subscriptionAffordance 防重叠机制）：
  - 官网 → **Stripe Checkout + webhook**（新增 `api/payment_stripe.go`，美元计价，Overleap LLC 主体的 Stripe 账号）
  - iOS → StoreKit IAP，复用现有验证链路，新 bundle id + 新商品 id
  - Google Play → **Play Billing** + Google Play Developer API 服务端验证（纯新增）
- 说明：Stripe 不是 IAP。苹果强制 App 内数字订阅走 StoreKit（Play 同理走 Play Billing）；Stripe 只用于官网购买。

## 3. web：同代码库，双独立部署

- build 时注入 `NEXT_PUBLIC_BRAND=kaitu|overleap`；两个 Amplify app 各自部署、各自发布节奏、互不感知。
- middleware 简化：**删除现有跨域 301 互跳**。单品牌 build 内非本品牌 locale 直接 404 或落到本品牌默认 locale。`web/src/lib/brands.ts` 保留为单一品牌事实源。
- Overleap 站的首页/落地页/定价页用独立设计的页面组件（现有品牌机制已支持 per-brand 页面）；共享的只有基础设施（账号/购买/自助流程组件）。
- Admin 后台留在 kaitu 部署线（内部工具），界面加品牌筛选维度。
- 收尾 `web/src/` 残留的 ~137 处 `Kaitu/开途` 字面量。
- Payload content 层已有 `showOnKaitu`/`showOnOverleap` 可见性，沿用。

## 4. webapp：品牌配置 + feature gates

- 新建 `webapp/src/brand/`，build-time `VITE_BRAND=kaitu|overleap` 选择品牌配置：displayName、logo、MUI 色板（现有 `theme.ts` 是纯色值，直接参数化）、兜底 baseURL（消灭散落的 `|| 'https://kaitu.io'`：`useAppLinks.ts`、`useInviteCodeActions.ts`、`ForceUpgradeDialog.tsx` 等）、支持邮箱、允许的 locale 集。
- **feature gates 挂品牌配置**：`invite`（邀请码）、`retailer`（代理商）、`stripeCheckout`、`antiblockRelay`（呈现层）等布尔开关。Overleap 关邀请/代理、开 Stripe；开途反之。交互结构共享，功能面和视觉分叉。
- i18n：品牌名从文案抽出，改 `{{brand}}` 插值 + 每品牌 override 文件（语气差异大的文案整条覆盖）。
- 散落品牌字面量收敛：`LoginDialog.tsx`、`Account.tsx`、`BridgeTest.tsx`、`ServiceError.tsx`、`ForceUpgradeDialog.tsx`。
- Overleap 默认 en-US；locale 集可配（界面语言 ≠ 品牌归属）。

## 5. desktop：双 profile 双产物

- 新增 `tauri.conf.overleap.json`（Tauri `--config` 合并覆盖）：productName `Overleap`、identifier `io.overleap.desktop`、updater endpoints 指向 `/overleap/desktop/` CDN 路径、独立图标。
- `BRAND=overleap make build-macos` 一条命令切品牌。
- Windows 签名走现有 SimplySign 线（证书主体名会暴露现有实体——已接受，见「已接受的品牌泄漏点」）。

## 6. mobile：flavor/scheme 双上架线

- **Android**：product flavors `kaitu`（`io.kaitu`）/ `overleap`（`io.overleap`），per-flavor 资源（strings.xml、图标），Overleap 独立 keystore。**顺手修既有漂移**：当前 `strings.xml` 的 `app_name` 写着 "Overleap.io"，kaitu flavor 改回「开途」。
- **iOS**：双 scheme + xcconfig。Overleap 用新 bundle id `io.overleap`（+ `.ThePacketTunnel` 扩展），独立显示名/图标，新建 App Store listing（不复用开途评价）。
- 开途现有遗留 ANC bundle id（`com.allnationconnect.anc.wgios`）迁移是**独立轨道**，不混进本次拆分。
- `mobile/capacitor.config.ts` 按环境变量参数化 appId/appName。
- App 发布沿用现有 Apple team / 新开 Google Play 账号线（用现有主体）。

## 7. 节点池：共享 + 品牌可见性标签

- schema 与 `/api/tunnels`、`/api/subs` 的按 `user.brand` 过滤逻辑**在 Phase 1 完成**（recommendScore/配额各自照常计算）；本节的后续 Phase 只剩节点打标与运营铺量。
- 同一物理节点可两边上架；SNI 伪装为 `www.<省份>.people.cn` 的节点**默认不给 Overleap**（暴露中国属性）——每节点标签由运营决定。
- Overleap 用户不在墙内，抗封锁中继非必需；k2subs 链路照常，只是节点列表内容不同。

## 8. 构建/发布链：BRAND 参数化

- Makefile 加 `BRAND` 变量（默认 `kaitu`）：产物前缀 `Kaitu_*` / `Overleap_*`、CDN 路径段 `/kaitu/` / `/overleap/`、latest.json 双份。
- workflows 加 brand 维度：`release-desktop.yml`、`build-mobile.yml` 先行；`release-k2s.yml`/`release-openwrt.yml` 不涉品牌不动。
- k2 子模块 ~8 处 kaitu.io URL（`cmd/k2r/upgrade.go`、`config/config.go`、`gateway/gateway.go` 等）通过 ldflags/配置注入品牌 entry URL，不 fork。

## 9. 实施顺序（每阶段独立可交付）

**Phase 1 = 后端一次做完**：数据层 + API 层的全部 brand 改造集中一个 Phase 交付，后续 Phase 不再改后端。

| Phase | 内容 | 依赖 |
|---|---|---|
| 0 | 基建前置：Overleap LLC 的 Stripe 账号、iOS listing、Play 上架准备、overleap.io SES 发件域、CDN `/overleap/` 路径 | 商务/运营动作，与开发并行 |
| 1 | **后端品牌地基（一次做完）**：品牌注册表 + 中间件；全部 schema（users/plans/campaigns/announcements/license_key_batches/edm_templates + nodes/tunnels 可见性）；ScopeBrand + 登录 403 强制；CORS/OTT/AppConfig/邮件/分享链接/worker 文案参数化；/api/tunnels + /api/subs 品牌过滤；支付品牌路由骨架（WordGate 锁 kaitu）；admin 跨品牌白名单 + brand 筛选；跨品牌泄漏测试矩阵（真 MySQL） | 无 |
| 2 | web 双部署拆分 + 字面量收尾 | Phase 1 |
| 3 | webapp 品牌配置 + feature gates | Phase 1 |
| 4 | desktop 双产物 + 发布链参数化 | Phase 3 |
| 5 | mobile flavors + 双上架线 | Phase 3 |
| 6 | 支付渠道接入：Stripe Checkout/webhook 先行（官网可买），IAP 新商品 + Play Billing 随 App 上架（路由骨架已在 Phase 1） | Phase 1 + Phase 0 |
| 7 | 节点打标 + 运营铺量（机制已在 Phase 1） | Phase 1 |

### Phase 1 部署与回滚策略

- **迁移纯增量、向后兼容**：只加列（带 default）不改不删，老代码跑在新 schema 上无感。部署顺序：手动迁移 SQL → 部署 API（沿用本仓库手动迁移惯例，不依赖 AutoMigrate 上生产）。
- **回滚 = 只回滚代码**：列留在库里无害；不做「新键→老键」兼容桥（既定反防御性迁移原则）。
- **老客户端零破坏**：不发 `X-K2-Brand` 的存量客户端按 host 兜底、host 不识别默认 kaitu，行为与拆分前完全一致——这是 Phase 1 上线的硬验收项。
- **上线验收**：staging 用双 host（kaitu/overleap 域名）各注册一个账号跑通 注册→登录→取节点→下单 全链路 + 跨品牌互访 403；此为进入 Phase 2+ 的门槛。

### 发布信心分级（沿用既定框架）

- Phase 1（后端）：真 MySQL 隔离矩阵 + staging 双 host 冒烟后可到 9+。
- Phase 4/5（客户端产物）：desk 全绿封顶 6-7，**每个品牌产物都要独立真机 smoke**（Overleap 包不是「开途包换皮，测一个等于测俩」——updater 源、entry URL、IAP 商品都不同）。

## 9.5 观测与统计（Phase 1 一并带上）

- 核心业务统计（注册/订单/活跃/收入，`user_statistics`/`order_statistics` 及 admin 报表）加 brand 维度——否则 Overleap 上线初期的数据会淹没在开途基数里。
- API 结构化日志加 `brand` 字段，方便按品牌 triage。
- 支付 webhook 品牌错配（如 kaitu 用户出现在 Stripe webhook）拒绝 + 告警，作为隔离性的线上哨兵。

## 已接受的品牌泄漏点（已拍板，不再讨论）

- Apple 卖家名 / Google Play 开发者名沿用现有账号 → 商店页会显示现有主体名。
- Windows 签名证书主体名为现有实体。
- 以上均为成本/速度权衡下的主动接受；未来若要彻底隔离，可在 Overleap LLC 名下另起证书与开发者账号，属后续独立决策。

## 错误处理与测试要点

- **跨品牌泄漏是本设计最大风险**：Phase 1 必须带隔离测试矩阵（登录 403、列表查询零泄漏、OTT 白名单、CORS、tunnels/subs 过滤）后才能进入后续 Phase。测试跑真 MySQL（仓库既定惯例）。
- 支付 webhook（Stripe/Play）验签与幂等；品牌错配（kaitu 用户收到 stripe webhook）必须拒绝并告警。
- 客户端 `X-K2-Brand` header 缺失时按 host 兜底、host 不识别时默认 kaitu——保证老客户端零破坏。
- **CI 品牌串包守卫**：发布链双产物各自跑 `scripts/test_build.sh` 类校验，并对 Overleap 产物 grep「开途/kaitu.io」、对开途产物 grep「overleap.io」断言为零（i18n 文案与 updater/entry URL 是重灾区）。
- Go `json.Marshal` snake_case → 客户端 camelCase 的既定桥接规则同样适用于新增 brand 字段。

## 文档同步（随实施更新，列入各 Phase 验收）

- `docs/marketing/brand-naming-strategy.md`：删除「跨语境 Kaitu by Overleap 衔接」规则，改为两品牌完全隔离。
- 根 `CLAUDE.md` Cross-Layer Conventions 加「brand 参数化」条目（BRAND/VITE_BRAND/NEXT_PUBLIC_BRAND、X-K2-Brand header 契约）；`api/CLAUDE.md`、`webapp/CLAUDE.md`、`web/CLAUDE.md` 各自补品牌段。
- `.agents/product-marketing-context.md` Glossary 与品牌事实同步。
