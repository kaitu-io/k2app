#!/bin/bash
# Deploy docker-compose.yml to Kaitu VPN nodes
#
# Usage:
#   ./scripts/deploy-compose.sh              # Active nodes only (tunnelCount > 0)
#   ./scripts/deploy-compose.sh --all        # All reachable nodes
#   ./scripts/deploy-compose.sh --dry-run    # Show what would be deployed
#
# Requires:
#   KAITU_CENTER_URL  — Center API base URL (e.g. https://k2.52j.me)
#   KAITU_ACCESS_KEY  — Center API access key
#
# SSH config (optional, defaults shown):
#   KAITU_SSH_USER=ubuntu  KAITU_SSH_PORT=1022

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
COMPOSE_FILE="$PROJECT_DIR/docker/docker-compose.yml"
DEPLOY_DIR="/apps/kaitu-slave"

SSH_USER="${KAITU_SSH_USER:-ubuntu}"
SSH_PORT="${KAITU_SSH_PORT:-1022}"
SSH_OPTS="-n -o ConnectTimeout=5 -o StrictHostKeyChecking=no -o BatchMode=yes -p $SSH_PORT"

INCLUDE_ALL=false
DRY_RUN=false

for arg in "$@"; do
  case "$arg" in
    --all) INCLUDE_ALL=true ;;
    --dry-run) DRY_RUN=true ;;
    -h|--help)
      echo "Usage: $0 [--all] [--dry-run]"
      echo "  --all      Include inactive nodes (tunnelCount=0)"
      echo "  --dry-run  Show what would be deployed without doing it"
      exit 0
      ;;
    *) echo "Unknown option: $arg"; exit 1 ;;
  esac
done

# --- Validate prerequisites ---

if [ ! -f "$COMPOSE_FILE" ]; then
  echo "ERROR: docker-compose.yml not found at $COMPOSE_FILE"
  exit 1
fi

if [ -z "${KAITU_CENTER_URL:-}" ] || [ -z "${KAITU_ACCESS_KEY:-}" ]; then
  echo "ERROR: KAITU_CENTER_URL and KAITU_ACCESS_KEY must be set"
  exit 1
fi

# --- Fetch node list from Center API ---

echo "Fetching node list from Center API..."
NODES_JSON=$(curl -sf -H "X-Access-Key: $KAITU_ACCESS_KEY" "$KAITU_CENTER_URL/app/nodes/batch-matrix")

if [ -z "$NODES_JSON" ]; then
  echo "ERROR: Failed to fetch nodes from Center API"
  exit 1
fi

# Parse nodes: extract ip, name, tunnelCount
# Filter: tunnelCount > 0 unless --all
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
LOCAL_MD5=$(md5 -q "$COMPOSE_FILE" 2>/dev/null || md5sum "$COMPOSE_FILE" | awk '{print $1}')

echo "Source: $COMPOSE_FILE (md5: $LOCAL_MD5)"
echo "Target: $DEPLOY_DIR/docker-compose.yml"
if [ "$INCLUDE_ALL" = true ]; then
  echo "Mode: ALL nodes ($NODE_COUNT)"
else
  echo "Mode: Active nodes only ($NODE_COUNT, tunnelCount > 0)"
fi
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
  printf "  %-24s %-16s tunnels=%-2s " "$LABEL" "$IP" "$TC"

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

  # Ensure deploy directory exists
  ssh $SSH_OPTS "$SSH_USER@$IP" "sudo mkdir -p $DEPLOY_DIR && sudo chown -R $SSH_USER: $DEPLOY_DIR" 2>/dev/null

  # Compare remote file MD5
  REMOTE_MD5=$(ssh $SSH_OPTS "$SSH_USER@$IP" "md5sum $DEPLOY_DIR/docker-compose.yml 2>/dev/null | awk '{print \$1}'" 2>/dev/null)

  if [ "$REMOTE_MD5" = "$LOCAL_MD5" ]; then
    echo "UP-TO-DATE"
    SKIPPED=$((SKIPPED + 1))
    continue
  fi

  # Upload
  if scp -P "$SSH_PORT" -o ConnectTimeout=5 -o StrictHostKeyChecking=no -o BatchMode=yes \
      "$COMPOSE_FILE" "$SSH_USER@$IP:$DEPLOY_DIR/docker-compose.yml" >/dev/null 2>&1; then
    if [ -n "$REMOTE_MD5" ]; then
      echo "UPDATED"
    else
      echo "DEPLOYED (new)"
    fi
    SUCCESS=$((SUCCESS + 1))
  else
    echo "SCP FAILED"
    FAILED=$((FAILED + 1))
  fi
done <<< "$NODE_LIST"

echo ""
echo "=== Summary ==="
echo "  Deployed/Updated: $SUCCESS"
echo "  Up-to-date:       $SKIPPED"
echo "  Failed:           $FAILED"
echo "  Total:            $NODE_COUNT"
