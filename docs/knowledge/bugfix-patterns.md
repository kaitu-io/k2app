# Bugfix Patterns

Issues discovered during implementation and their fixes.

---

## TypeScript Strict Array Access Requires Non-Null Assertion (2026-02-14, k2app-rewrite)

**Problem**: `noUncheckedIndexedAccess` causes `TS2532: Object is possibly 'undefined'` on array element access.

**Fix**: Non-null assertion `!` after length check: `if (arr.length > 0) arr[0]!.id`. Never use on unvalidated user input or API response arrays.

**Files fixed**: (original files deleted in webapp v2 migration — pattern still applies to any `noUncheckedIndexedAccess` project)

**Validation**: `npx tsc --noEmit` passes.

---

## Missing Peer Dependencies in Yarn Workspaces (2026-02-14, k2app-rewrite)

**Problem**: `@testing-library/react` requires `@testing-library/dom` as peer. Parallel agents installing in isolated worktrees left combined `node_modules` stale after merge.

**Fix**: `yarn install` from workspace root after merging branches that modify `package.json`.

**Rule**: Always run `yarn install` from root after merge.

---

## Unused Import Causes TS6133 After Merge (2026-02-14, k2app-rewrite)

**Problem**: Branch W1 imported `vi` for spy functionality. Branch W3 refactored tests, making `vi` unused. Merge created unused import.

**Fix**: Remove unused import.

**Prevention**: Run `tsc --noEmit` after every merge as delivery gate.

---

## Go→JS JSON Key Mismatch Causes Silent Data Loss (2026-02-14, mobile-rewrite)

**Problem**: Go `json.Marshal` outputs `connected_at`, TypeScript type declares `connectedAt`. Value exists under wrong key — `status.connectedAt === undefined` with no runtime error.

**Discovery**: Code review compared Go `Engine.StatusJSON()` output keys against `K2PluginInterface` TypeScript definitions.

**Fix**: Added `remapStatusKeys()` in K2Plugin.swift and K2Plugin.kt to transform snake_case → camelCase at native bridge.

**Prevention**: CLAUDE.md convention — always remap Go JSON keys at native bridge boundary.

**Cross-reference**: See Architecture Decisions → "Go→JS JSON Key Remapping" for the architectural decision.

---

## Overbroad .gitignore Silently Hides Source Files (2026-02-14, mobile-rewrite)

**Problem**: `.gitignore` patterns `mobile/ios/` and `mobile/android/` ignored all files, including source. Agent-created Swift/Kotlin files invisible to git — completely silent failure.

**Discovery**: Code review ran `git check-ignore` on source files.

**Fix**: Replaced directory patterns with targeted build artifact patterns (Pods/, build/, .gradle/).

**Prevention**: CLAUDE.md convention — never ignore entire source directories.

---

## Missing Info.plist Causes Runtime Failures in iOS App (2026-02-14, mobile-rewrite)

**Problem**: K2Plugin.swift calls `Bundle.main.infoDictionary?["CFBundleShortVersionString"]` — returns `nil` without Info.plist, version shows as "unknown".

**Root cause**: iOS agent created PacketTunnelExtension's Info.plist but not the main app target's.

**Fix**: Created `mobile/ios/App/App/Info.plist` with all required keys.

**Prevention**: When creating iOS targets, verify both main app and extension targets have their own Info.plist files.

---

## gomobile bind Fails Without Pre-Created Output Directory (2026-02-14, mobile-rewrite)

**Problem**: `gomobile bind` fails if the output directory (`k2/build/`) doesn't exist.

**Symptom**: `mkdir: no such file or directory` during mobile build.

**Fix**: Makefile creates `k2/build/` before running `gomobile bind`. See Makefile targets `build-mobile-ios` and `build-mobile-android`.

**Prevention**: Always create output directories before tool invocations that expect them.

---

## Capacitor registerPlugin vs npm Dynamic Import (2026-02-16, android-aar-fix)

**Problem**: `import('k2-plugin')` fails at runtime in Capacitor WebView with "Failed to resolve module specifier 'k2-plugin'". The npm package is a Capacitor plugin — it registers a native bridge, not a standard JS module. WebView can't resolve npm package names.

**Fix**: Use `registerPlugin('K2Plugin')` from `@capacitor/core` directly. The native plugin is registered by Capacitor's native loader; JS side just needs to call `registerPlugin` with the plugin name.

```typescript
// BROKEN: dynamic npm import doesn't work in WebView
const { K2Plugin } = await import(/* @vite-ignore */ 'k2-plugin');

// CORRECT: Capacitor's own plugin registration
const { registerPlugin } = await import('@capacitor/core');
const K2Plugin = registerPlugin('K2Plugin');
```

**Root cause**: Capacitor plugins are not standard ES modules. The npm package provides type definitions and native bridge setup, but the actual plugin object must be obtained via `registerPlugin()`.

**Files fixed**: (original `webapp/src/vpn-client/index.ts` deleted in webapp v2 — pattern applies to any Capacitor plugin loading)

---

## gomobile Swift API Uses Throws Pattern, Not NSError Out-Parameter (2026-02-16, android-aar-fix)

**Problem**: PacketTunnelProvider.swift called gomobile-generated Engine methods with NSError out-parameter pattern (`engine?.start(wireUrl, fd: fd, error: &startError)`). Compile error.

**Root cause**: gomobile generates ObjC API as `start:fd:error:` → Swift bridges this as `start(_:fd:) throws`, NOT as an error out-parameter. Swift's ObjC bridging automatically converts methods that take `NSError**` out-parameter into throwing methods.

**Fix**: Use `try engine?.start(wireUrl, fd: Int(fd))` with `do/catch`. Same for `stop()`: `try? engine?.stop()`.

**Files fixed**: `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift`

---

## iOS Extension Info.plist Must Have Bundle Metadata Keys (2026-02-16, android-aar-fix)

**Problem**: PacketTunnelExtension.appex installed to iPhone failed with "missing or invalid CFBundleExecutable in its Info.plist", then "does not have a CFBundleVersion key".

**Root cause**: Extension's Info.plist only had `NSExtension`, `CFBundleDisplayName`, `CFBundleIdentifier`. Missing: `CFBundleExecutable`, `CFBundleName`, `CFBundleShortVersionString`, `CFBundleVersion`, `CFBundlePackageType`. Xcode's `GENERATE_INFOPLIST_FILE = NO` means these aren't auto-injected.

**Fix**: Added all required keys with build-setting variables (`$(EXECUTABLE_NAME)`, `$(MARKETING_VERSION)`, etc.). Also had to add `CURRENT_PROJECT_VERSION` and `MARKETING_VERSION` to the extension target's build settings — these aren't inherited from the project.

**Prevention**: When creating iOS extension targets with custom Info.plist, always include the 5 standard `CFBundle*` keys. Verify with: `plutil -lint` and test install on real device.

**Files fixed**: `mobile/ios/App/PacketTunnelExtension/Info.plist`, `App.xcodeproj/project.pbxproj`

---

## Android VPN establish() Returns Null Silently (2026-02-16, mobile-debug)

**Problem**: `VpnService.Builder().establish()` returns `null` instead of a TUN file descriptor. Original code `val fd = vpnInterface?.fd ?: return` silently returned — no error to JS, no logcat, no user feedback. VPN appeared to "do nothing".

**Root cause (1)**: `VpnService.prepare()` was called with Application context (`Plugin.context`) instead of Activity context. On Android 15 (API 35), `prepare()` returns `null` (appears "already prepared") even when the VPN subsystem hasn't properly registered the app as a VPN provider. `establish()` then returns `null`.

**Root cause (2)**: Capacitor `file:` protocol local plugins (`"k2-plugin": "file:./plugins/k2-plugin"`) are copied to `node_modules/` at `yarn install` time. Editing source doesn't update the copy. `cap sync` reads from `node_modules/`, not source. Multiple deploy cycles ran with stale code.

**Fix**:
1. Use `VpnService.prepare(activity)` (Activity context from `Plugin.getActivity()`), not `VpnService.prepare(context)` (Application context)
2. Handle `establish()` null: report error via `plugin?.onError()` and call `stopVpn()`, never silently return
3. After editing local Capacitor plugin source: `rm -rf node_modules/k2-plugin && yarn install --force` before `cap sync`

**Files fixed**: `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt`, `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt`

**Validating tests**: Manual device testing — debug.html connect flow produces `vpnStateChange: "connecting"` events; Go engine starts and rejects invalid wireUrl with proper error propagation.

---

## Missing Tauri IPC Handler Registration Causes White Screen (2026-02-17, tauri-desktop-bridge)

**Problem**: App opens to white screen. No error visible in the window. Console shows `[WebApp] Failed to initialize: ...` but React never renders.

**Root cause**: `service.rs` defined 3 `#[tauri::command]` functions (`daemon_exec`, `get_udid`, `get_platform_info`) but `main.rs` `invoke_handler` only registered 2 of the 5 existing commands. The 3 new commands were never added to `tauri::generate_handler![]`.

**White screen chain**:
1. `main.tsx` detects `window.__TAURI__` → calls `injectTauriGlobals()`
2. First line: `await invoke('get_platform_info')` — Tauri rejects (unregistered command)
3. Error propagates to `main()` catch block — logs error but never calls `ReactDOM.render()`
4. White screen

**Companion issue**: `tauri-plugin-shell` had `Cargo.toml` dependency but no `.plugin()` registration in builder chain. `openExternal()` would fail at runtime.

**Fix**: Added all 3 commands to `invoke_handler` + registered `tauri_plugin_shell::init()`, `tauri_plugin_autostart::init()` in `main.rs`. (`tauri-plugin-http` was later removed entirely — see Framework Gotchas → "@tauri-apps/plugin-http Static Import Freezes WebKit JS Engine")

**Prevention**: When adding `#[tauri::command]` functions in Rust, immediately add them to `invoke_handler` in `main.rs`. When adding plugin dependencies to `Cargo.toml`, immediately add `.plugin()` to builder chain.

**Files fixed**: `desktop/src-tauri/src/main.rs`

**Validating tests**: `cargo check` passes; runtime verification — app renders after fix.

---

## VPN State Contract Mismatch: Daemon "stopped" vs Webapp "disconnected" (2026-02-17, vpn-error-reconnect)

**Problem**: Daemon returns `state: "stopped"` but webapp's `ServiceState` TypeScript type only has `"disconnected"`, `"connecting"`, `"connected"`, `"reconnecting"`, `"error"`, `"disconnecting"`. Tauri bridge was a pass-through — `"stopped"` arrived in the webapp store, making all derived booleans wrong:
- `isDisconnected = (state === "disconnected")` → always false when daemon sends `"stopped"`
- `isError` → never triggered
- `handleToggleConnection` disabled guard → broken (button always enabled)
- App "worked" only because `else`/`default` branches accidentally handled the unknown value

**Discovery**: Spec authoring for vpn-error-reconnect feature — code audit of bridge layers vs webapp type definitions revealed the mismatch.

**Fix**: Added `transformStatus()` to `tauri-k2.ts` that maps `"stopped"` → `"disconnected"` before returning from `run('status')`. This is a semantic correction, not a behavior change — the app was already rendering disconnected UI via else-branches.

**Why the bug persisted**: Tauri bridge had no transformStatus function (was pure pass-through). Tests mocked the IPC and didn't verify state normalization. The else-branch fallback made desktop "appear functional" while masking the underlying type contract violation.

**Files fixed**: `webapp/src/services/tauri-k2.ts`

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` — `test_status_stopped_normalized_to_disconnected`

---

## VPN Error State Never Synthesized — isError Always False (2026-02-17, vpn-error-reconnect)

**Problem**: When engine fails to connect, it returns `{ state: "disconnected", error: "some error string" }`. Both Tauri and Capacitor bridges passed this through without synthesizing `state: "error"`. Result: `useVPNStatus().isError` was always false on all platforms. Error state was completely invisible in the UI — the ConnectionButton never showed error styling, users got no feedback on connection failure.

**Root cause (Tauri bridge)**: No `transformStatus()` at all — raw daemon response passed directly to webapp.

**Root cause (Capacitor bridge)**: Had partial `transformStatus()` but missing the key synthesis step: `if (state === 'disconnected' && error) { state = 'error'; }`. The `let state` declaration was typed as `const` in the original, preventing reassignment.

**Fix**:
- Tauri: `transformStatus()` synthesizes `state = "error"` when `state === "disconnected" && raw.error`
- Capacitor: Changed `const state` to `let state`, added `if (state === 'disconnected') { state = 'error'; }` after error is detected

**Error lifecycle**: `_k2.run('up')` → engine enters `"connecting"` → state not `"disconnected"` → error auto-clears. `_k2.run('down')` → engine clears `lastError` → `state = "disconnected"`, no error → normal. Wire self-heal success → `"connected"` → error gone. Wire self-heal fail → `"disconnected" + lastError` → bridge synthesizes `"error"`.

**Files fixed**: `webapp/src/services/tauri-k2.ts`, `webapp/src/services/capacitor-k2.ts`

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` — `test_status_disconnected_with_error_synthesizes_error_state`; `webapp/src/services/__tests__/capacitor-k2.test.ts` — `test_status_disconnected_with_error_synthesizes_error_state`

---

## Dashboard handleToggleConnection Missing Error Branch (2026-02-17, vpn-error-reconnect)

**Problem**: `handleToggleConnection` in `Dashboard.tsx` had no `isError` branch. When engine state was `"error"`, the function fell through to the `else if (!isDisconnected)` branch (which is true when state is `"error"`), triggering `_k2.run('down')` — trying to disconnect an already-disconnected engine. Result: clicking the error button attempted to disconnect instead of reconnect.

**Fix**: Added explicit error branch before the `else if (!isDisconnected)` check:
```ts
if (isError && !isRetrying) {
  // Error state: engine already disconnected, reconnect directly
  setOptimisticState('connecting');
  const config = assembleConfig();
  await window._k2.run('up', config);
} else if (!isDisconnected) {
  // Connected/connecting: disconnect
  ...
}
```
Also fixed the guard condition: `(isDisconnected || isError) && !activeTunnelInfo.domain` — error state requires tunnel selection before reconnect, same as disconnected.

**Config assembly extracted**: `assembleConfig()` helper extracted from inline connect logic to be reusable in both the `isError` branch and the `isDisconnected` branch.

**Files fixed**: `webapp/src/pages/Dashboard.tsx`

**Validating tests**: `webapp/src/pages/__tests__/Dashboard.test.tsx` — `test_error_state_reconnect_calls_up`, `test_error_state_guard_no_tunnel_selected`

---

## Tauri v2 Event Listener Silent Failure: Wrong Capability Name (2026-02-18, tauri-updater-and-logs)

**Problem**: `listen('update-ready', callback)` from `@tauri-apps/api/event` registered without error but never fired. Rust side emitted the event successfully (`app.emit("update-ready", payload)` returned `Ok(())`). Frontend callback never called.

**Root cause**: `capabilities/default.json` had `"event:default"` instead of `"core:event:default"`. Tauri v2's event system is part of the `core` plugin namespace. Without the `core:` prefix, the permission wasn't recognized, and event delivery was silently blocked.

**Discovery**: Added `console.log` in the listen callback — never printed. Verified Rust emit returned Ok. Compared capability names against Tauri v2 docs and found the `core:` prefix requirement.

**Fix**: Changed `"event:default"` to `"core:event:default"` in `desktop/src-tauri/capabilities/default.json`.

**Why silent**: Tauri v2 does not warn at build time or runtime about unrecognized capability names. The permission simply doesn't match, and event delivery is blocked without any error. `listen()` returns a valid unlisten function regardless.

**Prevention**: When adding Tauri event listeners (`listen`, `emit`), verify `"core:event:default"` is in `capabilities/default.json`. For plugin-specific events, use the plugin's namespace (e.g., `"updater:default"` for updater plugin events).

**Cross-reference**: See Framework Gotchas → "Tauri v2 Event Capability: core:event:default, Not event:default"

**Files fixed**: `desktop/src-tauri/capabilities/default.json`

**Validating tests**: Runtime verification — listen callback fires after fix.

---

## Go Unexported Function Inaccessible Across Packages (2026-02-18, structured-error-codes)

**Problem**: Initial plan named the error classification function `classifyError()` (lowercase = unexported). The daemon package (`k2/daemon/`) needed to call it, but Go's visibility rules prevent cross-package access to unexported names.

**Symptom**: `daemon.go` references `engine.classifyError` → compile error: `cannot refer to unexported name engine.classifyError`.

**Fix**: Rename to `ClassifyError()` (exported). Function is pure classification logic with no sensitive state — exporting is appropriate.

**Prevention**: When a function in package A will be called from package B, name it exported from the start. Check call-site packages before finalizing function names in plan documents.

**Files fixed**: `k2/engine/error.go` — `classifyError` → `ClassifyError`

**Validating tests**: `k2/engine/error_test.go` (22 tests), `k2/daemon/daemon_test.go` (3 tests)

---

## GORM Soft Delete: Use Delete() Not Manual Status Field (2026-02-18, cloud-worker-fix)

**Problem**: `markOrphanedInstances()` in `worker_cloud.go` manually set `status = "deleted"` via `Updates(map[string]any{"status": "deleted"})` and filtered with `WHERE status != 'deleted'`. But the `CloudInstance` model already has `DeletedAt gorm.DeletedAt` — GORM's built-in soft delete mechanism.

**Why this was wrong**:
1. `gorm.DeletedAt` makes GORM auto-add `WHERE deleted_at IS NULL` to all queries — the manual `WHERE status != 'deleted'` was redundant
2. Manual `status = "deleted"` set a string field but didn't populate `deleted_at` — GORM soft-deleted records still appeared in queries (double identity: status says deleted, but `deleted_at` is NULL so GORM includes them)
3. Admin list APIs using `db.Find(&instances)` would show "deleted" instances because GORM only auto-filters on `deleted_at`, not on a custom `status` field

**Fix**: Replace `Updates(status: "deleted")` with `db.Delete(&dbInst)`. Remove redundant `WHERE status != 'deleted'` filter. Separate `last_synced_at` update from the delete (two operations — update timestamp, then soft-delete).

**Rule**: When a GORM model has `DeletedAt gorm.DeletedAt`, always use `db.Delete()` for soft delete. Never manually track deletion state in a separate status field — it creates conflicting sources of truth.

**Files fixed**: `api/worker_cloud.go`

**Validating tests**: `go test ./...` passes; manual verification of cloud sync worker behavior.

---

## Login Requests Must Include Device UDID (2026-02-18, login-udid-fix)

**Problem**: `EmailLoginForm.tsx` and `LoginDialog.tsx` sent login requests without `udid` field. The backend API declares `UDID string binding:"required"` — the field is mandatory for device association, token generation, and device limit enforcement.

**Impact**: Without UDID, the backend would either reject the request (400) or create a device record with an empty UDID, breaking device management (logout, device list, device limit).

**Fix**: Call `window._platform!.getUdid()` before each login request and include the result as `udid` in the POST body. Applied to all three login paths:
1. Email + verification code login (`EmailLoginForm`)
2. Email + password login (`EmailLoginForm`)
3. Verification code login in `LoginDialog`

**Why `!` assertion is safe**: Login UI is only reachable after platform injection completes (`main.tsx` bootstrap). `_platform` is guaranteed non-null at this point.

**Files fixed**: `webapp/src/components/EmailLoginForm.tsx`, `webapp/src/components/LoginDialog.tsx`

**Validating tests**: Manual login flow verification on all platforms.

---

## OpenWrt init.d Used Client Mode Instead of Daemon Mode (2026-02-18, openwrt-docker-testing)

**Problem**: `scripts/openwrt/k2.init` ran `k2 run -c /etc/k2/config.yaml` which triggers **foreground client mode** — direct tunnel connection with no HTTP server. The embedded webapp never starts, so LuCI iframe (`http://127.0.0.1:1777`) shows nothing. Users can't configure or control VPN via browser.

**Root cause**: `k2 run` has two modes:
- `-c config.yaml` → `runClientForeground()` — direct engine connection, no daemon HTTP server
- `-l 0.0.0.0:1777` → `runDaemon()` — HTTP API + embedded webapp + waits for user interaction

The init.d script was written for `-c` (client foreground), but OpenWrt needs daemon mode for the web UI.

**Fix**: Changed `procd_set_param command /usr/bin/k2 run -c /etc/k2/config.yaml` to `procd_set_param command /usr/bin/k2 run -l 0.0.0.0:1777`.

**Discovery**: Found during Docker smoke testing — attempted to run daemon and verify webapp serving, realized the init.d entry point was wrong.

**Files fixed**: `scripts/openwrt/k2.init`

**Validating tests**: `scripts/test-openwrt.sh` — webapp serves HTML at `/` confirms daemon mode works.

---

## Capacitor Local Plugin Stale Copy in node_modules (2026-02-16, mobile-debug)

**Problem**: Capacitor plugin declared as `"k2-plugin": "file:./plugins/k2-plugin"` in `mobile/package.json` is copied (not symlinked) to `node_modules/k2-plugin/`. Editing source files in `mobile/plugins/k2-plugin/` has no effect — `cap sync` and Gradle build use the stale `node_modules/` copy.

**Symptom**: Code changes don't take effect after rebuild. No error message. `yarn install` says "Already up-to-date" even when source files have changed. Can waste multiple deploy cycles debugging phantom issues.

**Fix**: After editing local plugin source, always run:
```bash
rm -rf node_modules/k2-plugin && yarn install --force
```
Then `npx cap sync android` and rebuild.

**Why it happens**: Yarn `file:` protocol copies files at install time and caches the result. It doesn't detect changes to source files — only `package.json` version changes trigger re-copy.

**Alternative**: Consider `"k2-plugin": "link:./plugins/k2-plugin"` (symlink) instead of `file:` (copy) — but `link:` has its own Capacitor compatibility issues.

**Prevention**: Add to CLAUDE.md as a convention.

---

## Service Label Mismatch: io.kaitu.k2 vs kaitu (2026-02-22, k2-cli-redesign)

**Problem**: Initial k2 CLI redesign used `io.kaitu.k2` as the launchd service label. But production kaitu-service already uses label `kaitu` with plist at `/Library/LaunchDaemons/kaitu.plist`. New label would create a second service instead of replacing the old one.

**Discovery**: UAT testing on local machine — `sudo k2 service install` created `/Library/LaunchDaemons/io.kaitu.k2.plist` alongside existing `kaitu.plist`. Two services running simultaneously.

**Fix**: Changed `serviceLabel` from `"io.kaitu.k2"` to `"kaitu"` in `k2/daemon/service_darwin.go`. Added `launchctl unload` before plist write to ensure clean overwrite.

**Prevention**: When replacing an existing service, use the SAME label/name. Check production environment for existing service identity before choosing a label.

**Cross-reference**: See Architecture Decisions → "Service Label Unification"

---

## sing-tun Missing tunIf.Start() — TUN Created But No Routes Installed (2026-02-22, sing-tun-lifecycle-fix)

**Problem**: VPN connects successfully (utun8 allocated, engine state `"connected"`) but no traffic flows through the tunnel. DNS queries bypass the TUN — youtube.com resolves to a Facebook IP via poisoned DNS (114.114.114.114), while bing.com works (not poisoned in China).

**Root cause**: sing-tun's correct lifecycle is `tun.New() → tunIf.Start() → tun.NewSystem() → stack.Start()`. Our code skipped `tunIf.Start()`. The `Tun.Start()` method is responsible for:
1. Installing AutoRoute entries into the OS routing table (the `/1` split routes)
2. Calling `InterfaceMonitor.RegisterMyInterface()` for TUN self-exclusion
3. Flushing DNS cache (macOS)

Without `Start()`, the TUN device exists (visible in `ifconfig`) but the routing table has zero routes through it. All traffic continues via the default interface (en0).

**Discovery**: `netstat -rn | grep utun` showed no routes through utun8 despite successful connection. Compared our lifecycle against sing-box source code (the canonical sing-tun consumer).

**Fix**: Added `tunIf.Start()` between `tun.New()` and `tun.NewSystem()` in `k2/provider/tun_desktop.go`:
```go
tunIf, err := tun.New(tunOpts)
if err != nil { return fmt.Errorf("provider: create TUN: %w", err) }

if err := tunIf.Start(); err != nil {
    tunIf.Close()
    return fmt.Errorf("provider: start TUN: %w", err)
}

stack, err := tun.NewSystem(tun.StackOptions{...})
```

**Why the bug persisted**: The TUN device was created and traffic was captured at the IP level (gVisor stack running), but without routing table entries, no traffic was directed to the TUN in the first place. The engine reported `"connected"` because all Go-level initialization succeeded — routing is an OS-level side effect of `Start()`.

**Secondary fix**: Replaced 30-line `nextAvailableUtun()` with `tun.CalculateInterfaceName("")` — sing-tun already provides this exact functionality.

**iOS/Android NOT affected**: Mobile providers don't call `tunIf.Start()` — the OS manages routes. `Start()` calls `InterfaceMonitor.RegisterMyInterface()` which panics if the monitor is nil (mobile doesn't set one).

**Files fixed**: `k2/provider/tun_desktop.go`, `k2/provider/tun_desktop_test.go`

**Validating tests**: `go test ./provider/ -v` — `TestDefaultTunName` passes. Live verification: `netstat -rn | grep utun` shows AutoRoute entries after reconnect.

---

## Over-Designed Include Directive Deleted (2026-02-22, k2-cli-redesign)

**Problem**: k2 config spec initially included an `Include` field for nginx-style config inclusion (`include /path/to/extra.conf`). Fully implemented with `ResolveInclude()` function, recursive resolution, and cycle detection.

**Why deleted**: k2 configs are simple (~10 lines). Include is a complexity multiplier with no current use case. User identified it as YAGNI: "本身我们的设置是干净利索的，没有太多内容，include 这个设计过重了。完全删除"

**Lesson**: For simple config formats, resist the urge to add "power user" features. If the config file is small enough to read in one screen, include/import mechanics add complexity without value.

**Files removed**: `k2/config/include.go`, `k2/config/include_test.go`. `Include` field removed from both `ClientConfig` and `ServerConfig` structs.

---
