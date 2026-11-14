# kaitu.io site notes — brand, i18n client messages, webapp boundary

Moved verbatim from `web/CLAUDE.md` (2026-11-14) so the per-session doc stays small. **Read this before changing `src/lib/brands.ts`, `middleware.ts` locale handling, the funnel pixel / cookie consent / legal docs, manager brand filtering, `/pricing` / router-edition pricing, `ClientMessages` groups, or before adding account features to the site** — the short invariant lists in `web/CLAUDE.md` point here. Funnel `source` values: `docs/web/funnel-events.md`; SEO checklist: `docs/web/seo-guidelines.md`.

## Brand（单品牌：只构建 kaitu.io）

- **本应用只服务 kaitu.io**。overleap.io 是独立应用 `sites/overleap/`（不共享代码、自带 lockfile / amplify.yml），2026-10 从本应用拆出（spec `2026-09-30-overleap-site-app-design.md` 阶段 ⑤）。`siteBrand()`（`src/lib/brands.ts`）恒返回 `KAITU`，不读环境变量；`next.config.ts` 在 `NEXT_PUBLIC_BRAND` 被设成 kaitu 以外的值时**让构建失败**——防止一个还按旧配置构建的 overleap Amplify app 把开途站部署到 overleap.io。
- **页面文件就是普通的 `page.tsx` / `layout.tsx`**。旧的按品牌编译（`pageExtensions` + `page.kaitu.tsx` / `page.overleap.tsx`）已删除；`page.kaitu.tsx` 这种名字现在**不会被 Next 编译**（路径静默 404，直接 import 模块的单测照样绿）——`tests/brand-page-tree.test.ts` 让任何带品牌中缀的路由文件红掉。
- **品牌注册表仍有两个品牌**：`/manager` 是跨品牌后台（唯一入口 kaitu.io/manager，管两个品牌的数据），`contracts/api-contract.json` 也按全量品牌比对（`tests/cross-layer-contract.test.ts`）。`KAITU` 是完整的站点配置（`Brand`），`OVERLEAP` 只有身份字段（`BrandIdentity`：id / displayName / wordmark / baseUrl / contactEmail）；`brandById()` 返回 `BrandIdentity`。**`BrandId` 联合类型与 `OVERLEAP` 条目别删**——manager 的 `BrandBadge` / `BrandPicker` / 品牌筛选靠它们渲染另一品牌的数据。
- **Locale**：zh-CN（默认）/ zh-TW / zh-HK + en-US / ko / ja / ru（`src/i18n/routing.ts`，`ALL_LOCALES` 在 `brands.ts` 里内联一份；与开途 App `webapp/src/brands/kaitu/index.ts` 的 `locales` **同一份清单，改一边必须改另一边**）。外语面向**在华外国人**（2026-11-10 决定）——开途仍是中国产品，不碰 Overleap 的海外市场。旧的 en-GB / en-AU URL 由 `middleware.ts` **301 到同路径的 en-US**（matcher 单列）。根路径按 `Accept-Language` 依次匹配（kk/ky/be 落 ru）；带了语言但一个都不认识 → en-US；没有该头（爬虫）→ zh-CN。
- **外语品牌名**：`localizedWordmark(brand, locale)`（`brands.ts`）——中文只写「开途」，其余语言写「开途 Kaitu」（不单独写 Kaitu，搜索引擎会纠错成 kaitai）。标题 / 页头页脚 / `{wordmark}` 模板都走它；外语 messages 里也一律「开途 Kaitu」。
- **外语文章**：`listCategoryPosts` 在外语 locale **不列** zh-CN 回落文章（详情页仍回落并 canonical 到 zh-CN）；外语指南写成 `content/<locale>/guides/*.md` 原创稿，`section: living-in-china`。法务页外语一律渲染英文母版（`lib/legal.ts`）。
- **Namespace**：`messages/namespaces.ts` 的 `namespaces` 就是全部（`request.ts` 全量加载，缺文件回落 zh-CN）。`tests/messages-parity.test.ts` 逐 namespace 比对全部 7 个 locale（以 zh-CN 为准），并断言 `messages/` 下只有这些 locale 目录、没有未注册的 json。`tests/messages-integrity.test.ts` 的政治敏感词禁令（翻墙 / 防火墙 / GFW / firewall / censorship / 방화벽 / 검열 / ファイアウォール / цензура …）覆盖**所有** locale。
- **品牌隔离仍然生效**：kaitu.io 的任何用户面都不得出现 Overleap。**没有任何例外**：法务署名是运营公司 `Wordgate LLC`（`Brand.legalName`，由 `Footer` 和 `public/legal/*.md` 的 `{{legalName}}` 渲染，与 overleap.io 同一主体），它不是品牌名，守卫扫描时不剥离任何字符串（2026-10-29 前是 `Overleap LLC` + 剥离豁免）。守卫 `tests/brand-guard.test.ts`（zh messages / src / public/legal 文件扫描）、`tests/brand-leak-ssr.test.tsx`（渲染后的 chrome + metadata + 首页）。**品牌字面量只能进 `src/lib/brands.ts`**——注释里也别写展示词（guard 扫整行），用 `overleap` / `kaitu` 小写 id。
- **站点结构（`src/lib/site/kaitu.ts`，经 `siteConfig()` 读）**：导航、页脚栏目、sitemap 静态路由、内容分类、SEO 默认 title/description、定价页 App 版快照。Header / Footer / sitemap / content-posts / metadata 只读配置渲染；配置里只放 key 与路径。守卫：`tests/site-config-keys.test.ts`、`tests/footer-brand-gates.test.tsx`（页脚/顶栏一条不少、已下线路径一条不多）。
- **Funnel pixel（转化漏斗埋点）**：`src/lib/funnel.ts` 是网站上报行为事件的**唯一入口**（`track()` / `pxUrl()`，`new Image()` 打 `/api/px`，失败一律吞掉）；事件名 `WEB_FUNNEL_EVENTS` 由 `tests/funnel-contract.test.ts` 与 `contracts/api-contract.json` 的 `funnelEvents`（surfaces 含 `web`）锁定，两边只改一头就红。各漏斗步骤的发射文件登记在 `api/logic_funnel_paths_test.go` `funnelStepEmitSites`（改名/挪文件要同步，否则 api 测试红）。
- 页面只拼 URL、不含后端逻辑（同意/GPC/退出/落库都在服务端 `/api/px`）；`FunnelPageView` 在 `[locale]/layout.tsx` 挂；共享组件（`EmailLogin` 等）只通过可选回调 prop 发事件，别的使用方不传 = 不产生事件。
- **预览请求不埋**：购买页 `fetchPreview`（`preview: true`）是页面自己发的，只有 `handleOrder` / `handleDelegatePay` / `handleEmptyStateDelegatePay` / 路由器结账页 `handlePay` 这类真下单才发 `checkout_start`，且**必须带套餐 pid**（服务端按它拆套餐维度；没选中套餐就不发）。`plan_select` 只在访客真的换了卡片时发，默认选中不算。
- **`install_click` 的 `source` 取值与 `qr_view` 的触发时机**：见 `docs/web/funnel-events.md`（`source` 是自由字符串，无测试锁定，新增发射点时同步那份表）。
- **`FunnelPageView` 跳过 App 内嵌页**（`?embed=true` / `#embed`，`isEmbeddedPage()`）；像素的 `u` 只带路径 + `utm_*`（`pixelLocation()` 在浏览器端剥掉其余 query 与 hash，token / code 不出浏览器）。
- 页脚「不参与访问统计」是 `NavItem.external: true` → 原生 `<a href="/api/px/optout">`（next-intl `Link` 会补 locale 前缀）；隐私政策 §5.2 / §5.5 描述该 cookie 与 App 事件，改埋点范围要同步改 `public/legal/privacy-policy.md`。§5.2 里「是否用第三方分析」那句是 `{{thirdPartyAnalytics}}` 占位，由 `lib/legal.ts` 按 `Brand.gaMeasurementId` 生成（`tests/legal-docs.test.ts` 锁「有 id ⇔ 有那句」）。Cookie 横幅「拒绝」会请求 `/api/px/optout`（cookie 是 HttpOnly，只有服务端能改成退出值）；改横幅文案要 bump `COOKIE_CONSENT_VERSION`。横幅的 `detailsThirdParty` 一句只在 `gaMeasurementId` 非空时渲染。`public/legal/*.md` 各含中文 + 英文母版：zh-* 渲染中文，en-US / ko / ja / ru 渲染英文——改法务条款**两份都要改**。
- **Manager 是跨品牌视角**：顶栏全局品牌筛选 `components/manager/brand.tsx`（`ManagerBrandProvider` / `useManagerBrand()`，「全部」= 不带参数，选择存 localStorage `manager.brandFilter`，读完才渲染子树以免先按「全部」多拉一次）。列表/统计页把 `brandParam` 作为 `?brand=` 传给 `/app/*`；带 `brand` 的行用 `BrandBadge`。**品牌自有实体（套餐/活动/公告/授权码批次/EDM 模板）的创建表单必须用 `BrandPicker` 显式选品牌**——无默认值，API 拒绝空 brand；编辑时品牌只读（创建后不可变）。展示名只取 `brandById(id).wordmark`，manager 源码里也不写品牌展示词。
- **`X-K2-Brand: kaitu`**: injected on every `/api/*`/`/app/*` request by BOTH `src/lib/api.ts` and middleware. Center resolves Host → header → kaitu (`api/brand.go`).
- **Content `brand:` frontmatter**: velite `brand: kaitu|overleap|both`（默认 both）。`isPostVisibleToBrand` 仍然生效：标成另一品牌的文章 404、不进 sitemap / 侧栏。
- **定价页 `/pricing`**：App 版价格是 `lib/site/kaitu.ts` `appPlans` 的**静态快照**（SSR/SEO 用），`components/pricing/AppPlansGrid` 挂载后拉 `/api/plans` 按 pid 覆盖——后台改价后同步一下快照即可，不同步只是首屏闪一下旧价。**路由器版预售**（2026-11-11 发售后精简本句）由 `lib/router-edition.ts` 的 `ROUTER_PRESALE`（$359、发售日 2026-11-11 东八区零点）按日期切换：`/routers`、`/pricing`、结账页、账户页都读 `routerOffer()`；这两页因此是 `revalidate = 3600` 而非 `force-static`。价格常量与 `docs/router-edition-prod-deploy.md` 的 SQL/UPDATE 由 `src/lib/__tests__/router-edition.test.ts` 锁定。服务期自发货日起算是后端规则（`api_admin_router.go` 标发货时重设线路到期），账户页在成品未发货前不显示到期日。

## i18n — namespace banner and client messages

**Namespace registry**: `messages/namespaces.ts` lists all active namespaces. When adding a new `*.json` namespace file, add its name to the `namespaces` array — otherwise it is never loaded and all keys return their raw key string silently.

> **Ignore that file's own `DO NOT EDIT` banner.** It says to regenerate via
> `node scripts/i18n/split-namespaces.js web`; **that script no longer exists**.
> Hand-editing `namespaces.ts` is the correct and only way to register a namespace.
> (Its `namespaceMapping` table is a separate flat-key→namespace map used by the
> splitter that generated the current layout — leave it alone unless you're moving keys.)

**Message-file shape trap**: `src/i18n/request.ts` stores each file under its namespace (`messages[ns] = file`), so the full key is `{namespace}.{keys inside the file}` — and the files come in three shapes. Single wrapper equal to the namespace (`install.json` = `{install: {...}}` → `t('install.install.windows')`, `InstallClient.tsx`); several sibling wrappers (`hero.json` = `{hero, security, download, routers, faq}` → with `namespace: 'hero'`, `t('hero.title')` is really `hero.hero.title`); flat leaves (`changelog.json`, `releases.json`, `survey.json` → `t('changelog.title')`). A key added at the wrong level in all 3 files passes every test — `src/test/setup.ts` stubs `useTranslations` to identity and `tests/messages-parity.test.ts` only compares locales against zh-CN — and renders as raw key text. Only a real browser render catches it.

**File-level fallback**: `request.ts` loads `messages/zh-CN/{ns}.json` when `{locale}/{ns}.json` is missing.

**客户端只拿自己用得到的翻译（2026-11-10，Amplify 产物上限 220 MiB）**：根布局**不再**把全量 messages 交给 `NextIntlClientProvider`——每个预渲染页的 `.html` 和 `.rsc` 都会内嵌 provider 拿到的全部内容（全量 ≈ 每页 300–450 KB，压缩后 55 KB），7 语言乘几百页撑爆了 Amplify 的 220 MiB 上限（job 145/146 失败）。现在是 `src/i18n/ClientMessages.tsx`：根布局给 BASE（外壳 + Header/Footer），需要更多的路由目录在自己的 `layout.tsx` 里 `<ClientMessages locale group="<目录名>">`（**嵌套 provider 是替换不是合并**，所以 group = BASE ∪ 本组额外）。服务端组件用 `getTranslations`，只发文字，不需要 messages。清单 `src/i18n/client-messages.generated.ts` 由 `node scripts/i18n/gen-client-messages.mjs` 沿 import 图生成（从 `"use client"` 边界往下，收集字符串字面量里真实存在的 `<文件>.<顶层键>`）。
- **改了客户端组件用到的键 / 新增带客户端组件的路由目录**：重跑上面那条命令。`tests/client-messages.test.ts` 会在清单过期、某个 group 没有对应 layout、或某语言缺路径时变红。
- **盲区**：完全在运行时拼出、源码里没有任何字面量的键，静态分析看不到——这种键必须在源码里至少出现一次带前缀的字面量，或者在真实浏览器里走一遍。`useTranslations` 在单测里被桩成恒等，所以单测抓不到；预渲染日志里的 `MISSING_MESSAGE` 要保持为 0。
- 页面能做成服务端组件就别 `"use client"`：纯文章页（隐私、条款、guides、定价）的客户端 messages 是 0 增量。

## Release notes frontmatter

- **Frontmatter**: `version` + `date` (required), plus optional `noDownloads: true` for releases that don't ship app binaries (e.g. router-only or hot-fix bumps that share the previous version's downloads). When set, the `/releases` page hides macOS / Windows / Linux / Android / iOS download buttons for that row. `windowsPending: true` hides only that row's Windows button — set it while the new exe hasn't cleared SmartScreen (`scripts/CLAUDE.md` 「Windows 上线门」), delete it once it has. The `/install` page needs no flag: `lib/downloads.ts` takes the Windows version from the manifest's `platforms.windows-x86_64.url`, so it follows the updater pointer by itself.

## 与 webapp 的职责边界（别在本站复刻账号中心）

`webapp/` 已经是完整账号中心（设备、专属节点、邀请、购买历史、代付、改邮箱、反馈）。
本站**不复刻**它 —— 边界按「用户此刻装没装 app」划，**不按功能划**：

- **website = 还没装 app 的人**：搜到 → 看价格 → 买 → **当场确认买成功了** → 下载。
  `/account` 只做订阅状态（到期时间、档位、购买记录），是购买闭环的最后一环，
  不是账号中心。设备 / 节点 / 邀请管理永远只在 webapp。
- **webapp = 已经在用的人**：所有日常账号管理。
- **唯一的交叉是资金面**（钱包 / 提现），而且它是被规则逼出来的，不是历史包袱：
  `webapp/src/App.tsx` 的 `/delegate` 路由在 iOS 上被显式摘掉（注释写明 Apple 3.1.1，
  IAP 以外支付），webapp 的钱包入口也是 `openExternal` 外链回本站。**想靠"都塞进
  webapp"消除两边的分裂，在 iOS 上做不到。**

不变量：**重复的那一份必然先腐烂**——别在本站复刻 webapp 已有的账号功能。

- **例外：路由器版**（`/account/router`）。路由器版客户不一定装 app，开通进度、续费与存量自备台账的安装命令只能在本站完成（spec `2026-09-16-router-edition-web-onboarding-design.md` §4.4）。它只读 `GET /api/user/router`，不做设备 / 节点管理。**自备路由器新购已下线**：`/routers/diy` 教程页已删，`/purchase/router` 只卖含路由器的成品，服务套餐（`hardwareSku` 为空）仅在 `?plan=svc` + 已登录 + 检测到可续的路由器版时作为续费出现（后端同样拒绝无路由器用户下服务套餐）；「我的路由器」的生成安装命令只服务存量自备台账，保留。

## Deployment

The kaitu.io AWS Amplify app builds the `website` branch — a pure mirror of `main`, never committed to directly — with `appRoot: web`. Deploy = `git push origin main:website`. (overleap.io is built from `sites/overleap/` with its own `amplify.yml`.) `amplify.yml` preBuild bakes console env vars into `.env.production` (standalone output does not forward them to the SSR Lambda); `scripts/amplify-prebuild.sh` (the `prebuild` npm script) only runs `scripts/generate-changelog.js`. Sentry: `withSentryConfig` in `next.config.ts` + `src/instrumentation*.ts` + `src/lib/sentry-filters.ts`; DSN via `NEXT_PUBLIC_SENTRY_DSN` (empty = SDK no-ops). AWS resource IDs (S3 / CloudFront / IAM / Route 53): [`docs/ops/web-amplify.md`](../docs/ops/web-amplify.md).

## Gotchas — full text

- **Velite mock in tests**: vitest tests mock `#velite` import with synthetic post data. Server Component pages tested by calling as async functions directly, asserting on returned JSX or `generateMetadata()` output.
- **next-intl IntlMessages interface**: `web/src/types/i18n.d.ts` uses an empty `interface IntlMessages {}` (permissive typing) because messages are split across namespace files loaded dynamically. This disables compile-time key checking — use runtime tests instead.
- **Server Component pages with setRequestLocale**: Cast locale to `(typeof routing.locales)[number]` when calling `setRequestLocale()`. The URL param type is `string` but next-intl requires the narrower union type.
- **Homepage is a Server Component**: `web/src/app/[locale]/page.tsx` (no `dynamic` export). Do NOT add `"use client"` — it would break SSR metadata and SEO.
- **Purchase page Server Component pattern**: `purchase/page.tsx` is a Server Component wrapper that exports `generateMetadata()` for SEO. Client-side purchase logic is in `PurchaseClient` (WordGate).
- **Embed mode** (`?embed=true`): Pages embedded in desktop app iframe. `useEmbedMode()` hook controls Header/Footer/CTA visibility. The chat widget (`ChatWidgetLazy`) and `CookieConsent` auto-hide in embed mode. Used by `/releases` and `/changelog` routes.
- **Platform labels in i18n**: Use user-friendly names, not technical ones. iOS → "iPhone / iPad", macOS → "苹果电脑" (zh) / "Mac" (en), Android → "安卓" (zh). No file extensions (.exe/.dmg/.apk) in download button labels.
- **`transpilePackages` in `next.config.ts`** (`intl-messageformat`, `@xterm/xterm`): Next does not transpile `node_modules`; a dep shipping class `static {}` blocks blanks the page on iOS 16.0-16.3 / Safari < 16.4 with `SyntaxError: Unexpected token '{'`. Add new offenders to that list.
- **`/` must stay `Cache-Control: private, no-store`** (`next.config.ts` headers + `middleware.ts`): the root 307 is computed from Accept-Language + `preferredLocale` cookie; a public cache pins the first visitor's locale for a whole CloudFront PoP. The dynamic-page cache rule deliberately matches `.+`, not `.*`, so `/` never falls into it.
- **Middleware passthrough is load-bearing**: `src/middleware.ts` must early-return `NextResponse.next()` for `/manager` before reaching `intlMiddleware`. The matcher deliberately **includes** `/api`, `/app`, `/manager` (`X-K2-Brand` injection must run there), plus the legacy `en-GB` / `en-AU` prefixes (their 301); only `_next`, `_vercel` and dotted static files are excluded by the catch-all. Without the early return, i18n middleware mangles admin requests into locale-prefixed redirects.`tests/middleware.test.ts` covers this — keep it green when editing.
- **`/i/k2s` and `/i/k2r` have a side effect**: `middleware.ts` fire-and-forget POSTs `{ip_raw, ua}` to `https://k2.52j.me/api/stats/{k2s,k2r}-download` before serving the script (`/i/k2` does not report).
