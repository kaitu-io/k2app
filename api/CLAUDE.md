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
waymaker/            Legacy k2oc protocol support (embedded CA certs)
templates/           Embedded templates (docker-compose, init-node.sh)
```

### Flat Package Pattern

All handlers, logic, and models live in the root `center` package. No internal subdirectories for domain entities. Convention is enforced by file naming, not directory structure.

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

## Local Development

```bash
# Start dependencies
cd api && docker-compose up -d      # MariaDB 10.6 (port 53721) + Redis (port 49183)

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
