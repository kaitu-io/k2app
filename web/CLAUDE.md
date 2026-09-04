# Web — Kaitu Website + Admin Dashboard

Next.js website serving public marketing pages, user self-service (purchase, account, wallet), and admin management dashboard.

**Separate from yarn workspaces** — has its own `yarn.lock` and `node_modules/`. Not part of the root workspace.

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

## Tech Stack

Next.js 15 (App Router) | React 19 | TypeScript | Tailwind CSS 4 | shadcn/ui | next-intl | Velite (content)

## Brand (双品牌拆分 Phase 2: 一套代码，两个部署)

- **Build-time baking**: `NEXT_PUBLIC_BRAND=kaitu|overleap` (default kaitu) → `siteBrand()` in `src/lib/brands.ts` — the ONLY brand source. No host/locale-based runtime resolution; no cross-domain 301/hreflang/canonical. Two Amplify apps (kaitu.io / overleap.io) build from the same repo with different env.
- **页面树按品牌编译（`pageExtensions`）**：`next.config.ts` 按品牌取 `[`${brand}.tsx`, 'tsx']` —— `page.tsx` / `layout.tsx` 两品牌共用，`page.kaitu.tsx` 只进开途构建（Overleap 构建里该路径**不存在**，原生 404，无需中间件门），`page.overleap.tsx` 只进 Overleap 构建。同一目录**不得**同时有 `page.tsx` 与 `page.<brand>.tsx`（那一品牌构建失败、另一品牌是绿的）。开途独有：`(manager)` 整树、discovery / opensource / routers / retailer / releases / changelog / g / s / survey / account/wallet / account/delegate；分裂页：首页 / install / support / purchase / account。守卫 `tests/brand-page-tree.test.ts`。**不要再写 `if (siteBrand().id === 'overleap')` 分流**——加页面就加 `page.<brand>.tsx`。
- **站点结构按品牌配置（`src/lib/site/<brand>.ts`）**：导航、页脚栏目、sitemap 静态路由、内容分类、SEO 默认 title/description、（Overleap）首页价表。Header / Footer / sitemap / content-posts / metadata 只读配置渲染；配置里只放 key 与路径，不放品牌展示词（`mailto:{contactEmail}` 由 Footer 用 `Brand.contactEmail` 填）。守卫：`tests/site-config-keys.test.ts`（每个 key 在该品牌全部 locale 存在）、`tests/footer-brand-gates.test.tsx`（开途一条不少 / Overleap 一条不多）。
- **Locale matrix**: kaitu → zh-CN (default)/zh-TW/zh-HK; overleap → **en-GB (default, 英式母版)**/en-US/en-AU/ja. Off-brand locale paths 301 to the brand default locale, same host; a bare `Accept-Language: en` lands on the brand default. Message files partition by locale: en/ja files say "Overleap", zh files say「开途」— never mix (enforced by `tests/brand-guard.test.ts`). **k2 文档的英文母版在 `content/en-GB/k2/`**（回落是"品牌默认语言"）。
- **Namespace 按品牌（`messages/namespaces.ts` `BRAND_NAMESPACES`）**：`request.ts` 只加载本品牌集合，缺文件回落**品牌默认语言**（不是 zh-CN）。共享集 common/nav/auth/purchase/account/discovery/errors/k2/admin（`admin` 因共享 Header / ChangePasswordDialog 用到 `admin.account.*`）；开途独有 hero/install/wallet/campaigns/invite/theme/changelog/releases/routers/guide-parents/licenseKeys/survey（只有 zh-* 有文件）；Overleap 独有 landing/download/help（只有 en-*/ja 有文件）。`tests/messages-parity.test.ts` 按品牌比对（base = 品牌默认语言），并断言任一品牌的 locale 目录下没有另一品牌的 namespace 文件。
- **Admin API is kaitu-only**: `/app/*` proxy → 404 on the overleap build (middleware). `/manager` 页面本身是 `.kaitu.tsx`，Overleap 构建没有这些路由。
- **`X-K2-Brand`**: injected on every `/api/*`/`/app/*` request by BOTH `src/lib/api.ts` and middleware. Center resolves Host → header → kaitu (`api/brand.go`).
- **Brand-leak guards**: `tests/brand-guard.test.ts` (file scan: messages per-locale + src allowlist + velite content) and `tests/brand-leak-ssr.test.tsx` (rendered chrome + metadata per brand page tree). `github.com/getoverleap` is the allow-listed protocol-layer org. Never add brand literals to src — extend the `Brand` registry instead. **注释里也不能写 Overleap / 开途 展示词**（guard 扫整行）——用 `overleap` / `kaitu` 小写 id。
- **Legal signature is the ONE cross-brand exception**: both brands sign 法务文书 as `Overleap LLC` (`Brand.legalName`, rendered by `Footer`). The SSR guard strips it before scanning and asserts it is present, so the exception stays scoped.
- **Feature gates**: `Brand.features` — routers/linuxInstall/androidApkGuide/releaseNotes/retailerProgram 描述品牌事实，开途独有页里的 `notFound()` 守卫保留；页脚 / sitemap **不再**靠它们条件渲染（由站点结构配置决定）。`Brand.storeLinks` 为空 = 未上架，下载页显示 Coming soon。
- **Kaitu-only content**: velite markdown that documents kaitu-only surfaces (e.g. the `/i/k2*` install scripts) carries `brand: kaitu` frontmatter — the sitemap and the guard both honour it.
- **Dev**: `yarn dev` (kaitu) / `yarn dev:overleap`; builds: `yarn build` / `yarn build:overleap`（**两个 build 都要过**——页面树不同，一边绿不代表另一边绿）。
- **Overleap 首页 / 下载页 / 帮助页**：`page.overleap.tsx` + `components/home-overleap/`、`components/install-overleap/`；文案 namespace `landing` / `download` / `help`，品牌名 `{brand}` 插值。隐私优先叙事（spec `2026-09-04-overleap-site-decoupling-and-uk-positioning-design.md` §3）。守卫：`tests/landing-overleap-ssr.test.tsx`、`tests/overleap-pages-ssr.test.tsx`（真实文案渲染，零开途词/零中国支付渠道/零原始 key/零未填占位）。en/ja 文案只有 Overleap 部署会读，`tests/messages-integrity.test.ts` 禁止其中出现支付宝/微信支付/银联。
- **定价三处同源**：Stripe Price 由 `scripts/stripe-setup-overleap.sh` 的 `ensure_price` 行创建（USD 主币 + GBP/EUR 固定本币价，Checkout 按属地自动选币）；首页静态价表在 `lib/site/overleap.ts`（`tests/pricing-source.test.ts` 锁两者逐币种相等）；购买页走 API `currencyPrices`。展示币由 `lib/pricing.ts` 按 locale 决定（en-GB 英镑，其余美元），价格数字**不进文案文件**（`{yearly}` `{monthly}` `{currency}` 插值）。改价 = 改脚本 + 改价表 + 跑脚本 + 回填 Plan 行，缺一处测试红。
- **主题按 `html[data-brand]` 覆盖**：`globals.css` 的 `:root` 是开途，`html[data-brand="overleap"]` 块必须覆盖全部变量（`tests/brand-theme-css.test.ts`）。**二进制资产也有守卫**：`tests/brand-assets.test.ts` 对 `public/` 里两品牌的 logo/og/favicon 算哈希，任何一对相同即红——Overleap 资产由 `webapp/brand-assets/overleap/generate.sh` 从 `logo.svg` 生成。

## Architecture

```
web/
├── src/
│   ├── app/
│   │   ├── [locale]/          # Public pages with i18n (next-intl)
│   │   │   ├── page.kaitu.tsx / page.overleap.tsx  # Home（按品牌编译，见 Brand 段）
│   │   │   ├── install/       # Download page（page.kaitu.tsx / page.overleap.tsx）
│   │   │   ├── purchase/      # Subscription purchase flow
│   │   │   ├── account/       # User profile, members, delegate, wallet
│   │   │   ├── discovery/     # App discovery
│   │   │   ├── releases/      # Version history + downloads (GitHub Releases style)
│   │   │   ├── changelog/     # Redirects to /releases (backward compat)
│   │   │   ├── login/         # Email OTP login
│   │   │   ├── support/       # 开途家长指南 / Overleap Help（page.<brand>.tsx）
│   │   │   ├── s/[code]/      # Invite link landing
│   │   │   ├── k2/[[...path]]/ # K2 protocol docs section (Velite + sidebar layout)
│   │   │   ├── [...slug]/     # Catch-all content pages (Velite markdown)
│   │   │   └── ...            # privacy, terms, routers, opensource
│   │   ├── (manager)/         # Admin dashboard (no locale prefix) — 整树 .kaitu.tsx
│   │   │   └── manager/       # /manager/* routes
│   │   │       ├── users/     # User management + detail
│   │   │       ├── orders/    # Order list
│   │   │       ├── nodes/     # Node matrix, SSH terminal, batch ops (tunnels shown inline per node)
│   │   │       ├── cloud/     # Cloud instance management
│   │   │       ├── approvals/  # Approval management (maker-checker)
│   │   │       ├── campaigns/ # Campaign management
│   │   │       ├── edm/       # Email marketing (templates + tasks + logs)
│   │   │       ├── license-keys/ # License key list (browse, filter by batch)
│   │   │       ├── license-key-batches/ # License key batch management (CRUD, stats, conversion tracking)
│   │   │       ├── retailers/ # Retailer CRM (notes, todos, levels)
│   │   │       ├── tickets/   # Support ticket management
│   │   │       ├── usages/    # Usage statistics
│   │   │       ├── withdraws/ # Withdraw approval
│   │   │       ├── plans/     # Subscription plan config
│   │   │       ├── announcements/ # 公告管理
│   │   │       ├── surveys/   # 问卷统计
│   │   │       ├── enterprise/ # 企业路由器
│   │   │       ├── node-operations/ # 节点运维
│   │   │       └── asynqmon/  # Asynq queue monitor (iframe)
│   ├── components/
│   │   ├── ui/                # shadcn/ui primitives (button, dialog, table, etc.)
│   │   ├── providers/         # LocaleProvider, EmbedThemeProvider
│   │   └── ...                # Feature components (Header, Footer, EmailLogin, etc.)
│   ├── contexts/              # AuthContext, AppConfigContext
│   ├── hooks/                 # useEmbedMode
│   ├── i18n/                  # next-intl routing + request config
│   ├── lib/
│   │   ├── api.ts             # API client (types + request methods + error handling)
│   │   ├── auth.ts            # JWT decode helpers
│   │   ├── constants.ts       # getDownloadLinks / getAndroidDownloadLinks (brand-aware CDN URLs)
│   │   ├── device-detection.ts # Device type detection for auto-download
│   │   ├── events.ts          # App event bus (auth:unauthorized, etc.)
│   │   ├── k2-posts.ts        # getK2Posts(locale) — Velite filter/group/sort for /k2/ sidebar
│   │   ├── content-posts.ts   # Velite lookup for the [...slug] catch-all（分类注册在 lib/site/<brand>.ts）
│   │   ├── site/              # 站点结构按品牌：nav / footer / sitemap 路由 / 内容分类 / SEO 默认 / 价表
│   │   ├── pricing.ts         # 展示币种（en-GB 英镑，其余美元）+ Intl 金额格式
│   │   ├── api-errors.ts      # Error code→i18n mapping (getApiErrorMessage + getApiErrorMessageZh)
│   │   ├── udid.ts            # Device fingerprint
│   │   └── utils.ts           # cn() helper (clsx + tailwind-merge)
│   └── middleware.ts          # brand gating + X-K2-Brand injection + next-intl locale detection
├── content/{locale}/          # Velite markdown: k2/ docs（英文母版 en-GB，中文母版 zh-CN）+ guides/（zh，开途）+ blog/（en，Overleap）
├── velite.config.ts           # Velite schema + collection config (order/section fields)
├── messages/                  # i18n JSON files（按品牌划分，见 Brand 段）
│   └── namespaces.ts          # Namespace registry + BRAND_NAMESPACES — hand-edit when adding new *.json files
├── tests/                     # Playwright E2E specs + vitest + build tests
└── public/                    # Static assets, legal docs, app icons
```

## API Integration

### API Client (`src/lib/api.ts`)

Single `api` object with typed methods returning unwrapped `data`. Envelope is the Center API's (`ApiResponse<T>` in api.ts): HTTP 200 always, `code` 0 = success, `message` is debug text — never show it to users. HttpOnly cookie auth (server-managed) with CSRF protection.

### API Proxy (Next.js → Center API)

`/api/*` and `/app/*` are proxied by `rewrites()` in `next.config.ts` — in every environment, dev and Amplify alike (default upstream `https://k2.52j.me`). `API_PROXY_TARGET=http://127.0.0.1:5899 yarn dev` points at a local Center; it is not in `.env.example`. Middleware injects `X-K2-Brand` on both prefixes and 404s `/app/*` on the overleap build.

### Error Handling

`ApiError` class with error codes matching `api/response.go`. On 401, emits `auth:unauthorized` event and auto-redirects to login (configurable via `autoRedirectToAuth` option).

Error code-to-i18n mapping lives in `lib/api-errors.ts`. Use `getApiErrorMessage(code, t)` in public `[locale]` pages and `getApiErrorMessageZh(code)` in manager pages. Never show `error.message` to users — it contains raw backend debug text.

## Authentication

- **Web auth**: HttpOnly cookie (`access_token`) + CSRF token. Cookies sent via `credentials: 'include'`.
- **Embed mode**: Bearer token in `localStorage` for iframe embedding.
- **Manager auth**: Same cookie auth. Admin role checked by Center API middleware.
- **Token refresh**: Server-side sliding expiration (< 7 days remaining → auto-renew). No client-side refresh.

## i18n (next-intl)

Locale ↔ brand matrix and the off-brand 301 live in the Brand section above; `src/i18n/routing.ts` lists all 7 (`defaultLocale: 'zh-CN'`).

**URL format**: `/{locale}/path` (e.g., `/zh-CN/purchase`, `/en-US/install`)

**Usage**: `const t = useTranslations()` — NOT `const { t } = useTranslations()`; destructuring does not work with next-intl.

**Locale-aware navigation**: inside `[locale]` code import `Link`, `redirect`, `usePathname`, `useRouter` from `@/i18n/routing` (strips / auto-prefixes the locale; `redirect` takes `{ href, locale }`, not a string). ESLint `no-restricted-imports` blocks only `redirect` / `permanentRedirect` / `useRouter` / `usePathname` from `next/navigation` — a stray `Link` from `next/link` is NOT caught by lint. `next/link` is for external links only.

**Files**: `messages/{locale}/{namespace}.json` — namespaces 按品牌划分（`BRAND_NAMESPACES`，见 Brand 段）：一个品牌独有的 namespace 只在该品牌的 locale 目录里有文件。

**Namespace registry**: `messages/namespaces.ts` lists all active namespaces. When adding a new `*.json` namespace file, add its name to the `namespaces` array — otherwise it is never loaded and all keys return their raw key string silently.

> **Ignore that file's own `DO NOT EDIT` banner.** It says to regenerate via
> `node scripts/i18n/split-namespaces.js web`; **that script no longer exists**.
> Hand-editing `namespaces.ts` is the correct and only way to register a namespace.
> (Its `namespaceMapping` table is a separate flat-key→namespace map used by the
> splitter that generated the current layout — leave it alone unless you're moving keys.)

**Message-file shape trap**: `src/i18n/request.ts` stores each file under its namespace (`messages[ns] = file`), so the full key is `{namespace}.{keys inside the file}` — and the files come in three shapes. Single wrapper equal to the namespace (`install.json` = `{install: {...}}` → `t('install.install.windows')`, `InstallClient.tsx`); several sibling wrappers (`hero.json` = `{hero, security, download, routers, faq}` → with `namespace: 'hero'`, `t('hero.title')` is really `hero.hero.title`); flat leaves (`changelog.json`, `releases.json`, `survey.json` → `t('changelog.title')`). A key added at the wrong level in all 7 files passes every test — `src/test/setup.ts` stubs `useTranslations` to identity and `tests/messages-parity.test.ts` only compares locales against en-US — and renders as raw key text. Only a real browser render catches it.

**File-level fallback**: `request.ts` loads `messages/{brand.defaultLocale}/{ns}.json` when `{locale}/{ns}.json` is missing — 同品牌同语系回落，永不回落到另一品牌的 locale。

## Content Publishing (Velite)

Velite compiles `content/{locale}/**/*.md` at build time (`velite.config.ts`: `order` / `section` sidebar fields, `brand: kaitu|overleap|both`, `canonicalBrand`). Consumers: `src/app/[locale]/k2/[[...path]]/page.tsx` + `src/lib/k2-posts.ts` (the `k2/` docs), `src/app/[locale]/[...slug]/page.tsx` + `src/lib/content-posts.ts` (everything else, e.g. `guides/`), and `src/app/sitemap.ts`. Velite is the ONLY content pipeline — Payload CMS and its PostgreSQL database were removed 2026-09 (articles migrated to `content/{locale}/guides/*.md`).

- **The `[...slug]` catch-all serves only registered categories**: `contentCategories` in `src/lib/site/<brand>.ts` is the per-brand registry (1 segment = category list, 2 = post detail, 3+ = 404) — 开途 `guides`，Overleap `blog`. A markdown directory without a registry entry has no page — register it AND check the reserved-paths list below.
- **Locale fallback**: a post missing in the requested locale falls back to the brand default locale (same pattern as `findK2Post`) — 开途内容写 zh-CN、Overleap 内容写 en-GB 为母版；其余 locale 按文件可选。
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

**Reserved paths** (content category slugs must NOT use — static routes win over `[...slug]`): 403, account, changelog, discovery, g, install, k2, login, opensource, privacy, purchase, releases, retailer, routers, s, support, survey, terms, manager. Extend this line when adding a `[locale]` route, and never register a category with one of these slugs in `content-posts.ts`.

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

不变量：**重复的那一份必然先腐烂**（2026-04-22 `fc5aa0d7` 删「成员管理」把 `/account` 删成空壳、`/g/[code]` 的「查看账号」随之落空即是例子；`getProHistories` 现已回到 `account/KaituAccountClient.tsx` 的购买记录）。

## Environment

See `.env.example` for all variables.

`NEXT_PUBLIC_BRAND=kaitu|overleap` (default `kaitu`) bakes the deployment brand at build time — set per Amplify app, appended to `.env.production` by `amplify.yml` and inlined into client bundles. **`web/.env.production` is a tracked file** (despite matching `.env*` in `.gitignore`) kept as an append target: the real vars are appended by `amplify.yml` at build. Editing it does not configure prod.

## Deployment

Two AWS Amplify apps (one per brand) build the `website` branch — a pure mirror of `main`, never committed to directly — from the same `amplify.yml`; they differ only by `NEXT_PUBLIC_BRAND` (kaitu.io / overleap.io). Deploy = `git push origin main:website`. `amplify.yml` preBuild bakes console env vars into `.env.production` (standalone output does not forward them to the SSR Lambda); `scripts/amplify-prebuild.sh` (the `prebuild` npm script) only runs `scripts/generate-changelog.js`. Sentry: `withSentryConfig` in `next.config.ts` + `src/instrumentation*.ts` + `src/lib/sentry-filters.ts`; DSN via `NEXT_PUBLIC_SENTRY_DSN` (empty = SDK no-ops). AWS resource IDs (S3 / CloudFront / IAM / Route 53): [`docs/ops/web-amplify.md`](../docs/ops/web-amplify.md).

## SEO & GEO Constitutional Rules

### SEO (Search Engine Optimization)

Every public `[locale]` page MUST follow these rules. Violations directly harm organic traffic.

**Technical SEO:**
- Every public page must export `generateMetadata()` returning title, description, canonical URL, and Open Graph tags. No page ships without metadata.
- Structured data (JSON-LD) required on all public pages: `Organization` (footer/layout), `SoftwareApplication` (install), `FAQPage` (support/guides), `BreadcrumbList` (content pages).
- `sitemap.ts` must include all locale variants with `hreflang` alternates. New public routes must be added to sitemap.
- Images must use `next/image` (auto WebP/AVIF, lazy loading) with descriptive `alt` text. Never use raw `<img>`.
- Heading hierarchy must be strict: one `<h1>` per page, `<h2>` > `<h3>` nested logically. Never skip levels.
- Meta descriptions: 120-160 characters, include primary keyword naturally. No keyword stuffing.
- URLs must be semantic English short words (`/install`, `/purchase`, `/support`). No version suffixes, no IDs.
- Internal linking: every public page reachable within 3 clicks from homepage.

**Content SEO:**
- Page titles follow pattern: `{Page Topic} | {brand.displayName}` —「开途」on zh, Overleap on en/ja. Max 60 characters.
- Every content page (Velite markdown) must have frontmatter with `title`, `description`. Description used for meta.
- Brand keyword consistency: product is `brand.displayName` — "Overleap" on en/ja, "开途" on zh (standalone "Kaitu" in en/ja messages fails `tests/messages-integrity.test.ts`); protocol is "k2", congestion control is "k2cc". Never deviate.

### GEO (AI search)

Content-writing rules (citable facts, FAQPage JSON-LD, semantic `<table>`, direct-answer-first) live in [`docs/marketing/geo-content-rules.md`](../docs/marketing/geo-content-rules.md); the `kaitu-content` skill applies them.

## Gotchas

- **Translation keys in ALL locales**: Every key must exist in all 7 locale JSON files before committing.
- **API response pattern**: Same as Center API — check `code` field, not HTTP status. Never show `message` to users.
- **Manager has no i18n**: Admin dashboard is Chinese-only, routes bypass next-intl middleware entirely.
- **Package manager**: Must use `yarn` exclusively (not npm).
- **Separate from workspaces**: `web/` has its own `yarn.lock`. Run `yarn install` inside `web/`, not from root.
- **Node version**: Requires Node >= 22 (see `.nvmrc`).
- **API chain linkage**: When modifying Center API endpoints, update `web/src/lib/api.ts` typed methods to match.
- **Velite `.velite/` directory**: Generated at build time, gitignored. Contains `index.js`, `index.d.ts`, `posts.json`. Rebuild with `npx velite build`.
- **Content prose styling**: Uses `@tailwindcss/typography` — article content rendered with `prose dark:prose-invert` classes.
- **Velite mock in tests**: vitest tests mock `#velite` import with synthetic post data. Server Component pages tested by calling as async functions directly, asserting on returned JSX or `generateMetadata()` output.
- **next-intl IntlMessages interface**: `web/src/types/i18n.d.ts` uses an empty `interface IntlMessages {}` (permissive typing) because messages are split across namespace files loaded dynamically. This disables compile-time key checking — use runtime tests instead.
- **Server Component pages with setRequestLocale**: Cast locale to `(typeof routing.locales)[number]` when calling `setRequestLocale()`. The URL param type is `string` but next-intl requires the narrower union type.
- **Homepage is a Server Component**: `web/src/app/[locale]/page.kaitu.tsx` / `page.overleap.tsx` (no `dynamic` export). Do NOT add `"use client"` — it would break SSR metadata and SEO.
- **k2cc protocol naming**: Protocol brand name is "k2cc" (congestion control), NOT "k2arc". Renamed in commit 80330ec for SEO clarity (avoids amateur radio / math formula collisions). All i18n, content, and JSON-LD reflect this.
- **Purchase page Server Component pattern**: `purchase/page.kaitu.tsx` / `page.overleap.tsx` are Server Component wrappers that export `generateMetadata()` for SEO. Client-side purchase logic is in `PurchaseClient` (WordGate) / `OverleapPurchaseClient` (Stripe).
- **Embed mode** (`?embed=true`): Pages embedded in desktop app iframe. `useEmbedMode()` hook controls Header/Footer/CTA visibility. `ChatwootWidget` and `CookieConsent` auto-hide in embed mode. Used by `/releases` and `/changelog` routes.
- **Platform labels in i18n**: Use user-friendly names, not technical ones. iOS → "iPhone / iPad", macOS → "苹果电脑" (zh) / "Mac" (en), Android → "安卓" (zh). No file extensions (.exe/.dmg/.apk) in download button labels.
- **`transpilePackages` in `next.config.ts`** (`intl-messageformat`, `@xterm/xterm`): Next does not transpile `node_modules`; a dep shipping class `static {}` blocks blanks the page on iOS 16.0-16.3 / Safari < 16.4 with `SyntaxError: Unexpected token '{'`. Add new offenders to that list.
- **`/` must stay `Cache-Control: private, no-store`** (`next.config.ts` headers + `middleware.ts`): the root 307 is computed from Accept-Language + `preferredLocale` cookie; a public cache pins the first visitor's locale for a whole CloudFront PoP. The dynamic-page cache rule deliberately matches `.+`, not `.*`, so `/` never falls into it.
- **Middleware passthrough is load-bearing**: `src/middleware.ts` must early-return `NextResponse.next()` for `/manager` before reaching `intlMiddleware`. The matcher deliberately **includes** `/api`, `/app`, `/manager` (`/app/*` brand gating + `X-K2-Brand` injection must run there); only `_next`, `_vercel` and dotted static files are excluded. Without the early return, i18n middleware mangles admin requests into locale-prefixed redirects. `/manager` 在 Overleap 构建里不存在（`.kaitu.tsx`），中间件不再挡它。`tests/middleware.test.ts` covers this — keep it green when editing.
- **`/i/k2s` and `/i/k2r` have a side effect**: `middleware.ts` fire-and-forget POSTs `{ip_raw, ua}` to `https://k2.52j.me/api/stats/{k2s,k2r}-download` before serving the script (kaitu-only; `/i/k2` does not report).

## Related Docs

- [Root Architecture](../CLAUDE.md)
- [Center API](../api/CLAUDE.md) — Backend endpoints consumed by `api.ts`
- [Webapp Frontend](../webapp/CLAUDE.md) — Separate in-app UI (different tech stack: MUI, React Router, Zustand)
