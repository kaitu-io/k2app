#!/bin/bash
# iOS device log collection for Kaitu VPN debugging.
#
# Usage:
#   ./scripts/ios-logs.sh              # Stream real-time logs (iOS <= 17)
#   ./scripts/ios-logs.sh --archive    # Archive + query logs (iOS 18+)
#   ./scripts/ios-logs.sh --archive --since 10m
#
# Filters: io.kaitu subsystem (NE + K2Plugin), PacketTunnelExtension process, Kaitu app process.

set -euo pipefail

PREDICATE='subsystem == "io.kaitu" OR process == "PacketTunnelExtension" OR process == "Kaitu"'

case "${1:-stream}" in
  --archive)
    SINCE="${3:-1h}"
    ARCHIVE="/tmp/kaitu-ios-$(date +%s).tar.gz"
    echo "Archiving device logs to $ARCHIVE ..."
    idevicesyslog archive "$ARCHIVE"
    DIR="${ARCHIVE%.tar.gz}"
    mkdir -p "$DIR"
    tar xf "$ARCHIVE" -C "$DIR" --strip-components=1 2>/dev/null || tar xf "$ARCHIVE" -C /tmp
    # Rename to .logarchive for `log show`
    LOGARCHIVE="$DIR.logarchive"
    if [ ! -d "$LOGARCHIVE" ]; then
      mv "$DIR" "$LOGARCHIVE"
    fi
    echo "Querying logs (last $SINCE)..."
    log show "$LOGARCHIVE" --predicate "$PREDICATE" --style compact --last "$SINCE"
    echo ""
    echo "Archive saved: $LOGARCHIVE"
    echo "Full query:  log show $LOGARCHIVE --predicate '$PREDICATE' --style compact"
    ;;
  --stream|stream|*)
    echo "Streaming logs (subsystem: io.kaitu) — Ctrl+C to stop"
    echo "Filter: $PREDICATE"
    echo "---"
    log stream --predicate "$PREDICATE" --style compact
    ;;
esac
