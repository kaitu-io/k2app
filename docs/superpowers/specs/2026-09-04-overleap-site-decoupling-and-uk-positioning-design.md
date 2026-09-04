# Overleap 站点解耦 + 英国市场定位 — 设计 spec

日期：2026-09-04 · 基线：`main` @ 777c228a（第一波 `2026-09-04-overleap-independent-release-design.md` 已上线）· 分支：`feat/overleap-site-decoupling`

## 0. 背景与目标

第一波把 overleap.io 拆成了独立 Amplify 部署，但 `web/` 里只有首页、`/purchase`、`/account` 三处按品牌分流；其余页面在 overleap.io 上都是开途页面套英文文案（`/support` 是家长指南英译、`/discovery` 是中国站点导航、`/install` 带中文截图、`/g` `/s` `/survey` `/opensource` 是开途渠道页），顶栏 "Why Overleap" 指向不存在的 `/#testimonials`，页脚三个链接指向空的 `/guides`，首页标价 `$79` 而购买页显示 `€`。分流靠 `if (siteBrand().id === 'overleap')`，每新增页面都要记得加门——漏门即泄漏，这已是第三次同型问题，按仓库规则改结构。

用户决策（2026-09-04）：
1. Overleap 面向英国等市场，主卖点**隐私优先**（对标 Proton / Mullvad 受众），与开途的抗封锁叙事完全分开。
2. 默认语言改为 **en-GB**（英式拼写作母版，en-US / en-AU 为变体，ja 翻译）。
3. 币种由系统基础决定（见 §4）；Overleap 直接用 Stripe 完成支付。
4. 两站内容后续完全不同——解耦必须让 Overleap 新增页面/内容时**不碰开途代码**，反之亦然。

**不变量**（沿用第一波）：kaitu.io 零视觉与行为变化；两站互不感知；品牌字面量只进注册表；Stripe key 永不入 git / 报告 / commit message。

## 1. 页面树按品牌物理分离（`pageExtensions`）

### 1.1 机制

`next.config.ts` 按构建期品牌设置：

```ts
const brand = parseBrandId(process.env.NEXT_PUBLIC_BRAND); // 'kaitu' | 'overleap'
pageExtensions: [`${brand}.tsx`, `${brand}.ts`, 'tsx', 'ts'],
```

- `page.tsx` / `layout.tsx`：两品牌共用。
- `page.kaitu.tsx`：只进开途构建；Overleap 构建里该路径**不存在**（Next 原生 404，无需中间件门）。
- `page.overleap.tsx`：只进 Overleap 构建。
- 同一目录**不得**同时存在 `page.tsx` 和 `page.<brand>.tsx`（同路径双页面会让该品牌构建失败——这是想要的：结构性冲突在构建期爆）。

已验证（2026-09-04 spike，Next 15.5.23）：`next build`（webpack）与 `next dev --turbopack` 都按此解析；`middleware.ts`、`sitemap.ts`、`robots.ts` 用普通扩展名不受影响。

### 1.2 分配表

| 路径 | 归属 | 动作 |
|---|---|---|
| `[locale]/page.tsx` | 分裂 | 现有开途 JSX → `page.kaitu.tsx`；`OverleapHome` 内联为 `page.overleap.tsx`，删除 `if overleap` 分流 |
| `[locale]/install/page.tsx` | 分裂 | 开途原页 → `page.kaitu.tsx`；新 `page.overleap.tsx`（§3.3） |
| `[locale]/support/page.tsx` | 分裂 | 家长指南 → `page.kaitu.tsx`；新 Help 页 → `page.overleap.tsx`（§3.4） |
| `[locale]/purchase/page.tsx` | 分裂 | `page.kaitu.tsx`（WordGate）/ `page.overleap.tsx`（Stripe），删除分流 if |
| `[locale]/account/page.tsx` | 分裂 | 同上，两个 Client 各归其页 |
| `discovery` `opensource` `routers` `retailer/rules` `releases` `changelog` `g` `g/[code]` `s/[code]` `survey/[surveyKey]` `account/delegate` `account/wallet/**` | 开途独有 | `git mv page.tsx page.kaitu.tsx` |
| `(manager)/layout.tsx` + `(manager)/manager/**/{page,layout}.tsx` | 开途独有 | 全部 `.kaitu.tsx`；中间件里 `/manager` `/admin` 的 404 门删除（页面不存在即 404）；`/app/*` 代理门保留（rewrite 与页面无关） |
| `login` `403` `privacy` `terms` `k2/[[...path]]` `[...slug]` `[locale]/layout.tsx` `account/layout.tsx` `account/security` | 共用 | 不动文件名 |

守卫：`tests/brand-page-tree.test.ts` —— 扫 `src/app`：① `(manager)` 下所有 route 文件必须带 `.kaitu.`；② 任一目录不得同时有 `page.tsx` 与 `page.<brand>.tsx`；③ 分配表里的开途独有目录不得出现裸 `page.tsx`。

### 1.3 副作用清单

- 测试里的 `import('../src/app/[locale]/discovery/page')` 等改为 `…/page.kaitu`；`homepage-content.test.ts` 读 `page.kaitu.tsx`；`landing-overleap-ssr.test.tsx` 改读 `page.overleap`；`homepage-ssr.test.ts` 不再需要 mock `OverleapHome`。
- `brand-guard.test.ts` 的 `SRC_ALLOW` 里 `src/app/[locale]/routers/` 保留（文件仍在，只是改名）。
- `Brand.features.{routers,releaseNotes,retailerProgram}` 标志与页内 `notFound()` 守卫保留（描述品牌事实、既有测试锚定），但**页脚不再靠它们做条件渲染**（§2）。

## 2. 站点结构按品牌配置

新增 `web/src/lib/site/`：

```ts
// site/types.ts
export interface NavItem { labelKey: string; href: string; }             // labelKey = 'nav.nav.pricing'
export interface FooterColumn { titleKey: string; items: (NavItem & { external?: boolean })[]; }
export interface SiteConfig {
  nav: { primary: NavItem[]; ctaKey: string; ctaHref: string };
  footer: FooterColumn[];
  staticRoutes: string[];                                                 // sitemap
  contentCategories: Record<string, CategoryDef>;                         // 原 content-posts.ts CATEGORIES
  seo: { defaultTitle: Record<Locale, string>; defaultDescription: Record<Locale, string> };
}
// site/kaitu.ts   —— 值 = 今天 Header/Footer/sitemap/CATEGORIES 的渲染结果，逐项对照
// site/overleap.ts —— §3.1
// site/index.ts   —— export function siteConfig(): SiteConfig { return siteBrand().id === 'overleap' ? OVERLEAP_SITE : KAITU_SITE }
```

- `Header.tsx` / `Footer.tsx` 改为遍历配置渲染；开途端逐像素等价（`tests/footer-brand-gates.test.tsx` 的开途断言原样保留作回归锚）。
- `sitemap.ts` 静态路由取 `siteConfig().staticRoutes`；`content-posts.ts` 的 `CATEGORIES` 移入配置（开途 `guides`，Overleap `blog`）。
- `metadata.ts` 的默认 title/description 表（现在七个 locale 都是开途 k2cc 文案）移入 `seo`，按品牌取值。
- 配置文件里只放 key 与路径，不放品牌展示词（`brand-guard` 的 src 扫描继续生效）。

## 3. Overleap 站：页面集与文案

### 3.1 信息架构

- 顶栏：`Why Overleap`（→ `/#features`）· `Pricing`（→ `/#pricing`）· `Help`（→ `/support`）；右侧 Log in / Account · `Download`（→ `/install`）。语言切换保留。
- 页脚：**Product**（Download `/install` · Pricing `/purchase` · Help `/support`）· **Developers**（k2 protocol `/k2` · Run your own server `/k2/quickstart` · GitHub）· **Company**（Privacy `/privacy` · Terms `/terms` · Contact `mailto:support@overleap.io`）。`/blog` 分类注册但首篇文章前不进页脚（空分类页不宣传，沿用 sitemap 既有规则）。
- sitemap 静态路由：`''` `/install` `/purchase` `/support` `/privacy` `/terms` `/login`（不含 `/discovery` `/opensource` `/releases` `/routers`）。

### 3.2 首页文案（en-GB 母版；en-US / en-AU 拼写变体；ja 翻译）

隐私优先叙事，六张功能卡，FAQ 十二条。不出现 GFW / censorship / China 等词（`messages-integrity` 既有禁词继续生效），不承诺流媒体解锁，不提年龄验证绕过。

- **meta**：title `Private VPN with no logs, fast on any network | Overleap`；description `Overleap is a private VPN that hides where you go, even from your ISP, keeps no logs, and stays fast on the networks that let you down. Windows, macOS, iOS and Android.`
- **hero**：badge `No logs · Open-source server · 5 devices`；title `Your browsing is your business.`；subtitle `A private VPN that hides where you go, even from your ISP, and stays fast on the networks that let you down.`；description `Overleap encrypts everything your devices send and hides the destination with Encrypted Client Hello. We keep no record of what you do. One subscription covers five devices on Windows, macOS, iOS and Android.`；ctaPrimary `Get Overleap`；ctaSecondary `See how it works`（→ `#features`）；mock `Protected` / `Auto · fastest route`。
- **features**（title `Why Overleap`）：
  1. `Hidden even from your ISP` — `UK providers are required to keep a record of the services you connect to for up to twelve months. With Overleap your provider sees one ordinary HTTPS session and nothing about where it goes: Encrypted Client Hello hides the destination that other VPNs leave visible in the handshake.`
  2. `No logs. Nothing to hand over.` — `We don't record the sites you visit, the apps you use or the traffic you send. We keep only what a subscription needs: your email address, your plan and the devices signed in.`
  3. `Fast on the networks that let you down` — `Train Wi-Fi, a packed stadium, a hotel connection: most VPNs crawl the moment packets go missing. Overleap's k2cc rate control tells interference apart from real congestion and keeps your speed up.`
  4. `Moves with you` — `Leave the house, lose the Wi-Fi, carry on. The tunnel migrates from Wi-Fi to mobile data mid-session, so downloads and calls continue without reconnecting.`
  5. `Keeps working abroad` — `Travelling somewhere VPNs usually fail? Overleap's connection looks like ordinary web traffic, so it keeps working where others go dark.`
  6. `Built in the open` — `The protocol is documented and the server is open source. Run your own node and use Overleap purely as the client, or read the code and take nothing on trust.`
- **steps**（title `Up and running in three steps`）：`Subscribe` / `Card, Apple Pay or Google Pay · cancel any time` → `Install the app` / `Windows · macOS · iOS · Android` → `Sign in and connect` / `One tap. No configuration.`；cta `Start now`。
- **pricing**（title `One plan. Every device.`；subtitle `Five devices, unlimited data, every location. Cancel any time.`）：价格数字**不写进文案**，由 §4.4 的品牌定价表按 locale 渲染；note 键：yearly `Best value — about {monthly} a month`，monthly `Cancel any time`；currencyNote `Prices in {currency}. Pay by card, Apple Pay or Google Pay. You're charged in your local currency where available.`；includes `5 devices at once` / `Unlimited data` / `All locations` / `Windows, macOS, iOS, Android`。
- **faq**（title `Questions`；subtitle `Straight answers about how Overleap works.`）：`logs`（Do you keep logs?）· `isp`（What does my ISP see?）· `legal`（Is using a VPN legal in the UK? — `Yes. Using a VPN is legal in the UK and across Europe. What you do online is still subject to the law.`）· `publicWifi`（Is it safe on public Wi-Fi?）· `travel`（Does it work when I travel?）· `ech`（What is Encrypted Client Hello?）· `selfHost`（Can I run my own server?）· `platforms` · `devices` · `pricing`（答案含 `{yearly}` `{monthly}` 插值，由定价表填）· `payment`（Card through Stripe; Apple Pay / Google Pay at checkout）· `cancel`。
- **download**：title `Get Overleap on every device`；subtitle `One account, five devices, the same connection everywhere.`；button `Download`。

组件层：`components/home-overleap/*` 已有，改为六卡网格与新 FAQ key 表；`OverleapPricing` 接收 §4.4 的展示价。

### 3.3 下载页 `/install`（`page.overleap.tsx` + `components/install-overleap/`）

- 标题 `Download Overleap`；设备自动检测高亮当前平台；四张平台卡（Windows `.exe` · macOS `.pkg` · iOS App Store · Android Play）。
- 桌面链接来自既有 `fetchAllDownloadLinks()`（品牌 CDN，`Brand.cdn`）；某平台无产物时卡片显示 `Coming soon` 而不是 `Overleap_null_*`（修第一波遗留）。
- 移动端：App Store / Play 链接来自 `Brand`（新增 `storeLinks: { ios: string; android: string }`，Overleap 先留空 → `Coming to the App Store`）。
- 无中文截图、无 Linux/APK 侧载指南（`features.linuxInstall/androidApkGuide` 为 false）。
- 文案 namespace `download`（Overleap 独有）。

### 3.4 帮助页 `/support`（`page.overleap.tsx`）

- 标题 `Help`；三块：**Getting started**（三步 + 下载链接）· **Account & billing**（管理/取消订阅 → `/account`；退款政策一句：`Contact us within 14 days of your first payment if Overleap doesn't work for you.`——**待法务确认**，实现时先放 `contact us` 通用句，不写天数）· **Questions**（复用 landing FAQ 数据，FAQPage JSON-LD）· **Contact**（`support@overleap.io`）。
- 文案 namespace `help`（Overleap 独有）。

### 3.5 其他

- `/blog`：`contentCategories.blog`（`Blog` / `ブログ`），目录 `content/en-GB/blog/`；空态文案 `Coming soon.`。
- k2 协议文档：`content/en-US/k2/*.md` **移到** `content/en-GB/k2/`（母版随默认语言走；`findK2Post` / `findContentPost` 的回落是"品牌默认语言"，不搬就会让 `/en-GB/k2/protocol` 与 `/en-AU/k2/*` 404）。`en-US/k2/comparison.md` 作为变体保留。`brand-guard` 的内容扫描从 `['en-US','ja']` 改为 `OVERLEAP.allowedLocales`。
- webapp `brands/overleap/index.ts` 的 `slogans.default` 改为 `Your browsing is your business.`（ja / zh 同步意译）——登录页展示。

## 4. 币种与定价（系统基础决定）

### 4.1 事实

- Stripe 账号：**US 主体，结算币 USD**（`GET /v1/account`）。
- 测试模式现有 Price（`lookup_key` `overleap_basic_1y` / `_1m`）：**EUR 主币** 8900 / 1199，`currency_options` {usd 7900/1199, gbp 7900/999}。生产（live）尚未建任何资源；生产与 dev 库都没有 overleap Plan 行。
- Checkout 对多币种 Price **按客户属地自动选币**（IP 判定，不传 `currency` 时），属地不在 `currency_options` 里则回落主币。Adaptive Pricing（Stripe 自动换算 150+ 国本币）要求 **Price 主币是结算币**——EUR 主币下它永远不生效，澳/日等客户会看到欧元。
- `Plan.Price` 注释即"美分"；webapp `StripePurchasePanel` 已按 `$` 格式化；只有 web 购买页写死 `€`。

### 4.2 决策

1. **Price 主币改为 USD**（$79 / $11.99），`currency_options` **GBP**（£79 / £9.99）与 **EUR**（€89 / €11.99）为固定本币价；其余国家由 Adaptive Pricing 换算（Dashboard 开关，ops 项）。数字与 7 月 spec / ASC 定价完全一致，只换主币。这**推翻 2026-07-22 spec 的"EUR 主币"**——理由是结算币事实。
2. Price 币种不可改 → `scripts/stripe-setup-overleap.sh` 改为：按 lookup_key 找到既有 Price，若主币 ≠ usd 则 `active=false` 归档，再以 `transfer_lookup_key=true` 新建。测试模式现在就跑；live 上线时同脚本重跑。
3. Checkout 创建**不传 `currency`**，让 Stripe 按属地选币；网站展示只是按 locale 的"预期币种"，并明示 `charged in your local currency where available`。
4. `Plan.Price` = USD 分（7900 / 1199）。dev 库补两条 overleap Plan 行（新 price id）；生产行仍是上线清单项。

### 4.3 API：`DataPlan.currencyPrices`

- `DataPlan` 新增 `CurrencyPrices map[string]int64 \`json:"currencyPrices,omitempty"\``（币种小写 → 最小单位金额，含主币）。
- `api/logic_stripe_price.go`：`stripePriceCurrencyAmounts(ctx, key, priceID)`，`price.Client.Get(id, &stripe.PriceParams{Expand: ["currency_options"]})`，包级 var 可替换（对标 `stripeNewCheckoutSession`）；进程内缓存 TTL 1h（`sync.Map` + 过期时间）；失败只记日志、字段省略（fail-open：客户端回落 `price` 美元）。
- `buildPlanDTO`：`plan.StripePriceID != "" && configStripe(c).Ready()` 时附带。kaitu plan 无 price id → 零行为变化。
- 测试：mock fetch var 验证映射与缓存命中；契约测试照跑（DataPlan 不在契约范围，但 `-count=1` 跑一遍确认）。

### 4.4 Web 展示

- `lib/pricing.ts`：`displayCurrency(locale)`：`en-GB → 'gbp'`；`en-US` / `en-AU` / `ja` → `'usd'`（EU locale 未来加入时 → `'eur'`）。`formatMinor(amount, currency, locale)` 用 `Intl.NumberFormat`。
- 首页定价区：**静态品牌定价表** `OVERLEAP_SITE.pricing = { yearly: { usd: 7900, gbp: 7900, eur: 8900 }, monthly: { usd: 1199, gbp: 999, eur: 1199 } }`（首页是静态营销页，不打 API）。守卫 `tests/pricing-source.test.ts` 解析 `scripts/stripe-setup-overleap.sh` 的 `ensure_price` 行，断言与定价表逐币种相等——三处（脚本 / 网站 / Stripe 建法）只能同时变。
- 购买页 `OverleapPurchaseClient`：`plan.currencyPrices?.[displayCurrency] ?? plan.price`（后者按 usd 格式化），删掉 `formatEur`；`stripe.currencyNote` 改为 `Prices in {currency}. You're charged in your local currency where available.`
- webapp `StripePurchasePanel`：同一规则按 app locale 取币，缺 `currencyPrices` 时维持 `$`（`api-types.ts` Plan 加可选字段）。

## 5. 文案与内容解耦

### 5.1 namespace 按品牌

`messages/namespaces.ts` 新增：

```ts
export const SHARED_NAMESPACES = ['common','nav','auth','purchase','account','discovery','errors','k2'] as const;
export const BRAND_NAMESPACES = {
  kaitu:    [...SHARED_NAMESPACES, 'hero','install','wallet','campaigns','admin','invite','theme','changelog','releases','routers','guide-parents','licenseKeys','survey'],
  overleap: [...SHARED_NAMESPACES, 'landing','download','help'],
} as const;
```

- `install` 归开途（`InstallClient` 是开途页）；Overleap 下载页用自己的 `download`。
- `request.ts` 只加载 `BRAND_NAMESPACES[siteBrand().id]`；缺文件回落 **品牌默认语言**（不再是 zh-CN——现在一个缺失的 en-GB 文件会让 overleap.io 静默出中文）。
- **删除**从未被 Overleap 渲染的开途叙事文件：en-US / en-GB / en-AU / ja 下的 13 个开途独有 namespace（52 个文件）；删除 zh-* 下的 `landing.json`（第一波为回落而放的英文副本）。
- `messages-parity.test.ts` 改为按品牌：base = 品牌默认语言（overleap 为 en-GB），只比该品牌的 locale × namespace。
- `nav.json`：en 侧只保留 Overleap 用到的 key + 共享组件 key；zh 侧不动。共享组件（Header/Footer/CookieConsent/LanguageDetectionBanner/BrowserWarningBar）用到的 key 两边必须都有——由 `brand-leak-ssr` 的真实文案渲染 + 原始 key 检测兜底。

### 5.2 语言矩阵

- `OVERLEAP.defaultLocale = 'en-GB'`，`allowedLocales = ['en-GB','en-US','en-AU','ja']`。
- 中间件 `getBestLocale`：无地区码的 `en` 与不匹配的 `en-XX` 落到**品牌默认语言**（现在写死 en-US）。`/en-US/*` 继续 200（仍在允许集）。
- hreflang `x-default` 自动变为 `/en-GB`（`metadata.ts` 既有逻辑）。

## 6. 非代码 / 上线清单（本 spec 不做，只登记）

1. Stripe Dashboard 开启 Adaptive Pricing（测试与 live 各一次）。
2. live 模式重跑 `scripts/stripe-setup-overleap.sh`，回填生产两条 Plan 行（`price` 7900 / 1199，`stripe_price_id` 新 id）。
3. 帮助页退款句的法务口径；隐私政策 / 条款的 UK GDPR 表述（现为通用英文）。
4. App Store / Play 上架后回填 `Brand.storeLinks`。
5. 桌面 `-overleap` tag 发布（第一波遗留）。

## 7. 验证

- `web`：全量 vitest（含新守卫 `brand-page-tree` / `pricing-source`，改造后的 `messages-parity`）；`yarn build` 与 `yarn build:overleap` 都通过（两棵页面树各自编译）；dev 下截图 overleap.io 首页 / install / support / purchase（en-GB 与 en-US 各一），kaitu.io 首页与页脚截图对照第一波基线逐像素一致。
- `api`：`go test ./... -v` 0 SKIP；`UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract`。
- `webapp`：双品牌 vitest；`check-brand-purity.sh`。
- Stripe 测试模式：脚本重建后，用 `customer_email=test+location_GB@example.com` 建一个 Checkout Session，断言返回 `currency == "gbp"` 且 `amount_total == 7900`；`+location_US` → usd 7900；`+location_DE` → eur 8900；`+location_AU`（Adaptive 未开时）→ usd。
- 部署前停：本 spec 的实现合入 `main` 后**不自动 push `website`**——线上文案与定位变更由用户确认后再发。
