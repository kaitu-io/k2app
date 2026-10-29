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
