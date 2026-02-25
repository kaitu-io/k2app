#!/usr/bin/env bash
# Phase 4: DNS resolution and hijack tests (Windows)
# Batches all DNS lookups into one SSH call for efficiency
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/env.sh"
PHASE_NAME="phase4-dns"
reset_counters

echo "=== Phase 4: DNS Tests (Windows) ==="

# All DNS lookups in ONE SSH call
echo "[1] Running all DNS lookups (single SSH call)..."
DNS_RESULTS=$(win "cmd /C \"echo ==HIJACK== & nslookup example.com 198.18.0.8 2>nul & echo ==CHINA== & nslookup $TEST_CHINA_DOMAIN 2>nul & echo ==FOREIGN== & nslookup $TEST_FOREIGN_DOMAIN 2>nul & echo ==QQ== & nslookup www.qq.com 2>nul & echo ==TAOBAO== & nslookup www.taobao.com 2>nul & echo ==YOUTUBE== & nslookup www.youtube.com 2>nul & echo ==GITHUB== & nslookup www.github.com 2>nul & echo ==INTERCEPT== & nslookup example.com 1.2.3.4 2>nul & echo ==END==\"" 2>&1 || echo "SSH_FAILED")

# Save raw results
mkdir -p "$DIAG_DIR"
echo "$DNS_RESULTS" > "$DIAG_DIR/phase4-dns-raw.txt"

# Parse helper: extract section between markers
section() {
    echo "$DNS_RESULTS" | LC_ALL=C sed -n "/==$1==/,/==[A-Z]/p" | LC_ALL=C sed '$d'
}

has_addr() {
    echo "$1" | grep -qi "address.*[0-9]\|addresses"
}

get_addr() {
    echo "$1" | grep -i "address" | grep -v "#" | tail -1 | awk '{print $NF}'
}

# Test 1: DNS hijack
echo "[2] Checking results..."
HIJACK=$(section HIJACK)
if has_addr "$HIJACK"; then
    pass "DNS hijack — example.com → $(get_addr "$HIJACK")"
else
    fail "DNS hijack not working"
fi

# Test 2: Chinese domain
CHINA=$(section CHINA)
if has_addr "$CHINA"; then
    pass "China DNS — $TEST_CHINA_DOMAIN → $(get_addr "$CHINA")"
else
    fail "China DNS failed"
fi

# Test 3: Foreign domain
FOREIGN=$(section FOREIGN)
if has_addr "$FOREIGN"; then
    pass "Proxy DNS — $TEST_FOREIGN_DOMAIN → $(get_addr "$FOREIGN")"
else
    fail "Proxy DNS failed"
fi

# Test 4: Batch routing
for tag in QQ TAOBAO YOUTUBE GITHUB; do
    name=$(echo "$tag" | tr '[:upper:]' '[:lower:]')
    RESULT=$(section "$tag")
    if has_addr "$RESULT"; then
        pass "www.${name}.com → $(get_addr "$RESULT")"
    else
        fail "www.${name}.com — failed to resolve"
    fi
done

# Test 5: DNS interception
INTERCEPT=$(section INTERCEPT)
if has_addr "$INTERCEPT"; then
    pass "DNS interception confirmed (1.2.3.4:53 intercepted by TUN)"
else
    echo "  (nslookup to 1.2.3.4 timed out — may be expected)"
fi

# Test 6: k2 log DNS activity (separate call, minimal data)
echo "[3] DNS log analysis..."
DNS_LOG=$(win "findstr /I /C:\"dns\" $WIN_K2_LOG 2>nul" | tail -15 || echo "(no entries)")
DNS_COUNT=$(echo "$DNS_LOG" | grep -c "dns" || echo "0")
echo "  DNS log entries: $DNS_COUNT"
echo "$DNS_LOG" | tail -5 | while read -r line; do echo "    $line"; done

echo ""
echo "=== Phase 4 results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
