# Webapp brand details (moved from `webapp/CLAUDE.md`)

The hard rules stay in `webapp/CLAUDE.md` "Brand". This file holds the descriptive parts.

## Stripe purchase flow (`features.stripeCheckout`, i.e. overleap)

The Purchase page branches to `components/stripe/StripePurchasePanel` before the WordGate
fallback. Subscribe mode shows plan cards plus an external checkout link. Manage mode
dispatches on `activeSub.manage.kind` (`stripe_portal` / `apple_settings` / `url`). The hook
`hooks/useStripeCheckout` calls `POST /api/user/stripe/{checkout,portal}` and then
`openExternal`. The order is booked by the server's Stripe webhook; the client never books it.

## Icons and `<title>`

Brand icons live in `src/brands/<brand>/assets/`, one source for each brand. `public/` holds
no brand icon. The `k2-brand` vite plugin copies the active brand's icons into dist at
writeBundle, serves them through dev-server middleware, and rewrites `<title>`. Runtime asset
paths (`/favicon.png`, `/icon-192x192.png`) are the same for both brands.

## `brand-assets/` (bitmap source)

`webapp/brand-assets/<brand>/logo.svg` plus `generate.sh` are the single source of every
brand bitmap: webapp `src/brands/<brand>/assets`, the site `public/` (kaitu → `web/public`,
overleap → `sites/overleap/public`), desktop `icons/` / `icons-overleap/`, the iOS AppIcon and
Splash, and the Android launcher and splash set. `OUT_ROOT` renders into another tree. Not
generated: `web/public/images/og-default.png` (after a kaitu logo change run
`cd web && node scripts/generate-og-image.mjs`). Regenerate; never edit a PNG by hand. `scripts/check-mobile-brand-assets.sh`
guards the mobile outputs.

## Kaitu-only surfaces

These are gated through `getCurrentAppConfig().features`:

- Account: Delegate Payer (`delegate`) and My Wallet (`wallet`, WordGate).
- LoginDialog: the invite-code alert and field, and IosMembershipPanel's invite-reward card (`invite`).
- LoginDialog "Activate Service": `purchaseSurfaceAvailable()`.

A platform-only gate (`os !== 'ios'`) is never enough for a kaitu payment or programme
surface, because the overleap build would ship it. A real-browser check found this; the fix
was `feat(webapp): Overleap 隐藏代付/钱包/邀请码…`.

## Brand rules — full text

Moved verbatim from `webapp/CLAUDE.md` "Brand" (2026-11-14). The short invariant list stays there; **read this before changing `src/brands/`, `i18n/i18n.ts` language handling, `utils/purchase-surface.ts`, or a brand-gated test.**

- **`src/brands/` is the single source of truth**: `brandConfig` carries productName /
  domainLabel / baseURL / supportEmail / locale-aware names & slogans /
  defaultLocale / MUI theme tokens / feature gates. Never fork on brand id in
  components — add a gate to `BrandFeatures` and read it via
  `getCurrentAppConfig().features.*` (config/apps.ts merges brand gates with
  platform-static features).
- **品牌差异只进 `src/brands/<id>/{index,theme,assets,locales}`** — 共享代码里禁止品牌
  id 分叉（唯一 resolver 是 `brandConfig`）。IAP 商品 id（`iapProductIds`）、FAQ 品牌
  key（`faqExtraKeys`）、antiblock CDN 镜像（`antiblockCdnSources`）均为品牌配置字段，
  消费方读 config，不留硬编码副本（Phase A defork）。
- **Stripe 购买流**（overleap，`features.stripeCheckout`）→ `components/stripe/StripePurchasePanel`；
  入账只由服务端 Stripe webhook 完成，客户端不落账。细节见 `docs/webapp/brand-details.md`。
- **`X-K2-Brand` header**: injected ONLY in `services/cloud-api.ts` (`request()` +
  `_doRefresh()`), riding both relay and direct transports. 403003 (BRAND_MISMATCH)
  clears the session and opens LoginDialog, mirroring 403002.
- **i18n is brand-neutral**: locale files use `{{brand}}` / `{{brandDomain}}` /
  `{{brandBaseUrl}}` / `{{supportEmail}}` interpolation (defaultVariables installed in
  `i18n/i18n.ts`, refreshed on languageChanged via `brands/i18n-vars.ts`). Brand-exclusive
  copy lives in `src/brands/<brand>/locales/<lang>/<ns>.json` overlays (deep-merged at load;
  only the active brand's overlays are bundled). Guard test:
  `src/i18n/__tests__/brand-literals.test.ts`. Kaitu default locale zh-CN; overleap en-US.
- **Languages**: `languages` in `i18n/i18n.ts` is the registry (native name, English name, direction); `src/i18n/locales/` holds 23 locale dirs, but a brand OFFERS only its `brandConfig.locales` (kaitu 7 = zh-CN / zh-TW / zh-HK / en-US / ko / ja / ru — the same list as the kaitu.io site `web/src/i18n/routing.ts`, change both together; overleap all 23) — the picker (`components/LanguageDialog.tsx`), boot detection and `normalizeLanguageCode` all stay inside that whitelist. Boot language = stored choice if still offered (a stored choice no longer offered is matched first, e.g. kaitu en-GB → en-US), else `matchLocale(navigator.languages)` (`i18n/match-locale.ts`), else brand default; auto-detection is never persisted. `ar` / `fa` are RTL: `<html dir>` + MUI `direction` + the stylis RTL plugin (`contexts/ThemeContext.tsx`) mirror emotion styles automatically — inline `style={{ marginLeft }}` is NOT mirrored. Adding a language: registry entry + `locales/<lang>/` with every namespace (`i18n/__tests__/locale-coverage.test.ts`, structure via `scripts/i18n-check-translation.mjs`) + the brand's `locales`. The brand name is never translated — copy uses `{{brand}}`.
- **Tests must be brand-adaptive**: `vitest` bakes the brand the same way builds do, so
  `K2_BRAND=overleap npx vitest run` must also exit 0. Never hardcode brand identity in
  assertions — assert against `brandConfig.*` / `getBrandName()`, or against the
  `KAITU_BRAND` / `OVERLEAP_BRAND` named imports for registry contracts. Gate
  brand-divergent suites with `describe.runIf(brandConfig.features.x)` and give the
  closed-gate branch a **real assertion** (see `Tunnels.test.tsx` "brand gate" and
  `Purchase.privateNode.test.tsx` "brand payment-channel gate"), not a bare skip.
  Brand literals inside a test are only OK as mock fixtures.
- **Brand literals belong in `src/brands/<brand>/index.ts`, never in a page/component**: pages are
  statically imported, so a literal there ships in EVERY brand's bundle and breaks purity
  (this is why `k2sInstallUrl` lives in the registry). `src/brands/overleap/index.ts` and
  `src/brands/i18n-vars.ts` ship in overleap artifacts — keep them free of `kaitu.io` /
  `开途` / `開途` **even in comments**; don't make the minifier's comment stripping
  load-bearing. (`types.ts` is type-only → erased, so it's exempt.)
- **Artifact purity**: `webapp/scripts/check-brand-purity.sh <brand> dist` (lives in
  `webapp/scripts/`, not the repo-root `scripts/`) — kaitu build must contain zero
  `overleap.io`; overleap build zero `kaitu.io|开途|開途`. Run after any build-affecting change. Bare `kaitu` protocol tokens (X-K2-Client `kaitu-service/`,
  `kaitu-language` storage key) are intentional and excluded.
- **Icons/title**: brand icons only in `src/brands/<brand>/assets/` (`public/` holds none); the
  `k2-brand` vite plugin copies them and rewrites `<title>`. Bitmaps are generated from
  `brand-assets/<brand>/logo.svg` + `generate.sh` — regenerate, never edit a PNG
  (`docs/webapp/brand-details.md`).
- **Purchase surface gate**: `utils/purchase-surface.ts` `purchaseSurfaceAvailable()` is the single
  authority for showing any purchase / subscription-management entry point — the `/purchase` route
  (`App.tsx`), the Side/BottomNavigation items, the CloudTunnelList renew CTA and the Account CTAs
  all consult it; never re-derive the condition at a call site. iOS without the StoreKit bridge →
  false (Apple 3.1.1); Android when `features.androidPurchase` is false (overleap: Google-Play-only,
  Play Payments policy) → false, and Account renders `components/SubscriptionStatusOnly` instead
  (status + expiry + `account.managedElsewhere`; no buttons, links or prices). Guard:
  `pages/__tests__/Account.android-overleap.test.tsx` (each brand runs the case that applies to it,
  so the `K2_BRAND=overleap` vitest run exercises the closed gate). `storeUrls: { ios, android }`
  in the registry feeds `ForceUpgradeDialog` (store listing first, `''` = fall back to `/install`).
  Kaitu-only surfaces (delegate / wallet / invite) are gated through `getCurrentAppConfig().features`
  (list in `docs/webapp/brand-details.md`). A platform-only (`os !== 'ios'`) gate is never enough
  for a kaitu payment/programme surface — the overleap build ships it otherwise.
- **Shells** never import webapp brand code: `make build-*` exports `K2_BRAND=$(BRAND)`
  (`Makefile`, `BRAND ?= kaitu`) into `yarn build`; the shared contract is only that env var
  name and the `src/brands/<brand>/assets/` artwork directory.
