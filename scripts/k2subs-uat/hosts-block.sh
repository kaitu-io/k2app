#!/usr/bin/env bash
# Block Center by redirecting k2.52j.me to 127.0.0.1 via /etc/hosts.
# Always installs detached fallback to auto-restore.

set -euo pipefail

ACTION="${1:-}"
HOSTS="/etc/hosts"
BACKUP="/tmp/hosts.k2subs-uat.backup"
MARKER="# k2subs-uat-block"

restore() {
  if sudo test -f "$BACKUP"; then
    sudo cp "$BACKUP" "$HOSTS"
    sudo rm -f "$BACKUP"
    sudo dscacheutil -flushcache 2>/dev/null || true
    sudo killall -HUP mDNSResponder 2>/dev/null || true
    echo "/etc/hosts restored"
  else
    # No backup — just strip our marker lines if present
    sudo sed -i '' "/$MARKER/d" "$HOSTS"
    sudo dscacheutil -flushcache 2>/dev/null || true
    echo "/etc/hosts marker lines stripped"
  fi
}

block() {
  # Backup
  if ! sudo test -f "$BACKUP"; then
    sudo cp "$HOSTS" "$BACKUP"
  fi
  # Append block lines for BOTH A and AAAA (Center is on Cloudfront which serves IPv6)
  sudo tee -a "$HOSTS" > /dev/null <<EOF
127.0.0.1 k2.52j.me $MARKER
::1 k2.52j.me $MARKER
EOF
  sudo dscacheutil -flushcache 2>/dev/null || true
  sudo killall -HUP mDNSResponder 2>/dev/null || true
  # Detached fallback: auto-restore after 120s
  sudo nohup bash -c "sleep 120; cp '$BACKUP' '$HOSTS' 2>/dev/null; rm -f '$BACKUP'; dscacheutil -flushcache 2>/dev/null; killall -HUP mDNSResponder 2>/dev/null" > /tmp/hosts-restore-fallback.log 2>&1 &
  disown
  echo "Center blocked (hosts → 127.0.0.1); auto-restore in 120s"
}

case "$ACTION" in
  block) block ;;
  unblock|restore) restore ;;
  *) echo "usage: $0 block|unblock" >&2; exit 1 ;;
esac
