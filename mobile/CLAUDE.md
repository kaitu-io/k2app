# Mobile — Capacitor 7 + gomobile

Capacitor 7 mobile app wrapping the k2 Go tunnel core via gomobile. K2Plugin bridges JS ↔ native VPN lifecycle.

## Toolchain baseline (Capacitor 7)

- Node ≥ 20
- **JDK 21** required for Android builds (Cap 7 regenerates `capacitor.build.gradle` with `VERSION_21` on every `cap sync`; JDK 17 will fail with `invalid source release: 21`).
  - **Local:** just `brew install openjdk@21`. The root `Makefile`'s `ANDROID_JAVA_HOME` auto-detects it and exports `JAVA_HOME` only for `appext-android` / `build-android` / `dev-android` targets — your shell's default `JAVA_HOME` (e.g. JDK 17 for other projects) stays untouched.
  - **CI:** `actions/setup-java@v4` with `java-version: '21'` already set in `.github/workflows/build-mobile.yml`.
  - If `make check-jdk-21` fails, the Makefile prints the install hint.
- Gradle wrapper 8.11.1 + AGP 8.7.2 + Kotlin 1.9.25
- **Xcode 26+** required for App Store submissions (Apple mandate from 2026-04-28: iOS 26 SDK + Xcode 26). CI pins `runs-on: macos-26` with `setup-xcode@v1 xcode-version: '26.4'`. Local dev machines need macOS 15.6+ to install Xcode 26.
- iOS deployment target 14 in pbxproj root, app target ships 15.6, NE 16 (unchanged — iOS 26 SDK supports old deployment targets via build settings)
- CocoaPods for iOS (NOT SPM — avoids Capacitor 8's SPM regression surface when we later upgrade)

## Commands

```bash
make dev-android                 # gomobile bind + cap sync + cap run android
make dev-ios                     # cap sync + cap run ios (gomobile bind manual)
make build-android               # gomobile bind + cap sync + assembleRelease
make build-ios                   # gomobile bind + cap sync + xcodebuild archive
cd plugins/k2-plugin && npm run build  # Rebuild K2Plugin dist/ (required after src/ edits)
```

After editing `plugins/k2-plugin/src/`:
```bash
cd plugins/k2-plugin && npm run build   # Regenerate dist/
rm -rf node_modules/k2-plugin && yarn install --force  # Re-copy to node_modules
npx cap sync                            # Sync to native projects
```

## Architecture

```
mobile/
├── capacitor.config.ts          # Capacitor config (appId: io.kaitu, webDir: ../webapp/dist)
├── plugins/k2-plugin/           # Capacitor plugin — JS ↔ native VPN bridge
│   ├── src/                     # TypeScript definitions + web stub
│   │   ├── definitions.ts       # K2PluginInterface (connect/disconnect/status/setLogLevel/updates)
│   │   ├── web.ts               # Web stub (throws unavailable)
│   │   └── index.ts             # registerPlugin('K2Plugin')
│   ├── dist/                    # Built output (MUST be committed — webapp tsc depends on it)
│   ├── android/src/.../K2Plugin.kt      # Android plugin (VPN lifecycle, auto-update, log level)
│   ├── android/src/.../K2PluginUtils.kt # Pure Kotlin utils (JVM-testable, no android.util.Log)
│   ├── android/src/.../VpnServiceBridge.kt  # Service ↔ Plugin interface
│   └── ios/Plugin/K2Plugin.swift        # iOS plugin (NE manager, auto-update, log level)
├── android/
│   ├── app/src/main/java/io/kaitu/
│   │   ├── K2VpnService.kt     # Android VpnService (engine lifecycle, memory pressure)
│   │   ├── K2VpnServiceUtils.kt # Pure Kotlin utils (parseCIDR, stripPort — JVM-testable)
│   │   └── MainActivity.kt     # Capacitor activity
│   └── app/libs/                # K2Mobile.aar (gomobile output, gitignored)
├── ios/App/
│   ├── App/
│   │   ├── AppBridgeViewController.swift  # Capacitor router fix (FixedCapacitorRouter)
│   │   ├── AppDelegate.swift    # Standard Capacitor delegate
│   │   └── App.entitlements     # NE + App Group entitlements
│   └── PacketTunnelExtension/
│       ├── PacketTunnelProvider.swift  # iOS NE provider (engine lifecycle, memory monitor, sleep/wake)
│       ├── NativeLogger.swift   # File logger for native layer events (logs to native.log)
│       ├── NEHelpers.swift      # Pure helpers (parseIPv4CIDR, parseIPv6CIDR, stripPort)
│       └── Info.plist           # Extension plist (must have CFBundleExecutable + CFBundleVersion)
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

## Server Selection — Manual only on mobile

Mobile has **no smart-mode / k2subs resolution**. Users pick a specific
tunnel on Dashboard and the webapp passes that single `k2v5://` URL to
`_k2.run('up', config)`. Mobile engine never sees `k2subs://`.

```
user → Dashboard tunnel list → picks one → _k2.run('up', {routes:[{via:'k2v5://...'}]})
```

**Why no smart mode on mobile:** iOS NE has a 50MB jetsam limit. A Go HTTPS
client + JSON cache + refresher goroutine inside the extension would bloat
the binary/memory footprint. Main App process (webapp) could host such a
resolver, but doing so in the webapp creates a double-encapsulation risk
(webapp fetches `/api/subs` while VPN is up → request goes through the
tunnel → fails the very session we're about to establish). So we keep
mobile strictly manual; smart selection is only available on desktop
where the daemon's in-process resolver has no such constraints.

**Node-probe note:** `probe.store` + `ProbeChip` populate RTT/loss
measurements on the Dashboard tunnel list via `runProbe()` so users have
data-driven guidance when picking manually. The daemon-side background
probe loop (which updates `probe.Registry`) runs on desktop only —
mobile's probe path is the explicit webapp-triggered one.

Failure mode: if any webapp code path leaks raw `k2subs://` to appext,
`engine.buildOutboundMap` drops the route as reserved scheme → code 570
"no k2v5 outbound configured". See `k2/appext/CLAUDE.md`. That is always
a webapp bug — the only legitimate `via` on mobile is `k2v5://` or
`direct`.

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
- **Self-UID exemption**: `Builder.addDisallowedApplication(packageName)` is mandatory. Android captures same-UID traffic in the app's own TUN by default — without this, K2Plugin's S3 log uploads, cloudApi calls, and OTA downloads all route through the very tunnel they're trying to debug, and fail precisely when VPN is unhealthy (the case logs are needed for). iOS gets this isolation for free via the separate NE process. Symptom of regression: Android tickets with `vpnState=connected` show `logCount=0` while iOS/desktop with the same state show `logCount=1`.

## Crash Diagnostics (appext)

Two-layer panic protection in `k2/appext/appext.go`:

1. **`debug.SetTraceback("crash")`** in `init()` — prints ALL goroutine stacks on unrecoverable panics (engine-internal goroutines). Output goes to logcat (Android) / os_log (iOS).
2. **`recover()` wrappers** on all Engine exported methods — catches panics from JNI/gomobile call stack, logs stack trace, returns safe defaults instead of crashing the process.

| Method | Recovery behavior |
|--------|-------------------|
| `Start()` | panic → error return |
| `Stop()` | panic → error return |
| `StatusJSON()` | panic → `{"state":"disconnected"}` |
| `Pause()` / `Wake()` / `OnNetworkChanged()` | panic → log only |

## Memory Pressure Handling

### Android: `onTrimMemory()`
- Triggers at `TRIM_MEMORY_RUNNING_CRITICAL` (level 15+) — K2VpnService is a foreground service, so it only ever sees the RUNNING_* tiers (5/10/15), never the backgroundable BACKGROUND/MODERATE/COMPLETE tiers (40/60/80). `RUNNING_LOW` (10) fired too readily during ordinary background use and tore down the tunnel far more often than genuine memory pressure warranted (ticket #3169 — UI kept reading "connected" for 11-58 min while the tunnel was dead).
- Calls `engine.pause()` (releases QUIC/TCP-WS connections) + `Appext.freeMemory()` (Go GC + return to OS)
- `AtomicBoolean(enginePaused)` prevents double-pause
- Primary wake: `onAvailable()` network callback (`compareAndSet(true, false)`)
- Safety-net wake: `pendingPauseTimeout` — `onAvailable()` does not fire on a stable, unchanging network, so a 60s `mainHandler.postDelayed` bound-of-last-resort force-wakes if no network callback arrives first. Cancelled by a real `onAvailable()` and by `stopVpn()`.
- `engine.GetStatus()`/`StatusJSON()` reports `"paused"` while `e.paused` is true (`k2/engine/engine.go buildStatusLocked`) — not just the one-shot `OnStatus(StatePaused)` push — so polling clients (webapp's 15s safety-net poll) don't overwrite the paused UI state back to "connected".
- Reset on `stopVpn()`

### iOS: `NETunnelProvider.sleep()` / `wake()`
- Apple's official NE resource conservation hooks
- `sleep()`: stops memory monitor → `engine.pause()` → `AppextFreeMemory()`
- `wake()`: `engine.wake()` (re-establishes wire connections)
- Memory monitor: 10s timer logs `AppextMemorySnapshot()` for diagnostics (per-component heap breakdown)

### iOS: Go Memory Optimization (appext)
- `GOGC=10`: aggressive GC at 10% heap growth (sing-box strategy)
- `SetMemoryLimit(35MB)`: hard ceiling, 15MB headroom for C/ObjC/system
- `FreeOSMemory()` after `Start()`: reclaims init-time allocations
- Platform limits: 512 max connections, 8KB TCP buffers, 15s UDP idle timeout
- Sampled GC: every 8 connection releases, force GC if HeapInuse > 20MB

### Go: `FreeMemory()`
- `debug.FreeOSMemory()` — forces GC + returns freed pages to OS
- gomobile exports: `Appext.freeMemory()` (Java) / `AppextFreeMemory()` (ObjC)

## File Logging & Upload

Three-layer logging system across platforms:

| Layer | File | Source |
|-------|------|--------|
| Go engine | `{LogDir}/k2.log` | slog via `config.SetupLogging()` |
| Native | `{LogDir}/native.log` | `NativeLogger` (Swift/Kotlin) |
| Webapp | `{LogDir}/webapp.log` | `K2Plugin.appendLogs(entries)` from JS |

- **iOS LogDir**: `{AppGroup}/logs/` — App Group `group.io.kaitu` shared between App process and NE process
- **Android LogDir**: `{filesDir}/logs/`
- **Upload**: `K2Plugin.uploadLogs()` — compress all logs → ZIP → PUT to S3 with `mobile/{version}/{udid}/{date}/logs-{ts}-{id}.zip` key format.
- **Redaction**: Token, password, Bearer, X-K2-Token patterns stripped before upload
- **Debug dual output**: `EngineConfig.Debug = true` enables `io.MultiWriter(file, stderr)` so Go engine logs appear in Xcode console / logcat. Set via `#if DEBUG` (Swift) / `BuildConfig.DEBUG` (Kotlin).

## Log Level Control

- **Go**: `appext.SetLogLevel(level)` — changes global `slog.LevelVar` at runtime. Also applied from `ClientConfig.Log.Level` on `Start()`.
- **Android**: `K2Plugin.setLogLevel()` → `VpnServiceBridge.setLogLevel()` → `Appext.setLogLevel()` (same process)
- **iOS**: `K2Plugin.setLogLevel()` logs only — NE runs in separate process, level applied via `configJSON.log.level` on next connect

## Gomobile Bindings

```bash
# Build (from k2app root)
make appext-android    # → mobile/android/app/libs/K2Mobile.aar
make appext-ios        # → mobile/ios/App/Pods/K2Mobile.xcframework (or manual copy)
```

Go package `k2/appext/` → gomobile naming:
- **Android**: package `appext`, classes `Appext` (static), `Engine`, `EventHandler`, `EngineConfig`, `SocketProtector`
- **iOS/ObjC**: prefix `Appext`, functions `AppextNewEngine()`, `AppextFreeMemory()`, `AppextSetLogLevel()`

## Gotchas

- **K2Plugin dist/ must be committed**: Webapp `tsc` depends on `dist/definitions.d.ts`. After editing `src/`, rebuild and commit `dist/`.
- **`file:` plugin sync**: Copied (not symlinked) to `node_modules/`. Must `rm -rf node_modules/k2-plugin && yarn install --force` after edits.
- **gomobile Swift API**: Generated methods use `throws` pattern, NOT NSError out-parameter.
- **iOS entitlements**: Debug config must use `App/App.entitlements` (has NE entitlement), not `App.simulator.entitlements`. Missing NE entitlement → "not entitled to establish IPC with plugins".
- **iOS extension plist**: Must have explicit `CFBundleExecutable` + `CFBundleVersion`. Build settings NOT inherited from project.
- **Android JVM unit tests**: Pure utils in `K2VpnServiceUtils.kt` / `K2PluginUtils.kt`. Needs `testImplementation "org.json:json:20231013"` (built into Android runtime but not JVM).
- **Capacitor iOS router fix**: `AppBridgeViewController` overrides `router()` with `FixedCapacitorRouter` — originally added for the Capacitor 6.x empty-path bug. Kept through the v7 upgrade since the override is harmless if the underlying bug was fixed upstream. Main.storyboard must reference this subclass. If we later confirm v7+ handles empty paths correctly, this can be removed.
- **Android 15 edge-to-edge**: Handled by `@capawesome/capacitor-android-edge-to-edge-support` (plugin auto-pads the WebView's parent container for system-bar insets). `BottomNavigation.tsx` uses plain `env(safe-area-inset-bottom, 0px)` — works on iOS natively and on Android via the plugin. Do not hand-roll CSS variables or MainActivity WindowInsets listeners.
- **VPN teardown critical**: `vpnInterface.close()` is mandatory on Android. Without it, Android keeps VPN routing active → all external requests hang. Only phone reboot recovers.
- **K2Plugin dual-CDN pattern**: `fetchManifest(endpoints)` tries CloudFront first, S3 fallback. `resolveDownloadURL()` handles relative vs absolute URLs.
- **Android `VpnService.protect()` scope**: Must protect wire transport (QUIC UDP, TCP-WS TCP), direct DNS (raw UDP), and direct tunnel connections (smart routing bypass). Uses `syscall.RawConn.Control()` in Go's `net.Dialer.Control`. gomobile requires `int32` fd parameter (not `int`).
- **iOS log level is cross-process**: `K2Plugin.setLogLevel()` only logs — NE runs in separate process. Level applied via `configJSON.log.level` on next `startVPNTunnel`. Android plugin and VPN service share a process so it takes effect immediately.
- **Mobile auto-update on cold start**: K2Plugin checks for updates on `load()` (plugin initialization) — every app launch, no explicit trigger needed.
- **VPN display name**: User-visible VPN name is `"kaitu.io"` across iOS (NE `localizedDescription`, `serverAddress`, Info.plist `CFBundleDisplayName`) and Android (`setSession()`, notification title).
- **iOS stale VPN config cleanup**: `loadVPNManager()` removes stale NE configs with wrong `providerBundleIdentifier` or `localizedDescription` on every load. Prevents "Found 0 registrations" after bundle ID migration.
- **iOS App Group**: `kAppGroup = "group.io.kaitu"` — used by both `K2Plugin.swift` and `PacketTunnelProvider.swift`. Changed from `group.waymaker` in March 2026.
- **Web OTA min_native**: Manifest `min_native` field prevents applying webapp that requires a newer native app. Source: `webapp/package.json` → `minNativeVersion`. Bump this when webapp adds new native bridge dependencies. Comparison uses BASE version only (ignores pre-release): `0.4.0-beta.6` satisfies `min_native=0.4.0`.
- **Web OTA boot verification**: `.boot-pending` marker in `web-update/` dir. Created on OTA apply, cleared by `checkReady()`. If present on cold start → OTA crashed → rollback to bundled webapp.

## AEO Constitutional Rules (App Store Optimization)

Optimizing for App Store discoverability. These rules apply to all App Store Connect submissions.

- **Name + Subtitle + Keywords are one system**: Never duplicate words across name, subtitle, and keyword fields. Apple indexes all three together — duplication wastes character budget.
- **Cross-locale keyword strategy**: China storefront indexes both zh-Hans AND en-US fields. Use zh-Hans for Chinese intent words (加速器, 翻墙, 科学上网), en-US for English competitor names (shadowsocks, clash, v2ray, surge). Zero overlap between the two.
- **100-character keyword budget**: Keywords are comma-separated, no spaces after commas. Every character counts. Prioritize by search volume × relevance. Drop low-conversion terms ruthlessly.
- **First 3 lines of description**: App Store folds description after ~3 lines. Core value proposition must be above the fold. Lead with user benefit, not feature list.
- **Screenshots convert**: First 3 screenshots determine install rate. Screenshot 1 must show core function + brand slogan overlay. Use device frames. Localize screenshot text per storefront.
- **Version updates = keyword refresh opportunity**: Every new version submission is a chance to A/B test keyword changes. Track keyword rankings before and after.
- **No "VPN" in user-visible text**: Apple flags VPN-related terminology. Use "network accelerator", "secure tunnel", "proxy" instead. Internal JSON keys (e.g., `"vpn"` namespace) are fine — only user-facing strings matter.
- **Review notes template**: Always include: app category justification, demo account credentials (if subscription), explanation of network extension usage.
- **Privacy nutrition labels**: Keep privacy declarations current with actual data collection. Discrepancies trigger review rejection.
- **Custom Product Pages**: Create locale-specific pages for different traffic sources (organic search vs social vs ads) when budget allows.

## Cross-Layer Conventions

- **Go→JS JSON key convention**: Go `json.Marshal` outputs snake_case. JS/TS expects camelCase. Native bridge layers (`K2Plugin.swift` / `K2Plugin.kt`) must remap at the boundary before forwarding to the webapp.
- **`.gitignore` for native platforms**: Never ignore entire source directories (`mobile/ios/`, `mobile/android/`). Only ignore build artifacts.

## Android APK Signing

Keystore at `mobile/android/app/kaitu-release.jks.enc` (AES-256-CBC encrypted).

```bash
make decrypt-keystore    # Requires KAITU_ANDROID_STORE_PASSWORD env var (also GH secret)
```

- Alias: `kaitu`, RSA 2048
- Gradle `signingConfigs.release` reads the password from the same env var

## Android S3 CDN Structure

`d13jc1jqzlg4yt.cloudfront.net/kaitu/android/`:

- `latest.json` — stable APK manifest
- `beta/latest.json` — beta channel
- `tools/tools.json` — adb binaries

`scripts/publish-mobile.sh` always updates the stable `android/latest.json` since the Android install flow reads the stable channel.

## S3 Log Upload (Mobile)

Feedback uploads use bundle zip with unique feedbackId key: `mobile/{version}/{udid}/{date}/logs-{ts}-{id}.zip`. Only feedback path — mobile has no beta auto-upload equivalent. Legacy prefixes (`service-logs/` / `feedback-logs/`) still supported by Lambda.

## Related Docs

- [Root Architecture](../CLAUDE.md)
- [Webapp Frontend](../webapp/CLAUDE.md) — Shared UI running inside Capacitor WebView
- [Go Core / appext](../k2/appext/CLAUDE.md) — gomobile engine wrapper
- [Engine](../k2/engine/CLAUDE.md) — Unified tunnel lifecycle
