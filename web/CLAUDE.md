# Web — Kaitu Website + Admin Dashboard

Next.js website serving public marketing pages, user self-service (purchase, account, wallet), and admin management dashboard. Includes Payload CMS for content management.

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
cd web && yarn payload:generate  # Generate Payload CMS types
cd web && yarn payload:migrate   # Run Payload DB migrations
```

## Tech Stack

Next.js 15 (App Router) | React 19 | TypeScript | Tailwind CSS 4 | shadcn/ui | next-intl | Payload CMS 3 | PostgreSQL (Payload) | S3 (media)

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
│   │   │   ├── changelog/     # Release notes
│   │   │   ├── login/         # Email OTP login
│   │   │   ├── s/[code]/      # Invite link landing
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
│   │   └── (payload)/         # Payload CMS routes (/manager/cms)
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
│   │   ├── constants.ts       # Shared constants
│   │   ├── events.ts          # App event bus (auth:unauthorized, etc.)
│   │   ├── udid.ts            # Device fingerprint
│   │   └── utils.ts           # cn() helper (clsx + tailwind-merge)
│   ├── payload/               # Payload CMS config
│   │   ├── collections/       # Users, Media, Articles
│   │   ├── hooks/             # AI content generation hook
│   │   └── auth/              # Kaitu SSO strategy for Payload
│   └── middleware.ts          # next-intl locale detection + manager bypass
├── messages/                  # i18n JSON files (7 locales × 12+ namespaces)
├── tests/                     # Playwright E2E specs
├── public/                    # Static assets, legal docs, app icons
└── payload.config.ts          # Payload CMS config (DB, S3, locales)
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

## Authentication

- **Web auth**: HttpOnly cookie (`access_token`) + CSRF token. Cookies sent via `credentials: 'include'`.
- **Embed mode**: Bearer token in `localStorage` for iframe embedding.
- **Manager auth**: Same cookie auth. Admin role checked by Center API middleware.
- **Payload CMS auth**: Kaitu SSO strategy — verifies `access_token` cookie against Center API JWT secret.
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

**Files**: `messages/{locale}/{namespace}.json` — namespaces: nav, common, auth, purchase, hero, install, discovery, invite, wallet, campaigns, changelog, admin, theme.

## Routing

| Path pattern | Layout group | Auth | Purpose |
|-------------|-------------|------|---------|
| `/{locale}/*` | `[locale]` | Public/Mixed | User-facing pages |
| `/manager/*` | `(manager)` | Admin | Management dashboard |
| `/manager/cms/*` | `(payload)` | Admin | Payload CMS admin |

**Manager routes bypass i18n middleware** — no locale prefix. Chinese-only admin UI.

## Environment

See `.env.example` for all variables. Key ones:

| Variable | Purpose |
|----------|---------|
| `DATABASE_URI` | PostgreSQL for Payload CMS |
| `PAYLOAD_SECRET` | Payload encryption key |
| `S3_BUCKET` / `S3_REGION` | Media storage |
| `JWT_SECRET` | Must match Center API's `jwt.secret` |
| `AI_PROVIDER` / `AI_API_KEY` | AI content generation (optional) |

## Deployment

AWS Amplify (`amplify.yml`). Prebuild script (`scripts/amplify-prebuild.sh`) handles env setup.

## Gotchas

- **`useTranslations()` pattern**: Must use `const t = useTranslations()` — destructuring `const { t }` does NOT work with next-intl.
- **Translation keys in ALL locales**: Every key must exist in all 7 locale JSON files before committing.
- **API response pattern**: Same as Center API — check `code` field, not HTTP status. Never show `message` to users.
- **Manager has no i18n**: Admin dashboard is Chinese-only, routes bypass next-intl middleware entirely.
- **Package manager**: Must use `yarn` exclusively (not npm).
- **Separate from workspaces**: `web/` has its own `yarn.lock`. Run `yarn install` inside `web/`, not from root.
- **Node version**: Requires Node >= 22 (see `.nvmrc`).
- **Payload types**: Run `yarn payload:generate` after changing collections to regenerate `payload-types.ts`.
- **API chain linkage**: When modifying Center API endpoints, update `web/src/lib/api.ts` typed methods to match.

## Related Docs

- [Root Architecture](../CLAUDE.md)
- [Center API](../api/CLAUDE.md) — Backend endpoints consumed by `api.ts`
- [Webapp Frontend](../webapp/CLAUDE.md) — Separate in-app UI (different tech stack: MUI, React Router, Zustand)
