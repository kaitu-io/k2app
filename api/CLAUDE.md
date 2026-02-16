# Center Service

Central API service responsible for authentication, user management, and business logic.

## AI Behavior Rules

### Hard Rules

```
Prohibited:
  Swagger/Swag annotations
  GORM raw SQL string queries (use struct queries only)
  Modifying API without reading Contract first

Required:
  Sync Contract and frontend when modifying API
  Follow file naming: api_*.go, logic_*.go, model.go
  Use predefined error code constants
```

### API Modification Checklist

- [ ] Read `docs/api-contracts/contracts/{api}.yaml`
- [ ] Updated Usage Map
- [ ] Synced frontend callsites (`web/src/lib/api.ts`, `client/desktop-tauri/src/services/api.ts`, `client/service/daemon/ipc/`)
- [ ] Added tests

## Tech Stack

**Go** + Gin | **GORM** ORM | **MySQL**

## File Layout

| Pattern | Purpose | Example |
|---------|---------|---------|
| `api_*.go` | HTTP handlers | `api_auth.go`, `api_user.go` |
| `api_admin_*.go` | Admin API | `api_admin_orders.go` |
| `api_app_*.go` | App API | `api_app_task.go` |
| `logic_*.go` | Business logic | `logic_auth.go`, `logic_task.go` |
| `handler_*.go` | Task handlers | `handler_edm.go` |
| `model.go` | Data models | - |
| `type.go` | Type definitions | - |
| `slave_api.go` | Slave node API | - |
| `route.go` | Route configuration | - |

## API Response Format

```go
Success(c, &user)                    // Single object
ListWithData(c, items, pagination)   // List with pagination
Error(c, ErrorCode, "message")       // Error
```

### Error Codes

| Code | Meaning | Scenario |
|------|---------|----------|
| 401 | Unauthenticated | Invalid/expired token |
| 402 | Membership expired | Renewal required |
| 403 | Forbidden | Insufficient permissions |
| 422 | Invalid parameters | Bad request payload |
| 500 | Internal error | System exception |

## Task System

### Model

```go
type Task struct {
    ID            uint64
    Name          string
    Action        string      // "edm.send", "my.action"
    Type          TaskType    // "once" or "repeat"
    IsActive      bool
    NextExecuteAt time.Time
    RepeatEvery   *int64      // Repeat interval in seconds
    Payload       string      // JSON parameters
}
```

### Handler Registration

```go
var taskHandlers = map[string]TaskHandlerFunc{
    "edm.send":  handleEDMSend,
    "my.action": handleMyAction,
}

type TaskHandlerFunc func(ctx context.Context, payload string) (output string, err error)
```

## Slave API

```bash
# Node registration
PUT    /slave/nodes/{uuid}                    # Register node
PUT    /slave/nodes/{uuid}/tunnels/{domain}   # Add tunnel
DELETE /slave/nodes/{uuid}/tunnels/{domain}   # Remove tunnel
POST   /slave/report_status                   # Report status

# Client API
GET    /api/tunnels?protocol=k2wss            # Get tunnels (JWT + Pro)

# Admin API
GET    /app/tunnels        # List tunnels
PUT    /app/tunnels/:id    # Update tunnel
DELETE /app/tunnels/:id    # Delete tunnel
GET    /app/nodes          # List nodes
DELETE /app/nodes/:ipv4    # Delete node (cascading)
```

## Build & Deploy

```bash
make build-center    # Build
make deploy-center   # Deploy
```

## Related Docs

- [API Contract Framework](../../docs/api-contracts/API-CONTRACT-FRAMEWORK.md)
- [Slave Service](../slave/CLAUDE.md)
