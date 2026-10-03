# sites/overleap — overleap.io

Standalone Next.js app for overleap.io. **One brand, one site**: there is no brand registry, no `siteBrand()`, no `page.<brand>.tsx`. The other brand's site lives in `web/` and the two share **no code** — only protocol contracts (`contracts/api-contract.json`). Design: `docs/superpowers/specs/2026-09-30-overleap-site-app-design.md`.

## Commands

```bash
cd sites/overleap && yarn install   # own yarn.lock — not in the root workspace
yarn dev                            # http://localhost:3100 (proxies /api/* to the Center API)
API_PROXY_TARGET=http://127.0.0.1:5899 yarn dev   # against a local Center
yarn test                           # vitest
npx eslint src tests
yarn build                          # also type-checks every message key — CI runs it
```

## Rules that fail silently

- **Do not import from `web/`** and do not create a shared package. Duplication is deliberate: a copy costs an agent a grep; coupling costs every change a two-site regression. `tests/source-guards.test.ts` fails on `../web/` imports.
- **Auth transport is a sibling copy.** `src/lib/api.ts` (cookie + CSRF + Bearer fallback) derives from `web/src/lib/api.ts`. A security fix to either must be checked on the other. This is the only cross-site note — don't add more.
- **Things that must agree with the API are tested, not shared**: brand id / host / support email / error codes → `tests/cross-layer-contract.test.ts` against the generated contract. The invariant is host ownership (`host(SITE.baseUrl) ∈ contract hosts`), not string equality.
- **Messages are typed.** `src/types/next-intl.d.ts` binds `useTranslations` to the en-GB files, so a wrong key or wrong nesting level is a compile error. Dynamic keys must be typed unions (`NavKey`), not `string`. Every locale carries every namespace with identical keys (`tests/messages-parity.test.ts`); there is no runtime fallback.
- **Adding a namespace**: create `messages/<locale>/<ns>.json` for every locale, add the name to `messages/namespaces.ts`, and add it to `src/types/next-intl.d.ts`.
- **Locales**: 20, declared once in `src/lib/site.ts` (`LOCALES` + `LOCALE_META`: native name, English name, text direction). `en-GB` is the master (British spelling); `en-US` / `en-AU` are spelling variants; the rest are machine translations checked for structure only (`scripts/i18n-check-translation.mjs` at the repo root — keys, placeholders, and the brand name, which is never translated). `ar` / `fa` are right-to-left: `<html dir>` comes from `LOCALE_META`, so use logical Tailwind classes (`ms-*` `pe-*` `start-*` `text-start`), never `ml-*` / `left-*`. `/` 307s by cookie → Accept-Language (never shared-cached; tag → parent → same language → `en-GB`); any other locale-shaped prefix 301s to `/en-GB`. Route params are `string` — narrow with `localeOf(params)` (`src/lib/locale-param.ts`), which 404s scanner paths before they reach `Intl`.
- **No other-brand words, no China-market payment channels** anywhere under `src/`, `messages/`, `content/`, `public/legal/` (`tests/source-guards.test.ts`).
- **Privacy posture is product, not config.** Sentry reports errors only: no session replay, `sendDefaultPii: false`, no tracing. Only strictly-necessary cookies (auth, CSRF, `preferredLocale`), so there is no consent banner. Adding analytics or a tracking cookie is a product decision, not a code change.
- **Closed, self-contained product (2026-09-30 decision).** The site does not publish protocol docs, source links, self-hosting guides or GitHub, and does not claim to be open source. Trust is argued from security practice, not from source availability. Protocol names (k2, k2cc, ECH) may appear as product facts, never as "read the code".
- **Legal documents** (`public/legal/*.md`) are single-language English with literal values (no placeholders). Their URLs (`/privacy`, `/terms`, `/delete-account`) are filed with Apple and Google — never move them. `/delete-account` must stay reachable signed-out (Play Data safety).
- **Every public page** exports `generateMetadata` via `pageMetadata(locale, path, …)` — it builds canonical + hreflang from `SITE.baseUrl`, never from a preview host. Pages that must not be indexed pass `index: false`.

## Layout

```
src/lib/site.ts          name, hosts, emails, nav, footer, sitemap routes — the only place the site names itself
src/lib/api.ts           Center API client (only endpoints this site calls)
src/lib/api-errors.ts    ApiError → `errors` message key
src/contexts/AuthContext profile-as-session: signed in ⇔ GET /api/user/info succeeds
src/middleware.ts        locale negotiation, X-K2-Brand on /api/*, 404 on /app/*
src/app/[locale]/        pages: home, install, pricing, purchase (Stripe checkout), support, login, account, legal
src/components/marketing/ landing sections shared by home / pricing / support (FAQ is native <details>: answers stay in the server HTML)
src/lib/pricing.ts       display currency + formatting; PRICING (lib/site.ts) must equal scripts/stripe-setup-overleap.sh (tests/pricing-source.test.ts)
```

## Deployment

Amplify app for overleap.io, `appRoot: sites/overleap`, build spec `sites/overleap/amplify.yml`. Until the cutover (spec §1 phase ④) overleap.io is still served by `web/` with `NEXT_PUBLIC_BRAND=overleap`; this app deploys to a preview domain only.
