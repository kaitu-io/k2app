#!/usr/bin/env bash
# Blackhole (block) a specific IP by routing it to 127.0.0.1.
# Always installs detached fallback to restore the route.

set -uo pipefail

ACTION="${1:-}"
IP="${2:-}"
DURATION="${3:-60}"

if [ -z "$IP" ]; then
  echo "usage: $0 block|unblock <IP> [duration-seconds]" >&2; exit 1
fi

case "$ACTION" in
  block)
    # Detached fallback first
    sudo nohup bash -c "sleep $DURATION; route delete -host '$IP' >> /tmp/blackhole-fallback.log 2>&1; echo 'auto-restore at' \$(date) >> /tmp/blackhole-fallback.log" > /tmp/blackhole-fallback.log 2>&1 &
    disown
    sudo route -n add -host "$IP" 127.0.0.1 2>&1 | tail -1
    echo "blackholed $IP → 127.0.0.1 (auto-restore in ${DURATION}s)"
    ;;
  unblock)
    sudo route -n delete -host "$IP" 2>&1 | tail -1
    echo "restored route to $IP"
    ;;
  *) echo "usage: $0 block|unblock <IP> [duration]" >&2; exit 1 ;;
esac
