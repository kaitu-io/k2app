# Linux tar.gz Distribution + Custom Updater

**Date**: 2026-03-18
**Status**: Design
**Problem**: Linux AppImage is 85MB (bundles webkit2gtk), causing slow downloads, high update failure rate (2/3 fail), and FUSE dependency issues on Ubuntu 24.04.
**Solution**: Replace AppImage with tar.gz (~5-8MB) containing only the binaries. Rely on system webkit2gtk. Custom updater in Rust replaces tauri-plugin-updater on Linux.

## Architecture Overview

```
                    ┌─ CDN (S3 + CloudFront)
                    │  ├─ latest.json  (linux-x86_64 → tar.gz URL)
                    │  ├─ Kaitu_{VER}_amd64.tar.gz     (~5-8MB)
                    │  └─ Kaitu_{VER}_amd64.tar.gz.sig  (minisign)
                    │
  First install     │         Auto-update
  ──────────────    │         ───────────
  curl | sudo bash  │         updater.rs (Linux branch)
        │           │              │
        ▼           │              ▼
  web/public/i/k2   │    1. Poll latest.json (reuse channel logic)
        │           │    2. Compare semver
        ▼           │    3. Download tar.gz → /tmp/kaitu-update/
  /opt/kaitu/       │    4. Verify minisign signature
  ├─ k2app          │    5. pkexec tar xzf → /opt/kaitu/
  ├─ k2             │    6. systemctl restart k2
  ├─ kaitu.png      │    7. Spawn relaunch helper → exit app
  └─ (uninstaller)  │
```

## Decisions

| Decision | Choice | Why |
|----------|--------|-----|
| Update granularity | Coupled (k2app + k2 together) | Version consistency, 5-8MB is small enough |
| Update executor | Rust self-implemented in updater.rs | Reuse channel/version/UI logic, secure signature verification |
| Signature | Existing minisign keypair | CI already has TAURI_SIGNING_PRIVATE_KEY |
| Install path | `/opt/kaitu/` | Already established by service.rs and install script |
| Privilege elevation | pkexec (fallback: prompt sudo) | Consistent with service install flow |
| latest.json format | Reuse existing, change URL to tar.gz | No macOS/Windows breakage |
| webkit2gtk | Auto-install from system package manager | Script runs as sudo, has root |
| libfuse2 | No longer needed | No AppImage = no FUSE mount |

## Component 1: tar.gz Build (`Makefile`)

### Current `build-linux` target
```makefile
build-linux: pre-build build-webapp build-k2-linux
	cd desktop && yarn tauri build --bundles appimage
	cp .../*.AppImage release/...
```

### New `build-linux` target
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
```

> **Note**: Tauri v2 uses `--no-bundle` flag (v1 used `--bundles none`). This compiles the binary to `target/release/kaitu` without creating AppImage/deb/rpm. The binary name is `kaitu` (lowercase of `productName` in tauri.conf.json).

**Signing** (CI step, after tar.gz created):
```bash
# TAURI_SIGNING_PRIVATE_KEY is base64-encoded minisign key — decode first
echo "$TAURI_SIGNING_PRIVATE_KEY" | base64 -d > /tmp/minisign.key
minisign -S -s /tmp/minisign.key \
  -m "release/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz"
rm -f /tmp/minisign.key
```

> **Key format**: Tauri stores minisign private key as base64 in `TAURI_SIGNING_PRIVATE_KEY`. Must `base64 -d` before passing to minisign CLI. The resulting `.tar.gz.minisig` file is standard minisign format — the `signature` field in latest.json stores this as-is (not double-encoded).

### tar.gz contents (~5-8MB compressed)

| File | Source | Size (est.) |
|------|--------|-------------|
| `k2app` | `target/release/kaitu` (Tauri binary, dynamically links webkit2gtk, stripped+LTO) | ~3-6MB |
| `k2` | `k2/build/k2-linux-amd64` (Go daemon) | ~13MB |
| `kaitu.png` | `desktop/src-tauri/icons/128x128.png` | ~5KB |

Uncompressed: ~16-19MB. Compressed (gzip): **~6-8MB** (Go and Rust binaries compress well).

> The Rust binary is smaller than macOS because: no .app bundle overhead, dynamically links webkit2gtk, and `Cargo.toml` has `strip = true`, `lto = true`, `opt-level = "s"`.

## Component 2: Install Script (`web/public/i/k2`)

### Changes from current script

1. **Remove** `libfuse2` dependency check (no AppImage)
2. **Auto-install** webkit2gtk if missing (script runs as root)
3. **Download** tar.gz instead of AppImage + separate k2
4. **Extract** tar.gz to `/opt/kaitu/`
5. **Keep** systemd install, .desktop entry, uninstaller

### webkit2gtk auto-install logic

```sh
install_webkit2gtk() {
    if (ldconfig -p 2>/dev/null | grep -q "libwebkit2gtk-4.1") || \
       (command -v pkg-config >/dev/null 2>&1 && pkg-config --exists webkit2gtk-4.1 2>/dev/null); then
        return 0  # already installed
    fi

    info "Installing webkit2gtk-4.1..."
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

### Updated `install_linux()`

```sh
install_linux() {
    [ "$ARCH" = "amd64" ] || error "Linux desktop currently only supports amd64."

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

    info "Installing k2 systemd service..."
    "${INSTALL_DIR}/k2" service install

    # .desktop entry
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

    # Uninstaller (same as before, minus AppImage references)
}
```

## Component 3: Linux Updater (`updater.rs`)

### Architecture

The Linux updater **replaces** tauri-plugin-updater for Linux. macOS and Windows continue using the Tauri updater unchanged.

```
updater.rs
├── start_auto_updater()           — unchanged entry point
├── check_download_and_install()   — dispatch to platform-specific path
│   ├── [macOS/Windows] tauri-plugin-updater (existing)
│   └── [Linux] linux_check_download_and_install() ← NEW
├── linux_check_download_and_install()
│   ├── fetch_latest_json()        — HTTP GET, parse version + URL + signature
│   ├── compare_version()          — semver comparison (reuse channel/force logic)
│   ├── download_tar_gz()          — reqwest download to /tmp with progress
│   ├── verify_signature()         — minisign-verify crate
│   └── apply_linux_update()       — pkexec tar xzf + systemctl restart + relaunch
└── UI integration (existing)
    ├── UPDATE_READY / UPDATE_INFO — unchanged
    ├── emit("update-ready")       — unchanged
    └── apply_update_now()         — Linux: relaunch from /opt/kaitu/k2app
```

### Key implementation details

**1. latest.json parsing (no Tauri updater dependency)**

```rust
#[derive(Deserialize)]
struct LatestJson {
    version: String,
    notes: Option<String>,
    pub_date: Option<String>,
    platforms: HashMap<String, PlatformEntry>,
}

#[derive(Deserialize)]
struct PlatformEntry {
    url: String,
    signature: String,
}
```

Fetch from same endpoints as channel.rs (`STABLE_ENDPOINTS` / `BETA_ENDPOINTS`). Try each endpoint in order (CDN failover).

**2. Version comparison**

Reuse existing logic:
- Parse semver: `0.4.0-beta.6 < 0.4.0` (beta→stable is upgrade)
- Normal check: `remote > current` → update available
- Force downgrade (`set_update_channel`): `remote != current` → update available

Use `semver` crate (add explicitly to Cargo.toml — transitive dependencies from Tauri are not directly accessible).

**3. Download with progress (atomic)**

```rust
async fn download_tar_gz(url: &str, dest: &Path) -> Result<(), String> {
    let client = reqwest::Client::new();
    let resp = client.get(url).send().await.map_err(|e| e.to_string())?;
    let total = resp.content_length();

    // Download to .tmp first, rename on completion (atomic against partial downloads)
    let tmp_dest = dest.with_extension("tar.gz.tmp");
    let mut file = tokio::fs::File::create(&tmp_dest).await.map_err(|e| e.to_string())?;
    let mut stream = resp.bytes_stream();
    let mut downloaded = 0u64;

    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|e| {
            let _ = std::fs::remove_file(&tmp_dest);
            e.to_string()
        })?;
        file.write_all(&chunk).await.map_err(|e| e.to_string())?;
        downloaded += chunk.len() as u64;
        // Log progress at 10% intervals (same pattern as existing updater)
    }
    drop(file);
    tokio::fs::rename(&tmp_dest, dest).await.map_err(|e| e.to_string())?;
    Ok(())
}
```

**4. Signature verification**

Add `minisign-verify` crate to Cargo.toml. The public key is the base64 string from `tauri.conf.json` `plugins.updater.pubkey`. Hardcode in Rust (it's a public key, safe to embed).

The `.tar.gz.minisig` file produced by `minisign -S` is standard minisign format. The `signature` field in `latest.json` stores the content of this file as-is.

```rust
use minisign_verify::{PublicKey, Signature};

/// Minisign public key (from tauri.conf.json plugins.updater.pubkey)
const MINISIGN_PUBKEY: &str = "RWSDHs7XV1TXQLaSafFQyIycEGH5v0d7EOsPUmQGJMRjnCuqq3eAVKEE";

fn verify_signature(tar_gz_path: &Path, sig_path: &Path) -> Result<(), String> {
    let pk = PublicKey::from_base64(MINISIGN_PUBKEY).map_err(|e| e.to_string())?;
    let sig_str = std::fs::read_to_string(sig_path).map_err(|e| e.to_string())?;
    let sig = Signature::decode(&sig_str).map_err(|e| e.to_string())?;
    let data = std::fs::read(tar_gz_path).map_err(|e| e.to_string())?;
    pk.verify(&data, &sig, false).map_err(|e| format!("Signature verification failed: {}", e))
}
```

> **Flow**: Download both `.tar.gz` and `.tar.gz.minisig` → verify locally → proceed. The signature file is a separate download (not embedded in latest.json for Linux, unlike macOS/Windows where Tauri embeds it).

**5. Apply update (pkexec + restart)**

```rust
fn apply_linux_update(tar_gz_path: &Path) -> Result<(), String> {
    // pkexec: extract tar.gz to staging dir, then atomically move files + restart k2
    // Two-phase: extract to temp dir first, then mv — protects against interrupted extraction
    let script = format!(
        "STAGE=$(mktemp -d /tmp/kaitu-update-XXXXXX) && \
         tar xzf '{}' -C \"$STAGE\" && \
         chmod +x \"$STAGE/k2app\" \"$STAGE/k2\" && \
         cp -f \"$STAGE/k2app\" /opt/kaitu/k2app && \
         cp -f \"$STAGE/k2\" /opt/kaitu/k2 && \
         [ -f \"$STAGE/kaitu.png\" ] && cp -f \"$STAGE/kaitu.png\" /opt/kaitu/kaitu.png; \
         rm -rf \"$STAGE\" && \
         systemctl restart k2 2>/dev/null || true",
        tar_gz_path.display()
    );

    let output = std::process::Command::new("pkexec")
        .args(["bash", "-c", &script])
        .output()
        .map_err(|e| format!("pkexec failed: {}", e))?;

    if !output.status.success() {
        if output.status.code() == Some(126) {
            return Err("User cancelled".to_string());
        }
        return Err(format!("Update failed: {}", String::from_utf8_lossy(&output.stderr)));
    }
    Ok(())
}
```

**6. App relaunch after update**

After pkexec completes, the new k2app binary is at `/opt/kaitu/k2app`. Relaunch:

```rust
fn relaunch_from_opt(app: &AppHandle) {
    let new_binary = "/opt/kaitu/k2app";
    let pid = std::process::id();

    // Spawn helper that waits for current process to exit, then launches new binary
    // Note: no semicolon after '&' — '&' already terminates the background command
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

    match std::process::Command::new("sh")
        .arg("-c")
        .arg(&script)
        .spawn()
    {
        Ok(_) => app.exit(0),
        Err(e) => {
            log::error!("[updater] Failed to spawn relaunch helper: {}", e);
            // User will need to relaunch manually
            app.exit(0);
        }
    }
}
```

### Flow integration with existing updater.rs

**Four** existing functions need Linux `#[cfg]` branches:

**1. `check_download_and_install()` — auto-check loop**
```rust
async fn check_download_and_install(app: &AppHandle, force_downgrade: bool) {
    #[cfg(target_os = "linux")]
    {
        linux_check_download_and_install(app, force_downgrade).await;
        return;
    }

    // Existing tauri-plugin-updater flow for macOS/Windows (unchanged)
    // ...
}
```

**2. `check_update_now()` — manual IPC check**

Currently calls `app.updater_builder()` directly (bypasses `check_download_and_install`). Must also dispatch to Linux path:
```rust
pub async fn check_update_now(app: AppHandle) -> Result<String, String> {
    #[cfg(target_os = "linux")]
    {
        return linux_check_update_now(&app).await;
    }

    // Existing tauri-plugin-updater flow (unchanged)
    // ...
}
```

**3. `apply_update_now()` — user clicks "Update Now"**
```rust
pub fn apply_update_now(app: AppHandle) -> Result<(), String> {
    if !is_update_ready() {
        return Err("No update available".to_string());
    }

    #[cfg(target_os = "linux")]
    {
        log::info!("[updater] Applying Linux update, relaunching...");
        relaunch_from_opt(&app);
        return Ok(());
    }

    #[cfg(not(target_os = "linux"))]
    {
        app.restart();
        Ok(())
    }
}
```

**4. `install_pending_update()` — called from RunEvent::ExitRequested**

On Linux, `UPDATE_READY` means tar.gz was downloaded+verified but NOT yet installed. `app.restart()` would just relaunch the old binary. Linux must apply the update (pkexec + extract) before relaunching:
```rust
pub fn install_pending_update(app: &AppHandle) -> bool {
    if !is_update_ready() {
        return false;
    }

    #[cfg(target_os = "linux")]
    {
        // On Linux, the update was already applied via apply_update_now → pkexec.
        // If we get here, user is exiting without applying — nothing to do.
        // (Unlike macOS where tauri-plugin-updater stages the binary for restart)
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

### Linux update state machine

```
linux_check_download_and_install()
  │
  ├─ fetch latest.json (CDN failover)
  ├─ compare semver
  ├─ download tar.gz → /tmp/kaitu-update.tar.gz.tmp → rename
  ├─ download .minisig → /tmp/kaitu-update.tar.gz.minisig
  ├─ verify_signature()
  │
  ├─ Store tar.gz path in LINUX_UPDATE_PATH (new static)
  ├─ Set UPDATE_READY = true
  └─ Emit "update-ready" event to frontend

apply_update_now() [user clicks "Update Now"]
  │
  ├─ apply_linux_update(LINUX_UPDATE_PATH)
  │   └─ pkexec: stage → extract → cp → systemctl restart k2
  ├─ relaunch_from_opt()
  │   └─ spawn helper: poll PID → nohup /opt/kaitu/k2app
  └─ app.exit(0)
```

### New Cargo.toml dependencies

```toml
[target.'cfg(target_os = "linux")'.dependencies]
minisign-verify = "0.2"
semver = "1"
```

`reqwest` (with `stream` feature) and `futures-util` are already unconditional dependencies.

## Component 4: Publish Script (`scripts/publish-desktop.sh`)

### Changes

1. Linux signature file: `.tar.gz.sig` instead of `.AppImage.sig`
2. Linux URL in latest.json: `.tar.gz` instead of `.AppImage`

```diff
-LINUX_SIG=$(cat "${TMPDIR}/Kaitu_${VERSION}_amd64.AppImage.sig" 2>/dev/null || echo "")
+LINUX_SIG=$(cat "${TMPDIR}/Kaitu_${VERSION}_amd64.tar.gz.sig" 2>/dev/null || echo "")

-    "linux-x86_64": {
-      "url": ".../${VERSION}/Kaitu_${VERSION}_amd64.AppImage",
+    "linux-x86_64": {
+      "url": ".../${VERSION}/Kaitu_${VERSION}_amd64.tar.gz",
```

## Component 5: CI Workflow (`release-desktop.yml`)

### Linux job changes

1. **Build step**: `make build-linux` (Makefile handles tar.gz packaging)
2. **Sign step**: Use minisign CLI to sign tar.gz (install via `cargo install minisign`)
3. **Upload step**: Upload `.tar.gz` + `.tar.gz.sig` + `k2-linux-amd64` to S3
4. **Remove**: AppImage-related references

```yaml
- name: Build Linux (k2 + webapp + tar.gz)
  if: matrix.platform == 'Linux'
  env:
    TAURI_SIGNING_PRIVATE_KEY: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY }}
    TAURI_SIGNING_PRIVATE_KEY_PASSWORD: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY_PASSWORD }}
  run: make build-linux

- name: Install minisign
  if: matrix.platform == 'Linux'
  run: cargo install minisign

- name: Sign Linux tar.gz
  if: matrix.platform == 'Linux'
  env:
    TAURI_SIGNING_PRIVATE_KEY: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY }}
    TAURI_SIGNING_PRIVATE_KEY_PASSWORD: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY_PASSWORD }}
  run: |
    VERSION=$(node -p "require('./package.json').version")
    # TAURI_SIGNING_PRIVATE_KEY is base64-encoded — decode to minisign key format
    echo "$TAURI_SIGNING_PRIVATE_KEY" | base64 -d > /tmp/minisign.key
    echo "$TAURI_SIGNING_PRIVATE_KEY_PASSWORD" | minisign -S -s /tmp/minisign.key \
      -m "release/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz"
    # Move .minisig to .sig for consistent naming
    mv "release/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz.minisig" \
       "release/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz.sig"
    rm -f /tmp/minisign.key

- name: Upload Linux to S3
  if: matrix.platform == 'Linux'
  run: |
    VERSION=$(node -p "require('./package.json').version")
    S3_BASE="s3://d0.all7.cc/kaitu/desktop/${VERSION}"
    aws s3 cp "release/${VERSION}/" "${S3_BASE}/" --recursive \
      --exclude "*" \
      --include "*.tar.gz" --include "*.tar.gz.sig"
    aws s3 cp "desktop/src-tauri/binaries/k2-x86_64-unknown-linux-gnu" \
      "${S3_BASE}/k2-linux-amd64"
```

### CI dependencies change

Remove from Linux build step:
```diff
-    - name: Install Linux system dependencies
-      if: matrix.platform == 'Linux'
-      run: |
-        sudo apt-get install -y \
-          libwebkit2gtk-4.1-dev \    # Still needed for compilation
```

**Note**: `libwebkit2gtk-4.1-dev` is still needed in CI for compilation (Tauri links against it). Only the runtime AppImage bundling is removed.

## Component 6: service.rs Linux changes

### `find_k2_from_sidecar()` — no changes needed

When running from tar.gz install, `current_exe()` returns `/opt/kaitu/k2app`, and `k2` is in the same directory. The existing function already handles this (tries `app_dir.join("k2")` first).

### `admin_reinstall_service_linux()` — simplify

No more FUSE mount issue. k2 binary is already at `/opt/kaitu/k2` (persistent path). The staging-to-/tmp step is no longer needed for tar.gz installs. However, keep it for safety (the function may be called from any context).

## Risks

| Risk | Level | Mitigation |
|------|-------|------------|
| minisign key format (Tauri base64-encodes) | Medium | CI must `base64 -d` before passing to minisign CLI. Test sign+verify in CI before first release |
| Tauri binary name uncertainty | Low | Verify `target/release/kaitu` exists in CI. productName → lowercase |
| pkexec unavailable on some distros | Low | Already handled: fallback to manual sudo instructions |
| webkit2gtk auto-install fails (unusual distro) | Low | Graceful error message with manual install instructions |
| Running app binary overwritten | None | Linux inode semantics: running process keeps old inode reference |
| Partial download corruption | None | Atomic download (.tmp → rename). Partial .tmp cleaned up on error |
| Interrupted extraction corrupts /opt/kaitu/ | Low | Extract to temp dir first, then cp files (two-phase) |
| Active VPN dropped during update | Low | systemctl restart k2 is deferred to apply_update_now (user-initiated) |
| Existing AppImage users get broken update | Low | Clean break — tiny user base (1 beta user), document in release notes |

## What We're NOT Doing

- No .deb/.rpm packages (tar.gz covers all distros)
- No delta updates (5-8MB is small enough)
- No auto-background install (needs root, must ask user)
- No AppImage fallback (clean break)
- No arm64 Linux (amd64 only, same as current)

## Testing Plan

1. **Unit tests**: minisign signature verification with test key
2. **Integration test (local)**: Build tar.gz → install to /tmp/test-kaitu/ → verify binary runs
3. **CI verification**: First beta release with tar.gz, verify download + install on Ubuntu 22.04/24.04
4. **Update flow**: Install old version → publish new → verify auto-update prompt → apply → verify new version running

## Migration

Existing AppImage users need to reinstall via `curl | sudo bash`. The install script overwrites `/opt/kaitu/`. No automatic migration from AppImage to tar.gz (AppImage users' updater would fail once we stop publishing AppImage — they'd see the update prompt but download would fail if the URL format changes).

**Mitigation**:
- Keep one final AppImage release that prompts users to reinstall
- Or: keep latest.json AppImage URL working for one more version, pointing to a "please reinstall" binary

**Recommended**: Clean break. Current Linux user base is tiny (1 known beta user). Document migration in release notes.

## File Changes Summary

| File | Change |
|------|--------|
| `Makefile` | `build-linux` target: `--no-bundle` + tar.gz packaging |
| `desktop/src-tauri/src/updater.rs` | Add `#[cfg(target_os = "linux")]` branches in 4 functions + new Linux updater module |
| `desktop/src-tauri/Cargo.toml` | Add `minisign-verify` + `semver` (Linux only) |
| `web/public/i/k2` | Update `install_linux()`: tar.gz download, auto webkit2gtk install, new .desktop entry |
| `scripts/publish-desktop.sh` | Linux artifact: `.tar.gz` instead of `.AppImage` |
| `.github/workflows/release-desktop.yml` | Linux: install minisign, sign tar.gz, upload tar.gz |
| `desktop/src-tauri/src/service.rs` | Minor: simplify Linux service install (no FUSE staging needed) |
