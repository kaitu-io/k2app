# Linux tar.gz Distribution + Custom Updater — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace 85MB Linux AppImage with ~6-8MB tar.gz bundle and custom Rust updater.

**Architecture:** Linux build compiles Tauri binary without bundling (`--no-bundle`), packages raw binary + k2 + icon into tar.gz. Custom updater in `updater.rs` fetches latest.json, downloads tar.gz, verifies minisign signature, extracts via pkexec. Install script auto-installs webkit2gtk from system package manager.

**Tech Stack:** Rust (updater), minisign-verify 0.2 (signature), semver 1 (version comparison), reqwest (HTTP), shell (install script)

**Spec:** `docs/plans/2026-03-18-linux-tgz-updater.md`

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `desktop/src-tauri/Cargo.toml` | Modify | Add `minisign-verify`, `semver` as Linux-only deps |
| `desktop/src-tauri/src/updater.rs` | Modify | Add `#[cfg(target_os = "linux")]` branches in 4 functions, add Linux updater module |
| `desktop/src-tauri/src/linux_updater.rs` | Create | Linux-specific update logic: fetch, download, verify, apply |
| `web/public/i/k2` | Modify | Install script: tar.gz flow, auto webkit2gtk, remove libfuse2 |
| `Makefile` | Modify | `build-linux` target: `--no-bundle` + tar.gz packaging |
| `scripts/publish-desktop.sh` | Modify | Linux artifact: `.tar.gz` instead of `.AppImage` |
| `.github/workflows/release-desktop.yml` | Modify | Linux CI: sign tar.gz, upload tar.gz |

---

### Task 1: Add Linux-only Dependencies to Cargo.toml

**Files:**
- Modify: `desktop/src-tauri/Cargo.toml`

- [ ] **Step 1: Add Linux-only dependencies**

In `desktop/src-tauri/Cargo.toml`, add after the `[target.'cfg(target_os = "windows")'.dependencies]` section:

```toml
[target.'cfg(target_os = "linux")'.dependencies]
minisign-verify = "0.2"
semver = "1"
base64 = "0.22"
```

`minisign-verify` and `semver` are already in `Cargo.lock` (transitive deps). `base64` is used to decode signatures from latest.json (Tauri stores them as base64-encoded minisign signature content).

- [ ] **Step 2: Verify compilation**

Run: `cd desktop/src-tauri && cargo check`
Expected: Compiles without errors. No code changes yet, just dep declaration.

- [ ] **Step 3: Commit**

```bash
git add desktop/src-tauri/Cargo.toml desktop/src-tauri/Cargo.lock
git commit -m "build(linux): add minisign-verify and semver as Linux-only deps"
```

---

### Task 2: Create Linux Updater Module

**Files:**
- Create: `desktop/src-tauri/src/linux_updater.rs`

This is the core module. It handles: fetch latest.json → compare version → download tar.gz → verify signature → extract via pkexec → relaunch.

- [ ] **Step 1: Write the linux_updater.rs module**

Create `desktop/src-tauri/src/linux_updater.rs`:

```rust
//! Linux-specific updater module
//!
//! Replaces tauri-plugin-updater on Linux. Downloads tar.gz bundles
//! (~6-8MB vs 85MB AppImage), verifies minisign signatures, and
//! extracts to /opt/kaitu/ via pkexec.
//!
//! Flow:
//!   1. Fetch latest.json from CDN (same endpoints as channel.rs)
//!   2. Compare semver (supports beta→stable upgrade, force downgrade)
//!   3. Download tar.gz to /tmp with atomic rename
//!   4. Download .sig and verify minisign signature
//!   5. On user action: pkexec extract to /opt/kaitu/ + systemctl restart k2
//!   6. Spawn relaunch helper + exit

use futures_util::StreamExt;
use serde::Deserialize;
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use tauri::{AppHandle, Emitter};
use tokio::io::AsyncWriteExt;

use crate::channel;
use crate::updater::{UpdateInfo, CHANNEL_SWITCH_PENDING, INSTALL_FAILED, UPDATE_INFO, UPDATE_READY};
use std::sync::atomic::Ordering;

/// Minisign public key — extracted from tauri.conf.json plugins.updater.pubkey.
/// The tauri.conf.json value is base64(full minisign pubkey file including comment line).
/// This constant is just the key line (what PublicKey::from_base64 expects).
const MINISIGN_PUBKEY: &str = "RWSD3s7XX1TXQLaSafFQyIycEGH5v0d7EOsPUmQGJMRjnCuqq3eAVKEE";

/// Path to downloaded tar.gz (set after successful download + verify)
static LINUX_UPDATE_PATH: Mutex<Option<PathBuf>> = Mutex::new(None);

/// latest.json schema
#[derive(Deserialize)]
struct LatestJson {
    version: String,
    notes: Option<String>,
    #[allow(dead_code)]
    pub_date: Option<String>,
    platforms: HashMap<String, PlatformEntry>,
}

#[derive(Deserialize)]
struct PlatformEntry {
    url: String,
    signature: String,
}

/// Main entry point for Linux auto-update check.
/// Called from updater.rs check_download_and_install() on Linux.
pub async fn check_and_download(app: &AppHandle, force_downgrade: bool) {
    let ch = channel::get_channel(app);
    let endpoints = match channel::endpoints_for_channel(&ch) {
        Ok(eps) => eps,
        Err(e) => {
            log::error!("[updater:linux] Failed to build endpoints: {}", e);
            return;
        }
    };

    let current_version = app.package_info().version.to_string();

    // Only allow downgrade when explicitly requested AND switching beta→stable
    // (mirrors updater.rs line 110 guard)
    let force_different = force_downgrade && ch == "stable" && current_version.contains("-beta");

    log::info!(
        "[updater:linux] Checking (channel={}, current={}, force={})",
        ch, current_version, force_different
    );

    // Try each endpoint until one succeeds
    let mut latest: Option<LatestJson> = None;
    for endpoint in &endpoints {
        match fetch_latest_json(endpoint.as_str()).await {
            Ok(l) => {
                latest = Some(l);
                break;
            }
            Err(e) => {
                log::warn!("[updater:linux] Endpoint {} failed: {}", endpoint, e);
            }
        }
    }

    let latest = match latest {
        Some(l) => l,
        None => {
            log::error!("[updater:linux] All endpoints failed");
            return;
        }
    };

    // Check if update is available
    let update_available = match compare_versions(&current_version, &latest.version, force_different) {
        Ok(available) => available,
        Err(e) => {
            log::error!("[updater:linux] Version comparison failed: {}", e);
            return;
        }
    };

    if !update_available {
        log::info!("[updater:linux] No update available (remote={})", latest.version);
        return;
    }

    // Get Linux platform entry
    let platform = match latest.platforms.get("linux-x86_64") {
        Some(p) => p,
        None => {
            log::error!("[updater:linux] No linux-x86_64 platform in latest.json");
            return;
        }
    };

    log::info!(
        "[updater:linux] Update available: {} -> {}",
        current_version, latest.version
    );

    // Download tar.gz
    let tmp_dir = std::env::temp_dir().join("kaitu-update");
    let _ = std::fs::create_dir_all(&tmp_dir);
    let tar_gz_path = tmp_dir.join(format!("Kaitu_{}_amd64.tar.gz", latest.version));

    if let Err(e) = download_file(&platform.url, &tar_gz_path).await {
        log::error!("[updater:linux] Download failed: {}", e);
        return;
    }

    // Download signature file (.sig is at same URL + ".sig" suffix)
    // The `signature` field in latest.json contains base64-encoded minisign signature
    // Decode it to standard minisign format for verification
    let sig_content = match base64_decode_signature(&platform.signature) {
        Ok(s) => s,
        Err(e) => {
            log::error!("[updater:linux] Failed to decode signature: {}", e);
            let _ = std::fs::remove_file(&tar_gz_path);
            return;
        }
    };

    // Verify signature
    if let Err(e) = verify_signature(&tar_gz_path, &sig_content) {
        log::error!("[updater:linux] {}", e);
        let _ = std::fs::remove_file(&tar_gz_path);
        return;
    }
    log::info!("[updater:linux] Signature verified");

    // Guard: discard if channel changed during download
    let current_ch = channel::get_channel(app);
    if current_ch != ch {
        log::info!(
            "[updater:linux] Channel changed during download ({} -> {}), discarding",
            ch, current_ch
        );
        let _ = std::fs::remove_file(&tar_gz_path);
        return;
    }

    // Store path for apply_update_now
    *LINUX_UPDATE_PATH.lock().unwrap() = Some(tar_gz_path.clone());

    // Channel switch auto-restart: if set_update_channel set this flag,
    // apply immediately instead of showing the "update ready" banner.
    if CHANNEL_SWITCH_PENDING
        .compare_exchange(true, false, Ordering::SeqCst, Ordering::SeqCst)
        .is_ok()
    {
        log::info!(
            "[updater:linux] Channel switch complete ({}), auto-applying...",
            latest.version
        );
        match extract_and_restart(&tar_gz_path) {
            Ok(()) => {
                let _ = std::fs::remove_file(&tar_gz_path);
                relaunch(app);
            }
            Err(e) => {
                log::error!("[updater:linux] Channel switch apply failed: {}", e);
                INSTALL_FAILED.store(true, Ordering::SeqCst);
            }
        }
        return;
    }

    // Normal path: notify frontend, user clicks "Update Now"
    let info = UpdateInfo {
        current_version: current_version.clone(),
        new_version: latest.version.clone(),
        release_notes: latest.notes.clone(),
    };
    *UPDATE_INFO.lock().unwrap() = Some(info.clone());
    UPDATE_READY.store(true, Ordering::SeqCst);

    let _ = app.emit("update-ready", info);
    log::info!("[updater:linux] Frontend notified via update-ready event");
}

/// Manual check triggered by IPC — same as auto but returns status string.
pub async fn check_now(app: &AppHandle) -> Result<String, String> {
    if crate::updater::is_update_ready() {
        if let Some(info) = UPDATE_INFO.lock().unwrap().as_ref() {
            return Ok(format!("Update {} already ready", info.new_version));
        }
    }

    // Run full check (non-force)
    check_and_download(app, false).await;

    if crate::updater::is_update_ready() {
        if let Some(info) = UPDATE_INFO.lock().unwrap().as_ref() {
            return Ok(format!("Update {} ready", info.new_version));
        }
    }

    Ok("Already on latest version".to_string())
}

/// Apply the downloaded update: pkexec extract + systemctl restart + relaunch app.
pub fn apply_update(app: &AppHandle) {
    let tar_gz_path = LINUX_UPDATE_PATH.lock().unwrap().take();
    let tar_gz_path = match tar_gz_path {
        Some(p) => p,
        None => {
            log::error!("[updater:linux] No update path stored");
            return;
        }
    };

    log::info!("[updater:linux] Applying update from {:?}", tar_gz_path);

    match extract_and_restart(&tar_gz_path) {
        Ok(()) => {
            log::info!("[updater:linux] Update applied, relaunching...");
            let _ = std::fs::remove_file(&tar_gz_path);
            relaunch(app);
        }
        Err(e) => {
            log::error!("[updater:linux] Apply failed: {}", e);
            let _ = std::fs::remove_file(&tar_gz_path);
            INSTALL_FAILED.store(true, Ordering::SeqCst);
            UPDATE_READY.store(false, Ordering::SeqCst);
        }
    }
}

// ============================================================================
// Internal helpers
// ============================================================================

async fn fetch_latest_json(url: &str) -> Result<LatestJson, String> {
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(30))
        .build()
        .map_err(|e| e.to_string())?;

    let resp = client
        .get(url)
        .send()
        .await
        .map_err(|e| format!("HTTP request failed: {}", e))?;

    if !resp.status().is_success() {
        return Err(format!("HTTP {}", resp.status()));
    }

    resp.json::<LatestJson>()
        .await
        .map_err(|e| format!("JSON parse failed: {}", e))
}

fn compare_versions(current: &str, remote: &str, force_different: bool) -> Result<bool, String> {
    let current = semver::Version::parse(current).map_err(|e| format!("Bad current version: {}", e))?;
    let remote = semver::Version::parse(remote).map_err(|e| format!("Bad remote version: {}", e))?;

    if force_different {
        Ok(remote != current)
    } else {
        Ok(remote > current)
    }
}

/// Download file with atomic rename (write to .tmp, rename on completion).
async fn download_file(url: &str, dest: &Path) -> Result<(), String> {
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(300))
        .build()
        .map_err(|e| e.to_string())?;

    let resp = client
        .get(url)
        .send()
        .await
        .map_err(|e| format!("Download request failed: {}", e))?;

    if !resp.status().is_success() {
        return Err(format!("Download HTTP {}", resp.status()));
    }

    let total = resp.content_length();
    // Append .tmp suffix (not with_extension which replaces .gz only)
    let tmp_dest = dest.with_file_name(format!(
        "{}.tmp",
        dest.file_name().unwrap().to_string_lossy()
    ));
    let mut file = tokio::fs::File::create(&tmp_dest)
        .await
        .map_err(|e| format!("Create tmp file: {}", e))?;

    let mut stream = resp.bytes_stream();
    let mut downloaded = 0u64;
    let mut last_log_percent = 0u32;

    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|e| {
            let _ = std::fs::remove_file(&tmp_dest);
            format!("Download stream error: {}", e)
        })?;
        file.write_all(&chunk).await.map_err(|e| {
            let _ = std::fs::remove_file(&tmp_dest);
            format!("Write error: {}", e)
        })?;
        downloaded += chunk.len() as u64;

        if let Some(total) = total {
            let percent = (downloaded as f64 / total as f64 * 100.0) as u32;
            if percent >= last_log_percent + 10 {
                last_log_percent = percent;
                log::info!("[updater:linux] Download progress: {}%", percent);
            }
        }
    }

    drop(file);
    log::info!("[updater:linux] Download complete ({} bytes)", downloaded);

    // Atomic rename
    tokio::fs::rename(&tmp_dest, dest)
        .await
        .map_err(|e| format!("Rename failed: {}", e))?;

    Ok(())
}

/// Decode the base64-encoded signature from latest.json to standard minisign format.
/// Tauri stores signatures as base64(minisign_signature_file_content).
fn base64_decode_signature(encoded: &str) -> Result<String, String> {
    use base64::Engine;
    let decoded = base64::engine::general_purpose::STANDARD
        .decode(encoded.trim())
        .map_err(|e| format!("Bad base64 in signature: {}", e))?;
    String::from_utf8(decoded).map_err(|e| format!("Signature is not UTF-8: {}", e))
}

/// Verify minisign signature against downloaded tar.gz.
fn verify_signature(tar_gz_path: &Path, sig_content: &str) -> Result<(), String> {
    let pk = minisign_verify::PublicKey::from_base64(MINISIGN_PUBKEY)
        .map_err(|e| format!("Bad public key: {:?}", e))?;

    let sig = minisign_verify::Signature::decode(sig_content)
        .map_err(|e| format!("Bad signature format: {:?}", e))?;

    let data = std::fs::read(tar_gz_path)
        .map_err(|e| format!("Failed to read tar.gz for verification: {}", e))?;

    pk.verify(&data, &sig, false)
        .map_err(|e| format!("Signature verification failed: {:?}", e))
}

/// Extract tar.gz to /opt/kaitu/ via pkexec (two-phase: temp dir then cp).
fn extract_and_restart(tar_gz_path: &Path) -> Result<(), String> {
    // Check pkexec availability
    let pkexec_available = std::process::Command::new("which")
        .arg("pkexec")
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false);

    if !pkexec_available {
        return Err("pkexec_unavailable: run update manually".to_string());
    }

    let script = format!(
        "STAGE=$(mktemp -d /tmp/kaitu-update-XXXXXX) && \
         tar xzf '{}' -C \"$STAGE\" && \
         chmod +x \"$STAGE/k2app\" \"$STAGE/k2\" && \
         mkdir -p /opt/kaitu && \
         cp -f \"$STAGE/k2app\" /opt/kaitu/k2app && \
         cp -f \"$STAGE/k2\" /opt/kaitu/k2 && \
         ([ -f \"$STAGE/kaitu.png\" ] && cp -f \"$STAGE/kaitu.png\" /opt/kaitu/kaitu.png || true) && \
         rm -rf \"$STAGE\" && \
         systemctl restart k2 2>/dev/null || true",
        tar_gz_path.display()
    );

    let output = std::process::Command::new("pkexec")
        .args(["bash", "-c", &script])
        .output()
        .map_err(|e| format!("pkexec exec failed: {}", e))?;

    if output.status.success() {
        Ok(())
    } else if output.status.code() == Some(126) {
        Err("User cancelled".to_string())
    } else {
        Err(format!(
            "pkexec failed ({}): {}",
            output.status,
            String::from_utf8_lossy(&output.stderr).trim()
        ))
    }
}

/// Spawn a helper process that relaunches k2app after the current process exits.
fn relaunch(app: &AppHandle) {
    let new_binary = "/opt/kaitu/k2app";
    let pid = std::process::id();

    let script = format!(
        "i=0; while [ $i -lt 30 ]; do \
           if ! kill -0 {} 2>/dev/null; then \
             sleep 1; \
             nohup '{}' >/dev/null 2>&1 & \
             exit 0; \
           fi; \
           sleep 1; \
           i=$((i + 1)); \
         done",
        pid, new_binary
    );

    log::info!(
        "[updater:linux] Spawning relaunch helper (pid={}, binary={})",
        pid, new_binary
    );

    match std::process::Command::new("sh")
        .arg("-c")
        .arg(&script)
        .spawn()
    {
        Ok(_) => app.exit(0),
        Err(e) => {
            log::error!("[updater:linux] Failed to spawn relaunch helper: {}", e);
            app.exit(0);
        }
    }
}
```

- [ ] **Step 2: Register the module in main.rs**

In `desktop/src-tauri/src/main.rs`, add:

```rust
#[cfg(target_os = "linux")]
mod linux_updater;
```

Place it near the other `mod` declarations at the top of the file.

- [ ] **Step 3: Verify compilation**

Run: `cd desktop/src-tauri && cargo check`
Expected: Compiles. Module is declared but not yet called.

- [ ] **Step 4: Commit**

```bash
git add desktop/src-tauri/src/linux_updater.rs desktop/src-tauri/src/main.rs
git commit -m "feat(linux): add custom updater module for tar.gz updates"
```

---

### Task 3: Wire Linux Updater into updater.rs

**Files:**
- Modify: `desktop/src-tauri/src/updater.rs`

Four functions need `#[cfg(target_os = "linux")]` branches to dispatch to the new Linux updater.

- [ ] **Step 1: Make UPDATE_INFO and INSTALL_FAILED pub(crate)**

In `updater.rs`, change visibility so `linux_updater.rs` can access shared state:

```rust
// Change these from private to pub(crate):
pub(crate) static UPDATE_READY: AtomicBool = AtomicBool::new(false);
pub(crate) static INSTALL_FAILED: AtomicBool = AtomicBool::new(false);
pub(crate) static CHANNEL_SWITCH_PENDING: AtomicBool = AtomicBool::new(false);
pub(crate) static UPDATE_INFO: Mutex<Option<UpdateInfo>> = Mutex::new(None);
```

And make `UpdateInfo` derive `Clone`:
```rust
#[derive(Clone, Debug, Serialize)]
```
(It already derives Clone — verify this.)

- [ ] **Step 2: Add Linux branch to check_download_and_install()**

At the **top** of the `check_download_and_install` function body, add:

```rust
async fn check_download_and_install(app: &AppHandle, force_downgrade: bool) {
    #[cfg(target_os = "linux")]
    {
        crate::linux_updater::check_and_download(app, force_downgrade).await;
        return;
    }

    // ... existing tauri-plugin-updater code unchanged ...
```

- [ ] **Step 3: Add Linux branch to check_update_now()**

At the **top** of the `check_update_now` function body:

```rust
pub async fn check_update_now(app: AppHandle) -> Result<String, String> {
    #[cfg(target_os = "linux")]
    {
        return crate::linux_updater::check_now(&app).await;
    }

    // ... existing code unchanged ...
```

- [ ] **Step 4: Add Linux branch to apply_update_now()**

Replace the entire function body:

```rust
#[tauri::command]
#[allow(unreachable_code)]
pub fn apply_update_now(app: AppHandle) -> Result<(), String> {
    if !is_update_ready() {
        return Err("No update available".to_string());
    }

    #[cfg(target_os = "linux")]
    {
        log::info!("[updater] Linux: applying update and relaunching...");
        crate::linux_updater::apply_update(&app);
        return Ok(());
    }

    #[cfg(not(target_os = "linux"))]
    {
        log::info!("[updater] User requested update, restarting...");
        app.restart();
        Ok(())
    }
}
```

- [ ] **Step 5: Add Linux branch to install_pending_update()**

Replace the entire function body:

```rust
#[allow(unreachable_code)]
pub fn install_pending_update(app: &AppHandle) -> bool {
    if !is_update_ready() {
        return false;
    }

    #[cfg(target_os = "linux")]
    {
        // On Linux, update is applied via apply_update_now → pkexec.
        // If user exits without applying, nothing to do (no staged binary).
        return false;
    }

    #[cfg(not(target_os = "linux"))]
    {
        log::info!("[updater] Applying pending update on exit...");
        app.restart();
        true
    }
}
```

- [ ] **Step 6: Verify compilation**

Run: `cd desktop/src-tauri && cargo check`
Expected: Compiles without errors.

- [ ] **Step 7: Run existing tests**

Run: `cd desktop/src-tauri && cargo test`
Expected: All existing tests pass (Linux branches are `#[cfg]`-gated, won't affect macOS test run).

- [ ] **Step 8: Commit**

```bash
git add desktop/src-tauri/src/updater.rs
git commit -m "feat(linux): wire custom updater into existing updater.rs dispatch"
```

---

### Task 4: Update Install Script for tar.gz

**Files:**
- Modify: `web/public/i/k2`

- [ ] **Step 1: Replace check_linux_deps with install_webkit2gtk**

In `web/public/i/k2`, replace the entire `check_linux_deps()` function with:

```sh
install_webkit2gtk() {
    if (ldconfig -p 2>/dev/null | grep -q "libwebkit2gtk-4.1") || \
       (command -v pkg-config >/dev/null 2>&1 && pkg-config --exists webkit2gtk-4.1 2>/dev/null); then
        return 0
    fi

    info "Installing webkit2gtk-4.1 (required for Kaitu GUI)..."
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update -qq && apt-get install -y -qq libwebkit2gtk-4.1-0
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y webkit2gtk4.1
    elif command -v pacman >/dev/null 2>&1; then
        pacman -S --noconfirm webkit2gtk-4.1
    elif command -v zypper >/dev/null 2>&1; then
        zypper install -y webkit2gtk-4.1
    else
        error "Cannot auto-install webkit2gtk-4.1. Install it manually and re-run."
    fi
}
```

- [ ] **Step 2: Replace install_linux() function**

Replace the entire `install_linux()` function with:

```sh
install_linux() {
    if [ "$ARCH" != "amd64" ]; then
        error "Linux desktop currently only supports amd64."
    fi

    install_webkit2gtk

    INSTALL_DIR="/opt/kaitu"
    mkdir -p "$INSTALL_DIR"

    info "Downloading Kaitu v${VERSION}..."
    TMP_TGZ="/tmp/kaitu-install-${VERSION}.tar.gz"
    download_with_fallback "$TMP_TGZ" \
        "${CDN_PRIMARY}/desktop/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz" \
        "${CDN_FALLBACK}/desktop/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz"

    info "Extracting to ${INSTALL_DIR}..."
    tar xzf "$TMP_TGZ" -C "$INSTALL_DIR"
    chmod +x "${INSTALL_DIR}/k2app" "${INSTALL_DIR}/k2"
    rm -f "$TMP_TGZ"

    ln -sf "${INSTALL_DIR}/k2" /usr/local/bin/k2
    info "k2 available at /usr/local/bin/k2"

    info "Installing k2 systemd service..."
    "${INSTALL_DIR}/k2" service install

    # Desktop entry
    REAL_USER="${SUDO_USER:-$(whoami)}"
    REAL_HOME=$(eval echo "~${REAL_USER}")
    DESKTOP_DIR="${REAL_HOME}/.local/share/applications"
    mkdir -p "$DESKTOP_DIR"

    cat > "${DESKTOP_DIR}/kaitu.desktop" << ENTRY
[Desktop Entry]
Name=Kaitu
Comment=Kaitu VPN
Exec=${INSTALL_DIR}/k2app
Icon=${INSTALL_DIR}/kaitu.png
Type=Application
Categories=Network;VPN;
StartupWMClass=kaitu
ENTRY
    chown "${REAL_USER}:" "${DESKTOP_DIR}/kaitu.desktop"

    # Uninstaller
    cat > /usr/local/bin/kaitu-uninstall << 'UNINSTALL'
#!/usr/bin/env bash
set -euo pipefail
if [ "$(id -u)" -ne 0 ]; then echo "Run with sudo: sudo kaitu-uninstall"; exit 1; fi
echo "Uninstalling Kaitu..."
systemctl stop k2 2>/dev/null || true
systemctl disable k2 2>/dev/null || true
/opt/kaitu/k2 service uninstall 2>/dev/null || true
rm -rf /opt/kaitu
rm -f /usr/local/bin/k2 /usr/local/bin/kaitu-uninstall
for home_dir in /home/*/; do rm -f "${home_dir}.local/share/applications/kaitu.desktop" 2>/dev/null || true; done
rm -f /root/.local/share/applications/kaitu.desktop 2>/dev/null || true
if [ "${1:-}" = "--purge" ]; then
  for home_dir in /home/*/; do rm -rf "${home_dir}.local/share/kaitu" "${home_dir}.cache/k2" 2>/dev/null || true; done
  rm -rf /var/log/kaitu 2>/dev/null || true
  echo "Purged all data and logs."
fi
echo "Kaitu uninstalled."
UNINSTALL
    chmod +x /usr/local/bin/kaitu-uninstall

    info ""
    info "=== Installation complete ==="
    info "  GUI:       ${INSTALL_DIR}/k2app"
    info "  CLI:       k2 (in PATH)"
    info "  Service:   systemctl status k2"
    info "  Uninstall: sudo kaitu-uninstall"
    info ""
    info "Launch Kaitu from your application menu or run:"
    info "  ${INSTALL_DIR}/k2app"
}
```

Key differences from current script:
- Downloads tar.gz (not AppImage + separate k2 binary)
- Auto-installs webkit2gtk (not just error-and-exit)
- No libfuse2 check
- `.desktop` Exec points to `k2app` (not `Kaitu.AppImage`)
- No AppImage icon extraction (icon is in tar.gz)

- [ ] **Step 3: Commit**

```bash
git add web/public/i/k2
git commit -m "feat(linux): install script downloads tar.gz, auto-installs webkit2gtk"
```

---

### Task 5: Update Makefile build-linux Target

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Replace build-linux target**

Replace the existing `build-linux` target (lines 84-92 in Makefile) with:

```makefile
build-linux: pre-build build-webapp build-k2-linux
	cd desktop && yarn tauri build --no-bundle
	@echo "--- Packaging tar.gz ---"
	@mkdir -p release/$(VERSION)/linux-pkg
	@cp desktop/src-tauri/target/release/kaitu release/$(VERSION)/linux-pkg/k2app
	@cp $(K2_BIN)/k2-x86_64-unknown-linux-gnu release/$(VERSION)/linux-pkg/k2
	@cp desktop/src-tauri/icons/128x128.png release/$(VERSION)/linux-pkg/kaitu.png
	@chmod +x release/$(VERSION)/linux-pkg/k2app release/$(VERSION)/linux-pkg/k2
	@cd release/$(VERSION)/linux-pkg && tar czf ../Kaitu_$(VERSION)_amd64.tar.gz k2app k2 kaitu.png
	@rm -rf release/$(VERSION)/linux-pkg
	@echo "=== Linux build complete ==="
	@echo "Release artifacts in release/$(VERSION)/:"
	@ls -la release/$(VERSION)/
```

> **Note**: The Tauri binary name is `kaitu` (lowercase of `productName` in tauri.conf.json). Verify this exists after build. If it's `k2app` or another name, adjust accordingly. This can only be confirmed on a Linux CI run.

- [ ] **Step 2: Commit**

```bash
git add Makefile
git commit -m "build(linux): switch from AppImage to tar.gz packaging"
```

---

### Task 6: Update Publish Script

**Files:**
- Modify: `scripts/publish-desktop.sh`

- [ ] **Step 1: Change Linux signature file reference**

In `scripts/publish-desktop.sh`, change the LINUX_SIG line (around line 69):

```bash
# From:
LINUX_SIG=$(cat "${TMPDIR}/Kaitu_${VERSION}_amd64.AppImage.sig" 2>/dev/null || echo "")
# To:
LINUX_SIG=$(cat "${TMPDIR}/Kaitu_${VERSION}_amd64.tar.gz.sig" 2>/dev/null || echo "")
```

- [ ] **Step 2: Change Linux URL in cloudfront.latest.json**

In both `cloudfront.latest.json` and `d0.latest.json` templates, change the `linux-x86_64` URL:

```bash
# From (in cloudfront.latest.json):
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_amd64.AppImage",
# To:
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz",

# From (in d0.latest.json):
      "url": "https://d0.all7.cc/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_amd64.AppImage",
# To:
      "url": "https://d0.all7.cc/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz",
```

- [ ] **Step 3: Update GitHub Release table**

Change the release notes table (around line 179):

```bash
# From:
| **Linux** (x86_64) | \`.AppImage\` | \`.AppImage\` (auto-update) |
# To:
| **Linux** (x86_64) | \`tar.gz\` | \`tar.gz\` (auto-update) |
```

- [ ] **Step 4: Commit**

```bash
git add scripts/publish-desktop.sh
git commit -m "build(linux): publish script uses tar.gz instead of AppImage"
```

---

### Task 7: Update CI Workflow

**Files:**
- Modify: `.github/workflows/release-desktop.yml`

- [ ] **Step 1: Add minisign install + signing step**

After the "Build Linux" step (around line 340), add a signing step:

```yaml
    - name: Install minisign (Linux)
      if: matrix.platform == 'Linux'
      run: |
        curl -fsSL https://github.com/jedisct1/minisign/releases/download/0.11/minisign-0.11-linux.tar.gz | tar xzf - -C /tmp
        sudo cp /tmp/minisign-linux/x86_64/minisign /usr/local/bin/minisign

    - name: Sign Linux tar.gz
      if: matrix.platform == 'Linux'
      env:
        TAURI_SIGNING_PRIVATE_KEY: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY }}
        TAURI_SIGNING_PRIVATE_KEY_PASSWORD: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY_PASSWORD }}
      run: |
        VERSION=$(node -p "require('./package.json').version")
        echo "$TAURI_SIGNING_PRIVATE_KEY" | base64 -d > /tmp/minisign.key
        echo "$TAURI_SIGNING_PRIVATE_KEY_PASSWORD" | minisign -S -s /tmp/minisign.key \
          -m "release/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz"
        mv "release/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz.minisig" \
           "release/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz.sig"
        rm -f /tmp/minisign.key
```

> **Note**: Install minisign from prebuilt binary (faster than `cargo install`). The password is piped via stdin.

- [ ] **Step 2: Update Linux S3 upload step**

Replace the existing "Upload Linux to S3" step:

```yaml
    - name: Upload Linux to S3
      if: matrix.platform == 'Linux'
      env:
        AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
        AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
        AWS_DEFAULT_REGION: ap-northeast-1
      run: |
        VERSION=$(node -p "require('./package.json').version")
        S3_BASE="s3://d0.all7.cc/kaitu/desktop/${VERSION}"
        aws s3 cp "release/${VERSION}/" "${S3_BASE}/" --recursive \
          --exclude "*" \
          --include "*.tar.gz" --include "*.tar.gz.sig"
        aws s3 cp "desktop/src-tauri/binaries/k2-x86_64-unknown-linux-gnu" \
          "${S3_BASE}/k2-linux-amd64"
```

- [ ] **Step 3: Update Slack notification**

Change the Linux download URL in the success notification (around line 417):

```yaml
          elif [ "${PLATFORM}" = "Linux" ]; then
            DOWNLOAD_URL="<${CDN_BASE}/Kaitu_${VERSION}_amd64.tar.gz|Linux tar.gz>"
```

- [ ] **Step 4: Remove TAURI_SIGNING env from Linux build step**

The Linux build step no longer needs `TAURI_SIGNING_PRIVATE_KEY` since `--no-bundle` doesn't generate updater artifacts. The signing is now a separate step. Remove the env block from the "Build Linux" step:

```yaml
    - name: Build Linux (k2 + webapp + tar.gz)
      if: matrix.platform == 'Linux'
      run: make build-linux
```

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/release-desktop.yml
git commit -m "ci(linux): sign and upload tar.gz instead of AppImage"
```

---

### Task 8: Verify Full Build Pipeline (Linux-only, CI)

This task cannot be completed locally (needs Linux host). It verifies the entire pipeline works on the first CI run.

- [ ] **Step 1: Create a test branch and push**

```bash
git checkout -b feat/linux-tgz
git push -u origin feat/linux-tgz
```

- [ ] **Step 2: Monitor CI**

Watch the Linux matrix job in the release workflow. Key checkpoints:
1. `yarn tauri build --no-bundle` succeeds
2. `target/release/kaitu` exists (binary name)
3. `tar czf` produces the tar.gz
4. `minisign -S` signs successfully (base64-decoded key works)
5. S3 upload includes `.tar.gz` + `.tar.gz.sig` + `k2-linux-amd64`

- [ ] **Step 3: Verify tar.gz content and size**

Download the tar.gz from S3 and verify:
```bash
tar tzf Kaitu_*.tar.gz   # Should list: k2app, k2, kaitu.png
ls -lh Kaitu_*.tar.gz    # Should be ~6-8MB
```

- [ ] **Step 4: Verify signature**

```bash
# Decode the pubkey
echo "dW50cnVzdGVkIGNvbW1lbnQ6IG1pbmlzaWduIHB1YmxpYyBrZXk6IDQwRDc1NDVGRDdDRURFODMKUldTRDNzN1hYMVRYUUxhU2FmRlF5SXljRUdINXYwZDdFT3NQVW1RR0pNUmpuQ3VxcTNlQVZLRUUK" | base64 -d > /tmp/kaitu.pub
minisign -V -p /tmp/kaitu.pub -m Kaitu_*.tar.gz
```
Expected: `Signature and comment signature verified`

---

### Task 9: End-to-End Update Test

- [ ] **Step 1: Install old version on Linux VM**

```bash
curl -fsSL https://kaitu.io/i/k2 | sudo bash
```

Verify: `/opt/kaitu/k2app` and `/opt/kaitu/k2` exist, `systemctl status k2` is running.

- [ ] **Step 2: Publish new version**

Run `scripts/publish-desktop.sh` with the new version.

- [ ] **Step 3: Launch k2app and wait for update notification**

Launch `/opt/kaitu/k2app`. Within 5 minutes (5s initial + check), the app should show "update-ready" notification.

- [ ] **Step 4: Apply update**

Click "Update Now" in the app. Expected:
1. pkexec password dialog appears
2. After entering password: binaries extracted to `/opt/kaitu/`
3. k2 daemon restarted
4. App relaunches with new version

- [ ] **Step 5: Verify new version**

Check `/opt/kaitu/k2 version` shows the new version.
