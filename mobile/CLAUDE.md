# Mobile — Capacitor 7 + gomobile

Capacitor 7 mobile app wrapping the k2 Go tunnel core via gomobile. K2Plugin bridges JS ↔ native VPN lifecycle.

## Toolchain baseline (Capacitor 7)

- Node ≥ 20
- **JDK 21** required for Android builds (Cap 7 regenerates `capacitor.build.gradle` with `VERSION_21` on every `cap sync`; JDK 17 will fail with `invalid source release: 21`).
  - **Local:** just `brew install openjdk@21`. The root `Makefile`'s `ANDROID_JAVA_HOME` auto-detects it and exports `JAVA_HOME` only for `appext-android` / `build-android` / `dev-android` targets — your shell's default `JAVA_HOME` stays untouched. `make check-jdk-21` prints the install hint on failure.
  - **CI:** `actions/setup-java@v4` with `java-version: '21'` in `.github/workflows/build-mobile.yml`.
- Gradle wrapper 8.11.1 + AGP 8.7.2 + Kotlin 1.9.25
- **Xcode 26+** required for App Store submissions (Apple mandate from 2026-04-28: iOS 26 SDK + Xcode 26). CI pins `runs-on: macos-26` with `setup-xcode@v1 xcode-version: '26.4'`. Local dev machines need macOS 15.6+ to install Xcode 26.
- iOS deployment target: 14.0 at the pbxproj project level, but every target (App, PacketTunnelExtension, K2Tests) overrides to **16.0** and `Podfile` is `platform :ios, '16.0'` — 16 is the real floor.
- CocoaPods for iOS (NOT SPM — avoids Capacitor 8's SPM regression surface when we later upgrade)

## Commands

```bash
make dev-android      # gomobile bind + cap sync + cap run android --flavor $(BRAND)
make dev-ios          # gomobile bind + cap sync, then scripts/deploy-ios-device.sh to the auto-detected
                      # physical iPhone (IOS_DEVICE=<udid> overrides); `cap run ios` (simulator) only
                      # when no device is detected — real devices only speak the CoreDevice tunnel
make build-android    # gomobile bind + decrypt-keystore + cap sync + :k2-plugin:testDebugUnitTest
                      # + assemble{Kaitu|Overleap}Release (per flavor — bare assembleRelease builds BOTH)
make build-ios        # gomobile bind + cap sync + xcodebuild archive
make publish-android VERSION=x.y.z BRAND=kaitu   # latest.json manifests → scripts/publish-mobile.sh
make publish-ios     VERSION=x.y.z BRAND=kaitu
make plugin-purity-check                          # k2-plugin must stay gomobile-free (see Gotchas)
cd plugins/k2-plugin && npm run build             # Rebuild K2Plugin dist/ (tsc; required after src/ edits)
```

After editing `plugins/k2-plugin/src/`:
```bash
cd plugins/k2-plugin && npm run build   # Regenerate dist/ (committed)
rm -rf node_modules/k2-plugin && yarn install --force  # Re-copy to node_modules
npx cap sync                            # Sync to native projects
```

CI entry points are `scripts/build-mobile-{ios,android}.sh`, not `make build-*`; both run the same steps and the same plugin-test gate. `build-mobile.yml` sets `K2_BUILD_LOG_LEVEL: info` and `EMBED_REQUIRE_FULL: "1"` (release fails if the full `krs.tar.gz` can't be fetched — enforces the k2-rules-first deploy order); the `dry_run` dispatch input builds + signs + runs the purity gate but skips ASC upload, S3 upload and Slack.

## Versioning (package.json → native literals)

`scripts/sync-version.sh` (run by `make pre-build` on every build path) rewrites, from root `package.json`:

- `android/app/build.gradle` `versionName` + `versionCode = MAJOR*10000 + MINOR*100 + PATCH`.
- `plugins/k2-plugin/ios/Plugin/K2Helpers.swift` `let k2AppVersion = "x.y.z"` — **compiled into the plugin on purpose**, not read from Info.plist: it is the native version the web-OTA `min_native` gate compares against, and a gate must never fall back to a different quantity. Bumping `package.json` without `pre-build` ships a stale iOS gate.
- `project.pbxproj` `MARKETING_VERSION` + `CURRENT_PROJECT_VERSION`, obtained by calling `scripts/build-mobile-ios.sh --print-build-number` — never a second copy of the formula.

**iOS build number** (`scripts/build-mobile-ios.sh`): marketing `0.x.y` → `4.x.y` (predecessor ANC shipped 3.0.1; Apple's downgrade check needs > 3.x). `CFBundleVersion = 4000000 + MINOR*100000 + PATCH*1000 + SLOT*10 + REV`; SLOT = N for `-beta.N` (1..98), 99 for final; REV = repo variable `IOS_BUILD_REV` (0..9), normally unset — set it **only** to re-upload an already-uploaded (version, slot) after an ASC rejection, then clear it; never wire it to a run number. The script aborts on MAJOR ≠ 0 or MINOR/PATCH > 99. `scripts/test-ios-build-number.sh` (ci.yml) drives the real script rather than restating the arithmetic.

**Bridge API version** is a triple literal that must stay equal: `K2Helpers.swift` `k2BridgeApiVersion`, `K2PluginUtils.kt` `BRIDGE_API_VERSION`, `webapp/src/types/bridge-version.ts` `BRIDGE_API_VERSION`. `webapp/src/types/__tests__/bridge-contract.test.ts` greps both native files and generates `contracts/bridge-api.json` (method list + version). Adding a `CAPPluginMethod` or bumping one literal without `cd webapp && UPDATE_BRIDGE_CONTRACT=1 npx vitest run src/types/__tests__/bridge-contract.test.ts` turns webapp vitest red.

## Brand (kaitu / overleap)

Same `K2_BRAND` build-time contract as desktop/webapp/web (root `Makefile` `BRAND ?= kaitu` → `export K2_BRAND`; recursive `make` only inherits the **exported env** `K2_BRAND`, never the make variable `BRAND` — any script invoking `make` directly must pass `BRAND=$BRAND` explicitly). `mobile/capacitor.config.ts` reads `process.env.K2_BRAND` at `cap sync` time to pick `appId`/`appName` (`io.kaitu`/`开途` vs `io.overleap`/`Overleap`); the values are inert (`cap sync` never reads them — real ids live in the native projects) but are kept brand-conditional. `scripts/build-mobile-{ios,android}.sh` validate `BRAND` and **re-export `K2_BRAND` themselves** because they call `npx cap sync` outside `make`'s recipe-scoped env — a stale overleap APK once shipped `"appId":"io.kaitu"` in `assets/capacitor.config.json` from missing exactly this export.

### Android

- Gradle product flavors `kaitu`/`overleap` on dimension `brand` (`android/app/build.gradle`): `applicationId` forks (`io.kaitu`/`io.overleap`) but `namespace "io.kaitu"` stays **shared** — that's why `io.kaitu.K2VpnService` / `io.kaitu.k2plugin` class names work unchanged for both flavors (namespace ≠ applicationId).
- Per-flavor resources: `app/src/{kaitu,overleap}/res/values/brand.xml` — keys `k2_cdn_primary` / `k2_cdn_fallback` / `k2_vpn_display_name`.
- Dual keystores: `app/kaitu-release.jks.enc` (alias `kaitu`, RSA 2048, `signingConfigs.release`) / `app/overleap-release.jks.enc` (alias `overleap`, `signingConfigs.overleap`), AES-256-CBC; passwords `KAITU_ANDROID_STORE_PASSWORD` / `OVERLEAP_ANDROID_STORE_PASSWORD` (env locally, GH secrets in CI). `make decrypt-keystore BRAND=<brand>` decrypts the matching pair; plaintext `.jks` is gitignored.
- `signingConfigs` are assigned **unconditionally** at the flavor level — a missing store password must fail `assemble*Release` loudly, not silently produce an unsigned APK. Debug variants get a *separate* conditional override in `androidComponents.onVariants(...withBuildType("debug"))`, because AGP's buildType > flavor merge order otherwise lets the implicit debug signingConfig clobber the flavor's. Don't collapse the two halves into one conditional.

### Plugin brand purity

- `plugins/k2-plugin` has no flavors and zero brand literals for CDN values: `K2PluginUtils.brandString()` resolves `k2_cdn_primary` / `k2_cdn_fallback` via `context.resources.getIdentifier(name, "string", context.packageName)` — whatever the **host app**'s active flavor merged in. The host app (`K2VpnService.kt`) reads `R.string.k2_vpn_display_name` directly — a compile-time ref, legal because that code lives in the flavored `app` module.
- Exempt internal tokens — bare `kaitu` literals expected on **both** brand builds: `kaitu-icon://` scheme (Android WebViewClient interception for app-bypass icons in `K2Plugin.kt`), `io.kaitu.k2plugin` package/class labels, `kaitu-service-logs` S3 bucket host (both `K2Plugin.swift` and `K2Plugin.kt`), `io.kaitu.K2VpnService`, and the unreferenced `package_name` string resource in `main/res/values/strings.xml`. `scripts/check-mobile-brand-purity.sh <brand> <apk-or-xcarchive>` (unzips / extracts, case-insensitive grep) therefore narrows the overleap-build forbidden pattern to `kaitu\.io|开途|開途|/kaitu/(android|ios|web)/` CDN path segments, not the bare word.

### iOS

- `brand-{kaitu,overleap}.xcconfig` (under `ios/App/App/Config/`) define `K2_BUNDLE_ID` / `K2_APP_GROUP` / `K2_DISPLAY_NAME` / `K2_CDN_PRIMARY` / `K2_CDN_FALLBACK` / `K2_VPN_DISPLAY_NAME` / `K2_APP_STORE_URL`. `scripts/apply-ios-brand.sh <brand>` copies the selected one to `brand-active.xcconfig` (committed content is always the kaitu fallback), stages localized `InfoPlist.strings` (en / ja / zh-Hans / zh-Hant from `App/brand/<brand>/`) and swaps `Assets.xcassets/AppIcon.appiconset` from `App/brand/<brand>/AppIcon.appiconset/`. **These diffs are never committed** — run `scripts/apply-ios-brand.sh kaitu` before committing anything else under `mobile/ios/`.
- Wrapper configs (`App-Base-{Debug,Release}.xcconfig`, `PacketTunnelExtension-Base-*.xcconfig`) `#include` both the CocoaPods-generated xcconfig *and* `brand-active.xcconfig` — the escape hatch that lets `pod install` keep working (CocoaPods refuses to overwrite a custom `baseConfigurationReference` but is satisfied once it sees its own xcconfig `#include`d).
- `K2_APP_STORE_URL`: kaitu's is the live listing (`https://apps.apple.com/app/id6448744655`); overleap's is **empty** in `brand-overleap.xcconfig`, and `K2Plugin.swift` treats empty as absent at both call sites (native-update check and cold-start auto-check). Distinct from `OVERLEAP_APPSTORE_URL`, the publish-time env var feeding `scripts/publish-mobile.sh`'s manifest `appstore_url`.
- `Kaitu.storekit` / `Overleap.storekit` are static per-brand placeholders (Overleap product ids follow `io.overleap.sub.*`), not touched by `apply-ios-brand.sh`; whether any scheme references them is unverified (no `.xcscheme` is committed).

### iOS derivation iron rule

- Swift reads brand values from `Info.plist` keys `K2AppGroup` / `K2CDNPrimary` / `K2CDNFallback` / `K2VpnDisplayName` / `K2AppStoreURL` (populated from the xcconfig `K2_*` vars via `$(K2_APP_GROUP)`-style substitution). `K2AppStoreURL` is main-app-only.
- The NE's bundle id is derived as `Bundle.main.bundleIdentifier + ".ThePacketTunnel"` — **only in the main app process** (`K2Plugin.swift`). `PacketTunnelProvider.swift` never derives it; it reads its own `Bundle.main.bundleIdentifier`.
- Every `?? ` fallback literal across `K2Plugin.swift` / `AppDelegate.swift` / `PacketTunnelProvider.swift` (`group.io.kaitu`, `kaitu.io`, `com.allnationconnect.anc.wgios`, the CDN URLs, the App Store URL) is intentionally the **pre-split kaitu value** — `loadVPNManager()` removes any NE config whose `providerBundleIdentifier` / `localizedDescription` doesn't match, so a derived value that drifts even slightly from the legacy literal wipes live users' VPN configs. Never edit these fallbacks. `K2Tests/BrandDerivationTests.swift` asserts equality against the legacy literals (skips itself on overleap via a bundle-id-prefix check) — but **no scheme or CI step runs `K2Tests`** (no `.xcscheme` in the repo, no `xcodebuild test` anywhere in `.github/` / `scripts/` / `Makefile`), so it is a reference, not an enforcement.
- **kaitu's real iOS bundle id is the legacy ANC one** (`com.allnationconnect.anc.wgios`, pbxproj `K2_BUNDLE_ID`), not `io.kaitu` — immutable post-publish (migrating would zero ratings and orphan every auto-renewable subscription). All kaitu IAP lives under ASC app `6448744655` (subscription group "Kaitu Pro" `22133714` — unverified from code; `Kaitu.storekit` uses local group id `20001`). Overleap is a **separate ASC record** (`6759199298`, bundle id `io.overleap` + `group.io.overleap` + `.ThePacketTunnel` from day one); whether that record is live is an external fact not verifiable here — the code is still Phase 0 (see below).

### Release chain

- `scripts/publish-mobile.sh VERSION --platform=android|ios --brand=kaitu|overleap` (brand falls back to `$K2_BRAND`, then `kaitu`; channel auto-detected from a `-beta` suffix). kaitu's `APPSTORE_URL` is fixed; overleap requires `OVERLEAP_APPSTORE_URL` — if unset, the iOS manifest step exits early with a WARN while Android still publishes.
- `.github/workflows/build-mobile.yml`: brand matrix is `["kaitu"]` unless repo variable `OVERLEAP_MOBILE_CI == 'true'` (then `["kaitu","overleap"]`) — set on both the iOS and Android jobs separately since `vars` isn't shared matrix context. GitHub-hosted runners (`macos-26`, `ubuntu-latest`, ephemeral). ASC upload runs for kaitu only. The purity gate is run against the brand-exact artifact path, never a glob (the desktop `.app.tar.gz` alphabetical-glob incident, `813bf3f5`). Success also triggers the `publish-web-ota` tail job for bare (non-beta) push tags.
- **Still Phase 0 in code**: overleap legs are skipped until `OVERLEAP_MOBILE_CI` is set, which needs the `OVERLEAP_ANDROID_STORE_PASSWORD` secret populated (the workflow already references it) and ASC provisioning for `io.overleap`; `K2_APP_STORE_URL` / `OVERLEAP_APPSTORE_URL` are empty; the overleap iconset is a placeholder from `web/public/brand/overleap`.

## Architecture

```
mobile/
├── capacitor.config.ts          # appId/appName via K2_BRAND (inert — see Brand); webDir: ../webapp/dist
├── plugins/k2-plugin/           # Capacitor plugin — JS ↔ native bridge (gomobile-FREE, see Gotchas)
│   ├── src/                     # definitions.ts (K2PluginInterface), web.ts (stub), index.ts (registerPlugin)
│   ├── dist/                    # Built output (MUST be committed — webapp tsc depends on it)
│   ├── android/src/.../K2Plugin.kt        # VPN lifecycle, auto-update, logs, relay, getDefaultGateway
│   ├── android/src/.../K2PluginUtils.kt   # Pure Kotlin (JVM-testable): brandString, BRIDGE_API_VERSION, minisign pubkey
│   ├── android/src/.../AutoUpdatePlan.kt  # Pure decision core: native-APK lane vs web-OTA lane
│   ├── android/src/.../WebBootDecision.kt # Pure decision core: .boot-pending / quarantine
│   ├── android/src/.../MinisignVerifier.kt # Web OTA signature check
│   ├── android/src/.../NativeLogger.kt    # native.log writer (20 MB cap)
│   ├── android/src/.../VpnServiceBridge.kt # Service ↔ Plugin interface (relayFetch, setLogLevel)
│   ├── android/src/test/                  # JVM unit tests — gate on both release paths
│   ├── ios/Plugin/K2Plugin.swift          # NE manager, auto-update, logs, getDefaultGateway
│   ├── ios/Plugin/K2Helpers.swift         # k2AppVersion, k2BridgeApiVersion, decideWebBoot, webBundleSkipReason
│   ├── ios/Plugin/K2Plugin+Iap.swift, StoreKitManager.swift, IapHelpers.swift  # StoreKit 2 IAP (iOS only)
│   ├── ios/Plugin/K2RelayBridge.swift     # Handler slots the App target fills with gomobile relay calls
│   ├── ios/Plugin/MinisignVerifier.swift  # Web OTA signature check (vendored BLAKE2b)
│   └── ios/Plugin/NativeLogger.swift      # native.log writer (20 MB cap)
├── android/
│   ├── app/src/main/java/io/kaitu/
│   │   ├── K2VpnService.kt     # VpnService (engine lifecycle, memory pressure)
│   │   ├── K2VpnServiceUtils.kt # Pure Kotlin utils (parseCIDR, stripPort — JVM-testable)
│   │   ├── MainApplication.kt  # Appext.prefetchRules at launch
│   │   └── MainActivity.kt     # Capacitor activity
│   └── app/libs/k2mobile.aar   # gomobile output (gitignored via *.aar)
├── ios/App/
│   ├── K2Mobile.xcframework     # gomobile output copied here by build-ios/dev-ios (gitignored)
│   ├── App/
│   │   ├── AppBridgeViewController.swift  # Capacitor router fix (FixedCapacitorRouter)
│   │   ├── AppDelegate.swift    # Registers K2RelayBridge handlers, AppextPrefetchRules
│   │   ├── App.entitlements     # NE + App Group entitlements
│   │   └── Config/, brand/      # Brand xcconfigs, per-brand InfoPlist.strings + AppIcon
│   ├── K2Tests/                 # XCTest (BrandDerivation, K2Helpers, IapHelpers, Minisign, NEHelpers) — NOT run in CI
│   └── PacketTunnelExtension/
│       ├── PacketTunnelProvider.swift  # NE provider (engine lifecycle, memory monitor, sleep/wake)
│       ├── NativeLogger.swift   # Separate copy from the plugin's — 50 MB cap
│       ├── NEHelpers.swift      # Pure helpers (parseIPv4CIDR, parseIPv6CIDR, stripPort)
│       └── Info.plist           # Must have explicit CFBundleExecutable + CFBundleVersion
```

## iOS Two-Process Architecture

```
┌─────────────────────────┐     ┌──────────────────────────────────┐
│ App Process              │     │ NE Process (PacketTunnelProvider) │
│                          │     │                                  │
│ K2Plugin.swift           │     │ gomobile Engine (appext)         │
│   NETunnelProviderMgr    │────→│   Start(configJSON, fd, cfg)    │
│   startVPNTunnel(opts)   │     │   StatusJSON()                  │
│                          │     │   Pause() / Wake()              │
│ NEVPNStatusDidChange     │←────│   EventBridge.onStatus(json)    │
│   (system notification)  │     │                                  │
│                          │     │ App Group (UserDefaults)         │
│ vpnError ← App Group    │←────│   vpnError → structured JSON    │
└─────────────────────────┘     └──────────────────────────────────┘
```

- **State source of truth**: `NEVPNStatusDidChange` notification ONLY
- **Error propagation**: NE writes `vpnError` to App Group → `cancelTunnelWithError()` → system `.disconnected` → K2Plugin reads App Group
- **Config delivery**: `configJSON` passed via `startVPNTunnel(options:)`, fallback to `providerConfiguration`
- **TUN fd acquisition** (in order): KVC `packetFlow.value(forKeyPath: "socket.fileDescriptor")` → utun fd scan (`findTunnelFileDescriptor()`)
- The **App target also links `K2Mobile.xcframework`** (relay fetch + rule prefetch run in the app process, VPN-independent); the K2Plugin pod does not — see Gotchas "plugin is gomobile-free".

## Server Selection — Manual only on mobile

Mobile has **no smart-mode / k2subs resolution**: users pick a tunnel on Dashboard and the webapp passes that single `k2v5://` URL as `_k2.run('up', {routes:[{via:'k2v5://...'}]})` — the NE's 50 MB jetsam limit rules out an in-extension resolver, and a webapp-side one would fetch `/api/subs` through the tunnel it is trying to establish. Failure mode: any raw `k2subs://` leaked to appext is dropped by `engine.buildOutboundMap` as a reserved scheme → code 570 "no k2v5 outbound configured" — always a webapp bug; the only legitimate `via` on mobile is `k2v5://` or `direct`. `probe.store` + `ProbeChip` populate RTT/loss via the webapp-triggered `runProbe()`; the daemon-side background probe loop is desktop-only.

## Router LAN Bridge (k2r headless app-control)

- `getDefaultGateway()` (iOS `defaultGatewayIPv4()` walks the `PF_ROUTE` table skipping `utun*`; Android filters `ConnectivityManager.allNetworks` to WiFi/Ethernet, deliberately not `activeNetwork` which is the TUN once connected) — **currently unconsumed**; discovery is anchor-only (`10.17.79.1:1779`), `router-service.ts` never calls it.
- `routerRequest` (`capacitor-k2.ts`) uses `CapacitorHttp.request()` with `disableRedirects: true`, behind the TS-side SSRF gate `assertRouterUrlAllowed` (`http://` to a private/loopback IPv4 **literal** only — mirrors desktop Rust `is_private_host`). iOS needs `NSLocalNetworkUsageDescription` + `NSAllowsLocalNetworking` (both in `Info.plist`); no Bonjour entitlement.
- Full flow: `webapp/CLAUDE.md` "Router Tab"; design `docs/superpowers/specs/2026-07-17-k2r-headless-app-control-design.md`.

## Android VpnService Architecture

```
┌─────────────────────────┐     ┌──────────────────────────────────┐
│ K2Plugin.kt              │     │ K2VpnService (foreground service) │
│   VpnServiceBridge       │────→│   gomobile Engine (appext)       │
│   bindService()          │     │   Builder().establish() → TUN fd │
│                          │     │   engineExecutor (background)    │
│ onStatus(statusJSON)     │←────│   EventHandler.onStatus()       │
│   → JS vpnStateChange   │     │   NetworkCallback → onAvailable  │
└─────────────────────────┘     └──────────────────────────────────┘
```

- **VPN permission**: `VpnService.prepare(activity)` — must use Activity context, not Application
- **TUN fd**: `Builder().establish()` returns `ParcelFileDescriptor`. Pass `fd` (not `detachFd()`) — Go `syscall.Dup()` internally. Kotlin retains ownership for `close()` on teardown.
- **Engine calls**: All gomobile JNI calls run on `engineExecutor` (single-thread) to prevent ANR
- **Foreground service**: Required for VPN. Uses `FOREGROUND_SERVICE_TYPE_SPECIAL_USE` on Android 14+
- **Self-UID exemption**: `Builder.addDisallowedApplication(packageName)` is mandatory. Android captures same-UID traffic in the app's own TUN by default — without this, K2Plugin's S3 log uploads, cloudApi calls, and OTA downloads all route through the very tunnel they're trying to debug, and fail precisely when VPN is unhealthy. iOS gets this isolation for free via the separate NE process. Symptom of regression: Android tickets with `vpnState=connected` show `logCount=0` while iOS/desktop show `logCount=1`.

## Crash Diagnostics & Memory (appext)

- `k2/appext/appext.go`: `debug.SetTraceback("crash")` in `init()` (all goroutine stacks to logcat / os_log on unrecoverable panics) + `recover()` wrappers on every exported Engine method — `Start()` / `Stop()` panic → error return, `StatusJSON()` → `{"state":"disconnected"}`, `Pause()` / `Wake()` / `NotifyNetEvent()` → log only. (Not yet documented in `k2/appext/CLAUDE.md`.)
- Go-side memory strategy (GOGC=10, 35 MB `SetMemoryLimit`, `FreeMemory()`, `MemorySnapshot()`, `k2/core/limits_ios.go` connection/buffer caps): see `k2/appext/CLAUDE.md`. Note its "15s UDP idle timeout" is stale — `limits_ios.go` has `udpIdleTimeoutSec = 60`.

### Android: `onTrimMemory()`
- Triggers at `TRIM_MEMORY_RUNNING_CRITICAL` (level 15+) — K2VpnService is a foreground service, so it only ever sees the RUNNING_* tiers (5/10/15). `RUNNING_LOW` (10) fired too readily during ordinary background use and tore down the tunnel far more often than warranted (ticket #3169 — UI read "connected" for 11-58 min while the tunnel was dead).
- Calls `engine.pause()` (releases QUIC/TCP-WS connections) + `Appext.freeMemory()`; `AtomicBoolean(enginePaused)` prevents double-pause; reset on `stopVpn()`.
- Primary wake: `onAvailable()` network callback (`compareAndSet(true, false)`). Safety-net wake: `pendingPauseTimeout` — `onAvailable()` does not fire on a stable network, so a 60 s `mainHandler.postDelayed` force-wakes if no callback arrives first. Cancelled by a real `onAvailable()` and by `stopVpn()`.
- `StatusJSON()` reports `"paused"` while `e.paused` is true (`k2/engine/engine.go buildStatusLocked`) — not just the one-shot `OnStatus(StatePaused)` push — so the webapp's 15 s safety-net poll doesn't overwrite the paused UI state back to "connected".

### iOS: `NETunnelProvider.sleep()` / `wake()`
- `sleep()`: stops memory monitor → `engine.pause()` → `AppextFreeMemory()`; `wake()`: `engine.wake()` (re-establishes wire connections).
- Memory monitor: `DispatchSourceTimer` logs `AppextMemorySnapshot()` for diagnostics (per-component heap breakdown).

## File Logging & Upload

| Layer | File | Source |
|-------|------|--------|
| Go engine | `{LogDir}/k2.log` | slog via `config.SetupLogging()` |
| Native | `{LogDir}/native.log` | `NativeLogger` (Swift/Kotlin) |
| Webapp | `{LogDir}/webapp.log` | `K2Plugin.appendLogs(entries)` from JS |

- **iOS LogDir**: `{AppGroup}/logs/` — App Group is brand-parameterized (`K2_APP_GROUP`), shared between App and NE process. **Android LogDir**: `{filesDir}/logs/`.
- **Two `NativeLogger.swift` files exist and have diverged**: the plugin's (`plugins/k2-plugin/ios/Plugin/`, 20 MB cap, matches `NativeLogger.kt`) and the NE's (`ios/App/PacketTunnelExtension/`, still 50 MB). The root doc's "20 MB cap is universal" is false for the NE process; fix both when touching either.
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
- **`file:` plugin sync**: Copied (not symlinked) to `node_modules/`. Must `rm -rf node_modules/k2-plugin && yarn install --force` after edits. **Gradle compiles the copy** (`capacitor.settings.gradle` points `:k2-plugin` at `node_modules/k2-plugin/android`), main *and* test sources — so `./gradlew :k2-plugin:testDebugUnitTest` after editing `mobile/plugins/k2-plugin/` silently tests the STALE copy and reports green. Re-sync before believing any local plugin test result. That gate does run on both release paths (`make build-android`, `build-mobile-android.sh`), after `cap sync`.
- **Web OTA bundles are minisign-signed**: pubkey `RWSD3s7X…` is duplicated as `K2Plugin.swift` `webOtaMinisignPublicKey` and `K2PluginUtils.kt` `WEB_OTA_MINISIGN_PUBKEY` (same key pair as desktop); verification is mandatory whenever the manifest carries `sig` (sha256 always). Rotating the key without both literals silently breaks mobile OTA.
- **IAP is iOS-only**: StoreKit 2 via `iapGetProducts` / `iapPurchase` / `iapRestore` / `iapFinishTransaction` (`K2Plugin+Iap.swift`, `StoreKitManager.swift`); `K2Plugin.kt` has no IAP surface.
- **gomobile Swift API**: Generated methods use `throws` pattern, NOT NSError out-parameter.
- **iOS entitlements**: Debug config must use `App/App.entitlements` (has NE entitlement), not `App.simulator.entitlements`. Missing NE entitlement → "not entitled to establish IPC with plugins".
- **iOS extension plist**: Must have explicit `CFBundleExecutable` + `CFBundleVersion`. Build settings NOT inherited from project.
- **Android JVM unit tests**: Pure utils in `K2VpnServiceUtils.kt` / `K2PluginUtils.kt` / `AutoUpdatePlan.kt` / `WebBootDecision.kt`. Need `testImplementation "org.json:json:20231013"` (built into Android runtime but not JVM).
- **Capacitor iOS router fix**: `AppBridgeViewController` overrides `router()` with `FixedCapacitorRouter` — added for the Capacitor 6.x empty-path bug, kept through v7 since it's harmless. Main.storyboard must reference this subclass.
- **Android 15 edge-to-edge**: Handled by `@capawesome/capacitor-android-edge-to-edge-support` (pads the WebView's parent for system-bar insets). `BottomNavigation.tsx` uses plain `env(safe-area-inset-bottom, 0px)` — works on iOS natively and on Android via the plugin. Do not hand-roll CSS variables or MainActivity WindowInsets listeners.
- **VPN teardown critical**: `vpnInterface.close()` is mandatory on Android. Without it, Android keeps VPN routing active → all external requests hang. Only phone reboot recovers.
- **K2Plugin dual-CDN pattern**: `fetchManifest(endpoints)` tries CloudFront first, S3 fallback. `resolveDownloadURL()` handles relative vs absolute URLs.
- **Android `VpnService.protect()` scope**: Must protect wire transport (QUIC UDP, TCP-WS TCP), direct DNS (raw UDP), and direct tunnel connections (smart routing bypass). Uses `syscall.RawConn.Control()` in Go's `net.Dialer.Control`. gomobile requires `int32` fd parameter (not `int`).
- **Mobile auto-update on cold start**: K2Plugin checks for updates on `load()` — every launch. The native-APK lane and the web-OTA lane are **independent and both always run**: a pending APK update must never suppress the web OTA, because the APK prompt is user-refusable and web OTA is the shell's only hour-scale remediation channel. Android enforces this in `AutoUpdatePlan.kt` (`AutoUpdatePlanTest`); iOS has the same shape (two independent `do` blocks in `performAutoUpdateCheck`). Only `min_native` / `min_bridge` legitimately hold back a web bundle.
- **VPN display name**: Brand-parameterized (`K2VpnDisplayName` Info.plist key / `k2_vpn_display_name` Android resource): `"kaitu.io"` on kaitu, `"Overleap"` on overleap, across iOS (NE `localizedDescription`, `serverAddress`, `CFBundleDisplayName`) and Android (`setSession()`, notification title).
- **iOS stale VPN config cleanup**: `loadVPNManager()` removes NE configs with wrong `providerBundleIdentifier` or `localizedDescription` on every load. Prevents "Found 0 registrations" after bundle ID migration — and is why the iron-rule fallbacks above must never drift.
- **Web OTA min_native**: Manifest `min_native` is CI-derived from `contracts/webapp-support-floor.json` — the **support floor** (oldest native app the latest webapp still supports), NOT a per-feature "webapp requires native X" bump. Hand-writing it was the 2026-03 incident root cause and is forbidden; there is no `minNativeVersion` field left to bump. Compatibility with older apps is runtime capability detection, not a version gate — see `webapp/CLAUDE.md` 兼容模型 and `docs/superpowers/specs/2026-08-14-web-ota-design.md` §4. Comparison uses BASE version only: `0.4.0-beta.6` satisfies `min_native=0.4.0`.
- **Web OTA boot verification**: `.boot-pending` marker in `web-update/`, created on OTA apply, cleared by **`confirmWebBootOk()`** — NOT by `checkReady()`. The webapp calls `checkReady()` from `injectCapacitorGlobals()`, before store init and the first React render, so clearing there marked a bundle "verified" that could still die before painting. It did, silently, for months — a white-screen bundle cleared its own marker and was served again on every cold start. `confirmWebBootOk()` is called from `main.tsx` only after `ReactDOM.render`, matching desktop `ui_boot_ok`. Decision logic is pure and unit-tested: `WebBootDecision.kt` / `K2Helpers.swift` (`decideWebBoot`).
- **Web OTA quarantine**: a rollback records the failed version in `web-quarantined-version.txt` — in `filesDir`/Documents, **outside `web-update/`**, because rollback deletes that directory along with the `version.txt` the quarantine needs. `planAutoUpdate` (Android) / `webBundleSkipReason` (iOS) then refuse that exact version until the CDN publishes something newer. Without it the shell is a reinstall treadmill: rollback drops `version.txt`, `localWebVersion` falls back to the app version, the same bad bundle reads as "newer", and `fetchManifest` has neither cache nor backoff. Mirrors desktop `evaluate_manifest` and Linux `shouldApply`.

## App Store Optimization

ASO / AEO constitutional rules for App Store Connect submissions (keyword system, no "VPN" in user-visible text, review-notes template, etc.) live in [`docs/marketing/aso-constitutional-rules.md`](../docs/marketing/aso-constitutional-rules.md).

## Cross-Layer Conventions

- **Go→JS JSON key convention**: Go `json.Marshal` outputs snake_case. JS/TS expects camelCase. `K2Plugin.swift` / `K2Plugin.kt` must remap at the boundary before forwarding to the webapp.
- **`.gitignore` for native platforms**: Never ignore entire source directories (`mobile/ios/`, `mobile/android/`). Only ignore build artifacts.

## Android S3 CDN Structure

`d13jc1jqzlg4yt.cloudfront.net/{kaitu,overleap}/android/` (brand-parameterized path, `--brand` flag on `scripts/publish-mobile.sh`; S3 origin `s3://d0.all7.cc/{brand}/`):

- `{VERSION}/{Kaitu|Overleap}-{VERSION}.apk` — CI uploads here (`scripts/ci/upload-release.sh --android`)
- `latest.json` — stable APK manifest; `beta/latest.json` — beta channel
- `tools/tools.json` — adb binaries (`scripts/sync-adb-tools.sh`; kaitu path only)

`publish-mobile.sh` copies the artifact into `beta/{VERSION}/` and then: a **stable** version updates both `latest.json` and `beta/latest.json` (beta is a superset of stable); a **`-beta` version updates only `beta/latest.json`** — it never touches the stable manifest the Android install flow reads. iOS manifests (`ios/latest.json`, `ios/beta/latest.json`) follow the same rule.

## Related Docs

- [Root Architecture](../CLAUDE.md)
- [Webapp Frontend](../webapp/CLAUDE.md) — Shared UI running inside Capacitor WebView
- [Go Core / appext](../k2/appext/CLAUDE.md) — gomobile engine wrapper
- [Engine](../k2/engine/CLAUDE.md) — Unified tunnel lifecycle
