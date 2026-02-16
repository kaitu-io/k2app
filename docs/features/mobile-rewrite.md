# Feature Spec: Mobile Rewrite (iOS + Android)

> **Status**: Draft
> **Created**: 2026-02-14
> **Feature**: k2app mobile client on Capacitor + gomobile
> **Parent**: docs/features/k2app-rewrite.md (architecture reference)

## Overview

Build iOS and Android clients for k2app, reusing the same webapp and Capacitor
shell pattern from old kaitu, but replacing Rust UniFFI with **gomobile bind**
for FFI. The webapp is identical to desktop — same build, same VpnClient
interface — only the `NativeVpnClient` implementation and native plugin differ.

## Context

- **Old mobile stack**: kaitu 0.3.22 — Capacitor 6 + Rust UniFFI (xcframework/JNI) + 12-crate workspace
- **New mobile stack**: k2app 0.4.x — Capacitor 6 + gomobile bind (xcframework/AAR) + k2 Go core
- **Key simplification**: Rust UniFFI → gomobile. No UDL files, no cargo-ndk, no complex cross-compilation.
  gomobile auto-generates ObjC/Java bindings from Go interfaces.
- **k2 mobile/ already implemented**: Engine, EventHandler, TUN providers, tests, Makefile targets

## Architecture

### What's Reused from Desktop Spec

The parent spec (`k2app-rewrite.md`) defines the core architecture that
mobile shares. These sections apply directly — **do not duplicate here**:

- **VpnClient interface** (types, factory, subscribe model)
- **Cloud API + Antiblock** (entry URL resolution, JSONP, CDN fallback)
- **Auth flow** (email + code, token management, UDID)
- **Webapp pages** (Dashboard, Servers, Settings, Login)
- **i18n** (zh-CN, en-US)

### What's Mobile-Specific (this spec)

1. NativeVpnClient implementation (Capacitor Plugin calls)
2. K2Plugin — Capacitor native plugin (Swift + Kotlin)
3. iOS PacketTunnelExtension (NE process)
4. Android VpnService (same process)
5. gomobile build pipeline
6. Mobile CI/CD
7. App Store / APK distribution

---

## Decision 1: Capacitor Project Structure

### Version & Framework

| Item | Choice | Rationale |
|------|--------|-----------|
| Capacitor | 6.x | Same as old kaitu, stable, no migration cost |
| Webapp reuse | 100% | Same `webapp/dist/` → Capacitor sync. No mobile-specific UI |
| Plugin name | `k2-plugin` | Single Capacitor plugin for iOS + Android |
| Monorepo workspace | `mobile/` | Added to root `package.json` workspaces |

### Project Structure

```
mobile/
├── package.json               # Capacitor app + deps
├── capacitor.config.ts        # Capacitor configuration
├── ios/
│   └── App/
│       ├── App/               # Main iOS app target
│       │   ├── AppDelegate.swift
│       │   ├── App.entitlements
│       │   ├── App.simulator.entitlements
│       │   └── Info.plist
│       ├── PacketTunnelExtension/  # NE target (separate process)
│       │   ├── PacketTunnelProvider.swift
│       │   ├── PacketTunnelExtension.entitlements
│       │   └── Info.plist
│       ├── Podfile
│       └── App.xcworkspace
├── android/
│   ├── app/
│   │   ├── src/main/
│   │   │   ├── AndroidManifest.xml
│   │   │   └── java/io/kaitu/
│   │   │       ├── MainActivity.kt
│   │   │       └── K2VpnService.kt
│   │   └── build.gradle
│   ├── k2-mobile/             # gomobile AAR wrapper module
│   │   ├── build.gradle
│   │   └── libs/k2mobile.aar
│   ├── build.gradle
│   ├── settings.gradle
│   └── variables.gradle
└── plugins/
    └── k2-plugin/             # Capacitor plugin (both platforms)
        ├── package.json
        ├── src/               # TypeScript definitions
        │   ├── definitions.ts
        │   ├── index.ts
        │   └── web.ts
        ├── ios/
        │   └── Plugin/
        │       ├── K2Plugin.swift
        │       └── K2Plugin.m  # ObjC bridging
        └── android/
            └── src/main/java/io/kaitu/k2plugin/
                └── K2Plugin.kt
```

### capacitor.config.ts

```typescript
import type { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  appId: 'io.kaitu',
  appName: 'Kaitu',
  webDir: '../webapp/dist',         // reuse desktop webapp build
  ios: {
    contentInset: 'always',         // extend to safe area (CSS handles padding)
    allowsLinkPreview: false,
    scrollEnabled: false,
  },
  android: {
    allowMixedContent: true,
    backgroundColor: '#0F0F13',
  },
  plugins: {
    StatusBar: {
      overlaysWebView: true,
      style: 'DARK',
      backgroundColor: '#00000000',
    },
  },
};

export default config;
```

### package.json dependencies

```json
{
  "dependencies": {
    "@capacitor/android": "^6.0.0",
    "@capacitor/app": "^6.0.0",
    "@capacitor/cli": "^6.0.0",
    "@capacitor/core": "^6.0.0",
    "@capacitor/ios": "^6.0.0",
    "@capacitor/status-bar": "^6.0.0",
    "k2-plugin": "file:./plugins/k2-plugin"
  }
}
```

Stripped vs old kaitu: removed browser, clipboard, device, haptics, share plugins.
Only keep what's needed. Add back individually if required.

---

## Decision 2: gomobile FFI Layer

### gomobile vs UniFFI

| Aspect | Old (Rust UniFFI) | New (gomobile bind) |
|--------|-------------------|---------------------|
| Build iOS | cargo build × 3 targets + lipo + xctool | `gomobile bind -target=ios` (one command) |
| Build Android | cargo-ndk × 4 ABIs + JNI copy | `gomobile bind -target=android` (one command) |
| Binding generation | UDL file → Swift/Kotlin code | Auto from Go exports (ObjC/Java) |
| Binary size | ~5-8MB per arch | ~8-12MB per arch (Go runtime) |
| Dev iteration | 2-5 min full rebuild | 1-2 min |
| Complexity | High (UDL, cross-compile toolchain) | Low (single gomobile command) |

### Exposed Go Interface (already in k2/mobile/)

```go
// mobile/mobile.go — already implemented
type Engine struct { ... }

func NewEngine() *Engine
func (e *Engine) SetEventHandler(handler EventHandler)
func (e *Engine) Start(url string, fd int) error
func (e *Engine) Stop() error
func (e *Engine) Status() string      // "disconnected" | "connecting" | "connected"
func (e *Engine) StatusJSON() string  // rich JSON: state, connected_at, uptime_seconds, error, wire_url

// mobile/event.go — already implemented
type EventHandler interface {
    OnStateChange(state string)
    OnError(message string)
    OnStats(txBytes, rxBytes int64)
}
```

### No MobileAPI Wrapper Needed

The original k2app-rewrite spec proposed a `MobileAPI` Go wrapper with
`CoreAction()` and `HandleProviderMessage()`. After analysis, this is
**unnecessary** — the Capacitor Plugin calls Engine methods directly, and
iOS NE message routing is trivially done in Swift:

| Original MobileAPI method | Replaced by |
|---------------------------|-------------|
| `CoreAction("up", params)` | `Engine.Start(url, fd)` directly (fd comes from native, can't pass through JSON dispatcher) |
| `CoreAction("down", "")` | `Engine.Stop()` directly |
| `CoreAction("status", "")` | `Engine.StatusJSON()` directly |
| `HandleProviderMessage(data)` | Swift code in NE (~5 lines, see iOS section) |
| `Engine.SetUDID(udid)` | Not needed (UDID handled by Capacitor Plugin → webapp, not Go engine) |

### Engine.StatusJSON() — Required k2 Repo Change

**Problem**: Webapp's `getStatus()` needs rich status (state + connectedAt +
uptimeSeconds + wireUrl + error), but Engine.Status() only returns a bare
state string like `"connected"`. This breaks the **cold start scenario** —
user opens app with VPN already running in background (iOS NE / Android
VpnService), webapp needs full context, not just a state word.

**Solution**: Add `Engine.StatusJSON() string` to k2 `mobile/mobile.go`.
Engine already internally tracks state; it needs to additionally record
`wireUrl` (from Start call) and `connectedAt` (when state becomes connected).

```go
// k2/mobile/mobile.go — additions (~30 lines)
type Engine struct {
    // ... existing fields
    wireUrl     string     // saved from Start(url, fd)
    connectedAt time.Time  // set when state becomes "connected"
    lastError   string     // set on error
}

// StatusJSON returns rich status as JSON, matching daemon /api/core action:status format.
func (e *Engine) StatusJSON() string {
    e.mu.Lock()
    defer e.mu.Unlock()
    uptimeSeconds := 0
    connectedAt := ""
    if e.state == StateConnected && !e.connectedAt.IsZero() {
        uptimeSeconds = int(time.Since(e.connectedAt).Seconds())
        connectedAt = e.connectedAt.Format(time.RFC3339)
    }
    data := map[string]interface{}{
        "state":          e.state,
        "connected_at":   connectedAt,
        "uptime_seconds": uptimeSeconds,
        "error":          e.lastError,
        "wire_url":       e.wireUrl,
    }
    b, _ := json.Marshal(data)
    return string(b)
}
```

This is the **only k2 repo change needed** for mobile. The response format
aligns with the daemon's `POST /api/core action:status` response, so the
webapp can use the same type mapping for both desktop and mobile.

### Build Commands

```makefile
# In k2/ repo Makefile (already exists)
mobile-ios:
    gomobile bind -target=ios -o build/K2Mobile.xcframework ./mobile/

mobile-android:
    gomobile bind -target=android -o build/k2mobile.aar -androidapi 24 ./mobile/
```

### Build Output

| Platform | Artifact | Location | Contents |
|----------|----------|----------|----------|
| iOS | K2Mobile.xcframework | k2/build/ | ObjC headers + static libs (device + sim) |
| Android | k2mobile.aar | k2/build/ | Java classes + .so libs (arm64, armv7, x86_64) |

xcframework is copied to `mobile/ios/App/` (Xcode references it).
AAR is copied to `mobile/android/k2-mobile/libs/`.

---

## Decision 3: iOS Implementation

### Identity & Certificates (reuse from kaitu)

| Item | Value |
|------|-------|
| Bundle ID (app) | `io.kaitu` |
| Bundle ID (NE) | `io.kaitu.PacketTunnelExtension` |
| App Group | `group.io.kaitu` |
| Team | Wordgate LLC (NJT954Q3RH) |
| Min iOS | 16.0 (up from 13.0 — drop legacy, gain modern APIs) |
| Signing | Existing certs in Apple Developer account |

All provisioning profiles already exist from old kaitu App Store submission.

### App Entitlements

```xml
<!-- App.entitlements -->
<dict>
    <key>com.apple.developer.networking.networkextension</key>
    <array><string>packet-tunnel-provider</string></array>
    <key>com.apple.security.application-groups</key>
    <array><string>group.io.kaitu</string></array>
</dict>
```

Simulator entitlements: App Group only (no NE — simulator limitation).

### iOS Architecture

```
┌────────────── Main App Process ──────────────┐
│                                                │
│  Capacitor + Webapp (same as desktop)          │
│       │ NativeVpnClient                        │
│       ▼                                        │
│  K2Plugin (Swift, Capacitor Plugin)            │
│       │                                        │
│       ├── checkReady() → { ready: true }       │  always local
│       ├── getUDID() → UIDevice.identifierForVendor
│       ├── getVersion() → Bundle.main.version   │
│       │                                        │
│       ├── connect(wireUrl)                     │
│       │   → NEVPNManager.loadAllFrom...        │
│       │   → save wireUrl to protocolConfig     │
│       │   → startVPNTunnel()                   │
│       │   → resolve (command sent, async)      │
│       │                                        │
│       ├── disconnect()                         │
│       │   → connection.stopVPNTunnel()         │
│       │                                        │
│       ├── getStatus()                          │
│       │   → sendProviderMessage("status")      │
│       │   → timeout 5s → fallback to           │
│       │     NEVPNConnection.status mapping      │
│       │                                        │
│       └── subscribe(listener)                  │
│           → NotificationCenter.observe          │
│             NEVPNStatusDidChange               │
│           → on change: map status → VpnEvent   │
│           → notifyListeners("vpnStateChange")  │
│                                                │
└──────────────────┬─────────────────────────────┘
                   │ NEVPNManager IPC
                   ▼
┌────────────── NE Process ────────────────────┐
│                                                │
│  PacketTunnelProvider (Swift)                   │
│       │                                        │
│  startTunnel(options:)                         │
│       ├── extract wireUrl from options          │
│       ├── engine = MobileNewEngine()            │
│       ├── engine.setEventHandler(self)          │
│       ├── fd = packetFlow.value(forKey: "socket")
│       ├── engine.start(wireUrl, fd)             │
│       └── fulfillment handler                  │
│                                                │
│  stopTunnel(reason:)                           │
│       └── engine.stop()                        │
│                                                │
│  handleAppMessage(data:) → Data?               │
│       ├── "status" → engine.status() → reply   │
│       └── unknown → nil                        │
│                                                │
│  EventHandler conformance:                     │
│       ├── onStateChange(state) → save to       │
│       │   UserDefaults(group.io.kaitu)         │
│       └── onError(msg) → save + post           │
│           CFNotificationCenter (Darwin)        │
│                                                │
└────────────────────────────────────────────────┘
```

### iOS NE Communication

| Mechanism | Direction | Use |
|-----------|-----------|-----|
| `NEVPNManager.startVPNTunnel(options:)` | App → NE | Start VPN, pass wire_url |
| `connection.stopVPNTunnel()` | App → NE | Stop VPN |
| `sendProviderMessage()` | App → NE → App | Status query RPC (≤1MB, 30s timeout) |
| `NEVPNStatusDidChange` notification | System → App | Coarse state changes |
| Darwin Notification | NE → App | Custom event signal |
| App Group UserDefaults | NE ↔ App | Shared data (last wire_url, rich status) |

### Podfile

```ruby
platform :ios, '16.0'
use_frameworks! :linkage => :static

target 'App' do
  # Capacitor pods (auto-generated by cap sync)
  pod 'Capacitor', :path => '../../node_modules/@capacitor/ios'
  pod 'CapacitorCordova', :path => '../../node_modules/@capacitor/ios'
  pod 'CapacitorApp', :path => '../../node_modules/@capacitor/app'
  pod 'CapacitorStatusBar', :path => '../../node_modules/@capacitor/status-bar'
  pod 'K2Plugin', :path => '../../plugins/k2-plugin'
end

target 'PacketTunnelExtension' do
  use_frameworks! :linkage => :static
  # NE target only needs gomobile xcframework — NO Capacitor
end
```

K2Mobile.xcframework is added directly to Xcode project (both targets need it,
but only NE uses it at runtime for Engine calls).

---

## Decision 4: Android Implementation

### Identity & SDK Versions

| Item | Value |
|------|-------|
| Package ID | `io.kaitu` |
| Min SDK | 24 (Android 7.0) |
| Target SDK | 35 (Android 15) |
| Compile SDK | 35 |
| Java | 17 |
| Kotlin | 1.9.x |
| ABIs | arm64-v8a, armeabi-v7a, x86_64 |

### Android Architecture

```
┌──────────────── App Process (single) ────────────────┐
│                                                        │
│  Capacitor + Webapp (same as desktop)                  │
│       │ NativeVpnClient                                │
│       ▼                                                │
│  K2Plugin (Kotlin, Capacitor Plugin)                   │
│       │                                                │
│       ├── checkReady() → { ready: true }               │
│       ├── getUDID() → Settings.Secure.ANDROID_ID       │
│       ├── getVersion() → PackageInfo.versionName       │
│       │                                                │
│       ├── connect(wireUrl)                             │
│       │   → save wireUrl to intent extra               │
│       │   → startService(K2VpnService)                 │
│       │   → resolve on service started                 │
│       │                                                │
│       ├── disconnect()                                 │
│       │   → send stop intent to K2VpnService           │
│       │   → OR engine.stop() directly                  │
│       │                                                │
│       ├── getStatus()                                  │
│       │   → engine.status() (direct, same process)     │
│       │                                                │
│       └── subscribe(listener)                          │
│           → EventHandler → notifyListeners()           │
│                                                        │
│  K2VpnService (Kotlin, extends VpnService)             │
│       │                                                │
│       ├── onCreate()                                   │
│       │   → engine = Mobile.newEngine()                │
│       │   → engine.setEventHandler(eventBridge)        │
│       │                                                │
│       ├── onStartCommand(intent)                       │
│       │   → wireUrl from intent extra                  │
│       │   → builder = Builder()                        │
│       │   → builder.addAddress("10.0.0.2", 32)        │
│       │   → builder.addDnsServer("1.1.1.1")           │
│       │   → builder.addRoute("0.0.0.0", 0)            │
│       │   → pfd = builder.establish()                  │
│       │   → engine.start(wireUrl, pfd.fd)              │
│       │   → startForeground(notification)              │
│       │                                                │
│       ├── onRevoke()                                   │
│       │   → engine.stop()                              │
│       │   → stopForeground()                           │
│       │                                                │
│       └── EventBridge: EventHandler                    │
│           → onStateChange → K2Plugin.notifyListeners() │
│           → onError → K2Plugin.notifyListeners()       │
│                                                        │
└────────────────────────────────────────────────────────┘
```

### AndroidManifest.xml

```xml
<manifest xmlns:android="http://schemas.android.com/apk/res/android">

  <uses-permission android:name="android.permission.INTERNET" />
  <uses-permission android:name="android.permission.ACCESS_NETWORK_STATE" />
  <uses-permission android:name="android.permission.FOREGROUND_SERVICE" />
  <uses-permission android:name="android.permission.FOREGROUND_SERVICE_SPECIAL_USE" />
  <uses-permission android:name="android.permission.POST_NOTIFICATIONS" />

  <application ...>
    <activity android:name=".MainActivity"
              android:launchMode="singleTask"
              android:exported="true">
      <intent-filter>
        <action android:name="android.intent.action.MAIN" />
        <category android:name="android.intent.category.LAUNCHER" />
      </intent-filter>
    </activity>

    <service android:name="io.kaitu.K2VpnService"
             android:exported="false"
             android:foregroundServiceType="specialUse"
             android:permission="android.permission.BIND_VPN_SERVICE">
      <intent-filter>
        <action android:name="android.net.VpnService" />
      </intent-filter>
      <property android:name="android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE"
                android:value="vpn" />
    </service>
  </application>
</manifest>
```

### Android k2-mobile Module

```gradle
// android/k2-mobile/build.gradle
apply plugin: 'com.android.library'

android {
    namespace "io.kaitu.k2mobile"
    compileSdk 35
    defaultConfig {
        minSdk 24
        targetSdk 35
    }
}

dependencies {
    api files('libs/k2mobile.aar')
}
```

The AAR from gomobile contains Java classes + JNI .so files for all ABIs.
The k2-mobile module wraps it as an Android library that app and plugin depend on.

---

## NativeVpnClient (webapp side)

File: `webapp/src/vpn-client/native-client.ts`

```typescript
import { K2Plugin } from 'k2-plugin';
import type { VpnClient, VpnStatus, VpnEvent, ReadyState, VersionInfo, VpnConfig } from './types';

export class NativeVpnClient implements VpnClient {
  private listeners = new Set<(event: VpnEvent) => void>();
  private pluginListener: any = null;

  async connect(wireUrl: string): Promise<void> {
    await K2Plugin.connect({ wireUrl });
  }

  async disconnect(): Promise<void> {
    await K2Plugin.disconnect();
  }

  async checkReady(): Promise<ReadyState> {
    const result = await K2Plugin.checkReady();
    return result;
  }

  async getStatus(): Promise<VpnStatus> {
    const result = await K2Plugin.getStatus();
    return result;
  }

  async getVersion(): Promise<VersionInfo> {
    return K2Plugin.getVersion();
  }

  async getUDID(): Promise<string> {
    const { udid } = await K2Plugin.getUDID();
    return udid;
  }

  async getConfig(): Promise<VpnConfig> {
    return K2Plugin.getConfig();
  }

  subscribe(listener: (event: VpnEvent) => void): () => void {
    this.listeners.add(listener);
    if (!this.pluginListener) {
      this.pluginListener = K2Plugin.addListener('vpnStateChange', (event: any) => {
        const vpnEvent: VpnEvent = { type: 'state_change', state: event.state };
        this.listeners.forEach(l => l(vpnEvent));
      });
      K2Plugin.addListener('vpnError', (event: any) => {
        const vpnEvent: VpnEvent = { type: 'error', message: event.message };
        this.listeners.forEach(l => l(vpnEvent));
      });
    }
    return () => {
      this.listeners.delete(listener);
      if (this.listeners.size === 0) {
        this.pluginListener?.remove();
        this.pluginListener = null;
      }
    };
  }

  destroy(): void {
    this.pluginListener?.remove();
    this.pluginListener = null;
    this.listeners.clear();
  }
}
```

### VpnClient Factory Update

```typescript
// webapp/src/vpn-client/index.ts — update createVpnClient
import { Capacitor } from '@capacitor/core';

export function createVpnClient(override?: VpnClient): VpnClient {
  if (override) { instance = override; return override; }
  if (!instance) {
    if (Capacitor.isNativePlatform()) {
      instance = new NativeVpnClient();
    } else {
      instance = new HttpVpnClient();
    }
  }
  return instance;
}
```

---

## K2Plugin Capacitor Interface

### TypeScript Definitions

```typescript
// plugins/k2-plugin/src/definitions.ts
export interface K2PluginInterface {
  checkReady(): Promise<{ ready: boolean; version?: string; reason?: string }>;
  getUDID(): Promise<{ udid: string }>;
  getVersion(): Promise<{ version: string; go: string; os: string; arch: string }>;
  getStatus(): Promise<{ state: string; connectedAt?: string; error?: string; wireUrl?: string }>;
  getConfig(): Promise<{ wireUrl?: string }>;
  connect(options: { wireUrl: string }): Promise<void>;
  disconnect(): Promise<void>;

  addListener(eventName: 'vpnStateChange', handler: (data: { state: string }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'vpnError', handler: (data: { message: string }) => void): Promise<PluginListenerHandle>;
}
```

---

## Build Flow

### Mobile Release

```
1. cd webapp && yarn build              # webapp → webapp/dist/
2. cd k2 && gomobile bind -target=ios   # → K2Mobile.xcframework
3. cd k2 && gomobile bind -target=android # → k2mobile.aar
4. cp k2/build/K2Mobile.xcframework mobile/ios/App/
5. cp k2/build/k2mobile.aar mobile/android/k2-mobile/libs/
6. cd mobile && npx cap sync            # webapp → iOS + Android native projects
7. cd mobile/ios/App && xcodebuild archive  # iOS IPA
8. cd mobile/android && ./gradlew assembleRelease  # Android APK
```

### Makefile Targets (k2app root)

```makefile
build-mobile-ios: build-webapp
    cd k2 && gomobile bind -target=ios -o build/K2Mobile.xcframework ./mobile/
    cp -r k2/build/K2Mobile.xcframework mobile/ios/App/
    cd mobile && npx cap sync ios
    cd mobile/ios/App && xcodebuild -workspace App.xcworkspace \
        -scheme App -configuration Release \
        -destination 'generic/platform=iOS' \
        -archivePath build/App.xcarchive archive

build-mobile-android: build-webapp
    cd k2 && gomobile bind -target=android -o build/k2mobile.aar -androidapi 24 ./mobile/
    cp k2/build/k2mobile.aar mobile/android/k2-mobile/libs/
    cd mobile && npx cap sync android
    cd mobile/android && ./gradlew assembleRelease

dev-ios: build-webapp
    cd mobile && npx cap sync ios && npx cap run ios

dev-android: build-webapp
    cd k2 && gomobile bind -target=android -o build/k2mobile.aar -androidapi 24 ./mobile/
    cp k2/build/k2mobile.aar mobile/android/k2-mobile/libs/
    cd mobile && npx cap sync android && npx cap run android
```

---

## Distribution

### App Store (iOS)

Already compliant from old kaitu. Reuse:
- Apple Developer account (Wordgate LLC, NJT954Q3RH)
- App ID: `io.kaitu`
- NE entitlement: approved
- Existing App Store listing (update with new build)
- Existing provisioning profiles

Release flow:
1. xcodebuild archive → .xcarchive
2. xcodebuild export → .ipa
3. xcrun altool upload (or Transporter app)
4. App Store Connect review

### Google Play (Android) — Deferred

**Cost assessment**:
- $25 one-time developer registration fee
- VPN apps require additional review + privacy policy
- Data Safety form required
- Google Play blocked in China (primary user base)
- Most China users install via APK sideloading or local app stores

**Recommendation**: Defer Google Play. Primary distribution via:
1. **APK download** from kaitu.io website (signed release APK)
2. **TestFlight** for iOS beta testing
3. **App Store** for iOS production
4. Google Play added later if international user base grows

---

## CI/CD

### Mobile Build Workflow

```yaml
# .github/workflows/build-mobile.yml
name: Build Mobile
on:
  workflow_dispatch:
    inputs:
      platform:
        type: choice
        options: [ios, android, both]

jobs:
  build-ios:
    runs-on: macos-latest
    if: inputs.platform == 'ios' || inputs.platform == 'both'
    steps:
      - uses: actions/checkout@v4
        with: { submodules: recursive }
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - uses: actions/setup-go@v5
        with: { go-version: '1.24' }
      - run: go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init
      - run: yarn install --frozen-lockfile
      - run: make build-mobile-ios
      - uses: actions/upload-artifact@v4
        with:
          name: ios-build
          path: mobile/ios/App/build/

  build-android:
    runs-on: ubuntu-latest
    if: inputs.platform == 'android' || inputs.platform == 'both'
    steps:
      - uses: actions/checkout@v4
        with: { submodules: recursive }
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - uses: actions/setup-go@v5
        with: { go-version: '1.24' }
      - uses: actions/setup-java@v4
        with: { distribution: temurin, java-version: '17' }
      - uses: android-actions/setup-android@v3
      - run: go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init
      - run: yarn install --frozen-lockfile
      - run: make build-mobile-android
      - uses: actions/upload-artifact@v4
        with:
          name: android-build
          path: mobile/android/app/build/outputs/apk/
```

---

## Platform Behavior Summary

| Aspect | Desktop (HttpVpnClient) | iOS (NativeVpnClient) | Android (NativeVpnClient) |
|--------|------------------------|-----------------------|---------------------------|
| Backend | k2 daemon HTTP :1777 | NEVPNManager IPC → NE → Engine | Direct gomobile → Engine |
| connect() | Sync (HTTP 200) | Async (command sent) | Sync (Engine.Start blocks) |
| Status | HTTP poll → event | sendProviderMessage + fallback | Engine.Status() direct |
| Events | Internal poll (2s) | NEVPNStatusDidChange + Darwin | EventHandler → notifyListeners |
| UDID | daemon /api/device/udid | UIDevice.identifierForVendor | Settings.Secure.ANDROID_ID |
| TUN fd | N/A (daemon manages) | System provides to NE | VpnService.establish() |
| Process model | Daemon (separate) | NE (separate process) | Same process |

---

## k2 Repo Dependencies

| Change | Status | Priority |
|--------|--------|----------|
| mobile/mobile.go (Engine) | Done | - |
| mobile/event.go (EventHandler) | Done | - |
| provider/tun_ios.go | Done | - |
| provider/tun_android.go | Done | - |
| Makefile mobile targets | Done | - |
| Engine.StatusJSON() | **Done** (k2 commit aa63238) | P0 |
| Daemon 3-state simplification | **Done** (k2 commit bfeb06c) | - |

---

## Acceptance Criteria

### Webapp
- [ ] NativeVpnClient implementation calls K2Plugin Capacitor methods
- [ ] createVpnClient() returns NativeVpnClient on native platform
- [ ] All UI pages work identically on mobile (no mobile-specific UI code)

### K2Plugin (Capacitor)
- [ ] checkReady() returns { ready: true } on both platforms
- [ ] getUDID() returns platform-specific device ID
- [ ] connect(wireUrl) starts VPN tunnel
- [ ] disconnect() stops VPN tunnel
- [ ] getStatus() returns current VPN state
- [ ] subscribe delivers vpnStateChange and vpnError events to webapp

### iOS
- [ ] PacketTunnelExtension starts Engine with fd from system
- [ ] handleAppMessage routes "status" to Engine.Status()
- [ ] NEVPNStatusDidChange events propagated to webapp
- [ ] App Group UserDefaults used for NE ↔ App shared state
- [ ] Codesign valid, entitlements correct
- [ ] App Store submission succeeds (update existing listing)

### Android
- [ ] K2VpnService starts with foreground notification
- [ ] VpnService.Builder.establish() provides fd to Engine
- [ ] EventHandler callbacks propagated to webapp via notifyListeners
- [ ] APK builds and installs on arm64 and armv7 devices
- [ ] VPN permission dialog shown on first connect

### Build
- [ ] `gomobile bind -target=ios` produces K2Mobile.xcframework
- [ ] `gomobile bind -target=android` produces k2mobile.aar
- [ ] `make build-mobile-ios` produces .xcarchive
- [ ] `make build-mobile-android` produces signed APK
- [ ] CI workflow builds both platforms successfully
