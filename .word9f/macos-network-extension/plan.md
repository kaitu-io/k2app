# Plan: macOS Network Extension

## Meta

| Field | Value |
|-------|-------|
| Feature | macos-network-extension |
| Spec | docs/features/macos-network-extension.md |
| Date | 2026-02-23 |
| Complexity | complex |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: DNS 返回正确 IP | manual: `dig youtube.com` after VPN connect | T5 |
| AC2: 不需要 root 密码 | manual: VPN install/start no sudo prompt | T5 |
| AC3: NE 随 app 安装 | manual: DMG/PKG contains KaituTunnel.appex | T4 |
| AC4: webapp run up/down/status 工作 | `test_ne_action_up`, `test_ne_action_down`, `test_ne_action_status` | T3 |
| AC5: NEDNSSettings 生效 | manual: `scutil --dns` shows NE DNS after connect | T2 |
| AC6: NEIPv4Route.default 捕获流量 | manual: `netstat -rn` shows default route via utun | T2 |
| AC7: gomobile macOS xcframework | `test_gomobile_macos_build` (build script verification) | T1 |
| AC8: engine FileDescriptor >= 0 路径 | existing: `k2/engine` tests cover mobile path | T2 |
| AC9: Windows/Linux 不受影响 | `test_daemon_exec_non_macos` (cfg gate test) | T3 |
| AC10: Tauri build 包含 appex | build script verification in T4 | T4 |
| AC11: 升级清理旧 service | `test_preinstall_unloads_launchd` (script test) | T5 |
| AC12: NEVPNStatusDidChange 传播 | `test_state_callback_propagation` | T3 |
| AC13: NE 未运行时 status fallback | `test_k2ne_status_fallback_when_ne_inactive` | T2 |
| AC14: ServiceResponse 信封格式 | `test_k2ne_status_returns_service_response_envelope`, `test_ne_action_response_format` | T2, T3 |
| AC15: macOS UDID 不依赖 daemon | `test_get_udid_macos_native` | T3 |
| AC16: ensure_ne_installed 替代 | `test_ensure_ne_installed_replaces_service` | T3 |

## Foundation Tasks

### T0: gomobile macOS xcframework build

**Scope**: Extend gomobile build to produce macOS-compatible xcframework. Currently `gomobile bind -target=ios` produces iOS-only. Need `macos` target for NE process.
**Files**:
- `scripts/build-mobile-macos-ne.sh` (新增)
- `Makefile` (add `build-macos-ne-lib` target)
**Depends on**: none
**TDD**:
- RED: Write build verification script that asserts `K2Mobile.xcframework` contains `macos-arm64` slice
  - Test functions: `test_xcframework_has_macos_slice`
- GREEN: Run `gomobile bind -target=macos -o K2Mobile.xcframework ./mobile` and verify output
- REFACTOR:
  - [SHOULD] Unify iOS + macOS xcframework build into single multi-target command
**Acceptance**: `K2Mobile.xcframework/macos-arm64/` exists with valid Go symbols
**Knowledge**: `docs/knowledge/task-splitting.md` — "Mobile Build Pipeline: Order Matters"

### T1: macOS NE App Extension target (PacketTunnelProvider)

**Scope**: Create KaituTunnel.appex — the NE process that runs gomobile engine. Reuse iOS PacketTunnelProvider.swift with macOS adaptations (App Group ID, no iOS-specific APIs).
**Files**:
- `desktop/src-tauri/KaituTunnel/PacketTunnelProvider.swift` (新增)
- `desktop/src-tauri/KaituTunnel/Info.plist` (新增)
- `desktop/src-tauri/KaituTunnel/KaituTunnel.entitlements` (新增)
**Depends on**: [T0]
**TDD**:
- RED: Verify Info.plist has correct NSExtensionPointIdentifier and principal class
  - Test functions: `test_info_plist_ne_point`, `test_entitlements_packet_tunnel`
- GREEN: Create PacketTunnelProvider.swift (adapted from iOS), Info.plist, entitlements
- REFACTOR:
  - [SHOULD] Extract shared CIDR parsing utilities if duplicated from iOS
**Acceptance**: KaituTunnel.appex compiles with K2Mobile.xcframework linked, NEDNSSettings configured with matchDomains: [""]

## Feature Tasks

### T2: Swift NE helper static library (libk2_ne_helper.a)

**Scope**: Swift static library wrapping NEVPNManager for C FFI. Exposes install/start/stop/status/callback/reinstall functions callable from Rust. Handles NETunnelProviderManager configuration, startVPNTunnel, sendProviderMessage, NEVPNStatusDidChange observation. All async NE APIs use DispatchSemaphore blocking pattern. All returns wrapped in ServiceResponse JSON envelope.
**Files**:
- `desktop/src-tauri/ne_helper/K2NEHelper.swift` (新增)
- `desktop/src-tauri/ne_helper/k2_ne_helper.h` (新增)
- `desktop/src-tauri/ne_helper/build.sh` (新增 — compile Swift to static lib)
**Depends on**: [T1]
**TDD**:
- RED: Write C header with expected function signatures; verify Swift compiles and exports them
  - Test functions: `test_k2ne_install_symbol_exists`, `test_k2ne_start_symbol_exists`, `test_k2ne_stop_symbol_exists`, `test_k2ne_status_returns_json`, `test_k2ne_status_returns_service_response_envelope`, `test_k2ne_status_fallback_when_ne_inactive`, `test_k2ne_reinstall_symbol_exists`
- GREEN: Implement K2NEHelper.swift:
  - `k2ne_install()` — loadAllFromPreferences (DispatchSemaphore) → saveToPreferences (DispatchSemaphore)。返回 `{"code":0,"message":"ok"}` 或 `{"code":-1,"message":"error msg"}`
  - `k2ne_start(config_json)` — 若无 NE profile 先自动 install → loadAllFromPreferences (semaphore) → startVPNTunnel(options: ["configJSON": ...])。`pid` 参数忽略（NE 由系统管理，不需 PID 监控）
  - `k2ne_stop()` — stopVPNTunnel() → 立即返回 `{"code":0,"message":"disconnecting"}`（NE 异步停止）
  - `k2ne_status()` — 尝试 sendProviderMessage("status") (semaphore, 3s timeout):
    - 成功: 将 engine StatusJSON 包装为 `{"code":0,"message":"ok","data":<engine json>}`
    - 失败（NE 未运行 / timeout）: fallback 到 NEVPNStatus 映射 `{connected→"connected", connecting→"connecting", disconnecting→"disconnecting", reasserting→"reconnecting", disconnected/invalid→"disconnected"}`，返回 `{"code":0,"message":"ok","data":{"state":"<mapped>"}}`
    - 参考: iOS K2Plugin.swift `mapVPNStatus()` + sendProviderMessage catch block
  - `k2ne_set_state_callback(cb)` — NEVPNStatusDidChange observer → C callback
  - `k2ne_reinstall()` — 移除现有 NE profile (removeFromPreferences) → 重新 install
- REFACTOR:
  - [MUST] Ensure thread-safe callback invocation (main queue -> C callback via `@convention(c)` function pointer)
  - [MUST] All C functions返回值为 `UnsafeMutablePointer<CChar>` (ServiceResponse JSON)，Rust 侧 free
  - [MUST] DispatchSemaphore 不能在 main queue 上 wait（死锁）— 确保 C 函数不在 main thread 调用，或使用后台 queue
  - [SHOULD] Add error code constants matching EngineError codes
**Acceptance**: `libk2_ne_helper.a` links into Rust binary; C functions callable from Rust extern blocks; status returns ServiceResponse envelope; NE 未运行时 graceful fallback

### T3: Rust NE bridge (ne.rs) + Tauri IPC routing

**Scope**: Rust module that calls Swift static library via C FFI. `#[cfg(target_os = "macos")]` gate routes `daemon_exec` to NE functions. Windows/Linux path unchanged. Tauri event emission for state changes. macOS 上替换 ensure_service_running → ensure_ne_installed，替换 get_udid → 本地系统 API，替换 admin_reinstall_service → k2ne_reinstall。
**Files**:
- `desktop/src-tauri/src/ne.rs` (新增)
- `desktop/src-tauri/src/service.rs` (修改 — extract macOS-specific code, keep Windows/Linux)
- `desktop/src-tauri/src/main.rs` (修改 — cfg branch for NE vs daemon)
- `desktop/src-tauri/build.rs` (修改 — link libk2_ne_helper.a on macOS)
**Depends on**: [T2]
**TDD**:
- RED: Write Rust tests for NE action routing and response parsing
  - Test functions: `test_ne_action_up`, `test_ne_action_down`, `test_ne_action_status`, `test_ne_action_version`, `test_ne_action_response_format`, `test_daemon_exec_non_macos`, `test_state_callback_propagation`, `test_get_udid_macos_native`, `test_ensure_ne_installed_replaces_service`
- GREEN: Implement:
  - `ne.rs`:
    - extern "C" bindings to all k2ne_* functions
    - `ne_action()` dispatcher: up/down/status/version → 对应 Swift FFI 调用
    - `ne_action("version")` → 直接返回 `ServiceResponse{code:0, data:{version: CARGO_PKG_VERSION, os:"macos"}}` — 不调 NE 进程
    - state callback → Tauri event emission
    - `ensure_ne_installed()` — 调 k2ne_install()，替代 ensure_service_running
    - `get_udid_native()` — macOS 上通过 `sysctl kern.uuid` 或 IOPlatformUUID 获取，不经 daemon
    - `admin_reinstall_ne()` — 调 k2ne_reinstall()，替代 osascript admin_reinstall
  - `service.rs`:
    - `#[cfg(not(target_os = "macos"))]` gate on daemon HTTP functions (core_action, ping_service, etc.)
    - `daemon_exec` command: `#[cfg(macos)]` → ne_action; `#[cfg(not(macos))]` → core_action
    - `ensure_service_running`: `#[cfg(macos)]` → ensure_ne_installed; `#[cfg(not(macos))]` → 现有逻辑
    - `admin_reinstall_service`: `#[cfg(macos)]` → admin_reinstall_ne; `#[cfg(not(macos))]` → 现有逻辑
    - `get_udid`: `#[cfg(macos)]` → get_udid_native; `#[cfg(not(macos))]` → HTTP daemon API
  - `main.rs`: `#[cfg]` branch for NE setup vs daemon setup
  - `build.rs`: conditional link flags for macOS (`-lk2_ne_helper -framework NetworkExtension -framework Foundation`)
- REFACTOR:
  - [MUST] `daemon_exec` IPC command must have identical `ServiceResponse` format across NE and daemon paths — NE helper 已包装信封，Rust 侧 parse JSON 并转 ServiceResponse struct
  - [MUST] `get_pid` on macOS 返回 Tauri app 自身 PID（`std::process::id()`），不再返回 daemon PID（无 daemon）
  - [SHOULD] Extract ServiceResponse mapping to shared module
**Acceptance**: `cargo test` passes; macOS `daemon_exec` routes to NE; Windows `daemon_exec` routes to HTTP daemon; macOS `get_udid` 不依赖 daemon; macOS `ensure_service_running` 调用 NE install; webapp zero changes needed
**Knowledge**: `docs/knowledge/task-splitting.md` — "Entry-Point Files Are Merge Conflict Hotspots"

### T4: Tauri build integration + signing

**Scope**: Integrate KaituTunnel.appex into Tauri build pipeline. Handle code signing for both app and extension. Modify build-macos.sh to include gomobile macOS target, appex compilation, and signing.
**Files**:
- `desktop/src-tauri/tauri.conf.json` (修改 — bundle config for appex)
- `scripts/build-macos.sh` (修改 — add gomobile macOS + appex build + signing)
- `Makefile` (修改 — update build-macos target)
- `desktop/src-tauri/Cargo.toml` (修改 — conditional macOS dependencies)
**Depends on**: [T3]
**TDD**:
- RED: Build script must produce DMG/PKG containing `Kaitu.app/Contents/PlugIns/KaituTunnel.appex`
  - Test functions: `test_dmg_contains_appex`, `test_appex_codesign_valid`
- GREEN: Update build-macos.sh:
  1. `gomobile bind -target=macos` → K2Mobile.xcframework
  2. Compile KaituTunnel.appex (xcodebuild or swiftc)
  3. Compile libk2_ne_helper.a
  4. `yarn tauri build` with appex in PlugIns/
  5. Code sign both app and extension with same identity
- REFACTOR:
  - [SHOULD] Document build pipeline in desktop/CLAUDE.md
**Acceptance**: `make build-macos` produces signed DMG with working NE extension

### T5: Migration + cleanup

**Scope**: Handle upgrade from daemon-based to NE-based macOS app. PKG preinstall removes old launchd service. First-launch NE install flow. Remove macOS-specific daemon code paths (k2 sidecar, osascript install, service lifecycle). Clean up已由 T3 实现 cfg gate 的 macOS 旧代码。
**Files**:
- `scripts/pkg/preinstall` (修改 — add launchd cleanup)
- `desktop/src-tauri/src/service.rs` (修改 — remove macOS daemon install/reinstall functions that T3 已 cfg-gated)
- `desktop/CLAUDE.md` (修改 — update architecture description)
- `CLAUDE.md` (修改 — update domain vocabulary with NE terms)
**Depends on**: [T4]
**TDD**:
- RED: Verify preinstall script handles both old (kaitu.plist) and new formats
  - Test functions: `test_preinstall_unloads_launchd`, `test_preinstall_handles_missing_service`
- GREEN: Implement:
  - preinstall: detect + unload old launchd service (io.kaitu.k2.plist, io.kaitu.service.plist, com.kaitu.service.plist)
  - service.rs: 清理 T3 留下的 macOS daemon 死代码（已被 cfg gate 隔离的函数体可以删除）
  - service.rs: `admin_reinstall_service_macos()` 整个函数在 macOS NE 模式下已由 T3 替换为 `admin_reinstall_ne()`，删除旧实现
  - 验证: macOS build 不再包含 k2 binary sidecar
- REFACTOR:
  - [MUST] Update desktop/CLAUDE.md with new NE architecture：
    - 新增 ne.rs 模块说明
    - 更新 service.rs 说明（macOS 已去 daemon）
    - 更新 IPC Commands 表（macOS 分支说明）
    - 新增 NE 签名/entitlements 说明
  - [MUST] Update root CLAUDE.md domain vocabulary:
    - 新增 `k2ne_*` C FFI 函数说明
    - 新增 `ensure_ne_installed` 说明
    - 更新 `ensure_service_running` 说明（仅 Windows/Linux）
  - [SHOULD] Remove dead macOS daemon code (detect_old_kaitu_service, remove_old_kaitu_service, admin_reinstall_service_macos)
  - [SHOULD] 清理 macOS 上不再需要的 k2 sidecar binary 配置
**Acceptance**: Upgrade from daemon-version to NE-version: old service removed, NE installed, VPN works; macOS build 无 k2 sidecar

## Execution Order

```
T0 (gomobile macOS xcframework)
  |
  v
T1 (NE App Extension: PacketTunnelProvider)
  |
  v
T2 (Swift NE helper: libk2_ne_helper.a)
  |
  v
T3 (Rust NE bridge: ne.rs + IPC routing)
  |
  v
T4 (Tauri build + signing)
  |
  v
T5 (Migration + cleanup)
```

All tasks are sequential — each depends on the previous. No parallelism possible because:
- T1 needs T0's xcframework to link
- T2 needs T1's appex bundle ID for NEVPNManager config
- T3 needs T2's static library to link
- T4 needs T3's Rust changes to build
- T5 needs T4's working build to test migration

## Runtime Flow Review (v1.1)

Post-plan runtime flow analysis identified 9 design flaws. All incorporated into T2/T3/T5 above.

| # | Flaw | Severity | Fix Location |
|---|------|----------|-------------|
| 1 | `ensure_service_running` must be fully replaced on macOS | MEDIUM | T3: `ensure_ne_installed()` |
| 2 | `pid` param useless for NE (no PID monitoring) | LOW | T2: `k2ne_start` ignores pid |
| 3 | Async NE APIs need DispatchSemaphore blocking | HIGH | T2: all k2ne_* use semaphore |
| 4 | First-launch race: `up` before `install` | MEDIUM | T2: `k2ne_start` auto-installs |
| 5 | Response format mismatch (no ServiceResponse envelope) | **CRITICAL** | T2: all returns wrapped in `{code,message,data}` |
| 6 | `sendProviderMessage` fails when NE not running | HIGH | T2: fallback to NEVPNStatus mapping |
| 7 | `stopVPNTunnel` is async (returns before NE stopped) | LOW | T2: immediate return, polling detects final state |
| 8 | Engine has no `version` action | LOW | T3: return app version from Rust directly |
| 9 | UDID comes from daemon, not available in NE mode | HIGH | T3: `get_udid_native()` via sysctl/IOKit |
