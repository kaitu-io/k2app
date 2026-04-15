#!/usr/bin/env bash
# Block / unblock outbound to Center (k2.52j.me). Always installs at-job fallback.

set -euo pipefail

ACTION="${1:-}"
ANCHOR="kaitu-uat"
# Resolve k2.52j.me IP(s) via public DNS (system DNS may be hijacked by active tunnel)
CENTER_IPS=$(dig @8.8.8.8 +short +time=3 +tries=1 k2.52j.me 2>/dev/null | grep -E '^[0-9.]+$' | tr '\n' ' ')
if [ -z "$CENTER_IPS" ]; then
  CENTER_IPS=$(dig @1.1.1.1 +short +time=3 +tries=1 k2.52j.me 2>/dev/null | grep -E '^[0-9.]+$' | tr '\n' ' ')
fi

unblock() {
  sudo pfctl -a "$ANCHOR" -F all 2>/dev/null || true
  echo "unblocked anchor=$ANCHOR"
}

block() {
  echo "blocking IPs: $CENTER_IPS"
  rules=""
  for ip in $CENTER_IPS; do
    rules+=$'block drop out quick to '"$ip"$'\n'
  done
  echo "$rules" | sudo pfctl -a "$ANCHOR" -f - 2>&1 | grep -vE '^$|Token|No ALTQ' || true
  # Ensure pf is enabled (harmless if already enabled)
  sudo pfctl -E 2>/dev/null || true
  # Fallback: detached nohup sleep+unblock, independent of parent shell
  sudo nohup bash -c "sleep 120; pfctl -a '$ANCHOR' -F all" > /tmp/pf-unblock-fallback.log 2>&1 &
  disown
  echo "blocked + fallback armed (auto-unblock in 120s via detached process)"
}

case "$ACTION" in
  block) block ;;
  unblock) unblock ;;
  *) echo "usage: $0 block|unblock" >&2; exit 1 ;;
esac
