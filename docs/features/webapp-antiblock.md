# Feature: Webapp Antiblock Integration

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | webapp-antiblock                         |
| Version   | v3                                       |
| Status    | draft                                    |
| Created   | 2026-02-17                               |
| Updated   | 2026-02-17                               |
| Tests     | Crypto + JSONP + resolveEntry + cloudApi + CORS |

## Version History

| Version | Date       | Summary                                            |
|---------|------------|-----------------------------------------------------|
| v1      | 2026-02-17 | Initial: webapp-only, CORS as external prereq       |
| v2      | 2026-02-17 | Add server-side CORS `*` for `/api` group           |
| v3      | 2026-02-17 | 收敛 CORS: 仅放行局域网 origin，保护 cookie 安全    |

## Overview

将 webapp2 中已实现的 antiblock（AES-256-GCM 加密入口解析）迁移到 webapp 的 `services/` 架构中。所有平台统一使用 `resolveEntry()` 解析绝对入口 URL。

**当前状态**:
- webapp 的 `cloudApi.request()` 使用相对路径 `fetch(path)` —— 没有 antiblock
- Cloud API 的 `/api/*` 路由组没有 CORS 中间件（只有 `/app/*` admin 组有）

**目标状态**:
- `cloudApi.request()` 通过 `resolveEntry()` 解析绝对入口 URL
- Cloud API `/api/*` 路由组对局域网 origin 放行 CORS

## 跨域策略

### 为什么不能用 `Access-Control-Allow-Origin: *`

`/api/auth/web-login`、`/api/auth/refresh`、`/api/auth/logout` 以及 sliding expiration 中间件都会 `Set-Cookie`（HttpOnly access_token + CSRF token）。如果用 `*`：
- 浏览器忽略 `Set-Cookie`（规范强制：`*` 不允许 credentials）
- web-login cookie 丢失

### 方案：仅放行局域网 origin

动态检测请求 Origin，仅对本地/局域网地址回显 `Access-Control-Allow-Origin: <origin>` + `Access-Control-Allow-Credentials: true`。

**匹配规则**:
- `localhost`、`127.0.0.1`（任意端口、http/https）
- `capacitor://localhost`
- RFC 1918 私有 IP：`10.x.x.x`、`172.16-31.x.x`、`192.168.x.x`

| 平台 | Origin | 匹配？ |
|------|--------|--------|
| Tauri Desktop | `http://localhost:14580` | localhost ✓ |
| Capacitor iOS | `capacitor://localhost` | capacitor ✓ |
| Capacitor Android | `https://localhost` | localhost ✓ |
| Web Dev | `http://localhost:1420` | localhost ✓ |
| OpenWrt 路由器 | `http://192.168.1.1` | RFC 1918 ✓ |
| 公网攻击者 | `https://evil.com` | ✗ 拒绝 |

**安全性**：
- 仅同网段设备能发起跨域请求（攻击者需在用户局域网内）
- Cookie 的 `SameSite=Lax` + CSRF token 提供额外防护
- 现有 `/app/*` admin CORS 保持不变（白名单模式）

## 变更范围

### 服务端: Cloud API CORS

#### 1. 新增 `/api` CORS 中间件 (`api/middleware.go`)

```go
// ApiCORSMiddleware handles CORS for /api/* client routes.
// Only allows local/LAN origins (localhost, 127.0.0.1, RFC 1918 private IPs,
// capacitor://localhost). Echoes back the specific origin with credentials support.
func ApiCORSMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        origin := c.GetHeader("Origin")
        if origin != "" && isPrivateOrigin(origin) {
            c.Header("Access-Control-Allow-Origin", origin)
            c.Header("Access-Control-Allow-Credentials", "true")
            c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
            c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token")
            c.Header("Access-Control-Max-Age", "86400")
        }

        if c.Request.Method == "OPTIONS" {
            c.AbortWithStatus(204)
            return
        }
        c.Next()
    }
}

// isPrivateOrigin checks if the origin is from localhost or RFC 1918 private network.
func isPrivateOrigin(origin string) bool {
    // capacitor://localhost (iOS)
    // http(s)://localhost[:port]
    // http(s)://127.0.0.1[:port]
    // http(s)://10.x.x.x[:port]
    // http(s)://172.(16-31).x.x[:port]
    // http(s)://192.168.x.x[:port]
}
```

#### 2. 挂载到 `/api` 路由组 (`api/route.go`)

```go
api := r.Group("/api")
api.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), ApiCORSMiddleware())
```

### 客户端: Webapp Antiblock

#### 3. 新增 `webapp/src/services/antiblock.ts`

从 `webapp2/src/api/antiblock.ts` 直接复制，无需修改。

#### 4. 修改 `webapp/src/services/cloud-api.ts`

```typescript
import { resolveEntry } from './antiblock';

// 主请求
const entry = await resolveEntry();
const httpResponse = await fetch(`${entry}${path}`, fetchOptions);

// 401 refresh
const refreshEntry = await resolveEntry();
const refreshResponse = await fetch(`${refreshEntry}/api/auth/refresh`, { ... });

// retry
const retryResponse = await fetch(`${entry}${path}`, fetchOptions);
```

#### 5. 新增 `webapp/src/services/__tests__/antiblock.test.ts`

从 `webapp2/src/api/__tests__/antiblock.test.ts` 复制并调整 import 路径。

#### 6. 更新 `webapp/src/services/index.ts`

```typescript
export { resolveEntry, DEFAULT_ENTRY } from './antiblock';
```

## Acceptance Criteria

- **AC1**: `cloudApi.request()` 使用 `resolveEntry()` 构建绝对 URL
- **AC2**: 401 refresh 和 retry 同样使用 `resolveEntry()`
- **AC3**: antiblock 14 个测试全部通过
- **AC4**: 现有 cloud-api 和 k2api 测试不受影响
- **AC5**: `yarn build` 构建成功，无 TypeScript 错误
- **AC6**: `/api/*` 对 localhost origin 返回 CORS 头 + credentials
- **AC7**: `/api/*` 对 RFC 1918 私有 IP origin 返回 CORS 头 + credentials
- **AC8**: `/api/*` 对 `capacitor://localhost` 返回 CORS 头
- **AC9**: `/api/*` 对公网 origin（如 `https://evil.com`）不返回 CORS 头
- **AC10**: `/api/*` OPTIONS 预检返回 204
- **AC11**: 现有 `/app/*` admin CORS 行为不变
