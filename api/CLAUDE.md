# Center Service

Central API service for authentication, user management, payments, tunnel management, and cloud infrastructure.

## Commands

```bash
cd api && go test ./... -count=1                           # Mock-only tier — silently SKIPS every DB test (see Test Convention)
cd api && go test -run TestName ./...                      # Run specific test
bash scripts/ci/api-db-test.sh [center-config.yml]         # Integration tier vs a real MariaDB (repo root; fails on any config-skip)
cd api/cmd && go build -o kaitu-center .                   # Build binary — cmd/ is its own Go module, see below
cd api/cmd && ./kaitu-center migrate -c ../config.yml      # Run DB migrations (first-time setup)
cd api/cmd && ./kaitu-center start -f -c ../config.yml     # Start foreground (dev)
./kaitu-center user add|set-admin -e user@example.com -c ../config.yml   # also del-admin / set-retailer / del-retailer / set-roles / send-email
./kaitu-center health-check -c ../config.yml               # other subcommands: stop, status, version, install, uninstall, unred create|delete
make deploy-api                                            # Build (go mod tidy in api/ AND api/cmd/) + deploy
# MySQL + Redis are the shared dev containers (dev-mariadb / dev-redis, managed via mysql-dev / redis-dev MCP):
#   MySQL  127.0.0.1:3306  root:dev   database `kaitu`
#   Redis  127.0.0.1:6379  pw=dev     db=1
# No project docker-compose any more (retired → api/docker-compose.yml.deprecated).
```

**`api/cmd` is a separate Go module** (`cmd/go.mod` with `replace github.com/kaitu-io/k2app/api => ../`, no `go.work`): `cd api && go test ./...` / `go vet ./...` never compile `cmd/`. Build, vet, and tidy it from `api/cmd`.

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

**Go 1.25** + Gin | **GORM** (MySQL/MariaDB) | **Redis** | **Asynq** (task queue) | **qtoolkit** (logging, DB, mail, slack)

## File Layout

| Pattern | Purpose | Example |
|---------|---------|---------|
| `api_*.go` | HTTP handlers | `api_auth.go`, `api_user.go`, `api_tunnel.go` |
| `api_admin_*.go` | Admin API handlers | `api_admin_order.go`, `api_admin_cloud.go` |
| `logic_*.go` | Business logic | `logic_auth.go`, `logic_order.go`, `logic_wallet.go` |
| `handler_*.go` | Helpers shared by Asynq handlers — **not** the handlers themselves | `handler_edm.go` (EDM send-log helpers only) |
| `worker_*.go` | Asynq task handlers (`handle*`) + cron; wired by `asynq.Handle` / `asynq.Cron` in `worker_integration.go InitWorker()` | `worker_cloud.go`, `worker_ech.go`, `worker_diagnosis.go` |
| `slave_api*.go` | Internal slave node APIs | `slave_api.go`, `slave_api_node.go` |
| `model*.go` | GORM data models | `model.go`, `model_wallet.go`, `model_push.go` |
| `type.go` | Request/response types | Role bitmask, API DTOs |
| `response.go` | Response helpers + error codes | `Success()`, `Error()`, `ListWithData()` |
| `middleware.go` | All middleware except `BrandResolver` (lives in `brand.go`) | Auth, roles, CORS, recovery |
| `route.go` | Route registration | All endpoint wiring |

## Architecture

```
cmd/                 CLI entry point, separate Go module (start/stop/status/migrate/health-check/version/install/uninstall, user *, unred *)
cloudprovider/       Multi-cloud VPS management (6 cloud providers + SSH standalone)
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

`User.RouterControlKey` (`*string`, `varchar(80)`, stored **plaintext** — accepted risk, it only controls a home router) is an account-level bearer credential every app instance on the account shares to talk to a headless k2r; k2r itself only ever holds the sha256. `POST /api/user/router-control-key` mints-or-returns idempotently (`EnsureRouterControlKey`, conditional `UPDATE … WHERE router_control_key IS NULL OR ''`, concurrent losers converge on the winner); `…/reset` rotates unconditionally (old holders get 401 from k2r and re-fetch). `/api/subs` injects `control_key_hash` for k2r's own refresh channel: the **gateway branch mints on serve** (`ensureAndInjectControlKeyHash`, closes the TOFU window for legacy routers), the **shared branch is read-only** (`injectControlKeyHash`) so a phone never silently provisions a router key. Files: `api_router_control_key.go`, `logic_router_control_key.go`, `api_subs.go`; client side: `webapp/CLAUDE.md` "Router Tab". The two `User` columns hit the "manual migrate" trap in Test Convention.

## API Response Format

```go
Success(c, &user)                    // Single object
ListWithData(c, items, pagination)   // List with pagination
Error(c, ErrorCode, "message")       // Error
```

### Error Codes

Base codes are HTTP-aligned (`0` none, `202` pending approval, `400`/`401`/`402`/`403`/`404`/`405`/`406`/`409`/`422`/`425`/`429`/`500`/`503`) — the constants and their meanings are in `response.go`; read it, this section only records the shape.

**Business-specific codes** — each is prefixed by the HTTP-aligned base whose semantics it refines. Pick the matching base; don't default everything to `400xxx`:

| Range | Constants | Refines |
|-------|-----------|---------|
| `400001`–`400013` | `ErrorInvalidCampaignCode` … `ErrorVerificationCodeExpired` | Bad request |
| `402001` | `ErrorPlanNoRouter` | Payment required |
| `403001`–`403003` | `ErrorRouterDeviceLimit` / `ErrorDeviceClassMismatch` / **`ErrorBrandMismatch`** | Forbidden |
| `405001` | `ErrorPaymentChannelUnavailable` | Not supported (brand gate) |
| `409001` | `ErrorEmailAlreadyInUse` | Conflict |
| `422001`–`422003` | `ErrorTierMismatch` / `ErrorProxyPurchaseDeprecated` / `ErrorInvalidClientClass` | Invalid argument |

> **Constitution**: Every error code added to `response.go` MUST be mirrored in `webapp/src/utils/errorCatalog.ts` (`errorCode.ts` is only the rendering shell). See `webapp/CLAUDE.md` "API Error Code Constitution" for the full checklist.

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
| `AuthRequired()` | Aborts 401 without valid auth; **403003 `ErrorBrandMismatch`** if a non-admin user's brand ≠ `ReqBrand(c)`; 403 if the user is blocked |
| `AuthOptional()` | Tries auth — but **still aborts 403003 on brand mismatch** (a cross-brand half-login must not leak into anonymous-allowed endpoints); blocked users degrade to anonymous |
| `ProRequired()` | Checks membership expiry, aborts 402 |
| `DeviceAuthRequired()` | Requires device context in auth |
| `EnforceDeviceClass()` | Validates the `X-K2-Client` class token — 422003 unknown token, 403002 class mismatch |
| `RouterRequired()` | 402001 `ErrorPlanNoRouter` unless `HasActivePrivateLines` |
| `AdminRequired()` | Superadmin only (`users.is_admin`) |
| `RoleRequired(bitmask)` | Per-route role check (`type.go` `Role*` bits, OR-combinable); `is_admin` bypasses |
| `RetailerRequired()` | Checks retailer role |
| `SlaveAuthRequired()` | Basic Auth (IPv4:NodeSecret) for slave nodes |
| `MiddleRecovery()` | Panic → **HTTP 500, empty body** + Slack alert — the one place a business route returns non-200 |
| `ApiCORSMiddleware()` | CORS for `/api/*` — allows localhost, loopback, RFC1918, capacitor:// |
| `CORSMiddleware()` | CORS for `/app/*` — union of every brand's `WebOrigins` + the local dev origin (`corsAllowedOrigins()`, sourced from `brandRegistry`) |
| `BrandResolver()` | In `brand.go` — see Brand section |

## Route Groups

| Group | Auth | Purpose |
|-------|------|---------|
| `/api/auth/*` | None | Login (OTP, password), refresh, logout |
| `/api/tunnels`, `/api/v20260717/tunnels` | Auth + DeviceClass + Pro + Device | VPN server list |
| `/api/relays` | Auth + DeviceClass + Pro + Device | Relay node list |
| `/api/subs` | None (Basic `udid:token` parsed in handler) | k2subs:// wire endpoint |
| `/api/user/*` | Auth (+ DeviceClass on most) | Profile, devices, orders, Stripe/IAP, email, members, delegate, router key |
| `POST /api/user/ticket`, `POST /api/user/device-log` | AuthOptional | Feedback ticket submission, device-log upload registration |
| `/api/user/tickets/*` | Auth | Own tickets: list / unread / detail / reply |
| `/api/invite/*` | Mixed | Invite codes CRUD |
| `/api/plans`, `/api/tiers`, `/api/app/config`, `/api/ech/config`, `/api/ca`, `/api/geo` | None | Public config |
| `/api/wallet/*` | Auth | Wallet, withdrawals |
| `/api/retailer/*` | Auth + Retailer | Retailer stats |
| `/api/strategy/*`, `/api/diagnosis/*` | Auth + DeviceClass + Device | Routing strategy rules, outbound-route diagnosis |
| `/api/router/*` | Auth + DeviceClass + Router | Router quota |
| `/api/telemetry/rule_miss` | **None** | Rule-miss telemetry |
| `/api/telemetry/batch` | Auth + DeviceClass + Device | Client telemetry batch |
| `/api/push/*`, `/api/survey/*` | Auth | Push tokens, survey |
| `/api/pair/*`, `/api/stats/*` | None (discover: Auth) | Pairing beacon, stats ingest |
| `/app/*` (group `admin`) | `AdminRequired()` — superadmin | Plans, users, wallets… (37 routes) |
| `/app/*` (group `opsAdmin`) | `AuthRequired()` + per-route `RoleRequired(bitmask)` | Tunnels, nodes, EDM, device-logs (`allOpsRoles`), feedback-tickets (list `allOpsRoles`; resolve/close/reply `RoleSupport`)… (76 routes) |
| `/app/approvals` list/detail/cancel | `AuthRequired()` only | Any role user sees own approvals; approve/reject are `AdminRequired()` |
| `/app/asynqmon` | `asynqmonAuthMiddleware()` (superadmin) | Asynq UI (HTML) |
| `/webhook/{wordgate,appstore,stripe}` | None | Payment callbacks (HTTP-status exception, see Response Convention) |
| `/slave/*` | `SlaveAuthRequired()` — **except `PUT /slave/nodes/:ipv4`** (registration), which is unauthenticated and validates `secretToken` from the body | Node management, status, usage, per-user device-traffic increments |
| `/csr/*` | None | Certificate signing requests |

## Cloud Provider (cloudprovider/)

Unified `Provider` interface (`provider.go`); constants `Provider*` there, one `<provider>.go` per provider (both Tencent variants share `tencent_lighthouse.go`):

| Provider | Cloud |
|----------|-------|
| `aliyun_swas` | Aliyun SWAS — domestic regions |
| `alibaba_swas` | Alibaba Cloud SWAS — international regions |
| `aws_lightsail` | AWS Lightsail multi-region (the only provider that keeps billing past quota — see `worker_cloud_overage.go`) |
| `tencent_lighthouse` | Tencent Lighthouse — **international** regions |
| `qcloud_lighthouse` | Tencent Lighthouse — **domestic** regions |
| `bandwagon` | BandwagonHost (VEID/APIKey) |
| `ssh_standalone` | No cloud API, direct SSH |

## Background Workers (Asynq)

| Worker | Purpose |
|--------|---------|
| `worker_integration.go` | `InitWorker()` — `asynq.Handle(TaskType…, handle…)` for every handler, `asynq.Cron` schedules, approval-callback registry |
| `worker_cloud.go` | Cloud instance sync, change-IP, create, delete |
| `worker_cloud_overage.go` | AWS Lightsail overage guard, tail step of `syncAccount` per instance (three layers, provider-reported usage is authoritative) |
| `worker_ech.go` | ECH key rotation |
| `worker_diagnosis.go` | Route diagnosis aggregation |
| `worker_renewal_reminder.go` | Membership renewal reminders + winback campaigns (daily 02:30) |
| `worker_abandoned_order.go` | Unpaid-order recall emails (hourly + daily) |
| `worker_retailer_followup.go` | Retailer follow-up notifications (every minute) |
| `worker_ticket_notify.go` | Aggregates un-notified admin ticket replies → email |
| `worker_private_node.go` | Private-node provisioning task + provisioning-timeout sweep (30 min tolerance, every 10 min) |
| `worker_private_node_lifecycle.go` | Daily private-node subscription lifecycle labels, grace→suspended cutover, renewal reclaim (`IsServiceable` timestamps stay authoritative) |
| `worker_private_node_traffic_warning.go` | Tiered private-line traffic warnings, per-tier dedupe (every 30 min) |
| `worker_subscription_reconcile.go` | Daily Apple/Stripe subscription reconcile — the fallback when webhooks are lost (spec 2026-08-22) |
| `worker_traffic_abuse.go` | 每小时聚合当月 per-user 流量，超阈值（`traffic.abuse_monthly_gb`，缺省 100GB）Slack 告警 + 用户警告邮件（模板 `traffic-abuse-warning`，月度去重）+ 60 天保留清理（隐私政策承诺 2 个月） |
| `worker_stats_retention.go` | 每日分批清扫无界时序表：`slave_node_loads` >30d（读取端只取每节点最新一条）、`stat_*`/`connection_ratings` >120d（admin 报表最大回看 90d）；stat 表按 `reported_at`（服务端权威，`created_at` 是客户端时钟含脏数据） |
| `handler_edm.go` | `createEmailSendLog` / `updateEmailSendLogStatus` helpers — not an Asynq handler despite the prefix |

Asynqmon UI available at `/app/asynqmon` (admin auth required).

## Approval Workflow (Maker-Checker)

Critical admin operations (EDM, campaigns, plans, withdrawals, hard delete, license key batches, order refunds) require dual approval via `SubmitApproval()`. Superadmin (`is_admin`) bypasses approval and executes synchronously. Non-superadmin creates a pending record requiring another admin's approval.

- **Core files**: `logic_approval.go` (service), `logic_approval_callbacks.go` (12 callbacks: `edm_send`, `campaign_{create,update,delete}`, `license_key_batch_{create,invalidate}`, `user_hard_delete`, `plan_{update,delete}`, `withdraw_{approve,complete}`, `order_refund`), `api_admin_approval.go` (handlers)
- **Pattern**: Handler validates → `approvalID, executed, err := SubmitApproval(c, action, params, summary)` → `!executed` ⇒ `PendingApproval(c, approvalID)` (202); superadmin path continues to `Success`
- **Callback registry**: `RegisterApprovalCallback(action, cb)` in `InitWorker()`. Each callback re-validates preconditions before executing.
- **Concurrency**: Atomic `UPDATE WHERE status='pending'` + `RowsAffected` check prevents double-approve
- **Notifications**: Slack DM via `qtoolkit/slack.SendDM(email, message)` — best-effort, never blocks main flow

## Brand (双品牌拆分: kaitu / overleap)

**Spec**: `docs/superpowers/specs/2026-07-14-brand-split-design.md`. `Brand` (`brand.go`) is a registry-backed enum (`BrandKaitu` / `BrandOverleap`), not a config flag — `brandRegistry` holds per-brand hosts, CORS origins, OTT redirect root domain, base URL, support email, EDM sender name, payment channels.

- **Cross-layer contract gate — regenerate after touching any brand data**: `cd api && UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract ./...`. The golden `contracts/api-contract.json` is exported from Go live values by `contract_export_test.go`; it lives outside the module so `-count=1` is mandatory (the go test cache never rechecks it — a hand-edited golden returns a stale `ok (cached)`); the golden is read-only (never auto-rewritten without the env var) and must be committed with the code.
- **Request brand resolution** (`resolveRequestBrand`, `BrandResolver()` middleware, mounted first on `/api`, `/app`, webhook groups): `Host` → `X-K2-Brand` header → default `kaitu`. Legacy clients (no header, any host) always resolve to kaitu — zero-breakage requirement. Read it downstream via `ReqBrand(c)`; stored in gin context under key `"brand"`.
- **`users.brand` is a birth attribute** — set at registration, immutable after. Both `AuthRequired()` and `AuthOptional()` enforce it: a non-admin user's brand must match `ReqBrand(c)` or the request is rejected with **403003 `ErrorBrandMismatch`**. Admins are exempt — they are the legitimate cross-brand view.
- **`ScopeBrand(b)`** (`brand.go`) is the *only* legitimate brand filter for user-facing queries — a GORM scope that does `WHERE brand = ?`. Admin (`/app/*`) is the sole legitimate cross-brand view: it does **not** use `ScopeBrand`, instead takes an explicit `?brand=` query param parsed by `parseBrandFilter` (empty/invalid = no filter, i.e. all brands).
- **`BrandForCreate(s)`**: used on admin create DTOs (Plan, Campaign, Announcement, LicenseKeyBatch). Empty string → `BrandKaitu` (old admin UI stays zero-breakage); non-empty but invalid → **`ErrorInvalidArgument`** (reject, never silently downgrade to kaitu). Do not confuse with `Brand.Config()`'s own fallback (unknown brand → kaitu config), which is a different, more permissive rule used for read paths.
- **Node visibility**: 生效可见性 = **节点自声明该品牌**（`SlaveNode.Brands`，来自节点 `.env` 的 `K2_NODE_BRANDS`，每次注册重发；空 = 只声明 kaitu） **∧ 运营没下架**（`VisibleKaitu` / `VisibleOverleap` `*bool`，**都默认 `true`** —— 语义是下架用的 kill switch，不是上架许可；nil = 未下架）。判定唯一入口是 `(*SlaveNode) VisibleTo(b Brand) bool`（`DeclaresBrand(b) ∧ 开关`）——只翻 admin 的 `visibleOverleap` 开关**不能**把节点放上 Overleap，节点必须同时自声明 `overleap`。Enforced in 4 endpoints: `api_tunnel.go`, `api_tunnel_v20260717.go`, `api_subs.go`, `api_relay.go` — admin bypasses the filter.
- **Verification code cache key** is brand-scoped: `auth:code:email:<brand>:<hash>` (`VerificationCodePrefix` in `logic_auth.go`). A binary deploy that adds/changes the brand segment invalidates in-flight codes (TTL is minutes) — see deploy checklist.
- **Payment channel gate**: `Brand.Config().AllowsPayment(channel)` — kaitu allows `wordgate` + `apple_iap`; overleap allows `stripe` + `apple_iap` (Phase A; google_play lands with the Android release). Handlers reject a disallowed channel with **405001 `ErrorPaymentChannelUnavailable`** (`api_order.go`, `api_apple_iap.go`, `api_stripe.go`). The wordgate webhook, `creditAppleTransaction`, and `creditStripeInvoice` all carry a brand-mismatch sentinel that alerts (**`alertPaymentBrandMismatch`** in `brand.go`: error log + `slack.Send("alert", ...)`) and refuses to credit — fail-loud by design (persistent mismatch retry-storms from the provider's webhook retries), not a transient condition to silence.
- **viper legacy keys are kaitu-only**: `frontend_config.app_links.base_url`, `frontend_config.web_base_url`, `support.email` etc. continue to serve kaitu only (backward compat with existing config.yml). Overleap always resolves the equivalent value from `BrandConfig` in the registry — never from viper. See `api_app_config.go`, `logic_config.go`, `api_ticket.go`.
- **EDM dual sender**: `logic_email_task.go` picks `edmSenderOverleap` (`mail.Config("edm_overleap")`) when `brand == BrandOverleap` **and** `viper.GetString("edm.overleap_from_email")` is non-empty; otherwise falls back to the kaitu sender. Both keys (`edm.overleap_from_email` gate + `edm_overleap.*` qtoolkit sender block) must be set together — drift between them means the wrong From-address ships. 6 high-frequency system email templates (verification code, new-device login, web login confirm, device transfer, password login code, password changed) have branded English variants; the rest are kaitu-only by design (their entry points are brand-gated or channel-locked) — full list in `email_templates_overleap.go`'s header comment. `deviceKickTemplate` (设备踢下线通知) is reachable by overleap users but has no English variant yet — tracked as Phase 2 backlog.
- **EDM lazy-translation row now copies Brand**: `getTemplateForLanguage`'s auto-translation path (`logic_email_task.go`) builds the new `EmailMarketingTemplate` row via `buildTranslatedTemplate`, which copies `Brand` from the source template. This was a gap found in final review — the inline construction used to omit `Brand`, so every auto-translated template silently fell back to the GORM column default (`kaitu`), regardless of the source template's real brand. `TestBuildTranslatedTemplate_PreservesBrand` (`logic_email_task_test.go`) pins the fix.
- **Pure-email EDM (`UserID == 0`) resolves to a kaitu stub user**: `sendSingleTemplatedEmail` (`logic_email_send.go`) calls `FindOrCreateUserByEmail(ctx, item.Email)` with the Asynq task's plain `context.Context` — not a `*gin.Context` — so `FindOrCreateUserByEmail`'s brand-from-host resolution can't run and it defaults to `BrandKaitu` (see the function's own defensive fallback in `logic_user.go`). A brand-blind EDM batch targeting a raw email list therefore always creates (or reuses) a kaitu-brand stub user, even if the recipient is actually an Overleap customer reached by email address alone. Not a bug to silently patch — EDM batches must carry `UserID` (or an explicit brand) when the campaign is Overleap-scoped.
- **`X-K2-Brand` is spoofable — by design, and safe**: header wins only when Host doesn't resolve to a known brand, and even then it only decides which *public, unauthenticated* config/response a request gets back (e.g. `/api/app/config`, `/api/plans` for a non-brand host) — data that's equally public on the real brand's own site. The authenticated surface doesn't trust it: `AuthRequired()` / `AuthOptional()` compare the token's immutable `users.brand` against `ReqBrand(c)` and hard-reject a mismatch with **403003 `ErrorBrandMismatch`** (admins exempt). Host-priority-over-header is intentional, not an oversight to "harden" later.

### Stripe (overleap 官网支付渠道, Phase 6)

- **Routes**: `POST /api/user/stripe/checkout` (Checkout Session, `mode=subscription`, returns `{url}`), `POST /api/user/stripe/portal` (Billing Portal `{url}`), `POST /webhook/stripe`. Files: `api_stripe.go` / `api_stripe_webhook.go` / `logic_stripe.go` (spec 提到的 `payment_stripe.go` 按本仓库命名铁律拆分).
- **Config**: viper `stripe.secret_key` / `stripe.webhook_secret` / `stripe.success_url` / `stripe.cancel_url` / `stripe.portal_return_url` — overleap 专用；URL 缺省回退 `BrandConfig(overleap).BaseURL`。缺 key/secret 时渠道自动不可用（handler 405001 / webhook 503），不 panic。
- **Single credit point**: `creditStripeInvoice` (`logic_stripe.go`) — `invoice.paid` 是唯一 bind+credit 事件；`subscription_data.metadata`(`user_uuid`/`plan_pid`/`brand`) 随每张 invoice 回传，事件自足。INV1 幂等键 `SubscriptionCredit(provider="stripe", transaction_id=invoice id)`；INV9 绑定键 = Stripe subscription id；INV3 叠加走 `applyGiftCredit`/`applyRenewalCredit`。入账 plan 查找用 `planByPIDForCredit`（不过滤 `is_active`——下架不停续费入账）。
- **Event-level idempotency**: `stripe_webhook_events` 表按 event id 去重（check → process → record）。
- **SDK-shape adapter**: `extractStripeInvoiceFacts` 是 stripe-go(v82/basil) invoice 形态的唯一适配点（`invoice.parent.subscription_details.*`、period 取 invoice line）。升 SDK 只改它。
- **Refund/dispute = passive**: `charge.refunded`/`charge.dispute.created` 只记账+Slack 告警，不自动 clawback、不置 `revoked`；主动退款走 admin 后续迭代。
- **Manage surface**: `DataSubscription.Manage.Kind == "stripe_portal"` → 客户端调 portal 端点换 URL 再跳转。
- **多币种展示价**: `DataPlan.CurrencyPrices`（`logic_stripe_price.go`）—— Stripe 套餐的 `/api/plans` 附带 `{币种→最小单位}`（主币 + `currency_options` 全部币种），从 Stripe Price 取（`expand=currency_options`）、进程内缓存 1h、失败只记日志字段省略（客户端回落 `Price` 美元）。Price 主币 **USD**（账号结算币；只有主币=结算币 Adaptive Pricing 才生效），GBP/EUR 为固定本币价，Checkout 按属地自动选币——建价只走 `scripts/stripe-setup-overleap.sh`（spec `2026-09-04-overleap-site-decoupling-and-uk-positioning-design.md` §4）。
- **Reminders**: `processRenewalReminders` 跳过 `usersWithLiveAutoRenew`（apple/stripe 活跃自动续订用户不收"手动续费"邮件）。

### Apple IAP brand split (Phase A) + remaining seams

Phase A opened `apple_iap` for overleap with bundle-level isolation:

- **`planByAppleProductID`** (`logic_apple_iap.go`) is **brand-scoped** — `ScopeBrand` filter applied, preventing cross-brand product ID collisions.
- **`appstore.bundleIds.<brand>`** (viper): per-brand Apple bundle id for IAP verify. kaitu keeps legacy `appstore.bundleId`; other brands read `appstore.bundleIds.<brand>` via `appleBundleIDForBrand` — empty = fail-loud (verify refuses, no silent fallback to kaitu's bundle). `verifyAndGrantTransaction` loads the user's brand and sends that brand's bundle id to Apple, so a kaitu-app transaction can never credit an overleap account (e2e #09 pins this).

Invariants from the 2026-08-10 sandbox-order incident (narrative: `docs/incidents/2026-08-10-apple-iap-sandbox-order.md`):

- **Sandbox transactions grant entitlement but never create an `Order`** — `creditAppleTransaction` returns before order creation when `info.Environment == appstore.Environment_Sandbox`. Orders are financial entities: `PayAmount` feeds cashback, revenue stats, and wallet refunds. `iapOrderFixture` defaults to `Environment_Production` for the same reason — flipping it tests a path the gate cuts off.
- **IAP refund reverses entitlement via `SubscriptionCredit.ID`, not the order id** — `orderEntitlementSecondsInTx` (`logic_order.go`) reads `orders.apple_transaction_id → SubscriptionCredit.CreditedSeconds`; IAP `UserProHistory.reference_id` is the credit row id, so querying by order id yields 0 ("money refunded, entitlement untouched").
- **An IAP order with empty `apple_transaction_id` refuses refund** — never degrade to "pay wallet, deduct 0 seconds". Refund ≠ unsubscribe (Apple keeps billing; `DID_RENEW` creates a new order). Wallet-then-Apple double refund is detected by `alertIfAlreadyWalletRefunded` → Slack `[DOUBLE-REFUND]`, alert-only.

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

## Constitution (Coding Conventions)

### Tunnel Scoring

- **The model is time-gated usage-sensitivity**: `score = 1 − trafficRatio · w(timeRatio)` where `w(t) = 0.15 + 0.85·t²`. The usage penalty's weight `w` climbs from a 0.15 floor at cycle start to 1.0 at cycle end, so early cycle is generous (a heavily-used node still scores high) and late cycle is strict (near-cap nodes get steered away). **The score is not an exhaustion check** — true exhaustion is handled by the hard cutoff / hide path (`isNodeOverQuota`), never by driving the score to 0. This replaced an earlier `trafficRatio − timeRatio` pacing model plus warmup/headroom terms; don't reintroduce those.
- **Single authority**: `ComputeRecommendScore(inst *DataTunnelInstance) float64` in `logic_tunnel_score.go` is the ONLY place that derives a tunnel's recommendation score `[0,1]` from its budget. `/api/tunnels` and `/api/subs` both call this helper — never inline a score formula elsewhere.
- **Nil instance = 0.5**: Non-cloud nodes get neutral 0.5, not 0. Zero would blacklist them from client-side `pickWeighted` / daemon `Subscription.Pick`.
- **Dual-emit**: `/api/subs` emits both `recommendScore: float` and legacy `weight: int = round(score*100)` for backward compat with pre-e210564 daemons. Drop `weight` one release after rollout is confirmed.
- **No Redis penalty layer**: The old Redis-based penalty scheme (`subsPenalty*` + `applyPenaltyWeights`) was removed in commit `9e12d0b` — it was patching the absence of real scoring, not solving it. Do not reintroduce request-side rate-limiting in the subscription response; if needed, compute a score server-side and expose it through `recommendScore`.

### Response Convention

- Use `Success(c, data)` for single objects, `List(c, items, pagination)` for paginated lists, `ItemsAll(c, items)` for unpaginated lists, `SuccessEmpty(c)` for void success.
- Use `Error(c, ErrorCode, "message")` for errors. Use predefined constants from `response.go` (e.g., `ErrorNotFound`, `ErrorInvalidArgument`). Never invent ad-hoc numeric codes.
- For rich error returns from logic layer, use `ErrorE(c, e(...))` with the `rerr` pattern.
- **Exceptions to the always-200 rule** (Hard Rules): **webhooks** (`api_webhook.go`, `api_stripe_webhook.go`, …) return HTTP status directly because providers retry on status — comment it at the handler top; **asynqmon** (`/app/asynqmon`) returns HTML; **`MiddleRecovery`** turns a panic into HTTP 500 with an empty body + Slack alert.

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
| Mock DB | `SetupMockDB(t)` | `testInitConfig()` — called by `SetupTestRouter()` / `skipIfNoConfig()`, **not** by `SetupMockDB` | None | Handler tests with go-sqlmock |
| Integration | Real MySQL | `../center/config.yml` (repo-root `center/`) | `skipIfNoConfig(t)` | Full DB round-trip tests |

**Rules:**

- **Silent skip is the default — a green `go test ./...` proves nothing about the DB half.** `skipIfNoConfig` looks for `../center/config.yml` (repo-root `center/`, gitignored) while `start`/`migrate` read `api/config.yml` (also gitignored) — two different files, neither in git; `.github/ci/center-config.yml` is the only in-repo template. The CI mock job (`go test ./... -count=1`) silently skipped **256 of 1085** tests at 0.4.8 and reported green; the separate `test-api-db` job (`scripts/ci/api-db-test.sh`: throwaway MariaDB + real `migrate`) **fails on any config-skip**. Locally the discriminator is `-v` showing 0 `config.yml not available` skips.
- **Tests never touch real Redis**: `testInitConfig()` (`testutil_test.go`) starts miniredis and overrides `redis.*` viper keys before and after the config load, and sets `EnableMockVerificationCode = true`. `testInitOnce` is a plain bool, not `sync.Once` — unsafe under `t.Parallel` (currently unused anywhere).
- **Always use `SetupMockDB(t)`** for mock DB tests. This is the canonical helper in `mock_db_test.go`. It uses `SkipInitializeWithVersion: true` and `QueryMatcherRegexp`.
- **Guard integration tests with `skipIfNoConfig(t)`** at the top of each test function. This allows tests to run in CI without `config.yml`.
- **New GORM model columns need a manual migrate before integration tests see them**: the long-lived test DB is pre-migrated out-of-band — `testInitConfig()`/`skipIfNoConfig()` never call `AutoMigrate`. After adding a field to a model already in `migrate.go`'s `AutoMigrate(...)` list, run `cd api/cmd && go run . migrate --config ../../center/config.yml` once against the test DB, or integration tests fail with `Unknown column` (not a skip — a real DB error). Production doesn't need this: `center.Migrate()` runs automatically on service start.
- **Never panic on missing config**. `testInitConfig()` gracefully sets `testConfigAvailable = false` when `config.yml` is absent. Tests that need config must call `skipIfNoConfig(t)`.
- **Use `t.Cleanup()`** for teardown, not `defer` in test body.
- **Use `t.Helper()`** in all test helper functions.
- **Use testify `assert`/`require`** for assertions, not raw `if` checks.
- **Avoid zero-value assertions**: `assert.Equal(t, 0, resp.Code)` passes trivially on unmarshal failure. Always verify the positive case.
- **Test file naming**: `api_*_test.go` for handler tests, `db_mock_test.go` for shared mock utilities, `mock_db_test.go` for MockDB struct, `testutil_test.go` for `testInitConfig` / `skipIfNoConfig` / `SetupTestRouter`.

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

## Related Docs

- [Client Architecture](../CLAUDE.md)
- [Webapp Frontend](../webapp/CLAUDE.md)
