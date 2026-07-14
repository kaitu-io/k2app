# 双品牌拆分设计：开途 / Overleap

*Status: Approved 2026-07-14*

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

## 1. API：品牌分区（最痛层，先做）

### 品牌解析

新增 `api/brand.go` 品牌注册表（仿 `web/src/lib/brands.ts` 模型）：

- `Brand = kaitu | overleap`
- 每品牌配置：host 集合、CORS 白名单、OTT 重定向白名单、AppConfig baseURL、发件人地址、邮件签名、支付渠道集合。
- 中间件按 `请求 host → X-K2-Brand header（客户端 build 时烘焙）→ 默认 kaitu` 解析出 brand 放进 request context。

### 数据模型

- `users` 加 `brand` 列（`default 'kaitu'`，索引）。brand 是用户出生属性，注册时确定，终身不变。
- 面向用户的配置表同样加 `brand` 列：`plans`（定价体系天然分开——Overleap plans 是独立美元价格行）、`campaigns`、`announcements`、`license_key_batches`。
- 订单/订阅/设备通过 user 继承品牌，不重复存。
- 迁移：所有存量行 `brand='kaitu'`，零迁移风险。

### 强制隔离

- 登录/注册校验 `user.brand == request.brand`，不匹配返回 403（开途账号登不进 Overleap，反之亦然）。
- 数据访问层提供 `ScopeBrand(ctx)` GORM helper，所有面向用户的列表查询必须经过它。
- 配套跨品牌泄漏测试：overleap 用户请求 kaitu 资源断言 404/403，反向同理。

### 现有硬编码点参数化（摸底已定位）

- `api/middleware.go` `CORSMiddleware()` 硬编码 kaitu.io → 按 brand 取白名单。
- `api/api_auth_ott.go` OTT 重定向只允许 `*.kaitu.io` → 按 brand 取白名单（否则 overleap.io 登录被拒）。
- `api/api_app_config.go` / `api/logic_config.go` 默认 baseURL → 按 brand 返回。
- 邮件模板（Go 字符串常量，含「开途团队」签名 + kaitu.io 链接，如 `api/email_templates_private_node.go`）→ 品牌变量化；Overleap 需要一套英文模板 + `@overleap.io` 发件域（SES 域名验证是基建前置）。
- 其他散点：`worker_retailer_followup.go`、`logic_approval.go`、`api_share_link.go`、`api_ticket.go`、`api_member.go`。

## 2. 支付：按品牌路由

- 支付路由按 `user.brand` 分发：开途用户永远看不到 Stripe，Overleap 用户永远碰不到 WordGate。
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

- `nodes`/`tunnels` 加品牌可见性字段（如 `brands` 集合，默认 `['kaitu']`）。
- `/api/tunnels` 与 `/api/subs` 按 `user.brand` 过滤（recommendScore/配额各自照常计算）。
- 同一物理节点可两边上架；SNI 伪装为 `www.<省份>.people.cn` 的节点**默认不给 Overleap**（暴露中国属性）——每节点标签由运营决定。
- Overleap 用户不在墙内，抗封锁中继非必需；k2subs 链路照常，只是节点列表内容不同。

## 8. 构建/发布链：BRAND 参数化

- Makefile 加 `BRAND` 变量（默认 `kaitu`）：产物前缀 `Kaitu_*` / `Overleap_*`、CDN 路径段 `/kaitu/` / `/overleap/`、latest.json 双份。
- workflows 加 brand 维度：`release-desktop.yml`、`build-mobile.yml` 先行；`release-k2s.yml`/`release-openwrt.yml` 不涉品牌不动。
- k2 子模块 ~8 处 kaitu.io URL（`cmd/k2r/upgrade.go`、`config/config.go`、`gateway/gateway.go` 等）通过 ldflags/配置注入品牌 entry URL，不 fork。

## 9. 实施顺序（每阶段独立可交付）

| Phase | 内容 | 依赖 |
|---|---|---|
| 0 | 基建前置：Overleap LLC 的 Stripe 账号、iOS listing、Play 上架准备、overleap.io SES 发件域、CDN `/overleap/` 路径 | 商务/运营动作，与开发并行 |
| 1 | API 品牌地基：schema + 中间件 + ScopeBrand + 隔离测试（存量全标 kaitu） | 无 |
| 2 | web 双部署拆分 + 字面量收尾 | Phase 1 |
| 3 | webapp 品牌配置 + feature gates | Phase 1 |
| 4 | desktop 双产物 + 发布链参数化 | Phase 3 |
| 5 | mobile flavors + 双上架线 | Phase 3 |
| 6 | 支付：Stripe 先行（官网可买），IAP/Play Billing 随 App 上架 | Phase 1 + Phase 0 |
| 7 | 节点标签 + 运营铺量 | Phase 1 |

## 已接受的品牌泄漏点（已拍板，不再讨论）

- Apple 卖家名 / Google Play 开发者名沿用现有账号 → 商店页会显示现有主体名。
- Windows 签名证书主体名为现有实体。
- 以上均为成本/速度权衡下的主动接受；未来若要彻底隔离，可在 Overleap LLC 名下另起证书与开发者账号，属后续独立决策。

## 错误处理与测试要点

- **跨品牌泄漏是本设计最大风险**：Phase 1 必须带隔离测试矩阵（登录 403、列表查询零泄漏、OTT 白名单、CORS）后才能进入后续 Phase。
- 支付 webhook（Stripe/Play）验签与幂等；品牌错配（kaitu 用户收到 stripe webhook）必须拒绝并告警。
- 客户端 `X-K2-Brand` header 缺失时按 host 兜底、host 不识别时默认 kaitu——保证老客户端零破坏。
- 发布链双产物需在 CI 各自跑 `scripts/test_build.sh` 类校验，防止品牌串包（Overleap 包里出现开途文案）。
