# Plan: Config-Driven Connect

## Meta

| Field | Value |
|-------|-------|
| Feature | config-driven-connect |
| Spec | docs/features/config-driven-connect.md |
| Date | 2026-02-16 |
| Complexity | moderate |

## Execution Note

Dependency graph is a sequential chain (F1 → T2 → T3). Per task-splitting knowledge: "If depends_on graph is a straight line, consider working directly on a single branch." Recommend single-branch execution — worktree overhead exceeds parallelism benefit.

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: ClientConfig TS mirrors Go | tsc --noEmit (types.ts compiles) | T3 |
| AC2: Webapp assembles config | test_buildConfig_assembles_server_and_rule | T3 |
| AC3: Omitted fields use defaults | TestSetDefaultsApplied | F1 |
| AC4: Daemon up accepts config JSON | TestHandleUpConfigJSON | F1 |
| AC5: wire_url/config_path deleted | TestHandleUpRejectsWireURL | F1 |
| AC6: doUp takes ClientConfig | TestDoUpAcceptsClientConfig | F1 |
| AC7: buildEngineConfig deleted | structural (function removed) | F1 |
| AC8: getConfig returns ClientConfig | TestHandleGetConfigReturnsClientConfig | F1 |
| AC9: Auto-reconnect uses saved ClientConfig | TestAutoReconnectWithSavedConfig | F1 |
| AC10: persistedState saves ClientConfig | TestPersistedStateWithClientConfig | F1 |
| AC11: CLI URL → ClientConfig | TestCmdUpURLToClientConfig | F1 |
| AC12: CLI YAML → ClientConfig | TestCmdUpYAMLToClientConfig | F1 |
| AC13: CLI sends config JSON | TestCmdUpSendsConfigJSON | F1 |
| AC14: K2Plugin connect config | tsc --noEmit (definitions.ts) | T2 |
| AC15: Engine.Start configJSON | TestMobileEngineStartConfigJSON | F1 |
| AC16: iOS NE receives configJSON | manual test | T2 |
| AC17: Android receives configJSON | manual test | T2 |
| AC18: setRuleMode removed K2Plugin | structural (method removed from defs+swift+kt) | T2 |
| AC19: setRuleMode removed VpnClient | test_vpn_client_interface_no_setRuleMode | T3 |
| AC20: No &rule= URL append | structural (code deleted from swift+kt) | T2 |
| AC21: No native ruleMode storage | structural (UserDefaults/SharedPrefs code deleted) | T2 |
| AC22: connect→getConfig round-trip | test_config_round_trip | T3 |
| AC23: Go ClientConfig has JSON tags | TestClientConfigJSONRoundTrip | F1 |
| AC24: JSON ↔ YAML equivalence | TestClientConfigJSONYAMLEquivalence | F1 |

## Foundation Tasks

### F1: Go Core — Config-Driven Everything

**Scope**: All k2/ submodule changes. Add JSON tags to ClientConfig. Export SetDefaults. Rewrite daemon API (config-only), doUp(*ClientConfig), persistedState, handleGetConfig, statusInfo, tryAutoReconnect. Delete buildEngineConfig, lastWireURL, lastConfigPath. Change mobile Engine.Start to accept configJSON. Change CLI to resolve input → ClientConfig before sending.

**Files**:
- `k2/config/config.go` — add `json:` tags to ClientConfig + all sub-structs, rename `setClientDefaults` → `SetDefaults` (export)
- `k2/daemon/api.go` — handleUp: parse `config` JSON only, reject wire_url/config_path; handleGetConfig: return `*ClientConfig`
- `k2/daemon/daemon.go` — doUp(cfg *ClientConfig, pid int), delete buildEngineConfig(), replace lastWireURL+lastConfigPath with lastConfig, update statusInfo(), update tryAutoReconnect()
- `k2/daemon/state.go` — persistedState: Config *ClientConfig (not WireURL+ConfigPath)
- `k2/mobile/mobile.go` — Start(configJSON string, fd int, dataDir string): parse JSON → SetDefaults → engine.Config
- `k2/cmd/k2/cmd_up.go` — resolve URL → ClientFromURL() or YAML → LoadClient(), send `{"config": cfg}`

**Depends on**: none

**TDD**:
- RED: Write failing tests for all new signatures and behaviors
  - `TestClientConfigJSONRoundTrip` — marshal ClientConfig to JSON → unmarshal → deep equal
  - `TestClientConfigJSONYAMLEquivalence` — same config via JSON and YAML produces identical struct
  - `TestSetDefaultsApplied` — SetDefaults fills all empty fields (dns, mode, proxy, log)
  - `TestHandleUpConfigJSON` — POST `{ "action": "up", "params": { "config": { "server": "k2v5://..." } } }` → 200 connecting
  - `TestHandleUpRejectsWireURL` — POST with `wire_url` param → error (no such param)
  - `TestDoUpAcceptsClientConfig` — doUp(&ClientConfig{Server: "..."}, 0) → engine starts
  - `TestPersistedStateWithClientConfig` — save state with Config field → load → Config preserved
  - `TestHandleGetConfigReturnsClientConfig` — after doUp, get_config returns full ClientConfig JSON
  - `TestAutoReconnectWithSavedConfig` — save connected state with Config → new daemon → auto-reconnect uses Config
  - `TestMobileEngineStartConfigJSON` — Start(`{"server":"k2v5://...","rule":{"global":true}}`, fd, dir) → engine starts with correct config
  - `TestMobileEngineStartInvalidJSON` — Start("not json", fd, dir) → error
  - `TestCmdUpURLToClientConfig` — URL arg produces ClientConfig with server field
  - `TestCmdUpYAMLToClientConfig` — YAML file arg produces ClientConfig from file

- GREEN: Implement all changes to pass RED tests. Key implementation order:
  1. config.go: add JSON tags + export SetDefaults
  2. state.go: new persistedState struct
  3. daemon.go: new doUp signature, delete buildEngineConfig, replace lastWireURL/lastConfigPath
  4. api.go: new handleUp, new handleGetConfig
  5. mobile.go: new Start(configJSON)
  6. cmd_up.go: resolve before send

- REFACTOR:
  - [MUST] Delete `buildEngineConfig()` entirely — its input-parsing job moved to API boundary, its mapping job is `engineConfigFromClientConfig()`
  - [MUST] Delete `lastWireURL` and `lastConfigPath` fields from Daemon struct
  - [SHOULD] Delete `parseRuleFromURL()` from engine/engine.go (dead code after mobile passes RuleMode explicitly)
  - [SHOULD] Update doc comments on Daemon struct

**Acceptance**: AC3–AC13, AC15, AC23, AC24

**Knowledge**:
- `docs/knowledge/architecture-decisions.md` → "Unified Engine Package" (engine.Config structure)
- `docs/knowledge/architecture-decisions.md` → "Mobile Rule Mode Storage" (this is being replaced)
- `docs/knowledge/task-splitting.md` → "Cross-Repo Worktree for Submodule Changes" (single branch for sequential k2 changes)

---

## Feature Tasks

### T2: K2Plugin — Native Bridge + TS Definitions

**Scope**: Update K2Plugin native implementations (Swift, Kotlin) and TypeScript definitions to accept config JSON instead of wireUrl. Delete setRuleMode from all three layers. Delete URL query hack. Delete native preference storage for ruleMode.

**Files**:
- `mobile/plugins/k2-plugin/src/definitions.ts` — connect({ config: string }) replaces connect({ wireUrl: string }), remove setRuleMode, update K2PluginPlugin interface
- `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` — connect: extract config JSON string, pass to engine.start(configJSON, fd, dataDir). Delete setRuleMode(). Delete ruleMode UserDefaults read. Delete `&rule=` URL append. Update providerConfiguration to pass configJSON instead of wireUrl.
- `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt` — connect: extract config JSON string, pass to engine.start(configJSON, fd, dataDir). Delete setRuleMode(). Delete ruleMode SharedPreferences read. Delete `&rule=` URL append. Update VpnService Intent extra to pass configJSON.

**Depends on**: [F1] (mobile Engine.Start signature)

**TDD**:
- RED: TypeScript type check is the automated gate
  - `tsc --noEmit` on k2-plugin definitions (verifies connect signature, no setRuleMode)
  - Manual device test: iOS connect with config JSON → engine receives correct config
  - Manual device test: Android connect with config JSON → engine receives correct config
- GREEN: Implement Swift/Kotlin changes:
  1. definitions.ts: new connect interface, remove setRuleMode
  2. K2Plugin.swift: pass configJSON through, delete setRuleMode + URL hack
  3. K2Plugin.kt: pass configJSON through, delete setRuleMode + URL hack
- REFACTOR:
  - [MUST] Delete setRuleMode from all three files (definitions.ts, Swift, Kotlin)
  - [MUST] Delete ruleMode UserDefaults read in K2Plugin.swift
  - [MUST] Delete ruleMode SharedPreferences read in K2Plugin.kt
  - [MUST] Delete `&rule=` URL append logic in both Swift and Kotlin
  - [SHOULD] Clean up unused imports after deletions

**Acceptance**: AC14, AC16–AC18, AC20, AC21

**Knowledge**:
- `docs/knowledge/gotchas.md` → "gomobile Swift throws pattern" (Swift call convention for engine.start)
- `docs/knowledge/gotchas.md` → "Go→JS JSON key remapping" (key convention at bridge)
- `docs/knowledge/architecture-decisions.md` → "iOS Two-Process vs Android Single-Process VPN" (iOS providerConfiguration vs Android Intent)

---

### T3: Webapp — VpnClient + Store + Dashboard

**Scope**: Add ClientConfig TypeScript type. Change VpnClient interface: connect(config: ClientConfig) replaces connect(wireUrl: string), remove setRuleMode. Update all three VpnClient implementations. Update vpn.store connect flow. Update Dashboard to assemble config from server + user preferences.

**Files**:
- `webapp/src/vpn-client/types.ts` — add ClientConfig interface, change connect(config: ClientConfig), change VpnConfig to return ClientConfig, remove setRuleMode from VpnClient
- `webapp/src/vpn-client/http-client.ts` — connect sends `{ config: cfg }` to daemon API
- `webapp/src/vpn-client/native-client.ts` — connect passes config JSON string to K2Plugin.connect({ config }), remove setRuleMode
- `webapp/src/vpn-client/mock-client.ts` — update connect signature, remove setRuleMode mock
- `webapp/src/stores/vpn.store.ts` — connect(config: ClientConfig) replaces connect(wireUrl: string), remove setRuleMode action
- `webapp/src/pages/Dashboard.tsx` — buildConfig(server, prefs) assembles ClientConfig, handleConnect uses buildConfig

**Depends on**: [T2] (K2Plugin TS definitions for native-client type safety)

**TDD**:
- RED: Write failing tests for all new signatures
  - `test_buildConfig_assembles_server_and_rule` — buildConfig with global=true → { server: wireUrl, rule: { global: true } }
  - `test_buildConfig_minimal` — buildConfig with defaults → { server: wireUrl } (no rule field when default)
  - `test_HttpVpnClient_connect_sends_config` — connect(config) → POST body contains `{ "action": "up", "params": { "config": {...} } }`
  - `test_NativeVpnClient_connect_passes_config` — connect(config) → K2Plugin.connect called with { config: JSON.stringify(config) }
  - `test_MockVpnClient_connect_accepts_config` — connect(config) → state changes to connecting
  - `test_vpn_client_interface_no_setRuleMode` — VpnClient type has no setRuleMode property
  - `test_vpn_store_connect_with_config` — store.connect(config) calls vpnClient.connect(config)
  - `test_config_round_trip` — connect(config) → getConfig() returns matching ClientConfig
  - `test_Dashboard_assembles_config` — handleConnect creates correct ClientConfig from selected server + rule mode pref
- GREEN: Implement all changes:
  1. types.ts: ClientConfig type, new connect signature, new VpnConfig = ClientConfig
  2. http-client.ts: connect sends config JSON
  3. native-client.ts: connect passes config string to K2Plugin
  4. mock-client.ts: new signature
  5. vpn.store.ts: connect(config)
  6. Dashboard.tsx: buildConfig + handleConnect
- REFACTOR:
  - [MUST] Remove setRuleMode from VpnClient interface, all implementations, and store
  - [MUST] Remove VpnConfig.wireUrl — replace with full ClientConfig return type
  - [SHOULD] Extract buildConfig to a shared util if used outside Dashboard
  - [SHOULD] Add VpnPreferences type to store for future extensibility

**Acceptance**: AC1, AC2, AC19, AC22

**Knowledge**:
- `docs/knowledge/architecture-decisions.md` → "VpnClient Abstraction Pattern" (DI pattern, factory)
- `docs/knowledge/architecture-decisions.md` → "NativeVpnClient Mobile Bridge" (plugin injection pattern)
- `docs/knowledge/testing-patterns.md` → webapp mock pattern (createVpnClient(mock) in tests)

---

## Execution Summary

```
F1 (Go core: config, daemon, mobile, CLI)
  │   13 Go tests, 6 files in k2/ submodule
  │
  └── T2 (K2Plugin: Swift, Kotlin, TS defs)
        │   tsc --noEmit + manual device tests, 3 files in mobile/plugins/
        │
        └── T3 (Webapp: types, clients, store, Dashboard)
              │   9 webapp tests, 6 files in webapp/src/
              │
              Done. Total: 15 files, 22+ automated tests.
```

Critical path: F1 → T2 → T3 (sequential, recommend single-branch execution)
Parallel opportunities: none (each task depends on the previous)
