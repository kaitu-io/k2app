# macOS System Extension Debug Steps

## Environment
- macOS 14.5, arm64, SIP disabled (Safe Mode)
- App at `/Applications/Kaitu.app` (Developer ID signed)
- Sysext `io.kaitu.desktop.tunnel (1.0.2/9)` activated

## Verified Sysext Deployment Workflow

### Prerequisites
```bash
SIGN_IDENTITY="Developer ID Application: ALL NATION CONNECT TECHNOLOGY PTE. LTD. (NJT954Q3RH)"
ROOT_DIR="/Users/david/projects/kaitu-io/k2app"
SYSEXT_PATH="Contents/Library/SystemExtensions/io.kaitu.desktop.tunnel.systemextension"
```

### Steps (repeat for each iteration)

1. **Kill app**
```bash
pkill -f "k2app"; sleep 2
```

2. **Copy to /tmp (user-writable, needed for signing without sudo)**
```bash
rm -rf /tmp/Kaitu.app
cp -R /Applications/Kaitu.app /tmp/Kaitu.app
```

3. **Replace sysext binary** (rebuild with gomobile if Go code changed)
```bash
# If Go code changed:
cd $ROOT_DIR/k2 && make mobile-macos  # or manual gomobile bind
# Then rebuild Swift:
# swiftc PacketTunnelProvider.swift ... -o /tmp/KaituTunnel
cp /tmp/KaituTunnel "/tmp/Kaitu.app/$SYSEXT_PATH/Contents/MacOS/KaituTunnel"
```

4. **Bump CFBundleVersion** (required for sysext replacement detection)
```bash
/usr/libexec/PlistBuddy -c "Set CFBundleVersion N" "/tmp/Kaitu.app/$SYSEXT_PATH/Contents/Info.plist"
# Increment N each time (was 9 last)
```

5. **Clean and sign** (order matters: k2 sidecar → sysext → main app)
```bash
find /tmp/Kaitu.app -name ".DS_Store" -delete

# k2 sidecar — runtime only, NO NE entitlements
codesign --force --sign "$SIGN_IDENTITY" --options runtime \
  "/tmp/Kaitu.app/Contents/MacOS/k2"

# Sysext — needs NE entitlements
codesign --force --sign "$SIGN_IDENTITY" \
  --entitlements "$ROOT_DIR/desktop/src-tauri/KaituTunnel/KaituTunnel.entitlements" \
  --options runtime \
  "/tmp/Kaitu.app/$SYSEXT_PATH"

# Main app — needs NE entitlements
codesign --force --sign "$SIGN_IDENTITY" \
  --entitlements "$ROOT_DIR/desktop/src-tauri/entitlements.plist" \
  --options runtime \
  "/tmp/Kaitu.app"

codesign --verify --deep --strict "/tmp/Kaitu.app"
```

6. **Deploy**
```bash
sudo rm -rf /Applications/Kaitu.app
sudo cp -R /tmp/Kaitu.app /Applications/Kaitu.app
```

7. **Launch and test**
```bash
open /Applications/Kaitu.app
systemextensionsctl list 2>&1 | grep kaitu
# Should show: [activated enabled] with new version
```

### Critical Signing Rules
- **Never `--deep` codesign** — AMFI validates each binary independently
- **Never sign with `sudo`** — root can't access login keychain (`errSecInternalComponent`)
- **Sign in /tmp, copy to /Applications** — avoids keychain access issues
- **k2 sidecar gets NO NE entitlements** — only runtime, otherwise AMFI rejects
- **Always `rm -rf /tmp/Kaitu.app` before copying** — prevents nested `.app` directory

## Testing VPN Connection

### Via MCP Bridge (debug.html)
```js
const { invoke } = window.__TAURI__.core;

// Connect
await invoke('daemon_exec', {
  action: 'up',
  params: {
    server: 'k2v5://...',
    dns: { proxy: ['198.18.0.7:53'], direct: ['223.5.5.5:53', '114.114.114.114:53'] },
    rule: { global: true }
  }
});

// Status
await invoke('daemon_exec', { action: 'status', params: {} });

// Disconnect
await invoke('daemon_exec', { action: 'down', params: {} });
```

### Via CLI
```bash
scutil --nc status "Kaitu VPN"   # NE tunnel state
curl https://api.ipify.org       # External IP verification
ifconfig utun19                  # TUN interface details
```

### Diagnostic Logs
```bash
# Go engine stderr (panic output, slog)
sudo cat "/private/var/root/Library/Group Containers/group.io.kaitu.desktop/go_stderr.log"

# Swift NE logs (NSLog)
log show --predicate 'process == "KaituTunnel"' --last 5m --style compact

# configJSON dump (post-mortem)
sudo cat "/private/var/root/Library/Group Containers/group.io.kaitu.desktop/diag_configJSON.txt"
```

## Bugs Found & Fixed

### Bug 1: InterfaceMonitor nil panic (SIGABRT)
- **Symptom**: Engine crashes on `engine.start(fd=N)` with SIGABRT
- **Root Cause**: sing-tun `NativeTun.Start()` unconditionally calls `InterfaceMonitor.RegisterMyInterface()`. macOS sysext uses desktop TUN code (`darwin && !ios` build tag) but gets FD from NE (mobile path). The FD path in `engine.go` didn't provide InterfaceMonitor.
- **Fix 1**: `k2/provider/tun_desktop.go` — create minimal `DefaultInterfaceMonitor` when `FileDescriptor > 0` and no monitor provided:
  ```go
  } else if p.cfg.FileDescriptor > 0 {
      netMon, _ := tun.NewNetworkUpdateMonitor(nil)
      ifMon, _ := tun.NewDefaultInterfaceMonitor(netMon, nil, tun.DefaultInterfaceMonitorOptions{
          UnderNetworkExtension: true,
      })
      tunOpts.InterfaceMonitor = ifMon
  }
  ```
- **Fix 2**: `k2/engine/engine.go` — pass `InterfaceMonitor` in the FD provider config

### Bug 2: TUN IP address mismatch (bind error)
- **Symptom**: `listen tcp4 198.18.0.7:0: bind: can't assign requested address`
- **Root Cause**: Swift NE default IPs (`10.0.0.2/24`, `fd00::2/64`) differ from Go defaults (`198.18.0.7/15`, `fdfe:dcba:9876::7/64`). NE creates utun with 10.0.0.2, Go stack tries to bind to 198.18.0.7.
- **Fix**: Updated `PacketTunnelProvider.swift` defaults to match Go's `config.DefaultTunIPv4/IPv6`

### Bug 3: freopen path (early iterations)
- **Symptom**: `go_stderr.log` never created in first attempts
- **Root Cause**: Container path resolution failed when using wrong App Group container path
- **Fix**: Container now correctly resolves to `/private/var/root/Library/Group Containers/group.io.kaitu.desktop/`

## Verified Working State (v9)

```
systemextensionsctl list:
* * NJT954Q3RH io.kaitu.desktop.tunnel (1.0.2/9) Kaitu VPN Tunnel [activated enabled]

scutil --nc status "Kaitu VPN": Connected
  IPv4: 198.18.0.7, utun19, MTU 1400
  IPv6: fdfe:dcba:9876::7/64
  DNS: 198.18.0.7, matchDomains: [""]
  IsPrimaryInterface: 1

Engine log sequence:
  configJSON from options → Engine created: ok → TUN fd via utun scan: 9
  → engine.start(fd=9) → Engine started successfully → Network path monitor started
  → QUIC client K2ARC enabled → DNS queries flowing

Connect/disconnect/reconnect cycle: all working
```
