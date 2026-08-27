#!/bin/bash
set -euo pipefail

# Install the SimplySign keep-alive LaunchAgent into the current user's GUI
# domain (gui/$uid), so the SimplySign cloud session is auto-reconnected every
# ~90 min and `build-windows` stops failing at the Authenticode preflight when
# the session has silently dropped.
#
# ── OPERATOR PREREQUISITES (do these ONCE, in the runner's GUI login session) ──
#   1. Store the TOTP URI in the login Keychain (NEVER commit it):
#        security add-generic-password -a "$USER" -s "kaitu-simplisign-totp" \
#          -w 'otpauth://totp/...?secret=...'
#   2. Grant Accessibility permission to whatever runs the reconnect osascript
#      (System Settings → Privacy & Security → Accessibility). Without it the
#      menu-bar automation fails with AppleScript error -1719 and the agent
#      falls back to the headless slot gate (which cannot reconnect).
#   3. Make sure SimplySign Desktop is installed and set to launch at login.
#
# Then run this script from that same GUI session:  bash install-simplisign-keepalive.sh
# It is idempotent — re-running replaces the agent and restarts it.

LABEL="io.kaitu.simplisign-keepalive"
SRC_DIR="$(cd "$(dirname "$0")" && pwd)"
INSTALL_DIR="$HOME/Library/Application Support/kaitu-simplisign"
AGENTS_DIR="$HOME/Library/LaunchAgents"
PLIST_DST="$AGENTS_DIR/$LABEL.plist"
UID_NUM="$(id -u)"

for f in simplisign-keepalive.sh simplisign-login.sh io.kaitu.simplisign-keepalive.plist; do
  [ -f "$SRC_DIR/$f" ] || { echo "ERROR: missing $SRC_DIR/$f" >&2; exit 1; }
done

# Warn (do not block) if the Keychain secret is not present yet.
if ! security find-generic-password -s "kaitu-simplisign-totp" -w >/dev/null 2>&1; then
  echo "WARNING: Keychain item 'kaitu-simplisign-totp' not found." >&2
  echo "  The agent will fail every tick until you run:" >&2
  echo "    security add-generic-password -a \"\$USER\" -s \"kaitu-simplisign-totp\" -w 'otpauth://totp/...'" >&2
fi

# Copy the runtime scripts to a STABLE dir (the runner checkout is wiped each run).
mkdir -p "$INSTALL_DIR" "$AGENTS_DIR" "$HOME/Library/Logs"
cp "$SRC_DIR/simplisign-keepalive.sh" "$INSTALL_DIR/"
cp "$SRC_DIR/simplisign-login.sh" "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/simplisign-keepalive.sh" "$INSTALL_DIR/simplisign-login.sh"

# Render the plist template (__HOME__ -> $HOME) into LaunchAgents.
sed "s|__HOME__|$HOME|g" "$SRC_DIR/io.kaitu.simplisign-keepalive.plist" > "$PLIST_DST"

# Reload into the GUI domain (idempotent).
launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$UID_NUM" "$PLIST_DST"
launchctl enable "gui/$UID_NUM/$LABEL" 2>/dev/null || true

echo "Installed $LABEL into gui/$UID_NUM."
echo "  scripts:  $INSTALL_DIR"
echo "  plist:    $PLIST_DST"
echo "  logs:     $HOME/Library/Logs/simplisign-keepalive*.log"
echo "Kick a reconnect now:  launchctl kickstart -k gui/$UID_NUM/$LABEL"
