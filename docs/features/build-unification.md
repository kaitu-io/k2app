# Feature: Build Unification

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | build-unification                        |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-14                               |
| Updated   | 2026-02-14                               |

## Version History

| Version | Date       | Summary                                                    |
|---------|------------|------------------------------------------------------------|
| v1      | 2026-02-14 | Initial: single build script + PKG installer + test_build  |

## Overview

Replace the fragmented macOS build process (separate Makefile targets + manual
DMG workflow) with a single `scripts/build-macos.sh` that produces a signed,
notarized PKG installer containing a universal (arm64 + x86_64) binary. Also
add `scripts/test_build.sh` for E2E build verification (14 checks).

## Context

- **Old flow**: `make build-macos` called individual Makefile targets, required
  manual DMG creation, CI workflow duplicated build logic
- **Problem**: DMG is unsigned container — macOS Gatekeeper blocks on download.
  PKG can be signed with `productsign` + notarized, yielding zero Gatekeeper friction.
- **Trigger**: Commit `fc2bd2a` introduced build-unification (8 files), but real
  build verification exposed 4 blocking issues fixed in `dd5d727`.

## Architecture

### Build Pipeline

```
scripts/build-macos.sh [--skip-notarization]
  │
  ├── 1. make pre-build           # version.json
  ├── 2. make build-webapp         # Vite → webapp/dist/
  │
  ├── 3. GOARCH=arm64  make build-k2 TARGET=aarch64-apple-darwin
  ├── 4. GOARCH=amd64  make build-k2 TARGET=x86_64-apple-darwin
  ├── 5. lipo -create → k2-universal-apple-darwin
  │
  ├── 6. yarn tauri build --target universal-apple-darwin
  │      (unset APPLE_ID/APPLE_PASSWORD/APPLE_TEAM_ID if --skip-notarization)
  │
  ├── 7. pkgbuild (staging dir + component plist → unsigned .pkg)
  ├── 8. productsign (→ signed .pkg)
  ├── 9. notarytool submit + stapler staple (unless --skip-notarization)
  │
  └── 10. Collect artifacts → release/{version}/
          ├── Kaitu-{version}.pkg
          ├── Kaitu.app.tar.gz     (Tauri updater)
          └── Kaitu.app.tar.gz.sig (Tauri updater signature)
```

### Makefile Targets

| Target | Description |
|--------|-------------|
| `build-macos` | Full build + notarization |
| `build-macos-fast` | Build + skip notarization (local dev) |
| `build-windows` | Windows NSIS build |
| `build-k2 TARGET=<triple>` | Go cross-compile k2 binary |
| `build-webapp` | Vite production build |
| `pre-build` | Generate webapp/public/version.json |

### CI Integration

`release-desktop.yml` triggers on `v*` tag push. macOS job calls
`scripts/build-macos.sh` (full notarization). Windows job calls
`make build-windows` directly. Both upload artifacts to S3 via
`scripts/publish-release.sh`.

### E2E Verification

`scripts/test_build.sh` runs 14 non-destructive checks:
- Workspace structure (package.json, k2 submodule, Makefile)
- Dependency health (yarn, Go, Rust toolchain)
- Configuration validity (tauri.conf.json, Cargo.toml)
- Build artifact paths and binary presence
- `--full` flag: actually run the macOS build pipeline

## Technical Decisions

### TD1: PKG over DMG

| Aspect | DMG | PKG |
|--------|-----|-----|
| Gatekeeper | Unsigned container, user must override | productsign + notarize, zero friction |
| Install UX | User drags .app to /Applications | Double-click, installer handles placement |
| CI automation | Requires create-dmg tool | Native pkgbuild + productsign |
| Updater compat | N/A (Tauri updater uses .tar.gz) | N/A (same .tar.gz + .sig) |

PKG wins on security UX and CI simplicity. Tauri updater is unaffected (uses
.app.tar.gz + .sig regardless of initial install format).

### TD2: Universal Binary via lipo

Go cross-compilation requires explicit `GOARCH`/`GOOS` env vars (Go does not
infer target from Rust-style triple names). Build both arches separately then
merge with `lipo`:

```bash
GOARCH=arm64 GOOS=darwin make build-k2 TARGET=aarch64-apple-darwin
GOARCH=amd64 GOOS=darwin make build-k2 TARGET=x86_64-apple-darwin
lipo -create k2-aarch64-apple-darwin k2-x86_64-apple-darwin -output k2-universal-apple-darwin
```

Tauri's `--target universal-apple-darwin` expects the binary named
`k2-universal-apple-darwin` in `desktop/src-tauri/binaries/`.

### TD3: pkgbuild Staging Directory

pkgbuild `--root` packages everything in the directory. The Tauri build output
directory contains `.app.tar.gz` and `.sig` files alongside `.app`. Without
staging, the PKG would contain updater artifacts.

Solution: copy only `.app` to a temp staging directory, use that as `--root`.

Component plist sets `BundleIsRelocatable=false` so the installer always places
the app in `/Applications`, and `BundleOverwriteAction=upgrade` for clean
upgrades.

### TD4: Notarization Guard

Tauri's bundler auto-detects `APPLE_ID`/`APPLE_PASSWORD`/`APPLE_TEAM_ID`
environment variables and attempts notarization internally. When
`--skip-notarization` is passed, the script must `unset` these variables
before calling `yarn tauri build` to prevent Tauri from notarizing the .app
(the script handles notarization of the .pkg separately).

## Acceptance Criteria

- [x] `scripts/build-macos.sh` uses explicit GOARCH/GOOS for Go cross-compilation
- [x] `scripts/build-macos.sh` creates universal k2 binary with lipo
- [x] `--skip-notarization` unsets Apple env vars to prevent Tauri auto-notarization
- [x] pkgbuild uses staging directory + component plist with BundleIsRelocatable=false
- [x] `scripts/test_build.sh` 14/14 pass (no regression)
- [x] `desktop/src-tauri/Cargo.toml` tauri-build dependency format corrected
- [x] `make build-macos-fast` produces signed PKG in release/{version}/
- [x] CI `release-desktop.yml` uses shared build script for macOS job
