# Feature: Desktop DNS Hijack via Middleware

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | desktop-dns-hijack                       |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-23                               |
| Updated   | 2026-02-23                               |

## Version History

| Version | Date       | Summary                                    |
|---------|------------|--------------------------------------------|
| v1      | 2026-02-23 | Initial: unify DNS middleware for desktop TUN mode |

## Product Requirements

- 桌面 TUN 模式的 DNS 查询经过完整的 13 步 DNS middleware pipeline（cache、filter、k2rule 路由、direct/proxy upstream、SetTmpRule）(v1)
- 移动端和桌面端 TUN 模式共享同一套 DNS 处理逻辑，无代码分支 (v1)
- Proxy 模式不受影响（无 TUN 设备，无 port 53 拦截）(v1)

## Background

移动端路径（`FileDescriptor >= 0`）在 TUN provider 和 `ClientTunnel` 之间插入 `dnsHandler`，拦截所有 port 53 UDP 并路由到 DNS middleware pipeline。桌面端路径（`FileDescriptor < 0`）绕过了这一层——`tunnel.Start(ctx)` 直接将 tunnel 自身作为 `ConnectionHandler`，DNS 查询被当作普通 UDP 流量处理。

`dnsHandler` 和 `dns.Middleware` 本身不含任何移动端特有逻辑。sing-tun gVisor stack 在桌面端以完全相同的方式（`HandleUDP(conn, "8.8.8.8:53")`）交付 DNS 连接。唯一的障碍是 `tunnel.Start()` 硬编码了 `prov.Start(ctx, self)`，无法注入 wrapper handler。

## Technical Decisions

- **StartWithHandler 方法**: `ClientTunnel` 新增 `StartWithHandler(ctx, handler)` 方法，允许在 provider 和 tunnel 之间注入中间件。原 `Start(ctx)` 委托给 `StartWithHandler(ctx, t)`，行为不变 (v1)
- **分支条件**: engine start 分支从 `FileDescriptor >= 0`（仅移动端）改为 `Mode != "proxy"`（所有 TUN 模式），proxy 模式保持原行为 (v1)
- **DNS 服务器传递**: daemon 将 `cfg.DNS.Proxy` 和 `cfg.DNS.Direct` 传入 engine config，桌面端获得与 `config.SetDefaults()` 一致的 DNS 服务器列表 (v1)
- **Anti-loop**: 无需额外处理——`DNSExclude` 已在 `tun_desktop.go` 通过 `Inet4RouteExcludeAddress` 将直连 DNS IP 排除在 TUN 路由之外 (v1)

## Design

### sing-tun 数据流

```
Kernel TUN (utun0) → tun.New() + AutoRoute
  → tun.NewSystem(Handler: &handlerAdapter{handler: <ConnectionHandler>})
    → gVisor 将 IP 包重组为 L4 会话
      → handlerAdapter.NewPacketConnectionEx(ctx, conn, src, dst, onClose)
        → go handler.HandleUDP(singPacketConn{conn}, "8.8.8.8:53")
```

### Before / After

**Before** (desktop TUN):
```
Provider → ClientTunnel.HandleUDP → Router → wire.DialUDP (DNS 当普通 UDP)
```

**After** (desktop TUN):
```
Provider → dnsHandler.HandleUDP → port 53? → dns.Middleware.HandleQuery → cache/filter/k2rule/upstream
                                → non-53  → ClientTunnel.HandleUDP → Router → wire/direct
```

### 修改文件

| File | Change |
|------|--------|
| `core/tunnel.go` | 新增 `StartWithHandler(ctx, handler)`，`Start` 委托 |
| `engine/engine.go` | start 分支从 `FileDescriptor >= 0` 改为 `Mode != "proxy"` |
| `daemon/daemon.go` | 传递 `cfg.DNS.Proxy/Direct` 到 engine config |

## Acceptance Criteria

- AC1: 桌面 TUN 模式下，port 53 UDP 经 `dnsHandler` 拦截并由 `dns.Middleware.HandleQuery` 处理 (v1)
- AC2: Proxy 模式行为不变（`tunnel.Start(ctx)` 直接启动，无 DNS 拦截）(v1)
- AC3: 移动端行为不变（仍走 `dnsHandler` + middleware）(v1)
- AC4: `go build ./...` 编译通过 (v1)
- AC5: `go test ./core/... ./engine/... ./daemon/...` 全部通过 (v1)
- AC6: daemon 将 `cfg.DNS.Proxy` 和 `cfg.DNS.Direct` 正确传递到 engine config (v1)

## Testing Strategy

- 编译检查：`go build ./...` (v1)
- 现有单元测试覆盖：`go test ./core/... ./engine/... ./daemon/...` (v1)
- `dnsHandler` 已有独立测试（`engine/dns_handler_test.go`），验证 port 53 拦截逻辑——桌面端复用同一代码路径 (v1)

## Deployment & CI/CD

- 无新依赖，无配置格式变更 (v1)
- 现有 `go test ./...` CI 流水线覆盖 (v1)
