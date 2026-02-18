#!/bin/bash
# Smoke test: run k2 daemon in Docker and verify endpoints
set -euo pipefail

BINARY=${1:?Usage: test-openwrt.sh <binary-path>}
CONTAINER=k2-openwrt-test
PORT=${OPENWRT_PORT:-11777}
IMAGE=${DOCKER_IMAGE:-alpine:latest}
PASS=0
FAIL=0

if [ ! -f "$BINARY" ]; then
    echo "Binary not found: $BINARY"
    exit 1
fi

cleanup() { docker stop "$CONTAINER" 2>/dev/null || true; }
trap cleanup EXIT

echo "Starting k2 daemon in Docker (port $PORT, image $IMAGE)..."
docker run --rm -d --name "$CONTAINER" -p "$PORT":1777 \
    --entrypoint "" \
    -v "$(pwd)/$BINARY:/usr/bin/k2:ro" \
    "$IMAGE" /usr/bin/k2 run -l 0.0.0.0:1777

# Wait for daemon
echo "Waiting for daemon..."
for i in $(seq 1 15); do
    if curl -sf "http://localhost:$PORT/ping" >/dev/null 2>&1; then
        break
    fi
    if [ "$i" -eq 15 ]; then
        echo "FAIL: daemon did not start within 15s"
        docker logs "$CONTAINER" 2>&1 | tail -20
        exit 1
    fi
    sleep 1
done

check() {
    local name=$1 result=$2
    if [ "$result" = "ok" ]; then
        echo "  PASS  $name"
        PASS=$((PASS + 1))
    else
        echo "  FAIL  $name"
        FAIL=$((FAIL + 1))
    fi
}

echo ""
echo "=== Smoke Tests ==="

# Test 1: /ping
if curl -sf "http://localhost:$PORT/ping" >/dev/null 2>&1; then
    check "/ping endpoint" "ok"
else
    check "/ping endpoint" "fail"
fi

# Test 2: webapp serves HTML
if curl -sf "http://localhost:$PORT/" 2>/dev/null | grep -q '<html'; then
    check "webapp serves HTML" "ok"
else
    check "webapp serves HTML" "fail"
fi

# Test 3: k2 version CLI
if docker exec "$CONTAINER" /usr/bin/k2 version >/dev/null 2>&1; then
    check "k2 version CLI" "ok"
else
    check "k2 version CLI" "fail"
fi

# Test 4: /api/core responds
if curl -sf -X POST "http://localhost:$PORT/api/core" \
    -H 'Content-Type: application/json' \
    -d '{"action":"version"}' 2>/dev/null | grep -q 'version'; then
    check "/api/core version" "ok"
else
    check "/api/core version" "fail"
fi

echo ""
echo "Results: $PASS passed, $FAIL failed"

if [ "$FAIL" -gt 0 ]; then
    echo ""
    echo "Container logs:"
    docker logs "$CONTAINER" 2>&1 | tail -20
    exit 1
fi
