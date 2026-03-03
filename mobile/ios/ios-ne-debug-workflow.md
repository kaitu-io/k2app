# iOS Network Extension Debug Workflow

## Quick Reference

| Layer | Tool | Command |
|-------|------|---------|
| TS Bridge | vitest | `cd webapp && npx vitest run src/services/__tests__/capacitor-k2.test.ts` |
| Swift Helpers | XCTest | `xcodebuild test -workspace App.xcworkspace -scheme K2Tests -destination 'platform=iOS Simulator,name=iPhone 16'` |
| Real Device Logs | ios-logs.sh | `./scripts/ios-logs.sh` (stream) or `./scripts/ios-logs.sh --archive` (iOS 18+) |
| Debug State Dump | Safari Inspector | `await Capacitor.Plugins.K2Plugin.debugDump()` |

---

## Xcode: Attach Debugger to NE Process

The PacketTunnelExtension runs in a **separate process**. Xcode doesn't automatically attach to it.

1. Build and run the App on a real device
2. **Debug → Attach to Process by PID or Name → `PacketTunnelExtension`**
3. Set breakpoints in `PacketTunnelProvider.swift`:
   - `startTunnel()` — tunnel lifecycle start
   - `onStatus()` in EventBridge — engine state changes
   - `handleAppMessage()` — IPC from K2Plugin
   - `stopTunnel()` — cleanup
4. Trigger VPN connect from the app UI
5. Xcode should break at `startTunnel()`

**Tip:** You can also attach to `PacketTunnelExtension` BEFORE triggering connect, so you catch the very first entry into `startTunnel()`.

---

## Console.app: Structured Log Filtering

After upgrading to `os.Logger` (subsystem: `io.kaitu`):

### Console.app (GUI)
1. Connect device via USB
2. Open Console.app → select device
3. Filter bar: `subsystem:io.kaitu`
4. Refine by category:
   - `category:NE` — PacketTunnelExtension (NE process)
   - `category:K2Plugin` — Main app process

### CLI (real-time stream)
```bash
log stream --predicate 'subsystem == "io.kaitu"' --style compact
```

### CLI (iOS 18+ archive mode)
```bash
./scripts/ios-logs.sh --archive --since 10m
```

---

## debugDump: One-Shot State Inspection

Via Safari Web Inspector (connect device → Develop → App):
```javascript
const dump = await Capacitor.Plugins.K2Plugin.debugDump();
console.log(JSON.stringify(dump, null, 2));
```

Returns:
| Field | Meaning |
|-------|---------|
| `appGroup` | App Group identifier |
| `containerPath` | App Group container filesystem path |
| `configJSON_exists` | Whether config was written to App Group |
| `configJSON_length` | Config size (0 = missing) |
| `vpnError` | Error string from NE (or "nil") |
| `vpnManager_loaded` | Whether NEVPNManager was loaded |
| `vpnManager_enabled` | Whether VPN profile is enabled |
| `vpnManager_status` | Current NEVPNStatus as string |
| `vpnManager_protoBundleId` | NE bundle identifier |
| `cacheDirExists` | Whether NE cache dir was created |
| `cacheDirContents` | Files in cache dir |
| `webUpdate_exists` | Whether web OTA was applied |
| `webUpdate_version` | Current web OTA version |

---

## Common Problems

### VPN icon doesn't appear
- Check: `debugDump()` → `vpnManager_protoBundleId` should be `io.kaitu.PacketTunnelExtension`
- Check: App entitlements include `packet-tunnel-provider`
- Check: Debug config uses `App.entitlements` (NOT `App.simulator.entitlements`)

### engine.start() fails
- Check NE logs: `category:NE` → look for `"FAILED"` errors
- Common: TUN fd = -1 → `setTunnelNetworkSettings` didn't complete
- Common: cacheDir empty → App Group container not accessible

### Status not syncing (app shows disconnected but VPN is on)
- Check: `debugDump()` → `vpnManager_status` vs what UI shows
- Check: `sendProviderMessage` timeout → NE process might have crashed
- Check: App Group `vpnError` key → stale error not cleared

### Disconnect shows no error message
- Check: `debugDump()` → `vpnError` field
- Check NE logs: `onStatus:` → was `cancelTunnelWithError()` called with error?
- Check: K2Plugin `registerStatusObserver` → is it reading App Group?

### NE process crashes silently
- iOS doesn't show crash alerts for NE extensions
- Check: `log show --predicate 'process == "PacketTunnelExtension" AND eventType == "logEvent" AND messageType == "error"'`
- Check: Xcode → Window → Devices → View Device Logs → filter for PacketTunnelExtension crashes

---

## Testing Layers (No Real Device Needed)

### Layer 1: Swift Pure Functions (Simulator)
```bash
cd mobile/ios/App
xcodebuild test -workspace App.xcworkspace -scheme K2Tests \
  -destination 'platform=iOS Simulator,name=iPhone 16'
```
Tests: `parseIPv4CIDR`, `parseIPv6CIDR`, `stripPort`, `remapStatusKeys`, `mapVPNStatusString`, `isNewerVersion`

### Layer 2: TS Bridge + Contract Tests
```bash
cd webapp && npx vitest run src/services/__tests__/capacitor-k2.test.ts
```
Tests: `transformStatus()`, error synthesis, retrying logic, cross-layer JSON contract

### Layer 3: Go Engine
```bash
cd k2 && go test ./engine/... ./mobile/...
```
Tests: `EngineError`, `ClassifyError()`, `statusJSON()`, lifecycle

---

## Real Device Minimal Verification Checklist

After passing all automated tests, real device only needs:

- [ ] NE Profile installs (VPN icon appears in Settings)
- [ ] TUN fd acquired (log: `Got TUN fd=XX`)
- [ ] Engine starts (log: `engine.start() returned OK`)
- [ ] Traffic routes through tunnel (open a webpage)
- [ ] Disconnect cleans up (VPN icon disappears)
- [ ] Network switch recovers (WiFi → cellular)
- [ ] Error propagates to UI (connect to invalid server)
