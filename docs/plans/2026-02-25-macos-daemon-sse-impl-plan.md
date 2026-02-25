# macOS Daemon Mode + SSE Event-Driven Status — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable macOS to build in daemon mode (same as Windows/Linux) via Cargo feature flag, and replace 2s polling with SSE event-driven status updates.

**Architecture:** Compile-time `ne-mode` Cargo feature gates all NE code; default macOS build uses daemon HTTP like Win/Linux. New `status_stream.rs` maintains SSE connection to daemon's `GET /api/events`, emitting Tauri events for service availability and VPN status. Bridge layer implements `onServiceStateChange` / `onStatusChange` on `IK2Vpn`. VPN store switches from polling to events when available, falls back to polling for standalone.

**Tech Stack:** Rust (reqwest async SSE), Tauri v2 events, TypeScript/React, Swift (PacketTunnelProvider EventBridge migration)

**Design doc:** `docs/plans/2026-02-25-macos-daemon-sse-design.md`

---

## Critical Discovery: VPN Store Has Its Own Polling

The `core/polling.ts` `useStatusPolling` hook is **not used anywhere** in the app. The real polling lives inside `webapp/src/stores/vpn.store.ts:initializeVPNStore()` (line 216-238). This is the code that must be adapted to use events.

---

## Task 1: Add `ne-mode` Cargo Feature Flag

**Files:**
- Modify: `desktop/src-tauri/Cargo.toml:27-28`

**Step 1: Add ne-mode feature**

In `desktop/src-tauri/Cargo.toml`, add `ne-mode` to the `[features]` section:

```toml
[features]
mcp-bridge = ["dep:tauri-plugin-mcp-bridge"]
ne-mode = []
```

**Step 2: Verify compilation**

Run: `cd desktop/src-tauri && cargo check`
Expected: PASS (feature added but not used yet)

**Step 3: Commit**

```bash
git add desktop/src-tauri/Cargo.toml
git commit -m "feat(desktop): add ne-mode Cargo feature flag"
```

---

## Task 2: Gate NE Code Behind `ne-mode` Feature

**Files:**
- Modify: `desktop/src-tauri/src/ne.rs:7-10`
- Modify: `desktop/src-tauri/src/service.rs:135,141,155,161,204,212,254,280,376,387,392`
- Modify: `desktop/src-tauri/src/main.rs:64-65`
- Modify: `desktop/src-tauri/build.rs:10`

### Step 1: Gate `ne.rs` module

In `desktop/src-tauri/src/ne.rs`, change both `#[cfg(target_os = "macos")]` to `#[cfg(all(target_os = "macos", feature = "ne-mode"))]`:

```rust
#[cfg(all(target_os = "macos", feature = "ne-mode"))]
pub use macos::*;

#[cfg(all(target_os = "macos", feature = "ne-mode"))]
mod macos {
```

### Step 2: Gate `service.rs` macOS branches

In `desktop/src-tauri/src/service.rs`, change every `#[cfg(target_os = "macos")]` to `#[cfg(all(target_os = "macos", feature = "ne-mode"))]`, and every `#[cfg(not(target_os = "macos"))]` to `#[cfg(not(all(target_os = "macos", feature = "ne-mode")))]`.

Affected blocks:

**`daemon_exec` (lines 135-146):**
```rust
    #[cfg(all(target_os = "macos", feature = "ne-mode"))]
    {
        tokio::task::spawn_blocking(move || crate::ne::ne_action(&action, params))
            .await
            .map_err(|e| format!("Task join error: {}", e))?
    }
    #[cfg(not(all(target_os = "macos", feature = "ne-mode")))]
    {
        tokio::task::spawn_blocking(move || core_action(&action, params))
            .await
            .map_err(|e| format!("Task join error: {}", e))?
    }
```

**`get_udid` (lines 155-183):**
```rust
    #[cfg(all(target_os = "macos", feature = "ne-mode"))]
    {
        tokio::task::spawn_blocking(|| crate::ne::get_udid_native())
            .await
            .map_err(|e| format!("Task join error: {}", e))?
    }
    #[cfg(not(all(target_os = "macos", feature = "ne-mode")))]
    {
        // ... existing daemon HTTP path ...
    }
```

**`admin_reinstall_service` (lines 204-221):**
```rust
    #[cfg(all(target_os = "macos", feature = "ne-mode"))]
    {
        tokio::task::spawn_blocking(|| crate::ne::admin_reinstall_ne())
            .await
            .map_err(|e| format!("Task join error: {}", e))?
    }

    #[cfg(target_os = "windows")]
    {
        admin_reinstall_service_windows().await
    }

    #[cfg(not(any(all(target_os = "macos", feature = "ne-mode"), target_os = "windows")))]
    {
        Err("Not supported on this platform".to_string())
    }
```

**`detect_old_kaitu_service` (lines 254-271):**
```rust
    #[cfg(all(target_os = "macos", feature = "ne-mode"))]
    {
        // NE mode: skip old service detection
        false
    }
    #[cfg(all(target_os = "macos", not(feature = "ne-mode")))]
    {
        std::path::Path::new("/Library/LaunchDaemons/io.kaitu.service.plist").exists()
            || std::path::Path::new("/Library/LaunchDaemons/com.kaitu.service.plist").exists()
    }
    #[cfg(target_os = "windows")]
    {
        // ... existing ...
    }
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        false
    }
```

**`cleanup_old_kaitu_service` (line 280):**
```rust
    #[cfg(all(target_os = "macos", not(feature = "ne-mode")))]
    {
        // ... existing launchd cleanup ...
    }
```

**`ensure_service_running` (lines 376-389):**
```rust
    #[cfg(all(target_os = "macos", feature = "ne-mode"))]
    {
        log::info!(
            "[service] macOS: ensuring NE installed (v{})",
            app_version
        );
        return tokio::task::spawn_blocking(|| crate::ne::ensure_ne_installed())
            .await
            .map_err(|e| format!("spawn_blocking failed: {}", e))?;
    }

    #[cfg(not(all(target_os = "macos", feature = "ne-mode")))]
    ensure_service_running_daemon(app_version).await
```

**`ensure_service_running_daemon` (line 392-393):**
```rust
#[cfg(not(all(target_os = "macos", feature = "ne-mode")))]
async fn ensure_service_running_daemon(app_version: String) -> Result<(), String> {
```

### Step 3: Gate `main.rs` NE callback

In `desktop/src-tauri/src/main.rs` line 64:
```rust
            #[cfg(all(target_os = "macos", feature = "ne-mode"))]
            ne::register_state_callback(app.handle().clone());
```

### Step 4: Gate `build.rs` NE linking

In `desktop/src-tauri/build.rs` line 10:
```rust
    #[cfg(all(target_os = "macos", feature = "ne-mode"))]
    {
        // ... existing NE helper linking logic (lines 12-58) ...
    }
```

Wait — actually `build.rs` runs at build time, not at compile time of the target. Cargo features ARE available in `build.rs` via `cfg!()` macro or `std::env::var("CARGO_FEATURE_NE_MODE")`. Change:

```rust
    // Only link NE helper in ne-mode
    let ne_mode = std::env::var("CARGO_FEATURE_NE_MODE").is_ok();

    if cfg!(target_os = "macos") && ne_mode {
        // ... existing NE helper linking logic ...
    }
```

### Step 5: Run tests without ne-mode

Run: `cd desktop/src-tauri && NE_HELPER_SKIP_LINK=1 cargo test`
Expected: All 14 tests pass. macOS daemon code paths compile instead of NE paths.

### Step 6: Run tests with ne-mode

Run: `cd desktop/src-tauri && NE_HELPER_SKIP_LINK=1 cargo test --features ne-mode`
Expected: All 14 tests pass. NE code paths compile.

### Step 7: Commit

```bash
git add desktop/src-tauri/src/ne.rs desktop/src-tauri/src/service.rs \
        desktop/src-tauri/src/main.rs desktop/src-tauri/build.rs
git commit -m "feat(desktop): gate NE code behind ne-mode feature flag

macOS default build now uses daemon HTTP (same as Win/Linux).
NE mode requires --features ne-mode."
```

---

## Task 3: Create `status_stream.rs` — Rust SSE Client

**Files:**
- Create: `desktop/src-tauri/src/status_stream.rs`
- Modify: `desktop/src-tauri/src/main.rs:3` (add `mod status_stream`)

### Step 1: Add tokio-stream dependency

In `desktop/src-tauri/Cargo.toml`, reqwest already exists with `features = ["blocking", "json"]`. We need the async reqwest for SSE streaming. No extra dependency needed — reqwest async is the default, `blocking` is an additional feature.

### Step 2: Create `status_stream.rs`

Create `desktop/src-tauri/src/status_stream.rs`:

```rust
//! SSE client for daemon's GET /api/events endpoint.
//!
//! Daemon mode only (not compiled in ne-mode).
//! Maintains a persistent SSE connection and emits Tauri events:
//! - `service-state-changed { available: bool }` — SSE connection state
//! - `vpn-status-changed { ...engine.Status }` — VPN status from SSE events

use tauri::{AppHandle, Emitter};

const SSE_URL: &str = "http://127.0.0.1:1777/api/events";
const RECONNECT_DELAY_SECS: u64 = 3;

/// Start the SSE status stream listener.
/// Runs indefinitely in a background tokio task.
/// Emits Tauri events for service availability and VPN status changes.
pub fn start(app_handle: AppHandle) {
    tauri::async_runtime::spawn(async move {
        loop {
            log::info!("[sse] Connecting to {}", SSE_URL);
            match connect_and_stream(&app_handle).await {
                Ok(()) => {
                    log::info!("[sse] Stream ended normally");
                }
                Err(e) => {
                    log::warn!("[sse] Stream error: {}", e);
                }
            }
            // SSE disconnected → service unavailable
            emit_service_state(&app_handle, false);
            log::info!("[sse] Reconnecting in {}s...", RECONNECT_DELAY_SECS);
            tokio::time::sleep(std::time::Duration::from_secs(RECONNECT_DELAY_SECS)).await;
        }
    });
}

fn emit_service_state(app_handle: &AppHandle, available: bool) {
    if let Err(e) = app_handle.emit("service-state-changed", serde_json::json!({ "available": available })) {
        log::error!("[sse] Failed to emit service-state-changed: {}", e);
    }
}

fn emit_vpn_status(app_handle: &AppHandle, status_json: &serde_json::Value) {
    if let Err(e) = app_handle.emit("vpn-status-changed", status_json) {
        log::error!("[sse] Failed to emit vpn-status-changed: {}", e);
    }
}

/// Connect to SSE endpoint, emit events, return when connection drops.
async fn connect_and_stream(app_handle: &AppHandle) -> Result<(), String> {
    let client = reqwest::Client::builder()
        .no_proxy()
        .timeout(std::time::Duration::from_secs(60))
        .build()
        .map_err(|e| format!("HTTP client error: {}", e))?;

    let response = client
        .get(SSE_URL)
        .header("Accept", "text/event-stream")
        .send()
        .await
        .map_err(|e| format!("SSE connect failed: {}", e))?;

    if !response.status().is_success() {
        return Err(format!("SSE HTTP {}", response.status()));
    }

    // Connection success → service available
    emit_service_state(app_handle, true);
    log::info!("[sse] Connected, streaming events");

    // Read the SSE stream line by line
    let mut event_type = String::new();
    let mut data_buf = String::new();

    // Use bytes_stream for streaming
    use futures_util::StreamExt;
    let mut stream = response.bytes_stream();
    let mut leftover = String::new();

    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|e| format!("SSE read error: {}", e))?;
        let text = format!("{}{}", leftover, String::from_utf8_lossy(&chunk));
        leftover.clear();

        for line in text.split('\n') {
            // Handle incomplete last line
            if !text.ends_with('\n') && std::ptr::eq(line, text.split('\n').last().unwrap()) {
                leftover = line.to_string();
                continue;
            }

            let line = line.trim_end_matches('\r');

            if line.is_empty() {
                // Empty line = end of event
                if !data_buf.is_empty() {
                    if event_type == "status" || event_type.is_empty() {
                        if let Ok(parsed) = serde_json::from_str::<serde_json::Value>(&data_buf) {
                            emit_vpn_status(app_handle, &parsed);
                        }
                    }
                    event_type.clear();
                    data_buf.clear();
                }
            } else if line.starts_with("event:") {
                event_type = line["event:".len()..].trim().to_string();
            } else if line.starts_with("data:") {
                if !data_buf.is_empty() {
                    data_buf.push('\n');
                }
                data_buf.push_str(line["data:".len()..].trim());
            } else if line.starts_with(':') {
                // Comment (heartbeat) — ignore
            }
        }
    }

    Ok(())
}
```

### Step 3: Add `futures-util` dependency

In `desktop/src-tauri/Cargo.toml`, add:
```toml
futures-util = "0.3"
```

### Step 4: Register module and start SSE in `main.rs`

In `desktop/src-tauri/src/main.rs`:

1. Add module declaration after existing mods (line 7):
```rust
mod status_stream;
```

2. In the setup closure, after the `ensure_service_running` spawn (after line 73), add SSE start for daemon mode:
```rust
            // Start SSE status stream (daemon mode only — not NE mode)
            #[cfg(not(all(target_os = "macos", feature = "ne-mode")))]
            {
                let sse_handle = app.handle().clone();
                status_stream::start(sse_handle);
            }
```

### Step 5: Verify compilation

Run: `cd desktop/src-tauri && cargo check`
Expected: PASS

### Step 6: Commit

```bash
git add desktop/src-tauri/src/status_stream.rs desktop/src-tauri/src/main.rs \
        desktop/src-tauri/Cargo.toml
git commit -m "feat(desktop): add SSE status stream client

Connects to daemon GET /api/events for real-time status.
Emits service-state-changed and vpn-status-changed Tauri events.
Auto-reconnects on disconnect with 3s delay.
Daemon mode only (not compiled in ne-mode)."
```

---

## Task 4: Extend `IK2Vpn` Interface

**Files:**
- Modify: `webapp/src/types/kaitu-core.ts:186-200`
- Modify: `webapp/src/services/vpn-types.ts` (add import if needed)

### Step 1: Add event methods to IK2Vpn

In `webapp/src/types/kaitu-core.ts`, extend the `IK2Vpn` interface (after `run` method, before closing `}`):

```typescript
export interface IK2Vpn {
  /**
   * Execute a VPN command
   */
  run<T = any>(action: string, params?: any): Promise<SResponse<T>>;

  /**
   * Service 可达性事件（可选）
   * daemon 模式: SSE 连接状态
   * NE 模式: NE 配置安装后恒 true
   * standalone: 不实现 → 退化为轮询
   */
  onServiceStateChange?(callback: (available: boolean) => void): () => void;

  /**
   * VPN 状态变更事件（可选）
   * daemon 模式: SSE status events
   * NE 模式: NE state callback → full status
   * standalone: 不实现 → 退化为轮询
   */
  onStatusChange?(callback: (status: StatusResponseData) => void): () => void;
}
```

Add import at top of file:
```typescript
import type { StatusResponseData } from '../services/vpn-types';
```

### Step 2: Verify TypeScript compiles

Run: `cd webapp && npx tsc --noEmit`
Expected: PASS

### Step 3: Commit

```bash
git add webapp/src/types/kaitu-core.ts
git commit -m "feat(webapp): add onServiceStateChange and onStatusChange to IK2Vpn

Optional event methods for event-driven status. Platforms that
don't implement them fall back to polling."
```

---

## Task 5: Implement Bridge Events in `tauri-k2.ts`

**Files:**
- Modify: `webapp/src/services/tauri-k2.ts:74-108`

### Step 1: Add event listeners to tauriK2

In `webapp/src/services/tauri-k2.ts`, extend the `tauriK2` object to include the two event methods. After the `run` method (line 107), add:

```typescript
  const tauriK2: IK2Vpn = {
    run: async <T = any>(action: string, params?: any): Promise<SResponse<T>> => {
      // ... existing run implementation ...
    },

    onServiceStateChange: (callback: (available: boolean) => void): (() => void) => {
      let unlisten: (() => void) | null = null;
      listen<{ available: boolean }>('service-state-changed', (event) => {
        callback(event.payload.available);
      }).then((fn) => {
        unlisten = fn;
      });
      return () => {
        unlisten?.();
      };
    },

    onStatusChange: (callback: (status: StatusResponseData) => void): (() => void) => {
      let unlisten: (() => void) | null = null;
      listen<any>('vpn-status-changed', (event) => {
        callback(transformStatus(event.payload));
      }).then((fn) => {
        unlisten = fn;
      });
      return () => {
        unlisten?.();
      };
    },
  };
```

### Step 2: Verify TypeScript compiles

Run: `cd webapp && npx tsc --noEmit`
Expected: PASS

### Step 3: Commit

```bash
git add webapp/src/services/tauri-k2.ts
git commit -m "feat(tauri-k2): implement onServiceStateChange and onStatusChange

Listen to Rust SSE-emitted Tauri events:
- service-state-changed → onServiceStateChange(available)
- vpn-status-changed → onStatusChange(transformStatus(raw))"
```

---

## Task 6: Event-Driven VPN Store

**Files:**
- Modify: `webapp/src/stores/vpn.store.ts:164-247`

### Step 1: Rewrite `initializeVPNStore` to use events when available

Replace the `initializeVPNStore` function (lines 164-247) in `webapp/src/stores/vpn.store.ts`:

```typescript
export function initializeVPNStore(): () => void {
  const handleStatusChange = (newStatus: StatusResponseData) => {
    const { localState, status: currentStatus } = useVPNStore.getState();
    const currentState = (currentStatus?.state as ServiceState) || 'disconnected';
    const backendState = newStatus.state as ServiceState;

    // 防抖逻辑
    if (shouldDebounce(currentState, backendState)) {
      if (debounceTimer) clearTimeout(debounceTimer);
      pendingState = backendState;

      debounceTimer = setTimeout(() => {
        if (pendingState) {
          useVPNStore.setState({ status: newStatus });
        }
        pendingState = null;
        debounceTimer = null;
      }, STATE_DEBOUNCE_MS);
      return;
    }

    // 取消防抖
    if (debounceTimer) {
      clearTimeout(debounceTimer);
      debounceTimer = null;
      pendingState = null;
    }

    // 更新状态
    useVPNStore.setState({ status: newStatus });

    // 清除合法的乐观状态
    if (localState && isValidTransition(localState, backendState)) {
      if (optimisticTimer) {
        clearTimeout(optimisticTimer);
        optimisticTimer = null;
      }
      useVPNStore.setState({ localState: null });
    }
  };

  // Event-driven mode: use _k2 events when available (desktop/mobile)
  if (window._k2?.onServiceStateChange && window._k2?.onStatusChange) {
    console.info('[VPNStore] Event-driven mode (SSE)');

    const unsubService = window._k2.onServiceStateChange((available) => {
      console.debug('[VPNStore] service-state-changed:', available);
      useVPNStore.getState().setServiceFailed(!available);
    });

    const unsubStatus = window._k2.onStatusChange((status) => {
      console.debug('[VPNStore] vpn-status-changed:', status.state);
      handleStatusChange(status);
      useVPNStore.getState().setServiceFailed(false);
    });

    // Bridge initial gap: one-time status query before first SSE event arrives
    window._k2.run('status').then((resp: any) => {
      if (resp.code === 0 && resp.data) {
        handleStatusChange(resp.data);
        useVPNStore.getState().setServiceFailed(false);
      }
    }).catch(() => {
      // Service may not be running yet — SSE will handle it
    });

    return () => {
      unsubService();
      unsubStatus();
      [optimisticTimer, debounceTimer].forEach(t => t && clearTimeout(t));
      optimisticTimer = debounceTimer = null;
      pendingState = null;
      useVPNStore.setState({ serviceConnected: true, serviceFailedSince: null });
    };
  }

  // Polling fallback: standalone/web mode
  console.info('[VPNStore] Polling mode (2s)');

  const pollStatus = async () => {
    try {
      const response = await window._k2.run('status') as {
        code: number;
        data?: StatusResponseData;
        message?: string;
      };

      if (response.code === 0 && response.data) {
        handleStatusChange(response.data);
        useVPNStore.getState().setServiceFailed(false);
      } else {
        console.warn('[VPNStore] 服务返回错误:', response.code, response.message);
        useVPNStore.getState().setServiceFailed(true);
      }
    } catch (error) {
      console.error('[VPNStore] 轮询异常:', error);
      useVPNStore.getState().setServiceFailed(true);
    }
  };

  pollStatus();
  const interval = setInterval(pollStatus, POLL_INTERVAL_MS);

  return () => {
    clearInterval(interval);
    [optimisticTimer, debounceTimer].forEach(t => t && clearTimeout(t));
    optimisticTimer = debounceTimer = null;
    pendingState = null;
    useVPNStore.setState({ serviceConnected: true, serviceFailedSince: null });
  };
}
```

### Step 2: Verify TypeScript compiles

Run: `cd webapp && npx tsc --noEmit`
Expected: PASS

### Step 3: Run existing tests

Run: `cd webapp && npx vitest run`
Expected: All tests pass

### Step 4: Commit

```bash
git add webapp/src/stores/vpn.store.ts
git commit -m "feat(vpn-store): event-driven status with polling fallback

Desktop: uses onServiceStateChange + onStatusChange from SSE.
Standalone/web: falls back to 2s polling.
One-time status query bridges gap before first SSE event."
```

---

## Task 7: Deprecate `useStatusPolling`, Keep `pollStatusOnce`

**Files:**
- Modify: `webapp/src/core/polling.ts:38`

### Step 1: Add deprecation JSDoc

In `webapp/src/core/polling.ts`, update the `useStatusPolling` JSDoc (line 24-37):

```typescript
/**
 * @deprecated Use event-driven status in vpn.store.ts instead.
 * This hook is only used for standalone/web fallback.
 * The actual polling logic lives in initializeVPNStore().
 */
export function useStatusPolling(options: PollingOptions = {}) {
```

### Step 2: Commit

```bash
git add webapp/src/core/polling.ts
git commit -m "docs(polling): deprecate useStatusPolling in favor of event-driven store"
```

---

## Task 8: Migrate PacketTunnelProvider EventBridge

**Files:**
- Modify: `desktop/src-tauri/KaituTunnel/PacketTunnelProvider.swift:281-321`

### Step 1: Migrate EventBridge from onStateChange+onError to onStatus

Replace the `EventBridge` class (lines 281-321):

```swift
// MARK: - EventBridge

class EventBridge: NSObject, AppextEventHandlerProtocol {
    weak var provider: PacketTunnelProvider?

    init(provider: PacketTunnelProvider) {
        self.provider = provider
    }

    /// Unified status callback — replaces old onStateChange + onError.
    /// statusJSON contains { state, error?: { code, message }, connected_at?, uptime_seconds? }
    func onStatus(_ statusJSON: String?) {
        guard let json = statusJSON,
              let data = json.data(using: .utf8),
              let parsed = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let state = parsed["state"] as? String else {
            NSLog("[KaituTunnel:NE] onStatus: invalid or nil JSON")
            return
        }

        NSLog("[KaituTunnel:NE] onStatus: state=%@", state)

        if state == "disconnected" {
            // Check for error
            if let errorObj = parsed["error"] as? [String: Any] {
                let code = errorObj["code"] as? Int ?? 0
                let message = errorObj["message"] as? String ?? "unknown error"
                NSLog("[KaituTunnel:NE] Disconnected with error: code=%d message=%@", code, message)

                // Write error to App Group so main app can read it
                UserDefaults(suiteName: kAppGroup)?.set(message, forKey: "vpnError")

                // Notify system that tunnel has failed
                let nsError = NSError(domain: "io.kaitu.desktop", code: code,
                                      userInfo: [NSLocalizedDescriptionKey: message])
                provider?.cancelTunnelWithError(nsError)
            } else {
                // Normal disconnect — no error
                NSLog("[KaituTunnel:NE] Normal disconnect")
                provider?.cancelTunnelWithError(nil)
            }
        }
        // Other states (connecting, connected, reconnecting, paused) are transient — log only
    }

    func onStats(_ txBytes: Int64, rxBytes: Int64) {
        // Stats tracking if needed
    }
}
```

Key improvements over old EventBridge:
- **No race condition**: Old bridge had `onError` + `onStateChange("disconnected")` calling `cancelTunnelWithError` twice. `onStatus` delivers state+error atomically.
- **No `hasReportedError` flag**: Single callback eliminates the need for the anti-double-fire guard.
- **Structured error**: Extracts `{code, message}` from status JSON instead of raw string.

### Step 2: Fix handleAppMessage fallback

In `PacketTunnelProvider.swift` line 266, change `"stopped"` to `"disconnected"`:

```swift
        case "status":
            let json = engine?.statusJSON() ?? "{\"state\":\"disconnected\"}"
            completionHandler?(json.data(using: .utf8))
```

### Step 3: Commit

```bash
git add desktop/src-tauri/KaituTunnel/PacketTunnelProvider.swift
git commit -m "feat(sysext): migrate EventBridge to onStatus(statusJSON)

Replaces onStateChange+onError with unified onStatus callback.
Eliminates dual-callback race condition.
handleAppMessage fallback: stopped → disconnected."
```

---

## Task 9: NE Mode — Emit Unified Tauri Events

**Files:**
- Modify: `desktop/src-tauri/src/ne.rs:176-195` (the `ne_state_callback`)

### Step 1: Update NE state callback to emit unified events

The NE state callback currently emits `ne-state-changed` with just `{ state }`. In the design, NE mode should emit the same `vpn-status-changed` event as daemon mode for webapp compatibility.

However, the NE callback only receives a raw state string from `NEVPNStatusDidChange`, not the full status. The full status requires calling `k2ne_status()`.

Update `ne_state_callback` in `ne.rs` (lines 176-195):

```rust
    unsafe extern "C" fn ne_state_callback(state_ptr: *const c_char) {
        if state_ptr.is_null() {
            return;
        }
        let state = match CStr::from_ptr(state_ptr).to_str() {
            Ok(s) => s.to_owned(),
            Err(_) => return,
        };

        let guard = STATE_HANDLE.lock();
        if let Ok(maybe_handle) = guard {
            if let Some(handle) = maybe_handle.as_ref() {
                // Emit service-state-changed (NE installed = always available)
                let _ = handle.emit(
                    "service-state-changed",
                    serde_json::json!({ "available": true }),
                );

                // Get full status from NE helper for vpn-status-changed
                let full_status = call_ne_fn(k2ne_status());
                let status_payload = match full_status {
                    Ok(resp) if resp.code == 0 => resp.data,
                    _ => serde_json::json!({ "state": state }),
                };
                if let Err(e) = handle.emit("vpn-status-changed", status_payload) {
                    log::error!("[ne] Failed to emit vpn-status-changed: {}", e);
                }
            }
        }
    }
```

### Step 2: Emit initial service-state-changed after NE install

In `main.rs`, after the `ensure_service_running` spawn succeeds in NE mode, the `register_state_callback` already runs before it. The initial `service-state-changed(true)` is emitted by the callback itself. This is sufficient.

### Step 3: Verify compilation with ne-mode

Run: `cd desktop/src-tauri && NE_HELPER_SKIP_LINK=1 cargo check --features ne-mode`
Expected: PASS

### Step 4: Commit

```bash
git add desktop/src-tauri/src/ne.rs
git commit -m "feat(ne): emit unified vpn-status-changed and service-state-changed events

NE state callback now emits the same Tauri events as daemon SSE mode.
Calls k2ne_status() for full status payload.
service-state-changed is always true in NE mode (NE installed = available)."
```

---

## Task 10: Update Makefile and Build Script

**Files:**
- Modify: `Makefile:17-24`
- Modify: `scripts/build-macos.sh`

### Step 1: Add `build-macos-sysext` target to Makefile

In `Makefile`, after the `build-macos-test` line (line 24):

```makefile
build-macos:
	bash scripts/build-macos.sh

build-macos-fast:
	bash scripts/build-macos.sh --skip-notarization

build-macos-test:
	bash scripts/build-macos.sh --single-arch --skip-notarization --features=mcp-bridge

build-macos-sysext:
	bash scripts/build-macos.sh --ne-mode

build-macos-sysext-fast:
	bash scripts/build-macos.sh --ne-mode --skip-notarization

build-macos-sysext-test:
	bash scripts/build-macos.sh --ne-mode --single-arch --skip-notarization
```

### Step 2: Update build-macos.sh for `--ne-mode` flag

Add `--ne-mode` argument parsing and conditional logic in `scripts/build-macos.sh`.

**Argument parsing** (after line 16):
```bash
NE_MODE=false
for arg in "$@"; do
  case "$arg" in
    --skip-notarization) SKIP_NOTARIZATION=true ;;
    --single-arch) SINGLE_ARCH=true ;;
    --ne-mode) NE_MODE=true ;;
    --features=*) EXTRA_FEATURES="${arg#--features=}" ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done
```

**Add ne-mode to EXTRA_FEATURES** (after argument parsing):
```bash
# Append ne-mode feature when building sysext variant
if [ "$NE_MODE" = true ]; then
  if [ -n "$EXTRA_FEATURES" ]; then
    EXTRA_FEATURES="$EXTRA_FEATURES,ne-mode"
  else
    EXTRA_FEATURES="ne-mode"
  fi
fi
```

**Conditional gomobile + NE helper** (lines 84-101): Wrap the gomobile and NE helper sections:
```bash
if [ "$NE_MODE" = true ]; then
  # --- Build gomobile macOS xcframework ---
  echo ""
  echo "--- Building gomobile macOS xcframework ---"
  make mobile-macos

  # --- Build libk2_ne_helper.a ---
  echo ""
  echo "--- Building NE helper static library ---"
  cd "$ROOT_DIR/desktop/src-tauri/ne_helper"
  if [ "$SINGLE_ARCH" = true ]; then
    bash build.sh --arch "$NE_ARCH"
  else
    bash build.sh --arch universal
  fi
  cd "$ROOT_DIR"

  # Set env var for Rust build.rs to find the library
  export NE_HELPER_LIB_DIR="$ROOT_DIR/desktop/src-tauri/ne_helper"
fi
```

**Conditional sysext injection** (lines 155-261): Wrap the entire "Build and inject System Extension" section:
```bash
if [ "$NE_MODE" = true ]; then
  # --- Build and inject System Extension ---
  # ... existing sysext build + sign code ...
else
  # Daemon mode: just sign the app without sysext
  echo ""
  echo "--- Codesigning app bundle (daemon mode, no sysext) ---"
  SIGN_IDENTITY="${APPLE_SIGNING_IDENTITY:-Developer ID Application: ALL NATION CONNECT TECHNOLOGY PTE. LTD. (NJT954Q3RH)}"

  codesign --force --sign "$SIGN_IDENTITY" \
    --options runtime \
    "$APP_PATH"
fi
```

### Step 3: Commit

```bash
git add Makefile scripts/build-macos.sh
git commit -m "feat(build): add macOS sysext build targets

build-macos: daemon mode (default, same as Win/Linux)
build-macos-sysext: NE mode (--features ne-mode + gomobile + sysext)
build-macos.sh: --ne-mode flag controls gomobile/NE/sysext steps."
```

---

## Task 11: Add Tauri Event Permissions

**Files:**
- Modify: `desktop/src-tauri/capabilities/default.json` (or equivalent capability file)

### Step 1: Check existing capabilities

Check if `service-state-changed` and `vpn-status-changed` events need to be declared in Tauri capabilities. Tauri v2 requires `core:event:default` permission for emit/listen. Verify the existing capability file includes this.

If missing, add to the capabilities:
```json
{
  "permissions": [
    "core:event:default"
  ]
}
```

### Step 2: Commit if changes needed

```bash
git add desktop/src-tauri/capabilities/
git commit -m "fix(desktop): ensure event permissions for SSE status events"
```

---

## Task 12: Run Full Test Suite

### Step 1: Rust tests (daemon mode)

Run: `cd desktop/src-tauri && NE_HELPER_SKIP_LINK=1 cargo test`
Expected: All tests pass

### Step 2: Rust tests (NE mode)

Run: `cd desktop/src-tauri && NE_HELPER_SKIP_LINK=1 cargo test --features ne-mode`
Expected: All tests pass

### Step 3: TypeScript type check

Run: `cd webapp && npx tsc --noEmit`
Expected: PASS

### Step 4: Webapp tests

Run: `cd webapp && npx vitest run`
Expected: All tests pass

### Step 5: Commit any test fixes if needed

---

## Task 13: Update CLAUDE.md and Memory

**Files:**
- Modify: `desktop/CLAUDE.md` — Update module descriptions for ne-mode feature flag
- Modify: `CLAUDE.md` — Update "macOS NE mode" convention to mention dual build

### Step 1: Update desktop/CLAUDE.md

Update the description of modules and IPC commands to reflect dual build mode.

### Step 2: Update root CLAUDE.md

Change the "macOS NE mode" convention to:
```
- **macOS dual build**: Default macOS build uses daemon HTTP (same as Win/Linux). NE mode requires `--features ne-mode` Cargo flag (build-macos-sysext target). `#[cfg(all(target_os = "macos", feature = "ne-mode"))]` gates all NE code.
- **Event-driven status**: Desktop uses SSE (daemon mode) or NE callbacks to push `service-state-changed` and `vpn-status-changed` Tauri events. Webapp VPN store subscribes via `_k2.onServiceStateChange` / `_k2.onStatusChange`. Standalone/web falls back to 2s polling.
```

### Step 3: Commit

```bash
git add desktop/CLAUDE.md CLAUDE.md
git commit -m "docs: update CLAUDE.md for macOS dual build + SSE events"
```

---

## Summary of Changes by File

| # | File | Change |
|---|------|--------|
| 1 | `Cargo.toml` | Add `ne-mode` feature |
| 2 | `ne.rs` | Gate behind `ne-mode` feature; emit unified Tauri events |
| 3 | `service.rs` | All `cfg(target_os = "macos")` → `cfg(all(target_os = "macos", feature = "ne-mode"))` |
| 4 | `main.rs` | Gate NE callback; add `mod status_stream`; start SSE in daemon mode |
| 5 | `build.rs` | Gate NE linking behind `ne-mode` feature |
| 6 | `status_stream.rs` (new) | Rust SSE client → Tauri events |
| 7 | `kaitu-core.ts` | Add `onServiceStateChange?` + `onStatusChange?` to IK2Vpn |
| 8 | `tauri-k2.ts` | Implement event methods (listen Tauri events) |
| 9 | `vpn.store.ts` | Event-driven init with polling fallback |
| 10 | `polling.ts` | Deprecate `useStatusPolling` |
| 11 | `PacketTunnelProvider.swift` | EventBridge: `onStateChange`+`onError` → `onStatus(statusJSON)` |
| 12 | `Makefile` | `build-macos-sysext` + `build-macos-sysext-fast` targets |
| 13 | `build-macos.sh` | `--ne-mode` flag: conditional gomobile/NE/sysext steps |
| 14 | `CLAUDE.md` + `desktop/CLAUDE.md` | Documentation updates |

## Dependencies Between Tasks

```
Task 1 (Cargo feature) → Task 2 (gate NE code) → Task 3 (status_stream.rs)
                                                         ↓
Task 4 (IK2Vpn interface) → Task 5 (tauri-k2 bridge) → Task 6 (vpn store)
                                                         ↓
Task 8 (EventBridge migration)                    Task 7 (deprecate polling)
Task 9 (NE unified events, depends on Task 2)
Task 10 (Makefile, depends on Task 2)
Task 11 (permissions)
Task 12 (full test, depends on all above)
Task 13 (docs, depends on all above)
```

Parallelizable: Tasks 1-3 (Rust side) and Tasks 4-7 (webapp side) can run in parallel once Task 1 (Cargo feature) is done.
