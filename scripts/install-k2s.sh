#!/bin/sh
# install-k2s.sh — Install or upgrade k2s (server)
#
# Usage:
#   curl -fsSL https://kaitu.io/i/k2s | sudo sh
#
# Pipe mode (curl|sh): runs k2s setup --port 443 --username prince --no-firewall
# Terminal mode:       runs k2s setup (interactive wizard)
set -e

INSTALL_DIR="/usr/local/bin"
BINARY="k2s"

CDN_PRIMARY="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2"
CDN_FALLBACK="https://d0.all7.cc/kaitu/k2"

# --- helpers ---

log()  { printf '  %s\n' "$*"; }
die()  { printf 'Error: %s\n' "$*" >&2; exit 1; }

require_root() {
    [ "$(id -u)" -eq 0 ] || die "run with sudo: curl -fsSL https://kaitu.io/i/k2s | sudo sh"
}

detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    case "$OS" in
        linux)  ;;
        darwin) ;;
        *)      die "unsupported OS: $OS" ;;
    esac

    case "$ARCH" in
        x86_64|amd64)  ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *)             die "unsupported architecture: $ARCH" ;;
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
        "$BINARY" version 2>/dev/null | awk '{print $2}' || echo ""
    else
        echo ""
    fi
}

is_service_running() {
    case "$OS" in
        linux)
            systemctl is-active "$BINARY" >/dev/null 2>&1
            ;;
        darwin)
            launchctl list 2>/dev/null | grep -q kaitu
            ;;
        *)
            return 1
            ;;
    esac
}

restart_service() {
    case "$OS" in
        linux)
            log "Restarting service..."
            systemctl restart "$BINARY" 2>/dev/null || true
            ;;
        darwin)
            log "Restarting service..."
            launchctl stop kaitu.k2s 2>/dev/null || true
            launchctl start kaitu.k2s 2>/dev/null || true
            ;;
    esac
}

# --- main ---

main() {
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

    # Install
    chmod +x "$TMP"
    mv "$TMP" "${INSTALL_DIR}/${BINARY}"
    trap - EXIT

    # Upgrade: restart service if running
    if [ -n "$CURRENT" ]; then
        if is_service_running; then
            restart_service
        fi
        log "Upgraded to $VERSION"
        exit 0
    fi

    # Fresh install: run setup
    log "Installed $BINARY to ${INSTALL_DIR}/${BINARY}"
    echo ""

    if [ -t 0 ]; then
        # Interactive terminal
        "$BINARY" setup
    else
        # Pipe mode (curl|sh) — non-interactive
        "$BINARY" setup --port 443 --username prince --no-firewall
    fi
}

main "$@"
