#!/bin/bash
# E2E smoke test: build k2r with embedded webapp, run in Docker, verify 12 HTTP checks.
set -euo pipefail

CONTAINER=k2r-webapp-test
PORT=${K2R_TEST_PORT:-11779}
IMAGE=alpine:latest
PASS=0
FAIL=0

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
K2_DIR="$REPO_ROOT/k2"
WEBAPP_DIR="$REPO_ROOT/webapp"
BINARY="$K2_DIR/build/k2r-linux-amd64-webapp-test"

# Version from package.json
VERSION=$(node -p "require('$REPO_ROOT/package.json').version" 2>/dev/null || echo "dev")
COMMIT=$(git -C "$K2_DIR" rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS="-s -w -X main.version=$VERSION -X main.commit=$COMMIT"

BASE_URL="http://localhost:$PORT"

cleanup() {
    docker stop "$CONTAINER" 2>/dev/null || true
    # Restore gateway/dist placeholder (remove real webapp dist)
    if [ -d "$K2_DIR/gateway/dist" ] && [ -f "$K2_DIR/gateway/dist/.k2r-webapp-test-marker" ]; then
        rm -rf "$K2_DIR/gateway/dist"
        mkdir -p "$K2_DIR/gateway/dist"
        touch "$K2_DIR/gateway/dist/.gitkeep"
    fi
    rm -f "$BINARY"
}
trap cleanup EXIT

check() {
    local name=$1 result=$2
    if [ "$result" = "ok" ]; then
        printf "  PASS  %s\n" "$name"
        PASS=$((PASS + 1))
    else
        printf "  FAIL  %s\n" "$name"
        FAIL=$((FAIL + 1))
    fi
}

echo "=== k2r Webapp E2E Smoke Test ==="
echo "Version: $VERSION  Commit: $COMMIT"
echo ""

# Step 1: Build webapp
echo "[1/4] Building webapp..."
(cd "$WEBAPP_DIR" && yarn build)

# Step 2: Copy to embed path
echo "[2/4] Copying webapp to gateway/dist..."
rm -rf "$K2_DIR/gateway/dist"
cp -r "$WEBAPP_DIR/dist" "$K2_DIR/gateway/dist"
# Mark so cleanup knows it was placed by this test
touch "$K2_DIR/gateway/dist/.k2r-webapp-test-marker"

# Step 3: Build k2r with webapp embedded
echo "[3/4] Building k2r (linux/amd64, webapp embedded)..."
mkdir -p "$K2_DIR/build"
(cd "$K2_DIR" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags "$LDFLAGS" -o "$BINARY" ./cmd/k2r)
echo "      Binary: $BINARY ($(du -sh "$BINARY" | cut -f1))"

# Step 4: Run in Docker
echo "[4/4] Starting k2r in Docker (port $PORT)..."
docker run --rm -d --name "$CONTAINER" \
    --platform linux/amd64 \
    -p "$PORT":1779 \
    --entrypoint "" \
    -v "$BINARY:/usr/bin/k2r:ro" \
    "$IMAGE" /usr/bin/k2r run

# Wait for startup (poll /ping, up to 20s)
echo "      Waiting for startup..."
for i in $(seq 1 20); do
    if curl -sf "$BASE_URL/ping" >/dev/null 2>&1; then
        echo "      Ready after ${i}s"
        break
    fi
    if [ "$i" -eq 20 ]; then
        echo "FAIL: k2r did not start within 20s"
        docker logs "$CONTAINER" 2>&1 | tail -30
        exit 1
    fi
    sleep 1
done

echo ""
echo "=== Smoke Tests ==="

# Test 1: GET /ping → code 0
result=$(curl -sf "$BASE_URL/ping" 2>/dev/null | jq -e '.code == 0' 2>/dev/null && echo "ok" || echo "fail")
check "GET /ping → code 0" "$result"

# Test 2: GET / → HTML with __K2_GATEWAY__ injection
result=$(curl -sf "$BASE_URL/" 2>/dev/null | grep -q '__K2_GATEWAY__' && echo "ok" || echo "fail")
check "GET / → HTML with __K2_GATEWAY__ injection" "$result"

# Test 3: GET / → version string in injection
result=$(curl -sf "$BASE_URL/" 2>/dev/null | grep -q "\"$VERSION\"" && echo "ok" || echo "fail")
check "GET / → version string in injection" "$result"

# Test 4: GET /dashboard → SPA fallback returns HTML
result=$(curl -sf "$BASE_URL/dashboard" 2>/dev/null | grep -q '<html' && echo "ok" || echo "fail")
check "GET /dashboard → SPA fallback returns HTML" "$result"

# Test 5: GET /api/platform → platformType=gateway
result=$(curl -sf "$BASE_URL/api/platform" 2>/dev/null | jq -e '.data.platformType == "gateway"' 2>/dev/null && echo "ok" || echo "fail")
check "GET /api/platform → platformType=gateway" "$result"

# Test 6: POST /api/core {action:version} → version matches
result=$(curl -sf -X POST "$BASE_URL/api/core" \
    -H 'Content-Type: application/json' \
    -d '{"action":"version"}' 2>/dev/null | jq -e --arg v "$VERSION" '.data.version == $v' 2>/dev/null && echo "ok" || echo "fail")
check "POST /api/core {action:version} → version matches" "$result"

# Test 7: POST /api/core {action:status} → state=disconnected
result=$(curl -sf -X POST "$BASE_URL/api/core" \
    -H 'Content-Type: application/json' \
    -d '{"action":"status"}' 2>/dev/null | jq -e '.data.state == "disconnected"' 2>/dev/null && echo "ok" || echo "fail")
check "POST /api/core {action:status} → state=disconnected" "$result"

# Test 8: Storage set+get roundtrip (string value)
set_resp=$(curl -sf -X POST "$BASE_URL/api/storage" \
    -H 'Content-Type: application/json' \
    -d '{"action":"set","key":"smoke_test_str","value":"hello"}' 2>/dev/null)
get_resp=$(curl -sf -X POST "$BASE_URL/api/storage" \
    -H 'Content-Type: application/json' \
    -d '{"action":"get","key":"smoke_test_str"}' 2>/dev/null)
result=$(echo "$get_resp" | jq -e '.data == "hello"' 2>/dev/null && echo "ok" || echo "fail")
check "Storage set+get roundtrip (string value)" "$result"

# Test 9: Storage nested object roundtrip
curl -sf -X POST "$BASE_URL/api/storage" \
    -H 'Content-Type: application/json' \
    -d '{"action":"set","key":"smoke_test_obj","value":{"a":1,"b":"two"}}' >/dev/null 2>&1
get_obj=$(curl -sf -X POST "$BASE_URL/api/storage" \
    -H 'Content-Type: application/json' \
    -d '{"action":"get","key":"smoke_test_obj"}' 2>/dev/null)
result=$(echo "$get_obj" | jq -e '.data.a == 1 and .data.b == "two"' 2>/dev/null && echo "ok" || echo "fail")
check "Storage nested object roundtrip" "$result"

# Test 10: Storage keys + clear
keys_resp=$(curl -sf -X POST "$BASE_URL/api/storage" \
    -H 'Content-Type: application/json' \
    -d '{"action":"keys"}' 2>/dev/null)
keys_ok=$(echo "$keys_resp" | jq -e '.data | length >= 2' 2>/dev/null && echo "ok" || echo "fail")
clear_resp=$(curl -sf -X POST "$BASE_URL/api/storage" \
    -H 'Content-Type: application/json' \
    -d '{"action":"clear"}' 2>/dev/null)
clear_ok=$(echo "$clear_resp" | jq -e '.code == 0' 2>/dev/null && echo "ok" || echo "fail")
if [ "$keys_ok" = "ok" ] && [ "$clear_ok" = "ok" ]; then result="ok"; else result="fail"; fi
check "Storage keys + clear" "$result"

# Test 11: SSE endpoint connectable
# timeout returns 124 when it kills the child; curl exits 0 on connect.
# Both 0 (data received) and 124 (timeout after successful connect) mean the endpoint works.
sse_exit=0; timeout 2 curl -sf -N "$BASE_URL/api/events" >/dev/null 2>&1 || sse_exit=$?
result=$( [ "$sse_exit" -eq 0 ] || [ "$sse_exit" -eq 124 ] && echo "ok" || echo "fail" )
check "SSE endpoint /api/events connectable" "$result"

# Test 12: POST /api/log-level → code 0
result=$(curl -sf -X POST "$BASE_URL/api/log-level" \
    -H 'Content-Type: application/json' \
    -d '{"level":"debug"}' 2>/dev/null | jq -e '.code == 0' 2>/dev/null && echo "ok" || echo "fail")
check "POST /api/log-level → code 0" "$result"

echo ""
echo "Results: $PASS passed, $FAIL failed (of $((PASS + FAIL)) checks)"

if [ "$FAIL" -gt 0 ]; then
    echo ""
    echo "Container logs (last 30 lines):"
    docker logs "$CONTAINER" 2>&1 | tail -30
    exit 1
fi
