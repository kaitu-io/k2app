# Web — Kaitu Website + Admin Dashboard

Next.js website serving public marketing pages, user self-service (purchase, account, wallet), and admin management dashboard.

**Separate from yarn workspaces** — has its own `yarn.lock` and `node_modules/`; run `yarn install` inside `web/` (yarn only, never npm). Node >= 22 (`.nvmrc`).

## Commands

```bash
cd web && yarn install           # Install dependencies (independent from root)
cd web && yarn dev               # Dev server (Turbopack)
cd web && yarn build             # Production build
cd web && yarn lint              # ESLint
cd web && yarn test              # Vitest unit tests
cd web && yarn test:e2e          # Playwright E2E tests
cd web && yarn test:e2e:headed   # E2E with browser visible
```

## Brand（单品牌：只构建 kaitu.io）

- **本应用只服务 kaitu.io**。overleap.io 是独立应用 `sites/overleap/`（不共享代码、自带 lockfile / amplify.yml），2026-10 从本应用拆出（spec `2026-09-30-overleap-site-app-design.md` 阶段 ⑤）。`siteBrand()`（`src/lib/brands.ts`）恒返回 `KAITU`，不读环境变量；`next.config.ts` 在 `NEXT_PUBLIC_BRAND` 被设成 kaitu 以外的值时**让构建失败**——防止一个还按旧配置构建的 overleap Amplify app 把开途站部署到 overleap.io。
- **页面文件就是普通的 `page.tsx` / `layout.tsx`**。旧的按品牌编译（`pageExtensions` + `page.kaitu.tsx` / `page.overleap.tsx`）已删除；`page.kaitu.tsx` 这种名字现在**不会被 Next 编译**（路径静默 404，直接 import 模块的单测照样绿）——`tests/brand-page-tree.test.ts` 让任何带品牌中缀的路由文件红掉。
- **品牌注册表仍有两个品牌**：`/manager` 是跨品牌后台（唯一入口 kaitu.io/manager，管两个品牌的数据），`contracts/api-contract.json` 也按全量品牌比对（`tests/cross-layer-contract.test.ts`）。`KAITU` 是完整的站点配置（`Brand`），`OVERLEAP` 只有身份字段（`BrandIdentity`：id / displayName / wordmark / baseUrl / contactEmail）；`brandById()` 返回 `BrandIdentity`。**`BrandId` 联合类型与 `OVERLEAP` 条目别删**——manager 的 `BrandBadge` / `BrandPicker` / 品牌筛选靠它们渲染另一品牌的数据。
- **Locale**：zh-CN（默认）/ zh-TW / zh-HK（`src/i18n/routing.ts`，`ALL_LOCALES` 在 `brands.ts` 里内联一份）。旧的 en-US / en-GB / en-AU / ja URL（曾属于 overleap 构建）由 `middleware.ts` **301 到同路径的 zh-CN**（matcher 里单列了这些前缀，带点的路径也覆盖）。`Accept-Language` 不是中文时根路径落 zh-CN。
- **Namespace**：`messages/namespaces.ts` 的 `namespaces` 就是全部（`request.ts` 全量加载，缺文件回落 zh-CN）。`tests/messages-parity.test.ts` 逐 namespace 比对三个 locale，并断言 `messages/` 下只有这三个 locale 目录、没有未注册的 json。
- **品牌隔离仍然生效**：kaitu.io 的任何用户面都不得出现 Overleap。**唯一跨品牌例外是法务署名** `Overleap LLC`（`Brand.legalName`，由 `Footer` 渲染）——SSR 守卫扫描前先剥掉它并断言它存在，例外因此不会扩散。守卫 `tests/brand-guard.test.ts`（zh messages / src / public/legal 文件扫描）、`tests/brand-leak-ssr.test.tsx`（渲染后的 chrome + metadata + 首页）。**品牌字面量只能进 `src/lib/brands.ts`**——注释里也别写展示词（guard 扫整行），用 `overleap` / `kaitu` 小写 id。
- **站点结构（`src/lib/site/kaitu.ts`，经 `siteConfig()` 读）**：导航、页脚栏目、sitemap 静态路由、内容分类、SEO 默认 title/description、定价页 App 版快照。Header / Footer / sitemap / content-posts / metadata 只读配置渲染；配置里只放 key 与路径。守卫：`tests/site-config-keys.test.ts`、`tests/footer-brand-gates.test.tsx`（页脚/顶栏一条不少、已下线路径一条不多）。
- **Funnel pixel（转化漏斗埋点）**：`src/lib/funnel.ts` 是网站上报行为事件的**唯一入口**（`track()` / `pxUrl()`，`new Image()` 打 `/api/px`，失败一律吞掉）；事件名 `WEB_FUNNEL_EVENTS` 由 `tests/funnel-contract.test.ts` 与 `contracts/api-contract.json` 的 `funnelEvents`（surfaces 含 `web`）锁定，两边只改一头就红。各漏斗步骤的发射文件登记在 `api/logic_funnel_paths_test.go` `funnelStepEmitSites`（改名/挪文件要同步，否则 api 测试红）。
- 页面只拼 URL、不含后端逻辑（同意/GPC/退出/落库都在服务端 `/api/px`）；`FunnelPageView` 在 `[locale]/layout.tsx` 挂；共享组件（`EmailLogin` 等）只通过可选回调 prop 发事件，别的使用方不传 = 不产生事件。
- **预览请求不埋**：购买页 `fetchPreview`（`preview: true`）是页面自己发的，只有 `handleOrder` / `handleDelegatePay` / `handleEmptyStateDelegatePay` / 路由器结账页 `handlePay` 这类真下单才发 `checkout_start`，且**必须带套餐 pid**（服务端按它拆套餐维度；没选中套餐就不发）。`plan_select` 只在访客真的换了卡片时发，默认选中不算。
- **`install_click` 的 `source` 取值与 `qr_view` 的触发时机**：见 `docs/web/funnel-events.md`（`source` 是自由字符串，无测试锁定，新增发射点时同步那份表）。
- **`FunnelPageView` 跳过 App 内嵌页**（`?embed=true` / `#embed`，`isEmbeddedPage()`）；像素的 `u` 只带路径 + `utm_*`（`pixelLocation()` 在浏览器端剥掉其余 query 与 hash，token / code 不出浏览器）。
- 页脚「不参与访问统计」是 `NavItem.external: true` → 原生 `<a href="/api/px/optout">`（next-intl `Link` 会补 locale 前缀）；隐私政策 §5.2 / §5.5 描述该 cookie 与 App 事件，改埋点范围要同步改 `public/legal/privacy-policy.md`。§5.2 里「是否用第三方分析」那句是 `{{thirdPartyAnalytics}}` 占位，由 `lib/legal.ts` 按 `Brand.gaMeasurementId` 生成（`tests/legal-docs.test.ts` 锁「有 id ⇔ 有那句」）。Cookie 横幅「拒绝」会请求 `/api/px/optout`（cookie 是 HttpOnly，只有服务端能改成退出值）；改横幅文案要 bump `COOKIE_CONSENT_VERSION`。横幅的 `detailsThirdParty` 一句只在 `gaMeasurementId` 非空时渲染。`public/legal/*.md` 仍各含一份英文母版（只渲染中文母版；英文那份现在是死文本）。
- **Manager 是跨品牌视角**：顶栏全局品牌筛选 `components/manager/brand.tsx`（`ManagerBrandProvider` / `useManagerBrand()`，「全部」= 不带参数，选择存 localStorage `manager.brandFilter`，读完才渲染子树以免先按「全部」多拉一次）。列表/统计页把 `brandParam` 作为 `?brand=` 传给 `/app/*`；带 `brand` 的行用 `BrandBadge`。**品牌自有实体（套餐/活动/公告/授权码批次/EDM 模板）的创建表单必须用 `BrandPicker` 显式选品牌**——无默认值，API 拒绝空 brand；编辑时品牌只读（创建后不可变）。展示名只取 `brandById(id).wordmark`，manager 源码里也不写品牌展示词。
- **`X-K2-Brand: kaitu`**: injected on every `/api/*`/`/app/*` request by BOTH `src/lib/api.ts` and middleware. Center resolves Host → header → kaitu (`api/brand.go`).
- **Content `brand:` frontmatter**: velite `brand: kaitu|overleap|both`（默认 both）。`isPostVisibleToBrand` 仍然生效：标成另一品牌的文章 404、不进 sitemap / 侧栏。
- **定价页 `/pricing`**：App 版价格是 `lib/site/kaitu.ts` `appPlans` 的**静态快照**（SSR/SEO 用），`components/pricing/AppPlansGrid` 挂载后拉 `/api/plans` 按 pid 覆盖——后台改价后同步一下快照即可，不同步只是首屏闪一下旧价。**路由器版预售**（2026-11-11 发售后精简本句）由 `lib/router-edition.ts` 的 `ROUTER_PRESALE`（$359、发售日 2026-11-11 东八区零点）按日期切换：`/routers`、`/pricing`、结账页、账户页都读 `routerOffer()`；这两页因此是 `revalidate = 3600` 而非 `force-static`。价格常量与 `docs/router-edition-prod-deploy.md` 的 SQL/UPDATE 由 `src/lib/__tests__/router-edition.test.ts` 锁定。服务期自发货日起算是后端规则（`api_admin_router.go` 标发货时重设线路到期），账户页在成品未发货前不显示到期日。

## Architecture（非显然的入口）

- `src/app/[locale]/` 公开页（next-intl）；`src/app/(manager)/manager/` 后台（无 locale 前缀、跨品牌、只中文）。
- `src/app/[locale]/k2/[[...path]]/` K2 协议文档（Velite + 侧栏）；`src/app/[locale]/[...slug]/` 其余 Velite 内容（见 Content Publishing）。
- `src/lib/api.ts` API client；`src/lib/api-errors.ts` 错误码 → i18n；`src/lib/constants.ts` `getDownloadLinks` / `getAndroidDownloadLinks`（CDN 下载链接）。
- `src/lib/site/kaitu.ts` 站点结构（nav / footer / sitemap 路由 / 内容分类 / SEO 默认 / 定价快照），经 `siteConfig()` 读。
- `src/lib/k2-posts.ts`（`/k2/` 侧栏）/ `src/lib/content-posts.ts`（`[...slug]`）；`src/lib/events.ts` app 事件总线（`auth:unauthorized`）。
- `src/middleware.ts`：`X-K2-Brand` 注入 + 旧 en/ja 301 + next-intl locale 协商（见 Gotchas "Middleware passthrough"）。
- `content/{locale}/`（zh-CN 母版：`k2/` + `guides/`）· `messages/{locale}/{ns}.json` + `messages/namespaces.ts`（手改注册）· `releases/v*.md`（发版说明源）。

## API Integration

### API Client (`src/lib/api.ts`)

Single `api` object with typed methods returning unwrapped `data`. Envelope is the Center API's (`ApiResponse<T>` in api.ts): HTTP 200 always, `code` 0 = success, `message` is debug text — never show it to users. HttpOnly cookie auth (server-managed) with CSRF protection.

### API Proxy (Next.js → Center API)

`/api/*` and `/app/*` are proxied by `rewrites()` in `next.config.ts` — in every environment, dev and Amplify alike (default upstream `https://k2.52j.me`). `API_PROXY_TARGET=http://127.0.0.1:5899 yarn dev` points at a local Center; it is not in `.env.example`. Middleware injects `X-K2-Brand` on both prefixes.

### Error Handling

`ApiError` class with error codes matching `api/response.go`. On 401, emits `auth:unauthorized` event and auto-redirects to login (configurable via `autoRedirectToAuth` option).

Error code-to-i18n mapping lives in `lib/api-errors.ts`. Use `getApiErrorMessage(code, t)` in public `[locale]` pages and `getApiErrorMessageZh(code)` in manager pages. Never show `error.message` to users — it contains raw backend debug text.

## Authentication

- **Web auth**: HttpOnly cookie (`access_token`) + CSRF token. Cookies sent via `credentials: 'include'`.
- **Embed mode**: Bearer token in `localStorage` for iframe embedding.
- **Manager auth**: Same cookie auth. Admin role checked by Center API middleware.
- **Token refresh**: Server-side sliding expiration (< 7 days remaining → auto-renew). No client-side refresh.

## i18n (next-intl)

Locales and the legacy en/ja 301 live in the Brand section above; `src/i18n/routing.ts` lists zh-CN / zh-TW / zh-HK (`defaultLocale: 'zh-CN'`).

**URL format**: `/{locale}/path` (e.g., `/zh-CN/purchase`, `/zh-TW/install`)

**Usage**: `const t = useTranslations()` — NOT `const { t } = useTranslations()`; destructuring does not work with next-intl.

**Locale-aware navigation**: inside `[locale]` code import `Link`, `redirect`, `usePathname`, `useRouter` from `@/i18n/routing` (strips / auto-prefixes the locale; `redirect` takes `{ href, locale }`, not a string). ESLint `no-restricted-imports` blocks only `redirect` / `permanentRedirect` / `useRouter` / `usePathname` from `next/navigation` — a stray `Link` from `next/link` is NOT caught by lint. `next/link` is for external links only.

**Files**: `messages/{locale}/{namespace}.json` — every registered namespace has a file in each of zh-CN / zh-TW / zh-HK.

**Namespace registry**: `messages/namespaces.ts` lists all active namespaces. When adding a new `*.json` namespace file, add its name to the `namespaces` array — otherwise it is never loaded and all keys return their raw key string silently.

> **Ignore that file's own `DO NOT EDIT` banner.** It says to regenerate via
> `node scripts/i18n/split-namespaces.js web`; **that script no longer exists**.
> Hand-editing `namespaces.ts` is the correct and only way to register a namespace.
> (Its `namespaceMapping` table is a separate flat-key→namespace map used by the
> splitter that generated the current layout — leave it alone unless you're moving keys.)

**Message-file shape trap**: `src/i18n/request.ts` stores each file under its namespace (`messages[ns] = file`), so the full key is `{namespace}.{keys inside the file}` — and the files come in three shapes. Single wrapper equal to the namespace (`install.json` = `{install: {...}}` → `t('install.install.windows')`, `InstallClient.tsx`); several sibling wrappers (`hero.json` = `{hero, security, download, routers, faq}` → with `namespace: 'hero'`, `t('hero.title')` is really `hero.hero.title`); flat leaves (`changelog.json`, `releases.json`, `survey.json` → `t('changelog.title')`). A key added at the wrong level in all 3 files passes every test — `src/test/setup.ts` stubs `useTranslations` to identity and `tests/messages-parity.test.ts` only compares locales against zh-CN — and renders as raw key text. Only a real browser render catches it.

**File-level fallback**: `request.ts` loads `messages/zh-CN/{ns}.json` when `{locale}/{ns}.json` is missing.

## Content Publishing (Velite)

Velite compiles `content/{locale}/**/*.md` at build time (`velite.config.ts`: `order` / `section` sidebar fields, `brand: kaitu|overleap|both`). Consumers: `src/app/[locale]/k2/[[...path]]/page.tsx` + `src/lib/k2-posts.ts` (the `k2/` docs), `src/app/[locale]/[...slug]/page.tsx` + `src/lib/content-posts.ts` (everything else, e.g. `guides/`), and `src/app/sitemap.ts`. Velite is the ONLY content pipeline — Payload CMS and its PostgreSQL database were removed 2026-09 (articles migrated to `content/{locale}/guides/*.md`).

- **The `[...slug]` catch-all serves only registered categories**: `contentCategories` in `src/lib/site/kaitu.ts` is the registry (1 segment = category list, 2 = post detail, 3+ = 404) — currently `guides`. A markdown directory without a registry entry has no page — register it AND check the reserved-paths list below.
- **Locale fallback**: a post missing in the requested locale falls back to zh-CN (same pattern as `findK2Post`) — 内容写 zh-CN 为母版；zh-TW / zh-HK 按文件可选。
- **Article markdown may contain inline HTML** (`<strong>`/`<em>`): CJK fullwidth punctuation adjacent to `**` breaks CommonMark emphasis flanking; the velite pipeline preserves raw inline HTML (rehypeRaw). Body starts at `##` — the page template renders the `<h1>` from frontmatter `title`.
- **Import data**: `import { posts } from '#velite'` (tsconfig path + vitest alias → `.velite/`)
- **Images**: `web/public/images/content/` → reference as `/images/content/filename.jpg`
- **Build**: Velite runs alongside Next.js via `process.argv` detection in `next.config.ts`
- **Skill**: `kaitu-content` writes articles as Velite markdown (git commit + deploy publishes them).

**Release notes / Changelog:**
- **Single source of truth**: `web/releases/v{VERSION}.md`
- **Frontmatter**: `version` + `date` (required), plus optional `noDownloads: true` for releases that don't ship app binaries (e.g. router-only or hot-fix bumps that share the previous version's downloads). When set, the `/releases` page hides macOS / Windows / Linux / Android / iOS download buttons for that row.
- **Sections**: `## New Features`, `## Bug Fixes`, `## Improvements`, `## Breaking Changes` (see `web/releases/README.md` for user-focused writing guidelines — user-visible items only, no internal refactors / pure deps bumps / dev-only changes)
- **Generate**: `cd web && node scripts/generate-changelog.js` → produces `public/releases.json`, `public/changelog.json`, `public/changelog.md` — **all three are gitignored** (`web/.gitignore`)
- **Display**: `/releases` page fetches `/releases.json` at runtime
- **Never edit `web/public/{releases,changelog}.{json,md}` directly** — always edit source `.md` under `web/releases/` then regenerate

**K2 protocol docs** (`web/content/{locale}/k2/*.md`):
- Served by `web/src/app/[locale]/k2/[[...path]]/page.tsx` (NOT the `[...slug]` catch-all)
- Sidebar navigation driven by `order` + `section` frontmatter via `getK2Posts(locale)` helper
- `getK2Posts()` is the single source: used by K2Sidebar, K2Page, and sitemap.ts

**Reserved paths** (content category slugs must NOT use — static routes win over `[...slug]`): 403, account, changelog, delete-account, discovery, g, go, install, k2, login, opensource, pay-result, pricing, privacy, purchase, releases, retailer, routers, s, support, survey, terms, manager. (= every directory under `src/app/[locale]/` except `[...slug]` / `__tests__`, plus `manager`.) Extend this line when adding a `[locale]` route, and never register a category with one of these slugs in `content-posts.ts`.

## Routing

| Path pattern | Layout group | Auth | Purpose |
|-------------|-------------|------|---------|
| `/{locale}/*` | `[locale]` | Public/Mixed | User-facing pages |
| `/{locale}/k2/[[...path]]` | `[locale]/k2` | Public | K2 protocol docs (Velite + sidebar) |
| `/{locale}/support` | `[locale]` | Public | Support / FAQ page |
| `/{locale}/{...slug}` | `[locale]` | Public | Velite content: 1 segment = category list, 2 = post detail |
| `/manager/*` | `(manager)` | Admin | Management dashboard |

**Manager routes bypass i18n middleware** — no locale prefix. Chinese-only admin UI.
**Static routes take priority** over the `[...slug]` catch-all (Next.js default behavior). `/k2/[[...path]]` takes priority over `[...slug]` for all `/k2/*` paths.

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

## Environment

See `.env.example` for all variables.

`NEXT_PUBLIC_BRAND` is no longer read (the brand is always kaitu); `next.config.ts` fails the build if it is set to anything else. **`web/.env.production` is a tracked file** (despite matching `.env*` in `.gitignore`) kept as an append target: the real vars are appended by `amplify.yml` at build. Editing it does not configure prod.

## Deployment

The kaitu.io AWS Amplify app builds the `website` branch — a pure mirror of `main`, never committed to directly — with `appRoot: web`. Deploy = `git push origin main:website`. (overleap.io is built from `sites/overleap/` with its own `amplify.yml`.) `amplify.yml` preBuild bakes console env vars into `.env.production` (standalone output does not forward them to the SSR Lambda); `scripts/amplify-prebuild.sh` (the `prebuild` npm script) only runs `scripts/generate-changelog.js`. Sentry: `withSentryConfig` in `next.config.ts` + `src/instrumentation*.ts` + `src/lib/sentry-filters.ts`; DSN via `NEXT_PUBLIC_SENTRY_DSN` (empty = SDK no-ops). AWS resource IDs (S3 / CloudFront / IAM / Route 53): [`docs/ops/web-amplify.md`](../docs/ops/web-amplify.md).

## SEO & GEO

- Every public `[locale]` page exports `generateMetadata()` (title, description, canonical, OG) and is in `sitemap.ts` with hreflang alternates. The rest of the SEO checklist: [`docs/web/seo-guidelines.md`](../docs/web/seo-guidelines.md); GEO content rules: [`docs/marketing/geo-content-rules.md`](../docs/marketing/geo-content-rules.md) (applied by the `kaitu-content` skill).
- Brand keyword consistency: product is「开途」in Chinese copy (standalone "Kaitu" in zh messages fails `tests/messages-integrity.test.ts`); protocol is "k2", congestion control is "k2cc" (not "k2arc"). Never deviate.

## Gotchas

- **Translation keys in every locale**: a key must exist in zh-CN / zh-TW / zh-HK before committing — `tests/messages-parity.test.ts` 逐 namespace 比对。
- **Manager has no i18n**: Admin dashboard is Chinese-only, routes bypass next-intl middleware entirely.
- **API chain linkage**: When modifying Center API endpoints, update `web/src/lib/api.ts` typed methods to match.
- **Velite `.velite/` directory**: Generated at build time, gitignored. Contains `index.js`, `index.d.ts`, `posts.json`. Rebuild with `npx velite build`.
- **Velite mock in tests**: vitest tests mock `#velite` import with synthetic post data. Server Component pages tested by calling as async functions directly, asserting on returned JSX or `generateMetadata()` output.
- **next-intl IntlMessages interface**: `web/src/types/i18n.d.ts` uses an empty `interface IntlMessages {}` (permissive typing) because messages are split across namespace files loaded dynamically. This disables compile-time key checking — use runtime tests instead.
- **Server Component pages with setRequestLocale**: Cast locale to `(typeof routing.locales)[number]` when calling `setRequestLocale()`. The URL param type is `string` but next-intl requires the narrower union type.
- **Homepage is a Server Component**: `web/src/app/[locale]/page.tsx` (no `dynamic` export). Do NOT add `"use client"` — it would break SSR metadata and SEO.
- **Purchase page Server Component pattern**: `purchase/page.tsx` is a Server Component wrapper that exports `generateMetadata()` for SEO. Client-side purchase logic is in `PurchaseClient` (WordGate).
- **Embed mode** (`?embed=true`): Pages embedded in desktop app iframe. `useEmbedMode()` hook controls Header/Footer/CTA visibility. The chat widget (`ChatWidgetLazy`) and `CookieConsent` auto-hide in embed mode. Used by `/releases` and `/changelog` routes.
- **Platform labels in i18n**: Use user-friendly names, not technical ones. iOS → "iPhone / iPad", macOS → "苹果电脑" (zh) / "Mac" (en), Android → "安卓" (zh). No file extensions (.exe/.dmg/.apk) in download button labels.
- **`transpilePackages` in `next.config.ts`** (`intl-messageformat`, `@xterm/xterm`): Next does not transpile `node_modules`; a dep shipping class `static {}` blocks blanks the page on iOS 16.0-16.3 / Safari < 16.4 with `SyntaxError: Unexpected token '{'`. Add new offenders to that list.
- **`/` must stay `Cache-Control: private, no-store`** (`next.config.ts` headers + `middleware.ts`): the root 307 is computed from Accept-Language + `preferredLocale` cookie; a public cache pins the first visitor's locale for a whole CloudFront PoP. The dynamic-page cache rule deliberately matches `.+`, not `.*`, so `/` never falls into it.
- **Middleware passthrough is load-bearing**: `src/middleware.ts` must early-return `NextResponse.next()` for `/manager` before reaching `intlMiddleware`. The matcher deliberately **includes** `/api`, `/app`, `/manager` (`X-K2-Brand` injection must run there), plus the legacy `en-*` / `ja` prefixes (their 301); only `_next`, `_vercel` and dotted static files are excluded by the catch-all. Without the early return, i18n middleware mangles admin requests into locale-prefixed redirects.`tests/middleware.test.ts` covers this — keep it green when editing.
- **`/i/k2s` and `/i/k2r` have a side effect**: `middleware.ts` fire-and-forget POSTs `{ip_raw, ua}` to `https://k2.52j.me/api/stats/{k2s,k2r}-download` before serving the script (`/i/k2` does not report).
