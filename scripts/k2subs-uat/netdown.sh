#!/usr/bin/env bash
# Take primary interface down; ALWAYS install detached fallback to bring it back.

set -uo pipefail

IFACE="${1:-en0}"
DURATION="${2:-60}"   # seconds before auto-up; caller can override

echo "Taking $IFACE down for up to ${DURATION}s (auto-up fallback armed)"

# Detached fallback FIRST (before actually taking interface down) so a failure
# doesn't leave us locked out.
sudo nohup bash -c "sleep $DURATION; ifconfig '$IFACE' up; echo 'auto-up fired at' \$(date) >> /tmp/netdown-fallback.log" > /tmp/netdown-fallback.log 2>&1 &
disown

sudo ifconfig "$IFACE" down
echo "$IFACE down at $(date)"
