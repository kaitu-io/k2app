# Feature: Web Pages SSR Migration

## Meta

| Field     | Value                          |
|-----------|--------------------------------|
| Feature   | web-pages-ssr-migration        |
| Version   | v1                             |
| Status    | draft                          |
| Created   | 2026-02-21                     |
| Updated   | 2026-02-21                     |

> (from TODO: web-pages-ssr-migration)

## Version History

| Version | Date       | Summary                                              |
|---------|------------|------------------------------------------------------|
| v1      | 2026-02-21 | Initial: SEO-focused SSR migration for 10 public pages |

## Product Requirements

- Convert 10 SEO-relevant "use client" public pages to Server Components for improved SEO/GEO crawlability (v1)
- Initial HTML must contain meaningful text content (not empty shell requiring JS hydration) (v1)
- Interactive elements preserved via composition pattern (client component islands inside server-rendered shell) (v1)
- `(manager)` admin dashboard pages unaffected (v1)
- Embed mode (`useEmbedMode`) continues to work on pages that use it (v1)

## Technical Decisions

- **Composition pattern**: Server Component shell renders static content (headings, text, metadata); interactive parts extracted as client component children. Proven by `[...slug]/page.tsx` and homepage AC1 (v1)
- **`getTranslations()` from `next-intl/server`**: Replaces client-side `useTranslations()` for server-rendered i18n text (v1)
- **`setRequestLocale(locale)`**: Required in every Server Component page for static generation support (v1)
- **`export const dynamic = 'force-static'`**: Applied to purely static pages (no runtime data fetching). Pages with runtime API calls use default SSR instead (v1)
- **Embed mode pages**: `useEmbedMode()` uses `useSearchParams()` which requires client boundary. Wrap in `<Suspense>` inside a client component island. Server shell still renders content (v1)
- **Runtime fetch pages (privacy, terms, retailer/rules)**: Move markdown fetch to build-time or server-side `fetch()` in the Server Component. Eliminates client-side loading spinner (v1)
- **Skip auth-gated pages**: login, purchase, account/* (7 pages) provide near-zero SEO benefit since they're behind auth or require full client state. Not in scope (v1)

## Pages In Scope

### Tier 1: Trivial (swap translations only)

| Page | Path | Interactive Elements | Strategy |
|------|------|---------------------|----------|
| routers | `[locale]/routers/page.tsx` | None (buttons are no-ops or mailto links) | Direct conversion: `useTranslations` → `getTranslations`, remove "use client" |

### Tier 2: Easy (small client island extraction)

| Page | Path | Interactive Elements | Strategy |
|------|------|---------------------|----------|
| 403 | `[locale]/403/page.tsx` | `useRouter` + 2 onClick buttons | Extract `<ErrorActions>` client component for back/home buttons |
| privacy | `[locale]/privacy/page.tsx` | `fetch('/legal/privacy-policy.md')` at runtime | Move markdown fetch to server-side `fetch()` in async Server Component |
| terms | `[locale]/terms/page.tsx` | Same pattern as privacy | Same: server-side fetch |
| retailer/rules | `[locale]/retailer/rules/page.tsx` | Same pattern as privacy | Same: server-side fetch |

### Tier 3: Moderate (interactive islands + special handling)

| Page | Path | Interactive Elements | Strategy |
|------|------|---------------------|----------|
| discovery | `[locale]/discovery/page.tsx` | `useEmbedMode` (useSearchParams) | Server shell renders all link cards; extract `<EmbedModeWrapper>` client island for nav/footer control |
| opensource | `[locale]/opensource/page.tsx` | `setInterval` countdown timer | Server shell renders all static content; extract `<CountdownTimer>` client island |
| changelog | `[locale]/changelog/page.tsx` | Runtime JSON fetch + expand/collapse accordion | Server-side fetch `changelog.json`; extract `<ChangelogAccordion>` client island for expand/collapse state |

### Tier 4: Moderate-hard (SEO-valuable despite complexity)

| Page | Path | Interactive Elements | Strategy |
|------|------|---------------------|----------|
| install | `[locale]/install/page.tsx` | Device detection, countdown, download triggers | Server shell renders all platform download cards with static content; extract `<InstallClient>` for device detection + auto-download logic |
| s/[code] | `[locale]/s/[code]/page.tsx` | API fetch, cookie write, download | Server-side fetch invite info via `generateMetadata` params; extract `<InviteActions>` client island for download/activate buttons |

## Pages Out of Scope

| Page | Reason |
|------|--------|
| login | Full auth flow requiring client state |
| purchase | Multi-step purchase flow with 15+ state variables |
| account (redirect) | Client-side router redirect |
| account/delegate | Auth-gated API CRUD |
| account/members | Auth-gated CRUD dialogs |
| account/wallet | Auth-gated with withdraw dialog |
| account/wallet/accounts | Auth-gated complex form |
| account/wallet/changes | Auth-gated paginated table |
| account/wallet/withdraws | Auth-gated paginated table |

## Acceptance Criteria

### AC1: Tier 1 — routers page SSR (v1)

- `routers/page.tsx` is a Server Component (no "use client")
- Uses `getTranslations()` from `next-intl/server`
- Has `setRequestLocale(locale)` call
- `yarn build` succeeds
- `curl` of the rendered page contains product names and descriptions in initial HTML

### AC2: Tier 2 — static content pages SSR (v1)

- 403, privacy, terms, retailer/rules pages are Server Components
- 403 page: `<ErrorActions>` extracted as client component with back/home buttons
- privacy/terms/retailer-rules: markdown content fetched server-side (no client-side loading spinner)
- All pages pass `yarn build`
- Initial HTML contains page content (not empty loading states)

### AC3: Tier 3 — discovery, opensource, changelog SSR (v1)

- discovery: server shell renders all link cards; `<EmbedModeWrapper>` client island controls nav/footer visibility
- opensource: server shell renders all static content; `<CountdownTimer>` client island for live countdown
- changelog: server-side fetch of `changelog.json`; `<ChangelogAccordion>` client island for expand/collapse
- Embed mode still works on discovery page (hide nav/footer via query param)
- All pages pass `yarn build`
- Initial HTML contains meaningful content

### AC4: Tier 4 — install and invite pages SSR (v1)

- install: server shell renders all platform download cards with i18n text; `<InstallClient>` handles device detection + auto-download
- s/[code]: server-side metadata generation from invite code; `<InviteActions>` handles download/activate interactions
- Both pages pass `yarn build`
- Initial HTML contains download information and invite details

### AC5: Verification — build + crawlability (v1)

- `yarn build` produces no errors across all converted pages
- For each converted page: `curl http://localhost:3000/{locale}/{path}` returns HTML containing page-specific text content (not empty div shells)
- `(manager)` admin pages unaffected (spot-check 2-3 manager routes)
- Existing E2E tests pass (`yarn test:e2e`)
- No regressions in embed mode functionality

### AC6: generateMetadata for converted pages (v1)

- Each converted page has `generateMetadata` returning page-specific title + description
- Open Graph tags (og:title, og:description) present in initial HTML
- Replaces any hardcoded or missing metadata

## Testing Strategy

- **Build verification**: `yarn build` must succeed with all converted pages (v1)
- **Crawlability test**: For each page, verify initial HTML contains expected text via curl/wget (v1)
- **E2E regression**: Run `yarn test:e2e` to catch navigation/rendering regressions (v1)
- **Embed mode**: Manual verification that discovery page embed mode still hides nav/footer (v1)
- **Visual spot-check**: Verify no layout shifts or missing content after conversion (v1)

## Deployment & CI/CD

- Same AWS Amplify deployment as existing pages (v1)
- `force-static` pages generate `.html` at build time, served from CDN (v1)
- Pages with runtime server fetches (privacy/terms/retailer-rules if using server-side fetch) work under `WEB_COMPUTE` Amplify mode (v1)
- No infrastructure changes required (v1)

## Implementation Order

Recommended execution order (each tier can be verified independently):

1. **Tier 1**: routers — trivial, proves the workflow
2. **Tier 2**: 403, privacy, terms, retailer/rules — small extractions, builds confidence
3. **Tier 3**: discovery, opensource, changelog — moderate complexity, embed mode validation
4. **Tier 4**: install, s/[code] — hardest conversions, highest SEO value

Each tier should be committed separately and verified with `yarn build` + curl check before proceeding.
