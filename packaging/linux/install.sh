#!/usr/bin/env bash
#
# Kaitu Linux install script.
#
# Installs the k2 binary to /usr/local/bin, registers kaitu.service with
# systemd, and starts the daemon listening on http://127.0.0.1:1777.
# Handles both fresh install and upgrade (stop -> replace -> start).
#
# Usage:
#   sudo ./install.sh        # install or upgrade
#   sudo ./install.sh --help # show this message

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_SRC="${SCRIPT_DIR}/k2"
UNIT_SRC="${SCRIPT_DIR}/kaitu.service"

BIN_DST="/usr/local/bin/k2"
UNIT_DST="/etc/systemd/system/kaitu.service"
STATE_DIR="/etc/kaitu"

URL="http://127.0.0.1:1777"

usage() {
    cat <<EOF
Kaitu Linux installer

Usage: sudo ./install.sh [--help]

Installs /usr/local/bin/k2 and enables the kaitu.service systemd unit.
The daemon serves the Kaitu webapp on ${URL}; open it in your browser
after install.

Files written:
  ${BIN_DST}
  ${UNIT_DST}
  ${STATE_DIR}/ (created with mode 0700 by the daemon on first run)

Upgrade path: stops the existing kaitu.service, replaces the binary,
and restarts. Storage at ${STATE_DIR}/storage.json is preserved.

Uninstall: sudo ./uninstall.sh
EOF
    exit 0
}

case "${1:-}" in
    -h|--help|help) usage ;;
esac

if [ "$(id -u)" != "0" ]; then
    echo "ERROR: install.sh must be run as root (sudo ./install.sh)." >&2
    exit 1
fi

if [ ! -f "$BIN_SRC" ]; then
    echo "ERROR: k2 binary not found at ${BIN_SRC}" >&2
    echo "       Run install.sh from the unpacked tarball directory." >&2
    exit 1
fi

if [ ! -f "$UNIT_SRC" ]; then
    echo "ERROR: kaitu.service not found at ${UNIT_SRC}" >&2
    exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
    echo "ERROR: systemctl not found — this installer requires systemd." >&2
    echo "       OpenRC / init-based distros are not supported yet." >&2
    exit 1
fi

# Detect existing install for the upgrade path.
UPGRADING=0
if [ -f "$UNIT_DST" ] || [ -f "$BIN_DST" ]; then
    UPGRADING=1
    echo "--- Existing Kaitu install detected; upgrading ---"
    systemctl stop kaitu.service 2>/dev/null || true
fi

echo "--- Installing ${BIN_DST} ---"
install -m 0755 "$BIN_SRC" "$BIN_DST"

echo "--- Installing ${UNIT_DST} ---"
install -m 0644 "$UNIT_SRC" "$UNIT_DST"

echo "--- Reloading systemd ---"
systemctl daemon-reload

echo "--- Enabling kaitu.service ---"
systemctl enable kaitu.service >/dev/null

echo "--- Starting kaitu.service ---"
systemctl start kaitu.service

# Poll the local daemon for up to 10s before declaring success.
echo "--- Waiting for daemon to come up on ${URL} ---"
for i in 1 2 3 4 5 6 7 8 9 10; do
    if curl -sS --max-time 1 "${URL}/ping" >/dev/null 2>&1; then
        echo ""
        if [ "$UPGRADING" = "1" ]; then
            echo "=== Kaitu upgraded and running ==="
        else
            echo "=== Kaitu installed and running ==="
        fi
        echo ""
        echo "Open ${URL} in your browser to sign in."
        if command -v xdg-open >/dev/null 2>&1; then
            # best-effort; never fail the installer if the user has no display
            xdg-open "${URL}" >/dev/null 2>&1 || true
        fi
        exit 0
    fi
    sleep 1
done

echo ""
echo "WARNING: daemon did not respond on ${URL}/ping within 10s."
echo "         Check journalctl -u kaitu.service for errors."
echo "         The binary is installed and systemd will keep retrying."
exit 0
