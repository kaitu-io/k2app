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
| `/slave/*` | Slave | Node management, status reporting |
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
| `worker_integration.go` | `InitWorker()` — registers all handlers + cron schedules |

Asynqmon UI available at `/app/asynqmon` (admin auth required).

## Approval Workflow (Maker-Checker)

Critical admin operations (EDM, campaigns, plans, withdrawals, hard delete, license key batches) require dual approval via `SubmitApproval()`. Superadmin (`is_admin`) bypasses approval and executes synchronously. Non-superadmin creates a pending record requiring another admin's approval.

- **Core files**: `logic_approval.go` (service), `logic_approval_callbacks.go` (10 callbacks), `api_admin_approval.go` (handlers)
- **Pattern**: Handler validates → `SubmitApproval(c, action, params, summary)` → returns `(approvalID, status, error)` where status is `"executed"` (superadmin) or `"pending_approval"` (needs approval)
- **Callback registry**: `RegisterApprovalCallback(action, cb)` in `InitWorker()`. Each callback re-validates preconditions before executing.
- **Concurrency**: Atomic `UPDATE WHERE status='pending'` + `RowsAffected` check prevents double-approve
- **Notifications**: Slack DM via `qtoolkit/slack.SendDM(email, message)` — best-effort, never blocks main flow

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
