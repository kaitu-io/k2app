# Feature: k2v5 Tunnel Expression

## Meta

| Field | Value |
|-------|-------|
| Feature | k2v5-tunnel-expression |
| Version | v2 |
| Status | implemented |
| Created | 2026-02-20 |
| Updated | 2026-02-20 |

## Overview

让 k2v5 tunnel 在整条链路上表达完整：k2s 启动时生成的 cert pin 和 ECH config 通过 sidecar 注册到 Center，API 返回 `serverUrl` 字段，客户端只需插入 auth 凭据即可连接。

核心原则：**ECH 放权给 k2s**。k2s 已有完整的 ECH key 生成和轮换逻辑（`setupECHRotation()`），Center 不再管理 k2v5 节点的 ECH 密钥。Center 的 ECHKey 表 + worker_ech.go 仅保留服务 k2v4-slave 旧节点。

## Product Requirements

### 数据流

```
k2s 启动
  ├─ 自签证书 → cert pin（稳定 10 年）
  ├─ 生成 ECH keys → ECHConfigList（k2s 自管轮换）
  └─ 写 {certDir}/connect-url.txt: k2v5://host:port?ech=xxx&pin=sha256:xxx

Sidecar 读 connect-url.txt
  ├─ BuildServerURL() 构建完整 serverUrl（使用配置的 domain/port + hop range）
  └─ PUT /slave/nodes/:ipv4 时上报 serverUrl

Center 存储
  └─ SlaveTunnel.ServerURL 字段（完整连接 URL）

GET /api/tunnels/k2v5
  └─ 直接返回存储的 serverUrl
     客户端拼入 udid:token@ 即连接
```

### API 响应变化

**GET /api/tunnels/k2v5** 返回：

```json
{
  "items": [
    {
      "serverUrl": "k2v5://hk1.example.com:443?ech=base64url...&pin=sha256:xxx",
      "node": {
        "country": "HK",
        "region": "hong-kong",
        "load": 35
      }
    }
  ]
}
```

- `serverUrl` 由 sidecar 构建后直接存储在 DB，API 直接返回（不再实时拼接）
- 不含 auth 凭据（udid:token 由客户端注入）
- hop port 信息：若有 hop range，包含在 URL 中 `&hop=10020-10119`

**GET /api/tunnels/k2v4** 保持不变：返回结构化字段 + response 级 `echConfigList`（Center 管理的 ECH，向后兼容）。k2v5 tunnel 仍出现在 k2v4 列表中（front-door 转发兼容），但以 k2v4 格式返回（无 serverUrl）。

## Technical Decisions

### 1. SlaveTunnel ServerURL 字段

```go
// SlaveTunnel model
ServerURL string `gorm:"column:server_url;type:text"` // Complete k2v5 connection URL
```

- Sidecar 从 connect-url.txt 构建完整 URL 后直接上报，Center 直接存储
- 比分解为 CertPin + ECHConfigList 再重组更简单（无分解-存储-重组开销）
- 仅 k2v5 tunnel 使用，k2v4/k2wss/k2oc tunnel 为空

### 2. Sidecar BuildServerURL

Sidecar 读取 connect-url.txt 后调用 `BuildServerURL()` 构建完整 URL：
1. 解析 connect-url.txt 提取 `ech` 和 `pin` query params
2. 使用配置的 domain/port（非原始 URL 中的 host:port）
3. 追加 hop range（如有配置）
4. 剥离 auth 凭据和 dev flags（如 `insecure=1`）

```go
func BuildServerURL(connectURLContent, domain string, port, hopStart, hopEnd int) string
// Output: k2v5://domain:port?ech=xxx&pin=sha256:xxx[&hop=start-end]
```

### 3. ECH 所有权分离

| 节点类型 | ECH 管理者 | 分发路径 |
|---------|-----------|---------|
| k2v5 节点 | k2s 自管（setupECHRotation） | k2s → connect-url.txt → sidecar BuildServerURL → Center DB → API |
| k2v4-slave 旧节点 | Center（ECHKey 表 + worker_ech.go） | Center → sidecar FetchECHKeys → ech_keys.yaml → k2v4-slave |

不删除 Center 的 ECH 系统，两套并存：
- k2v5 tunnel 的 ECH 嵌入在 ServerURL 中（k2s 上报）
- k2v4 tunnel 响应中的 ECH 来自 Center ECHKey 表（旧流程不变）

### 4. Sidecar FetchECHKeys 控制

k2v5 部署中，sidecar 仍可调用 FetchECHKeys——但仅在配置了 k2v4-slave 时才需要。如果 `ech.enabled=false`（k2v5 自管），sidecar 跳过 FetchECHKeys。这是现有逻辑，无需改动。

## Key Files

| 文件 | 变更 |
|------|------|
| `api/model.go` | SlaveTunnel: ServerURL 字段（替代 CertPin + ECHConfigList） |
| `api/slave_api_node.go` | TunnelConfigInput: serverUrl 字段 |
| `api/api_tunnel.go` | 直接返回 tunnel.ServerURL（删除 buildK2V5ServerURL） |
| `docker/sidecar/sidecar/connect_url.go` | BuildServerURL() 构建完整连接 URL |
| `docker/sidecar/sidecar/node.go` | TunnelConfig: ServerURL 字段 |
| `docker/sidecar/main.go` | 调用 BuildServerURL 并上报 |

## Acceptance Criteria

- [x] AC1: SlaveTunnel model 有 server_url 字段，DB migration 正常
- [x] AC2: Sidecar 读取 k2s 的 connect-url.txt，BuildServerURL 构建完整连接 URL
- [x] AC3: Sidecar PUT /slave/nodes/:ipv4 注册时上报 serverUrl
- [x] AC4: Center 存储 k2v5 tunnel 的 server_url
- [x] AC5: GET /api/tunnels/k2v5 返回每个 tunnel 的 serverUrl
- [x] AC6: serverUrl 格式正确：k2v5://domain:port?ech=xxx&pin=xxx[&hop=start-end]
- [x] AC7: GET /api/tunnels/k2v4 保持不变（k2v5 tunnel 以 k2v4 格式出现，无 serverUrl）
- [x] AC8: 旧节点（无 server_url）不影响现有功能

## Version History

| Version | Date | Changes |
|---------|------|---------|
| v1 | 2026-02-20 | Initial spec: cert pin + ECH config registration, serverUrl response |
| v2 | 2026-02-20 | Simplify: replace CertPin + ECHConfigList with single ServerURL field |
