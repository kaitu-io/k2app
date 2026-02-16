# Bugfix Patterns

Issues discovered during implementation and their fixes.

---

## TypeScript Strict Array Access Requires Non-Null Assertion (2026-02-14, k2app-rewrite)

**Problem**: `noUncheckedIndexedAccess` causes `TS2532: Object is possibly 'undefined'` on array element access.

**Fix**: Non-null assertion `!` after length check: `if (arr.length > 0) arr[0]!.id`. Never use on unvalidated user input or API response arrays.

**Files fixed**: `webapp/src/stores/servers.store.ts`, `webapp/src/components/__tests__/ServerList.test.tsx`

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

**Files fixed**: `webapp/src/vpn-client/index.ts`

---

## Async VpnClient Bootstrap Required on Mobile (2026-02-16, android-aar-fix)

**Problem**: Webapp rendered before `initVpnClient()` completed. Zustand stores called `getVpnClient()` during render, throwing "VpnClient not initialized".

**Symptom**: Blank screen on mobile with console error "VpnClient not initialized". Desktop worked because `createVpnClient()` (sync) was used.

**Fix**: `main.tsx` now awaits `initVpnClient()` before calling `ReactDOM.createRoot().render()`. This is required because mobile's `initVpnClient()` uses dynamic imports (`NativeVpnClient`, `@capacitor/core`) which are async.

**Files fixed**: `webapp/src/main.tsx`

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
