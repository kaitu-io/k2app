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
