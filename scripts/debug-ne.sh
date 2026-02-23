#!/usr/bin/env bash
# debug-ne.sh — One-shot macOS Network Extension debug cycle
#
# Usage:
#   bash scripts/debug-ne.sh              # Kill + log stream (foreground)
#   bash scripts/debug-ne.sh --reset      # Also reset systemextensionsctl (requires SIP disabled)
#   bash scripts/debug-ne.sh --log-file   # Save logs to timestamped file
#   bash scripts/debug-ne.sh --status     # Just show current NE/sysext state

set -euo pipefail

RESET=false
LOG_FILE=false
STATUS_ONLY=false

for arg in "$@"; do
  case "$arg" in
    --reset) RESET=true ;;
    --log-file) LOG_FILE=true ;;
    --status) STATUS_ONLY=true ;;
    *) echo "Unknown: $arg"; exit 1 ;;
  esac
done

SYSEXT_ID="io.kaitu.desktop.tunnel"
APP_BUNDLE_ID="io.kaitu.desktop"

# --- Status ---
echo "=== macOS NE Debug ==="
echo ""

echo "--- systemextensionsctl list ---"
systemextensionsctl list 2>&1 || true
echo ""

echo "--- NE VPN configurations ---"
scutil --nc list 2>&1 | grep -i kaitu || echo "(none found)"
echo ""

echo "--- Running processes ---"
pgrep -fl "Kaitu|KaituTunnel|k2" 2>/dev/null || echo "(none running)"
echo ""

if [ "$STATUS_ONLY" = true ]; then
  exit 0
fi

# --- Kill old processes ---
echo "--- Killing Kaitu processes ---"
pkill -f "Kaitu.app" 2>/dev/null && echo "Killed Kaitu.app" || echo "Kaitu.app not running"
pkill -f "KaituTunnel" 2>/dev/null && echo "Killed KaituTunnel" || echo "KaituTunnel not running"
sleep 1

# --- Reset sysext (optional, requires SIP disabled) ---
if [ "$RESET" = true ]; then
  echo ""
  echo "--- Resetting systemextensionsctl ---"
  if csrutil status 2>&1 | grep -q "disabled"; then
    systemextensionsctl reset 2>&1 || echo "WARNING: reset failed"
    echo "System extensions reset. You may need to reboot."
  else
    echo "WARNING: SIP is enabled. Cannot reset systemextensionsctl."
    echo "To reset, boot into Recovery Mode and run: csrutil disable"
  fi
fi

# --- Start log stream ---
echo ""
echo "--- Starting NE log stream ---"
echo "Press Ctrl+C to stop"
echo ""

PREDICATE='subsystem == "com.apple.networkextension"
  OR process == "KaituTunnel"
  OR process == "nesessionmanager"
  OR process == "sysextd"
  OR process == "nehelper"
  OR process == "Kaitu"
  OR subsystem == "io.kaitu.desktop"
  OR subsystem == "io.kaitu.desktop.tunnel"'

if [ "$LOG_FILE" = true ]; then
  LOGDIR="/tmp/kaitu-ne-debug"
  mkdir -p "$LOGDIR"
  LOGPATH="$LOGDIR/ne-$(date +%Y%m%d-%H%M%S).log"
  echo "Logging to: $LOGPATH"
  log stream --level debug --predicate "$PREDICATE" 2>&1 | tee "$LOGPATH"
else
  log stream --level debug --predicate "$PREDICATE"
fi
