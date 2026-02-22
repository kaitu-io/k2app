#!/bin/sh
# k2 installer — installs k2 (client) or k2s (server) to /usr/local/bin/
#
# Server:  curl -fsSL https://kaitu.io/install.sh | sudo sh -s k2s
# Client:  curl -fsSL https://kaitu.io/install.sh | sudo sh -s k2
set -e

INSTALL_DIR="/usr/local/bin"
CDN_PRIMARY="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2"
CDN_FALLBACK="https://d0.all7.cc/kaitu/k2"
GITHUB_REPO="kaitu-io/k2"
MANIFEST=""

detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    case "$OS" in
        linux)  OS="linux" ;;
        darwin) OS="darwin" ;;
        *)      echo "Error: unsupported OS: $OS"; exit 1 ;;
    esac

    case "$ARCH" in
        x86_64|amd64)  ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *)             echo "Error: unsupported architecture: $ARCH"; exit 1 ;;
    esac
}

fetch() {
    url="$1"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "$url"
    else
        echo "Error: curl or wget required" >&2
        exit 1
    fi
}

download() {
    url="$1"
    dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$dest" "$url"
    else
        wget -qO "$dest" "$url"
    fi
}

# Detect sha256 tool
sha256_cmd() {
    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | cut -d' ' -f1
    elif command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    else
        echo ""
    fi
}

get_latest_version() {
    name="$1"
    # Pick manifest by binary: k2 -> cloudfront.latest.json, k2s -> k2s-cloudfront.latest.json
    if [ "$name" = "k2s" ]; then
        manifest_cf="k2s-cloudfront.latest.json"
        manifest_d0="k2s-d0.latest.json"
    else
        manifest_cf="cloudfront.latest.json"
        manifest_d0="d0.latest.json"
    fi

    # Try CloudFront manifest first — save full response for checksum extraction
    MANIFEST=$(fetch "${CDN_PRIMARY}/${manifest_cf}" 2>/dev/null || true)
    VERSION=$(echo "$MANIFEST" | grep '"version"' | head -1 | sed 's/.*"version" *: *"//;s/".*//' || true)

    # Fallback: d0 manifest
    if [ -z "$VERSION" ]; then
        MANIFEST=$(fetch "${CDN_FALLBACK}/${manifest_d0}" 2>/dev/null || true)
        VERSION=$(echo "$MANIFEST" | grep '"version"' | head -1 | sed 's/.*"version" *: *"//;s/".*//' || true)
    fi

    # Fallback: GitHub API (no checksum available)
    if [ -z "$VERSION" ]; then
        MANIFEST=""
        VERSION=$(fetch "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" 2>/dev/null | \
            grep '"tag_name"' | head -1 | sed 's/.*"tag_name" *: *"//;s/".*//' || true)
    fi

    if [ -z "$VERSION" ]; then
        echo "Error: could not determine latest version"
        exit 1
    fi
}

verify_checksum() {
    file="$1"
    platform="${OS}-${ARCH}"

    # Extract expected checksum from manifest: "linux-amd64": "sha256:abc123..."
    expected=$(echo "$MANIFEST" | grep "\"${platform}\"" | grep "sha256:" | \
        sed 's/.*"sha256:\([a-f0-9]*\)".*/\1/' || true)

    if [ -z "$expected" ]; then
        echo "  (checksum not available, skipping verification)"
        return 0
    fi

    actual=$(sha256_cmd "$file")
    if [ -z "$actual" ]; then
        echo "  (no sha256 tool found, skipping verification)"
        return 0
    fi

    if [ "$actual" != "$expected" ]; then
        echo "  Error: checksum mismatch!"
        echo "    expected: ${expected}"
        echo "    got:      ${actual}"
        rm -f "$file"
        return 1
    fi

    echo "  Checksum verified (sha256)"
}

download_binary() {
    name="$1"
    file="${name}-${OS}-${ARCH}"
    tmp="$(mktemp)"

    cdn_primary_url="${CDN_PRIMARY}/${VERSION}/${file}"
    cdn_fallback_url="${CDN_FALLBACK}/${VERSION}/${file}"
    gh_url="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/${file}"

    echo "  Downloading ${name} ${VERSION} (${OS}/${ARCH})..."
    if ! download "$cdn_primary_url" "$tmp" 2>/dev/null; then
        if ! download "$cdn_fallback_url" "$tmp" 2>/dev/null; then
            if ! download "$gh_url" "$tmp" 2>/dev/null; then
                echo "  Error: failed to download ${name}"
                rm -f "$tmp"
                return 1
            fi
        fi
    fi

    verify_checksum "$tmp"

    chmod +x "$tmp"
    mv "$tmp" "${INSTALL_DIR}/${name}"
}

main() {
    NAME="$1"

    if [ "$NAME" != "k2" ] && [ "$NAME" != "k2s" ]; then
        echo "k2 installer"
        echo ""
        echo "Usage:"
        echo "  Server:  curl -fsSL https://kaitu.io/install.sh | sudo sh -s k2s"
        echo "  Client:  curl -fsSL https://kaitu.io/install.sh | sudo sh -s k2"
        exit 1
    fi

    if [ "$(id -u)" -ne 0 ]; then
        echo "Error: run with sudo"
        echo "  curl -fsSL https://kaitu.io/install.sh | sudo sh -s ${NAME}"
        exit 1
    fi

    detect_platform
    get_latest_version "$NAME"

    echo "Installing ${NAME} ${VERSION}..."
    mkdir -p "$INSTALL_DIR"
    download_binary "$NAME"

    echo ""
    echo "Installed ${NAME} to ${INSTALL_DIR}/${NAME}"
    echo ""
    if [ "$NAME" = "k2s" ]; then
        echo "Next: start server and get connection URI"
        echo "  sudo k2s setup"
    else
        echo "Next: connect to server"
        echo "  sudo k2 setup <k2v5://URI>"
    fi
    echo ""
}

main "$@"
