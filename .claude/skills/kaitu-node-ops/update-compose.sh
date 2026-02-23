#!/bin/bash
# Update docker compose on all active nodes: pull latest images + restart
#
# Usage:
#   ./update-compose.sh                    # Update all active nodes
#   ./update-compose.sh --dry-run          # Preview what would be updated
#   ./update-compose.sh --sleep=30         # Custom interval between nodes (default: 60s)
#   ./update-compose.sh --node=8.218.55.0  # Update single node only
#
# Requires:
#   KAITU_CENTER_URL  — Center API base URL
#   KAITU_ACCESS_KEY  — Center API access key
#
# What it does per node:
#   1. docker compose pull (fetch latest images)
#   2. docker compose up -d (recreate changed containers)
#   3. Wait for sidecar healthy
#   4. Sleep before next node

set -euo pipefail

SSH_USER="${KAITU_SSH_USER:-ubuntu}"
SSH_PORT="${KAITU_SSH_PORT:-1022}"
SSH_OPTS="-n -o ConnectTimeout=10 -o StrictHostKeyChecking=no -o BatchMode=yes -p $SSH_PORT"

DRY_RUN=false
SLEEP_INTERVAL=60
SINGLE_NODE=""

for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=true ;;
    --sleep=*) SLEEP_INTERVAL="${arg#--sleep=}" ;;
    --node=*) SINGLE_NODE="${arg#--node=}" ;;
    -h|--help)
      echo "Usage: $0 [--dry-run] [--sleep=SECONDS] [--node=IP]"
      exit 0
      ;;
    *) echo "Unknown option: $arg"; exit 1 ;;
  esac
done

if [ -z "${KAITU_CENTER_URL:-}" ] || [ -z "${KAITU_ACCESS_KEY:-}" ]; then
  echo "ERROR: KAITU_CENTER_URL and KAITU_ACCESS_KEY must be set"
  exit 1
fi

# --- Fetch active nodes ---
echo "Fetching node list from Center API..."
NODE_LIST=$(curl -sf -H "X-Access-Key: $KAITU_ACCESS_KEY" "$KAITU_CENTER_URL/app/nodes/batch-matrix" | python3 -c "
import json, sys
data = json.load(sys.stdin)
if data.get('code', -1) != 0:
    sys.exit(1)
for n in sorted(data['data']['nodes'], key=lambda x: x.get('name','')):
    ip = n.get('ipv4','')
    name = n.get('name','')
    tc = n.get('tunnelCount', len(n.get('tunnels',[])))
    if not ip or tc == 0:
        continue
    print(f'{ip}|{name}|{tc}')
")

if [ -z "$NODE_LIST" ]; then
  echo "No active nodes found."
  exit 0
fi

# --- Filter nodes ---
UPDATE_LIST=()

while IFS='|' read -r IP NAME TC; do
  if [ -n "$SINGLE_NODE" ] && [ "$IP" != "$SINGLE_NODE" ]; then
    continue
  fi
  UPDATE_LIST+=("$IP|$NAME")
done <<< "$NODE_LIST"

TOTAL=${#UPDATE_LIST[@]}
if [ "$TOTAL" -eq 0 ]; then
  echo "No matching nodes found."
  exit 0
fi

echo ""
echo "=== Updating docker compose on $TOTAL nodes (${SLEEP_INTERVAL}s between each) ==="
if [ "$DRY_RUN" = true ]; then
  echo "    DRY RUN — no changes will be made"
fi
echo ""

SUCCESS=0
FAILED=0
COUNT=0

for entry in "${UPDATE_LIST[@]}"; do
  IP="${entry%%|*}"
  NAME="${entry##*|}"
  COUNT=$((COUNT + 1))

  echo "[$COUNT/$TOTAL] $NAME ($IP)"

  if [ "$DRY_RUN" = true ]; then
    echo "  WOULD UPDATE"
    echo ""
    continue
  fi

  # Step 1: Pull latest images
  echo "  [1/3] docker compose pull..."
  PULL_RC=0
  ssh $SSH_OPTS "$SSH_USER@$IP" "cd /apps/kaitu-slave && docker compose pull -q 2>&1" || PULL_RC=$?
  if [ $PULL_RC -ne 0 ]; then
    echo "  WARNING: pull failed (rate limit?), continuing with up -d..."
  fi

  # Step 2: Restart with new images
  echo "  [2/3] docker compose up -d..."
  if ! ssh $SSH_OPTS "$SSH_USER@$IP" "cd /apps/kaitu-slave && docker compose up -d 2>&1"; then
    echo "  FAILED"
    FAILED=$((FAILED + 1))
    echo ""
    continue
  fi

  # Step 3: Verify sidecar health (k2-sidecar)
  echo "  [3/3] Waiting 10s for sidecar health..."
  sleep 10
  SIDECAR_NAME=$(ssh $SSH_OPTS "$SSH_USER@$IP" "docker ps --format '{{.Names}}' 2>/dev/null" 2>/dev/null | grep -o 'k2[^ ]*sidecar' || echo "k2-sidecar")
  HEALTH=$(ssh $SSH_OPTS "$SSH_USER@$IP" "docker inspect --format='{{.State.Health.Status}}' $SIDECAR_NAME 2>/dev/null" 2>/dev/null || echo "not-found")
  CONTAINERS=$(ssh $SSH_OPTS "$SSH_USER@$IP" "docker ps --format '{{.Names}}' 2>/dev/null" 2>/dev/null | sort | tr '\n' ',' | sed 's/,$//')

  echo "  OK — sidecar=$HEALTH containers=$CONTAINERS"
  SUCCESS=$((SUCCESS + 1))

  if [ $COUNT -lt $TOTAL ]; then
    echo "  Sleeping ${SLEEP_INTERVAL}s..."
    sleep "$SLEEP_INTERVAL"
  fi
  echo ""
done

echo "=== Update Summary ==="
echo "  Success: $SUCCESS"
echo "  Failed:  $FAILED"
echo "  Total:   $TOTAL"
