# Web — Kaitu Website + Admin Dashboard

Next.js website serving public marketing pages, user self-service (purchase, account, wallet), and admin management dashboard.

**Separate from yarn workspaces** — has its own `yarn.lock` and `node_modules/`; run `yarn install` inside `web/` (yarn only, never npm). Node >= 22 (`.nvmrc`).

**Long-form notes (not auto-loaded — read before changing that area):** `docs/web/site-notes.md` (Brand full text, ClientMessages background, webapp-boundary rationale, Gotchas full text) · `docs/web/funnel-events.md` (funnel `source` values) · `docs/web/seo-guidelines.md` (SEO checklist).

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

不变量清单；背景与机制全文：`docs/web/site-notes.md`。

- **只服务 kaitu.io**（overleap.io = `sites/overleap/`）。`siteBrand()`（`src/lib/brands.ts`）恒返回 `KAITU`；`NEXT_PUBLIC_BRAND` 设成别的值时 `next.config.ts` **让构建失败**。
- **页面文件就是普通的 `page.tsx` / `layout.tsx`**——`page.kaitu.tsx` **不会被编译**（静默 404，单测照样绿）；`tests/brand-page-tree.test.ts` 守。
- **`BrandId` 联合类型与 `OVERLEAP` 条目别删**：`/manager` 跨品牌（`BrandBadge` / `BrandPicker` 靠它们），`tests/cross-layer-contract.test.ts` 按全量品牌比对 `contracts/api-contract.json`。
- **Locale 清单**（`src/i18n/routing.ts`，`brands.ts` 内联 `ALL_LOCALES`）与 `webapp/src/brands/kaitu/index.ts` 的 `locales` **同一份，改一边必须改另一边**。外语面向在华外国人；旧 en-GB / en-AU 由 `middleware.ts` **301 到 en-US**（matcher 单列）。品牌名走 `localizedWordmark(brand, locale)`：中文「开途」，其余「开途 Kaitu」，不单独写 Kaitu。
- **外语文章**：`listCategoryPosts` 在外语 locale **不列** zh-CN 回落文章；外语指南 = `content/<locale>/guides/*.md` 原创稿，`section: living-in-china`。法务页外语渲染英文母版（`lib/legal.ts`）。
- **Messages 守卫**：`tests/messages-parity.test.ts`（7 个 locale 逐 namespace 对齐 zh-CN，禁未注册目录 / json）；`tests/messages-integrity.test.ts` 政治敏感词禁令覆盖**所有** locale。
- **品牌隔离，没有例外**：kaitu.io 用户面不得出现 Overleap（法务署名 `Wordgate LLC` = `Brand.legalName`，不是品牌名）。守卫 `tests/brand-guard.test.ts`、`tests/brand-leak-ssr.test.tsx`。**品牌字面量只能进 `src/lib/brands.ts`**——注释里也别写展示词（guard 扫整行），用 `overleap` / `kaitu` 小写 id。
- **站点结构只在 `src/lib/site/kaitu.ts`**（`siteConfig()`；只放 key 与路径）。守卫 `tests/site-config-keys.test.ts`、`tests/footer-brand-gates.test.tsx`。
- **Funnel pixel**：`src/lib/funnel.ts` 是**唯一入口**；事件名由 `tests/funnel-contract.test.ts` 锁到 `contracts/api-contract.json`，发射文件登记在 `api/logic_funnel_paths_test.go` `funnelStepEmitSites`（挪文件要同步）。**预览请求不埋**：`checkout_start` 只由真下单发且**必须带套餐 pid**；`plan_select` 默认选中不算。像素的 `u` 只带路径 + `utm_*`（`pixelLocation()`，token 不出浏览器）。`source` 取值：`docs/web/funnel-events.md`。
  - 改埋点范围同步改 `public/legal/privacy-policy.md`（无测试守）；`{{thirdPartyAnalytics}}` ⇔ `Brand.gaMeasurementId` 由 `tests/legal-docs.test.ts` 锁；页脚退出链接必须是 `NavItem.external: true`（next-intl `Link` 会补 locale 前缀）；改 Cookie 横幅文案 bump `COOKIE_CONSENT_VERSION`；`public/legal/*.md` 中英两份母版**都要改**。
- **Manager**：列表/统计页把 `brandParam` 作为 `?brand=` 传给 `/app/*`；品牌自有实体的创建表单**必须用 `BrandPicker` 显式选品牌**（无默认值，编辑只读）；展示名只取 `brandById(id).wordmark`。
- **`X-K2-Brand: kaitu`**: injected on every `/api/*`/`/app/*` request by BOTH `src/lib/api.ts` and middleware. Center resolves Host → header → kaitu (`api/brand.go`).
- **Content `brand:` frontmatter**（velite，默认 both）：标成另一品牌的文章 404、不进 sitemap（`isPostVisibleToBrand`）。
- **`/pricing`**：App 价格是 `lib/site/kaitu.ts` `appPlans` 静态快照（挂载后 `/api/plans` 覆盖）；路由器版价格读 `routerOffer()`，所以 `/routers`、`/pricing` 是 `revalidate = 3600` 而非 `force-static`；价格常量由 `src/lib/__tests__/router-edition.test.ts` 锁定。

## Architecture（非显然的入口）

- `src/app/[locale]/` 公开页（next-intl）；`src/app/(manager)/manager/` 后台（无 locale 前缀、跨品牌、只中文）。
- `src/app/[locale]/k2/[[...path]]/` K2 协议文档（Velite + 侧栏）；`src/app/[locale]/[...slug]/` 其余 Velite 内容（见 Content Publishing）。
- `src/lib/api.ts` API client；`src/lib/api-errors.ts` 错误码 → i18n；`src/lib/constants.ts` `getDownloadLinks` / `getAndroidDownloadLinks`（CDN 下载链接）。
- `src/lib/site/kaitu.ts` 站点结构（nav / footer / sitemap 路由 / 内容分类 / SEO 默认 / 定价快照），经 `siteConfig()` 读。
- `src/lib/k2-posts.ts`（`/k2/` 侧栏）/ `src/lib/content-posts.ts`（`[...slug]`）；`src/lib/events.ts` app 事件总线（`auth:unauthorized`）。
- `src/middleware.ts`：`X-K2-Brand` 注入 + 旧 en-GB/en-AU 301 + next-intl locale 协商（见 Gotchas "Middleware passthrough"）。
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

Locales and the legacy en-GB/en-AU 301 live in the Brand section above; `src/i18n/routing.ts` lists zh-CN / zh-TW / zh-HK / en-US / ko / ja / ru (`defaultLocale: 'zh-CN'`).

**URL format**: `/{locale}/path` (e.g., `/zh-CN/purchase`, `/zh-TW/install`)

**Usage**: `const t = useTranslations()` — NOT `const { t } = useTranslations()`; destructuring does not work with next-intl.

**Locale-aware navigation**: inside `[locale]` code import `Link`, `redirect`, `usePathname`, `useRouter` from `@/i18n/routing` (strips / auto-prefixes the locale; `redirect` takes `{ href, locale }`, not a string). ESLint `no-restricted-imports` blocks only `redirect` / `permanentRedirect` / `useRouter` / `usePathname` from `next/navigation` — a stray `Link` from `next/link` is NOT caught by lint. `next/link` is for external links only.

**Files**: `messages/{locale}/{namespace}.json` — every registered namespace has a file in each of the 7 locales. **File-level fallback**: `request.ts` loads `messages/zh-CN/{ns}.json` when `{locale}/{ns}.json` is missing.

**Namespace registry**: `messages/namespaces.ts` `namespaces` is the full list (`request.ts` loads all of them). A new `*.json` namespace file not added there is never loaded and all its keys silently render as raw key strings. **Ignore that file's own `DO NOT EDIT` banner** — the generator it names no longer exists; hand-editing is the only way (leave `namespaceMapping` alone unless you're moving keys).

**Message-file shape trap**: `src/i18n/request.ts` stores each file under its namespace (`messages[ns] = file`), so the full key is `{namespace}.{keys inside the file}` — and the files come in three shapes. Single wrapper equal to the namespace (`install.json` = `{install: {...}}` → `t('install.install.windows')`, `InstallClient.tsx`); several sibling wrappers (`hero.json` = `{hero, security, download, routers, faq}` → with `namespace: 'hero'`, `t('hero.title')` is really `hero.hero.title`); flat leaves (`changelog.json`, `releases.json`, `survey.json` → `t('changelog.title')`). A key added at the wrong level in every locale's file passes every test — `src/test/setup.ts` stubs `useTranslations` to identity and `tests/messages-parity.test.ts` only compares locales against zh-CN — and renders as raw key text. Only a real browser render catches it.

**客户端只拿自己用得到的翻译**（2026-11-10，Amplify 产物上限 220 MiB；背景见 `docs/web/site-notes.md`）：根布局只给 `NextIntlClientProvider` BASE；需要更多的路由目录在自己的 `layout.tsx` 里 `<ClientMessages locale group="<目录名>">`（`src/i18n/ClientMessages.tsx`；**嵌套 provider 是替换不是合并**，group = BASE ∪ 本组额外）。服务端组件用 `getTranslations`。
- **改了客户端组件用到的键 / 新增带客户端组件的路由目录**：重跑 `node scripts/i18n/gen-client-messages.mjs`（生成 `src/i18n/client-messages.generated.ts`）。`tests/client-messages.test.ts` 会在清单过期、某个 group 没有对应 layout、或某语言缺路径时变红。
- **盲区**：完全在运行时拼出、源码里没有任何字面量的键，静态分析看不到——这种键必须在源码里至少出现一次带前缀的字面量，或者在真实浏览器里走一遍。`useTranslations` 在单测里被桩成恒等，所以单测抓不到；预渲染日志里的 `MISSING_MESSAGE` 要保持为 0。
- 页面能做成服务端组件就别 `"use client"`：纯文章页（隐私、条款、guides、定价）的客户端 messages 是 0 增量。

## Content Publishing (Velite)

Velite compiles `content/{locale}/**/*.md` at build time (`velite.config.ts`: `order` / `section` sidebar fields, `brand: kaitu|overleap|both`). Consumers: `src/app/[locale]/k2/[[...path]]/page.tsx` + `src/lib/k2-posts.ts` (the `k2/` docs), `src/app/[locale]/[...slug]/page.tsx` + `src/lib/content-posts.ts` (everything else, e.g. `guides/`), and `src/app/sitemap.ts`. Velite is the ONLY content pipeline — Payload CMS and its PostgreSQL database were removed 2026-09 (articles migrated to `content/{locale}/guides/*.md`).

- **The `[...slug]` catch-all serves only registered categories**: `contentCategories` in `src/lib/site/kaitu.ts` is the registry (1 segment = category list, 2 = post detail, 3+ = 404) — currently `guides`. A markdown directory without a registry entry has no page — register it AND check the reserved-paths list below.
- **Locale fallback**: a post missing in the requested locale falls back to zh-CN (same pattern as `findK2Post`) — 内容写 zh-CN 为母版；zh-TW / zh-HK 按文件可选。外语 locale 的**列表页**不列回落文章（见 Brand「外语文章」）。
- **Article markdown may contain inline HTML** (`<strong>`/`<em>`): CJK fullwidth punctuation adjacent to `**` breaks CommonMark emphasis flanking; the velite pipeline preserves raw inline HTML (rehypeRaw). Body starts at `##` — the page template renders the `<h1>` from frontmatter `title`.
- **Import data**: `import { posts } from '#velite'` (tsconfig path + vitest alias → `.velite/`)
- **Images**: `web/public/images/content/` → reference as `/images/content/filename.jpg`
- **Build**: Velite runs alongside Next.js via `process.argv` detection in `next.config.ts`
- **Skill**: `kaitu-content` writes articles as Velite markdown (git commit + deploy publishes them).

**Release notes / Changelog:**
- **Single source of truth**: `web/releases/v{VERSION}.md`
- **Frontmatter**: `version` + `date` (required); optional `noDownloads: true` hides that row's download buttons on `/releases` (releases that ship no binaries); `windowsPending: true` hides only its Windows button — set it until the new exe clears SmartScreen (`scripts/CLAUDE.md` 「Windows 上线门」), then delete it. `/install` needs no flag (`lib/downloads.ts` follows the manifest's `platforms.windows-x86_64.url`).
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

边界按「用户此刻装没装 app」划，**不按功能划**（理由全文：`docs/web/site-notes.md`）：

- **website = 还没装 app 的人**（搜到 → 看价格 → 买 → 当场确认 → 下载）。`/account` 只做订阅状态（到期时间、档位、购买记录），设备 / 节点 / 邀请管理永远只在 webapp。
- **唯一的交叉是资金面**（钱包 / 提现）——`webapp/src/App.tsx` 的 `/delegate` 在 iOS 上被摘掉（Apple 3.1.1），所以"都塞进 webapp"在 iOS 上做不到。
- 不变量：**重复的那一份必然先腐烂**——别在本站复刻 webapp 已有的账号功能。
- **例外：路由器版**（`/account/router`），只读 `GET /api/user/router`，不做设备 / 节点管理（spec `2026-09-16-router-edition-web-onboarding-design.md` §4.4）。**自备路由器新购已下线**：`/purchase/router` 只卖含路由器的成品；服务套餐（`hardwareSku` 为空）仅在 `?plan=svc` + 已登录 + 有可续路由器版时作为续费出现。

## Environment

See `.env.example` for all variables.

`NEXT_PUBLIC_BRAND` is no longer read (the brand is always kaitu); `next.config.ts` fails the build if it is set to anything else. **`web/.env.production` is a tracked file** (despite matching `.env*` in `.gitignore`) kept as an append target: the real vars are appended by `amplify.yml` at build. Editing it does not configure prod.

## Deployment

The kaitu.io AWS Amplify app builds the `website` branch — a pure mirror of `main`, never committed to directly — with `appRoot: web`. Deploy = `git push origin main:website`. `amplify.yml` preBuild bakes console env vars into `.env.production` (standalone output does not forward them to the SSR Lambda); `scripts/amplify-prebuild.sh` (the `prebuild` npm script) only runs `scripts/generate-changelog.js`. Sentry: `withSentryConfig` in `next.config.ts` + `src/instrumentation*.ts` + `src/lib/sentry-filters.ts`; DSN via `NEXT_PUBLIC_SENTRY_DSN` (empty = SDK no-ops). AWS resource IDs: [`docs/ops/web-amplify.md`](../docs/ops/web-amplify.md).

## SEO & GEO

- Every public `[locale]` page exports `generateMetadata()` (title, description, canonical, OG) and is in `sitemap.ts` with hreflang alternates. **hreflang 只有一个来源 `lib/hreflang.ts` `hreflangAlternates()`**——页面 `<link rel=alternate>` 与 sitemap 条目都调它（两边不一致 = 给搜索引擎矛盾信号）：en-US 广播为 `en`，另加 `zh`→zh-CN，x-default = `Brand.xDefaultLocale`（en-US，与根路径不认识语言时落英文一致），页面没有英文版时退回 zh-CN。正文是纯中文内容的静态路由登记在 `siteConfig().chineseOnlyRoutes`（现为 `/releases`）：只进中文 sitemap，外语 URL noindex + canonical zh-CN。 The rest of the SEO checklist: [`docs/web/seo-guidelines.md`](../docs/web/seo-guidelines.md); GEO content rules: [`docs/marketing/geo-content-rules.md`](../docs/marketing/geo-content-rules.md) (applied by the `kaitu-content` skill).
- Brand keyword consistency: product is「开途」in Chinese copy (standalone "Kaitu" in zh messages fails `tests/messages-integrity.test.ts`); protocol is "k2", congestion control is "k2cc" (not "k2arc"). Never deviate.

## Gotchas

Full text of the shortened bullets: `docs/web/site-notes.md` "Gotchas".

- **Translation keys in every locale**: a key must exist in all 7 locales before committing — `tests/messages-parity.test.ts` 逐 namespace 比对。
- **Manager has no i18n**: Admin dashboard is Chinese-only, routes bypass next-intl middleware entirely.
- **API chain linkage**: When modifying Center API endpoints, update `web/src/lib/api.ts` typed methods to match.
- **Velite `.velite/` directory**: Generated at build time, gitignored. Contains `index.js`, `index.d.ts`, `posts.json`. Rebuild with `npx velite build`.
- **Velite mock in tests**: vitest tests mock `#velite`; Server Component pages are tested by calling them as async functions directly.
- **next-intl IntlMessages interface**: `web/src/types/i18n.d.ts` uses an empty `interface IntlMessages {}` — no compile-time key checking; use runtime tests.
- **Server Component pages with setRequestLocale**: Cast locale to `(typeof routing.locales)[number]` when calling `setRequestLocale()`.
- **Homepage is a Server Component**: `web/src/app/[locale]/page.tsx`. Do NOT add `"use client"` — it would break SSR metadata and SEO. Same pattern for `purchase/page.tsx` (client logic in `PurchaseClient`).
- **Embed mode** (`?embed=true`): `useEmbedMode()` hides Header/Footer/CTA; `ChatWidgetLazy` and `CookieConsent` auto-hide. Used by `/releases` and `/changelog`.
- **Platform labels in i18n**: Use user-friendly names, not technical ones. iOS → "iPhone / iPad", macOS → "苹果电脑" (zh) / "Mac" (en), Android → "安卓" (zh). No file extensions (.exe/.dmg/.apk) in download button labels.
- **`transpilePackages` in `next.config.ts`** (`intl-messageformat`, `@xterm/xterm`): Next does not transpile `node_modules`; a dep shipping class `static {}` blocks blanks the page on iOS 16.0-16.3 / Safari < 16.4 with `SyntaxError: Unexpected token '{'`. Add new offenders to that list.
- **`/` must stay `Cache-Control: private, no-store`** (`next.config.ts` headers + `middleware.ts`): the root 307 is computed from Accept-Language + `preferredLocale` cookie; a public cache pins the first visitor's locale for a whole CloudFront PoP. The dynamic-page cache rule deliberately matches `.+`, not `.*`, so `/` never falls into it.
- **Middleware passthrough is load-bearing**: `src/middleware.ts` must early-return `NextResponse.next()` for `/manager` before reaching `intlMiddleware`. The matcher deliberately **includes** `/api`, `/app`, `/manager` (`X-K2-Brand` injection must run there), plus the legacy `en-GB` / `en-AU` prefixes (their 301); only `_next`, `_vercel` and dotted static files are excluded by the catch-all. Without the early return, i18n middleware mangles admin requests into locale-prefixed redirects. `tests/middleware.test.ts` covers this — keep it green when editing.
- **`/i/k2s` and `/i/k2r` have a side effect**: `middleware.ts` fire-and-forget POSTs `{ip_raw, ua}` to `https://k2.52j.me/api/stats/{k2s,k2r}-download` before serving the script (`/i/k2` does not report).
