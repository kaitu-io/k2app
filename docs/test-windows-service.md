# Windows Service Test Matrix

Test environment: Parallels ARM64 VM, x86_64 emulated, Windows 11.

## Prerequisites

```powershell
# Build + deploy
make build-windows-test           # Remote build on Windows VM
# OR with auto-deploy:
bash scripts/build-windows-test.sh --deploy

# SSH tunnel for Tauri MCP
ssh -N -L 19223:127.0.0.1:9223 david@10.211.55.6
# Then: driver_session(action='start', port=19223)
```

## Debug Commands

```powershell
sc query k2                              # Windows Service status
sc qc k2                                 # Service config (binary path, start type)
netstat -an | findstr 1777               # Daemon port
tasklist | findstr "k2\|Kaitu"           # Process list
curl.exe http://127.0.0.1:1777/ping      # Daemon health
curl.exe -X POST http://127.0.0.1:1777/api/core -d "{\"action\":\"version\"}"
curl.exe -X POST http://127.0.0.1:1777/api/core -d "{\"action\":\"status\"}"
# Tauri app logs
type "%LOCALAPPDATA%\io.kaitu.desktop\logs\k2app.log"
# k2 daemon logs
type "%ProgramFiles%\Kaitu\logs\k2.log"
```

## Test Scenarios

### 1. Fresh Install

**Precondition**: No previous Kaitu installation. Remove if exists:
```powershell
sc stop k2 2>NUL & sc delete k2 2>NUL
taskkill /F /IM Kaitu.exe 2>NUL & taskkill /F /IM k2.exe 2>NUL
rmdir /S /Q "C:\Program Files\Kaitu" 2>NUL
```

**Steps**:
1. Run NSIS installer
2. App launches after install
3. App calls `ensure_service_running` → detects `ServiceNotRunning`
4. Waits 8s (NSIS may have started the service)
5. If still not running → shows UAC dialog → `k2.exe service install`
6. Service starts on :1777

**Verify**:
- [ ] `sc query k2` → RUNNING
- [ ] `curl 127.0.0.1:1777/ping` → `{"code":0}`
- [ ] SSE status stream connected (check Tauri logs)
- [ ] Webapp shows dashboard (no ServiceAlert)

### 2. Upgrade (version mismatch)

**Precondition**: Previous version installed and running.

**Steps**:
1. Run new NSIS installer (replaces binaries)
2. App detects version mismatch via `check_service_version()`
3. Immediately triggers `admin_reinstall_service` → UAC → `k2.exe service install`
4. New daemon starts

**Verify**:
- [ ] `curl .../api/core -d '{"action":"version"}'` → new version
- [ ] SSE reconnects after brief disconnect
- [ ] No ServiceAlert shown (or briefly shown then disappears)

### 3. Service Stopped

**Steps**:
1. App running normally, service connected
2. Manually stop: `sc stop k2`
3. SSE stream disconnects → `service-state-changed { available: false }`
4. After 10s → `isServiceFailedLongTime` = true → ServiceAlert (red banner)
5. Click "Resolve" → UAC → `k2.exe service install` → service restarts

**Verify**:
- [ ] ServiceAlert appears within ~12s of stop
- [ ] "Resolve" button triggers UAC dialog
- [ ] After UAC approval, service restarts
- [ ] ServiceAlert disappears
- [ ] Dashboard becomes interactive again

### 4. Service Crash

**Steps**:
1. App running normally
2. Kill k2 daemon: `taskkill /F /IM k2.exe`
3. Same flow as scenario 3

**Verify**: Same as scenario 3.

### 5. VPN Connect + Split Routing

**Precondition**: Service running, tunnels loaded.

**Steps**:
1. Select a cloud tunnel from the list
2. Set rule to "Global" (proxy all)
3. Click connect
4. Wait for connected state
5. Open youtube.com → should load via proxy
6. Switch rule to "Smart" (chnroute)
7. Reconnect
8. Open baidu.com → should load direct (no proxy)
9. Open youtube.com → should still load via proxy

**Verify**:
- [ ] VPN state shows "connected"
- [ ] youtube.com loads in both modes
- [ ] baidu.com loads (fast, direct) in smart mode
- [ ] `curl.exe -X POST .../api/core -d '{"action":"status"}'` shows connected state with config

### 6. VPN Disconnect

**Steps**:
1. From connected state, click disconnect
2. Wait for disconnected state

**Verify**:
- [ ] State transitions: connected → disconnecting → disconnected
- [ ] No residual TUN adapter: `ipconfig /all` should not show Kaitu adapter
- [ ] Direct internet works: `curl.exe https://www.baidu.com`
- [ ] No error state after disconnect

## Common Issues

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| UAC dialog not appearing | PowerShell `-Verb RunAs` failing | Check k2.exe path in service.rs |
| Service starts but wrong version | Old binary not replaced | Check NSIS installer file replacement |
| SSE never connects | Daemon not listening on :1777 | Check `netstat -an \| findstr 1777` |
| ServiceAlert never disappears | `setServiceFailed(false)` not called | Check SSE event emission |
| VPN connects but no traffic | TUN adapter not created | Check wintun driver, admin rights |
| Split routing wrong | Rule config not applied | Check ClientConfig.rule.global |
