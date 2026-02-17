# Plan: iOS NE Lifecycle & Engine Safety Fixes

## Meta

| Field | Value |
|-------|-------|
| Feature | ios-ne-lifecycle-fixes |
| Spec | docs/features/ios-ne-lifecycle-fixes.md |
| Date | 2026-02-17 |
| Complexity | simple |

## Complexity Rationale

3 files to modify, no new abstractions, no refactoring. Three independent
file-ownership groups across two repos (k2 submodule + iOS native). All
tasks can run in parallel — zero file overlap.

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: error cancel 只调一次 | verify_error_cancel_once (manual) | T1 |
| AC2: 正常断开 cancel(nil) | verify_normal_disconnect (manual) | T1 |
| AC3: disconnect 等待 NE 拆除 | verify_disconnect_await (manual) | T2 |
| AC4: disconnect 5s 超时兜底 | verify_disconnect_timeout (manual) | T2 |
| AC5: disconnecting 状态传播 | verify_disconnecting_state (manual) | T2 |
| AC6: reasserting→reconnecting | verify_reasserting_mapping (manual) | T2 |
| AC7: VPN profile 过滤 bundle ID | verify_profile_filter (manual) | T2 |
| AC8: Engine Stop 取消 Start | TestEngineStartStopRace | T3 |
| AC9: NE settings 从 config 读 | verify_ne_settings_from_config (manual) | T1 |
| AC10: observer 不接收其他 VPN 事件 | verify_observer_scoped (manual) | T2 |

## Feature Tasks

### T1: PacketTunnelProvider — EventBridge 防双重 cancel + NE settings 动态化

**Scope**: TD1 (EventBridge `hasReportedError` 标记) + TD6 (NE settings 从 configJSON 解析)
**Files**:
- `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift`
**Depends on**: none
**TDD**:
- RED: iOS NE 无法做自动化单元测试（系统 API + 双进程）。定义手动验证步骤：
  - `verify_error_cancel_once`: 连接到无效服务器 → Console.app 确认 `cancelTunnelWithError` 只出现一次（带 error）→ K2Plugin 读取到 vpnError
  - `verify_normal_disconnect`: 正常连接后断开 → Console.app 确认 `cancelTunnelWithError(nil)` 只出现一次，App Group 无 vpnError
  - `verify_ne_settings_from_config`: 传入含 `tunnel.dns: ["8.8.4.4"]` 的 configJSON → NE 日志确认使用了自定义 DNS；不传 tunnel 字段 → 确认使用默认值
- GREEN:
  1. EventBridge 类添加 `private var hasReportedError = false`
  2. `onError()`: 设置 `hasReportedError = true`，写 App Group，`cancelTunnelWithError(error)`
  3. `onStateChange("disconnected")`: 检查 `hasReportedError`，已设置则跳过 cancel，未设置则 cancel(nil)
  4. `onStateChange("connecting")`: 重置 `hasReportedError = false`（下一次连接周期）
  5. 添加 `TunnelSettings: Codable` 结构体 + `parseTunnelSettings(from:)` 辅助函数
  6. `startTunnel` 中解析 configJSON 获取 tunnel settings，有值则用，无值则保持默认
- REFACTOR:
  - [SHOULD] 将 NE settings 构建提取为独立函数 `buildNetworkSettings(from:)`
**Acceptance**: AC1, AC2, AC9
**Knowledge**: `docs/knowledge/architecture-decisions.md` → "iOS NE→App Error Propagation via App Group + cancelTunnelWithError"

---

### T2: K2Plugin — disconnect 等待 + 状态映射 + profile 过滤 + observer 限定

**Scope**: TD2 (disconnect await) + TD3 (mapVPNStatus) + TD4 (loadAllFromPreferences filter) + TD7 (observer object scope)
**Files**:
- `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift`
**Depends on**: none
**TDD**:
- RED: K2Plugin 方法依赖 NETunnelProviderManager 系统 API，自动化测试需要完整 NE 环境。定义手动验证步骤：
  - `verify_disconnect_await`: 连接 VPN → 调用 disconnect → 记录 resolve 时间 → Console.app 确认 resolve 在 `.disconnected` 之后（不是立即）
  - `verify_disconnect_timeout`: 模拟 NE 不响应 stop（难以触发，验证 5s 超时代码路径存在）
  - `verify_disconnecting_state`: 连接 VPN → 调用 disconnect → 立即调 getStatus → 确认返回 `state: "disconnecting"`（不是 `"disconnected"`）
  - `verify_reasserting_mapping`: 代码审查确认 `.reasserting` case 映射为 `"reconnecting"`
  - `verify_profile_filter`: 代码审查确认 `loadVPNManager` 使用 `first(where: { bundleIdentifier == ... })`
  - `verify_observer_scoped`: 代码审查确认 `addObserver(object: vpnManager?.connection)` 而非 `object: nil`
- GREEN:
  1. `disconnect()`: 替换为注册一次性 `NEVPNStatusDidChange` 观察者 → 等待 `.disconnected` → resolve。5s `asyncAfter` 超时兜底。
  2. `mapVPNStatus()`: 添加 `.disconnecting → "disconnecting"` 和 `.reasserting → "reconnecting"` case
  3. `loadVPNManager()`: `managers?.first` → `managers?.first(where: { bundleIdentifier filter })`
  4. `load()` 中 `statusObserver`: `object: nil` → `object: vpnManager?.connection`
  5. 注意：observer 注册在 `loadVPNManager` 完成后（vpnManager 有值时），需要在 loadVPNManager completion 中重新注册 observer
- REFACTOR:
  - [MUST] disconnect 中的 observer + timeout 清理逻辑封装，确保 observer 被正确 remove（避免内存泄漏）
  - [SHOULD] 将 `mapVPNStatus` 从 static 函数改为覆盖所有 case 的 switch（去掉 `default` 分支，让编译器检查完整性）
**Acceptance**: AC3, AC4, AC5, AC6, AC7, AC10
**Knowledge**: `docs/knowledge/framework-gotchas.md` → "iOS NE Engine Errors Invisible to Main App by Default"

---

### T3: Engine Start/Stop 竞态安全

**Scope**: TD5 (Start() context 保护 + fail() cancel 清理)
**Files**:
- `k2/engine/engine.go`
- `k2/engine/engine_test.go`
**Depends on**: none
**Note**: k2/ 是 git submodule (read-only from k2app 视角)。此 task 需要在 k2 repo 执行，完成后更新 k2app 的 submodule 引用。
**TDD**:
- RED: 在 `k2/engine/engine_test.go` 添加并发测试
  - `TestEngineStartStopRace`: 启动 Start() goroutine → 50ms 后调用 Stop() → 验证最终 state 为 `StateDisconnected` 且无泄露（cancel 被调用）
  - `TestEngineStopDuringStart_ResourceCleanup`: Start() 中途被取消 → 验证 provider 和 transport 被正确关闭
  - `TestEngineFailCleansCancel`: 调用 fail() → 验证 e.cancel 被调用并置 nil
- GREEN:
  1. `Start()`: 在 `setState(StateConnecting)` 之前创建 `ctx, cancel`，立即保存 `e.cancel = cancel`
  2. `Start()`: 长时间操作前后检查 `ctx.Err() != nil`，被取消则清理资源并返回
  3. `Start()`: 重新获取锁后再次检查 `ctx.Err()`，被取消则关闭 prov + tm 并返回
  4. `fail()`: 调用 `e.cancel()` 并置 nil（如果非 nil）
  5. `Stop()`: 保持现有逻辑（已有 `if e.cancel != nil { e.cancel() }`）
- REFACTOR:
  - [MUST] 确保 Start() 中所有 error 返回路径都调用 `cancel()` 防止 context 泄露
  - [SHOULD] 考虑将 Start() 的长时间操作提取为子函数，减少锁管理复杂度
**Acceptance**: AC8
**Knowledge**: `docs/knowledge/architecture-decisions.md` → "Unified Engine Package for Desktop + Mobile"

---

## Execution Notes

### Parallel Execution

所有三个 task 可完全并行：
- T1 owns `PacketTunnelProvider.swift` (NE extension)
- T2 owns `K2Plugin.swift` (Capacitor plugin)
- T3 owns `k2/engine/engine.go` (Go submodule)

零文件重叠。

### Merge Order

建议：T3 (Go submodule) 先完成并更新 submodule 引用 → T1 + T2 在 k2app 主仓库。
T1 和 T2 之间无顺序依赖。

### iOS 真机验证

T1 + T2 完成后需要一轮完整的 iOS 真机验证：
1. 正常连接 → 断开 → 连接（基础流程）
2. 连接到无效服务器 → 确认错误信息到达 UI
3. 快速 连接→断开→连接（验证 disconnect await）
4. Console.app 检查 cancelTunnelWithError 调用次数

### k2 Submodule 工作流

T3 需要在 k2 repo 操作：
```bash
cd k2
git checkout -b fix/engine-start-stop-race
# ... implement + test ...
go test ./engine/...
git commit && git push
cd ..
git add k2
git commit -m "chore: update k2 submodule — engine start/stop race fix"
```
