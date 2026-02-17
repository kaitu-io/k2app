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
make build-center                                          # Build for deploy
make deploy-center                                         # Deploy
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
| `handler_*.go` | Asynq task handlers | `handler_edm.go`, `handler_ssh_terminal.go` |
| `worker_*.go` | Background workers + cron | `worker_cloud.go`, `worker_ech.go`, `worker_batch.go` |
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
| 401 | `ErrorUnauthorized` | Invalid/expired token |
| 402 | `ErrorPaymentRequired` | Membership expired |
| 403 | `ErrorForbidden` | Insufficient permissions |
| 405 | `ErrorNotSupported` | Not supported |
| 406 | `ErrorUpgradeRequired` | Client upgrade required |
| 409 | `ErrorConflict` | Resource conflict |
| 422 | `ErrorInvalidParams` | Bad request payload |
| 425 | `ErrorTooEarly` | Too early (rate limit) |
| 500 | `ErrorInternal` | System exception |
| 503 | `ErrorServiceUnavailable` | Service unavailable |

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
| `/api/app/config` | None | Frontend app config |
| `/api/ech/config` | None | ECH config |
| `/api/ca` | None | CA certificate |
| `/app/*` | Admin | All admin endpoints |
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
| `worker_batch.go` | Batch script execution on nodes |
| `worker_diagnosis.go` | Route diagnosis aggregation |
| `worker_renewal_reminder.go` | Membership renewal reminders |
| `worker_retailer_followup.go` | Retailer follow-up notifications |
| `worker_integration.go` | `InitWorker()` — registers all handlers + cron schedules |

Asynqmon UI available at `/app/asynqmon` (admin auth required).

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

## Related Docs

- [Client Architecture](../CLAUDE.md)
- [Webapp Frontend](../webapp/CLAUDE.md)
