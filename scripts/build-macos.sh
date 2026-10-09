#!/usr/bin/env bash
set -euo pipefail

# macOS build script for k2app.
# Builds universal binary, creates .pkg installer, signs, and notarizes.
# Usage: bash scripts/build-macos.sh [--skip-notarization] [--single-arch] [--features=FEATURE]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

# --- Argument parsing ---
SKIP_NOTARIZATION=false
SINGLE_ARCH=false
EXTRA_FEATURES=""
for arg in "$@"; do
  case "$arg" in
    --skip-notarization) SKIP_NOTARIZATION=true ;;
    --single-arch) SINGLE_ARCH=true ;;
    --features=*) EXTRA_FEATURES="${arg#--features=}" ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

# --- Read version ---
VERSION=$(node -p "require('./package.json').version")

# --- Brand ---
BRAND="${K2_BRAND:-kaitu}"
if [ "$BRAND" = "overleap" ]; then
  BRAND_PRODUCT="Overleap"
  BUNDLE_ID="io.overleap.desktop"
  TAURI_CONFIG_ARG="--config src-tauri/tauri.conf.overleap.json"
else
  BRAND="kaitu"
  BRAND_PRODUCT="Kaitu"
  BUNDLE_ID="io.kaitu.desktop"
  TAURI_CONFIG_ARG=""
fi

if [ "$SINGLE_ARCH" = true ]; then
  echo "=== Building $BRAND_PRODUCT $VERSION for macOS (single-arch) ==="
else
  echo "=== Building $BRAND_PRODUCT $VERSION for macOS (universal) ==="
fi

# Determine native architecture for single-arch builds
if [ "$SINGLE_ARCH" = true ]; then
  NATIVE_ARCH=$(uname -m)
  if [ "$NATIVE_ARCH" = "arm64" ]; then
    K2_TARGET="aarch64-apple-darwin"
    K2_GOARCH="arm64"
  else
    K2_TARGET="x86_64-apple-darwin"
    K2_GOARCH="amd64"
  fi
fi

# --- Pre-build + webapp ---
echo ""
echo "--- Pre-build ---"
make pre-build

echo ""
echo "--- Building webapp ---"
make build-webapp BRAND=$BRAND

echo ""
echo "--- Brand purity gate (webapp dist, pre-package) ---"
bash "$ROOT_DIR/webapp/scripts/check-brand-purity.sh" "$BRAND" "$ROOT_DIR/webapp/dist"

echo ""
echo "--- Building k2 (universal) ---"
make build-k2-macos
# When single-arch, Tauri expects k2-<target> (e.g. k2-aarch64-apple-darwin)
# but we always build universal. Copy to arch-specific name for sidecar resolution.
if [ "$SINGLE_ARCH" = true ]; then
  cp "desktop/src-tauri/binaries/k2-universal-apple-darwin" \
     "desktop/src-tauri/binaries/k2-$K2_TARGET"
else
  # Tauri universal build compiles each arch separately, each needs its own sidecar
  cp "desktop/src-tauri/binaries/k2-universal-apple-darwin" \
     "desktop/src-tauri/binaries/k2-aarch64-apple-darwin"
  cp "desktop/src-tauri/binaries/k2-universal-apple-darwin" \
     "desktop/src-tauri/binaries/k2-x86_64-apple-darwin"
fi

# --- Tauri build ---
echo ""
if [ "$SINGLE_ARCH" = true ]; then
  echo "--- Building Tauri app ($K2_TARGET) ---"
else
  echo "--- Building Tauri app (universal-apple-darwin) ---"
fi

cd desktop
# Always skip Tauri's built-in notarization — we re-sign after build with the
# hardened runtime, which changes the CDHash and invalidates Tauri's signature.
# After re-signing, we rebuild the .app.tar.gz so it matches the PKG binary,
# and PKG notarization (below) covers both artifacts via the shared CDHash.
_SAVED_APPLE_ID="${APPLE_ID:-}"
_SAVED_APPLE_PASSWORD="${APPLE_PASSWORD:-}"
_SAVED_APPLE_TEAM_ID="${APPLE_TEAM_ID:-}"
_SAVED_APPLE_CERTIFICATE="${APPLE_CERTIFICATE:-}"
_SAVED_APPLE_CERTIFICATE_PASSWORD="${APPLE_CERTIFICATE_PASSWORD:-}"
unset APPLE_ID APPLE_PASSWORD APPLE_TEAM_ID APPLE_CERTIFICATE APPLE_CERTIFICATE_PASSWORD

if [ "$SINGLE_ARCH" = true ]; then
  TAURI_ARGS="--target $K2_TARGET"
else
  TAURI_ARGS="--target universal-apple-darwin"
fi
if [ -n "$EXTRA_FEATURES" ]; then
  TAURI_ARGS="--features $EXTRA_FEATURES $TAURI_ARGS"
fi
yarn tauri build $TAURI_ARGS $TAURI_CONFIG_ARG
cd "$ROOT_DIR"

# Restore Apple credentials for PKG signing + notarization
export APPLE_ID="$_SAVED_APPLE_ID"
export APPLE_PASSWORD="$_SAVED_APPLE_PASSWORD"
export APPLE_TEAM_ID="$_SAVED_APPLE_TEAM_ID"
export APPLE_CERTIFICATE="$_SAVED_APPLE_CERTIFICATE"
export APPLE_CERTIFICATE_PASSWORD="$_SAVED_APPLE_CERTIFICATE_PASSWORD"

# --- Locate .app bundle ---
if [ "$SINGLE_ARCH" = true ]; then
  BUNDLE_DIR="desktop/src-tauri/target/$K2_TARGET/release/bundle/macos"
else
  BUNDLE_DIR="desktop/src-tauri/target/universal-apple-darwin/release/bundle/macos"
fi
APP_PATH="$BUNDLE_DIR/${BRAND_PRODUCT}.app"

if [ ! -d "$APP_PATH" ]; then
  echo "ERROR: $APP_PATH not found"
  exit 1
fi
echo "Found app bundle: $APP_PATH"

# --- Sign app bundle with hardened runtime ---
echo ""
echo "--- Codesigning app bundle ---"
SIGN_IDENTITY="${APPLE_SIGNING_IDENTITY:-Developer ID Application: ALL NATION CONNECT TECHNOLOGY PTE. LTD. (NJT954Q3RH)}"

# One signing routine for the universal app and the per-arch updater apps
# below, so the two can never drift apart.
sign_app() {
  local app="$1"
  # Sign k2 sidecar with hardened runtime
  codesign --force --sign "$SIGN_IDENTITY" \
    --options runtime \
    "$app/Contents/MacOS/k2"

  # Sign main app
  codesign --force --sign "$SIGN_IDENTITY" \
    --options runtime \
    "$app"

  echo "--- Verifying codesign ($(basename "$app")) ---"
  codesign --verify --deep --strict "$app"
  echo "codesign verification passed"
}

# Tauri updater signature: minisign over the tar.gz, stored as base64 .sig.
# No-op (and no .sig) without TAURI_SIGNING_PRIVATE_KEY.
updater_sign() {
  local tgz="$1" sig_out="$2"
  [ -n "${TAURI_SIGNING_PRIVATE_KEY:-}" ] || { rm -f "$sig_out"; return 0; }
  if ! command -v minisign &>/dev/null; then
    echo "Installing minisign..."
    brew install --quiet minisign 2>/dev/null || {
      echo "ERROR: minisign not available and brew install failed"
      exit 1
    }
  fi
  local key
  key=$(mktemp /tmp/minisign.key.XXXXXX)
  echo "$TAURI_SIGNING_PRIVATE_KEY" | base64 -d > "$key"
  echo "${TAURI_SIGNING_PRIVATE_KEY_PASSWORD:-}" | minisign -S -s "$key" -m "$tgz"
  base64 < "${tgz}.minisig" | tr -d '\n' > "$sig_out"
  rm -f "$key" "${tgz}.minisig"
}

sign_app "$APP_PATH"

# --- Rebuild .app.tar.gz from re-signed .app ---
# Tauri's tar.gz was created BEFORE our codesign --force re-signing, so it contains
# the old CDHash. The PKG (built from re-signed .app) gets notarized, but the old
# tar.gz binary is NOT notarized → Gatekeeper rejects it on macOS 10.15+.
# Fix: re-create tar.gz from the re-signed .app so both share the same CDHash.
echo ""
echo "--- Rebuilding .app.tar.gz from re-signed app ---"
REBUILT_TAR_GZ="$BUNDLE_DIR/${BRAND_PRODUCT}.app.tar.gz"
tar czf "$REBUILT_TAR_GZ" -C "$BUNDLE_DIR" "${BRAND_PRODUCT}.app"
echo "Rebuilt: $REBUILT_TAR_GZ ($(du -h "$REBUILT_TAR_GZ" | cut -f1))"

# Re-sign the tar.gz with Tauri updater key (minisign) if available.
# The old .sig matches the old tar.gz; we need a new .sig for the rebuilt tar.gz.
if [ -n "${TAURI_SIGNING_PRIVATE_KEY:-}" ]; then
  echo "--- Re-signing .app.tar.gz with Tauri updater key ---"
  updater_sign "$REBUILT_TAR_GZ" "$BUNDLE_DIR/${BRAND_PRODUCT}.app.tar.gz.sig"
  echo "Updater signature regenerated"
else
  echo "WARN: TAURI_SIGNING_PRIVATE_KEY not set, skipping updater re-sign"
  rm -f "$BUNDLE_DIR/${BRAND_PRODUCT}.app.tar.gz.sig"
fi

# --- Create .pkg with pkgbuild ---
echo ""
echo "--- Creating .pkg installer ---"
RELEASE_DIR="release/$VERSION"
mkdir -p "$RELEASE_DIR"

PKG_UNSIGNED="$RELEASE_DIR/${BRAND_PRODUCT}_${VERSION}_universal-unsigned.pkg"
PKG_SIGNED="$RELEASE_DIR/${BRAND_PRODUCT}_${VERSION}_universal.pkg"

# Stage only the .app for pkgbuild (exclude updater artifacts)
PKG_STAGE=$(mktemp -d /tmp/k2app-pkg-stage.XXXXXX)
cp -R "$APP_PATH" "$PKG_STAGE/"

# Create component plist with BundleIsRelocatable=false
COMPONENT_PLIST=$(mktemp /tmp/k2app-component.XXXXXX)
cat > "$COMPONENT_PLIST" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<array>
  <dict>
    <key>BundleHasStrictIdentifier</key>
    <true/>
    <key>BundleIsRelocatable</key>
    <false/>
    <key>BundleIsVersionChecked</key>
    <false/>
    <key>BundleOverwriteAction</key>
    <string>upgrade</string>
    <key>RootRelativeBundlePath</key>
    <string>${BRAND_PRODUCT}.app</string>
  </dict>
</array>
</plist>
PLIST

# Render pkg-scripts template for the active brand
PKG_SCRIPTS_RENDERED=$(mktemp -d /tmp/k2app-pkg-scripts.XXXXXX)
for f in preinstall postinstall; do
  sed -e "s/@APP_NAME@/${BRAND_PRODUCT}/g" -e "s/@BUNDLE_ID@/${BUNDLE_ID}/g" \
    "$ROOT_DIR/scripts/pkg-scripts/$f" > "$PKG_SCRIPTS_RENDERED/$f"
  chmod +x "$PKG_SCRIPTS_RENDERED/$f"
done

pkgbuild \
  --root "$PKG_STAGE" \
  --component-plist "$COMPONENT_PLIST" \
  --scripts "$PKG_SCRIPTS_RENDERED" \
  --identifier "$BUNDLE_ID" \
  --version "$VERSION" \
  --install-location "/Applications" \
  "$PKG_UNSIGNED"

rm -rf "$PKG_STAGE" "$COMPONENT_PLIST" "$PKG_SCRIPTS_RENDERED"

echo "Created unsigned pkg: $PKG_UNSIGNED"

# --- Sign .pkg with productsign (if identity available) ---
if [ -n "${APPLE_INSTALLER_IDENTITY:-}" ]; then
  echo ""
  echo "--- Signing .pkg ---"
  productsign --sign "$APPLE_INSTALLER_IDENTITY" "$PKG_UNSIGNED" "$PKG_SIGNED"
  rm -f "$PKG_UNSIGNED"
  echo "Signed pkg: $PKG_SIGNED"
else
  echo "APPLE_INSTALLER_IDENTITY not set, skipping pkg signing"
  mv "$PKG_UNSIGNED" "$PKG_SIGNED"
fi

# --- Notarize .pkg ---
if [ "$SKIP_NOTARIZATION" = true ]; then
  echo ""
  echo "--- Skipping notarization (--skip-notarization) ---"
elif [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_PASSWORD:-}" ] && [ -n "${APPLE_TEAM_ID:-}" ]; then
  echo ""
  echo "--- Notarizing .pkg ---"
  xcrun notarytool submit "$PKG_SIGNED" \
    --apple-id "$APPLE_ID" \
    --password "$APPLE_PASSWORD" \
    --team-id "$APPLE_TEAM_ID" \
    --wait

  echo "--- Stapling notarization ticket ---"
  xcrun stapler staple "$PKG_SIGNED"
  echo "Notarization complete"
else
  echo ""
  echo "Notarization credentials not set (APPLE_ID, APPLE_PASSWORD, APPLE_TEAM_ID), skipping"
fi

# --- Per-arch updater archives (first download ≠ update) ---
# First downloads stay universal: the .pkg above (a browser can't reliably tell
# Apple Silicon from Intel). Auto-updates don't need that — the updater asks for
# its own {os}-{arch} key — so each arch gets a thinned app at ~half the bytes.
# lipo -thin rewrites the Mach-O files, so each thin app is re-signed with the
# same sign_app as the universal one and notarized + stapled on its own (the
# universal tar.gz rides on the pkg's notarization via the shared CDHash; the
# thin apps share nothing with it). publish-desktop.sh points darwin-aarch64 /
# darwin-x86_64 at these and falls back to the universal archive without them.
build_thin_update() {
  local lipo_arch="$1" label="$2"
  local stage
  stage=$(mktemp -d /tmp/k2app-thin-${label}.XXXXXX)
  ditto "$APP_PATH" "$stage/${BRAND_PRODUCT}.app"
  local app="$stage/${BRAND_PRODUCT}.app" f archs
  echo ""
  echo "--- Thin updater app: $label ($lipo_arch) ---"
  while IFS= read -r -d '' f; do
    archs=$(lipo -archs "$f" 2>/dev/null) || continue   # not Mach-O
    [ "$archs" = "$lipo_arch" ] && continue
    lipo -thin "$lipo_arch" "$f" -output "$f.thin"
    mv "$f.thin" "$f"
  done < <(find "$app/Contents" -type f -perm -u+x -print0)
  # Every Mach-O must now be exactly this arch — a leftover fat binary would
  # silently cost the bytes we're here to save, a wrong one won't launch.
  while IFS= read -r -d '' f; do
    archs=$(lipo -archs "$f" 2>/dev/null) || continue
    [ "$archs" = "$lipo_arch" ] || { echo "ERROR: $f is '$archs', expected '$lipo_arch'" >&2; exit 1; }
  done < <(find "$app/Contents" -type f -perm -u+x -print0)

  sign_app "$app"

  if [ "$SKIP_NOTARIZATION" != true ] && [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_PASSWORD:-}" ] && [ -n "${APPLE_TEAM_ID:-}" ]; then
    echo "--- Notarizing thin app ($label) ---"
    local zip="$stage/${BRAND_PRODUCT}-${label}.zip"
    ditto -c -k --keepParent "$app" "$zip"
    xcrun notarytool submit "$zip" \
      --apple-id "$APPLE_ID" \
      --password "$APPLE_PASSWORD" \
      --team-id "$APPLE_TEAM_ID" \
      --wait
    rm -f "$zip"
    xcrun stapler staple "$app"
    xcrun stapler validate "$app"
    spctl --assess --type execute -vv "$app"
  else
    echo "Notarization skipped for thin app ($label)"
  fi

  local tgz="$RELEASE_DIR/${BRAND_PRODUCT}_${VERSION}_${label}.app.tar.gz"
  tar czf "$tgz" -C "$stage" "${BRAND_PRODUCT}.app"
  updater_sign "$tgz" "${tgz}.sig"
  rm -rf "$stage"
  echo "Thin updater: $(basename "$tgz") ($(du -h "$tgz" | cut -f1))"
  bash "$ROOT_DIR/scripts/check-desktop-brand-purity.sh" "$BRAND" "$tgz"
}

if [ "$SINGLE_ARCH" = true ]; then
  echo "Single-arch build: no per-arch updater archives"
else
  build_thin_update arm64 aarch64
  build_thin_update x86_64 x64
fi

# --- Collect updater artifacts (.app.tar.gz + .sig) ---
echo ""
echo "--- Collecting artifacts ---"

# Collect the brand-exact tar.gz — NEVER glob: both brands share BUNDLE_DIR, so a
# stale other-brand tar.gz from a previous build would win by alphabetical order
# (Kaitu < Overleap) and ship the wrong app to updater clients.
APP_TAR_GZ="$BUNDLE_DIR/${BRAND_PRODUCT}.app.tar.gz"
if [ ! -f "$APP_TAR_GZ" ]; then
  echo "ERROR: $APP_TAR_GZ not found — updater artifact missing"
  exit 1
fi
cp "$APP_TAR_GZ" "$RELEASE_DIR/${BRAND_PRODUCT}_${VERSION}_universal.app.tar.gz"
echo "Renamed: $(basename "$APP_TAR_GZ") → ${BRAND_PRODUCT}_${VERSION}_universal.app.tar.gz"

# .sig may legitimately be absent (deleted above when TAURI_SIGNING_PRIVATE_KEY unset)
APP_SIG="$BUNDLE_DIR/${BRAND_PRODUCT}.app.tar.gz.sig"
if [ -f "$APP_SIG" ]; then
  cp "$APP_SIG" "$RELEASE_DIR/${BRAND_PRODUCT}_${VERSION}_universal.app.tar.gz.sig"
  echo "Renamed: $(basename "$APP_SIG") → ${BRAND_PRODUCT}_${VERSION}_universal.app.tar.gz.sig"
fi

echo ""
echo "--- Brand purity gate (updater .app.tar.gz) ---"
bash "$ROOT_DIR/scripts/check-desktop-brand-purity.sh" "$BRAND" "$RELEASE_DIR/${BRAND_PRODUCT}_${VERSION}_universal.app.tar.gz"

# --- Summary ---
echo ""
echo "=== Build complete ==="
echo "Release artifacts in $RELEASE_DIR/:"
ls -la "$RELEASE_DIR/"
