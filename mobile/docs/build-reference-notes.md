# Mobile build & reference notes — toolchain, version literals, code map, logging, gomobile names, small gotchas

Moved verbatim from `mobile/CLAUDE.md` (2026-11-14) so the per-session doc stays small. **Read this before bumping the Android/iOS toolchain (Node, JDK, Gradle/AGP/Kotlin, SDK levels, Xcode), editing `scripts/sync-version.sh` targets, touching `NativeLogger` / log upload / log level plumbing, or calling a gomobile-generated `Appext*` symbol** — the CLAUDE.md keeps one-line invariants; the full original wording is here.

**Corrections (2026-11-14):** the File Logging paragraph below says the Lambda source is "not in this repo" — it is: `scripts/lambda/s3-log-notify/index.mjs` (`LEGACY_PREFIXES`). The "kept through v7" router-fix note predates Capacitor 8; the override is still present.

## Toolchain baseline (Capacitor 8, upgraded 2026-10-21)

- **Node ≥ 22** — `@capacitor/cli` 8 declares `engines.node >=22`, and yarn 1 **fails the whole root `yarn install`** on an engines mismatch, so every workflow that installs the root workspace (not just mobile) must run Node 22. All `.github/workflows` pin `'22'`; don't drop one back to 20.
- **JDK 21** required for Android builds (Capacitor regenerates `capacitor.build.gradle` with `VERSION_21` on every `cap sync`; JDK 17 will fail with `invalid source release: 21`).
  - **Local:** just `brew install openjdk@21`. The root `Makefile`'s `ANDROID_JAVA_HOME` auto-detects it and exports `JAVA_HOME` only for `appext-android` / `build-android` / `dev-android` targets — your shell's default `JAVA_HOME` stays untouched. `make check-jdk-21` prints the install hint on failure.
  - **CI:** `actions/setup-java@v4` with `java-version: '21'` in `.github/workflows/build-mobile.yml`.
- Gradle wrapper 8.14.3 + AGP 8.13.0 + Kotlin 2.2.20 (Kotlin 2.2 rejects the old `kotlinOptions {}` block — JVM target lives in a top-level `kotlin { compilerOptions { … } }`). `versionCode` / `versionName` keep the old space-separated Groovy syntax on purpose: `scripts/sync-version.sh` and CI `check-versions` match them by sed pattern.
- **compileSdk / targetSdk 36** (`android/variables.gradle`, minSdk 24). Play rejects an upload
  targeting below 36, so this is a floor, not a preference — bumping it back breaks the Play lane
  silently (CI still builds; only the Console refuses).
- **Xcode 26+** required for App Store submissions (Apple mandate from 2026-04-28: iOS 26 SDK + Xcode 26). CI pins `runs-on: macos-26` with `setup-xcode@v1 xcode-version: '26.4'`. Local dev machines need macOS 15.6+ to install Xcode 26.
- iOS deployment target: 14.0 at the pbxproj project level, but every target (App, PacketTunnelExtension, K2Tests) overrides to **16.0** and `Podfile` is `platform :ios, '16.0'` — 16 is the real floor.
- CocoaPods for iOS (NOT SPM — Capacitor 8 defaults *new* projects to SPM; existing CocoaPods projects keep working, and switching is a separate change)

## Commands (CI entry points)

CI entry points are `scripts/build-mobile-{ios,android}.sh`, not `make build-*`; both run the same steps and the same plugin-test gate. `build-mobile.yml` sets `K2_BUILD_LOG_LEVEL: info` and `EMBED_REQUIRE_FULL: "1"` (root CLAUDE.md); the `dry_run` dispatch input builds + signs + runs the purity gate but skips ASC upload, S3 upload and Slack.

## Versioning (package.json → native literals)

`scripts/sync-version.sh` (run by `make pre-build` on every build path) rewrites, from root `package.json`:

- `android/app/build.gradle` `versionName` + `versionCode = MAJOR*1000000 + MINOR*10000 + PATCH*100 + REV` (0.4.10 → 41000; the old `10000/100/1` scheme topped out at 410, so every installed client still sees an upgrade). `REV` = env `ANDROID_BUILD_REV` (0-99; CI repo variable of the same name, normally unset) — bump it **only** to re-upload an already-uploaded version to Google Play (which permanently burns every accepted versionCode), then clear it. CI `check-versions` reads only `versionName`.
- `plugins/k2-plugin/ios/Plugin/K2Helpers.swift` `let k2AppVersion = "x.y.z"` — **compiled into the plugin on purpose**, not read from Info.plist: it is the native version the web-OTA `min_native` gate compares against, and a gate must never fall back to a different quantity. Bumping `package.json` without `pre-build` ships a stale iOS gate.
- `project.pbxproj` `MARKETING_VERSION` + `CURRENT_PROJECT_VERSION`, obtained by calling `scripts/build-mobile-ios.sh --print-build-number` — never a second copy of the formula.
**Bridge API version**: the mobile mirrors `K2Helpers.swift` `k2BridgeApiVersion` and `K2PluginUtils.kt` `BRIDGE_API_VERSION` must equal `webapp/src/types/bridge-version.ts` `BRIDGE_API_VERSION` (as must desktop `DESKTOP_BRIDGE_VERSION`; Linux `LinuxBridgeVersion` is the ungated fifth). `webapp/src/types/__tests__/bridge-contract.test.ts` greps them and snapshots `contracts/bridge-api.json`; adding a `CAPPluginMethod` or bumping one literal without the regen turns webapp vitest red. Procedure: `webapp/CLAUDE.md` 兼容模型 "契约门".

## Architecture（非显然的位置）

- `plugins/k2-plugin/` — Capacitor plugin, **gomobile-free** (Gotchas). `dist/` is committed (webapp `tsc` reads it).
  - Android pure-Kotlin, JVM-tested decision cores: `K2PluginUtils.kt` (brandString, `BRIDGE_API_VERSION`, minisign pubkey), `AutoUpdatePlan.kt` (APK lane vs web-OTA lane), `WebBootDecision.kt` (`.boot-pending` / quarantine); `VpnServiceBridge.kt` = Service ↔ Plugin interface.
  - iOS: `K2Helpers.swift` (`k2AppVersion`, `k2BridgeApiVersion`, `decideWebBoot`, `webBundleSkipReason`), `K2Plugin+Iap.swift` / `StoreKitManager.swift` / `IapHelpers.swift` (StoreKit 2), `K2RelayBridge.swift` (handler slots the App target fills).
- App shells (where gomobile calls live): Android `android/app/src/main/java/io/kaitu/` — `K2VpnService.kt`, `K2VpnServiceUtils.kt` (JVM-testable), `MainApplication.kt` (`Appext.prefetchRules`); iOS `ios/App/App/AppDelegate.swift` (fills `K2RelayBridge`, `AppextPrefetchRules`), `AppBridgeViewController.swift`, `Config/` + `brand/` (brand xcconfigs, InfoPlist.strings, AppIcon).
- `ios/App/PacketTunnelExtension/` — `PacketTunnelProvider.swift`, its own `NativeLogger.swift` (50 MB — diverged from the plugin's 20 MB copy; fix both when touching either), `NEHelpers.swift`, `Info.plist` (explicit `CFBundleExecutable` + `CFBundleVersion`).
- `ios/App/K2Tests/` — XCTest, **not run in CI**. gomobile outputs `android/app/libs/k2mobile.aar` / `ios/App/K2Mobile.xcframework` are gitignored copies.

## iOS Two-Process Architecture

App process (`K2Plugin.swift`, `NETunnelProviderManager.startVPNTunnel(options:)`) ↔ NE process (`PacketTunnelProvider`, gomobile Engine: `Start` / `StatusJSON` / `Pause` / `Wake`), sharing state through the App Group.

- **State source of truth**: `NEVPNStatusDidChange` notification ONLY
- **Error propagation**: NE writes `vpnError` to App Group → `cancelTunnelWithError()` → system `.disconnected` → K2Plugin reads App Group
- **Config delivery**: `configJSON` passed via `startVPNTunnel(options:)`, fallback to `providerConfiguration`
- **TUN fd acquisition** (in order): KVC `packetFlow.value(forKeyPath: "socket.fileDescriptor")` → utun fd scan (`findTunnelFileDescriptor()`)
- The **App target also links `K2Mobile.xcframework`** (relay fetch + rule prefetch run in the app process, VPN-independent); the K2Plugin pod does not — see Gotchas "plugin is gomobile-free".

## Android VpnService Architecture

`K2Plugin.kt` binds `K2VpnService` (foreground service) through `VpnServiceBridge`; the service runs the gomobile Engine, gets the TUN fd from `Builder().establish()`, and pushes `EventHandler.onStatus()` back as the JS `vpnStateChange`.

- **VPN permission**: `VpnService.prepare(activity)` — must use Activity context, not Application
- **TUN fd**: `Builder().establish()` returns `ParcelFileDescriptor`. Pass `fd` (not `detachFd()`) — Go `syscall.Dup()` internally. Kotlin retains ownership for `close()` on teardown.
- **Engine calls**: All gomobile JNI calls run on `engineExecutor` (single-thread) to prevent ANR
- **Foreground service**: Required for VPN. Uses `FOREGROUND_SERVICE_TYPE_SPECIAL_USE` on Android 14+

## File Logging & Upload

| Layer | File | Source |
|-------|------|--------|
| Go engine | `{LogDir}/k2.log` | slog via `config.SetupLogging()` |
| Native | `{LogDir}/native.log` | `NativeLogger` (Swift/Kotlin) |
| Webapp | `{LogDir}/webapp.log` | `K2Plugin.appendLogs(entries)` from JS |

- **iOS LogDir**: `{AppGroup}/logs/` — App Group is brand-parameterized (`K2_APP_GROUP`), shared between App and NE process. **Android LogDir**: `{filesDir}/logs/`.
- **Upload**: `K2Plugin.uploadLogs()` — ZIP all logs → PUT to S3 key `mobile/{version}/{udid}/{date}/logs-{ts}-{id}.zip` (feedback path only — mobile has no beta auto-upload). Legacy prefixes (`service-logs/` / `feedback-logs/`) still supported by the Lambda (unverified — Lambda source is not in this repo).
- **Redaction**: Token, password, `Bearer`, `X-K2-Token` patterns stripped before upload.
- **Debug dual output**: `EngineConfig.Debug = true` → `io.MultiWriter(file, stderr)` so Go logs reach Xcode console / logcat. Set via `#if DEBUG` (Swift) / `BuildConfig.DEBUG` (Kotlin).

## Log Level Control

- **Go**: `appext.SetLogLevel(level)` — changes global `slog.LevelVar` at runtime. Also applied from `ClientConfig.Log.Level` on `Start()`.
- **Android**: `K2Plugin.setLogLevel()` → `VpnServiceBridge.setLogLevel()` → `Appext.setLogLevel()` (same process, immediate).
- **iOS**: `K2Plugin.setLogLevel()` only logs — NE is a separate process; level applies via `configJSON.log.level` on the next `startVPNTunnel`.

## Gomobile Bindings

```bash
make appext-android    # → k2/build/k2mobile.aar; build-android/dev-android copy it to mobile/android/app/libs/
make appext-ios        # → k2/build/K2Mobile.xcframework; build-ios/dev-ios copy it to mobile/ios/App/
```

Go package `k2/appext/` → gomobile naming:
- **Android**: package `appext`, classes `Appext` (static: `freeMemory`, `setLogLevel`, `prefetchRules`, `relayFetch`, `relayAddNodes`, `classifyApps`), `Engine`, `EventHandler`, `EngineConfig`, `SocketProtector`, `NetEvent`
- **iOS/ObjC**: prefix `Appext` — `AppextNewEngine()`, `AppextNewEngineConfig()`, `AppextFreeMemory()`, `AppextSetLogLevel()`, `AppextMemorySnapshot()`, `AppextPrefetchRules()`, `AppextRelayFetch()`, `AppextRelayAddNodes()`, protocol `AppextEventHandlerProtocol`

## Gotchas

- **Plugin is gomobile-free by rule**: `make plugin-purity-check` (a prerequisite of `appext-ios` / `appext-android`) fails on any `appext.` / `Appext*` reference under `plugins/k2-plugin/` — the plugin's gradle/podspec do not link the aar/xcframework. gomobile calls live in the app shells: `AppDelegate.swift` (`AppextRelayFetch` / `AppextRelayAddNodes` installed into `K2RelayBridge.handler` / `.addNodesHandler`, `AppextPrefetchRules`), `MainApplication.kt` (`Appext.prefetchRules`), `K2VpnService.kt`. The iOS plugin reaches relay only through those handler slots (nil → reports relay unsupported); Android through `VpnServiceBridge.relayFetch`.
- **K2Plugin dist/ must be committed**: Webapp `tsc` depends on `dist/definitions.d.ts`. After editing `src/`, rebuild and commit `dist/`.
- **Web OTA bundles are minisign-signed**: pubkey `RWSD3s7X…` is duplicated as `K2Plugin.swift` `webOtaMinisignPublicKey` and `K2PluginUtils.kt` `WEB_OTA_MINISIGN_PUBKEY` (same key pair as desktop); verification is mandatory whenever the manifest carries `sig` (sha256 always). Rotating the key without both literals silently breaks mobile OTA.
- **IAP is iOS-only**: StoreKit 2 via `iapGetProducts` / `iapPurchase` / `iapRestore` / `iapFinishTransaction` (`K2Plugin+Iap.swift`, `StoreKitManager.swift`); `K2Plugin.kt` has no IAP surface.
- **gomobile Swift API**: Generated methods use `throws` pattern, NOT NSError out-parameter.
- **iOS entitlements**: Debug config must use `App/App.entitlements` (has NE entitlement), not `App.simulator.entitlements`. Missing NE entitlement → "not entitled to establish IPC with plugins".
- **iOS extension plist**: Must have explicit `CFBundleExecutable` + `CFBundleVersion`. Build settings NOT inherited from project.
- **Android JVM unit tests**: Pure utils in `K2VpnServiceUtils.kt` / `K2PluginUtils.kt` / `AutoUpdatePlan.kt` / `WebBootDecision.kt`. Need `testImplementation "org.json:json:20231013"` (built into Android runtime but not JVM).
- **Capacitor iOS router fix**: `AppBridgeViewController` overrides `router()` with `FixedCapacitorRouter` — added for the Capacitor 6.x empty-path bug, kept through v7 since it's harmless. Main.storyboard must reference this subclass.
- **VPN teardown critical**: `vpnInterface.close()` is mandatory on Android. Without it, Android keeps VPN routing active → all external requests hang. Only phone reboot recovers.
- **K2Plugin dual-CDN pattern**: `fetchManifest(endpoints)` tries CloudFront first, S3 fallback. `resolveDownloadURL()` handles relative vs absolute URLs.
- **Android `VpnService.protect()` scope**: Must protect wire transport (QUIC UDP, TCP-WS TCP), direct DNS (raw UDP), and direct tunnel connections (smart routing bypass). Uses `syscall.RawConn.Control()` in Go's `net.Dialer.Control`. gomobile requires `int32` fd parameter (not `int`).
- **VPN display name**: Brand-parameterized (`K2VpnDisplayName` Info.plist key / `k2_vpn_display_name` Android resource): `"kaitu.io"` on kaitu, `"Overleap"` on overleap, across iOS (NE `localizedDescription`, `serverAddress`, `CFBundleDisplayName`) and Android (`setSession()`, notification title).
- **iOS stale VPN config cleanup**: `loadVPNManager()` removes NE configs with wrong `providerBundleIdentifier` or `localizedDescription` on every load. Prevents "Found 0 registrations" after bundle ID migration — and is why the iron-rule fallbacks above must never drift.
