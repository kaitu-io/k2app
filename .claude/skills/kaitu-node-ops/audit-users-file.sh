#!/bin/bash
# Audit /apps/k2s/users across the fleet — BLOCKING pre-enforce check.
# Any non-empty entry in the bind-mounted users file bypasses Center auth
# entirely (validator chain: users_file FIRST, then remote_url). Before
# flipping K2_ENFORCE_AUTH anywhere, every shared node must show 0.
#
# Usage: ./audit-users-file.sh            # audit all active nodes
#        ./audit-users-file.sh --node=IP  # single node
# Requires: KAITU_CENTER_URL, KAITU_ACCESS_KEY (same as update-compose.sh)
set -euo pipefail

SSH_USER="${KAITU_SSH_USER:-ubuntu}"
SSH_PORT="${KAITU_SSH_PORT:-1022}"
SSH_OPTS="-n -o ConnectTimeout=10 -o StrictHostKeyChecking=no -o BatchMode=yes -p $SSH_PORT"
SINGLE_NODE=""
for arg in "$@"; do
  case "$arg" in
    --node=*) SINGLE_NODE="${arg#--node=}" ;;
    -h|--help) echo "Usage: $0 [--node=IP]"; exit 0 ;;
    *) echo "Unknown option: $arg"; exit 1 ;;
  esac
done
if [ -z "${KAITU_CENTER_URL:-}" ] || [ -z "${KAITU_ACCESS_KEY:-}" ]; then
  echo "ERROR: KAITU_CENTER_URL and KAITU_ACCESS_KEY must be set"; exit 1
fi

NODE_LIST=$(curl -sf -H "X-Access-Key: $KAITU_ACCESS_KEY" -H "Content-Type: application/json" "$KAITU_CENTER_URL/app/nodes?pageSize=100" | python3 -c "
import json, sys
data = json.load(sys.stdin)
if data.get('code', -1) != 0:
    sys.exit(1)
for n in sorted(data['data']['items'], key=lambda x: x.get('name','')):
    ip = n.get('ipv4',''); name = n.get('name',''); tc = len(n.get('tunnels',[]))
    if not ip or tc == 0:
        continue
    print(f'{ip}|{name}')
")

DIRTY=0
while IFS='|' read -r IP NAME; do
  [ -n "$SINGLE_NODE" ] && [ "$IP" != "$SINGLE_NODE" ] && continue
  COUNT=$(ssh $SSH_OPTS "$SSH_USER@$IP" "sudo awk 'NF{n++} END{print n+0}' /apps/k2s/users 2>/dev/null" 2>/dev/null || echo "SSH_ERR")
  printf '%-16s %-28s users_entries=%s\n' "$IP" "$NAME" "$COUNT"
  if [ "$COUNT" != "0" ]; then DIRTY=$((DIRTY + 1)); fi
done <<< "$NODE_LIST"

echo ""
if [ "$DIRTY" -ne 0 ]; then
  echo "BLOCKED: $DIRTY node(s) have non-empty (or unreadable) /apps/k2s/users — resolve before enabling enforce."
  exit 1
fi
echo "OK: all audited nodes have an empty users file."
