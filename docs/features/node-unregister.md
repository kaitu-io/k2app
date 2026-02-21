# Feature: Node Unregister (Sidecar Graceful Shutdown Cleanup)

## Meta

| Field | Value |
|-------|-------|
| Feature | node-unregister |
| Version | v1 |
| Status | draft |
| Created | 2026-02-21 |
| Updated | 2026-02-21 |

## Version History

| Version | Date | Summary |
|---------|------|---------|
| v1 | 2026-02-21 | Initial: sidecar graceful shutdown 调用 unregister 清理节点记录。修复 `MarkOffline()` 被 Center API 忽略、导致 0-tunnel 孤儿节点的问题。(from TODO: api-node-status-field) |

## Problem

Sidecar 关闭时（`docker compose down`、容器更新、VPS 销毁）执行 graceful shutdown：

1. 逐个调用 `RemoveTunnel(domain)` → 删除所有隧道 ✅
2. 调用 `MarkOffline()` → 发送 `isAlive: false` ❌ **Center API 完全忽略此字段**

结果：SlaveNode 记录留在数据库，tunnelCount=0，成为孤儿节点。当前线上 22 个节点中有 9 个是此类孤儿（全部 SSH 不可达）。

`deploy-compose.sh` 和 `list_nodes` MCP tool 只能用 `tunnelCount == 0` 启发式判断节点是否活跃。

## Solution

用 `Unregister()` 替代 `RemoveTunnel` × N + `MarkOffline`。Sidecar 关闭时一个调用清理全部数据。

### 1. Center API: 新增 `DELETE /slave/nodes/:ipv4`

Slave 认证的节点自注销端点。

```
DELETE /slave/nodes/:ipv4
Auth: SlaveAuthRequired() (Basic Auth: IPv4:SecretToken)
```

**行为**：
- 验证 IPv4 参数与认证节点匹配
- 在事务中级联删除：
  - `SlaveTunnel` (该节点所有隧道)
  - `SlaveNodeLoad` (该节点所有负载记录)
  - `SlaveNode` (节点本身)
- 全部硬删除（`Unscoped()`），不走软删除
- 返回 `SuccessEmpty(c)`
- 幂等：节点不存在也返回成功

**文件**: `api/slave_api_node.go` — 新增 `api_slave_node_unregister()`
**路由**: `api/route.go` — `slaveManage.DELETE("/nodes/:ipv4", SlaveAuthRequired(), api_slave_node_unregister)`

### 2. Sidecar: 新增 `Node.Unregister()` 方法

```go
// Unregister deletes the node and all associated data from Center.
// Used during graceful shutdown to prevent orphaned node records.
func (n *Node) Unregister() error
```

**行为**：
- 调用 `DELETE /slave/nodes/:ipv4`（带 Basic Auth）
- 失败时返回 error（调用方 log warning 但不阻塞关闭）

**文件**: `docker/sidecar/sidecar/node.go`

### 3. Sidecar: 简化 `shutdown()` 流程

**Before**:
```go
func (s *Sidecar) shutdown() error {
    // 逐个删除隧道
    s.nodeInstance.RemoveTunnel(domain1)
    s.nodeInstance.RemoveTunnel(domain2)
    // MarkOffline 被 API 忽略
    s.nodeInstance.MarkOffline()
}
```

**After**:
```go
func (s *Sidecar) shutdown() error {
    if err := s.nodeInstance.Unregister(); err != nil {
        log.Printf("[Sidecar] Warning: Failed to unregister node: %v", err)
    } else {
        log.Printf("[Sidecar] Node unregistered successfully")
    }
}
```

**文件**: `docker/sidecar/main.go`

### 4. Sidecar: 注册前校验 tunnels 非空

`buildTunnelConfigs()` 返回空时不应调用 `Register()`。当前如果 `K2_DOMAIN` 和 `K2OC_DOMAIN` 都为空，会注册一个 0-tunnel 节点。

**Before**:
```go
tunnels := s.buildTunnelConfigs()
result, err := s.nodeInstance.Register(tunnels) // 可能 0 tunnel
```

**After**:
```go
tunnels := s.buildTunnelConfigs()
if len(tunnels) == 0 {
    return fmt.Errorf("no tunnels configured (K2_DOMAIN and K2OC_DOMAIN are both empty)")
}
result, err := s.nodeInstance.Register(tunnels)
```

**文件**: `docker/sidecar/main.go`

### 5. 清理: 删除 `MarkOffline()` 和 `IsAlive` 字段

`MarkOffline()` 从未真正生效（Center API 忽略 `isAlive`），删除死代码：

- `node.go`: 删除 `MarkOffline()` 方法
- `node.go`: 删除 `NodeUpsertRequest.IsAlive` 字段

## 关于 `restart: unless-stopped`

Docker compose 配置了 `restart: unless-stopped`。几种关闭场景：

| 场景 | SIGTERM | Unregister | 重启 | 结果 |
|------|---------|------------|------|------|
| `docker compose down` | ✅ | ✅ | ❌ | 节点记录被清理 |
| `docker compose up -d`（更新镜像） | ✅ | ✅ | ✅ 自动重启 | 清理后重新注册（10s gap） |
| `docker restart k2-sidecar` | ✅ | ✅ | ✅ 自动重启 | 同上 |
| VPS 销毁 | ✅ (Docker daemon 先收到信号) | 最大努力 | ❌ | 通常能成功 |
| VPS 突然断电 | ❌ | ❌ | ❌ | 无法清理（不可避免） |

更新期间的短暂 gap（~10s）可以接受——当前 `RemoveTunnel` 已经在做同样的事，客户端有重连逻辑。

## 不做的事

- **不加 status 字段**: Unregister 从根源解决问题（删除记录），不需要标记状态
- **不加心跳超时 cron**: 突然断电导致的极少量孤儿可通过管理后台手动删除
- **不改 `api_slave_node_upsert`**: 现有的「先删后建」注册逻辑保持不变
- **不改 batch-matrix / list_nodes / deploy-compose**: 这些消费方的启发式逻辑保留作为防御性措施，但不再是主要依赖

## Acceptance Criteria

- [ ] AC1: Center API `DELETE /slave/nodes/:ipv4` 正确级联删除节点 + 隧道 + 负载记录
- [ ] AC2: Center API `DELETE /slave/nodes/:ipv4` 幂等（节点不存在返回成功）
- [ ] AC3: Center API `DELETE /slave/nodes/:ipv4` 需要 SlaveAuthRequired 认证，且 IPv4 参数与认证节点匹配
- [ ] AC4: Sidecar `Node.Unregister()` 方法调用 `DELETE /slave/nodes/:ipv4`
- [ ] AC5: Sidecar `shutdown()` 调用 `Unregister()` 替代旧的 `RemoveTunnel` × N + `MarkOffline`
- [ ] AC6: Sidecar `Start()` 在 `buildTunnelConfigs()` 返回空时拒绝启动
- [ ] AC7: `MarkOffline()` 和 `IsAlive` 字段被删除
- [ ] AC8: 所有 Center API 测试通过（`cd api && go test ./...`）
- [ ] AC9: 所有 Sidecar 测试通过（`cd docker/sidecar && go test ./...`）

## Technical Notes

- Center API 路由注册：`slaveManage.DELETE("/nodes/:ipv4", SlaveAuthRequired(), api_slave_node_unregister)`
- 新端点不需要 Swagger 注解（项目规范禁止）
- 与现有 `api_admin_delete_node` 逻辑类似但认证方式不同（admin 用 cookie/token，slave 用 Basic Auth）
- 不检查 active batch tasks（shutdown 不需要，批量任务自会处理节点不存在的情况）
