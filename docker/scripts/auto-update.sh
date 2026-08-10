#!/bin/bash
# Auto-update k2 containers daily
# Deployed via cron: 0 4 * * * /apps/k2s/auto-update.sh
# 04:00 Beijing time (host TZ Asia/Singapore = UTC+8)
#
# What it does:
#   1. Random delay 0-10 minutes (stagger across nodes)
#   2. Snapshot current image IDs
#   3. Pull the pinned images (retry up to 5x for ECR rate limits)
#   4. Compare running vs pinned tag — skip restart if nothing changed
#   5. docker compose down (remove containers + networks, keep volumes)
#   6. docker compose up -d (fresh start)
#   7. Verify sidecar healthy
#   8. Slack notification on update or error (silent when no changes)

COMPOSE_DIR="/apps/k2s"
LOG_FILE="${COMPOSE_DIR}/auto-update.log"
MAX_LOG_SIZE=1048576  # 1MB
SLACK_WEBHOOK="https://hooks.slack.com/services/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"

# Read node name from .env
NODE_NAME=$(grep -oP '^K2_NODE_NAME=\K.*' "${COMPOSE_DIR}/.env" 2>/dev/null || hostname)

# The tag compose actually deploys. This MUST mirror docker-compose.yml's
# `image: public.ecr.aws/d6n9t2r2/...:${K2_VERSION:-latest}` — the restart
# decision below compares the running container against this tag, and comparing
# against anything else asks a question whose answer cannot change what a
# restart produces.
K2_VERSION=$(grep -oP '^K2_VERSION=\K.*' "${COMPOSE_DIR}/.env" 2>/dev/null)
DEPLOY_TAG="${K2_VERSION:-latest}"

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

# --- Compare running container image IDs vs the pulled DEPLOY_TAG images ---
# If any container uses an outdated image, trigger restart.
#
# This compares against DEPLOY_TAG, not `:latest`. Comparing against `:latest`
# caused nine consecutive nights of pointless restarts across 13 nodes
# (2026-08-01 .. 2026-08-09, found 2026-08-10): `docker compose pull` only ever
# fetches the pinned tag, so a `:latest` image left on disk by provisioning is
# frozen forever. Once the fleet moved to a newer pinned build, running never
# again equalled that stale `:latest`, so every node answered CHANGED every
# night and did a full down/up — dropping every live connection — while coming
# back on the very same pinned build it was already running. The restart could
# not converge, because a restart does not change what `:latest` points at.
#
# Comparing against DEPLOY_TAG converges by construction: after the pull above,
# the two differ only when the pinned tag genuinely moved (a re-push) or the
# .env was pointed at a new version, and the restart then makes them equal.
NEEDS_RESTART=0
for SVC_CONTAINER in "k2-sidecar:k2-sidecar" "k2s:k2s"; do
    CONTAINER="${SVC_CONTAINER%%:*}"
    IMAGE="${SVC_CONTAINER##*:}"
    RUNNING_ID=$(docker inspect --format='{{.Image}}' "$CONTAINER" 2>/dev/null | cut -c8-19)
    PULLED_ID=$(docker image inspect --format='{{.Id}}' "public.ecr.aws/d6n9t2r2/${IMAGE}:${DEPLOY_TAG}" 2>/dev/null | cut -c8-19)
    if [ -n "$RUNNING_ID" ] && [ -n "$PULLED_ID" ] && [ "$RUNNING_ID" != "$PULLED_ID" ]; then
        echo "  $CONTAINER: running=$RUNNING_ID ${DEPLOY_TAG}=$PULLED_ID -> CHANGED"
        NEEDS_RESTART=1
    fi
done

if [ "${K2_FORCE_RESTART:-}" = "1" ]; then
    NEEDS_RESTART=1
    echo "Force restart requested (K2_FORCE_RESTART=1)"
fi

if [ "$NEEDS_RESTART" = "0" ]; then
    echo "All containers already on ${DEPLOY_TAG}, no restart needed."
    echo "Finished: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
    echo ""
    exit 0
fi

echo "Restarting containers..."

# --- Snapshot k2s logs before destroying containers ---
echo "--- Snapshotting k2s logs before down ---"
SNAPSHOT_DIR="/var/log/k2s-crashes"
mkdir -p "$SNAPSHOT_DIR"
SNAPSHOT_TS=$(date -u '+%Y%m%d-%H%M%S')
SNAPSHOT_FILE="${SNAPSHOT_DIR}/snapshot-${SNAPSHOT_TS}.log"
{
    echo "=== pre-update log snapshot ==="
    echo "Node: ${NODE_NAME}"
    echo "Time: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
    echo ""
    echo "=== k2s container state ==="
    docker inspect --format='ExitCode={{.State.ExitCode}} Status={{.State.Status}} StartedAt={{.State.StartedAt}} OOMKilled={{.State.OOMKilled}} RestartCount={{.RestartCount}}' k2s 2>/dev/null || echo "(not running)"
    echo ""
    echo "=== k2s last 500 log lines ==="
    docker logs --tail 500 --timestamps k2s 2>&1 || echo "(no logs)"
} > "$SNAPSHOT_FILE" 2>&1
echo "Saved to $SNAPSHOT_FILE"

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
