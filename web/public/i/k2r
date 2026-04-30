#!/bin/sh
# install-k2r.sh — Install or upgrade k2r (gateway)
#
# Usage:
#   wget -qO- https://kaitu.io/i/k2r | sudo sh
#   wget -qO- https://kaitu.io/i/k2r | sudo sh -s <k2v5://URL>   # install + setup with server URL
#
# Installs k2r to /usr/bin, registers service (procd on OpenWrt, systemd on Linux),
# and optionally installs LuCI integration if /usr/lib/lua/luci is present.
set -e

INSTALL_DIR="/usr/bin"
BINARY="k2r"
GATEWAY_PORT="1779"

CDN_PRIMARY="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2r"
CDN_FALLBACK="https://dl.kaitu.io/kaitu/k2r"

# --- helpers ---

log()  { printf '  %s\n' "$*"; }
die()  { printf 'Error: %s\n' "$*" >&2; exit 1; }

require_root() {
    [ "$(id -u)" -eq 0 ] || die "run with sudo: wget -qO- https://kaitu.io/i/k2r | sudo sh"
}

detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    case "$OS" in
        linux) ;;
        *) die "k2r is Linux only (got: $OS)" ;;
    esac

    case "$ARCH" in
        x86_64|amd64)   ARCH="amd64" ;;
        aarch64|arm64)  ARCH="arm64" ;;
        armv7l)         ARCH="armv7" ;;
        *)              die "unsupported architecture: $ARCH" ;;
    esac
}

fetch() {
    url="$1"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --connect-timeout 10 "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- --timeout=10 "$url"
    else
        die "curl or wget required"
    fi
}

download() {
    url="$1"; dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --connect-timeout 10 -o "$dest" "$url"
    else
        wget -qO "$dest" --timeout=10 "$url"
    fi
}

# fetch_cdn <path> — try primary CDN, fallback on failure
fetch_cdn() {
    path="$1"
    fetch "${CDN_PRIMARY}${path}" 2>/dev/null || fetch "${CDN_FALLBACK}${path}"
}

# download_cdn <path> <dest> — try primary CDN, fallback on failure
download_cdn() {
    path="$1"; dest="$2"
    download "${CDN_PRIMARY}${path}" "$dest" 2>/dev/null || download "${CDN_FALLBACK}${path}" "$dest"
}

# sha256_check <file> <expected_hash>
sha256_check() {
    file="$1"; expected="$2"
    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "$file" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        actual="$(shasum -a 256 "$file" | awk '{print $1}')"
    else
        log "WARNING: no sha256sum or shasum found, skipping checksum verification"
        return 0
    fi
    [ "$actual" = "$expected" ] || die "checksum mismatch: expected $expected, got $actual"
}

installed_version() {
    if command -v "$BINARY" >/dev/null 2>&1; then
        # "k2r VERSION (COMMIT)" — extract second word
        "$BINARY" -v 2>/dev/null | awk '{print $2}' || echo ""
    else
        echo ""
    fi
}

is_service_running() {
    if command -v procd >/dev/null 2>&1 || [ -f /etc/openwrt_release ]; then
        # OpenWrt procd
        /etc/init.d/k2r status >/dev/null 2>&1
    elif command -v systemctl >/dev/null 2>&1; then
        systemctl is-active k2r >/dev/null 2>&1
    else
        return 1
    fi
}

restart_service() {
    log "Restarting service..."
    if command -v procd >/dev/null 2>&1 || [ -f /etc/openwrt_release ]; then
        /etc/init.d/k2r restart 2>/dev/null || true
    elif command -v systemctl >/dev/null 2>&1; then
        systemctl restart k2r 2>/dev/null || true
    fi
}

# install_luci — install LuCI controller + view if LuCI is present
install_luci() {
    LUCI_DIR="/usr/lib/lua/luci"
    [ -d "$LUCI_DIR" ] || return 0

    log "Installing LuCI integration..."

    # Controller
    CTRL_DIR="${LUCI_DIR}/controller"
    mkdir -p "$CTRL_DIR"
    cat > "${CTRL_DIR}/k2r.lua" <<'EOF'
module("luci.controller.k2r", package.seeall)

function index()
    entry({"admin", "services", "k2r"}, template("k2r"), _("K2 VPN"), 90)
end
EOF

    # View (iframe to gateway HTTP port)
    VIEW_DIR="${LUCI_DIR}/view"
    mkdir -p "$VIEW_DIR"
    cat > "${VIEW_DIR}/k2r.htm" <<'EOF'
<%+header%>
<iframe
  src="http://127.0.0.1:1779"
  style="width:100%;height:calc(100vh - 120px);border:none;"
  allowfullscreen
></iframe>
<%+footer%>
EOF

    # Clear LuCI cache so new menu entry appears
    rm -rf /tmp/luci-*
    log "LuCI integration installed (Admin > Services > K2 VPN)"
}

get_lan_ip() {
    if [ -f /etc/openwrt_release ] || command -v uci >/dev/null 2>&1; then
        uci get network.lan.ipaddr 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}'
    else
        hostname -I 2>/dev/null | awk '{print $1}'
    fi
}

# --- main ---

main() {
    SERVER_URL="$1"

    require_root
    detect_platform

    log "Fetching latest version..."
    VERSION="$(fetch_cdn "/LATEST")" || die "failed to fetch latest version"
    VERSION="$(echo "$VERSION" | tr -d '[:space:]')"
    [ -n "$VERSION" ] || die "empty version from LATEST"

    CURRENT="$(installed_version)"
    FILENAME="${BINARY}-${OS}-${ARCH}"

    # Already installed?
    if [ -n "$CURRENT" ]; then
        if [ "$CURRENT" = "$VERSION" ]; then
            log "$BINARY is up to date ($VERSION)"
            exit 0
        fi
        log "Upgrading $BINARY: $CURRENT -> $VERSION"
    else
        log "Installing $BINARY $VERSION ($OS/$ARCH)"
    fi

    # Download + verify
    log "Downloading ${FILENAME}..."
    CHECKSUMS="$(fetch_cdn "/${VERSION}/checksums.txt")" || die "failed to fetch checksums"
    EXPECTED="$(echo "$CHECKSUMS" | grep "  ${FILENAME}\$" | awk '{print $1}')"
    [ -n "$EXPECTED" ] || die "no checksum found for ${FILENAME}"

    TMP="$(mktemp)"
    trap 'rm -f "$TMP"' EXIT
    download_cdn "/${VERSION}/${FILENAME}" "$TMP" || die "download failed"
    sha256_check "$TMP" "$EXPECTED"
    log "Checksum verified"

    # Install binary
    chmod +x "$TMP"
    mv "$TMP" "${INSTALL_DIR}/${BINARY}"
    trap - EXIT

    # Upgrade: restart service if running, then done
    if [ -n "$CURRENT" ]; then
        if is_service_running; then
            restart_service
        fi
        log "Upgraded to $VERSION"
        exit 0
    fi

    # Fresh install
    log "Installed $BINARY to ${INSTALL_DIR}/${BINARY}"
    echo ""

    # Register service (k2r auto-detects OpenWrt procd vs systemd)
    log "Installing service..."
    "$BINARY" service install

    # LuCI integration (OpenWrt only — no-op on plain Linux)
    install_luci

    # Run setup if a server URL was provided
    if [ -n "$SERVER_URL" ]; then
        echo ""
        log "Running setup with provided server URL..."
        "$BINARY" setup "$SERVER_URL"
    fi

    # Print access URL
    LAN_IP="$(get_lan_ip)"
    echo ""
    if [ -n "$LAN_IP" ]; then
        log "Gateway web UI: http://${LAN_IP}:${GATEWAY_PORT}"
    else
        log "Gateway web UI: http://<LAN-IP>:${GATEWAY_PORT}"
    fi
    echo ""
}

main "$@"
