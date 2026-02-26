#!/bin/bash
# Deploy auto-update.sh + cron to Kaitu VPN nodes
#
# Usage:
#   .claude/skills/kaitu-node-ops/deploy-auto-update.sh              # Active nodes only
#   .claude/skills/kaitu-node-ops/deploy-auto-update.sh --all        # All reachable nodes
#   .claude/skills/kaitu-node-ops/deploy-auto-update.sh --dry-run    # Show what would be deployed
#   .claude/skills/kaitu-node-ops/deploy-auto-update.sh --node=1.2.3.4  # Single node
#
# Deploys:
#   1. docker/scripts/auto-update.sh → /apps/kaitu-slave/auto-update.sh
#   2. Cron entry: 0 4 * * * ... >> auto-update.log 2>&1 (04:00 Beijing time, requires Asia/Singapore timezone)
#
# Requires:
#   KAITU_CENTER_URL  — Center API base URL
#   KAITU_ACCESS_KEY  — Center API access key

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR" && git rev-parse --show-toplevel)"
AUTO_UPDATE_SCRIPT="$PROJECT_DIR/docker/scripts/auto-update.sh"
DEPLOY_DIR="/apps/kaitu-slave"
CRON_ENTRY="0 4 * * * /apps/kaitu-slave/auto-update.sh >> /apps/kaitu-slave/auto-update.log 2>&1"

SSH_USER="${KAITU_SSH_USER:-ubuntu}"
SSH_PORT="${KAITU_SSH_PORT:-1022}"
SSH_OPTS="-n -o ConnectTimeout=5 -o StrictHostKeyChecking=no -o BatchMode=yes -p $SSH_PORT"

INCLUDE_ALL=false
DRY_RUN=false
SINGLE_NODE=""

for arg in "$@"; do
  case "$arg" in
    --all) INCLUDE_ALL=true ;;
    --dry-run) DRY_RUN=true ;;
    --node=*) SINGLE_NODE="${arg#--node=}" ;;
    -h|--help)
      echo "Usage: $0 [--all] [--dry-run] [--node=IP]"
      echo "  --all        Include inactive nodes (tunnelCount=0)"
      echo "  --dry-run    Show what would be deployed without doing it"
      echo "  --node=IP    Deploy to a single node only"
      exit 0
      ;;
    *) echo "Unknown option: $arg"; exit 1 ;;
  esac
done

# --- Validate prerequisites ---

if [ ! -f "$AUTO_UPDATE_SCRIPT" ]; then
  echo "ERROR: auto-update.sh not found at $AUTO_UPDATE_SCRIPT"
  exit 1
fi

if [ -n "$SINGLE_NODE" ]; then
  NODE_LIST="$SINGLE_NODE|single|1"
  NODE_COUNT=1
  echo "Target: single node $SINGLE_NODE"
else
  if [ -z "${KAITU_CENTER_URL:-}" ] || [ -z "${KAITU_ACCESS_KEY:-}" ]; then
    echo "ERROR: KAITU_CENTER_URL and KAITU_ACCESS_KEY must be set"
    exit 1
  fi

  echo "Fetching node list from Center API..."
  NODES_JSON=$(curl -sf -H "X-Access-Key: $KAITU_ACCESS_KEY" "$KAITU_CENTER_URL/app/nodes/batch-matrix")

  if [ -z "$NODES_JSON" ]; then
    echo "ERROR: Failed to fetch nodes from Center API"
    exit 1
  fi

  NODE_LIST=$(echo "$NODES_JSON" | python3 -c "
import json, sys
data = json.load(sys.stdin)
if data.get('code', -1) != 0:
    print('API_ERROR', file=sys.stderr)
    sys.exit(1)
for n in sorted(data['data']['nodes'], key=lambda x: x.get('name','')):
    ip = n.get('ipv4','')
    name = n.get('name','')
    tc = n.get('tunnelCount', len(n.get('tunnels',[])))
    include_all = '$INCLUDE_ALL' == 'true'
    if not ip:
        continue
    if not include_all and tc == 0:
        continue
    print(f'{ip}|{name}|{tc}')
")

  if [ -z "$NODE_LIST" ]; then
    echo "No nodes to deploy to."
    exit 0
  fi

  NODE_COUNT=$(echo "$NODE_LIST" | wc -l | tr -d ' ')
fi

LOCAL_MD5=$(md5 -q "$AUTO_UPDATE_SCRIPT" 2>/dev/null || md5sum "$AUTO_UPDATE_SCRIPT" | awk '{print $1}')

echo "Source: $AUTO_UPDATE_SCRIPT (md5: $LOCAL_MD5)"
echo "Cron:  $CRON_ENTRY"
echo "Nodes: $NODE_COUNT"
if [ "$DRY_RUN" = true ]; then
  echo "Mode: DRY RUN"
fi
echo ""

SUCCESS=0
SKIPPED=0
FAILED=0

while IFS='|' read -r IP NAME TC; do
  LABEL="$NAME"
  [ "$NAME" = "$IP" ] && LABEL="$IP"
  printf "  %-24s %-16s " "$LABEL" "$IP"

  if [ "$DRY_RUN" = true ]; then
    echo "WOULD DEPLOY"
    SUCCESS=$((SUCCESS + 1))
    continue
  fi

  # Test SSH connectivity
  if ! ssh $SSH_OPTS "$SSH_USER@$IP" "echo ok" >/dev/null 2>&1; then
    echo "UNREACHABLE"
    FAILED=$((FAILED + 1))
    continue
  fi

  # Compare remote file MD5
  REMOTE_MD5=$(ssh $SSH_OPTS "$SSH_USER@$IP" "md5sum $DEPLOY_DIR/auto-update.sh 2>/dev/null | awk '{print \$1}'" 2>/dev/null)

  SCRIPT_STATUS="UP-TO-DATE"
  if [ "$REMOTE_MD5" != "$LOCAL_MD5" ]; then
    # Upload script
    if scp -P "$SSH_PORT" -o ConnectTimeout=5 -o StrictHostKeyChecking=no -o BatchMode=yes \
        "$AUTO_UPDATE_SCRIPT" "$SSH_USER@$IP:$DEPLOY_DIR/auto-update.sh" >/dev/null 2>&1; then
      ssh $SSH_OPTS "$SSH_USER@$IP" "chmod +x $DEPLOY_DIR/auto-update.sh" 2>/dev/null
      SCRIPT_STATUS="DEPLOYED"
    else
      # Try with sudo (e.g. Lightsail ubuntu user)
      if ssh $SSH_OPTS "$SSH_USER@$IP" "sudo tee $DEPLOY_DIR/auto-update.sh > /dev/null" < "$AUTO_UPDATE_SCRIPT" 2>/dev/null; then
        ssh $SSH_OPTS "$SSH_USER@$IP" "sudo chmod +x $DEPLOY_DIR/auto-update.sh" 2>/dev/null
        SCRIPT_STATUS="DEPLOYED (sudo)"
      else
        echo "SCP FAILED"
        FAILED=$((FAILED + 1))
        continue
      fi
    fi
  fi

  # Ensure cron entry exists (idempotent)
  CRON_EXISTS=$(ssh $SSH_OPTS "$SSH_USER@$IP" "sudo crontab -l 2>/dev/null | grep -c 'auto-update.sh'" 2>/dev/null || echo "0")
  if [ "$CRON_EXISTS" = "0" ]; then
    # Ensure cron is installed
    ssh $SSH_OPTS "$SSH_USER@$IP" "which crontab >/dev/null 2>&1 || sudo apt-get install -y cron >/dev/null 2>&1" 2>/dev/null
    ssh $SSH_OPTS "$SSH_USER@$IP" "sudo bash -c '(crontab -l 2>/dev/null; echo \"$CRON_ENTRY\") | crontab -'" 2>/dev/null
    echo "$SCRIPT_STATUS + CRON ADDED"
  else
    echo "$SCRIPT_STATUS + cron ok"
  fi

  SUCCESS=$((SUCCESS + 1))
done <<< "$NODE_LIST"

echo ""
echo "=== Summary ==="
echo "  Success:    $SUCCESS"
echo "  Skipped:    $SKIPPED"
echo "  Failed:     $FAILED"
echo "  Total:      $NODE_COUNT"
