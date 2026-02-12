#!/bin/sh
# k2 installer — installs k2 (client) or k2s (server) to /usr/local/bin/
#
# Server:  curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2s
# Client:  curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2
set -e

INSTALL_DIR="/usr/local/bin"
GITHUB_REPO="kaitu-io/k2"
CDN_BASE="https://dl.k2.52j.me"

detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    case "$OS" in
        linux)  OS="linux" ;;
        darwin) OS="darwin" ;;
        *)      echo "Unsupported OS: $OS"; exit 1 ;;
    esac

    case "$ARCH" in
        x86_64|amd64)  ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *)             echo "Unsupported architecture: $ARCH"; exit 1 ;;
    esac
}

get_latest_version() {
    if command -v curl >/dev/null 2>&1; then
        VERSION="$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')"
    elif command -v wget >/dev/null 2>&1; then
        VERSION="$(wget -qO- "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')"
    else
        echo "Error: curl or wget required"
        exit 1
    fi

    if [ -z "$VERSION" ]; then
        echo "Error: could not determine latest version"
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

download_binary() {
    name="$1"
    file="${name}-${OS}-${ARCH}"
    tmp="$(mktemp)"

    cdn_url="${CDN_BASE}/${VERSION}/${file}"
    gh_url="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/${file}"

    echo "  Downloading ${name}..."
    if ! download "$cdn_url" "$tmp" 2>/dev/null; then
        if ! download "$gh_url" "$tmp" 2>/dev/null; then
            echo "  Error: failed to download ${name}"
            rm -f "$tmp"
            return 1
        fi
    fi

    chmod +x "$tmp"
    mv "$tmp" "${INSTALL_DIR}/${name}"
}

main() {
    NAME="$1"

    if [ "$NAME" != "k2" ] && [ "$NAME" != "k2s" ]; then
        echo "Usage:"
        echo "  Server:  curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2s"
        echo "  Client:  curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2"
        exit 1
    fi

    if [ "$(id -u)" -ne 0 ]; then
        echo "Error: run with sudo"
        echo "  curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s ${NAME}"
        exit 1
    fi

    detect_platform
    get_latest_version

    echo "Installing ${NAME} ${VERSION} (${OS}/${ARCH})"
    mkdir -p "$INSTALL_DIR"
    download_binary "$NAME"

    echo ""
    echo "Installed ${NAME} to ${INSTALL_DIR}/${NAME}"
    echo ""
    if [ "$NAME" = "k2s" ]; then
        echo "Next step:"
        echo "  sudo k2s run"
    else
        echo "Next step:"
        echo "  sudo k2 up <url from server>"
    fi
    echo ""
}

main "$@"
