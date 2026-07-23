# Center Service

Central API service for authentication, user management, payments, tunnel management, and cloud infrastructure.

## Commands

```bash
cd api && go test ./...                                    # Run all tests
cd api && go test -run TestName ./...                      # Run specific test
cd api/cmd && go build -o kaitu-center .                   # Build binary
cd api && docker-compose up -d                             # Start local MySQL + Redis
cd api/cmd && ./kaitu-center migrate -c ../config.yml      # Run DB migrations
cd api/cmd && ./kaitu-center start -f -c ../config.yml     # Start foreground (dev)
make deploy-api                                            # Build + deploy
```

## AI Behavior Rules

### Hard Rules

```
Prohibited:
  Swagger/Swag annotations
  GORM raw SQL string queries (use struct queries only)

Required:
  Follow file naming: api_*.go, logic_*.go, model.go, worker_*.go
  Use predefined error code constants from response.go
  HTTP status always 200 — error state in JSON code field
```

## Tech Stack

**Go 1.24** + Gin | **GORM** (MySQL/MariaDB) | **Redis** | **Asynq** (task queue) | **qtoolkit** (logging, DB, mail, slack)

## File Layout

| Pattern | Purpose | Example |
|---------|---------|---------|
| `api_*.go` | HTTP handlers | `api_auth.go`, `api_user.go`, `api_tunnel.go` |
| `api_admin_*.go` | Admin API handlers | `api_admin_orders.go`, `api_admin_cloud.go` |
| `logic_*.go` | Business logic | `logic_auth.go`, `logic_order.go`, `logic_wallet.go` |
| `handler_*.go` | Asynq task handlers | `handler_edm.go` |
| `worker_*.go` | Background workers + cron | `worker_cloud.go`, `worker_ech.go`, `worker_diagnosis.go` |
| `slave_api*.go` | Internal slave node APIs | `slave_api.go`, `slave_api_node.go` |
| `model*.go` | GORM data models | `model.go`, `model_wallet.go`, `model_push.go` |
| `type.go` | Request/response types | Role bitmask, API DTOs |
| `response.go` | Response helpers + error codes | `Success()`, `Error()`, `ListWithData()` |
| `middleware.go` | All middleware | Auth, CORS, recovery, admin guard |
| `route.go` | Route registration | All endpoint wiring |

## Architecture

```
cmd/                 CLI entry point (start, stop, migrate, health-check, user management)
cloudprovider/       Multi-cloud VPS management (5 providers + SSH standalone)
templates/           Embedded templates (docker-compose, init-node.sh)
```

### Flat Package Pattern

All handlers, logic, and models live in the root `center` package. No internal subdirectories for domain entities. Convention is enforced by file naming, not directory structure.

## Dedicated Line (专属线路): entitlement ↔ node ↔ k2subs

**A `PrivateNodeSubscription` is an entitlement, not a node topology.** It models tier / quota (`TrafficTotalBytes`) / independent clock (`ExpiresAt`) — **not** "how many nodes" or "which nodes". Provisioning, binding, count, and lifecycle of the backing VPS nodes are an **ops responsibility** (NodeOperation queue + provisioning agent), invisible to the subscription and pricing model.

**A router consumes its line(s) through the k2subs URL — not through any node binding on the subscription.** `/api/subs` → `ResolveGatewayPrivateTunnels` (`entitlement_resolver.go`) gathers all of a user's *serviceable* private subscriptions and resolves them into a list of `k2v5://` tunnels. **Multi-node = multiple tunnels in that list**; the router Picks/switches among them. There is no "one subscription → N nodes" schema — multiple nodes surface as multiple k2subs tunnels.

Implications:
- A tier like "4T = 2×2T (two nodes, two IPs)" is purely an **ops provisioning choice** (provision N nodes for the user). It needs **no** subscription-model or schema change — the extra node just appears as another k2subs tunnel. Do not conflate it with the deferred "multi-node subscription" work — k2subs already delivers multi-node.
- `PrivateNodeSubscription.SlaveNodeID` is the **per-line metering/quota anchor**: the node self-meters to `/slave/usage`, Center mirrors it into `NodeUsage` (1:1 by `NodeID`), and `isNodeOverQuota` (剩余 ≤ 500MB) drops the line from `/api/subs`. `CloudInstanceID` (nullable) is now display-only (IP/Region). Neither is the multi-node mechanism.
- Router admission gate = `HasActivePrivateLines` (owning ≥1 serviceable private line), fully decoupled from App `tier`/`MaxRouterDevice`.

## Router Control Key (k2r headless app-control)

`User.RouterControlKey` (`*string`, `varchar(80)`) is an account-level bearer credential the app uses to authenticate to a headless k2r router's control API — all devices on the same account share one key. Stored **plaintext** (not hashed) because the plaintext must be re-issued to every app instance on the account on demand; k2r itself only ever receives/stores a sha256 hash. Accepted risk: the key only controls a home router, and Center already custodies subscription credentials at the same trust level.

- `POST /api/user/router-control-key` (`AuthRequired`, `api_router_control_key.go`) — idempotent mint-or-return via `EnsureRouterControlKey` (`logic_router_control_key.go`): first caller wins a conditional `UPDATE ... WHERE router_control_key IS NULL OR ''`, concurrent losers re-read and converge on the winner's value. No-op if a key already exists.
- `POST /api/user/router-control-key/reset` (`AuthRequired`) — unconditional rotation via `ResetRouterControlKey`. Any app instance still holding the old plaintext gets 401 from k2r on its next control request and must re-fetch (see `webapp/CLAUDE.md` "Router Tab" — `routerFetch`'s 401-retry).
- `/api/subs` response gains `control_key_hash` (`SubsResponse.ControlKeyHash`, `sha256(RouterControlKey)` via `HashRouterControlKey`) — this is k2r's own subscription-refresh channel picking up the current key hash, not an app-facing field. Two injection paths in `api_subs.go`:
  - **gateway branch** (k2r client, matched before the shared-pool branch — ordering is load-bearing) calls `ensureAndInjectControlKeyHash()`: **mints on serve** if the account has no key yet, so a k2r that's never had the app "set-credential" it can still converge on a hash purely by completing a subscription refresh (closes the TOFU window for legacy routers that predate this feature — see the spec's §3.2 "legacy 升级" case).
  - **shared branch** (App/desktop clients) calls `injectControlKeyHash()` — read-only, never mints. A phone/desktop pulling its own `/api/subs` should not silently provision a router key nobody asked for.
- Not yet consumed anywhere except k2r itself (submodule, out of scope here) and the mint/reset endpoints above.
- `RouterControlKey`/`RouterControlKeyCreatedAt` are new `User` columns — hits the "New GORM model columns need a manual migrate" trap below for any integration test touching them against a pre-existing test DB.

## API Response Format

```go
Success(c, &user)                    // Single object
ListWithData(c, items, pagination)   // List with pagination
Error(c, ErrorCode, "message")       // Error (HTTP 200, error in code field)
```

### Error Codes

| Code | Constant | Meaning |
|------|----------|---------|
| 400 | `ErrorInvalidOperation` | Invalid operation |
| 401 | `ErrorNotLogin` | Not logged in / expired token |
| 402 | `ErrorPaymentRequired` | Membership expired |
| 403 | `ErrorForbidden` | Insufficient permissions |
| 404 | `ErrorNotFound` | Resource not found |
| 405 | `ErrorNotSupported` | Not supported |
| 406 | `ErrorUpgradeRequired` | Client upgrade required |
| 409 | `ErrorConflict` | Resource conflict |
| 422 | `ErrorInvalidArgument` | Bad request payload |
| 425 | `ErrorTooEarly` | Too early (rate limit) |
| 429 | `ErrorTooManyRequests` | Rate limited |
| 500 | `ErrorSystemError` | System exception |
| 503 | `ErrorServiceUnavailable` | Service unavailable |
| 400001–400011 | `ErrorInvalidCampaignCode`…`ErrorLicenseKeyAlreadyRedeemed` | Business-specific codes |

> **Constitution**: Every error code added to `response.go` MUST be mirrored in `webapp/src/utils/errorCode.ts`. See `webapp/CLAUDE.md` "API Error Code Constitution" for the full checklist.

## Middleware (middleware.go)

### Auth Chain (priority order)

1. **HttpOnly Cookie** `access_token` (with CSRF check: `X-CSRF-Token` must match `csrf_token` cookie for non-GET)
2. **`X-Access-Key` header** (retailer API key auth, no device context)
3. **`Authorization: Bearer <token>`** header
4. **`?token=` query param** (WebSocket cross-domain)

Sliding expiration: cookie auth auto-renews token if remaining lifetime < 7 days.

### Middleware Functions

| Middleware | Purpose |
|-----------|---------|
| `AuthRequired()` | Aborts 401 if no valid auth |
| `AuthOptional()` | Tries auth, never aborts |
| `ProRequired()` | Checks membership expiry, aborts 402 |
| `DeviceAuthRequired()` | Requires device context in auth |
| `AdminRequired()` | Checks admin role |
| `RetailerRequired()` | Checks retailer role |
| `SlaveAuthRequired()` | Basic Auth (IPv4:NodeSecret) for slave nodes |
| `ApiCORSMiddleware()` | CORS for `/api/*` — allows localhost, loopback, RFC1918, capacitor:// |
| `CORSMiddleware()` | CORS for `/app/*` — allows kaitu.io, localhost:3000 |

## Route Groups

| Group | Auth | Purpose |
|-------|------|---------|
| `/api/auth/*` | None | Login (OTP, password), refresh, logout |
| `/api/tunnels` | Auth + Pro + Device | VPN server list |
| `/api/relays` | Auth + Pro + Device | Relay node list |
| `/api/user/*` | Auth | User profile, devices, email, membership |
| `/api/invite/*` | Mixed | Invite codes CRUD |
| `/api/plans` | None | Subscription plans |
| `/api/wallet/*` | Auth | Wallet, withdrawals |
| `/api/retailer/*` | Auth + Retailer | Retailer stats |
| `/api/strategy/*` | Auth + Device | Routing strategy rules |
| `/api/telemetry/*` | Auth + Device | Client telemetry |
| `/api/issues/*` | Auth | GitHub Issues proxy |
| `/api/device-logs` | Auth + Device | Device log upload registration |
| `/api/feedback-tickets` | Auth/Anonymous | Feedback ticket submission |
| `/api/app/config` | None | Frontend app config |
| `/api/ech/config` | None | ECH config |
| `/api/ca` | None | CA certificate |
| `/app/device-logs` | Admin | Device log list (filter by udid/reason/time) |
| `/app/feedback-tickets` | Admin | Feedback ticket list + resolve/close |
| `/app/*` | Admin | All other admin endpoints |
| `/slave/*` | Slave | Node management, status reporting, per-user device-traffic increment upload |
| `/csr/*` | None | Certificate signing requests |

## Cloud Provider (cloudprovider/)

Unified `Provider` interface for cloud VPS management:

| Provider | File | Cloud |
|----------|------|-------|
| `aliyun_swas` | Alibaba domestic | Aliyun SWAS |
| `alibaba_swas` | Alibaba international | Alibaba Cloud |
| `aws_lightsail` | AWS | Lightsail multi-region |
| `tencent_lighthouse` | Tencent | Lighthouse domestic + intl |
| `bandwagon` | BandwagonHost | VEID/APIKey |
| `ssh_standalone` | SSH-only | No cloud API, direct SSH |

## Background Workers (Asynq)

| Worker | Purpose |
|--------|---------|
| `worker_cloud.go` | Cloud instance sync, change-IP, create, delete |
| `worker_ech.go` | ECH key rotation |
| `worker_diagnosis.go` | Route diagnosis aggregation |
| `worker_renewal_reminder.go` | Membership renewal reminders |
| `worker_retailer_followup.go` | Retailer follow-up notifications |
| `worker_traffic_abuse.go` | 每小时聚合当月 per-user 流量，超阈值（`traffic.abuse_monthly_gb`，缺省 100GB）Slack 告警 + 用户警告邮件（模板 `traffic-abuse-warning`，月度去重）+ 60 天保留清理（隐私政策承诺 2 个月） |
| `worker_integration.go` | `InitWorker()` — registers all handlers + cron schedules |

Asynqmon UI available at `/app/asynqmon` (admin auth required).

## Approval Workflow (Maker-Checker)

Critical admin operations (EDM, campaigns, plans, withdrawals, hard delete, license key batches) require dual approval via `SubmitApproval()`. Superadmin (`is_admin`) bypasses approval and executes synchronously. Non-superadmin creates a pending record requiring another admin's approval.

- **Core files**: `logic_approval.go` (service), `logic_approval_callbacks.go` (10 callbacks), `api_admin_approval.go` (handlers)
- **Pattern**: Handler validates → `SubmitApproval(c, action, params, summary)` → returns `(approvalID, status, error)` where status is `"executed"` (superadmin) or `"pending_approval"` (needs approval)
- **Callback registry**: `RegisterApprovalCallback(action, cb)` in `InitWorker()`. Each callback re-validates preconditions before executing.
- **Concurrency**: Atomic `UPDATE WHERE status='pending'` + `RowsAffected` check prevents double-approve
- **Notifications**: Slack DM via `qtoolkit/slack.SendDM(email, message)` — best-effort, never blocks main flow

## Brand (双品牌拆分: kaitu / overleap)

**Spec**: `docs/superpowers/specs/2026-07-14-brand-split-design.md`. `Brand` (`brand.go`) is a registry-backed enum (`BrandKaitu` / `BrandOverleap`), not a config flag — `brandRegistry` holds per-brand hosts, CORS origins, OTT redirect root domain, base URL, support email, EDM sender name, payment channels.

- **Request brand resolution** (`resolveRequestBrand`, `BrandResolver()` middleware, mounted first on `/api`, `/app`, webhook groups): `Host` → `X-K2-Brand` header → default `kaitu`. Legacy clients (no header, any host) always resolve to kaitu — zero-breakage requirement. Read it downstream via `ReqBrand(c)`; stored in gin context under key `"brand"`.
- **`users.brand` is a birth attribute** — set at registration, immutable after. Auth enforces it: token's brand must match `ReqBrand(c)` or the request is rejected with **403003 `ErrorBrandMismatch`**.
- **`ScopeBrand(b)`** (`brand.go`) is the *only* legitimate brand filter for user-facing queries — a GORM scope that does `WHERE brand = ?`. Admin (`/app/*`) is the sole legitimate cross-brand view: it does **not** use `ScopeBrand`, instead takes an explicit `?brand=` query param parsed by `parseBrandFilter` (empty/invalid = no filter, i.e. all brands).
- **`BrandForCreate(s)`**: used on admin create DTOs (Plan, Campaign, Announcement, LicenseKeyBatch). Empty string → `BrandKaitu` (old admin UI stays zero-breakage); non-empty but invalid → **`ErrorInvalidArgument`** (reject, never silently downgrade to kaitu). Do not confuse with `Brand.Config()`'s own fallback (unknown brand → kaitu config), which is a different, more permissive rule used for read paths.
- **Node visibility**: `SlaveNode.VisibleKaitu *bool` (default `true`) / `VisibleOverleap *bool` (default `false`) + `(*SlaveNode) VisibleTo(b Brand) bool` (nil pointer = column default semantics). Enforced in 4 endpoints: `api_tunnel.go`, `api_tunnel_v20260717.go`, `api_subs.go`, `api_relay.go` — admin bypasses the filter.
- **Verification code cache key** is brand-scoped: `auth:code:email:<brand>:<hash>` (`VerificationCodePrefix` in `logic_auth.go`). A binary deploy that adds/changes the brand segment invalidates in-flight codes (TTL is minutes) — see deploy checklist.
- **Payment channel gate**: `Brand.Config().AllowsPayment(channel)` — kaitu allows `wordgate` + `apple_iap`; overleap allows `stripe` + `apple_iap` (Phase A; google_play lands with the Android release). Handlers reject a disallowed channel with **405001 `ErrorPaymentChannelUnavailable`** (`api_order.go`, `api_apple_iap.go`, `api_stripe.go`). The wordgate webhook, `creditAppleTransaction`, and `creditStripeInvoice` all carry a brand-mismatch sentinel that alerts (**`alertPaymentBrandMismatch`** in `brand.go`: error log + `slack.Send("alert", ...)`) and refuses to credit — fail-loud by design (persistent mismatch retry-storms from the provider's webhook retries), not a transient condition to silence.
- **viper legacy keys are kaitu-only**: `frontend_config.app_links.base_url`, `frontend_config.web_base_url`, `support.email` etc. continue to serve kaitu only (backward compat with existing config.yml). Overleap always resolves the equivalent value from `BrandConfig` in the registry — never from viper. See `api_app_config.go`, `logic_config.go`, `api_ticket.go`.
- **EDM dual sender**: `logic_email_task.go` picks `edmSenderOverleap` (`mail.Config("edm_overleap")`) when `brand == BrandOverleap` **and** `viper.GetString("edm.overleap_from_email")` is non-empty; otherwise falls back to the kaitu sender. Both keys (`edm.overleap_from_email` gate + `edm_overleap.*` qtoolkit sender block) must be set together — drift between them means the wrong From-address ships. 6 high-frequency system email templates (verification code, new-device login, web login confirm, device transfer, password login code, password changed) have branded English variants; the rest are kaitu-only by design (their entry points are brand-gated or channel-locked) — full list in `email_templates_overleap.go`'s header comment. `deviceKickTemplate` (设备踢下线通知) is reachable by overleap users but has no English variant yet — tracked as Phase 2 backlog.
- **EDM lazy-translation row now copies Brand**: `getTemplateForLanguage`'s auto-translation path (`logic_email_task.go`) builds the new `EmailMarketingTemplate` row via `buildTranslatedTemplate`, which copies `Brand` from the source template. This was a gap found in final review — the inline construction used to omit `Brand`, so every auto-translated template silently fell back to the GORM column default (`kaitu`), regardless of the source template's real brand. `TestBuildTranslatedTemplate_PreservesBrand` (`logic_email_task_test.go`) pins the fix.
- **Pure-email EDM (`UserID == 0`) resolves to a kaitu stub user**: `sendSingleTemplatedEmail` (`logic_email_send.go`) calls `FindOrCreateUserByEmail(ctx, item.Email)` with the Asynq task's plain `context.Context` — not a `*gin.Context` — so `FindOrCreateUserByEmail`'s brand-from-host resolution can't run and it defaults to `BrandKaitu` (see the function's own defensive fallback in `logic_user.go`). A brand-blind EDM batch targeting a raw email list therefore always creates (or reuses) a kaitu-brand stub user, even if the recipient is actually an Overleap customer reached by email address alone. Not a bug to silently patch — EDM batches must carry `UserID` (or an explicit brand) when the campaign is Overleap-scoped.
- **`X-K2-Brand` is spoofable — by design, and safe**: header wins only when Host doesn't resolve to a known brand, and even then it only decides which *public, unauthenticated* config/response a request gets back (e.g. `/api/app/config`, `/api/plans` for a non-brand host) — data that's equally public on the real brand's own site. The authenticated surface doesn't trust it: `AuthRequired()` compares the token's immutable `users.brand` against `ReqBrand(c)` and hard-rejects a mismatch with **403003 `ErrorBrandMismatch`**. Host-priority-over-header is intentional, not an oversight to "harden" later.

### Stripe (overleap 官网支付渠道, Phase 6)

- **Routes**: `POST /api/user/stripe/checkout` (Checkout Session, `mode=subscription`, returns `{url}`), `POST /api/user/stripe/portal` (Billing Portal `{url}`), `POST /webhook/stripe`. Files: `api_stripe.go` / `api_stripe_webhook.go` / `logic_stripe.go` (spec 提到的 `payment_stripe.go` 按本仓库命名铁律拆分).
- **Config**: viper `stripe.secret_key` / `stripe.webhook_secret` / `stripe.success_url` / `stripe.cancel_url` / `stripe.portal_return_url` — overleap 专用；URL 缺省回退 `BrandConfig(overleap).BaseURL`。缺 key/secret 时渠道自动不可用（handler 405001 / webhook 503），不 panic。
- **Single credit point**: `creditStripeInvoice` (`logic_stripe.go`) — `invoice.paid` 是唯一 bind+credit 事件；`subscription_data.metadata`(`user_uuid`/`plan_pid`/`brand`) 随每张 invoice 回传，事件自足。INV1 幂等键 `SubscriptionCredit(provider="stripe", transaction_id=invoice id)`；INV9 绑定键 = Stripe subscription id；INV3 叠加走 `applyGiftCredit`/`applyRenewalCredit`。入账 plan 查找用 `planByPIDForCredit`（不过滤 `is_active`——下架不停续费入账）。
- **Event-level idempotency**: `stripe_webhook_events` 表按 event id 去重（check → process → record）。
- **SDK-shape adapter**: `extractStripeInvoiceFacts` 是 stripe-go(v82/basil) invoice 形态的唯一适配点（`invoice.parent.subscription_details.*`、period 取 invoice line）。升 SDK 只改它。
- **Refund/dispute = passive**: `charge.refunded`/`charge.dispute.created` 只记账+Slack 告警，不自动 clawback、不置 `revoked`；主动退款走 admin 后续迭代。
- **Manage surface**: `DataSubscription.Manage.Kind == "stripe_portal"` → 客户端调 portal 端点换 URL 再跳转。
- **Reminders**: `processRenewalReminders` 跳过 `usersWithLiveAutoRenew`（apple/stripe 活跃自动续订用户不收"手动续费"邮件）。

### Apple IAP brand split (Phase A) + remaining seams

Phase A opened `apple_iap` for overleap with bundle-level isolation:

- **`planByAppleProductID`** (`logic_apple_iap.go`) is **brand-scoped** — `ScopeBrand` filter applied, preventing cross-brand product ID collisions.
- **`appstore.bundleIds.<brand>`** (viper): per-brand Apple bundle id for IAP verify. kaitu keeps legacy `appstore.bundleId`; other brands read `appstore.bundleIds.<brand>` via `appleBundleIDForBrand` — empty = fail-loud (verify refuses, no silent fallback to kaitu's bundle). `verifyAndGrantTransaction` loads the user's brand and sends that brand's bundle id to Apple, so a kaitu-app transaction can never credit an overleap account (e2e #09 pins this).

Still open before overleap Play Billing / campaign sends:
- **Overleap winback campaign codes**: `winbackCampaigns` (`worker_renewal_reminder.go`) has no overleap-scoped codes — `campaignVarsForBrand` returns an empty vars map for overleap recipients (silently no-op; verify intent once overleap runs campaigns).
- **`SavingsText` is hardcoded Chinese** (`worker_renewal_reminder.go`): leaks Chinese copy into English winback email the moment overleap gets a campaign code in `winbackCampaigns` — needs an English variant gated on `brand` before that happens.
- **Overleap lifecycle EDM templates** (`renewal-Nd` / `winback-Nd` slugs, brand=overleap) don't exist yet — reminder/winback sends to overleap users skip with a Slack alert until ops creates them.

## Campaign Matcher Semantics (single source of truth)

Campaign `matcherType` gates who may redeem a code (`logic_campaign.go getCampaignMatcherWithDB`). The names are **audience labels, not order-state checks** — read them as "who is this code for":

| matcherType | matches | use for |
|-------------|---------|---------|
| `first_order` | 新客 — `!IsFirstOrderDone` (nil = new) | 首单优惠、弃单召回（只发新客） |
| `vip` | 老客 — `IsFirstOrderDone == true` (= `IsVip()`) | 续费 / 召回老客 |
| `all` | anyone | 通用码 |
| `paid_before` | first paid before `matcherParams.beforeDate` | 时间窗定向 |
| `paid_before_active` | `paid_before` AND membership still active | 时间窗定向且在期 |

**`LicenseKeyBatch` is a different thing — don't fold it into campaigns.** 授权码批次是独立于活动码的分发单位：batch 自己存渠道标签 (`sourceTag`)、兑换条件 (`recipientMatcher`)、过期时间，统计维度包含兑换率和兑换→付费转化率。创建需走审批（见上面 Approval Workflow）。

`first_order` and `vip` are exact mirrors and must never collapse into the same meaning — `logic_campaign_matcher_test.go` pins both. **History (do not repeat):** `first_order` once meant "已付费" (duplicating `vip`) while every campaign author read the name/label as "new customer" — all 5 `first_order` campaigns (FIRST_ORDER_20, READY4U, STAYFREE, SMOOTHDAY, KEEPGOING) silently rejected 100% of recipients with `ErrorInvalidCampaignCode`. Fixed 2026-06-06 by aligning the code to the name. When adding a matcher, keep the name describing the **audience**, and mirror the admin UI label in `web/.../manager/campaigns/page.tsx`.

## Local Development

```bash
# Dependencies are the shared dev containers (dev-mariadb / dev-redis) managed
# at the user level via mysql-dev / redis-dev MCP. Connect on standard ports:
#   MySQL  127.0.0.1:3306  root:dev   database `kaitu`
#   Redis  127.0.0.1:6379  pw=dev     db=1
# Project no longer ships its own docker-compose for these — see api/docker-compose.yml.deprecated

# First time setup
cd api/cmd && go build -o kaitu-center . && ./kaitu-center migrate -c ../config.yml

# Run service
cd api/cmd && ./kaitu-center start -f -c ../config.yml   # Foreground mode

# CLI tools
./kaitu-center user add -e user@example.com -c ../config.yml
./kaitu-center user set-admin -e user@example.com -c ../config.yml
./kaitu-center health-check -c ../config.yml
```

## Constitution (Coding Conventions)

### Tunnel Scoring

- **The model is time-gated usage-sensitivity**: `score = 1 − trafficRatio · w(timeRatio)` where `w(t) = 0.15 + 0.85·t²`. The usage penalty's weight `w` climbs from a 0.15 floor at cycle start to 1.0 at cycle end, so early cycle is generous (a heavily-used node still scores high) and late cycle is strict (near-cap nodes get steered away). **The score is not an exhaustion check** — true exhaustion is handled by the hard cutoff / hide path (`isNodeOverQuota`), never by driving the score to 0. This replaced an earlier `trafficRatio − timeRatio` pacing model plus warmup/headroom terms; don't reintroduce those.
- **Single authority**: `ComputeRecommendScore(inst *DataTunnelInstance) float64` in `logic_tunnel_score.go` is the ONLY place that derives a tunnel's recommendation score `[0,1]` from its budget. `/api/tunnels` and `/api/subs` both call this helper — never inline a score formula elsewhere.
- **Nil instance = 0.5**: Non-cloud nodes get neutral 0.5, not 0. Zero would blacklist them from client-side `pickWeighted` / daemon `Subscription.Pick`.
- **Dual-emit**: `/api/subs` emits both `recommendScore: float` and legacy `weight: int = round(score*100)` for backward compat with pre-e210564 daemons. Drop `weight` one release after rollout is confirmed.
- **No Redis penalty layer**: The old Redis-based penalty scheme (`subsPenalty*` + `applyPenaltyWeights`) was removed in commit `9e12d0b` — it was patching the absence of real scoring, not solving it. Do not reintroduce request-side rate-limiting in the subscription response; if needed, compute a score server-side and expose it through `recommendScore`.

### Response Convention

- **HTTP status always 200** — error state in JSON `code` field. Never return HTTP 4xx/5xx from business endpoints.
- Use `Success(c, data)` for single objects, `List(c, items, pagination)` for paginated lists, `ItemsAll(c, items)` for unpaginated lists, `SuccessEmpty(c)` for void success.
- Use `Error(c, ErrorCode, "message")` for errors. Use predefined constants from `response.go` (e.g., `ErrorNotFound`, `ErrorInvalidArgument`). Never invent ad-hoc numeric codes.
- For rich error returns from logic layer, use `ErrorE(c, e(...))` with the `rerr` pattern.
- **Exception — webhooks**: Payment provider callbacks (e.g., `api_webhook.go`) return HTTP status codes directly because upstream providers use HTTP status for retry logic. Document this exception with a comment at the handler top.
- **Exception — asynqmon**: The embedded Asynq monitoring UI at `/app/asynqmon` returns HTML. This is intentional for its browser-based UI.

### Logging Convention

- Use `log.Errorf(c, ...)`, `log.Warnf(c, ...)`, `log.Debugf(c, ...)` from `qtoolkit/log`.
- **No redundant prefixes**: Write `log.Errorf(c, "failed to get tunnels: %v", err)`, NOT `log.Errorf(c, "[ERROR] failed to get tunnels: %v", err)`. The log level already carries the severity.
- **No Tracef for debug info**: Use `log.Debugf`, not `log.Tracef`. Trace is for protocol-level wire dumps, not application debug messages.
- Always pass `c` (gin.Context) as first arg to enable request-scoped tracing.

### Test Convention

**Three test tiers:**

| Tier | DB | Config | Guard | Example |
|------|-----|--------|-------|---------|
| Unit | None | None | None | Pure function tests |
| Mock DB | `SetupMockDB(t)` | `testInitConfig()` (auto) | None | Handler tests with go-sqlmock |
| Integration | Real MySQL | `config.yml` | `skipIfNoConfig(t)` | Full DB round-trip tests |

**Rules:**

- **Always use `SetupMockDB(t)`** for mock DB tests. This is the canonical helper in `mock_db_test.go`. It uses `SkipInitializeWithVersion: true` and `QueryMatcherRegexp`.
- **Guard integration tests with `skipIfNoConfig(t)`** at the top of each test function. This allows tests to run in CI without `config.yml`.
- **New GORM model columns need a manual migrate before integration tests see them**: the long-lived test DB is pre-migrated out-of-band — `testInitConfig()`/`skipIfNoConfig()` never call `AutoMigrate`. After adding a field to a model already in `migrate.go`'s `AutoMigrate(...)` list, run `cd api/cmd && go run . migrate --config ../../center/config.yml` once against the test DB, or integration tests fail with `Unknown column` (not a skip — a real DB error). Production doesn't need this: `center.Migrate()` runs automatically on service start.
- **Never panic on missing config**. `testInitConfig()` gracefully sets `testConfigAvailable = false` when `config.yml` is absent. Tests that need config must call `skipIfNoConfig(t)`.
- **Use `t.Cleanup()`** for teardown, not `defer` in test body.
- **Use `t.Helper()`** in all test helper functions.
- **Use testify `assert`/`require`** for assertions, not raw `if` checks.
- **Avoid zero-value assertions**: `assert.Equal(t, 0, resp.Code)` passes trivially on unmarshal failure. Always verify the positive case.
- **Test file naming**: `api_*_test.go` for handler tests, `db_mock_test.go` for shared mock utilities, `mock_db_test.go` for MockDB struct.

### GORM Model Convention

- Always specify `column:` tag when Go field name auto-derivation differs from DB column. Example: `DeviceUDID` → GORM derives `device_ud_id`, but DB has `device_udid`. Fix: `gorm:"column:device_udid"`.
- Use struct-based queries, not raw SQL strings.
- **Soft delete: use `db.Delete()`, not manual status field**. When model has `DeletedAt gorm.DeletedAt`, GORM auto-filters on `deleted_at`. A manual `status = "deleted"` field creates conflicting sources of truth — records appear in queries despite being "deleted".
- Soft delete via `gorm.DeletedAt` field with index.
- Timestamps: `CreatedAt`/`UpdatedAt` auto-managed by GORM.

### Route Convention

- All business routes under `/api/` prefix.
- Admin routes under `/app/` prefix.
- Slave node routes under `/slave/` prefix.
- CSR routes under `/csr/` prefix.
- Test routers must match production route prefixes. Use `/api/strategy/rules`, not `/api/k2v4/strategy/rules`.

### Import Convention

- Standard library first, then third-party, then internal packages.
- Use blank identifier import only for side effects (e.g., `_ "embed"` in templates package).

### Deprecated Stdlib

- `strings.Title` is deprecated. Use manual ASCII title-case: `strings.ToUpper(s[:1]) + strings.ToLower(s[1:])`.
- `golang.org/x/text/cases` is overkill for ASCII-only identifiers.

## Related Docs

- [Client Architecture](../CLAUDE.md)
- [Webapp Frontend](../webapp/CLAUDE.md)
