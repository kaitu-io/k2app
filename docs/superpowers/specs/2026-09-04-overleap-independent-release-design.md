# Overleap 独立发布设计（第一波：网站 + 桌面）

*Status: Approved 2026-09-04*

## 背景

双品牌拆分（`2026-07-14-brand-split-design.md`）的代码已于 2026-07-23 全部合入 `main`：三层品牌注册表、feature gates、双产物构建链、纯度守卫、跨层契约门都在。但 Overleap 从未真正对外出现过。2026-09-04 核实的现状：

| 层 | 已就绪 | 缺口 |
|---|---|---|
| web | 品牌注册表 `src/lib/brands.ts`、中间件品牌门控、Stripe 购买页 `OverleapPurchaseClient`、`tests/brand-guard.test.ts` + `tests/brand-leak-ssr.test.tsx`、`<html data-brand>` 已输出 | **overleap.io 域名挂在 kaitu 的 Amplify app（`d3q8wll74rs94h`，ap-northeast-1）上，该 app 没有 `NEXT_PUBLIC_BRAND`** → 线上 overleap.io 就是开途站原样（307 到 `/zh-CN`，标题「开途 k2cc」）。首页 `page.tsx` 与 `components/home/*` 零品牌分支；`globals.css` 只有开途 Terminal Dark 一套变量；`public/overleap-icon.png`、`public/brand/overleap/*` 是开途的 K2 圆形图标，`public/overleap-og.png` 是「别人断线，你满速 / kaitu.io」海报——**二进制资产泄漏，现有文本守卫看不见**；en/ja 文案是开途叙事英译，含 "Alipay · WeChat · Credit Card"、"HK · Best Node" 等中国市场残留 |
| webapp | `src/brands/` 品牌 token 体系（palette / surface / status / semantic）、feature gates、双品牌 vitest + 纯度门 CI | `OVERLEAP_THEME` 是"故意难看以便识别串包"的紫青占位值；`src/brands/overleap/assets/*` 是纯色紫方块；布局与开途完全一致 |
| desktop | `tauri.conf.overleap.json`（identifier / updater / 图标目录）、`cfg(brand_overleap)` 编译期分叉、`-overleap` tag 独立发布链 | 窗口参数与开途相同（430×956、maxWidth 480、9:20 锁比）；webapp 的 `isDesktop` 侧栏布局从未在 Tauri 里显示过 |
| mobile | 双 flavor/scheme、独立 keystore、ASC 记录 6759199298 = "Overleap VPN"、订阅商品已定价 | `OVERLEAP_MOBILE_CI` 未翻；ASC 元数据/截图全是开途；runner 无 provisioning profile |
| 业务 | 后端品牌分区已上线 | Stripe live、`VisibleOverleap` 节点翻转、生产 Plan 行、法律实体、独立 ASC 账号均未做 |

**两个硬约束决定了品牌视觉差异化不是可选项：**

1. 线上 overleap.io 就是开途站——"独立品牌"目前只存在于代码里。
2. **Apple 4.3(a) 重复应用风险**：开途 iOS 在架（appId 6448744655），Overleap iOS 记录在**同一个** ASC 团队（ALL NATION CONNECT TECHNOLOGY PTE. LTD.）。同一开发者、两款 VPN、同一套界面只换名换色，是 4.3(a) 拒审的典型形态。至少在 iOS 上，webapp 视觉差异化是上架前提。

## 已确认的决策（2026-09-04）

| 决策点 | 结论 |
|---|---|
| 网站形态 | **同仓库双部署 + Overleap 专属页面**。不另起仓库：独立仓库要复制账号/购买/安装/支持流程并丢掉现有纯度守卫，而用户可感知的独立（域名、外观、叙事、零互提）在现架构内全能做到 |
| webapp 独立度 | **品牌识别层，不是第二套 UI**：真实 logo/图标、配色、字体、圆角、连接状态色、窗口形态。信息架构、组件树、状态机、桥接层全部共享 |
| Overleap 桌面窗口 | **宽屏桌面应用（侧栏布局）**，启用 webapp 已有的 `isDesktop` 布局 |
| 首发顺序 | **网站 + 桌面（macOS / Windows）先**；移动端第二波 |
| 视觉方向 | 沿用现有占位的紫色系（图标、token 已朝这个方向），不再开配色决策 |
| 不变量 | **开途一像素都不变**：kaitu 的主题、首页、窗口尺寸原样；每一处品牌分叉都要有"kaitu 路径未动"的测试锚定 |

## §1 Overleap 视觉识别（web + webapp 共用一套定义）

### 1.1 Token 取值

| Token | 值 | 说明 |
|---|---|---|
| 背景 `background` | `#0B0E14` | 深靛黑 |
| 卡片 `paper` / `card` | `#141926` | |
| 边框 `border` | `rgba(124, 92, 255, 0.18)` | 主色低透明 |
| 主文本 `textPrimary` / `foreground` | `#E6E8F0` | |
| 次文本 `textSecondary` / `muted-foreground` | `#9AA0B4` | |
| 主色（可操作）`primary` | main `#7C5CFF` · light `#9D85FF` · dark `#5B3FE0` | 电光紫：导航、按钮、Radio、CTA |
| 次色 `secondary` | main `#2DD4BF` · light `#5EEAD4` · dark `#14B8A6` | 薄荷青 |
| 状态 **已连接** `status.connected` | main `#2DD4BF`，gradient `linear-gradient(135deg, #2DD4BF 0%, #14B8A6 100%)`，glow `rgba(45, 212, 191, 0.35)` / glowStrong `0.5` | 与开途的绿错开 |
| 状态 **待命** `status.idle` | main `#7C5CFF`，gradient `linear-gradient(135deg, #7C5CFF 0%, #5B3FE0 100%)`，glow `rgba(124, 92, 255, 0.3)` / glowStrong `0.5` | 与开途的青错开 |
| 状态 **熄灭** `status.dormant` | border `rgba(255,255,255,0.10)`，icon `rgba(255,255,255,0.30)` | 中性，不变 |
| 语义色 `semantic` | success `#34D399`/`#6EE7B7`/`#059669` · warning `#FBBF24`/`#FCD34D`/`#D97706` · error `#F87171`/`#FCA5A5`/`#DC2626` | |
| 圆角 `radius` | 12px（web `--radius: 0.75rem`） | 开途 10px |
| 字体 `typography.fontFamily` | `Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif` | 开途首页用等宽字体的位置，Overleap 一律无衬线 |

对比原则：开途 = 绿（通了）/ 青（待命）/ 等宽字体 / 10px；Overleap = 青（通了）/ 紫（待命）/ 无衬线 / 12px。两者并排一眼可分。

### 1.2 Token 契约扩展

`webapp/src/brands/types.ts` 的 `BrandThemeTokens` 新增 **必填** `typography: { fontFamily: string }`。两个品牌都显式声明（kaitu 填 `theme.ts` 现有的 fontFamily 数组拼接值），`theme.ts` 的 `sharedThemeConfig.typography.fontFamily` 改为读 `brandTheme.typography.fontFamily`。不设 optional 兜底——与 2026-08-18 扩展 `surface`/`status` 时的原则一致：隐式默认值会让共享默认值的改动静默改变某个品牌的外观。

### 1.3 Logo / 图标

- 源文件：`webapp/brand-assets/overleap/logo.svg`（单一事实源）。v1 是几何标志：圆形 "O" 加一道从右上跃出的弧，主色紫底、白色线条。将来换专业稿只替换这个 SVG 并重跑生成脚本。
- 生成脚本：`webapp/brand-assets/overleap/generate.sh`——PNG 用 librsvg 的 `rsvg-convert`，`.icns` 用 macOS 自带 `iconutil`，`.ico` 用 ImageMagick `magick`；脚本开头检测三者缺一即报错退出。输出到：
  - `webapp/src/brands/overleap/assets/{favicon.png, icon-192x192.png, icon-512x512.png}`
  - `desktop/src-tauri/icons-overleap/{32x32.png, 64x64.png, 128x128.png, 128x128@2x.png, icon.icns, icon.ico}`（`android/` 子目录留给第二波）
  - `web/public/overleap-icon.png`、`web/public/brand/overleap/{favicon-16x16, favicon-32x32, icon-48x48, icon-96x96, icon-192x192, icon-512x512}.png`
- OG 图 `web/public/overleap-og.png`（1200×630）：Overleap 背景色 + 标志 + 英文标语 "Stays connected where others drop." + `overleap.io`。
- 生成产物进 git（与现状一致，构建不依赖本地工具）。

## §2 web 站

### 2.1 部署：真正建第二个 Amplify app

运维步骤（写入 `docs/ops/web-amplify.md`，不是代码）：

1. 在 ap-northeast-1 新建 Amplify app `overleap.io`：仓库 `kaitu-io/k2app`、分支 `website`、`AMPLIFY_MONOREPO_APP_ROOT=web`、`AMPLIFY_DIFF_DEPLOY=false`、**`NEXT_PUBLIC_BRAND=overleap`**、Sentry 四个变量（先复用 kaitu 的 DSN/项目；独立 Sentry 项目是可选后续）。
2. 从 kaitu app（`d3q8wll74rs94h`）**移除** `overleap.io` 域名关联，再在新 app 上关联 `overleap.io`（含 `www` 子域）。overleap.io 的 Route 53 zone（`Z0660232TESUJOGFHJV8`）在同一账号，Amplify 自动写 DNS 与证书验证记录。
3. 切换窗口内 overleap.io 会短暂不可达；它今天本来就是占位，可接受。
4. 验收：`curl -sI https://overleap.io/` → 307 `Location: /en-US`；`/en-US` 页 `<title>` 含 `Overleap`；`/zh-CN` → 301 `/en-US`；`https://www.overleap.io/` 同样行为；kaitu.io 行为前后不变。

### 2.2 主题：按 `data-brand` 覆盖 CSS 变量

- `web/src/app/globals.css`：在 `:root` 块之后新增 `html[data-brand="overleap"] { ... }`，覆盖**全部** `:root` 里定义的变量（含 `--chart-*`、`--sidebar-*`、`--radius`），取值按 §1.1。不允许只覆盖一部分——漏掉的变量会以开途绿渗出。
- `page.tsx` 根 div 的内联 `style={{ backgroundColor: '#050508' }}` 改为 Tailwind `bg-background`，其它页面同类内联色值一并清理（实施计划逐个列出 grep 结果）。
- `OverleapHome` 及其组件正文用 `font-sans`，只在代码块/协议名处用 `font-mono`。**不**在 `[data-brand="overleap"]` 下改写 `--font-mono` 变量——那会把 `/k2` 文档的代码块也变成无衬线；共享页面（install/support/account）里既有的 `font-mono` 用法本波不动。
- kaitu 不变：`:root` 一个值都不动，`tests/homepage-ssr.test.ts` 继续在默认（kaitu）品牌下跑。

### 2.3 首页：按品牌分流到 `OverleapHome`

`web/src/app/[locale]/page.tsx` 在服务端按 `siteBrand().id === 'overleap'` 分流到 `web/src/app/[locale]/OverleapHome.tsx`（同 `purchase/page.tsx` → `OverleapPurchaseClient` 的既有模式）；kaitu 分支保持现有 JSX 原样。`generateMetadata` 也分流（Overleap 标题 `{landing.hero.title} | Overleap`，不再带 `k2cc` 后缀）。

`OverleapHome` 结构（服务端组件，组件放 `web/src/components/home-overleap/`）：

1. **Hero**：标语 + 副标 + 两个 CTA（`/purchase` 主、`/install` 次）。不用 `K2ccPulseCanvas`（那是开途技术面的视觉），用静态渐变背景 + 简洁客户端 mock（复用 HeroSection 的 mock 结构但换品牌 token）。
2. **三步开始**：Subscribe → Download → Connect。支付方式描述只写 "Card · Apple Pay · Google Pay"（Stripe Checkout 实际支持面），不出现支付宝/微信。
3. **四张能力卡**：ECH（连接不可见）、k2cc（高丢包下稳速，引用「26% 丢包下 2–5× BBR」）、QUIC + TCP-WS 双栈自动切换、无日志 + 可自托管 `k2s`（链接到 `/k2` 协议文档）。
4. **定价卡**：年付 **$79/年**、月付 **$8.99/月（12 个月）**，与 ASC 已定价一致；按钮到 `/purchase`。价格是文案常量，不在此页读 API（`/purchase` 才读真实 Plan）。
5. **FAQ**（海外版问题集，`FAQPage` JSON-LD 同步）：whatIsK2cc、howDoesEchWork、networkThrottlingSpeed、platforms、selfHosting、pricing、trial、refund、deviceLimit、privacy、ctLog、wifiSwitch。**去掉** chinaAccess、chinaAppStore、routerSupport、androidInstall、portReuse。
6. **下载 CTA**。
7. JSON-LD：`SoftwareApplication` + `Organization` + `FAQPage`，与现有首页同构，`sameAs` 保留 `github.com/getoverleap`。

### 2.4 文案：新 namespace `landing`

- 新建 `web/messages/{7 locales}/landing.json`，并在 `messages/namespaces.ts` 的 `namespaces` 数组登记（手编，见 web/CLAUDE.md）。
- **七个 locale 都必须有文件**：`src/i18n/request.ts` 对缺失文件回落 `zh-CN`，zh-CN 也缺就抛错。zh 三份是翻译副本，开途首页不读它们。
- 品牌名一律用 `{brand}` 插值（渲染时传 `brand.displayName`），文件里不出现 "Overleap" 字面量——否则 zh 副本会触发 `brand-guard` 的"zh 文件零 overleap 词"断言。
- 文件形状：单一 wrapper `{ "landing": { hero, steps, features, pricing, faq, download } }`，与 `install.json` 同型（`t('landing.hero.title')`）。`tests/messages-parity.test.ts` 会自动覆盖新文件。
- 加一条渲染面测试 `tests/landing-overleap-ssr.test.tsx`：以 `NEXT_PUBLIC_BRAND=overleap` 渲染 `OverleapHome`，断言无原始 key 文本（防 web/CLAUDE.md 记录的"key 加错层级全测试绿、页面渲染成 key 名"陷阱），并复用 `brand-leak-ssr` 的扫描器断言零开途词。

### 2.5 en/ja 现有文案审计（Overleap 专属文件，可直接改）

en-US / en-GB / en-AU / ja 四个 locale 只有 Overleap 部署会读（kaitu `allowedLocales` 仅 zh 三种）。实施计划逐文件列出并改掉中国市场残留，已知的：

- `hero.json`：`hero.nodeInfo` "HK · Best Node"、`onboarding.step1.detail` "Alipay · WeChat · Credit Card"、`faq.items.pricing.answer` 的 "Alipay, WeChat Pay, UnionPay"、`download.hongkong`。
- `guide-parents.json`：支付宝/微信支付整段（`payment`、`wechatPay` 问答）、"domestic apps like Baidu and WeChat"。
- `install.json`、`purchase.json`、`discovery.json`（隐私/条款）：grep `Alipay|WeChat|UnionPay|Baidu|Hong Kong|GFW|China` 逐条判断，协议技术描述里的 "GFW" 可保留，支付与地域默认值必须改。
- 守卫：`tests/messages-integrity.test.ts` 加一条断言——en/ja 文件禁用 `Alipay|WeChat Pay|UnionPay|支付宝|微信` 词（支付渠道是最硬的品牌错位信号）。

### 2.6 资产替换与二进制守卫

- 按 §1.3 替换 `public/overleap-icon.png`、`public/brand/overleap/*`、`public/overleap-og.png`。
- 新增 `tests/brand-assets.test.ts`：对 `KAITU.logoPath` / `OVERLEAP.logoPath`、`ogImagePath`、两套 favicon 目录逐文件算 SHA-256，断言**跨品牌无任何一对相同**，并断言每个 Overleap 资产文件存在且非空。这条守卫把"把开途图标复制过去当 Overleap 用"变成红灯。

### 2.7 其余页面

install / support / account / login / purchase / k2 文档共享组件，跟随 CSS 变量换肤，不动结构。`/purchase` 已是 `OverleapPurchaseClient`（Stripe），只需视觉核对。`Header` / `Footer` 已读注册表。

## §3 webapp 识别层

1. `webapp/src/brands/overleap/theme.ts`：`OVERLEAP_THEME` 按 §1.1 填正式值；加 `typography`。`kaitu/theme.ts` 只加 `typography`（现值），其它不动。`theme.brand.test.ts` 补断言：两个品牌的 `status.connected.main`、`dark.primary.main`、`typography.fontFamily` 两两不同。
2. `webapp/src/brands/overleap/assets/` 三个 PNG 由 §1.3 脚本生成。`k2-brand` vite 插件已负责拷贝与 `<title>` 改写，不动。
3. 宽屏布局打磨（只改 `isDesktop === true` 路径；受影响面 = Overleap 桌面、iPad、路由器模式；开途桌面窗口 maxWidth 480 永远走 mobile 路径，不受影响）：
   - `Dashboard.tsx` 节点列表容器：`isDesktop` 时 `maxWidth: 760`、`margin: 0 auto`，行内元素不变。
   - `SideNavigation.tsx` Logo 区：用 `/icon-192x192.png`（品牌稳定路径）+ `brandConfig.productName` 文字，去掉纯色方块依赖。
   - 实测基线：2026-09-04 以 `K2_BRAND=overleap` 在 1000×680 视口下，侧栏、连接按钮、节点列表、底部高级设置栏均正常渲染；本项只是收口。
4. `layout.store.ts` 断点（短边 ≥ 600 → desktop）**不改**；靠 §4 的最小窗口尺寸保证 Overleap 桌面永远落在 desktop 侧。
5. 品牌名 / 标语 / 支持邮箱已在注册表，不动。

## §4 desktop 窗口

### 4.1 `desktop/src-tauri/tauri.conf.overleap.json`

```json
"width": 1040, "height": 700,
"minWidth": 880, "minHeight": 620,
"maximizable": true, "resizable": true
```

删除 `maxWidth`。其余键（identifier、updater、图标、签名）不动。`tauri.conf.json`（kaitu）一个键都不动。

### 4.2 `desktop/src-tauri/src/window.rs`

用现有 `cfg(brand_overleap)` 编译期分叉（与 `channel.rs` / `updater.rs` / `main.rs` 同一手法）：

- **kaitu 路径**：现有常量与 `calculate_window_size` / `calculate_dynamic_min_height` 逐字不动，现有测试原样保留。
- **overleap 路径**：常量 `DEFAULT_WIDTH=1040`、`DEFAULT_HEIGHT=700`、`MIN_WIDTH=880`、`MIN_HEIGHT=620`、`MAX_AREA_RATIO=0.85`。尺寸计算：默认尺寸按工作区宽高各 85% 夹紧（`min(DEFAULT, usable*0.85)`），再夹到不低于 MIN；不锁比例。动态 min_size：宽固定 880，高 `min(620, usable_h).max(560)`（560 是布局硬地板，须与 `minHeight` 的对应关系在测试里锚定，同 kaitu 的 `MIN_HEIGHT_FLOOR` 手法）。`adjust_window_size` 的日志文案去掉 "portrait orientation" 字样（两品牌共用一句中性文案）。
- 测试：overleap 常量与计算函数各配 `#[cfg(brand_overleap)]` 测试模块（1080p、1440p、小屏 700 高、极小屏 500 高四个用例），验证清单必须包含 `K2_BRAND=overleap cargo test` 与默认 `cargo test` 各跑一次。

### 4.3 与 webapp 断点的耦合

Overleap 最小窗口 880×620 → 短边 620 ≥ 600 → webapp 永远 desktop 布局；动态 min 高最低 560 只在工作区高度 < 620 的极小屏出现，此时短边 560 会落到 mobile 布局——可接受的退化（仍可用），不做特殊处理。

## §5 发布链（第一波）

1. **web**：`git push origin main:website`，两个 Amplify app 各自构建。验收见 §2.1；另外 kaitu.io 首页发布前后截图逐像素对照（同一视口、同一 locale）。
2. **desktop**：版本号以发布时根 `package.json` 为准（写作时 0.4.9），tag 形如 `v0.4.10-overleap`，走 `release-desktop.yml` 的独立触发（`-overleap` 后缀 → MAC_OVERLEAP + WIN_OVERLEAP）。**先用 `workflow_dispatch` 的 `dry_run=true` 跑一次**，确认签名、纯度门、产物命名 `Overleap_{VERSION}_{ARCH}.{EXT}` 全过，再正式打 tag。
3. **S3 / CDN 权限**：`overleap/web/` 前缀已被 `publish-web-ota` 成功写入（2026-09-01 有 0.4.9 产物），`overleap/desktop/` 前缀从未写过——dry-run 之外用 CI 同一 IAM 身份做一次 `aws s3 cp` 探针（写一个 1 字节临时对象再删），确认后再打 tag。
4. **下载链接**：`web/src/lib/brands.ts` 里 Overleap 的 `cdn.desktopBases` 指向 `d13jc1jqzlg4yt.cloudfront.net/overleap/desktop`；发布后对 `/install` 页每个下载 URL curl 验 200。`dl.overleap.io` CNAME 不是本波前置。
5. **Web OTA**：`publish-web-ota.yml` 已恒双品牌，无需改动。

## §6 上线前的非代码检查单（第一波必须齐）

全部是运维/商务动作，不在代码范围，但**任何一项未打钩，Overleap 都不能对外收钱**：

- [ ] Stripe：live key + 生产 webhook secret 进生产 `config.yml`；Dashboard 注册 `/webhook/stripe` 事件；Billing Portal **只开取消，不开换套餐**（换套餐绕过 `validatePurchase` 会静默错档）；真卡小额验证一笔并退款。
- [ ] admin 建生产 Plan 行：`brand=overleap`、`stripe_price_id`（年付 + 月付两行）、`apple_product_id=io.overleap.sub.basic.1y`（第二波用，可先填）。
- [ ] 节点：选一批翻 `VisibleOverleap=true`，**排除** SNI 伪装为 `www.<省份>.people.cn` 的节点。不翻 = 上线当天 Overleap 零可见节点。
- [ ] `www.overleap.io` → apex 跳转确认（Amplify 域名关联自带）。
- [ ] `support@overleap.io` 的 SES 发件域验证（DKIM/SPF），否则注册验证码发不出。
- [ ] Slack `alert` 频道确认，否则三道支付哨兵退化为纯日志。
- [ ] `web/public/legal/*.md` Overleap 版文案（隐私 / 条款，署名 Overleap LLC）——内容创作，需要人写。
- [ ] GA4 measurement id、Chatwoot token（`brands.ts` 里为空 = 功能关闭，可以先空着上线）。

## §7 验证

### 7.1 web

- `cd web && yarn test`（含新增 `landing-overleap-ssr`、`brand-assets`、`messages-integrity` 新断言）；`yarn build` 与 `yarn build:overleap` 各一次。
- 真实浏览器：`yarn dev:overleap` 过首页 / purchase / install / support / account 五页，en-US 与 ja 各一遍，检查无原始 key、无开途词、无中国支付渠道；`yarn dev` 过 kaitu 首页与前一版对照。
- 守卫反向验证（本项目既定规矩）：临时把 `public/overleap-icon.png` 换回开途图标，确认 `brand-assets.test.ts` 变红；临时在 `landing.json` en-US 写入 "Alipay"，确认 `messages-integrity` 变红；再恢复。

### 7.2 webapp

- `npx vitest run` 与 `K2_BRAND=overleap npx vitest run` 各一次全绿；`yarn build` 两个品牌 + `scripts/check-brand-purity.sh` 各一次。
- 浏览器截图：`K2_BRAND=overleap` 下 1040×700、880×620 两个视口，确认 desktop 布局、节点列表居中、侧栏标志；430×956 视口确认 mobile 布局未受影响。
- kaitu 默认品牌 430×956 视口截图与前一版对照。

### 7.3 desktop

- `cargo test` 与 `K2_BRAND=overleap cargo test`（在 `desktop/src-tauri`）各一次。
- 本机 `BRAND=overleap make build-macos` 实装：窗口默认 1040×700、可缩到 880×620、可最大化、标题 "Overleap"、图标为新标志；纯度门通过。
- 本机 `make build-macos`（kaitu）实装：窗口仍为竖版且尺寸不变。

### 7.4 信心口径

按既定框架分两个数：**代码信心**目标 9/10（差的一分仍是 staging 双 host 冒烟，与 7-23 合并时同一缺口）；**业务信心**只取决于 §6 打钩数量，与代码无关，不混着报。

## 第二波预览（移动端，不在本 spec 范围）

ASC 6759199298 的 4.0 版本元数据/截图全换、年龄分级、隐私标签、服务器通知 URL；`icons-overleap/android/` 与 iOS AppIcon 从 §1.3 的 SVG 源生成；`OVERLEAP_MOBILE_CI` 翻转 + runner provisioning profiles；Google Play 新账号；`IosMembershipPanel` 的 `stripe_portal` 分派修复；真机 smoke。**强烈建议为 Overleap LLC 开独立 Apple 开发者账号**：这是消解 4.3 重复应用风险、并让 App Store 卖家名不显示 ALL NATION CONNECT 的根本办法，前置是法律实体落地。

## 文档同步（列入验收）

- `docs/ops/web-amplify.md`：两个 Amplify app 的真实 ID、域名归属、`NEXT_PUBLIC_BRAND` 设置，替换"两个 app"的空口径。
- `web/CLAUDE.md` Brand 段：首页按品牌分流的位置、`landing` namespace、二进制资产守卫。
- `webapp/CLAUDE.md` Brand 段：`typography` token、`brand-assets/overleap/logo.svg` 是资产单一来源。
- `desktop/CLAUDE.md`：Overleap 窗口形态与 `window.rs` 的品牌分叉。
