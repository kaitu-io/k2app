#!/usr/bin/env bash
#
# Kaitu Linux uninstall script.
#
# Stops kaitu.service, removes /usr/local/bin/k2 and the systemd unit.
# By default preserves /etc/kaitu (encrypted storage with login state);
# pass --purge to remove that too.

set -euo pipefail

BIN_DST="/usr/local/bin/k2"
UNIT_DST="/etc/systemd/system/kaitu.service"
STATE_DIR="/etc/kaitu"

PURGE=0
for arg in "$@"; do
    case "$arg" in
        --purge)
            PURGE=1
            ;;
        -h|--help|help)
            cat <<EOF
Usage: sudo ./uninstall.sh [--purge]

Stops and removes the Kaitu systemd service and binary.

  --purge   Also delete ${STATE_DIR} (login state, encrypted storage).
            Omit to keep it for reinstall.
EOF
            exit 0
            ;;
    esac
done

if [ "$(id -u)" != "0" ]; then
    echo "ERROR: uninstall.sh must be run as root (sudo ./uninstall.sh)." >&2
    exit 1
fi

if command -v systemctl >/dev/null 2>&1; then
    systemctl stop kaitu.service 2>/dev/null || true
    systemctl disable kaitu.service 2>/dev/null || true
fi

rm -f "$UNIT_DST"
rm -f "$BIN_DST"

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload 2>/dev/null || true
fi

if [ "$PURGE" = "1" ]; then
    if [ -d "$STATE_DIR" ]; then
        echo "Removing ${STATE_DIR}"
        rm -rf "$STATE_DIR"
    fi
    echo "=== Kaitu uninstalled and state purged ==="
else
    echo "=== Kaitu uninstalled ==="
    if [ -d "$STATE_DIR" ]; then
        echo "Kept ${STATE_DIR} (pass --purge to delete)."
    fi
fi
