# Linux Desktop App Design

## Overview

Add Linux desktop support to Kaitu VPN using Tauri v2 AppImage distribution. The architecture mirrors macOS/Windows: Tauri GUI app communicates with a privileged k2 daemon via HTTP on port 1777.

## Architecture

```
Tauri AppImage (normal user)
    ↓ HTTP :1777
k2 daemon (systemd service, root)
    ↓
Engine → TUN/Proxy → Wire → k2 server
```

Identical to macOS (Tauri ↔ launchd daemon) and Windows (Tauri ↔ Windows service).

## Distribution Strategy

| Scenario | Format | Install Method |
|----------|--------|----------------|
| Linux desktop | AppImage + k2 binary (separate downloads) | One-line curl install script |
| Linux server (headless) | Standalone k2 binary | Existing `publish-k2.sh` CDN distribution |

### Why AppImage Only

- Single package works on all Linux distros (Ubuntu, Fedora, Arch, etc.)
- Only Linux format supported by `tauri-plugin-updater` for auto-updates
- deb/rpm can be added later with minimal effort if needed

**System dependency**: AppImage dynamically links `webkit2gtk-4.1`. The install script must verify it is installed and guide the user to install it if missing (distro-specific package names).

### Two-Tier Linux Distribution

The desktop AppImage and headless k2 binary are independent distribution paths:
- Desktop users get the full GUI experience via AppImage
- Server users download only the k2 binary (zero dependencies, pure Go static build)
- Both share the same k2 daemon — identical tunnel behavior

## Install Script

```bash
curl -fsSL https://kaitu.io/install-linux.sh | sudo bash
```

Script behavior:
1. Detect architecture (amd64 initially, arm64 later)
2. Check `webkit2gtk-4.1` is installed; if missing, print distro-specific install command and exit
3. Download AppImage → `/opt/kaitu/Kaitu.AppImage`, `chmod +x`
4. Download k2 binary separately → `/opt/kaitu/k2`, create symlink `/usr/local/bin/k2`
5. Run `k2 service install` to register systemd service
6. Create `~/.local/share/applications/kaitu.desktop` (desktop entry with icon)
7. Create `/usr/local/bin/kaitu-uninstall` uninstall script
8. Start k2 daemon: `systemctl start k2`

The k2 binary is downloaded separately (not extracted from AppImage) for reliability and to keep the headless install path consistent.

### Uninstall Script

`kaitu-uninstall` performs:
1. `systemctl stop k2 && systemctl disable k2`
2. `k2 service uninstall` (removes systemd unit file)
3. Remove `/opt/kaitu/`, `/usr/local/bin/k2`, `/usr/local/bin/kaitu-uninstall`
4. Remove desktop entry
5. Optionally remove logs and config (`--purge` flag)

## Privilege Model

- **Install time**: `sudo` (one-time — installs systemd service + places binaries)
- **Runtime**: Normal user launches AppImage, connects to running k2 daemon via HTTP
- **Service upgrade**: `pkexec k2 service install` triggered by Tauri when version mismatch detected

Matches macOS (PKG postinstall runs as root) and Windows (NSIS installer runs as admin).

## Rust Code Changes

### service.rs — Linux Platform Support

**UDID resolution** (already implemented):
- Source: `/etc/machine-id` → SHA-256 hash to 32 lowercase hex
- No changes needed

**`ensure_service_running()` — new Linux branch**:
- Check: `systemctl is-active k2` (exit code 0 = running)
- Version check: HTTP GET `http://127.0.0.1:1777/api/core` with `{"action":"version"}`
- If not running or version mismatch → trigger `admin_reinstall_service()`
- Diagnostic log: use `k2` (not `k2.exe`) in path checks on Linux

**`admin_reinstall_service()` — new Linux branch**:
- Replace the existing `#[cfg(not(any(target_os = "macos", target_os = "windows")))]` catch-all (`Err("Not supported on this platform")`) with a Linux-specific implementation
- Find k2 binary: check `/usr/local/bin/k2`, then sidecar path
- Elevate: `pkexec k2 service install`
- pkexec shows native graphical password dialog on desktop environments
- If pkexec not found: return error with code `"pkexec_unavailable"` — frontend displays instructions to run `sudo k2 service install` manually

**`detect_old_kaitu_service()` — new Linux branch**:
- Replace the existing catch-all `false` with a `#[cfg(target_os = "linux")]` branch
- Check for legacy `/etc/systemd/system/kaitu-service.service`
- If found: `systemctl stop kaitu-service && systemctl disable kaitu-service`, remove unit file

### tauri.conf.json

No config changes needed. AppImage target selected via CLI flag `--bundles appimage` during CI build (Tauri defaults include appimage on Linux).

### main.rs

- Log directory: already has Linux branch (`~/.local/share/kaitu/logs`) ✅
- Plugin registration: cross-platform, no changes ✅
- Window setup: cross-platform, no changes ✅
- `hiddenTitle: true` — verify behavior on Linux GTK; may need conditional disable if title bar is absent

### status_stream.rs / tray.rs / channel.rs / updater.rs / log_upload.rs

All cross-platform code. No changes needed. ✅

System tray uses `libayatana-appindicator` (linked dynamically, available on most desktop distros).

## Makefile Changes

`build-k2-linux` already exists in Makefile. Add only the desktop build target:

```makefile
# Full Linux desktop build (CI only — requires Linux host)
build-linux: pre-build build-webapp build-k2-linux
	cd desktop && cargo tauri build --bundles appimage
```

Note: Linux builds cannot be cross-compiled from macOS due to webkit2gtk native linking. Build runs exclusively on CI.

## CI Pipeline

### New job in `release-desktop.yml`

Runner: `ubuntu-22.04` (minimum for webkit2gtk-4.1, ensures AppImage backward compatibility)

Steps:
1. Install system dependencies: `libwebkit2gtk-4.1-dev libayatana-appindicator3-dev librsvg2-dev`
2. Install Rust + Go toolchains
3. `make build-linux` (runs pre-build → build-webapp → build-k2-linux → cargo tauri build)
4. Upload to S3: `kaitu/desktop/{VERSION}/Kaitu_{VERSION}_amd64.AppImage` + `.sig`
5. Add `linux-x86_64` entry to existing `cloudfront.latest.json` and `d0.latest.json` manifests (alongside existing macOS/Windows entries)

### Artifact Naming

Follows existing convention (underscore-separated):
- `Kaitu_{VERSION}_amd64.AppImage`
- `Kaitu_{VERSION}_amd64.AppImage.sig`

S3 path: `kaitu/desktop/{VERSION}/`

### Updater Manifest

Add `linux-x86_64` platform to the existing multi-platform manifests (`cloudfront.latest.json`, `d0.latest.json`). No separate `latest-linux.json` needed. Example entry:

```json
{
  "version": "0.4.0-beta.3",
  "platforms": {
    "linux-x86_64": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/0.4.0-beta.3/Kaitu_0.4.0-beta.3_amd64.AppImage",
      "signature": "..."
    }
  }
}
```

## Scope Boundaries

### In Scope
- AppImage packaging and distribution
- One-line install/uninstall scripts (with webkit2gtk dependency check)
- Rust service.rs Linux branches (ensure_service, admin_reinstall, detect_old_service)
- Makefile `build-linux` target
- CI pipeline (GitHub Actions ubuntu-22.04)
- Auto-update via tauri-plugin-updater (AppImage format)
- S3 CDN artifact upload and manifest generation

### Out of Scope
- arm64 Linux desktop (add later by extending CI matrix)
- deb/rpm native packages (add later if demand warrants)
- Wayland-specific adaptation (webkit2gtk handles this)
- NE mode (no Linux equivalent)
- Flatpak/Snap distribution
- Local development build on macOS (CI-only)

## Testing Strategy

- **CI smoke test**: AppImage launches without crash on ubuntu-22.04 runner (headless, `xvfb-run`)
- **Manual QA**: Test on Ubuntu 22.04/24.04 desktop VM — install script, launch, connect, tray, auto-update
- **Existing test suites**: `cargo test` (Rust), `yarn test` (webapp) — already cross-platform
- **k2 daemon**: Already tested on Linux (`go test ./...` with linux build tags)
