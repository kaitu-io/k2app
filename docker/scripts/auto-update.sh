#!/bin/bash
# Auto-update k2 containers daily
# Deployed via cron: 0 20 * * * /apps/kaitu-slave/auto-update.sh
# 20:00 UTC = 04:00 Beijing time
#
# What it does:
#   1. Random delay 0-10 minutes (stagger across nodes)
#   2. Snapshot current image IDs
#   3. Pull latest images (retry up to 5x for ECR rate limits)
#   4. Compare — skip restart if nothing changed
#   5. docker compose down (remove containers + networks, keep volumes)
#   6. docker compose up -d (fresh start)
#   7. Verify sidecar healthy
#   8. Slack notification on update or error (silent when no changes)

COMPOSE_DIR="/apps/kaitu-slave"
LOG_FILE="${COMPOSE_DIR}/auto-update.log"
MAX_LOG_SIZE=1048576  # 1MB
SLACK_WEBHOOK="https://hooks.slack.com/services/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"

# Read node name from .env
NODE_NAME=$(grep -oP '^K2_NODE_NAME=\K.*' "${COMPOSE_DIR}/.env" 2>/dev/null || hostname)

# --- Slack helper (only fires on update/error, not on "no changes") ---
slack_notify() {
    local emoji="$1" title="$2" msg="$3"
    [ -z "$SLACK_WEBHOOK" ] && return
    local payload=$(cat <<EOFSLACK
{"text":"${emoji} *${title}*\n\`${NODE_NAME}\` — ${msg}"}
EOFSLACK
)
    curl -sf -X POST -H 'Content-type: application/json' -d "$payload" "$SLACK_WEBHOOK" >/dev/null 2>&1 || true
}

# --- Log rotation ---
if [ -f "$LOG_FILE" ] && [ "$(stat -c%s "$LOG_FILE" 2>/dev/null || stat -f%z "$LOG_FILE" 2>/dev/null)" -gt "$MAX_LOG_SIZE" ]; then
    mv "$LOG_FILE" "${LOG_FILE}.old"
fi

exec >> "$LOG_FILE" 2>&1

echo "========================================"
echo "Auto-update started: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "========================================"

cd "$COMPOSE_DIR" || { echo "ERROR: Cannot cd to $COMPOSE_DIR"; slack_notify "🔴" "Auto-update FAILED" "Cannot cd to $COMPOSE_DIR"; exit 1; }

# --- Stagger: random 0-600s delay to avoid all nodes restarting simultaneously ---
if [ "${K2_NO_STAGGER:-}" != "1" ]; then
    DELAY=$((RANDOM % 600))
    echo "Stagger delay: ${DELAY}s"
    sleep "$DELAY"
fi

# --- Pull latest images (retry up to 5 times with backoff for ECR rate limits) ---
echo "--- Pulling images ---"
PULL_OK=0
for ATTEMPT in 1 2 3 4 5; do
    if docker compose pull 2>&1; then
        PULL_OK=1
        break
    fi
    BACKOFF=$((ATTEMPT * 30))
    echo "Pull attempt $ATTEMPT failed, retrying in ${BACKOFF}s..."
    sleep "$BACKOFF"
done
if [ "$PULL_OK" != "1" ]; then
    echo "ERROR: docker compose pull failed after 5 attempts"
    slack_notify "🔴" "Auto-update FAILED" "docker compose pull failed after 5 attempts"
    exit 1
fi

# --- Compare running container image IDs vs pulled :latest IDs ---
# If any container uses an outdated image, trigger restart.
NEEDS_RESTART=0
for SVC_CONTAINER in "k2-sidecar:k2-sidecar" "k2v5:k2v5" "k2v4-slave:k2-slave" "k2-oc:k2-oc"; do
    CONTAINER="${SVC_CONTAINER%%:*}"
    IMAGE="${SVC_CONTAINER##*:}"
    RUNNING_ID=$(docker inspect --format='{{.Image}}' "$CONTAINER" 2>/dev/null | cut -c8-19)
    LATEST_ID=$(docker image inspect --format='{{.Id}}' "public.ecr.aws/d6n9t2r2/${IMAGE}:latest" 2>/dev/null | cut -c8-19)
    if [ -n "$RUNNING_ID" ] && [ -n "$LATEST_ID" ] && [ "$RUNNING_ID" != "$LATEST_ID" ]; then
        echo "  $CONTAINER: running=$RUNNING_ID latest=$LATEST_ID -> CHANGED"
        NEEDS_RESTART=1
    fi
done

if [ "$NEEDS_RESTART" = "0" ]; then
    echo "All containers already on latest images, no restart needed."
    echo "Finished: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
    echo ""
    exit 0
fi

echo "Image changes detected, restarting..."

# --- Down: remove containers + networks, keep volumes ---
echo "--- Stopping containers (down) ---"
docker compose down 2>&1

# --- Up: fresh start ---
echo "--- Starting containers (up) ---"
docker compose up -d 2>&1

# --- Verify ---
echo "--- Verifying (wait 30s) ---"
sleep 30

HEALTH=$(docker inspect --format='{{.State.Health.Status}}' k2-sidecar 2>/dev/null || echo "unknown")
RUNNING=$(docker compose ps --status running --format json 2>/dev/null | wc -l)

echo "Sidecar health: $HEALTH"
echo "Running containers: $RUNNING"

if [ "$HEALTH" = "healthy" ]; then
    slack_notify "✅" "Auto-update OK" "Updated and restarted. Sidecar healthy, ${RUNNING} containers running."
else
    slack_notify "🟡" "Auto-update WARNING" "Updated but sidecar status: ${HEALTH}. ${RUNNING} containers running."
fi

echo "Finished: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo ""
