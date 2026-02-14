# Plan: Mobile Rewrite (iOS + Android)

## Meta

| Field | Value |
|-------|-------|
| Feature | mobile-rewrite |
| Spec | docs/features/mobile-rewrite/spec.md |
| Date | 2026-02-14 |
| Complexity | Complex (>20 files, 4 tech stacks: Go, Swift, Kotlin, TypeScript) |
| Scope | iOS + Android client. Capacitor + gomobile. |
| Prerequisites | webapp-state-alignment (must complete first) |

## Prerequisites

### k2 Submodule Changes (DONE)

k2 repo already contains the required changes (submodule at `bfeb06c`):

| Commit | Change | Status |
|--------|--------|--------|
| `aa63238` | Engine.StatusJSON() — rich status for cold start | **Done** |
| `bfeb06c` | Daemon 5→3 state simplification (stopped/connecting/connected) | **Done** |

### webapp-state-alignment (REQUIRED before mobile)

Webapp VpnState type must be aligned with k2's 3-state model before
NativeVpnClient can be implemented. See `docs/features/webapp-state-alignment/plan.md`.

Changes: Remove `'disconnecting'` and `'error'` from VpnState union.
Update store, ConnectionButton, and tests.

**Why first**: NativeVpnClient (T4) imports and uses VpnState type. The mobile
Engine uses `"disconnected"` as its idle state (vs daemon's `"stopped"`).
T4 must map this. If VpnState still has 5 states when T4 is built, the
mapping logic becomes confused and tests are wrong.

---

## AC Mapping

| AC | Test | Task |
|----|------|------|
| NativeVpnClient calls K2Plugin | vitest: NativeVpnClient.connect → K2Plugin.connect | T3 |
| createVpnClient returns NativeVpnClient on native | vitest: factory returns NativeVpnClient when Capacitor detected | T3 |
| All UI pages work on mobile | manual: same webapp dist loaded via Capacitor | T5 |
| K2Plugin checkReady returns ready:true | vitest plugin mock + manual native test | T1, T2 |
| K2Plugin getUDID returns device ID | manual: iOS identifierForVendor, Android ANDROID_ID | T1, T2 |
| K2Plugin connect starts VPN | manual: VPN tunnel established on device | T1, T2 |
| K2Plugin disconnect stops VPN | manual: VPN tunnel torn down | T1, T2 |
| K2Plugin getStatus returns state | vitest mock + manual: StatusJSON parsed correctly | T1, T2 |
| K2Plugin subscribe delivers events | manual: state_change events received in webapp | T1, T2 |
| PacketTunnelExtension starts Engine with fd | manual: iOS NE connects tunnel | T1 |
| handleAppMessage routes to StatusJSON | manual: sendProviderMessage returns JSON | T1 |
| NEVPNStatusDidChange propagated | manual: webapp receives state changes | T1 |
| App Group UserDefaults shared | manual: NE writes, app reads | T1 |
| Codesign valid | `codesign --verify --deep` in build script | T4 |
| App Store submission succeeds | manual: upload via Transporter | T5 |
| K2VpnService foreground notification | manual: notification shown during VPN | T2 |
| VpnService.establish provides fd | manual: Engine.Start receives valid fd | T2 |
| EventHandler → notifyListeners | manual: webapp receives events from Android | T2 |
| APK on arm64 + armv7 | manual: install and connect on both ABIs | T5 |
| VPN permission dialog on first connect | manual: Android shows VPN consent | T2 |
| gomobile bind iOS → xcframework | `make mobile-ios` succeeds in CI | T0 |
| gomobile bind Android → AAR | `make mobile-android` succeeds in CI | T0 |
| make build-mobile-ios → xcarchive | CI artifact produced | T4 |
| make build-mobile-android → signed APK | CI artifact produced | T4 |
| CI workflow succeeds | GitHub Actions green | T4 |
| Engine.StatusJSON() returns rich status | go test in k2/mobile/ | **Done in k2** |

## Dependency Graph

```
[webapp-state-alignment]  ←── must complete first
         │
         ▼
T0 (scaffold: Capacitor + K2Plugin defs + gomobile verify)
  │
  ├──→ T1 (iOS: K2Plugin Swift + PacketTunnelExtension)  ─┐
  ├──→ T2 (Android: K2Plugin Kotlin + K2VpnService)       ├──→ T4 (CI/CD + build) → T5 (E2E)
  └──→ T3 (webapp: NativeVpnClient + factory update)      ─┘
```

T1, T2, T3 are **parallel** — no file overlap:
- T1 touches `mobile/ios/**` + `mobile/plugins/k2-plugin/ios/**`
- T2 touches `mobile/android/**` + `mobile/plugins/k2-plugin/android/**`
- T3 touches `webapp/src/vpn-client/**`

---

## T0: Mobile Project Scaffold

**Scope**: Create `mobile/` workspace, Capacitor config, K2Plugin TypeScript
definitions, gomobile build verification, Makefile targets. After this task,
`gomobile bind` works and Capacitor project structure exists.

**Files**:
- `mobile/package.json`
- `mobile/capacitor.config.ts`
- `mobile/plugins/k2-plugin/package.json`
- `mobile/plugins/k2-plugin/src/definitions.ts`
- `mobile/plugins/k2-plugin/src/index.ts`
- `mobile/plugins/k2-plugin/src/web.ts`
- `package.json` (root — add `mobile` to workspaces)
- `Makefile` (add mobile targets)
- `.gitignore` (add mobile build artifacts)

**Depends on**: [webapp-state-alignment] (VpnState must be 3-state before plugin defs)

**Steps**:
1. Verify k2 submodule already at `bfeb06c` (StatusJSON + 3-state model)
2. Create `mobile/package.json` with Capacitor 6 deps + k2-plugin local ref
3. Create `mobile/capacitor.config.ts` (from spec)
4. Create K2Plugin TypeScript definitions:
   - `definitions.ts` — K2PluginInterface with all methods + events
   - `index.ts` — plugin registration (`registerPlugin('K2Plugin', ...)`)
   - `web.ts` — web stub (throws "not available on web")
5. Add root workspace: `"workspaces": ["webapp", "desktop", "mobile"]`
6. Add Makefile targets:
   ```makefile
   mobile-ios:
       cd k2 && gomobile bind -target=ios -o build/K2Mobile.xcframework ./mobile/
   mobile-android:
       cd k2 && gomobile bind -target=android -o build/k2mobile.aar -androidapi 24 ./mobile/
   build-mobile-ios: pre-build build-webapp mobile-ios
       ...
   build-mobile-android: pre-build build-webapp mobile-android
       ...
   ```
7. Verify: `gomobile bind -target=ios` succeeds, produces xcframework
8. Verify: `gomobile bind -target=android` succeeds, produces AAR
9. Run `yarn install` from root (workspace validation)

**TDD**:
- RED: `gomobile bind -target=ios ./mobile/` → fails (no gomobile installed)
- GREEN: Install gomobile, bind succeeds, xcframework produced
- RED: `cd mobile && npx cap --version` → fails (no Capacitor project)
- GREEN: Scaffold complete, Capacitor CLI available
- REFACTOR: Verify K2Plugin definitions compile (`cd mobile && npx tsc --noEmit`)

---

## T1: iOS Implementation

**Scope**: K2Plugin Swift implementation (Capacitor Plugin for main app),
PacketTunnelExtension (NE process with gomobile Engine), entitlements,
Podfile, Xcode project configuration.

**Files**:
- `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift`
- `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.m`
- `mobile/plugins/k2-plugin/K2Plugin.podspec`
- `mobile/ios/App/App/AppDelegate.swift`
- `mobile/ios/App/App/App.entitlements`
- `mobile/ios/App/App/App.simulator.entitlements`
- `mobile/ios/App/App/Info.plist`
- `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift`
- `mobile/ios/App/PacketTunnelExtension/PacketTunnelExtension.entitlements`
- `mobile/ios/App/PacketTunnelExtension/Info.plist`
- `mobile/ios/App/Podfile`

**Depends on**: [T0]

**Steps**:
1. Initialize Capacitor iOS platform: `cd mobile && npx cap add ios`
2. Create PacketTunnelExtension target in Xcode project:
   - Bundle ID: `io.kaitu.PacketTunnelExtension`
   - Add NE + App Group entitlements
   - Link K2Mobile.xcframework to this target
3. Configure Podfile:
   - App target: Capacitor pods + K2Plugin pod
   - PacketTunnelExtension target: no Capacitor, only xcframework
   - Platform iOS 16.0
4. Implement K2Plugin.swift:
   - `checkReady()` → always `{ ready: true, version: Bundle.main.version }`
   - `getUDID()` → `UIDevice.current.identifierForVendor`
   - `getVersion()` → bundle info
   - `connect(wireUrl)` → `NEVPNManager.shared().loadFromPreferences` →
     save wireUrl in protocolConfiguration → `startVPNTunnel(options:)`
   - `disconnect()` → `connection.stopVPNTunnel()`
   - `getStatus()` → `sendProviderMessage("status")` with 5s timeout,
     fallback to `NEVPNConnection.status` mapped to VpnStatus
   - Subscribe: observe `NEVPNStatusDidChange` → `notifyListeners("vpnStateChange")`
   - **State mapping**: Engine uses `"disconnected"`, webapp expects `"stopped"` — map in getStatus() and event handler
5. Implement PacketTunnelProvider.swift:
   - `startTunnel(options:)` → extract wireUrl → `MobileNewEngine()` →
     `engine.setEventHandler(self)` → get fd from `packetFlow` →
     `engine.start(wireUrl, fd)`
   - `stopTunnel(reason:)` → `engine.stop()`
   - `handleAppMessage(data:)` → parse "status" → `engine.statusJSON()` → reply
   - `EventHandler` conformance: `onStateChange` → write to App Group UserDefaults
     + post Darwin notification
6. Configure App.entitlements (NE + App Group `group.io.kaitu`)
7. `pod install` → verify build: `xcodebuild -workspace ... -scheme App build`

**TDD**:
- RED: `xcodebuild build` → fails (no PacketTunnelExtension target)
- GREEN: Add NE target, entitlements, implement providers → build succeeds
- RED: manual on device: connect → VPN not established (no K2Plugin)
- GREEN: Implement K2Plugin.connect → VPN establishes via NE
- REFACTOR: Verify status query via sendProviderMessage, event propagation

---

## T2: Android Implementation

**Scope**: K2Plugin Kotlin implementation (Capacitor Plugin), K2VpnService
(foreground service with gomobile Engine), AndroidManifest, Gradle config.

**Files**:
- `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt`
- `mobile/plugins/k2-plugin/android/build.gradle`
- `mobile/android/app/src/main/java/io/kaitu/MainActivity.kt`
- `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt`
- `mobile/android/app/src/main/AndroidManifest.xml`
- `mobile/android/app/build.gradle`
- `mobile/android/k2-mobile/build.gradle`
- `mobile/android/k2-mobile/libs/` (AAR copied here)
- `mobile/android/variables.gradle`
- `mobile/android/settings.gradle`

**Depends on**: [T0]

**Steps**:
1. Initialize Capacitor Android platform: `cd mobile && npx cap add android`
2. Create `k2-mobile` library module wrapping gomobile AAR:
   - Copy `k2/build/k2mobile.aar` to `k2-mobile/libs/`
   - build.gradle: `api files('libs/k2mobile.aar')`
3. Add to `settings.gradle`: `include ':k2-mobile'`
4. Configure AndroidManifest.xml:
   - VpnService with `specialUse` foreground type
   - INTERNET, ACCESS_NETWORK_STATE, FOREGROUND_SERVICE, POST_NOTIFICATIONS permissions
5. Implement K2VpnService.kt:
   - `onCreate()` → `Mobile.newEngine()` + `engine.setEventHandler(eventBridge)`
   - `onStartCommand(intent)` → extract wireUrl → `VpnService.Builder()` →
     configure addresses/routes/DNS → `establish()` → fd →
     `engine.start(wireUrl, pfd.fd)` → `startForeground(notification)`
   - `onRevoke()` → `engine.stop()` → `stopForeground()`
   - EventBridge: implements `mobile.EventHandler` → forwards to K2Plugin
6. Implement K2Plugin.kt:
   - `checkReady()` → `{ ready: true }`
   - `getUDID()` → `Settings.Secure.ANDROID_ID`
   - `getVersion()` → `PackageInfo.versionName`
   - `connect(wireUrl)` → start K2VpnService with wireUrl extra
   - `disconnect()` → `engine.stop()` + stop service
   - `getStatus()` → `engine.statusJSON()` (direct call, same process) → parse JSON
   - **State mapping**: Engine uses `"disconnected"`, webapp expects `"stopped"` — map in getStatus() and event handler
   - Subscribe: EventHandler bridge → `notifyListeners("vpnStateChange", ...)`
7. Configure build.gradle: add k2-mobile dependency, set SDK versions, ABIs
8. Verify: `./gradlew assembleDebug` succeeds

**TDD**:
- RED: `./gradlew assembleDebug` → fails (no VpnService, no plugin)
- GREEN: Implement VpnService + K2Plugin → build succeeds
- RED: manual on device: connect → VPN not established
- GREEN: Wire up K2Plugin → VpnService → Engine → tunnel connects
- REFACTOR: Verify foreground notification, event propagation, permission dialog

---

## T3: NativeVpnClient (webapp)

**Scope**: Implement `NativeVpnClient` in webapp's vpn-client module. Update
`createVpnClient()` factory to detect Capacitor and return NativeVpnClient.
Use dynamic import so Capacitor deps are not loaded on desktop.

**Files**:
- `webapp/src/vpn-client/native-client.ts` (new)
- `webapp/src/vpn-client/index.ts` (update factory)
- `webapp/src/vpn-client/__tests__/native-client.test.ts` (new)
- `webapp/src/vpn-client/__tests__/index.test.ts` (update factory tests)

**Depends on**: [T0] (needs K2Plugin TypeScript definitions from plugins/k2-plugin/src/)

**Steps**:
1. Create `native-client.ts`:
   - Imports K2Plugin from `k2-plugin`
   - Implements VpnClient interface (VpnState is now 3-state: stopped/connecting/connected)
   - `connect()` → `K2Plugin.connect({ wireUrl })`
   - `disconnect()` → `K2Plugin.disconnect()`
   - `checkReady()` → `K2Plugin.checkReady()` mapped to ReadyState
   - `getStatus()` → `K2Plugin.getStatus()` mapped to VpnStatus
     - **State mapping**: If plugin returns `"disconnected"` (from Engine), map to `"stopped"` for VpnState
   - `getVersion()` → `K2Plugin.getVersion()`
   - `getUDID()` → `K2Plugin.getUDID()` → extract `.udid`
   - `subscribe()` → `K2Plugin.addListener('vpnStateChange', ...)` +
     `K2Plugin.addListener('vpnError', ...)` → emit VpnEvent
     - **State mapping**: Map `"disconnected"` → `"stopped"` in event handler
   - `destroy()` → remove all listeners
2. Update `index.ts` factory:
   ```typescript
   export async function createVpnClient(override?: VpnClient): Promise<VpnClient> {
     if (override) { instance = override; return override; }
     if (!instance) {
       if (isCapacitorNative()) {
         const { NativeVpnClient } = await import('./native-client');
         instance = new NativeVpnClient();
       } else {
         instance = new HttpVpnClient();
       }
     }
     return instance;
   }

   function isCapacitorNative(): boolean {
     return typeof (window as any)?.Capacitor?.isNativePlatform === 'function'
       && (window as any).Capacitor.isNativePlatform();
   }
   ```
   Note: factory becomes async (dynamic import). Callers already `await` init.
3. Update factory tests to cover Capacitor detection path
4. Write NativeVpnClient unit tests with mocked K2Plugin

**TDD**:
- RED: vitest: `NativeVpnClient.connect("k2v5://...")` → calls `K2Plugin.connect({ wireUrl: "k2v5://..." })`
- GREEN: Implement NativeVpnClient with K2Plugin calls
- RED: vitest: `createVpnClient()` returns NativeVpnClient when `window.Capacitor.isNativePlatform()` returns true
- GREEN: Update factory with platform detection + dynamic import
- RED: vitest: `subscribe()` receives vpnStateChange events from K2Plugin listener
- GREEN: Implement listener bridging
- RED: vitest: getStatus() maps `"disconnected"` → `"stopped"` for VpnState
- GREEN: Add state mapping in getStatus() and subscribe()
- REFACTOR: Verify destroy() removes all listeners, no memory leaks

---

## T4: Build System + CI/CD

**Scope**: Makefile mobile targets (finalize), build scripts, GitHub Actions
workflow for iOS + Android builds.

**Files**:
- `Makefile` (finalize mobile targets)
- `scripts/build-mobile-ios.sh` (new)
- `scripts/build-mobile-android.sh` (new)
- `.github/workflows/build-mobile.yml` (new)

**Depends on**: [T1, T2, T3]

**Steps**:
1. Create `scripts/build-mobile-ios.sh`:
   - gomobile bind → xcframework
   - Copy xcframework to iOS project
   - `npx cap sync ios`
   - `xcodebuild archive`
   - Verify codesign
2. Create `scripts/build-mobile-android.sh`:
   - gomobile bind → AAR
   - Copy AAR to Android project
   - `npx cap sync android`
   - `./gradlew assembleRelease`
3. Finalize Makefile targets:
   ```makefile
   build-mobile-ios: pre-build build-webapp
       bash scripts/build-mobile-ios.sh
   build-mobile-android: pre-build build-webapp
       bash scripts/build-mobile-android.sh
   dev-ios: pre-build build-webapp
       cd mobile && npx cap sync ios && npx cap run ios
   dev-android: pre-build build-webapp mobile-android
       ...
   ```
4. Create `.github/workflows/build-mobile.yml`:
   - Manual trigger with platform selection (ios/android/both)
   - iOS job: macos-latest, Go 1.24, Node 20, gomobile, Xcode
   - Android job: ubuntu-latest, Go 1.24, Node 20, Java 17, Android SDK, gomobile
   - Artifact upload (30-day retention)
5. Verify: trigger workflow, both jobs green

**TDD**:
- RED: `make build-mobile-ios` → fails (no script)
- GREEN: Script builds xcarchive
- RED: `make build-mobile-android` → fails (no script)
- GREEN: Script builds APK
- RED: CI workflow fails (no workflow file)
- GREEN: Workflow runs, artifacts uploaded
- REFACTOR: Verify artifact sizes reasonable, codesign valid in CI

---

## T5: E2E Verification

**Scope**: End-to-end testing on real devices / simulators. Manual verification
of all mobile-specific ACs.

**Files**:
- `scripts/test_mobile.sh` (new — checklist runner)

**Depends on**: [T4]

**Steps**:
1. iOS simulator test:
   - `make dev-ios` → app launches in simulator
   - Webapp loads correctly (no blank screen)
   - Login flow works (Cloud API via antiblock)
   - Server list loads
   - Note: VPN connection not testable on simulator (NE limitation)
2. iOS device test:
   - Install via Xcode → physical device
   - Connect to VPN server → tunnel established
   - Status shows connected + uptime
   - Disconnect → tunnel torn down
   - Kill app → reopen → cold start shows connected state (StatusJSON)
   - Background → foreground → state preserved
3. Android emulator test:
   - `make dev-android` → app launches in emulator
   - Webapp loads, login works
   - VPN permission dialog appears on first connect
4. Android device test:
   - Install APK on arm64 device
   - Connect → foreground notification shown
   - Status + uptime display correct
   - Disconnect → notification removed
   - Kill app → reopen → cold start recovery
5. Cross-platform parity:
   - Same server list on both platforms
   - Same UI layout (no mobile-specific code)
   - Same auth flow

**TDD**: Integration test script (manual verification checklist), not unit tests.
- Create `scripts/test_mobile.sh` with pass/fail checklist output
- Each check is a manual step with expected result

---

## Execution Order

```
Phase 0: Webapp Prerequisite (execute first)
  webapp-state-alignment       align VpnState with k2 3-state model
  (see docs/features/webapp-state-alignment/plan.md)

Phase 1: Foundation (sequential, after webapp alignment done)
  T0 (scaffold + gomobile verify)

Phase 2: Platform Implementation (parallel)
  T1 (iOS)       ─┐
  T2 (Android)    ├── all parallel, no file overlap
  T3 (webapp)    ─┘

Phase 3: Integration
  T4 (build + CI/CD)              after T1 + T2 + T3
  T5 (E2E verification)           after T4
```

## Execution Notes

- **k2 StatusJSON is DONE**: Submodule at `bfeb06c` includes Engine.StatusJSON() and 3-state simplification. No k2 repo work needed.
- **webapp-state-alignment FIRST**: VpnState must be 3-state (stopped/connecting/connected) before mobile NativeVpnClient can be implemented correctly.
- **State mapping in K2Plugin / NativeVpnClient**: gomobile Engine uses `"disconnected"` as idle state, webapp uses `"stopped"`. Both K2Plugin (native side) and NativeVpnClient (TS side) should map this. Prefer mapping in NativeVpnClient (single place) for testability.
- **T1 and T2 are parallel**: iOS touches `mobile/ios/` + `plugins/k2-plugin/ios/`, Android touches `mobile/android/` + `plugins/k2-plugin/android/`. No file overlap.
- **T3 parallel with T1/T2**: Only touches `webapp/src/vpn-client/`. Independent.
- **Entry-point conflict**: T1 and T2 both modify `mobile/plugins/k2-plugin/package.json` (podspec ref, gradle ref). Merge the simpler one first, per task-splitting knowledge.
- **Factory becomes async**: T3 changes `createVpnClient()` to async. All callers already use `await` for init, so this is safe. But verify no synchronous callers exist.
- **gomobile prerequisite**: T1 and T2 need xcframework/AAR built by T0. They copy the artifact into their platform project.
- **Webapp subagent**: T3 should invoke `/word9f-frontend` for implementation decisions.
- **Removed speedtest**: k2 daemon removed speedtest action in `bfeb06c`. Webapp doesn't use it currently, no impact.
