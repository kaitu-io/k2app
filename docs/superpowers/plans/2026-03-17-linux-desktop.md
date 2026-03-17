# Linux Desktop Support Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Linux desktop support to Kaitu VPN via Tauri v2 AppImage distribution with the same daemon-mode architecture as macOS/Windows.

**Architecture:** Tauri AppImage (normal user) communicates with k2 daemon (systemd service, root) via HTTP on port 1777. Install script downloads AppImage + k2 binary, registers systemd service. CI builds on ubuntu-22.04.

**Tech Stack:** Tauri v2, Rust, Go (k2 sidecar), GitHub Actions, AppImage, systemd, bash install script

**Spec:** `docs/superpowers/specs/2026-03-17-linux-desktop-design.md`

---

### Task 1: Rust service.rs — Linux admin_reinstall_service

Add Linux branch to `admin_reinstall_service()` using `pkexec` for privilege escalation.

**Files:**
- Modify: `desktop/src-tauri/src/service.rs:381-408` (admin_reinstall_service)
- Test: `desktop/src-tauri/src/service.rs` (inline tests module)

- [ ] **Step 1: Write the test for Linux admin reinstall path**

Add to the `#[cfg(test)] mod tests` block in `service.rs`:

```rust
#[cfg(target_os = "linux")]
mod linux_tests {
    use super::super::*;

    /// Verify the k2 binary lookup logic for Linux:
    /// checks /usr/local/bin/k2 first, then sidecar path.
    #[test]
    fn test_linux_k2_binary_lookup() {
        // find_k2_binary_linux should return a PathBuf
        // On dev machines k2 may not exist — we just verify no panic
        let result = find_k2_binary_linux();
        match result {
            Ok(path) => {
                let path_str = path.to_string_lossy();
                assert!(
                    path_str.contains("k2"),
                    "Path should contain 'k2': {}",
                    path_str
                );
            }
            Err(e) => {
                assert!(
                    e.contains("not found"),
                    "Error should indicate k2 not found: {}",
                    e
                );
            }
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/src-tauri && cargo test linux_tests --target x86_64-unknown-linux-gnu 2>&1 || echo "Expected failure — function not defined"`

Expected: Compilation error — `find_k2_binary_linux` not defined.

- [ ] **Step 3: Implement Linux admin_reinstall_service**

In `service.rs`, replace the catch-all block at line 404-407:

```rust
// REPLACE this:
#[cfg(not(any(target_os = "macos", target_os = "windows")))]
{
    Err("Not supported on this platform".to_string())
}

// WITH this:
#[cfg(target_os = "linux")]
{
    tokio::task::spawn_blocking(admin_reinstall_service_linux)
        .await
        .map_err(|e| format!("Task join error: {}", e))?
}
```

Then add the implementation function:

```rust
/// Linux daemon mode: install k2 service with pkexec for privilege escalation.
/// pkexec shows a graphical password dialog on desktop environments.
/// Falls back to error with manual sudo instructions if pkexec is unavailable.
#[cfg(target_os = "linux")]
fn find_k2_binary_linux() -> Result<std::path::PathBuf, String> {
    // 1. Check /usr/local/bin/k2 (install script symlink)
    let system_path = std::path::Path::new("/usr/local/bin/k2");
    if system_path.exists() {
        return Ok(system_path.to_path_buf());
    }

    // 2. Check sidecar path relative to current exe
    let exe_path = std::env::current_exe()
        .map_err(|e| format!("Failed to get exe path: {}", e))?;
    let app_dir = exe_path.parent().ok_or("Failed to get app directory")?;

    // Try plain `k2` first, then `k2-*` (target-triple variant)
    let k2_path = app_dir.join("k2");
    if k2_path.exists() {
        return Ok(k2_path);
    }

    // Find k2-{target-triple} in the same directory
    if let Ok(entries) = std::fs::read_dir(app_dir) {
        for entry in entries.flatten() {
            let name = entry.file_name();
            let name_str = name.to_string_lossy();
            if name_str.starts_with("k2-") && !name_str.contains('.') {
                return Ok(entry.path());
            }
        }
    }

    Err(format!("k2 binary not found in /usr/local/bin or {:?}", app_dir))
}

#[cfg(target_os = "linux")]
fn admin_reinstall_service_linux() -> Result<String, String> {
    let k2_path = find_k2_binary_linux()?;
    let k2_str = k2_path.to_string_lossy();
    log::info!("[service] Linux: installing via pkexec: {}", k2_str);

    // Check if pkexec is available
    let pkexec_available = Command::new("which")
        .arg("pkexec")
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false);

    if !pkexec_available {
        log::warn!("[service] pkexec not available");
        return Err("pkexec_unavailable: run 'sudo k2 service install' manually".to_string());
    }

    let output = Command::new("pkexec")
        .args([&*k2_str, "service", "install"])
        .output()
        .map_err(|e| format!("pkexec failed: {}", e))?;

    if output.status.success() {
        log::info!("[service] Service installed successfully via pkexec");
        Ok("Service installed and started".to_string())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr);
        let stderr_str = stderr.trim();
        // pkexec returns 126 when user dismisses the dialog
        if output.status.code() == Some(126) {
            log::info!("[service] User cancelled pkexec prompt");
            Err("User cancelled".to_string())
        } else {
            log::error!("[service] pkexec failed: {}", stderr_str);
            Err(format!("Failed to install service: {}", stderr_str))
        }
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd desktop/src-tauri && cargo test -- --include-ignored 2>&1 | tail -20`

Expected: All tests pass (Linux test runs on Linux, skipped on macOS/Windows due to `#[cfg(target_os = "linux")]`).

On macOS dev machine, verify compilation: `cd desktop/src-tauri && cargo check`

- [ ] **Step 5: Commit**

```bash
git add desktop/src-tauri/src/service.rs
git commit -m "feat(linux): add admin_reinstall_service Linux branch with pkexec"
```

---

### Task 2: Rust service.rs — Linux detect/cleanup old service + diagnostic fix

Add Linux branch to `detect_old_kaitu_service()` and fix the `k2.exe` diagnostic in `ensure_service_running_daemon`.

**Files:**
- Modify: `desktop/src-tauri/src/service.rs:501-525` (detect_old_kaitu_service)
- Modify: `desktop/src-tauri/src/service.rs:639-644` (diagnostic log)

- [ ] **Step 1: Replace detect_old_kaitu_service catch-all with Linux branch**

In `service.rs`, replace lines 521-524:

```rust
// REPLACE this:
#[cfg(not(any(target_os = "macos", target_os = "windows")))]
{
    false
}

// WITH this:
#[cfg(target_os = "linux")]
{
    std::path::Path::new("/etc/systemd/system/kaitu-service.service").exists()
}
```

- [ ] **Step 2: Add cleanup_old_kaitu_service Linux branch**

In `cleanup_old_kaitu_service()`, add a `#[cfg(target_os = "linux")]` block after the Windows block (after line 556):

```rust
#[cfg(target_os = "linux")]
{
    let _ = Command::new("systemctl").args(["stop", "kaitu-service"]).output();
    let _ = Command::new("systemctl").args(["disable", "kaitu-service"]).output();
    let _ = std::fs::remove_file("/etc/systemd/system/kaitu-service.service");
}
```

- [ ] **Step 3: Fix diagnostic log to use platform-aware binary name**

In `ensure_service_running_daemon`, replace the diagnostic block at line 642:

```rust
// REPLACE this:
let k2_path = exe.parent().map(|d| d.join("k2.exe"));

// WITH this:
let k2_name = if cfg!(windows) { "k2.exe" } else { "k2" };
let k2_path = exe.parent().map(|d| d.join(k2_name));
```

- [ ] **Step 4: Verify compilation**

Run: `cd desktop/src-tauri && cargo check`

Expected: No errors.

- [ ] **Step 5: Run all tests**

Run: `cd desktop/src-tauri && cargo test`

Expected: All existing tests pass.

- [ ] **Step 6: Commit**

```bash
git add desktop/src-tauri/src/service.rs
git commit -m "feat(linux): add detect/cleanup old kaitu-service Linux branch, fix platform-aware k2 binary name"
```

---

### Task 3: Makefile — add build-linux target

Add the `build-linux` target for CI.

**Files:**
- Modify: `Makefile:68` (after build-windows target)

- [ ] **Step 1: Add build-linux target to Makefile**

Add after the `build-windows` target (after line 83):

```makefile
build-linux: pre-build build-webapp build-k2-linux
	cd desktop && yarn tauri build --bundles appimage
	@echo "--- Collecting artifacts ---"
	@mkdir -p release/$(VERSION)
	@cp desktop/src-tauri/target/release/bundle/appimage/*.AppImage release/$(VERSION)/Kaitu_$(VERSION)_amd64.AppImage
	@cp desktop/src-tauri/target/release/bundle/appimage/*.AppImage.sig release/$(VERSION)/Kaitu_$(VERSION)_amd64.AppImage.sig 2>/dev/null || true
	@echo "=== Linux build complete ==="
	@echo "Release artifacts in release/$(VERSION)/:"
	@ls -la release/$(VERSION)/
```

- [ ] **Step 2: Verify Makefile syntax**

Run: `make -n build-linux 2>&1 | head -5`

Expected: Shows the commands that would run (dry-run), no syntax errors.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "feat(linux): add build-linux Makefile target for AppImage"
```

---

### Task 4: CI — add Linux job to release-desktop.yml

Add Linux build job to the release matrix.

**Files:**
- Modify: `.github/workflows/release-desktop.yml:128-140` (matrix strategy)
- Modify: `.github/workflows/release-desktop.yml` (add Linux build + upload steps)

- [ ] **Step 1: Add Linux to the matrix**

In `release-desktop.yml`, add to the matrix `include` array (after the Windows entry, around line 139):

```yaml
          - runner: ubuntu-22.04
            target: x86_64-unknown-linux-gnu
            platform: Linux
```

- [ ] **Step 2: Add Linux system dependencies step**

After the "Verify tools (self-hosted macOS)" step, add:

```yaml
      - name: Install Linux system dependencies
        if: matrix.platform == 'Linux'
        run: |
          sudo apt-get update
          sudo apt-get install -y \
            libwebkit2gtk-4.1-dev \
            libayatana-appindicator3-dev \
            librsvg2-dev \
            libssl-dev \
            patchelf
```

- [ ] **Step 3: Add Linux toolchain setup steps**

After the "Verify Rust Windows target (self-hosted)" step, add:

```yaml
      - name: Setup Node.js (Linux)
        if: matrix.platform == 'Linux'
        uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'yarn'

      - name: Setup Go (Linux)
        if: matrix.platform == 'Linux'
        uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache-dependency-path: k2/go.sum

      - name: Setup Rust (Linux)
        if: matrix.platform == 'Linux'
        uses: dtolnay/rust-toolchain@stable

      - name: Setup sccache (Linux)
        if: matrix.platform == 'Linux'
        uses: mozilla-actions/sccache-action@v0.0.6

      - name: Verify sccache works (Linux)
        if: matrix.platform == 'Linux'
        run: |
          export SCCACHE_GHA_ENABLED=true
          if sccache rustc -vV >/dev/null 2>&1; then
            echo "RUSTC_WRAPPER=sccache" >> "$GITHUB_ENV"
            echo "SCCACHE_GHA_ENABLED=true" >> "$GITHUB_ENV"
          else
            echo "::warning::sccache GHA backend unavailable, compiling without cache"
          fi

      - name: Cache cargo registry (Linux)
        if: matrix.platform == 'Linux'
        uses: actions/cache@v4
        with:
          path: |
            ~/.cargo/registry
            ~/.cargo/git
          key: cargo-registry-Linux-${{ hashFiles('desktop/src-tauri/Cargo.lock') }}
          restore-keys: |
            cargo-registry-Linux-
```

- [ ] **Step 4: Add Linux build step**

After the Windows build step, add:

```yaml
      - name: Build Linux (k2 + webapp + AppImage)
        if: matrix.platform == 'Linux'
        env:
          TAURI_SIGNING_PRIVATE_KEY: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY }}
          TAURI_SIGNING_PRIVATE_KEY_PASSWORD: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY_PASSWORD }}
        run: make build-linux
```

- [ ] **Step 5: Add Linux S3 upload step**

After the Windows upload step, add:

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
            --include "*.AppImage" --include "*.AppImage.sig"
```

- [ ] **Step 6: Add Linux to Slack notification**

In the "Notify Slack on build success" step, add an elif for Linux:

```yaml
          if [ "${PLATFORM}" = "macOS" ]; then
            DOWNLOAD_URL="<${CDN_BASE}/Kaitu_${VERSION}_universal.pkg|macOS PKG>"
          elif [ "${PLATFORM}" = "Linux" ]; then
            DOWNLOAD_URL="<${CDN_BASE}/Kaitu_${VERSION}_amd64.AppImage|Linux AppImage>"
          else
            DOWNLOAD_URL="<${CDN_BASE}/Kaitu_${VERSION}_x64.exe|Windows Setup>"
          fi
```

- [ ] **Step 7: Commit**

```bash
git add .github/workflows/release-desktop.yml
git commit -m "feat(linux): add Linux AppImage build job to release-desktop CI"
```

---

### Task 5: publish-desktop.sh — add Linux platform to manifests

Add Linux signature reading and `linux-x86_64` platform entry to the updater manifests.

**Files:**
- Modify: `scripts/publish-desktop.sh:60-132` (sig download + manifest generation)

- [ ] **Step 1: Add Linux sig download**

After line 68 (`WINDOWS_SIG=...`), add:

```bash
LINUX_SIG=$(cat "${TMPDIR}"/*.AppImage.sig 2>/dev/null || echo "")
```

And after the Windows sig warning:

```bash
if [ -z "${LINUX_SIG}" ]; then
  echo "WARNING: Linux signature not found"
fi
```

- [ ] **Step 2: Add linux-x86_64 to cloudfront.latest.json**

In the `cloudfront.latest.json` heredoc, add after the `windows-x86_64` entry (before the closing `}`):

```json
    ,"linux-x86_64": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_amd64.AppImage",
      "signature": "${LINUX_SIG}"
    }
```

- [ ] **Step 3: Add linux-x86_64 to d0.latest.json**

Same pattern in the `d0.latest.json` heredoc:

```json
    ,"linux-x86_64": {
      "url": "https://d0.all7.cc/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_amd64.AppImage",
      "signature": "${LINUX_SIG}"
    }
```

- [ ] **Step 4: Add Linux to GitHub Release table**

In the `gh release create` notes, add a Linux row:

```
| **Linux** (x86_64) | \`.AppImage\` | \`.AppImage\` (auto-update) |
```

- [ ] **Step 5: Commit**

```bash
git add scripts/publish-desktop.sh
git commit -m "feat(linux): add linux-x86_64 platform to desktop updater manifests"
```

---

### Task 6: Install script — scripts/install-linux.sh

Create the one-line curl install script for Linux desktop users.

**Files:**
- Create: `scripts/install-linux.sh`

- [ ] **Step 1: Write the install script**

```bash
#!/usr/bin/env bash
set -euo pipefail

# Kaitu Linux Desktop Installer
# Usage: curl -fsSL https://kaitu.io/install-linux.sh | sudo bash
#
# Installs:
#   - /opt/kaitu/Kaitu.AppImage (GUI app)
#   - /opt/kaitu/k2 (daemon binary)
#   - /usr/local/bin/k2 (symlink)
#   - k2 systemd service
#   - Desktop entry (for current user)
#   - /usr/local/bin/kaitu-uninstall (uninstaller)

CDN_BASE="https://d0.all7.cc/kaitu"
INSTALL_DIR="/opt/kaitu"

# --- Helpers ---

info()  { echo "[kaitu] $*"; }
error() { echo "[kaitu] ERROR: $*" >&2; exit 1; }

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) error "Unsupported architecture: $(uname -m)" ;;
  esac
}

check_root() {
  if [ "$(id -u)" -ne 0 ]; then
    error "This script must be run as root (use: curl ... | sudo bash)"
  fi
}

get_real_user() {
  # When running via sudo, get the actual user
  echo "${SUDO_USER:-$(whoami)}"
}

check_webkit2gtk() {
  # Check if webkit2gtk-4.1 is available
  if ldconfig -p 2>/dev/null | grep -q "libwebkit2gtk-4.1"; then
    return 0
  fi

  # Try pkg-config as fallback
  if command -v pkg-config >/dev/null 2>&1 && pkg-config --exists webkit2gtk-4.1 2>/dev/null; then
    return 0
  fi

  echo ""
  echo "webkit2gtk-4.1 is required but not installed."
  echo ""
  echo "Install it for your distribution:"
  echo "  Ubuntu/Debian:  sudo apt install libwebkit2gtk-4.1-0"
  echo "  Fedora:         sudo dnf install webkit2gtk4.1"
  echo "  Arch:           sudo pacman -S webkit2gtk-4.1"
  echo "  openSUSE:       sudo zypper install webkit2gtk-4.1"
  echo ""
  error "Install webkit2gtk-4.1 and re-run this script."
}

get_latest_version() {
  local manifest_url="${CDN_BASE}/desktop/cloudfront.latest.json"
  local version
  version=$(curl -fsSL "$manifest_url" 2>/dev/null | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
  if [ -z "$version" ]; then
    error "Failed to fetch latest version from $manifest_url"
  fi
  echo "$version"
}

# --- Main ---

check_root
ARCH=$(detect_arch)
info "Detected architecture: ${ARCH}"

# Currently only amd64 is supported
if [ "$ARCH" != "amd64" ]; then
  error "Linux desktop currently only supports amd64. For arm64 server use: curl -fsSL https://kaitu.io/install-k2.sh | sudo bash"
fi

check_webkit2gtk

VERSION=$(get_latest_version)
info "Latest version: ${VERSION}"

# Download AppImage
info "Downloading Kaitu AppImage..."
mkdir -p "$INSTALL_DIR"
curl -fSL "${CDN_BASE}/desktop/${VERSION}/Kaitu_${VERSION}_amd64.AppImage" \
  -o "${INSTALL_DIR}/Kaitu.AppImage"
chmod +x "${INSTALL_DIR}/Kaitu.AppImage"

# Download k2 binary
info "Downloading k2 daemon..."
curl -fSL "${CDN_BASE}/k2/${VERSION}/k2-linux-amd64" \
  -o "${INSTALL_DIR}/k2"
chmod +x "${INSTALL_DIR}/k2"

# Symlink k2 to PATH
ln -sf "${INSTALL_DIR}/k2" /usr/local/bin/k2
info "k2 available at /usr/local/bin/k2"

# Install systemd service
info "Installing k2 systemd service..."
"${INSTALL_DIR}/k2" service install

# Create desktop entry for the real user
REAL_USER=$(get_real_user)
REAL_HOME=$(eval echo "~${REAL_USER}")
DESKTOP_DIR="${REAL_HOME}/.local/share/applications"
mkdir -p "$DESKTOP_DIR"

cat > "${DESKTOP_DIR}/kaitu.desktop" << EOF
[Desktop Entry]
Name=Kaitu
Comment=Kaitu VPN
Exec=${INSTALL_DIR}/Kaitu.AppImage
Icon=${INSTALL_DIR}/kaitu.png
Type=Application
Categories=Network;VPN;
StartupWMClass=kaitu
EOF

# Set ownership to real user
chown "${REAL_USER}:" "${DESKTOP_DIR}/kaitu.desktop"

# Extract icon from AppImage if possible (best-effort)
(
  cd /tmp
  "${INSTALL_DIR}/Kaitu.AppImage" --appimage-extract "*.png" 2>/dev/null || true
  ICON=$(find squashfs-root -name "*.png" -path "*/256x256/*" -print -quit 2>/dev/null)
  if [ -z "$ICON" ]; then
    ICON=$(find squashfs-root -maxdepth 1 -name "*.png" -print -quit 2>/dev/null)
  fi
  if [ -n "$ICON" ]; then
    cp "$ICON" "${INSTALL_DIR}/kaitu.png"
  fi
  rm -rf squashfs-root
) 2>/dev/null || true

# Create uninstall script
cat > /usr/local/bin/kaitu-uninstall << 'UNINSTALL'
#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "Run with sudo: sudo kaitu-uninstall"
  exit 1
fi

echo "Uninstalling Kaitu..."

# Stop and remove service
systemctl stop k2 2>/dev/null || true
systemctl disable k2 2>/dev/null || true
/opt/kaitu/k2 service uninstall 2>/dev/null || true

# Remove files
rm -rf /opt/kaitu
rm -f /usr/local/bin/k2
rm -f /usr/local/bin/kaitu-uninstall

# Remove desktop entries for all users
for home_dir in /home/*/; do
  rm -f "${home_dir}.local/share/applications/kaitu.desktop" 2>/dev/null || true
done
rm -f /root/.local/share/applications/kaitu.desktop 2>/dev/null || true

# Purge logs and config if --purge flag
if [ "${1:-}" = "--purge" ]; then
  for home_dir in /home/*/; do
    rm -rf "${home_dir}.local/share/kaitu" 2>/dev/null || true
    rm -rf "${home_dir}.cache/k2" 2>/dev/null || true
  done
  rm -rf /var/log/k2 2>/dev/null || true
  echo "Purged all data and logs."
fi

echo "Kaitu uninstalled."
UNINSTALL

chmod +x /usr/local/bin/kaitu-uninstall

info ""
info "=== Installation complete ==="
info "  GUI:       ${INSTALL_DIR}/Kaitu.AppImage"
info "  CLI:       k2 (in PATH)"
info "  Service:   systemctl status k2"
info "  Uninstall: sudo kaitu-uninstall"
info ""
info "Launch Kaitu from your application menu or run:"
info "  ${INSTALL_DIR}/Kaitu.AppImage"
```

- [ ] **Step 2: Make executable and test syntax**

Run: `chmod +x scripts/install-linux.sh && bash -n scripts/install-linux.sh`

Expected: No syntax errors.

- [ ] **Step 3: Commit**

```bash
git add scripts/install-linux.sh
git commit -m "feat(linux): add one-line curl install script for Linux desktop"
```

---

### Task 7: CLAUDE.md — document Linux build

Add Linux build commands and conventions to the project documentation.

**Files:**
- Modify: `CLAUDE.md` (Quick Commands section, Key Conventions)
- Modify: `desktop/CLAUDE.md` (Commands section)

- [ ] **Step 1: Add Linux commands to root CLAUDE.md Quick Commands**

After `make build-windows` line, add:

```
make build-linux                  # AppImage (CI only — requires Linux host + webkit2gtk)
```

- [ ] **Step 2: Add Linux convention to Key Conventions**

Add to the conventions list:

```
- **Linux AppImage**: webkit2gtk-4.1 is dynamically linked (not bundled). Install script checks for it. Only amd64 initially. Auto-update via `tauri-plugin-updater` (AppImage only format that supports it).
- **Linux admin elevation**: `pkexec` for graphical password dialog. Returns `"pkexec_unavailable"` error if pkexec missing — frontend shows manual `sudo k2 service install` instructions.
```

- [ ] **Step 3: Add to desktop/CLAUDE.md Commands**

Add to the commands block:

```bash
yarn tauri build --bundles appimage  # Linux AppImage (requires Linux host)
```

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md desktop/CLAUDE.md
git commit -m "docs: add Linux desktop build commands and conventions"
```

---

### Task 8: Verification — cargo check + existing tests pass

Final verification that all changes compile and existing tests still pass.

**Files:** None (verification only)

- [ ] **Step 1: Verify Rust compilation (macOS host)**

Run: `cd desktop/src-tauri && cargo check`

Expected: No errors (Linux cfg branches are not compiled on macOS, but macOS/Windows branches should still compile).

- [ ] **Step 2: Run Rust tests**

Run: `cd desktop/src-tauri && cargo test`

Expected: All 43+ existing tests pass.

- [ ] **Step 3: Run webapp tests**

Run: `cd webapp && yarn test`

Expected: All tests pass (no webapp changes, but verify nothing is broken).

- [ ] **Step 4: Verify Makefile dry-run**

Run: `make -n build-linux 2>&1 | head -10`

Expected: Shows build sequence without errors.

- [ ] **Step 5: Final commit — merge-ready state**

If any fixups were needed during verification, commit them:

```bash
git add -A
git commit -m "fix: address verification issues for Linux desktop support"
```
