# Plan: config-driven-connect v2

**Spec**: `docs/features/config-driven-connect.md` (v2 section)
**Complexity**: Simple (<5 webapp files, no refactoring, sequential dependency)
**Scope**: Webapp-only. Go daemon/CLI/mobile K2Plugin unchanged.

---

## T1: Foundation — ClientConfig type + configStore + init wiring

**Branch**: `w9f/config-v2-store`
**Depends**: none

### Scope

| Action | File | Details |
|--------|------|---------|
| NEW | `webapp/src/types/client-config.ts` | `ClientConfig` interface + `CLIENT_CONFIG_DEFAULTS` |
| NEW | `webapp/src/stores/config.store.ts` | Zustand store: loadConfig, updateConfig, buildConnectConfig, getters |
| NEW | `webapp/src/stores/__tests__/config.store.test.ts` | Unit tests |
| CHANGE | `webapp/src/stores/index.ts` | Export useConfigStore, add configStore.loadConfig() to initializeAllStores() |

### TDD Steps

#### RED — Write failing tests first

Test file: `webapp/src/stores/__tests__/config.store.test.ts`

```
test_loadConfig_from_empty_storage
  — mock _platform.storage.get returns null → config = {}, loaded = true

test_loadConfig_from_existing_storage
  — mock _platform.storage.get returns { rule: { global: true }, dns_mode: 'real-ip' }
  → config matches stored value, loaded = true

test_updateConfig_deep_merges_and_persists
  — start with { rule: { global: false } }
  → updateConfig({ dns_mode: 'real-ip' })
  → config = { rule: { global: false }, dns_mode: 'real-ip' }
  → verify _platform.storage.set called with merged config

test_updateConfig_nested_merge_preserves_siblings
  — start with { rule: { global: true }, mode: 'tun' }
  → updateConfig({ rule: { global: false } })
  → config.mode still 'tun'

test_buildConnectConfig_merges_defaults_stored_server
  — stored config = { rule: { global: true } }
  → buildConnectConfig('k2v5://example')
  → result has: server='k2v5://example', rule.global=true, mode='tun', dns_mode='fake-ip', log.level='info'

test_buildConnectConfig_without_serverUrl_uses_stored
  — stored config = { server: 'k2v5://saved', rule: { global: false } }
  → buildConnectConfig() (no arg)
  → result.server = 'k2v5://saved'

test_getter_ruleMode_returns_chnroute_by_default
  — config = {} → ruleMode = 'chnroute'

test_getter_ruleMode_returns_global_when_set
  — config = { rule: { global: true } } → ruleMode = 'global'

test_getter_dnsMode_returns_default
  — config = {} → dnsMode = 'fake-ip'

test_getter_mode_returns_default
  — config = {} → mode = 'tun'

test_getter_logLevel_returns_default
  — config = {} → logLevel = 'info'

test_initializeAllStores_calls_loadConfig
  — verify configStore.loadConfig() is called during initializeAllStores()
  — verify call order: layoutStore → configStore → authStore → vpnStore
```

#### GREEN — Implement to pass tests

1. Create `webapp/src/types/client-config.ts`:
   - `ClientConfig` interface (server, mode, rule, dns_mode, log)
   - `CLIENT_CONFIG_DEFAULTS` constant

2. Create `webapp/src/stores/config.store.ts`:
   - Zustand store with `create()`
   - `loadConfig()`: `_platform.storage.get('k2.vpn.config')` → set state
   - `updateConfig(partial)`: deep merge → set state → `_platform.storage.set()` (fire-and-forget)
   - `buildConnectConfig(serverUrl?)`: merge CLIENT_CONFIG_DEFAULTS + stored config + optional serverUrl
   - Computed getters: `ruleMode`, `dnsMode`, `mode`, `logLevel` (with default fallbacks)

3. Update `webapp/src/stores/index.ts`:
   - Add configStore exports
   - Add `configStore.loadConfig()` to `initializeAllStores()` between layout and auth

#### REFACTOR

- [SHOULD] Verify deep merge utility handles nested objects correctly (no lodash needed — simple 1-level nesting)
- [SHOULD] Verify getters use Zustand selector pattern for React re-render efficiency

### AC Coverage

| AC | Test |
|----|------|
| AC25 | test_loadConfig_from_empty_storage (store exists, Zustand) |
| AC26 | test_loadConfig_from_existing_storage (_platform.storage.get) |
| AC27 | test_updateConfig_deep_merges_and_persists |
| AC28 | test_buildConnectConfig_merges_defaults_stored_server |
| AC29 | test_getter_ruleMode_returns_*, test_getter_dnsMode_*, test_getter_mode_*, test_getter_logLevel_* |
| AC30 | test_updateConfig_deep_merges_and_persists (verifies storage key) |
| AC32 | test_updateConfig_deep_merges_and_persists (auto-persist) |
| AC33 | By design — all platforms use same _platform.storage API |
| AC40 | test_initializeAllStores_calls_loadConfig |
| AC44 | By design — no persist middleware used |
| AC45 | By design — single file client-config.ts |

---

## T2: Feature — Dashboard UI rewiring + new controls + i18n

**Branch**: `w9f/config-v2-dashboard`
**Depends**: T1

### Scope

| Action | File | Details |
|--------|------|---------|
| CHANGE | `webapp/src/pages/Dashboard.tsx` | Remove useState(activeRuleType/activeDnsMode), remove assembleConfig(), use configStore. Add mode toggle + log level select. Remove anonymity toggle (unsupported). |
| ADD | `webapp/src/i18n/locales/zh-CN/dashboard.json` | New keys: proxyModeOptions, logLevel, logLevelDescription, logLevelOptions |
| ADD | `webapp/src/i18n/locales/en-US/dashboard.json` | Same keys (English) |

### TDD Steps

#### RED — Write failing tests first

Test file: `webapp/src/stores/__tests__/config.store.test.ts` (extend T1 test file)

```
test_dashboard_connect_calls_buildConnectConfig_with_server
  — simulate: configStore has { rule: { global: true } }
  → buildConnectConfig('k2v5://newserver')
  → verify result contains server + stored preferences

test_dashboard_connect_success_saves_server
  — updateConfig({ server: 'k2v5://connected' })
  → verify _platform.storage.set called with server in config
```

> Note: Dashboard component tests are lightweight — the real logic is in configStore (tested in T1).
> Dashboard changes are primarily wiring (remove useState, add configStore hook, add MUI controls).
> Manual verification covers the UI integration.

#### GREEN — Implement Dashboard changes

1. **Remove volatile state**:
   - Delete `useState(activeRuleType)` and `useState(activeDnsMode)`
   - Delete `assembleConfig()` callback
   - Delete `handleRuleTypeChange` local handler
   - Import `useConfigStore` from stores

2. **Wire existing controls to configStore**:
   - Rule mode toggle: read `configStore.ruleMode`, write via `updateConfig({ rule: { global } })`
   - DNS mode toggle: read `configStore.dnsMode`, write via `updateConfig({ dns_mode })`

3. **Add new controls in Advanced Settings section**:
   - **Mode toggle** (TUN/Proxy): `ToggleButtonGroup` reading `configStore.mode`
     - Place before DNS mode toggle (more fundamental setting)
     - Disabled when VPN running (same pattern as rule toggle)
   - **Log level select**: MUI `Select` reading `configStore.logLevel`
     - Options: debug, info, warn, error
     - Place at bottom of K2 Advanced Options
     - Always enabled (can change while connected for next session)

4. **Remove anonymity toggle** (lines 445-465):
   - The Switch was always `checked={false}`, handler was no-op
   - Daemon doesn't support this feature
   - Remove associated experimental chip and description

5. **Wire connect button**:
   - Replace `assembleConfig()` call with `configStore.buildConnectConfig(selectedCloudTunnel?.url)`
   - On connect success: `configStore.updateConfig({ server: selectedCloudTunnel?.url })`

6. **Add i18n keys** (zh-CN first, then en-US):
   ```json
   "proxyModeLabel": "代理模式",
   "proxyModeDescription": "选择网络代理方式",
   "proxyModeOptions": { "tun": "TUN 模式", "proxy": "代理模式" },
   "logLevel": "日志级别",
   "logLevelDescription": "调试时可设置为 Debug 查看详细日志",
   "logLevelOptions": { "debug": "Debug", "info": "Info", "warn": "Warn", "error": "Error" }
   ```

#### REFACTOR

- [MUST] Verify no `useState` remains for config-related state (AC43)
- [SHOULD] Remove unused imports (useState if no longer needed, getCurrentAppConfig if proxyRule.defaultValue no longer referenced)
- [SHOULD] Verify `assembleConfig` is fully deleted (grep)

### AC Coverage

| AC | Test / Verification |
|----|---------------------|
| AC31 | Manual: change rule → reload → verify persisted |
| AC34 | Verified by: rule toggle reads configStore.ruleMode (code inspection + manual) |
| AC35 | Verified by: DNS toggle reads configStore.dnsMode |
| AC36 | Verified by: new Mode ToggleButtonGroup renders |
| AC37 | Verified by: new log level Select renders |
| AC38 | Verified by: grep for assembleConfig returns 0 results |
| AC39 | Verified by: grep for activeRuleType/activeDnsMode returns 0 results |
| AC41 | test_dashboard_connect_success_saves_server |
| AC42 | By design: no status → configStore write path exists |
| AC43 | REFACTOR [MUST] check — grep for config-related useState |

---

## AC Coverage Matrix (complete)

| AC | Description | Task | Test/Verification |
|----|-------------|------|-------------------|
| AC25 | config.store.ts exists, Zustand | T1 | test_loadConfig_from_empty_storage |
| AC26 | loadConfig from _platform | T1 | test_loadConfig_from_existing_storage |
| AC27 | updateConfig deep merge + persist | T1 | test_updateConfig_deep_merges_and_persists |
| AC28 | buildConnectConfig merge | T1 | test_buildConnectConfig_merges_defaults_stored_server |
| AC29 | getter defaults | T1 | test_getter_* (4 tests) |
| AC30 | storage key k2.vpn.config | T1 | test_updateConfig (verifies key) |
| AC31 | persist across restart | T2 | Manual: change → reload → verify |
| AC32 | auto-persist on change | T1 | test_updateConfig (auto-save) |
| AC33 | all platforms same path | T1 | By design (_platform abstraction) |
| AC34 | rule toggle → configStore | T2 | Code inspection + manual |
| AC35 | DNS toggle → configStore | T2 | Code inspection + manual |
| AC36 | mode toggle UI | T2 | Manual: new control renders |
| AC37 | log level UI | T2 | Manual: new control renders |
| AC38 | assembleConfig deleted | T2 | grep verification |
| AC39 | useState deleted | T2 | grep verification |
| AC40 | loadConfig in init order | T1 | test_initializeAllStores_calls_loadConfig |
| AC41 | server saved on success | T2 | test_dashboard_connect_success_saves_server |
| AC42 | status no writeback | T2 | By design: no write path |
| AC43 | no config useState | T2 | REFACTOR [MUST] grep check |
| AC44 | no persist middleware | T1 | By design |
| AC45 | single type definition | T1 | By design: client-config.ts |

---

## Execution Notes

- **No worktree needed**: 2 sequential tasks, simple scope. Execute on current branch.
- **Task-splitting lesson applied**: Foundation (T1) completes before Feature (T2) — Dashboard imports from config.store.
- **i18n convention**: zh-CN first, en-US second. Other locales (ja, zh-TW, zh-HK, en-AU, en-GB) can be added later.
- **Deep merge**: Only 1-level nesting (rule.global, log.level). No need for lodash — spread + Object.assign suffices.
- **Anonymity toggle removal**: Cleaning up dead code (always false, no daemon support). Not tracked as separate AC since it's incidental cleanup.
