# Feature: k2v5 Tunnel Expression

## Meta

| Field | Value |
|-------|-------|
| Feature | k2v5-tunnel-expression |
| Version | v1 |
| Status | draft |
| Created | 2026-02-20 |
| Updated | 2026-02-20 |

## Overview

让 k2v5 tunnel 在整条链路上表达完整：k2s 启动时生成的 cert pin 和 ECH config 通过 sidecar 注册到 Center，API 返回计算好的 `serverUrl` 字段，客户端只需插入 auth 凭据即可连接。

核心原则：**ECH 放权给 k2s**。k2s 已有完整的 ECH key 生成和轮换逻辑（`setupECHRotation()`），Center 不再管理 k2v5 节点的 ECH 密钥。Center 的 ECHKey 表 + worker_ech.go 仅保留服务 k2v4-slave 旧节点。

## Product Requirements

### 数据流

```
k2s 启动
  ├─ 自签证书 → cert pin（稳定 10 年）
  ├─ 生成 ECH keys → ECHConfigList（k2s 自管轮换）
  └─ 写 {certDir}/connect-url.txt: k2v5://host:port?ech=xxx&pin=sha256:xxx

Sidecar 读 connect-url.txt
  ├─ 解析 certPin + echConfigList
  └─ PUT /slave/nodes/:ipv4 时一起上报

Center 存储
  └─ SlaveTunnel 新增 cert_pin + ech_config_list 字段

GET /api/tunnels/k2v5
  └─ 每个 tunnel 返回计算好的 serverUrl
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

- `serverUrl` 是 API 在响应时从 domain + port + cert_pin + ech_config_list 实时拼接
- 不含 auth 凭据（udid:token 由客户端注入）
- hop port 信息：若有 hop range，追加 `&hop=10020-10119`

**GET /api/tunnels/k2v4** 保持不变：返回结构化字段 + response 级 `echConfigList`（Center 管理的 ECH，向后兼容）。k2v5 tunnel 仍出现在 k2v4 列表中（front-door 转发兼容），但以 k2v4 格式返回（无 serverUrl）。

## Technical Decisions

### 1. SlaveTunnel 新增字段

```go
// SlaveTunnel model
CertPin       string `gorm:"type:varchar(128)"`  // k2v5 cert pin (e.g., "sha256:base64...")
ECHConfigList string `gorm:"type:text"`           // k2v5 ECH config list (base64url encoded)
```

- 这两个字段仅 k2v5 tunnel 使用，k2v4/k2wss/k2oc tunnel 为空
- CertPin 稳定不变（10 年自签证书），ECHConfigList 随 k2s 轮换更新
- 未来 Tunnel 精简（TODO #2）时这些字段保留

### 2. Sidecar 读取 connect-url.txt

k2s 已将完整 URL 写入 `{certDir}/connect-url.txt`（`k2/server/server.go:357`）。

Sidecar 流程：
1. 等待 k2s 启动完成（`.ready` flag 或 connect-url.txt 文件出现）
2. 读取 connect-url.txt
3. 解析 URL query params：`ech=` → ECHConfigList，`pin=` → CertPin
4. 在 `TunnelConfig` 中携带 `CertPin` + `ECHConfigList`
5. PUT /slave/nodes/:ipv4 注册时上报

### 3. serverUrl 拼接逻辑

API 在响应 k2v5 tunnel 时实时计算 `serverUrl`：

```go
func buildK2V5ServerURL(t *SlaveTunnel) string {
    u := fmt.Sprintf("k2v5://%s:%d", t.Domain, t.Port)
    params := []string{}
    if t.ECHConfigList != "" {
        params = append(params, "ech="+t.ECHConfigList)
    }
    if t.CertPin != "" {
        params = append(params, "pin="+t.CertPin)
    }
    if t.HopPortStart > 0 && t.HopPortEnd > 0 {
        params = append(params, fmt.Sprintf("hop=%d-%d", t.HopPortStart, t.HopPortEnd))
    }
    if len(params) > 0 {
        u += "?" + strings.Join(params, "&")
    }
    return u
}
```

### 4. ECH 所有权分离

| 节点类型 | ECH 管理者 | 分发路径 |
|---------|-----------|---------|
| k2v5 节点 | k2s 自管（setupECHRotation） | k2s → connect-url.txt → sidecar → Center DB → API |
| k2v4-slave 旧节点 | Center（ECHKey 表 + worker_ech.go） | Center → sidecar FetchECHKeys → ech_keys.yaml → k2v4-slave |

不删除 Center 的 ECH 系统，两套并存：
- k2v5 tunnel 的 ECH 来自 SlaveTunnel.ECHConfigList（k2s 上报）
- k2v4 tunnel 响应中的 ECH 来自 Center ECHKey 表（旧流程不变）

### 5. Sidecar FetchECHKeys 控制

k2v5 部署中，sidecar 仍可调用 FetchECHKeys——但仅在配置了 k2v4-slave 时才需要。如果 `ech.enabled=false`（k2v5 自管），sidecar 跳过 FetchECHKeys。这是现有逻辑，无需改动。

## Key Files

| 文件 | 变更 |
|------|------|
| `api/model.go` | SlaveTunnel 新增 CertPin、ECHConfigList 字段 |
| `api/slave_api_node.go` | TunnelConfigInput 新增 certPin、echConfigList |
| `api/api_tunnel.go` | k2v5 响应拼接 serverUrl |
| `api/type.go` | 新增 k2v5 tunnel response type（含 serverUrl） |
| `docker/sidecar/sidecar/node.go` | TunnelConfig 新增 CertPin、ECHConfigList |
| `docker/sidecar/main.go` | 读取 connect-url.txt 并解析 |

## Acceptance Criteria

- [ ] AC1: SlaveTunnel model 新增 cert_pin 和 ech_config_list 字段，DB migration 正常
- [ ] AC2: Sidecar 读取 k2s 的 connect-url.txt，解析出 certPin 和 echConfigList
- [ ] AC3: Sidecar PUT /slave/nodes/:ipv4 注册时上报 certPin 和 echConfigList
- [ ] AC4: Center 存储并更新 k2v5 tunnel 的 cert_pin 和 ech_config_list
- [ ] AC5: GET /api/tunnels/k2v5 返回每个 tunnel 的 serverUrl 计算字段
- [ ] AC6: serverUrl 格式正确：k2v5://domain:port?ech=xxx&pin=xxx[&hop=start-end]
- [ ] AC7: GET /api/tunnels/k2v4 保持不变（k2v5 tunnel 以 k2v4 格式出现，无 serverUrl）
- [ ] AC8: 旧节点（无 cert_pin/ech_config_list）不影响现有功能

## Version History

| Version | Date | Changes |
|---------|------|---------|
| v1 | 2026-02-20 | Initial spec: cert pin + ECH config registration, serverUrl response |
