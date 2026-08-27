#!/bin/bash
set -euo pipefail

# SimplySign keep-alive wrapper (macOS GUI session only).
#
# WHY THIS EXISTS
# The SimplySign cloud session drops every ~2-3 hours. The self-hosted release
# runner's LaunchDaemon runs with SessionCreate=true and NO Aqua GUI, so it can
# never drive the SimplySign Desktop UI to reconnect — `build-windows` then dies
# at the Authenticode signing preflight (CKR_ATTRIBUTE_TYPE_INVALID) even though
# `--list-slots` falsely reports the token online. This wrapper is meant to run
# from a *GUI-session* LaunchAgent (see io.kaitu.simplisign-keepalive.plist) so
# the reconnect actually has a UI to drive.
#
# SECRET HANDLING — the TOTP URI is NEVER stored in this repo or the plist.
# It is read at runtime from the macOS login Keychain. Provision it once:
#   security add-generic-password -a "$USER" -s "kaitu-simplisign-totp" \
#     -w 'otpauth://totp/...?secret=...'
# Rotate/replace with `security delete-generic-password -s kaitu-simplisign-totp`
# then add again.

KEYCHAIN_SERVICE="kaitu-simplisign-totp"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOGIN_SCRIPT="$SCRIPT_DIR/simplisign-login.sh"
LOG="$HOME/Library/Logs/simplisign-keepalive.log"

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S %z')] $*" >>"$LOG"; }

mkdir -p "$(dirname "$LOG")"

if [ ! -x "$LOGIN_SCRIPT" ]; then
  log "FATAL: $LOGIN_SCRIPT not found or not executable"
  exit 1
fi

# Pull the TOTP URI from the Keychain (never from the repo/plist/env-in-git).
if ! TOTP_URI="$(security find-generic-password -s "$KEYCHAIN_SERVICE" -w 2>/dev/null)" || [ -z "$TOTP_URI" ]; then
  log "FATAL: no TOTP URI in Keychain (service '$KEYCHAIN_SERVICE')."
  log "  Provision it once with:"
  log "    security add-generic-password -a \"\$USER\" -s \"$KEYCHAIN_SERVICE\" -w 'otpauth://totp/...?secret=...'"
  exit 1
fi

export SIMPLISIGN_TOTP_URI="$TOTP_URI"

log "keep-alive tick: invoking simplisign-login.sh"
if "$LOGIN_SCRIPT" >>"$LOG" 2>&1; then
  log "keep-alive tick: OK"
else
  rc=$?
  log "keep-alive tick: simplisign-login.sh exited $rc"
  exit "$rc"
fi
