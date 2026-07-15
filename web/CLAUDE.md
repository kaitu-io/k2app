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
- **Locale matrix**: kaitu → zh-CN (default)/zh-TW/zh-HK; overleap → en-US (default)/en-GB/en-AU/ja. Off-brand locale paths 301 to the brand default locale, same host. Message files partition by locale: en/ja files say "Overleap", zh files say「开途」— never mix (enforced by `tests/brand-guard.test.ts`).
- **Admin is kaitu-only**: `/manager`, `/payload`, `/admin`, `/app/*` proxy → 404 on the overleap build (middleware).
- **`X-K2-Brand`**: injected on every `/api/*`/`/app/*` request by BOTH `src/lib/api.ts` and middleware. Center resolves Host → header → kaitu (`api/brand.go`).
- **Brand-leak guards**: `tests/brand-guard.test.ts` (file scan: messages per-locale + src allowlist + velite content) and `tests/brand-leak-ssr.test.tsx` (rendered chrome). `github.com/getoverleap` is the allow-listed protocol-layer org. Never add brand literals to src — extend the `Brand` registry instead.
- **Legal signature is the ONE cross-brand exception**: both brands sign 法务文书 as `Overleap LLC` (`Brand.legalName`, rendered by `Footer`). The SSR guard strips it before scanning and asserts it is present, so the exception stays scoped.
- **Feature gates**: `Brand.features` — routers/linuxInstall/androidApkGuide are kaitu-only surfaces. A gated surface must be gated everywhere it can be reached: page (`notFound()`), navigation tile, AND sitemap.
- **Kaitu-only content**: velite markdown that documents kaitu-only surfaces (e.g. the `/i/k2*` install scripts) carries `brand: kaitu` frontmatter — the sitemap and the guard both honour it.
- **Dev**: `yarn dev` (kaitu) / `yarn dev:overleap`; builds: `yarn build` / `yarn build:overleap`.

## Architecture

```
web/
├── src/
│   ├── app/
│   │   ├── [locale]/          # Public pages with i18n (next-intl)
│   │   │   ├── page.tsx       # Home / hero
│   │   │   ├── install/       # Download page
│   │   │   ├── purchase/      # Subscription purchase flow
│   │   │   ├── account/       # User profile, members, delegate, wallet
│   │   │   ├── discovery/     # App discovery
│   │   │   ├── releases/      # Version history + downloads (GitHub Releases style)
│   │   │   ├── changelog/     # Redirects to /releases (backward compat)
│   │   │   ├── login/         # Email OTP login
│   │   │   ├── support/       # Support / FAQ page
│   │   │   ├── s/[code]/      # Invite link landing
│   │   │   ├── k2/[[...path]]/ # K2 protocol docs section (Velite + sidebar layout)
│   │   │   ├── [...slug]/     # Catch-all content pages (Velite markdown)
│   │   │   └── ...            # privacy, terms, routers, opensource
│   │   ├── (manager)/         # Admin dashboard (no locale prefix)
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
│   │   ├── constants.ts       # Shared constants (BETA_VERSION, DESKTOP_VERSION, getDownloadLinks)
│   │   ├── device-detection.ts # Device type detection for auto-download
│   │   ├── events.ts          # App event bus (auth:unauthorized, etc.)
│   │   ├── k2-posts.ts        # getK2Posts(locale) — Velite filter/group/sort for /k2/ sidebar
│   │   ├── api-errors.ts      # Error code→i18n mapping (getApiErrorMessage + getApiErrorMessageZh)
│   │   ├── udid.ts            # Device fingerprint
│   │   └── utils.ts           # cn() helper (clsx + tailwind-merge)
│   └── middleware.ts          # next-intl locale detection + manager bypass
├── content/                   # Markdown content files (Velite)
│   ├── zh-CN/                 # Chinese content (primary)
│   │   └── k2/                # K2 protocol docs (zh-CN)
│   └── en-US/                 # English content (fallback to zh-CN)
│       └── k2/                # K2 protocol docs (en-US)
├── velite.config.ts           # Velite schema + collection config (order/section fields)
├── messages/                  # i18n JSON files (7 locales × 16 namespaces)
│   └── namespaces.ts          # Namespace registry — update when adding new *.json files
├── tests/                     # Playwright E2E specs + vitest + build tests
└── public/                    # Static assets, legal docs, app icons
```

## API Integration

### API Client (`src/lib/api.ts`)

Single `api` object with typed methods for all endpoints. Uses HttpOnly cookie auth (server-managed) with CSRF protection.

```typescript
// Pattern: typed methods return unwrapped data
const user = await api.getUserProfile();
const orders = await api.getOrders({ page: 1, pageSize: 20 });
```

**Response format**: Same as Center API — HTTP 200 always, error in `code` field.

```typescript
interface ApiResponse<T> {
  code: number;      // 0 = success, ErrorCode.* for errors
  message?: string;  // Debug only, never show to users
  data?: T;
}
```

### API Proxy (Next.js → Center API)

Public pages call `/api/*` and `/app/*` which Next.js proxies to the Center API service. In production this is handled by the reverse proxy (Amplify/nginx).

### Error Handling

`ApiError` class with error codes matching `api/response.go`. On 401, emits `auth:unauthorized` event and auto-redirects to login (configurable via `autoRedirectToAuth` option).

Error code-to-i18n mapping lives in `lib/api-errors.ts`. Use `getApiErrorMessage(code, t)` in public `[locale]` pages and `getApiErrorMessageZh(code)` in manager pages. Never show `error.message` to users — it contains raw backend debug text.

## Authentication

- **Web auth**: HttpOnly cookie (`access_token`) + CSRF token. Cookies sent via `credentials: 'include'`.
- **Embed mode**: Bearer token in `localStorage` for iframe embedding.
- **Manager auth**: Same cookie auth. Admin role checked by Center API middleware.
- **Token refresh**: Server-side sliding expiration (< 7 days remaining → auto-renew). No client-side refresh.

## i18n (next-intl)

Each locale belongs to exactly one brand — the deployment only serves its own
(off-brand locales 301 to the brand's default locale).

| Code | Language | Brand | Brand default |
|------|----------|-------|---------------|
| `zh-CN` | Simplified Chinese | kaitu | Yes |
| `zh-TW` | Traditional Chinese | kaitu | |
| `zh-HK` | Traditional Chinese (HK) | kaitu | |
| `en-US` | English (US) | overleap | Yes |
| `en-GB` | English (UK) | overleap | |
| `en-AU` | English (AU) | overleap | |
| `ja` | Japanese | overleap | |

**URL format**: `/{locale}/path` (e.g., `/zh-CN/purchase`, `/en-US/install`)

**Usage**:
```typescript
import { useTranslations } from 'next-intl';
const t = useTranslations();  // NOT const { t } = useTranslations()
```

**Navigation**: Use `Link` from `@/i18n/routing` for internal links (auto locale prefix). Use `next/link` for external links.

**Files**: `messages/{locale}/{namespace}.json` — namespaces: nav, common, auth, purchase, hero, install, discovery, invite, wallet, campaigns, changelog, admin, theme, k2, releases, guide-parents, errors.

**Namespace registry**: `messages/namespaces.ts` lists all active namespaces. When adding a new `*.json` namespace file, add its name to the `namespaces` array in `namespaces.ts` — otherwise it is never loaded and all keys return their raw key string silently.

**usePathname / Link for locale-aware navigation**: Inside `[locale]` components, use `usePathname` and `Link` from `@/i18n/routing`, NOT from `next/navigation` or `next/link`. The `@/i18n/routing` versions strip the locale prefix from pathnames and auto-prefix links.

## Content Publishing (Velite)

Markdown files in `content/{locale}/` are processed by Velite at build time and served via the `[...slug]` catch-all route or dedicated routes.

- **Content files**: `web/content/{locale}/{path}.md` → URL: `/{locale}/{path}`
- **Directory listing**: Any directory with content files gets an automatic listing page
- **Multi-language**: Same path across locales = same article. Falls back to zh-CN if locale version missing.
- **Images**: `web/public/images/content/` → reference as `/images/content/filename.jpg`
- **Import data**: `import { posts } from '#velite'` (tsconfig path alias)
- **Build**: Velite runs alongside Next.js via `process.argv` detection in `next.config.ts`
- **Skill**: Use `/publish-content` to create content with AI assistance

**Velite schema optional fields** (post frontmatter):
- `order: number` — sidebar sort weight (used by `/k2/` sidebar). Omit for non-sidebar content.
- `section: string` — sidebar grouping key (e.g., `"getting-started"`, `"technical"`, `"comparison"`). Omit for non-sidebar content.

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

**Reserved paths** (content must NOT use): 403, account, discovery, install, login, opensource, privacy, purchase, retailer, routers, s, support, terms, changelog, releases, manager, k2

## Routing

| Path pattern | Layout group | Auth | Purpose |
|-------------|-------------|------|---------|
| `/{locale}/*` | `[locale]` | Public/Mixed | User-facing pages |
| `/{locale}/k2/[[...path]]` | `[locale]/k2` | Public | K2 protocol docs (Velite + sidebar) |
| `/{locale}/support` | `[locale]` | Public | Support / FAQ page |
| `/{locale}/{...slug}` | `[locale]` | Public | Content pages (Velite catch-all) |
| `/manager/*` | `(manager)` | Admin | Management dashboard |

**Manager routes bypass i18n middleware** — no locale prefix. Chinese-only admin UI.
**Static routes take priority** over the `[...slug]` catch-all (Next.js default behavior). `/k2/[[...path]]` takes priority over `[...slug]` for all `/k2/*` paths.

## Environment

See `.env.example` for all variables.

`NEXT_PUBLIC_BRAND=kaitu|overleap` (default `kaitu`) bakes the deployment brand at build time — set per Amplify app, baked into `.env.production` by `amplify.yml` and inlined into client bundles.

## Deployment

Two AWS Amplify apps (one per brand) built from the same repo and the same `amplify.yml`; they differ only by `NEXT_PUBLIC_BRAND` (kaitu.io / overleap.io). Prebuild script (`scripts/amplify-prebuild.sh`) handles env setup.

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
- Brand keyword consistency: product is "Kaitu" (en) / "开途" (zh), protocol is "k2", congestion control is "k2cc". Never deviate.

### GEO (Generative Engine Optimization — AI Search)

Optimizing for AI search engines (Google AI Overview, Perplexity, ChatGPT Search). These rules ensure content is extractable and citable by LLMs.

- **Citable facts over marketing**: Technical content must use factual statements + data that AI can directly quote. "k2cc maintains stable throughput under high packet loss" > "blazing fast speeds".
- **FAQ with structured data**: Feature pages and support pages must have FAQ sections marked with `FAQPage` JSON-LD. Questions in natural language ("What is the difference between k2 and Clash?").
- **Comparison tables in semantic HTML**: Protocol comparison tables must use `<table>` with `<thead>`/`<tbody>`. AI parsers extract tabular data. Never use CSS grid for comparison data.
- **E-E-A-T signals**: Link to GitHub repo, changelog, technical docs. Show team/org info. These authority signals determine whether AI cites your content.
- **Long-tail query coverage**: Create content pages answering specific technical queries ("What is ECH encryption?", "How does k2cc congestion control work?"). These are the queries AI search surfaces.
- **Schema.org SoftwareApplication**: Install page must include `SoftwareApplication` schema with `name`, `operatingSystem`, `downloadUrl`, `applicationCategory`.
- **Direct answers first**: Content pages should lead with a concise 1-2 sentence answer, then expand. AI extracts the first definitive statement.

## Gotchas

- **`useTranslations()` pattern**: Must use `const t = useTranslations()` — destructuring `const { t }` does NOT work with next-intl.
- **Translation keys in ALL locales**: Every key must exist in all 7 locale JSON files before committing.
- **Namespace registry**: Adding a new `messages/{locale}/*.json` namespace requires adding it to `messages/namespaces.ts` `namespaces` array. Missing registry entry = silent key passthrough (no error).
- **usePathname / Link — use @/i18n/routing**: Inside `[locale]` layout components, import `usePathname` and `Link` from `@/i18n/routing` (NOT `next/navigation` or `next/link`). ESLint enforces this.
- **API response pattern**: Same as Center API — check `code` field, not HTTP status. Never show `message` to users.
- **Manager has no i18n**: Admin dashboard is Chinese-only, routes bypass next-intl middleware entirely.
- **Package manager**: Must use `yarn` exclusively (not npm).
- **Separate from workspaces**: `web/` has its own `yarn.lock`. Run `yarn install` inside `web/`, not from root.
- **Node version**: Requires Node >= 22 (see `.nvmrc`).
- **API chain linkage**: When modifying Center API endpoints, update `web/src/lib/api.ts` typed methods to match.
- **Velite `.velite/` directory**: Generated at build time, gitignored. Contains `index.js`, `index.d.ts`, `posts.json`. Rebuild with `npx velite build`.
- **Content prose styling**: Uses `@tailwindcss/typography` — article content rendered with `prose dark:prose-invert` classes.
- **Optional catch-all for root match**: Use `[[...path]]` (not `[...path]`) when the route must also match the root path (e.g., `/k2/` with no segments). Static routes always win over catch-all.
- **Velite mock in tests**: vitest tests mock `#velite` import with synthetic post data. Server Component pages tested by calling as async functions directly, asserting on returned JSX or `generateMetadata()` output.
- **Vitest 3 behavior shifts** (matters when writing new tests, not for existing ones): `mockReset()` now restores the original implementation instead of returning `undefined`; `vi.spyOn()` reuses an existing spy if called twice on the same method; `expect(err).toEqual(new Error('x'))` checks `name` + `message` + `cause` + prototype (so `TypeError` ≠ generic `Error`); `vi.useFakeTimers()` mocks `performance.now()` by default — pass `{ toFake: [...] }` to restore prior scope.
- **next-intl IntlMessages interface**: `web/src/types/i18n.d.ts` uses an empty `interface IntlMessages {}` (permissive typing) because messages are split across namespace files loaded dynamically. This disables compile-time key checking — use runtime tests instead.
- **Server Component pages with setRequestLocale**: Cast locale to `(typeof routing.locales)[number]` when calling `setRequestLocale()`. The URL param type is `string` but next-intl requires the narrower union type.
- **Homepage is Server Component + force-static**: `web/src/app/[locale]/page.tsx` has `export const dynamic = 'force-static'`. Do NOT add `"use client"` — it would break SSG and SEO.
- **redirect — use @/i18n/routing**: Inside `[locale]` pages, import `redirect` from `@/i18n/routing` (NOT `next/navigation`). ESLint enforces this. The next-intl redirect takes `{ href, locale }` object, not a plain string.
- **k2cc protocol naming**: Protocol brand name is "k2cc" (congestion control), NOT "k2arc". Renamed in commit 80330ec for SEO clarity (avoids amateur radio / math formula collisions). All i18n, content, and JSON-LD reflect this.
- **Purchase page Server Component pattern**: `purchase/page.tsx` is a Server Component wrapper that exports `generateMetadata()` for SEO. Client-side purchase logic is in a separate `PurchaseClient` component.
- **Embed mode** (`?embed=true`): Pages embedded in desktop app iframe. `useEmbedMode()` hook controls Header/Footer/CTA visibility. `ChatwootWidget` and `CookieConsent` auto-hide in embed mode. Used by `/releases` and `/changelog` routes.
- **Platform labels in i18n**: Use user-friendly names, not technical ones. iOS → "iPhone / iPad", macOS → "苹果电脑" (zh) / "Mac" (en), Android → "安卓" (zh). No file extensions (.exe/.dmg/.apk) in download button labels.

## CMS (Payload)

Payload v3 admin mounted at `/manager/cms`, REST API at `/payload/api`. GraphQL is disabled (no `(payload)/payload/api/graphql` folders exist). URL is namespaced under `/manager` for consistency with the rest of the admin dashboard, but Payload runs under its own `(payload)` root layout (independent from `(manager)` — Payload's admin ships its own `<html>` shell). A "← 返回管理后台" link is injected into Payload's left nav via `admin.components.beforeNavLinks`. Content collections:

- `posts` — Articles with Lexical rich text, draft/published versions, localized to 7 locales (zh-CN source, 6 AI-translated). Each post belongs to exactly one `category` (required when publishing; validated via `validateCategoryRequired` export). Per-post brand visibility via `showOnKaitu` / `showOnOverleap` booleans; at least one must be true when status=published. Category list + detail pages rendered through the unified `[locale]/[...slug]/page.tsx` catch-all: 1 segment = category list, 2 segments = post detail (with brand visibility 404), 3+ = 404. `canonicalBrand` resolved from the visibility pair + locale fallback (`en-*` → overleap, else kaitu).
- `categories` — Flat grouping (no nesting) with localized name + description; URL structure is `/{category.slug}/{post.slug}`
- `tags` — Flat labels with localized name
- `media` — Uploaded images, stored in S3 (`kaitu-cms-media`, ap-northeast-1) and served via CloudFront at `https://media.kaitu.io`. REST read is admin-only; public post pages reach images via `generateFileURL` → CDN URL embedded in post content.
- `admins` — Auth collection; custom Center API cookie strategy (no local password)

### Auth
Reuses `/manager` admin's `access_token` HttpOnly cookie OR an `X-Access-Key: ktu_...` header (for service tokens / MCP clients). Bridge: `src/payload/auth/centerAuthStrategy.ts` extracts whichever credential is present, calls Center `/api/user/info`, checks `isAdmin || (roles & 0xfffffffe) !== 0`, upserts admin record by Center `uuid`. The `kaitu-center` MCP uses the header path to drive Payload REST programmatically.

### Translation (lazy on-demand)

Translations are **populated on first read**, not in the write request.

Why: Amplify Hosting SSR Lambda is capped at 30s. Translating into six locales synchronously inside the `afterChange` hook exceeds the cap and rolls back the create/update via gateway timeout. The lazy model bounds each request to at most one translation (~5–15s on `gemini-2.5-flash`).

Flow:
1. **Source write** (`zh-CN`) — `autoTranslate` hook (`src/payload/hooks/autoTranslate.ts`) nulls `title` / `excerpt` / `content` on every non-source locale via `db.updateOne` (~50ms × 6 ≈ 300ms, well inside the cap). `db.updateOne` bypasses collection validation (required fields would otherwise reject null) and does not fire hooks (no re-entry).
2. **Detail-page read** (`src/app/[locale]/[...slug]/queries.ts → findPostInCategory`) — probe with `fallbackLocale: false` to detect a missing translation, then `lazyTranslate(...)` fills it under a Postgres advisory lock with a 25s `AbortController`-style timeout.
3. **List read** (`listPostsInCategory`) — does **not** trigger lazy translate. A list of N items would serialize N translations. Lists render via the default source-locale fallback; the first detail-page visit hydrates the locale and subsequent list renders pick up the cached value.

Failure modes (caller falls back to source locale automatically because the final `findByID` uses default fallback):
- `locked-by-other` — another reader holds the advisory lock; we return without translating.
- `timeout` — OpenRouter > 25s; logged at error level.
- `error` — translator threw; logged with the error object.

Configurable via env:
- `TRANSLATOR_BASE_URL=https://openrouter.ai/api`
- `TRANSLATOR_API_KEY=sk-or-v1-...`
- `TRANSLATOR_MODEL=google/gemini-2.5-flash`

### Local dev

```bash
# Postgres comes from the shared dev-postgres container (managed by user-level postgres-dev MCP).
# Standard port 5432 — credentials in web/.env.local DATABASE_URL.
# Project no longer ships docker-compose.dev.yml — see docker-compose.dev.yml.deprecated
cd web && yarn payload generate:types                 # Regenerate types after collection changes (gitignored output)
cd web && yarn dev                                    # Next.js + Payload; visit http://localhost:3000/manager/cms
```

### Consumption
Public `[locale]/{category}/*` pages use the Payload Local API via `getPayload().find({ ... overrideAccess: true })` in `web/src/app/[locale]/[...slug]/page.tsx` + its `./queries.ts` helpers. No public REST, no GraphQL.

### Production (Amplify)
Required env vars: `DATABASE_URL`, `PAYLOAD_SECRET`, `TRANSLATOR_API_KEY`, `TRANSLATOR_BASE_URL`, `TRANSLATOR_MODEL`, `CENTER_API_URL`, `S3_BUCKET`, `S3_REGION`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, `CDN_URL`.

**Schema migrations are explicit** (`push: false`). Migration files live in `src/migrations/`. After committing schema changes, run migration manually against prod before/after deploy:
```bash
cd web && PAYLOAD_SECRET=<prod> DATABASE_URL=<prod> yarn payload migrate
```
amplify.yml does NOT run `payload migrate` automatically because `@payload-enchants/translator` has an ESM dir-import bug that crashes the payload CLI on Node 22 (Next.js bundler tolerates it; raw CLI does not). Until that's resolved, treat migrations as a manual deploy step.

**Media infra (AWS):**
- S3 bucket `kaitu-cms-media` (ap-northeast-1) — private, block-public-access ON, bucket policy grants read only to CloudFront via OAC
- CloudFront dist `EDW1KA2NDICCJ` — alias `media.kaitu.io`, ACM cert in us-east-1, Managed-CachingOptimized policy, CORS-S3Origin origin-request policy, `CORS-With-Preflight` response-headers policy
- IAM user `payload-cms-media-uploader` — scoped to `s3:PutObject/DeleteObject/GetObject/ListBucket` on bucket ARN; access key in Amplify env only
- Route 53 A+AAAA alias: `media.kaitu.io.` → `d2cb1b6o656sch.cloudfront.net.`

**Generating new migrations**: When changing collections, regenerate via `cd web && yarn payload migrate:create <name>`. Review the generated `src/migrations/<timestamp>_<name>.ts` (drop columns / type narrows are flagged in DOWN). Commit migration + apply with `yarn payload migrate` against prod.

**importMap path gotcha**: `payload generate:importmap` writes to `src/app/(payload)/manager/cms/importMap.js` (NOT inside `[[...segments]]/`). Both `layout.tsx` and `[[...segments]]/page.tsx`/`not-found.tsx` import from that path — keep them in sync. Stale importMap = silent blank page (server can't resolve internal Payload components, Suspense renders empty).

### Gotchas specific to Payload
- **`.ts` extensions required** on all payload-internal imports (e.g., `import { X } from './collections/X.ts'`) — Payload CLI's tsx loader mandates them.
- **`--disable-transpile`** on `yarn payload` CLI — avoids `ERR_REQUIRE_ASYNC_MODULE` from tsx loader.
- **Postgres on standard port 5432** — uses the shared dev-postgres container managed by user-level `postgres-dev` MCP. The dedicated `web/docker-compose.dev.yml` (host port 5435) was retired 2026-05-27; the file remains as `.deprecated` for reference.
- **`admins.auth: { disableLocalStrategy: true }`** — removes Payload's auto email/password; explicit `email` field needed.
- **Middleware passthrough is load-bearing**: `src/middleware.ts` must early-return for `/manager` AND `/payload` prefixes, and the matcher must exclude them (`/((?!api|app|manager|payload|_next|...))`). `/manager` covers both the manager dashboard and the nested Payload admin at `/manager/cms`; `/payload` covers Payload's REST. Without this, i18n middleware mangles admin requests into locale-prefixed redirects. Verify whenever editing `middleware.ts`.

## Related Docs

- [Root Architecture](../CLAUDE.md)
- [Center API](../api/CLAUDE.md) — Backend endpoints consumed by `api.ts`
- [Webapp Frontend](../webapp/CLAUDE.md) — Separate in-app UI (different tech stack: MUI, React Router, Zustand)
