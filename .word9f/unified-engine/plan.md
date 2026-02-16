# Plan: Unified Engine + Rule Mode

## Meta

| Field | Value |
|-------|-------|
| Feature | unified-engine |
| Spec | docs/features/mobile-rule-storage.md |
| Date | 2026-02-16 |
| Complexity | moderate |

## Dependency Graph

```
F1 (engine/ package) ──┬── T2 (desktop daemon migration)
                       ├── T3 (mobile wrapper migration) ──┬── T4 (iOS native)
                       │                                   └── T5 (Android native)
                       └── T6 (webapp TypeScript)
```

Parallel groups:
- After F1: T2, T3, T6 can run in parallel
- After T3: T4, T5 can run in parallel

Critical path: F1 → T3 → T4 (or T5)

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: engine/ package | TestEngineNew | F1 |
| AC2: unified assembly | TestEngineStart_MobileConfig, TestEngineStart_DesktopConfig | F1 |
| AC3: mobile fd DNS middleware | TestEngineStart_MobileFD_UsesDNSMiddleware | F1 |
| AC4: desktop fd=-1 self-create TUN | TestEngineStart_DesktopConfig (mock provider) | F1 |
| AC5: DirectDialer in config | TestEngineStart_WithDirectDialer | F1 |
| AC6: PreferIPv6 | TestEngineStart_PreferIPv6 | F1 |
| AC7: proxy mode | TestEngineStart_ProxyMode | F1 |
| AC8: delete BuildTunnel | TestDaemonDoUp_UsesEngine | T2 |
| AC9: HTTP API unchanged | TestHandleCoreUpDown (existing, adapted) | T2 |
| AC10: auto-reconnect unchanged | TestAutoReconnect_UsesEngine | T2 |
| AC11: tests pass | `go test ./engine/... ./daemon/... ./mobile/...` | T2, T3 |
| AC12: mobile thin shell | TestMobileEngine_DelegatesToEngine | T3 |
| AC13: Start(url, fd, dataDir) | TestMobileEngine_Start_ThreeParams | T3 |
| AC14: rule from URL query | TestEngineStart_RuleFromURL_Smart | F1 |
| AC15: rule=smart/global → k2rule | TestEngineStart_RuleSmart_K2ruleNotGlobal, TestEngineStart_RuleGlobal_K2ruleGlobal | F1 |
| AC16: K2Plugin.swift setRuleMode + URL | Manual: iOS connect verify URL contains &rule= | T4 |
| AC17: K2Plugin.kt setRuleMode + URL | Manual: Android connect verify URL contains &rule= | T5 |
| AC18: PacketTunnelProvider dataDir | Manual: iOS NE cold start with dataDir | T4 |
| AC19: K2VpnService dataDir | Manual: Android VPN start with dataDir | T5 |
| AC20: definitions.ts + native-client.ts | TestNativeVpnClient_setRuleMode | T6 |
| AC21: Dashboard UI disconnect+reconnect | Manual: toggle mode → verify disconnect → reconnect | T6 |

## Foundation Tasks

### F1: Create `engine/` package — unified tunnel assembly

**Scope**: Extract shared tunnel assembly logic from `daemon/tunnel.go` BuildTunnel() and
`mobile/mobile.go` Engine.Start() into a new `k2/engine/` package. This becomes the single
tunnel lifecycle manager used by both desktop and mobile.

**Files**:
- `k2/engine/event.go` — NEW: EventHandler interface + state constants
- `k2/engine/config.go` — NEW: Config struct with all platform options
- `k2/engine/dns_handler.go` — NEW: dnsHandler (moved from mobile/mobile.go)
- `k2/engine/engine.go` — NEW: Engine struct, Start(), Stop(), StatusJSON(), Status()
- `k2/engine/engine_test.go` — NEW: unit tests

**Depends on**: none

**TDD**:
- RED: Write failing tests for engine lifecycle and config variations
  - `TestEngineNew` — New() returns disconnected engine
  - `TestEngineStart_InvalidURL` — bad URL returns error, state stays disconnected
  - `TestEngineStart_DoubleStart` — second Start() returns "already started" error
  - `TestEngineStop_NotStarted` — Stop() on fresh engine is no-op
  - `TestEngineStart_RuleFromURL_Smart` — URL `?rule=smart` → k2rule.Init(IsGlobal: false)
  - `TestEngineStart_RuleFromURL_Global` — URL `?rule=global` → k2rule.Init(IsGlobal: true)
  - `TestEngineStart_RuleDefault_Global` — no `rule` param defaults to global
  - `TestEngineStart_DataDir_PassedToK2rule` — cfg.DataDir sets k2rule CacheDir
  - `TestEngineStart_MobileConfig` — fd >= 0 uses DNS middleware path
  - `TestEngineStart_DesktopConfig` — fd == -1, Mode "tun" uses self-create TUN path
  - `TestEngineStart_ProxyMode` — fd == -1, Mode "proxy" uses ProxyProvider
  - `TestEngineStart_WithDirectDialer` — non-nil DirectDialer passed to transports
  - `TestEngineStart_PreferIPv6` — PreferIPv6 + wireCfg.IPv6 available → host replaced
  - `TestEngineStart_RuleConfig_Override` — non-nil RuleConfig takes priority over RuleMode
  - `TestEngineStart_EventHandler_StateChanges` — handler receives connecting → error|connected
  - `TestEngineStatusJSON_Disconnected` — JSON has state only
  - `TestEngineStatusJSON_Connected` — JSON has state, connected_at, uptime_seconds, wire_url
  - `TestEngineStatusJSON_AfterFailedStart` — JSON has error field
- GREEN: Implement engine.go assembling tunnel from Config:
  1. k2rule init (RuleConfig priority > RuleMode+DataDir > URL query `rule=`)
  2. wire.ParseURL
  3. Optional IPv6 preference
  4. Build transports with optional DirectDialer
  5. Router
  6. Provider selection (fd >= 0 → platform TUN; fd == -1 + proxy → ProxyProvider; fd == -1 + tun → self-create TUN)
  7. ClientTunnel
  8. Start method: fd >= 0 → prov.Start(ctx, dnsHandler); fd < 0 → tunnel.Start(ctx)
  9. Stop, StatusJSON, Status — same logic as current mobile/mobile.go
- REFACTOR:
  - [MUST] Extract `parseRuleFromURL(rawURL string) string` helper — reusable in tests
  - [MUST] Extract `buildK2ruleConfig(cfg Config) *k2rule.Config` helper — used by both RuleConfig and RuleMode paths
  - [SHOULD] Consolidate state constant names if daemon and mobile used different ones

**Acceptance**:
- `go test ./engine/...` passes all tests
- Config struct covers all desktop and mobile parameters
- dnsHandler moved from mobile, tests passing in new location
- No import cycles (engine/ does not import daemon/ or mobile/)

**Knowledge**:
- `docs/knowledge/architecture-decisions.md` → "iOS Two-Process vs Android Single-Process VPN"
- `docs/knowledge/architecture-decisions.md` → "NativeVpnClient Mobile Bridge"

---

## Feature Tasks

### T2: Desktop daemon migration to engine.Engine

**Scope**: Replace `daemon.BuildTunnel()` with `engine.Engine`, making daemon a thin HTTP shell.
Daemon keeps HTTP API, persisted state, auto-reconnect, process monitoring — delegates
tunnel lifecycle entirely to Engine.

**Files**:
- `k2/daemon/daemon.go` — MODIFY: replace tunnel/cancel fields with engine field, rewrite doUp/closeTunnel/statusInfo
- `k2/daemon/tunnel.go` — DELETE: BuildTunnel() logic moved to engine/
- `k2/daemon/api_test.go` — MODIFY: adapt mock pattern (EngineBuilder instead of TunnelBuilder)

**Depends on**: [F1]

**TDD**:
- RED: Write/adapt failing tests for daemon using engine
  - `TestDaemonDoUp_UsesEngine` — doUp creates engine, calls Start with correct Config
  - `TestHandleCoreUpDown` — EXISTING: must still pass after migration (adapt mock)
  - `TestDoUpFromError` — EXISTING: must still pass (error recovery via engine)
  - `TestConcurrentDoUp` — EXISTING: must still pass (concurrency safety)
  - `TestDaemonBuildEngineConfig` — new: buildEngineConfig maps ClientConfig → engine.Config correctly
  - `TestDaemonBuildEngineConfig_DirectDialer` — DirectDialer created and set
  - `TestDaemonBuildEngineConfig_RuleConfig` — RuleConfig populated from ClientConfig.Rule
  - `TestDaemonBuildEngineConfig_DNSExclude` — DNS server IPs parsed into exclude list
- GREEN:
  1. Replace `tunnel *core.ClientTunnel` + `tunnelCnl` with `engine *engine.Engine` in Daemon struct
  2. Add `EngineBuilder func() *engine.Engine` field (for test DI, like current TunnelBuilder)
  3. Add `buildEngineConfig(cfg *config.ClientConfig) engine.Config` method — maps ClientConfig fields to engine.Config including DirectDialer, RuleConfig, DNSExclude, PreferIPv6
  4. Rewrite `doUp()`: create engine via EngineBuilder, call engine.Start(cfg)
  5. Rewrite `closeTunnel()`: call engine.Stop()
  6. Rewrite `statusInfo()`: delegate to engine.StatusJSON() or keep own state tracking
  7. Delete `tunnel.go` (BuildTunnel no longer needed)
  8. Update api_test.go: replace mockTunnelBuilder with mock Engine or EngineBuilder
- REFACTOR:
  - [MUST] Remove `TunnelBuilder` field, add `EngineBuilder` (breaking change for tests)
  - [SHOULD] Remove `splitHostPort` if no longer needed locally

**Acceptance**:
- `go test ./daemon/...` passes (all 13 existing tests + new tests)
- `tunnel.go` deleted
- Daemon struct no longer imports `core.ClientTunnel` directly
- HTTP API behavior identical (same request/response format)

**Knowledge**:
- `docs/knowledge/task-splitting.md` → "Desktop Tasks: Parallel Modules, Sequential main.rs Wiring"

---

### T3: Mobile wrapper migration to thin gomobile shell

**Scope**: Rewrite `mobile/mobile.go` from 251 lines to ~60 lines by delegating to
`engine.Engine`. Keep gomobile-compatible API surface (string, int, int64 params only).

**Files**:
- `k2/mobile/mobile.go` — REWRITE: thin wrapper around engine.Engine
- `k2/mobile/event.go` — MODIFY: keep EventHandler interface for gomobile, add eventBridge adapter
- `k2/mobile/mobile_test.go` — MODIFY: adapt tests to new wrapper API

**Depends on**: [F1]

**TDD**:
- RED: Write/adapt tests for new mobile wrapper
  - `TestMobileEngine_DelegatesToEngine` — NewEngine creates inner engine.Engine
  - `TestMobileEngine_Start_ThreeParams` — Start(url, fd, dataDir) delegates to inner.Start(engine.Config{...})
  - `TestMobileEngine_Start_InvalidURL` — EXISTING: must still pass
  - `TestMobileEngine_DoubleStart` — EXISTING: must still pass
  - `TestMobileEngine_Stop_NotStarted` — EXISTING: must still pass
  - `TestMobileEngine_EventHandler_StateChanges` — EXISTING: eventBridge adapter works
  - `TestMobileEngine_StatusJSON_Disconnected` — EXISTING: must still pass
  - `TestMobileEngine_StatusJSON_Connected` — EXISTING: must still pass
  - `TestMobileEngine_StatusJSON_MatchesDaemonFormat` — EXISTING: must still pass
- GREEN:
  1. Rewrite Engine struct: single `inner *engine.Engine` field
  2. NewEngine() creates `engine.New()` internally
  3. Start(url, fd, dataDir) → `inner.Start(engine.Config{WireURL: url, FileDescriptor: fd, DataDir: dataDir})`
  4. Stop(), StatusJSON(), Status() → direct delegation
  5. Add `eventBridge` struct adapting mobile.EventHandler → engine.EventHandler
  6. SetEventHandler wraps caller's handler in eventBridge
- REFACTOR:
  - [MUST] Remove dnsHandler from mobile package (now in engine/)
  - [MUST] Move DNS-related test helpers to engine/ test or keep in mobile test (if engine tests have their own)
  - [SHOULD] Simplify mobile_test.go by removing tests that are now covered by engine_test.go

**Acceptance**:
- `go test ./mobile/...` passes
- `mobile.go` < 80 lines
- gomobile bind compiles without error (verify: `gomobile bind -target=ios ./mobile/`)
- Start() signature is `Start(url string, fd int, dataDir string) error`

**Knowledge**:
- `docs/knowledge/task-splitting.md` → "Mobile Build Pipeline: Order Matters"

---

### T4: iOS native — rule mode + dataDir

**Scope**: Add setRuleMode to K2Plugin.swift, append rule to wireUrl in connect(),
pass dataDir from App Group container to PacketTunnelProvider engine.start().

**Files**:
- `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` — MODIFY: add setRuleMode, modify connect()
- `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift` — MODIFY: pass dataDir to engine.start()

**Depends on**: [T3] (gomobile xcframework must have new Start(url, fd, dataDir) signature)

**TDD**:
- RED: Manual test scenarios (native Swift, no unit test framework in plugin)
  - Scenario: `setRuleMode("smart")` → verify UserDefaults(`group.io.kaitu`).ruleMode == "smart"
  - Scenario: `connect(wireUrl)` → verify startVPNTunnel receives wireUrl with `&rule=smart`
  - Scenario: PacketTunnelProvider.startTunnel → verify engine.start called with 3 params including dataDir
  - Scenario: iOS NE cold start → wireUrl from providerConfiguration contains `&rule=xxx`
- GREEN:
  1. Add `setRuleMode` to pluginMethods array and implement method
  2. In connect(): read ruleMode from UserDefaults, append `&rule=xxx` to wireUrl
  3. Update providerConfiguration to include rule-appended wireUrl
  4. Update startVPNTunnel options to use finalUrl
  5. In PacketTunnelProvider.startTunnel: compute App Group `/k2` path, create dir, pass to engine.start(wireUrl, fd, dataDir)
- REFACTOR:
  - [SHOULD] Extract URL rule-appending into private helper method

**Acceptance**:
- Xcode build succeeds
- `gomobile bind` → xcframework copy → `cap sync` → xcodebuild archive — full pipeline works
- setRuleMode persists to App Group UserDefaults
- connect() appends `&rule=xxx` to wireUrl
- NE cold start receives correct wireUrl with rule and passes dataDir

**Knowledge**:
- `docs/knowledge/architecture-decisions.md` → "iOS Two-Process vs Android Single-Process VPN"
- `docs/knowledge/architecture-decisions.md` → "iOS NE→App Error Propagation via App Group"

---

### T5: Android native — rule mode + dataDir

**Scope**: Add setRuleMode to K2Plugin.kt, append rule to wireUrl in startVpnService(),
pass dataDir (filesDir) to K2VpnService engine.start().

**Files**:
- `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt` — MODIFY: add setRuleMode, modify startVpnService()
- `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt` — MODIFY: pass dataDir to engine.start()

**Depends on**: [T3] (gomobile AAR must have new Start(url, fd, dataDir) signature)

**TDD**:
- RED: Manual test scenarios (Android)
  - Scenario: `setRuleMode("smart")` → verify SharedPreferences("k2vpn").ruleMode == "smart"
  - Scenario: `startVpnService(wireUrl)` → verify Intent extra wireUrl contains `&rule=smart`
  - Scenario: K2VpnService.startVpn → verify engine.start called with 3 params including filesDir
- GREEN:
  1. Add `setRuleMode` @PluginMethod to K2Plugin.kt
  2. In startVpnService(): read ruleMode from SharedPreferences, append `&rule=xxx`
  3. In K2VpnService.startVpn(): pass `filesDir.absolutePath` as dataDir to engine.start()
- REFACTOR:
  - [SHOULD] Extract URL rule-appending into private helper method

**Acceptance**:
- `./gradlew assembleRelease` succeeds
- `gomobile bind` → AAR copy → `cap sync` → gradle build — full pipeline works
- setRuleMode persists to SharedPreferences
- startVpnService appends `&rule=xxx` to wireUrl
- K2VpnService passes dataDir to engine

**Knowledge**:
- `docs/knowledge/architecture-decisions.md` → "Android AAR: Direct flatDir, No Wrapper Module"

---

### T6: Webapp TypeScript — setRuleMode type + method

**Scope**: Add setRuleMode to K2Plugin TypeScript definitions and NativeVpnClient.
Minimal change — webapp connect() signature stays identical.

**Files**:
- `mobile/plugins/k2-plugin/src/definitions.ts` — MODIFY: add setRuleMode declaration
- `webapp/src/vpn-client/native-client.ts` — MODIFY: add setRuleMode to K2PluginType + NativeVpnClient
- `webapp/src/vpn-client/types.ts` — MODIFY: add optional setRuleMode to VpnClient interface
- `webapp/src/vpn-client/__tests__/native-client.test.ts` — MODIFY: add setRuleMode test

**Depends on**: [F1] (only for timing — no actual file dependency)

**TDD**:
- RED: Write failing test for setRuleMode
  - `TestNativeVpnClient_setRuleMode` — calls plugin.setRuleMode({mode: "smart"})
  - `TestNativeVpnClient_setRuleMode_global` — calls plugin.setRuleMode({mode: "global"})
- GREEN:
  1. Add `setRuleMode(options: { mode: string }): Promise<void>` to K2PluginInterface in definitions.ts
  2. Add same to K2PluginType in native-client.ts
  3. Add `async setRuleMode(mode: string): Promise<void>` to NativeVpnClient class
  4. Add optional `setRuleMode?(mode: string): Promise<void>` to VpnClient interface in types.ts
  5. Add test: mock plugin, call setRuleMode, assert plugin.setRuleMode called with {mode: "smart"}
- REFACTOR:
  - [SHOULD] Verify existing native-client tests still pass with added interface method

**Acceptance**:
- `yarn test` passes (all 279+ tests)
- `npx tsc --noEmit` passes
- definitions.ts and native-client.ts have matching setRuleMode signatures
- VpnClient interface has optional setRuleMode (HttpVpnClient/MockVpnClient don't need it)

---

## Execution Summary

| Task | Files | Depends | Parallel Group |
|------|-------|---------|----------------|
| F1 | engine/ (5 new) | none | Group 0 |
| T2 | daemon/ (2 modify + 1 delete) | F1 | Group 1 |
| T3 | mobile/ (3 modify) | F1 | Group 1 |
| T6 | webapp + definitions (4 modify) | F1 | Group 1 |
| T4 | iOS native (2 modify) | T3 | Group 2 |
| T5 | Android native (2 modify) | T3 | Group 2 |

**Merge order**: F1 → (T2 ‖ T3 ‖ T6) → (T4 ‖ T5)

**Estimated file overlap conflicts**: 0 (all tasks touch disjoint files)

**Note on k2 submodule**: F1, T2, T3 all modify files in the `k2/` submodule. For worktree
execution, these should be branches in the k2 repo. F1 merges first, then T2 and T3 can be
parallel branches off F1. After all k2 changes merge, update the submodule pointer in k2app.
T4, T5, T6 are k2app branches.
