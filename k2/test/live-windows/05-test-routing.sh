#!/usr/bin/env bash
# Phase 5: Smart routing and data plane tests (Windows)
# Batches HTTP tests into minimal SSH calls
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/env.sh"
PHASE_NAME="phase5-routing"
reset_counters

echo "=== Phase 5: Smart Routing & Data Plane (Windows) ==="

mkdir -p "$DIAG_DIR"

# Individual HTTP tests (avoids CMD escaping issues in batched commands)
echo "[1] Exit IP check..."
IPCHECK=$(win "curl -s --connect-timeout 10 --max-time 15 $TEST_IP_CHECK_URL" 2>&1 || echo "")
echo "$IPCHECK" > "$DIAG_DIR/phase5-ipcheck.txt"
if echo "$IPCHECK" | grep -q "$K2_SERVER_IP"; then
    pass "Exit IP is server ($K2_SERVER_IP) — proxy working"
elif echo "$IPCHECK" | grep -q '"ip"'; then
    EXIT_IP=$(echo "$IPCHECK" | python3 -c "import sys,json; print(json.load(sys.stdin).get('ip','?'))" 2>/dev/null || echo "?")
    fail "Exit IP is $EXIT_IP (expected $K2_SERVER_IP)"
else
    fail "ipinfo.io request failed"
fi

echo "[2] China site..."
CHINA=$(win "curl -s -o NUL -w %{http_code} --connect-timeout 10 --max-time 15 https://$TEST_CHINA_DOMAIN" 2>&1 || echo "000")
if [ "$CHINA" = "200" ] || [ "$CHINA" = "302" ] || [ "$CHINA" = "301" ]; then
    pass "China (HTTP $CHINA) — $TEST_CHINA_DOMAIN"
else
    fail "China site failed (HTTP $CHINA)"
fi

echo "[3] Foreign site..."
FOREIGN=$(win "curl -s -o NUL -w %{http_code} --connect-timeout 10 --max-time 15 https://$TEST_FOREIGN_DOMAIN" 2>&1 || echo "000")
if [ "$FOREIGN" = "200" ] || [ "$FOREIGN" = "302" ] || [ "$FOREIGN" = "301" ]; then
    pass "Foreign proxy (HTTP $FOREIGN) — $TEST_FOREIGN_DOMAIN"
else
    fail "Foreign site failed (HTTP $FOREIGN)"
fi

echo "[4] Google..."
GOOGLE=$(win "curl -s --connect-timeout 10 --max-time 15 https://www.google.com/" 2>&1 || echo "")
if echo "$GOOGLE" | grep -qi "google\|html\|doctype"; then
    pass "Google page loaded"
else
    fail "Google failed"
fi

echo "[5] YouTube..."
YOUTUBE=$(win "curl -s -o NUL -w %{http_code} --connect-timeout 10 --max-time 15 https://www.youtube.com" 2>&1 || echo "000")
if [ "$YOUTUBE" = "200" ] || [ "$YOUTUBE" = "302" ] || [ "$YOUTUBE" = "301" ]; then
    pass "YouTube reachable (HTTP $YOUTUBE)"
else
    fail "YouTube unreachable (HTTP $YOUTUBE)"
fi

echo "[6] Safety checks..."
GW_PING=$(win "ping -n 1 -w 2000 $WIN_GATEWAY" 2>&1 || echo "")
if echo "$GW_PING" | LC_ALL=C grep -qi "TTL=\|ttl="; then
    pass "Gateway ($WIN_GATEWAY) reachable"
else
    fail "Gateway UNREACHABLE — safety layer may have failed!"
fi

SSH_OK=$(win "echo SSH_ALIVE" 2>&1 || echo "")
if echo "$SSH_OK" | grep -q "SSH_ALIVE"; then
    pass "SSH alive — network safety confirmed"
else
    fail "SSH broken!"
fi

echo ""
echo "=== Phase 5 results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
