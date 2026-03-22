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
│   │   │       ├── nodes/     # Node matrix, SSH terminal, batch ops
│   │   │       ├── tunnels/   # Tunnel management
│   │   │       ├── cloud/     # Cloud instance management
│   │   │       ├── campaigns/ # Campaign management
│   │   │       ├── edm/       # Email marketing (templates + tasks + logs)
│   │   │       ├── retailers/ # Retailer CRM (notes, todos, levels)
│   │   │       ├── withdraws/ # Withdraw approval
│   │   │       ├── plans/     # Subscription plan config
│   │   │       ├── tasks/     # Batch task management
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

| Code | Language | Default |
|------|----------|---------|
| `zh-CN` | Simplified Chinese | Yes |
| `en-US` | English (US) | |
| `en-GB` | English (UK) | |
| `en-AU` | English (AU) | |
| `zh-TW` | Traditional Chinese | |
| `zh-HK` | Traditional Chinese (HK) | |
| `ja` | Japanese | |

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
- **Single source of truth**: `releases/v{VERSION}.md` (repo root, NOT in `web/`)
- **Frontmatter**: `version` + `date`. Sections: `## New Features`, `## Bug Fixes`, `## Improvements`, `## Breaking Changes`
- **Generate**: `node scripts/generate-changelog.js` → produces `web/public/releases.json` (gitignored), `changelog.json`, `changelog.md`
- **Display**: `/releases` page fetches `/releases.json` at runtime
- **Never edit `web/public/releases.json` directly** — always edit source `.md` then regenerate

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

See `.env.example` for all variables. Key ones:

No special environment variables required. Build-time config (desktop version, download URL) is set in `next.config.ts`.

## Deployment

AWS Amplify (`amplify.yml`). Prebuild script (`scripts/amplify-prebuild.sh`) handles env setup.

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
- Page titles follow pattern: `{Page Topic} | Kaitu` (zh) / `{Page Topic} | Kaitu` (en). Max 60 characters.
- Every content page (Velite markdown) must have frontmatter with `title`, `description`. Description used for meta.
- Brand keyword consistency: product is "Kaitu" (en) / "开途" (zh), protocol is "k2v5", congestion control is "k2cc". Never deviate.

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
- **next-intl IntlMessages interface**: `web/src/types/i18n.d.ts` uses an empty `interface IntlMessages {}` (permissive typing) because messages are split across namespace files loaded dynamically. This disables compile-time key checking — use runtime tests instead.
- **Server Component pages with setRequestLocale**: Cast locale to `(typeof routing.locales)[number]` when calling `setRequestLocale()`. The URL param type is `string` but next-intl requires the narrower union type.
- **Homepage is Server Component + force-static**: `web/src/app/[locale]/page.tsx` has `export const dynamic = 'force-static'`. Do NOT add `"use client"` — it would break SSG and SEO.
- **redirect — use @/i18n/routing**: Inside `[locale]` pages, import `redirect` from `@/i18n/routing` (NOT `next/navigation`). ESLint enforces this. The next-intl redirect takes `{ href, locale }` object, not a plain string.
- **k2cc protocol naming**: Protocol brand name is "k2cc" (congestion control), NOT "k2arc". Renamed in commit 80330ec for SEO clarity (avoids amateur radio / math formula collisions). All i18n, content, and JSON-LD reflect this.
- **Purchase page Server Component pattern**: `purchase/page.tsx` is a Server Component wrapper that exports `generateMetadata()` for SEO. Client-side purchase logic is in a separate `PurchaseClient` component.
- **Embed mode** (`?embed=true`): Pages embedded in desktop app iframe. `useEmbedMode()` hook controls Header/Footer/CTA visibility. `ChatwootWidget` and `CookieConsent` auto-hide in embed mode. Used by `/releases` and `/changelog` routes.
- **Platform labels in i18n**: Use user-friendly names, not technical ones. iOS → "iPhone / iPad", macOS → "苹果电脑" (zh) / "Mac" (en), Android → "安卓" (zh). No file extensions (.exe/.dmg/.apk) in download button labels.

## Related Docs

- [Root Architecture](../CLAUDE.md)
- [Center API](../api/CLAUDE.md) — Backend endpoints consumed by `api.ts`
- [Webapp Frontend](../webapp/CLAUDE.md) — Separate in-app UI (different tech stack: MUI, React Router, Zustand)
