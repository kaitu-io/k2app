# Center Service

Central API service for authentication, user management, payments, tunnel management, and cloud infrastructure. 本文件只留"改 api 必须知道、违反了不会报错"的规则 + 指向 `docs/api/` 的指针；细节用到时再读。

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
# Dev MySQL (127.0.0.1:3306, database `kaitu`) + Redis (127.0.0.1:6379) are served by the `devdb` MCP:
#   devdb_status = which side serves the ports now (pve ssh tunnel / local colima); devdb_switch flips it.
#   Credentials / redis db: read center/config.yml. No project docker-compose (retired → api/docker-compose.yml.deprecated).
```

**`api/cmd` is a separate Go module** (`cmd/go.mod` with `replace github.com/kaitu-io/k2app/api => ../`, no `go.work`): `cd api && go test ./...` / `go vet ./...` never compile `cmd/`. Build, vet, and tidy it from `api/cmd`.

## Hard Rules

```
Prohibited:
  Swagger/Swag annotations
  GORM raw SQL string queries (use struct queries only)
  Ad-hoc numeric error codes (use the constants in response.go)
  Manual status="deleted" on a model with gorm.DeletedAt (use db.Delete(); two sources of truth)

Required:
  File naming: api_*.go, logic_*.go, model*.go, worker_*.go (flat package — no domain subdirectories)
  HTTP status always 200 — error state in JSON code field (exceptions below)
  GORM `column:` tag whenever name derivation differs (DeviceUDID → device_ud_id, DB has device_udid)
  Route prefixes: business /api/, admin /app/, slave /slave/, CSR /csr/ — test routers use the production prefixes
  Credential / PII routes in a *Sensitive group (see Request Logging)
```

**Always-200 exceptions**: **webhooks** (`api_webhook.go`, `api_stripe_webhook.go`, …) return HTTP status directly because providers retry on status — comment it at the handler top; `/api/antiblock/seed` returns real 503/401 so CI `curl -fsS` fails loudly; **asynqmon** returns HTML; **`MiddleRecovery`** turns a panic into HTTP 500 with an empty body + Slack alert.

### Request Logging — 静默泄漏凭证的头号来源

普通组挂 `log.MiddlewareRequestLog(true)`：它把 POST/PUT/PATCH **请求体**和 JSON **响应体**原样写进 app.log。**请求或响应里带凭证（密码、验证码、idToken、access/refresh token）或个人敏感信息（收款账号、真实姓名、解密后的字段）的新路由，必须挂进 `*Sensitive` 组**（`route.go`：`walletSensitive` / `socialSensitive` / `adminSensitive`，用 `requestLogWithoutBodies()` 只记方法 / 路由 / 状态 / 耗时），并加一个 `*_NotBodyLogged` 接线守卫测试（范本 `TestSocialTokenRoutes_NotBodyLogged`、`TestAlipayRoutes_NotBodyLogged`）。挂错组不报错、测试全绿，只有翻 app.log 才看得见。`/webhook/slack/events` 刻意不挂任何请求日志（验签前会整体读入 body）。

## Tech Stack

**Go 1.26** (`go.mod` `go 1.26.0`) + Gin | **GORM** (MySQL/MariaDB) | **Redis** | **Asynq** (task queue) | **qtoolkit** (logging, DB, mail, slack)

## File Layout

| Pattern | Purpose | Example |
|---------|---------|---------|
| `api_*.go` / `api_admin_*.go` | HTTP handlers / admin handlers | `api_auth.go`, `api_admin_order.go` |
| `logic_*.go` | Business logic | `logic_auth.go`, `logic_order.go`, `logic_wallet.go` |
| `handler_*.go` | Helpers shared by Asynq handlers — **not** the handlers themselves | `handler_edm.go` (EDM send-log helpers only) |
| `worker_*.go` | Asynq task handlers (`handle*`) + cron | `worker_cloud.go`, `worker_chat.go` |
| `slave_api*.go` | Internal slave node APIs | `slave_api.go`, `slave_api_node.go` |
| `model*.go` | GORM data models | `model.go`, `model_wallet.go`, `model_chat.go` |
| `type.go` | Request/response types | Role bitmask, API DTOs |
| `response.go` | Response helpers + error codes | see below |
| `middleware.go` | All middleware except `BrandResolver` / `BrandRequired` (in `brand.go`) | Auth, roles, CORS, recovery |
| `route.go` | Route registration (chat routes via `registerChatRoutes` / `registerAdminChatRoutes`) | All endpoint wiring |
| `cmd/` | CLI entry point, separate Go module | start/stop/status/migrate/… |
| `cloudprovider/` | Multi-cloud VPS management | see Cloud Provider |

## Response & Error Codes

```go
Success(c, &user)                    // single object
List(c, items, pagination)           // paginated list (nil pagination → ItemsAll + warning)
ItemsAll(c, items)                   // unpaginated list
SuccessEmpty(c)                      // void success
PendingApproval(c, approvalID)       // code 202 — submitted for approval
Error(c, ErrorCode, "message")       // error; ErrorE(c, e(...)) for rich logic-layer errors (rerr pattern)
```

`ListWithData` is only a backward-compat alias of `List` — new code calls `List`.

Base codes are HTTP-aligned (`0` none, `202` pending approval, `400`/`401`/`402`/`403`/`404`/`405`/`406`/`409`/`422`/`425`/`429`/`500`/`503`) — constants and meanings in `response.go`. **Business-specific codes** are prefixed by the HTTP-aligned base whose semantics they refine; pick the matching base, don't default everything to `400xxx`:

| Range | Constants | Refines |
|-------|-----------|---------|
| `400001`–`400013` | `ErrorInvalidCampaignCode` … `ErrorVerificationCodeExpired` | Bad request |
| `400014`–`400015` | `ErrorSocialTokenInvalid` / `ErrorSocialEmailUnverified` | Bad request (联合登录) |
| `402001` | `ErrorPlanNoRouter` | Payment required |
| `403001`–`403003` | `ErrorRouterDeviceLimit` / `ErrorDeviceClassMismatch` / **`ErrorBrandMismatch`** | Forbidden |
| `403004`–`403006` | `ErrorDeviceAccountDisabled`（品牌不支持设备账号）/ `ErrorDeviceAccountLoginRequired`（该设备属有身份用户，需登录）/ `ErrorDeviceAccountProofRequired`（属设备账号，需 Keychain token 或 Apple 恢复） | Forbidden (设备账号) |
| `405001` | `ErrorPaymentChannelUnavailable` | Not supported (brand gate) |
| `405002` | `ErrorSocialProviderDisabled` | Not supported (provider 未配置) |
| `409001`–`409003` | `ErrorEmailAlreadyInUse` / `ErrorSocialIdentityTaken` / `ErrorSocialProviderAlreadyBound` | Conflict |
| `409004` | `ErrorAppleSubscriptionOwnedElsewhere`（Apple 订阅属于另一台设备的账号） | Conflict |
| `422001`–`422003` | `ErrorTierMismatch` / `ErrorProxyPurchaseDeprecated` / `ErrorInvalidClientClass` | Invalid argument |

> **Constitution**: Every error code added to `response.go` MUST be mirrored in `webapp/src/utils/errorCatalog.ts` (`errorCode.ts` is only the rendering shell). See `webapp/CLAUDE.md` "API Error Code Constitution".

## Middleware (middleware.go)

**Auth chain** (priority): ① HttpOnly cookie `access_token` (non-GET: `X-CSRF-Token` must match `csrf_token` cookie) ② `X-Access-Key` (retailer API key, no device context) ③ `Authorization: Bearer` ④ `?token=` (WebSocket cross-domain). Cookie auth slides: renews if remaining lifetime < 7 days.

| Middleware | Purpose |
|-----------|---------|
| `AuthRequired()` | `/api/*` auth. 401 without auth; **403003** if a non-admin user's brand ≠ `ReqBrand(c)` (only `IsAdmin` exempt); 403 if blocked |
| `StaffAuthRequired()` | `/app/*` auth (`opsAdmin`, approvals). Same as `AuthRequired` but brand exemption covers `IsAdmin` **or any non-`RoleUser` role bit** (`isCrossBrandStaff`). Real permission still comes from `RoleRequired` / `AdminRequired` |
| `AuthOptional()` | Tries auth — **still aborts 403003 on brand mismatch** (no cross-brand half-login on anonymous endpoints); blocked users degrade to anonymous |
| `ProRequired()` | Membership expiry, aborts 402 |
| `DeviceAuthRequired()` | Requires device context |
| `EnforceDeviceClass()` | `X-K2-Client` class token — 422003 unknown, 403002 mismatch |
| `RouterRequired()` | 402001 unless `HasActivePrivateLines` |
| `WalletRequired()` | Brand gate on **every** `/api/wallet*` route; mount **after** `AuthRequired()` — reads the **user's** brand (`BrandConfig.Wallet`), not the request brand. Missing it → `TestWalletRoutes_AllGatedForBrandsWithoutWallet` (enumerates production `SetupRouter`) goes red |
| `AdminRequired()` | Superadmin only (`users.is_admin`) |
| `RoleRequired(bitmask)` | Per-route role check (`type.go` `Role*` bits, OR-combinable); `is_admin` bypasses |
| `RetailerRequired()` | Retailer role |
| `SlaveAuthRequired()` | Basic Auth (IPv4:NodeSecret) for slave nodes |
| `MiddleRecovery()` | Panic → HTTP 500, empty body + Slack alert |
| `requestLogWithoutBodies()` | Metadata-only request log for `*Sensitive` groups |
| `ApiCORSMiddleware()` / `CORSMiddleware()` | `/api/*`: localhost, loopback, RFC1918, capacitor:// · `/app/*`: union of every brand's `WebOrigins` + local dev origin (`corsAllowedOrigins()`) |
| `BrandResolver()` | In `brand.go` — see Brand |

## Route Groups (route.go)

| Group | Logging | Auth | Purpose |
|-------|---------|------|---------|
| `/api/auth/*` | body | None (`/logout`: Auth) | OTP / password login, web-login, refresh, OTT exchange, device-account |
| `/api/tunnels`, `/api/v20260717/tunnels`, `/api/relays` | body | Auth + DeviceClass + Pro + Device | VPN server / relay list |
| `/api/subs` | body | None (Basic `udid:token` parsed in handler) | k2subs:// wire endpoint |
| `/api/user/*` | body | Auth (+ DeviceClass on most; `gateway-credential` / `router-control-key` deliberately without) | Profile, devices, orders, Stripe/IAP, email, members, delegate, router, password, tickets |
| `POST /api/user/ticket`, `POST /api/user/device-log` | body | AuthOptional | Feedback ticket, device-log registration |
| `/api/invite/*` | body | Mixed | Invite codes |
| `/api/plans`, `/api/products/:product/plans`, `/api/tiers`, `/api/app/config`, `/api/ech/config`, `/api/ca`, `/api/geo`, `GET /api/license-keys/code/:code` | body | None | Public config |
| `/api/antiblock/seed` | body | in-handler shared secret | Antiblock relay seed (real HTTP codes) |
| `GET /api/orders/:uuid/pay` | body | None (uuid is the credential) | 代付邮件耐久支付链接 → 302 checkout |
| `/api/wallet/*` | body | Auth + Wallet | Wallet, withdrawals |
| `/api/retailer/*` | body | Auth + Retailer | Retailer stats |
| `/api/strategy/*`, `POST /api/telemetry/batch` | body | Auth + DeviceClass + Device | Rules, telemetry |
| `/api/router/*` | body | Auth + DeviceClass + Router | Router quota |
| `POST /api/telemetry/rule_miss`, `/api/stats/*`, `/api/px*`, `POST /api/pair/beacon` | body | **None** | Rule-miss, stats ingest, funnel pixel, pairing beacon (`/pair/discover`: Auth) |
| `/api/push/*`, `/api/survey/*` | body | Auth | Push tokens, survey |
| `/api/chat/*` | body | None（登录用户自动识别；`/ws` 品牌取自令牌） | 访客客服聊天 — see Chat |
| `walletSensitive` `POST /api/wallet/withdraw-accounts` | **no bodies** | Auth + Wallet | 支付宝账号 + 真实姓名 |
| `socialSensitive` `POST /api/auth/social/{login,web-login}`, `POST /api/user/social/bind` | **no bodies** | None / Auth + DeviceClass | idToken + rawNonce in, tokens out |
| `adminSensitive` `GET /app/wallet/withdraws` | **no bodies** | `AdminRequired()` | 响应含解密后的收款信息 |
| `/app/*` (group `admin`) | body | `AdminRequired()` — superadmin | Plans, users, wallets, refunds… |
| `/app/*` (group `opsAdmin`) | body | **`StaffAuthRequired()`** + per-route `RoleRequired` | Tunnels, nodes, EDM, device-logs, tickets, chat conversations (`RoleSupport`), enterprise, funnel stats… |
| `/app/approvals` list/detail/cancel · approve/reject | body | `StaffAuthRequired()` · `AdminRequired()` | Approvals |
| `/app/asynqmon` | body | `asynqmonAuthMiddleware()` (superadmin) | Asynq UI (HTML) |
| `/webhook/{wordgate,appstore,stripe,nextpay}` | body | None (signature in handler) | Payment callbacks (HTTP-status exception) |
| `/webhook/slack/events` | **none** | Slack signature, no `BrandResolver` | 客服在 Slack 会话频道回复访客 |
| `/slave/*` | body | `SlaveAuthRequired()` — **except `PUT /slave/nodes/:ipv4`** (registration, validates body `secretToken`) | Node management, status, usage, device traffic |
| `/csr/*`, `GET /cron/execute`, `/version`, `/healthy` | body | None | CSR, external cron trigger, health |

`TestRouteWiring_AppGroupsUseStaffAuth` fails if any `/app` group mounts `AuthRequired()`.

## Cloud Provider (cloudprovider/)

Unified `Provider` interface (`provider.go`), one `<provider>.go` per provider: `aliyun_swas` (domestic) · `alibaba_swas` (international) · `aws_lightsail` (the only one that keeps billing past quota — `worker_cloud_overage.go`) · `tencent_lighthouse` (**international**) / `qcloud_lighthouse` (**domestic**, both in `tencent_lighthouse.go`) · `bandwagon` (VEID/APIKey) · `ssh_standalone` (no cloud API).

## Workers (Asynq)

- Every handler/cron is wired in `worker_integration.go` `InitWorker()` (`asynq.Handle(TaskType…, handle…)` + `asynq.Cron`), including the approval-callback registry; domain registrars like `RegisterChatWorker()` are called from there. A `handle*` not wired there never runs — no error.
- `handler_edm.go` is helpers only despite the prefix. Asynqmon UI at `/app/asynqmon`.
- Non-obvious: `worker_subscription_reconcile.go` is the fallback when Apple/Stripe webhooks are lost; `worker_stats_retention.go` sweeps stat tables by `reported_at` (server-authoritative; `created_at` is client clock); `worker_cloud_overage.go` runs as the tail step of `syncAccount`.
- Full table: **`docs/api/workers.md`** — read when looking for which file owns a job or adding a worker.

## Approval Workflow (Maker-Checker)

Critical admin operations (EDM, campaigns, plans, withdrawals, hard delete, license key batches, order refunds, Stripe withdrawal) go through `SubmitApproval()`. Superadmin bypasses and executes synchronously; others create a pending record another admin approves.

- **Files**: `logic_approval.go` (service + action labels), `logic_approval_callbacks.go`, `api_admin_approval.go`. Callbacks are registered by `RegisterApprovalCallback(action, cb)` in `InitWorker()` — that list is the source of truth for actions.
- **Pattern**: validate → `approvalID, executed, err := SubmitApproval(c, action, params, summary)` → `!executed` ⇒ `PendingApproval(c, approvalID)`; superadmin path continues to `Success`. **Each callback re-validates preconditions** (state may have changed since submission).
- **Concurrency**: atomic `UPDATE WHERE status='pending'` + `RowsAffected` check prevents double-approve. Slack DM notifications are best-effort.

## Brand (双品牌拆分: kaitu / overleap)

Spec `docs/superpowers/specs/2026-07-14-brand-split-design.md`. `Brand` (`brand.go`) is a registry-backed enum (`brandRegistry`: hosts, CORS origins, base URL, support email, EDM sender, payment channels, wallet, device-account, social client IDs, chat AI store…), not a config flag. Details (EDM / mail sender mechanics, admin list coverage, node-visibility admin fields, history): **`docs/api/brand.md`** — read before touching brand gates, admin brand filters, node visibility or branded mail.

**跨层契约门（唯一事实源就是这一条）**：改任一层的品牌数据（`brandRegistry` 或 webapp / web 的品牌注册表）后必须重生成 `cd api && UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract ./...`，产物与代码一起提交。
- `contracts/api-contract.json` 由 `contract_export_test.go` 从 **Go 活值**导出（不是手写清单），锁住 api / webapp / web 三层品牌注册表的交集。
- **必须带 `-count=1`**：golden 在 api/ 模块外，go test 缓存不 recheck 模块外文件 → 手改 golden 迁就代码会拿到陈旧的 `ok (cached)`。
- golden **只读**：不带 env 变量绝不自动重写（自动重写 = CI 永远绿）。
- 契约文件**必须进 git**（gitignore 掉 = 本地绿、CI 瞎）。
- 跨层不变量是**宿主归属**（`host(各层 baseURL) ∈ api.Hosts[该品牌]`），不是字符串相等——api/webapp 用 `www.`、web 用裸域是合法漂移。

Hard rules:
- **Resolution** (`BrandResolver()`, first on `/api`, `/app`, webhooks): `Host` → `X-K2-Brand` → default `kaitu`; read via `ReqBrand(c)`. Legacy clients always resolve to kaitu. The header is spoofable **by design**: it only picks which public config a non-brand host gets; the authenticated surface compares the token's `users.brand`. Don't "harden" host-over-header.
- **`users.brand` is a birth attribute**, immutable. `AuthRequired` / `AuthOptional` / `StaffAuthRequired` enforce it (403003) — exemptions in the Middleware table.
- **User-facing queries filter with `ScopeBrand(b)` only.** Admin (`/app/*`) is the sole cross-brand view: explicit `?brand=` via `adminBrandScope(c, "brand")` / `adminUserBrandScope(c, "user_id")` (orders, devices inherit the user's brand). `/app/users?email=` returns **every** brand's account — never `First()`.
- **Admin creates take an explicit brand**: handlers use `BrandRequired(s)` (empty/invalid → `ErrorInvalidArgument`, never a silent kaitu default); `BrandForCreate` survives only in approval callbacks / logic for pre-switch approvals. Writes on an existing user take the **user's** brand. Plan payment IDs must match the brand's channels (`validatePlanPaymentIDs`).
- **Node visibility** has one entry: `(*SlaveNode) VisibleTo(b)` = node self-declares the brand (`K2_NODE_BRANDS`) ∧ ops kill switch not set (`VisibleKaitu`/`VisibleOverleap`, default true). Enforced in `api_tunnel.go`, `api_tunnel_v20260717.go`, `api_subs.go`, `api_relay.go`, `api_antiblock.go`. Flipping the admin switch alone cannot put a node on Overleap.
- **Payment channel gate**: `Brand.Config().AllowsPayment(channel)` → 405001; brand-mismatch sentinels in the wordgate webhook / `creditAppleTransaction` / `creditStripeInvoice` (`alertPaymentBrandMismatch`) alert and refuse to credit — fail-loud by design, don't silence.
- **viper legacy keys** (`frontend_config.*`, `support.email` …) are kaitu-only; overleap values come from `BrandConfig`, never viper.
- **Mail**: `emailToUser` takes a `brandedEmailTemplate[T]` (recipient's brand drives copy **and** From); `emailToUserSingleBrand` is the escape hatch and callers must state why the other brand can't reach it. `FindOrCreateUserByEmail(ctx, brand, email, …)` takes the brand explicitly — never infer it from ctx. EDM never creates users; EDM template slugs are unique per `(slug, brand)`.
- **Verification-code cache key is brand-scoped** (`VerificationCodePrefix`) — a deploy that changes the segment invalidates in-flight codes.

## Payments — 违反了就出钱的事故

Full mechanics (NextPay config / 灰度 / 耐久链接, Stripe Checkout / 撤回执行器 / 结账同意 / 多币种, Apple bundle split, Apple 退款算法 / 对账, open overleap seams): **`docs/api/payments.md`** — read before touching any order / credit / refund / revoke / reconcile path.

- **One credit point per channel**: NextPay `/webhook/nextpay` `order.paid` → `MarkOrderAsPaid` (key `ObjectID == orders.uuid`); Stripe `creditStripeInvoice` on `invoice.paid` only (idempotent on `SubscriptionCredit(provider, transaction_id)` + `stripe_webhook_events`); Apple `creditAppleTransaction`. Amount/currency/brand mismatch is fail-loud (Slack + 5xx so the provider retries) — never ack-and-ignore. Credit plan lookup `planByPIDForCredit` ignores `is_active` (下架不停续费入账).
- **Post-commit side effects go through `OrderPostCommit`** (`logic_order.go`): `applyOrderToBuyer` / `applyRouterOrder` only accumulate provisioning / renewal IDs inside the tx; jobs and notifications fire after commit — a later rollback must not leave an enqueued job behind.
- **Apple sandbox transactions grant entitlement but never create an `Order`** (`PayAmount` feeds cashback / revenue / wallet refunds). Incident: `docs/incidents/2026-08-10-apple-iap-sandbox-order.md`.
- **`channel=apple_iap` orders never take the admin wallet refund** (`ProcessOrderRefund`, `api_admin_refund_order`, approval callback → `appleIAPWalletRefundMsg`): only Apple refunds Apple money; wallet + Apple = double refund. Legacy cases are caught by `alreadyWalletRefunded` (`[DOUBLE-REFUND]`).
- **Deduction uses `Subscription.PaidThrough`**, never `CurrentPeriodEnd` (reconcile pushes it to the unpaid next period → you'd cut gifted time). Stripe and Apple share this rule; PaidThrough 0 falls back to `CurrentPeriodEnd` (fail-closed).
- **Stripe `revoked` is terminal — four gates**: `creditStripeInvoice` adds no time, reconcile cover-through skips it under lock, `subscription.updated` writes carry `status<>revoked`, `subscription.deleted` short-circuits. Any new path that changes a Stripe subscription or user expiry needs the same check. Apple: `applyRenewalInfo` / `setSubStatus` SQL carry `status<>'revoked'`; a refunded transaction that was never credited is never credited (`errAppleTxnRevoked`).
- **The Stripe account is shared with other businesses** (NextPay / WordGate): `revokeStripeSubscription` only touches subscriptions that are `ours` — never cancel a non-ours one; account-level settings affect the others too. Don't partial-refund by hand in the Stripe dashboard (membership is not revoked) — use the 14-day withdrawal executor.
- **Wallet is brand-gated** (`BrandConfig.Wallet`, overleap false): `WalletRequired()` on routes, plus `ProcessOrderRefund` / `api_admin_refund_order` / `addCashbackIncomeInTx` refuse by brand.
- **Apple bundle id per brand** (`appleBundleIDForBrand`): empty = verify refuses; never fall back to kaitu's bundle. `planByAppleProductID` is `ScopeBrand`-filtered.

## Dedicated Line / Router Edition / Router Control Key

Full detail (fulfillment stages, online signal, credential mint flow, order-time gates and their history, DTOs, endpoints, control-key mint semantics): **`docs/api/router-edition.md`** — read before touching private lines, router orders, fulfillments, gateway credentials or `RouterControlKey`.

- **`PrivateNodeSubscription` is an entitlement** (tier / `TrafficTotalBytes` / own `ExpiresAt`), not a node topology. Multi-node = multiple `k2v5://` tunnels in `/api/subs` (`ResolveGatewayPrivateTunnels`) — no schema change needed. `SlaveNodeID` is the per-line metering anchor; `isNodeOverQuota` drops the line. Router admission = `HasActivePrivateLines`, decoupled from App tier.
- **One router per account**; "is this a router line" = the line has a `RouterFulfillment` ledger row (`routerLineIDs`). A 定制线路 (`pn-*`, no ledger row) is **not** a router line — tests that seed a router line must seed its ledger row.
- **Renewal (`router-svc-1y`) never provisions and never creates a fulfillment** — it extends (`extendPrivateLine`) and re-syncs; service plans are renewal-only (users without a router are rejected).
- `User.RouterControlKey` is plaintext by accepted risk; `/api/subs` gateway branch mints-on-serve, shared branch is read-only (a phone must never provision a router key).

## Social Sign-In (Google / Apple)

Endpoints, nonce scheme, matching order, conflict codes, `social_auth` yaml, deploy check: **`docs/api/social-login.md`**.

- **`aud` is the second brand lock** (first is `users.brand`): exactly one `aud`, in `BrandConfig.Social.Audiences(provider)`. Client IDs are in the cross-layer contract — regenerate it (Brand section) after changing them. All identity lookups scope to `ReqBrand(c)`.
- **Untrusted / missing email (Google unverified, Apple no email) never matches by email and never creates an account** (400015).
- **Any new user-deletion path must call `loadAppleRevocations` before deleting `LoginIdentify` rows and `revokeAppleTokensAsync` after commit** (App Review requirement; forgetting it errors nowhere — Apple authorization lingers forever). Existing points: unbind, self-delete, admin hard delete.
- Network calls (JWKS, Apple token/revoke) are package-level `var` funcs swapped in tests — tests never hit the network.

## Funnel (转化漏斗 / 留存)

Registries, lanes/hops, retention math, inferred attribution: **`docs/api/funnel.md`**.
- **Facts (signup / purchase / renewal / refund) never go into `funnel_events`** — projected at query time by `logic_funnel_facts.go`; client-reported fact names are dropped.
- Add events / paths only via `funnelEventRegistry` (`logic_funnel_events.go`) / `funnelPathRegistry` (`logic_funnel_paths.go`); collection never blocks the product path.
- **Raw IP never reaches any store** (inferred attribution keeps a daily-salted HMAC in Redis only); inferred links never write `funnel_identities`.

## Chat (访客客服聊天 + AI)

Spec `docs/superpowers/specs/2026-10-01-support-chat-design.md`. Entry: `api_chat.go` (`/api/chat/*`), `api_chat_slack_events.go`, `api_admin_chat.go`, `logic_chat_*.go`, `model_chat.go` (`Guest`, `GuestIdentity`, …), `worker_chat.go`.
- Gates: viper `chat.enabled` + `chat.preview_enabled` (both false = hard off, even for existing cookies) and `chat.brands` allow-list (empty = all brands off).
- Slack is the agent's primary UI: one private channel per conversation, every message mirrored in id order; `slack_mirrored_at` is an outbox marker backfilled by the `chat:slack-sweep` cron. Slack failures only log — they never fail `appendMessage`.
- AI replies via `qtoolkit/openai/filesearch` against the brand's `BrandConfig.ChatAIStore` (KB sources in `docs/customer-service/<brand>/`, synced by `scripts/sync-kb.sh`); errors / empty / cap (`chatAIMaxReplies`) hand off to a human.

## Device Account (零身份设备账号)

Spec `docs/superpowers/specs/2026-10-20-ios-device-account-design.md`. `POST /api/auth/device-account`, `/device-account/restore` (`api_device_account.go`, `logic_device_account.go`). Gated by `BrandConfig.DeviceAccount` (kaitu only → else 403004); router (gateway) clients are refused.
- "零身份" is decided **only** by `userHasIdentity` (no `login_identifies` row) — don't hand-count elsewhere.
- **A udid is not a secret**: an existing device row never gets a token by udid — 403005 (owner has an identity → log in) / 403006 (device account → Keychain token or Apple-transaction restore).

## Enterprise (企业路由器多 SSID)

Spec `docs/superpowers/specs/2026-07-22-enterprise-router-multi-ssid-design.md`. Admin `/app/enterprise/*` (`api_admin_enterprise.go`, opsAdmin: read `RoleDevopsViewer|Editor`, write `RoleDevopsEditor`); models `EnterpriseCustomer` / `EnterpriseLine` / `EnterpriseRouterBinding` (`model.go`). `/api/subs` adds `slot_bindings` via `resolveSlotBindings` (`logic_enterprise.go`) — snake_case wire contract with k2 `config.SlotBinding`, `tunnel_index` indexes the same response's `tunnels`. **Deliberately health-blind**: quota cutoff / node death never remove a slot; only a human rebind does.

## Campaign Matcher Semantics (single source of truth)

Campaign `matcherType` gates who may redeem a code (`logic_campaign.go getCampaignMatcherWithDB`). The names are **audience labels, not order-state checks**:

| matcherType | matches | use for |
|-------------|---------|---------|
| `first_order` | 新客 — `!IsFirstOrderDone` (nil = new) | 首单优惠、弃单召回（只发新客） |
| `vip` | 老客 — `IsFirstOrderDone == true` (= `IsVip()`) | 续费 / 召回老客 |
| `all` | anyone | 通用码 |
| `paid_before` | first paid before `matcherParams.beforeDate` | 时间窗定向 |
| `paid_before_active` | `paid_before` AND membership still active | 时间窗定向且在期 |

`first_order` and `vip` are exact mirrors and must never collapse into the same meaning — `logic_campaign_matcher_test.go` pins both. **History (do not repeat):** `first_order` once meant "已付费" while every author read it as "new customer" — all 5 `first_order` campaigns (FIRST_ORDER_20, READY4U, STAYFREE, SMOOTHDAY, KEEPGOING) silently rejected 100% of recipients with `ErrorInvalidCampaignCode` (fixed 2026-06-06). Keep new matcher names describing the **audience**, and mirror the admin UI label in `web/.../manager/campaigns/page.tsx`.

**`LicenseKeyBatch` is a different thing — don't fold it into campaigns**: 授权码批次自带渠道标签 (`sourceTag`)、兑换条件 (`recipientMatcher`)、过期时间，统计兑换率与兑换→付费转化；创建走审批。

## Tunnel Scoring

- **Model = time-gated usage-sensitivity**: `score = 1 − trafficRatio · w(timeRatio)`, `w(t) = 0.15 + 0.85·t²` — generous early in the cycle, strict late. **Not an exhaustion check**: exhaustion is the hard cutoff / hide path (`isNodeOverQuota`), never score 0. Replaced an earlier `trafficRatio − timeRatio` pacing model plus warmup/headroom terms; don't reintroduce those.
- **Single authority**: `ComputeRecommendScore(inst *DataTunnelInstance) float64` (`logic_tunnel_score.go`); `/api/tunnels` and `/api/subs` both call it — never inline a formula.
- **List vs subs**: `/api/tunnels` returns `listRecommendScore` (healthy projected into `[0.6, 1]`, exhausted 0.45, so the client shows yellow only when a node is out of quota); `/api/subs` keeps the raw score, because the daemon splits traffic by its proportions.
- **Nil instance = 0.5** (non-cloud nodes neutral; 0 would blacklist them from `pickWeighted` / `Subscription.Pick`).
- **Dual-emit**: `/api/subs` emits `recommendScore` and legacy `weight = round(score*100)` for pre-e210564 daemons; drop `weight` one release after rollout is confirmed.
- **No Redis penalty layer** (`subsPenalty*` / `applyPenaltyWeights` removed in `9e12d0b`) — no request-side rate-limiting in the subscription response; compute a score server-side instead.

## Logging Convention

`log.Errorf/Warnf/Debugf(c, ...)` from `qtoolkit/log`, always passing `c` first (request-scoped tracing). No `[ERROR]`-style prefixes (the level carries it). No `Tracef` for app debug — Trace is for wire dumps.

## Test Convention

| Tier | DB | Config | Guard |
|------|-----|--------|-------|
| Unit | None | None | None |
| Mock DB | `SetupMockDB(t)` (`mock_db_test.go`: `SkipInitializeWithVersion`, `QueryMatcherRegexp`) — always use it | `testInitConfig()` — called by `SetupTestRouter()` / `skipIfNoConfig()`, **not** by `SetupMockDB` | None |
| Integration | Real MySQL | `../center/config.yml` (repo-root `center/`) | `skipIfNoConfig(t)` at the top of each test |

- **Silent skip is the default — a green `go test ./...` proves nothing about the DB half.** `skipIfNoConfig` looks for `../center/config.yml` while `start`/`migrate` read `api/config.yml`; both gitignored (in the main checkout `center` is a local symlink to `api`, so they are the same file there; a fresh worktree has neither). `.github/ci/center-config.yml` is the only in-repo template. The CI mock job silently skipped **256 of 1085** tests at 0.4.8 and reported green; the `test-api-db` job (`scripts/ci/api-db-test.sh`: throwaway MariaDB + real `migrate`) **fails on any config-skip**. Locally the discriminator is `-v` showing 0 `config.yml not available` skips.
- **Restore viper keys to their original value, never to a literal**: one shared viper; `center/config.yml` sets `mail.dev_mode: true`. `defer viper.Set("mail.dev_mode", false)` makes every later worker test really send — capture `viper.Get(key)` and restore in `t.Cleanup` (`restoreViper` in `logic_email_sender_test.go`).
- **Tests never touch real Redis**: `testInitConfig()` (`testutil_test.go`) starts miniredis, overrides `redis.*` before and after the config load, sets `EnableMockVerificationCode = true`. `testInitOnce` is a plain bool, not `sync.Once` — unsafe under `t.Parallel`. Never panic on missing config (`testConfigAvailable = false`).
- **New GORM columns need a manual migrate before integration tests see them**: the long-lived test DB is migrated out-of-band (`testInitConfig`/`skipIfNoConfig` never `AutoMigrate`). After adding a field to a model in `migrate.go`'s list, run `cd api/cmd && go run . migrate --config ../../center/config.yml` once, or integration tests fail with `Unknown column`. Production runs `center.Migrate()` on start.
- **Avoid zero-value assertions**: `assert.Equal(t, 0, resp.Code)` passes trivially on unmarshal failure — verify the positive case.
- **File naming**: `api_*_test.go` handler tests, `db_mock_test.go` shared mock utilities, `mock_db_test.go` MockDB struct, `testutil_test.go` `testInitConfig` / `skipIfNoConfig` / `SetupTestRouter`.

## UAT (NextPay + Router Edition, real services)

`scripts/uat/nextpay-router-env.sh up` **rewrites `center/config.yml` in place** (backup `center/config.yml.pre-uat.bak`) — **always finish with `down`**; `pkill` leaves the UAT config there permanently (it is gitignored, git can't restore it). Full procedure, egress lockdown, why the live test avoids viper: **`docs/api/uat.md`**.

## Related Docs

- [Client Architecture](../CLAUDE.md) · [Webapp Frontend](../webapp/CLAUDE.md)
- `docs/api/`: [brand](../docs/api/brand.md) · [payments](../docs/api/payments.md) · [router-edition](../docs/api/router-edition.md) · [social-login](../docs/api/social-login.md) · [funnel](../docs/api/funnel.md) · [workers](../docs/api/workers.md) · [uat](../docs/api/uat.md)
