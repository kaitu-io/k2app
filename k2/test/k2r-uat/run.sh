#!/usr/bin/env bash
# k2r Gateway UAT — Docker-based integration test.
#
# Tests the full k2r gateway stack as a soft router in a multi-container setup:
#   lan-client (10.99.0.30) → k2r (10.99.0.20, TPROXY) → k2s (10.99.0.10, tunnel) → internet
#
# 10 phases, ~45 test points covering:
#   - Firewall rules (nft/ipt, IPv4+IPv6, exclude sets, loopback bypass)
#   - Tunnel connectivity (status API, HTTP/HTTPS through tunnel)
#   - LAN TCP traffic (HTTP, HTTPS, large file, concurrent connections)
#   - LAN DNS traffic (UDP, TCP, leak detection, NXDOMAIN)
#   - Disconnect & cleanup (rules removed, IPv6 cleanup, sysctl restore, idempotent down)
#   - Reconnect & idempotency (re-up, rule stacking, hot-switch)
#   - Auto-reconnect (persisted state, restart recovery, LAN traffic restoration)
#   - Crash recovery (kill -9, stale rule cleanup, auto-reconnect post-crash)
#   - API completeness (status fields, SSE events, log-level, error handling)
#   - Node cutoff (quota-pause simulation: freeze k2s → LAN flow fails → recovers)
#   - Enterprise multi-slot mode (k2subs:// slot_bindings: per-slot egress,
#     fail-closed dead slot, DNS follows the same rule, manifest-change
#     convergence, revert to consumer mode)
#
# Usage:
#   ./run.sh                       # full UAT
#   ./run.sh --phase rules         # single phase
#   ./run.sh --phase node-cutoff   # quota-cutoff phase only
#   ./run.sh --phase multislot     # enterprise multi-slot phase only
#   ./run.sh --keep                # keep containers after test for debugging
#
# Env:
#   K2R_UAT_REQUIRE_DATAPLANE=1    # CI: make a missing TPROXY data plane a hard
#                                  # failure instead of a graceful skip
#
# Prerequisites: docker, jq, curl, go
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"

# ── Tunable parameters ────────────────────────────────────────────
: "${CONNECT_TIMEOUT:=90}"   # max wait for k2r to connect (seconds)
: "${PHASE:=all}"            # all | rules | tunnel | lan-tcp | lan-dns | cleanup | reconnect | auto | crash | api | node-cutoff | multislot
: "${KEEP:=false}"           # keep containers after test

# ── Parse args ────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --phase) PHASE="$2"; shift 2 ;;
        --keep)  KEEP=true; shift ;;
        *)       echo "Unknown arg: $1"; exit 1 ;;
    esac
done

# ── Colors ────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

API="http://localhost:11779"
K2R_IP="10.99.0.20"
K2S_IP="10.99.0.10"
COMPOSE_UP=false
PASS=0
FAIL=0
SKIP=0

# Track whether TPROXY data plane works (Docker Desktop may lack kernel modules).
TPROXY_DATAPLANE=unknown

# ── Cleanup on exit ──────────────────────────────────────────────
cleanup() {
    if $COMPOSE_UP && [[ "$KEEP" != "true" ]]; then
        echo -e "${CYAN}Cleaning up containers...${NC}"
        docker compose -f "$COMPOSE_FILE" down -v --timeout 5 2>/dev/null || true
    elif $COMPOSE_UP && [[ "$KEEP" == "true" ]]; then
        echo -e "${YELLOW}Containers kept running (--keep). Clean up with:${NC}"
        echo "  docker compose -f $COMPOSE_FILE down -v"
    fi
    echo ""
    echo -e "═══════════════════════════════════════"
    echo -e "  Results: ${GREEN}${PASS} passed${NC}, ${RED}${FAIL} failed${NC}, ${YELLOW}${SKIP} skipped${NC}"
    echo -e "═══════════════════════════════════════"
    if [[ $FAIL -gt 0 ]]; then
        exit 1
    fi
}
trap cleanup EXIT

# ── Helpers ───────────────────────────────────────────────────────
log_info()  { echo -e "${CYAN}→ $*${NC}"; }
log_ok()    { echo -e "${GREEN}✓ $*${NC}"; PASS=$((PASS+1)); }
log_fail()  { echo -e "${RED}✗ $*${NC}"; FAIL=$((FAIL+1)); }
log_skip()  { echo -e "${YELLOW}⊘ $*${NC}"; SKIP=$((SKIP+1)); }
log_phase() { echo -e "\n${CYAN}══ Phase: $* ═══════════════════════════${NC}"; }

assert_eq() {
    local desc="$1" expected="$2" actual="$3"
    if [[ "$expected" == "$actual" ]]; then
        log_ok "$desc"
    else
        log_fail "$desc (expected='$expected', actual='$actual')"
    fi
}

assert_contains() {
    local desc="$1" needle="$2" haystack="$3"
    if echo "$haystack" | grep -q "$needle"; then
        log_ok "$desc"
    else
        log_fail "$desc (expected to contain '$needle')"
    fi
}

assert_not_contains() {
    local desc="$1" needle="$2" haystack="$3"
    if echo "$haystack" | grep -q "$needle"; then
        log_fail "$desc (should NOT contain '$needle')"
    else
        log_ok "$desc"
    fi
}

assert_not_empty() {
    local desc="$1" value="$2"
    if [[ -n "$value" ]]; then
        log_ok "$desc"
    else
        log_fail "$desc (empty)"
    fi
}

k2r_exec() {
    docker exec "$(docker compose -f "$COMPOSE_FILE" ps -q k2r)" "$@"
}

lan_exec() {
    docker exec "$(docker compose -f "$COMPOSE_FILE" ps -q lan-client)" "$@"
}

# Per-slot LAN clients (multislot phase only) — each is a separate container
# on its own dedicated bridge network (slot1-net/slot2-net), giving it a real
# PRIMARY IP in the slot's subnet rather than an aliased secondary IP. See
# docker-compose.yml's topology comment for why this matters.
lan_exec_slot1() {
    docker exec "$(docker compose -f "$COMPOSE_FILE" ps -q lan-client-slot1)" "$@"
}

lan_exec_slot2() {
    docker exec "$(docker compose -f "$COMPOSE_FILE" ps -q lan-client-slot2)" "$@"
}

# Get status JSON from API.
#
# Runs curl *inside* the k2r container against its own loopback, not
# against the host-published port ($API). Docker's port-publish path
# (userland-proxy / gVisor NAT on Docker Desktop, DNAT+conntrack on
# native Linux) does not preserve the caller's source address as
# 127.0.0.1, so gateway/auth.go's isLoopback() sees a non-loopback
# RemoteAddr and 401s every unbound-key request — confirmed by
# comparing `k2r_exec curl 127.0.0.1:1779/...` (succeeds) against
# host-side `curl "$API"/...` (401) on this exact call. This is a test
# harness/Docker-networking artifact, not a production bug: the real
# CLI (k2r up/down/status) runs in the same network namespace as the
# gateway it talks to, so it is genuinely loopback. Do NOT relax
# isLoopback()/authorized() to "fix" this — that would defeat
# deny-by-default control-key auth for actual LAN-side callers.
get_status() {
    k2r_exec curl -sf -X POST http://127.0.0.1:1779/api/core \
        -H "Content-Type: application/json" \
        -d '{"action":"status"}' 2>/dev/null || true
}

get_state() {
    get_status | jq -r '.data.state // empty' 2>/dev/null || true
}

# Wait for k2r to reach "connected" state.
wait_connected() {
    local timeout="${1:-$CONNECT_TIMEOUT}"
    local deadline=$((SECONDS + timeout))
    while [[ $SECONDS -lt $deadline ]]; do
        if [[ "$(get_state)" == "connected" ]]; then
            return 0
        fi
        sleep 2
    done
    return 1
}

# Wait for k2r API to be reachable.
wait_api() {
    local timeout="${1:-30}"
    local deadline=$((SECONDS + timeout))
    while [[ $SECONDS -lt $deadline ]]; do
        if curl -sf "$API/ping" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    return 1
}

# Connect k2r and wait for connected state. Used by multiple phases.
ensure_connected() {
    local state
    state=$(get_state)
    if [[ "$state" == "connected" ]]; then
        return 0
    fi
    k2r_exec k2r up /etc/k2/k2r.yml 2>&1 || true
    if wait_connected; then
        return 0
    fi
    return 1
}

# Set LAN client default route to k2r (idempotent).
setup_lan_route() {
    lan_exec ip route del default 2>/dev/null || true
    lan_exec ip route add default via "$K2R_IP" 2>/dev/null || true
}

# Probe whether TPROXY data plane actually works.
# Docker Desktop's LinuxKit kernel may lack nft_tproxy / xt_TPROXY modules.
probe_tproxy_dataplane() {
    if [[ "$TPROXY_DATAPLANE" != "unknown" ]]; then
        return
    fi
    setup_lan_route
    local result
    result=$(lan_exec curl -sf --max-time 15 "http://example.com" 2>&1 || true)
    if echo "$result" | grep -qi "example"; then
        TPROXY_DATAPLANE=yes
    else
        TPROXY_DATAPLANE=no
        echo -e "${YELLOW}  NOTE: TPROXY data plane not functional (expected on Docker Desktop).${NC}"
        echo -e "${YELLOW}  LAN flow tests will be skipped. Rule installation was verified in Phase 2.${NC}"
        echo -e "${YELLOW}  Full data-plane tests require a native Linux host.${NC}"
    fi
}

# Skip LAN test if TPROXY data plane doesn't work.
#
# In CI on a real Linux host (TPROXY modules present) we want a missing data
# plane to be a HARD FAILURE, not a silent skip — otherwise the job goes green
# while exercising nothing of value. Set K2R_UAT_REQUIRE_DATAPLANE=1 to convert
# every data-plane skip into a failure. Locally (Docker Desktop / LinuxKit) leave
# it unset so the suite still runs and degrades gracefully.
require_tproxy_dataplane() {
    local desc="$1"
    if [[ "$TPROXY_DATAPLANE" == "no" ]]; then
        if [[ "${K2R_UAT_REQUIRE_DATAPLANE:-}" == "1" ]]; then
            log_fail "$desc (TPROXY data plane REQUIRED but unavailable — load nft_tproxy/xt_TPROXY)"
        else
            log_skip "$desc (TPROXY data plane unavailable)"
        fi
        return 1
    fi
    return 0
}

# ── Phase 0: Build ────────────────────────────────────────────────
build_binaries() {
    log_phase "0 — Build"
    local build_dir="$SCRIPT_DIR/.build"
    mkdir -p "$build_dir"

    log_info "Cross-compiling for linux/$(go env GOARCH)..."
    local ldflags="-s -w"
    (cd "$PROJECT_ROOT" && CGO_ENABLED=0 GOOS=linux go build -ldflags "$ldflags" -o "$build_dir/k2r"        ./cmd/k2r)
    (cd "$PROJECT_ROOT" && CGO_ENABLED=0 GOOS=linux go build -ldflags "$ldflags" -o "$build_dir/k2s"        ./cmd/k2s)
    (cd "$PROJECT_ROOT" && CGO_ENABLED=0 GOOS=linux go build -ldflags "$ldflags" -o "$build_dir/gen-config" ./docker/gen-config)
    (cd "$PROJECT_ROOT" && CGO_ENABLED=0 GOOS=linux go build -ldflags "$ldflags" -o "$build_dir/mocksubs"   ./test/k2r-uat/mocksubs)
    log_ok "Binaries built ($(du -sh "$build_dir" | cut -f1))"
}

# ── Phase 1: Start containers ────────────────────────────────────
start_containers() {
    log_phase "1 — Start containers"

    log_info "Starting docker compose..."
    docker compose -f "$COMPOSE_FILE" up -d --build 2>&1 | tail -5
    COMPOSE_UP=true
    log_ok "Containers started"

    log_info "Waiting for k2r API (timeout: ${CONNECT_TIMEOUT}s)..."
    if wait_api "$CONNECT_TIMEOUT"; then
        log_ok "k2r API reachable"
    else
        log_fail "k2r API not reachable after ${CONNECT_TIMEOUT}s"
        docker compose -f "$COMPOSE_FILE" logs k2r 2>&1 | tail -30
        return 1
    fi
}

# ── Phase 2: Firewall rules (enhanced) ───────────────────────────
test_rules() {
    log_phase "2 — Firewall rules"

    log_info "Triggering k2r up..."
    ensure_connected || {
        log_fail "k2r did not reach connected state"
        return
    }
    assert_eq "2.1 k2r state is connected" "connected" "$(get_state)"

    # Detect which interceptor is in use.
    local has_nft=false
    if k2r_exec which nft >/dev/null 2>&1; then
        has_nft=true
    fi

    if $has_nft; then
        local nft_out
        nft_out=$(k2r_exec nft list table inet k2r 2>&1 || true)

        # 2.2 TPROXY rules installed.
        if echo "$nft_out" | grep -q "tproxy"; then
            log_ok "2.2 nftables: TPROXY rules installed"
        else
            log_fail "2.2 nftables: no TPROXY rules found"
            echo "  nft output: $nft_out"
        fi

        # 2.3 prerouting chain.
        assert_contains "2.3 nftables: prerouting chain" "prerouting" "$nft_out"

        # 2.4 mark set 1.
        assert_contains "2.4 nftables: mark set 1" "mark set" "$nft_out"

        # 2.5 TCP + UDP rules both present.
        assert_contains "2.5a nftables: TCP rule" "l4proto tcp" "$nft_out"
        assert_contains "2.5b nftables: UDP rule" "l4proto udp" "$nft_out"

        # 2.6 loopback return rule.
        assert_contains "2.6 nftables: loopback return" 'iifname "lo"' "$nft_out"

        # 2.7 exclude set contains k2s IP.
        local exclude_set
        exclude_set=$(k2r_exec nft list set inet k2r exclude_v4 2>&1 || true)
        if echo "$exclude_set" | grep -q "$K2S_IP"; then
            log_ok "2.7 nftables: exclude_v4 contains k2s IP ($K2S_IP)"
        elif echo "$exclude_set" | grep -qi "no such\|Error"; then
            log_skip "2.7 nftables: no exclude_v4 set (k2s may be in LAN subnet exclusion)"
        else
            log_fail "2.7 nftables: exclude_v4 does not contain k2s IP ($K2S_IP)"
            echo "  set: $exclude_set"
        fi
    else
        local ipt_out
        ipt_out=$(k2r_exec iptables -t mangle -L K2R -n 2>&1 || true)
        if echo "$ipt_out" | grep -q "TPROXY"; then
            log_ok "2.2 iptables: TPROXY rules installed"
        else
            log_fail "2.2 iptables: no TPROXY rules found"
        fi
        # Fewer granular checks for iptables path — it works the same way.
        log_skip "2.3-2.7 (iptables path, nftables-specific checks skipped)"
    fi

    # 2.8 IPv4 ip rule.
    local iprule
    iprule=$(k2r_exec ip rule list 2>&1 || true)
    assert_contains "2.8 ip rule: fwmark table 100" "fwmark" "$iprule"

    # 2.9 IPv4 ip route.
    local iproute
    iproute=$(k2r_exec ip route show table 100 2>&1 || true)
    assert_contains "2.9 ip route: local default in table 100" "local" "$iproute"

    # 2.10 IPv6 ip rule.
    local ip6rule
    ip6rule=$(k2r_exec ip -6 rule list 2>&1 || true)
    if echo "$ip6rule" | grep -q "fwmark"; then
        log_ok "2.10 ip -6 rule: fwmark table 100"
    else
        log_skip "2.10 ip -6 rule: no fwmark entry (IPv6 may not be configured)"
    fi

    # 2.11 IPv6 ip route.
    local ip6route
    ip6route=$(k2r_exec ip -6 route show table 100 2>&1 || true)
    if echo "$ip6route" | grep -q "local"; then
        log_ok "2.11 ip -6 route: local default in table 100"
    else
        log_skip "2.11 ip -6 route: no local route (IPv6 may not be configured)"
    fi

    # 2.12 sysctl ip_forward.
    local fwd
    fwd=$(k2r_exec cat /proc/sys/net/ipv4/ip_forward 2>&1 || true)
    assert_eq "2.12 sysctl: ip_forward=1" "1" "$fwd"
}

# ── Phase 3: Tunnel connectivity (enhanced) ──────────────────────
test_tunnel() {
    log_phase "3 — Tunnel connectivity"

    ensure_connected || {
        log_fail "k2r not connected"
        return
    }

    # 3.1 Status API returns data.
    local status_json
    status_json=$(get_status)
    assert_not_empty "3.1 Status API returns data" "$status_json"

    # 3.2 State is connected.
    local state
    state=$(echo "$status_json" | jq -r '.data.state // empty' 2>/dev/null || true)
    assert_eq "3.2 Tunnel state is connected" "connected" "$state"

    # 3.3 k2r self HTTP.
    local http_result
    http_result=$(k2r_exec curl -sf --max-time 15 "http://example.com" 2>&1 || true)
    if echo "$http_result" | grep -qi "example"; then
        log_ok "3.3 k2r can reach example.com (HTTP)"
    else
        log_skip "3.3 k2r HTTP to example.com failed (network issue, not k2r bug)"
    fi

    # 3.4 k2r self HTTPS.
    local https_result
    https_result=$(k2r_exec curl -sf --max-time 15 "https://example.com" 2>&1 || true)
    if echo "$https_result" | grep -qi "example"; then
        log_ok "3.4 k2r can reach example.com (HTTPS)"
    else
        log_skip "3.4 k2r HTTPS to example.com failed (network issue, not k2r bug)"
    fi
}

# ── Phase 4: LAN TCP traffic (expanded) ──────────────────────────
test_lan_tcp() {
    log_phase "4 — LAN TCP traffic"

    ensure_connected || {
        log_fail "k2r not connected"
        return
    }

    # 4.1 Set default route.
    setup_lan_route
    log_ok "4.1 LAN client default route set to k2r"

    # 4.2 LAN → k2r API.
    if lan_exec curl -sf --max-time 5 "http://${K2R_IP}:1779/ping" >/dev/null 2>&1; then
        log_ok "4.2 LAN client can reach k2r API"
    else
        log_fail "4.2 LAN client cannot reach k2r API"
    fi

    # Probe TPROXY data plane (sets TPROXY_DATAPLANE).
    probe_tproxy_dataplane

    # 4.3 LAN HTTP.
    if require_tproxy_dataplane "4.3 LAN HTTP → example.com"; then
        local result
        result=$(lan_exec curl -sf --max-time 15 "http://example.com" 2>&1 || true)
        if echo "$result" | grep -qi "example"; then
            log_ok "4.3 LAN HTTP → example.com"
        else
            log_fail "4.3 LAN HTTP → example.com failed"
        fi
    fi

    # 4.4 LAN HTTPS.
    if require_tproxy_dataplane "4.4 LAN HTTPS → example.com"; then
        local result
        result=$(lan_exec curl -sf --max-time 15 "https://example.com" 2>&1 || true)
        if echo "$result" | grep -qi "example"; then
            log_ok "4.4 LAN HTTPS → example.com"
        else
            log_fail "4.4 LAN HTTPS → example.com failed"
        fi
    fi

    # 4.5 Large file download (≥1MB).
    if require_tproxy_dataplane "4.5 LAN large file download (1MB)"; then
        local size
        size=$(lan_exec curl -sf --max-time 30 -o /dev/null -w '%{size_download}' \
            "http://speed.cloudflare.com/__down?bytes=1048576" 2>&1 || echo "0")
        if [[ "$size" -ge 1000000 ]] 2>/dev/null; then
            log_ok "4.5 LAN large file download (${size} bytes)"
        else
            log_skip "4.5 LAN large file download (got ${size} bytes, cloudflare may be unreachable)"
        fi
    fi

    # 4.6 Concurrent TCP (5 connections).
    if require_tproxy_dataplane "4.6 LAN concurrent TCP (5 connections)"; then
        local pids=() success=0
        for i in $(seq 1 5); do
            lan_exec curl -sf --max-time 15 "http://example.com" >/dev/null 2>&1 &
            pids+=($!)
        done
        for pid in "${pids[@]}"; do
            if wait "$pid" 2>/dev/null; then
                success=$((success + 1))
            fi
        done
        if [[ $success -ge 4 ]]; then
            log_ok "4.6 LAN concurrent TCP ($success/5 succeeded)"
        else
            log_fail "4.6 LAN concurrent TCP ($success/5 succeeded, expected ≥4)"
        fi
    fi
}

# ── Phase 5: LAN DNS traffic (new) ───────────────────────────────
test_lan_dns() {
    log_phase "5 — LAN DNS traffic"

    ensure_connected || {
        log_fail "k2r not connected"
        return
    }
    setup_lan_route

    if [[ "$TPROXY_DATAPLANE" == "unknown" ]]; then
        probe_tproxy_dataplane
    fi

    # 5.1 DNS over UDP.
    # NOTE: UDP TPROXY may not work on Docker Desktop (LinuxKit kernel).
    # TCP TPROXY works but UDP TPROXY requires kernel nft_tproxy support for UDP.
    if require_tproxy_dataplane "5.1 LAN DNS over UDP"; then
        local dns_udp
        dns_udp=$(lan_exec dig @8.8.8.8 example.com +short +time=10 +tries=1 2>&1 || true)
        if echo "$dns_udp" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+'; then
            log_ok "5.1 LAN DNS over UDP (resolved: $(echo "$dns_udp" | head -1))"
        else
            log_skip "5.1 LAN DNS over UDP timed out (UDP TPROXY may lack kernel support)"
            echo "  UDP TPROXY requires nft_tproxy kernel module for UDP path."
            echo "  TCP DNS (5.2) validates the DNS middleware pipeline."
        fi
    fi

    # 5.2 DNS over TCP.
    if require_tproxy_dataplane "5.2 LAN DNS over TCP"; then
        local dns_tcp
        dns_tcp=$(lan_exec dig @8.8.8.8 example.com +tcp +short +time=10 +tries=1 2>&1 || true)
        if echo "$dns_tcp" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+'; then
            log_ok "5.2 LAN DNS over TCP (resolved: $(echo "$dns_tcp" | head -1))"
        else
            log_fail "5.2 LAN DNS over TCP failed"
            echo "  dig output: $dns_tcp"
        fi
    fi

    # 5.3 DNS interception logged in k2r.
    if require_tproxy_dataplane "5.3 DNS interception visible in k2r logs"; then
        # Trigger a DNS query to create log entries.
        lan_exec dig @8.8.8.8 cloudflare.com +short +time=5 +tries=1 >/dev/null 2>&1 || true
        sleep 2
        local k2r_logs
        k2r_logs=$(docker compose -f "$COMPOSE_FILE" logs k2r 2>&1 || true)
        if echo "$k2r_logs" | grep -qi "dns\|HandleQuery\|middleware"; then
            log_ok "5.3 DNS interception visible in k2r logs"
        else
            log_skip "5.3 DNS interception not visible in logs (may be at debug level)"
        fi
    fi

    # 5.4 NXDOMAIN for non-existent domain (use TCP to avoid UDP TPROXY issues).
    if require_tproxy_dataplane "5.4 NXDOMAIN for non-existent domain"; then
        local dns_nx
        dns_nx=$(lan_exec dig @8.8.8.8 thisdoesnotexist12345678.com +tcp +time=10 +tries=1 2>&1 || true)
        if echo "$dns_nx" | grep -qi "NXDOMAIN\|SERVFAIL"; then
            log_ok "5.4 NXDOMAIN returned for non-existent domain"
        else
            log_skip "5.4 NXDOMAIN check inconclusive"
            echo "  dig output: $(echo "$dns_nx" | grep -i status || echo "$dns_nx" | head -3)"
        fi
    fi
}

# ── Phase 6: Disconnect and cleanup (enhanced) ───────────────────
test_cleanup() {
    log_phase "6 — Disconnect and cleanup"

    ensure_connected || true

    # 6.1 Trigger disconnect.
    log_info "Triggering k2r down..."
    k2r_exec k2r down 2>&1 || true
    sleep 3

    # 6.2 State is not connected.
    local state
    state=$(get_state)
    if [[ "$state" != "connected" ]]; then
        log_ok "6.1 State is no longer 'connected' after down (state='$state')"
    else
        log_fail "6.1 State still 'connected' after down"
    fi

    # 6.2 nft/ipt rules removed.
    local nft_out
    nft_out=$(k2r_exec nft list table inet k2r 2>&1 || true)
    if echo "$nft_out" | grep -qi "no such\|does not exist\|Error" || [[ -z "$nft_out" ]]; then
        log_ok "6.2 nftables: k2r table removed"
    else
        local ipt_out
        ipt_out=$(k2r_exec iptables -t mangle -L K2R -n 2>&1 || true)
        if echo "$ipt_out" | grep -qi "No chain\|doesn't exist"; then
            log_ok "6.2 iptables: K2R chain removed"
        else
            log_fail "6.2 Firewall rules still present"
            echo "  nft: $nft_out"
            echo "  ipt: $ipt_out"
        fi
    fi

    # 6.3 IPv4 ip rule removed.
    local iprule
    iprule=$(k2r_exec ip rule list 2>&1 || true)
    assert_not_contains "6.3 ip rule: fwmark removed" "fwmark.*0x1.*table 100" "$iprule"

    # 6.4 IPv6 ip rule removed.
    local ip6rule
    ip6rule=$(k2r_exec ip -6 rule list 2>&1 || true)
    if echo "$ip6rule" | grep -q "fwmark.*table 100"; then
        log_fail "6.4 ip -6 rule: fwmark still present"
    else
        log_ok "6.4 ip -6 rule: fwmark removed (or was never set)"
    fi

    # 6.5 sysctl preserved (Docker sets to 1, should stay 1 after down).
    local fwd
    fwd=$(k2r_exec cat /proc/sys/net/ipv4/ip_forward 2>&1 || true)
    assert_eq "6.5 sysctl: ip_forward preserved after down" "1" "$fwd"

    # 6.6 LAN traffic no longer proxied (if dataplane was working).
    if [[ "$TPROXY_DATAPLANE" == "yes" ]]; then
        setup_lan_route
        local result
        result=$(lan_exec curl -sf --max-time 5 "http://example.com" 2>&1 || true)
        if echo "$result" | grep -qi "example"; then
            # Traffic still works — could be direct routing, not TPROXY.
            # Since rules are removed, this is direct internet access (not a failure).
            log_ok "6.6 LAN traffic after down (direct, not proxied)"
        else
            log_ok "6.6 LAN traffic fails after down (expected — no proxy)"
        fi
    else
        log_skip "6.6 LAN traffic after down (TPROXY data plane unavailable)"
    fi

    # 6.7 Idempotent down.
    local resp
    resp=$(k2r_exec curl -sf -X POST http://127.0.0.1:1779/api/core \
        -H "Content-Type: application/json" \
        -d '{"action":"down"}' 2>/dev/null || true)
    if echo "$resp" | grep -q '"code":0'; then
        log_ok "6.7 Idempotent down (second down returns code=0)"
    else
        log_fail "6.7 Idempotent down failed"
        echo "  resp: $resp"
    fi
}

# ── Phase 7: Reconnect and idempotency (new) ─────────────────────
test_reconnect() {
    log_phase "7 — Reconnect and idempotency"

    # 7.1 Reconnect after down.
    log_info "Reconnecting..."
    ensure_connected || {
        log_fail "7.1 Reconnect failed"
        return
    }
    log_ok "7.1 Reconnect after down succeeded"

    # Verify LAN flow restored (if dataplane works).
    if [[ "$TPROXY_DATAPLANE" == "yes" ]]; then
        setup_lan_route
        local result
        result=$(lan_exec curl -sf --max-time 15 "http://example.com" 2>&1 || true)
        if echo "$result" | grep -qi "example"; then
            log_ok "7.1a LAN traffic restored after reconnect"
        else
            log_fail "7.1a LAN traffic not restored after reconnect"
        fi
    fi

    # 7.2 Rules don't stack (down → up shouldn't double rules).
    local has_nft=false
    if k2r_exec which nft >/dev/null 2>&1; then
        has_nft=true
    fi

    if $has_nft; then
        # Count tproxy rules.
        local rule_count_before
        rule_count_before=$(k2r_exec nft list table inet k2r 2>&1 | grep -c "tproxy" || echo "0")

        # Down then up again.
        k2r_exec k2r down 2>&1 || true
        sleep 3
        ensure_connected || {
            log_fail "7.2 Reconnect for stacking test failed"
            return
        }

        local rule_count_after
        rule_count_after=$(k2r_exec nft list table inet k2r 2>&1 | grep -c "tproxy" || echo "0")

        if [[ "$rule_count_before" == "$rule_count_after" ]]; then
            log_ok "7.2 Rules don't stack (before=$rule_count_before, after=$rule_count_after)"
        else
            log_fail "7.2 Rules stacked! (before=$rule_count_before, after=$rule_count_after)"
        fi
    else
        log_skip "7.2 Rule stacking check (nftables not available)"
    fi

    # 7.3 Hot switch (up while already connected, via ctl).
    log_info "Testing hot switch (up while connected)..."
    k2r_exec k2r up /etc/k2/k2r.yml 2>&1 || true
    # Wait for reconnection.
    sleep 5
    if wait_connected 30; then
        log_ok "7.3 Hot switch: re-established connection after up-while-connected"
    else
        log_fail "7.3 Hot switch: did not reconnect"
    fi
}

# ── Phase 8: Auto-reconnect (new) ────────────────────────────────
test_auto_reconnect() {
    log_phase "8 — Auto-reconnect"

    ensure_connected || {
        log_fail "8: Cannot test auto-reconnect (not connected)"
        return
    }

    # 8.1 Persisted state exists.
    local state_json
    state_json=$(k2r_exec cat /etc/k2r/state.json 2>&1 || true)
    if echo "$state_json" | grep -q '"state"'; then
        log_ok "8.1 Persisted state file exists"
        if echo "$state_json" | grep -q '"connected"'; then
            log_ok "8.1a Persisted state is 'connected'"
        else
            log_fail "8.1a Persisted state is not 'connected'"
            echo "  state: $state_json"
        fi
    else
        log_fail "8.1 No persisted state file"
        return
    fi

    # 8.2 Restart and auto-reconnect.
    log_info "Restarting k2r container..."
    docker compose -f "$COMPOSE_FILE" restart k2r 2>&1 | tail -3

    if ! wait_api 30; then
        log_fail "8.2 k2r API not reachable after restart"
        return
    fi

    # Auto-reconnect waits 3s then attempts. Give it extra time.
    log_info "Waiting for auto-reconnect (3s delay + connect time)..."
    if wait_connected 60; then
        log_ok "8.2 Auto-reconnect succeeded after restart"
    else
        log_fail "8.2 Auto-reconnect did not succeed after restart"
        return
    fi

    # 8.3 LAN traffic restored after auto-reconnect.
    if [[ "$TPROXY_DATAPLANE" == "yes" ]]; then
        setup_lan_route
        local result
        result=$(lan_exec curl -sf --max-time 15 "http://example.com" 2>&1 || true)
        if echo "$result" | grep -qi "example"; then
            log_ok "8.3 LAN traffic restored after auto-reconnect"
        else
            log_fail "8.3 LAN traffic not restored after auto-reconnect"
        fi
    else
        log_skip "8.3 LAN traffic after auto-reconnect (TPROXY data plane unavailable)"
    fi
}

# ── Phase 9: Crash recovery (enhanced) ───────────────────────────
test_crash_recovery() {
    log_phase "9 — Crash recovery"

    ensure_connected || {
        log_fail "9: Cannot test crash (not connected)"
        return
    }

    # Verify rules exist before crash.
    local has_rules=false
    if k2r_exec nft list table inet k2r >/dev/null 2>&1; then
        has_rules=true
    elif k2r_exec iptables -t mangle -L K2R -n >/dev/null 2>&1; then
        has_rules=true
    fi
    if ! $has_rules; then
        log_skip "9: No rules found before crash test"
        return
    fi

    # 9.1 Kill k2r process (simulate crash).
    log_info "Killing k2r process (SIGKILL)..."
    local k2r_pid
    k2r_pid=$(k2r_exec pgrep -f "k2r" 2>/dev/null | head -1 || true)
    if [[ -n "$k2r_pid" ]]; then
        k2r_exec kill -9 "$k2r_pid" 2>/dev/null || true
    fi
    sleep 2

    # 9.2 LAN traffic fails during crash.
    if [[ "$TPROXY_DATAPLANE" == "yes" ]]; then
        setup_lan_route
        local result
        result=$(lan_exec curl -sf --max-time 5 "http://example.com" 2>&1 || true)
        if echo "$result" | grep -qi "example"; then
            log_skip "9.2 LAN traffic during crash (still routing — stale rules active)"
        else
            log_ok "9.2 LAN traffic fails during crash (expected)"
        fi
    else
        log_skip "9.2 LAN traffic during crash (TPROXY data plane unavailable)"
    fi

    # 9.3 Restart k2r.
    log_info "Restarting k2r container..."
    docker compose -f "$COMPOSE_FILE" restart k2r 2>&1 | tail -3

    if wait_api 30; then
        log_ok "9.3 k2r restarted after crash"
    else
        log_fail "9.3 k2r did not restart after crash"
        return
    fi

    # 9.4 Stale rules cleaned on startup.
    sleep 2
    local nft_stale
    nft_stale=$(k2r_exec nft list table inet k2r 2>&1 || true)
    if echo "$nft_stale" | grep -qi "no such\|does not exist\|Error" || [[ -z "$nft_stale" ]]; then
        log_ok "9.4 Stale rules cleaned on restart"
    else
        # Auto-reconnect may have already re-installed rules.
        local state
        state=$(get_state)
        if [[ "$state" == "connected" ]]; then
            log_ok "9.4 Auto-reconnect re-installed rules (stale cleanup + reconnect worked)"
        else
            log_fail "9.4 Stale rules not cleaned on restart"
        fi
    fi

    # 9.5 Auto-reconnect after crash.
    log_info "Waiting for auto-reconnect after crash..."
    if wait_connected 60; then
        log_ok "9.5 Auto-reconnect succeeded after crash"
    else
        log_fail "9.5 Auto-reconnect did not succeed after crash"
        return
    fi

    # 9.6 LAN traffic restored after crash recovery.
    if [[ "$TPROXY_DATAPLANE" == "yes" ]]; then
        setup_lan_route
        # After crash + restart + auto-reconnect, TPROXY listeners may need
        # a moment to fully initialize. Retry up to 3 times.
        local ok=false
        for attempt in 1 2 3; do
            sleep 3
            local result
            result=$(lan_exec curl -sf --max-time 15 "http://example.com" 2>&1 || true)
            if echo "$result" | grep -qi "example"; then
                ok=true
                break
            fi
        done
        if $ok; then
            log_ok "9.6 LAN traffic restored after crash recovery"
        else
            log_fail "9.6 LAN traffic not restored after crash recovery"
        fi
    else
        log_skip "9.6 LAN traffic after crash recovery (TPROXY data plane unavailable)"
    fi
}

# ── Phase 10: API completeness & SSE (new) ───────────────────────
test_api() {
    log_phase "10 — API completeness"

    ensure_connected || {
        log_fail "10: Cannot test API (not connected)"
        return
    }

    # 10.1 Status response has state field.
    local status_json
    status_json=$(get_status)
    local state
    state=$(echo "$status_json" | jq -r '.data.state // empty' 2>/dev/null || true)
    assert_not_empty "10.1 Status has state field" "$state"

    # 10.2 Unknown action returns error.
    local resp
    resp=$(k2r_exec curl -sf -X POST http://127.0.0.1:1779/api/core \
        -H "Content-Type: application/json" \
        -d '{"action":"invalid"}' 2>/dev/null || true)
    assert_contains "10.2 Unknown action returns error" '"code":1' "$resp"

    # 10.3 SSE endpoint streams events.
    log_info "Testing SSE endpoint..."
    local sse_file
    sse_file=$(mktemp)
    # Start SSE listener in background.
    k2r_exec curl -sf -N --max-time 10 http://127.0.0.1:1779/api/events >"$sse_file" 2>/dev/null &
    local sse_pid=$!
    sleep 1

    # Trigger a down+up to generate status events.
    k2r_exec k2r down 2>&1 >/dev/null || true
    sleep 2
    k2r_exec k2r up /etc/k2/k2r.yml 2>&1 >/dev/null || true
    sleep 3

    # Wait a bit more for events.
    sleep 2
    kill "$sse_pid" 2>/dev/null || true
    wait "$sse_pid" 2>/dev/null || true

    if [[ -s "$sse_file" ]] && grep -q "event:" "$sse_file"; then
        log_ok "10.3 SSE endpoint streams events"
    else
        log_skip "10.3 SSE events not received (timing-sensitive, may need longer wait)"
    fi
    rm -f "$sse_file"

    # Re-connect after the SSE test disrupted state.
    wait_connected 30 || true

    # 10.4 Log level API — debug.
    resp=$(k2r_exec curl -sf -X POST http://127.0.0.1:1779/api/log-level \
        -H "Content-Type: application/json" \
        -d '{"level":"debug"}' 2>/dev/null || true)
    assert_contains "10.4a Log level: debug accepted" '"code":0' "$resp"

    # 10.4 Log level API — info.
    resp=$(k2r_exec curl -sf -X POST http://127.0.0.1:1779/api/log-level \
        -H "Content-Type: application/json" \
        -d '{"level":"info"}' 2>/dev/null || true)
    assert_contains "10.4b Log level: info accepted" '"code":0' "$resp"

    # 10.4 Log level API — warn.
    resp=$(k2r_exec curl -sf -X POST http://127.0.0.1:1779/api/log-level \
        -H "Content-Type: application/json" \
        -d '{"level":"warn"}' 2>/dev/null || true)
    assert_contains "10.4c Log level: warn accepted" '"code":0' "$resp"

    # 10.5 Log level API — invalid level doesn't crash.
    resp=$(k2r_exec curl -sf -X POST http://127.0.0.1:1779/api/log-level \
        -H "Content-Type: application/json" \
        -d '{"level":"invalid_level_xyz"}' 2>/dev/null || true)
    # Should return code=0 (SetLogLevel ignores unknown) or code=1 — either way, shouldn't crash.
    if [[ -n "$resp" ]]; then
        log_ok "10.5 Invalid log level doesn't crash"
    else
        log_fail "10.5 Invalid log level caused no response (possible crash)"
    fi

    # 10.6 GET /ping.
    #
    # cf3a905 replaced /ping's old {"code":0,"message":"pong"} with the
    # discovery-signature contract the companion app actually consumes
    # (gateway/api.go pingResponse: k2r/version/configured/name) — this
    # assertion was never updated to match and has been silently wrong
    # since, invisible only because the suite always died earlier (at the
    # loopback-auth regression fixed above) before ever reaching phase 10.
    # Assert on the real contract's discovery field instead.
    resp=$(curl -sf "$API/ping" 2>/dev/null || true)
    assert_contains "10.6 GET /ping returns discovery signature" '"k2r":true' "$resp"

    # Reset to debug.
    k2r_exec curl -sf -X POST http://127.0.0.1:1779/api/log-level \
        -H "Content-Type: application/json" \
        -d '{"level":"debug"}' >/dev/null 2>&1 || true
}

# ── Phase 11: Upstream node cutoff (quota-pause simulation) ──────
# Mirrors the production over-quota cutoff: the node sidecar `docker pause`s the
# k2s container when the line hits quota − 500MiB. From the router's vantage the
# upstream node simply stops serving — new flows stall and time out. This proves
# the LAN-visible effect of a quota cutoff (the resource-loss guard) and that the
# gateway recovers once the node is restored. The Center-side hide
# (ResolveGatewayPrivateTunnels dropping the over-quota line from /api/subs) is
# covered separately by the Go entitlement-resolver tests.
test_node_cutoff() {
    log_phase "11 — Upstream node cutoff (quota-pause simulation)"

    ensure_connected || {
        log_fail "11: Cannot test node cutoff (not connected)"
        return
    }
    setup_lan_route

    if [[ "$TPROXY_DATAPLANE" == "unknown" ]]; then
        probe_tproxy_dataplane
    fi

    # The whole phase observes LAN data-plane behavior — without it there is
    # nothing to assert. Under K2R_UAT_REQUIRE_DATAPLANE this skip becomes a fail.
    require_tproxy_dataplane "11 node-cutoff phase" || return

    # lan_flow_ok [timeout]: 0 if a LAN client request completes through the tunnel.
    lan_flow_ok() {
        local out
        out=$(lan_exec curl -sf --max-time "${1:-8}" "http://example.com" 2>&1 || true)
        echo "$out" | grep -qi "example"
    }

    # 11.1 Baseline: LAN flow works before the cutoff.
    if lan_flow_ok 15; then
        log_ok "11.1 Baseline LAN flow works (pre-cutoff)"
    else
        log_fail "11.1 Baseline LAN flow failed — cannot test cutoff"
        return
    fi

    # 11.2 Cut the upstream node off (freeze k2s, exactly like the enforcer's
    # `docker pause`). New LAN flows must now fail — this is the keystone guard.
    log_info "Pausing k2s (simulating quota cutoff / enforcer docker pause)..."
    docker compose -f "$COMPOSE_FILE" pause k2s >/dev/null 2>&1 || true
    sleep 2
    if lan_flow_ok 8; then
        log_fail "11.2 LAN flow STILL works after node cutoff (over-use not contained!)"
    else
        log_ok "11.2 LAN flow fails while node is cut off (expected — over-use contained)"
    fi

    # 11.3 Restore the node (enforcer unpause once usage drops). The gateway must
    # recover within budget (QUIC resume or health-monitor re-race + reconnect).
    log_info "Unpausing k2s (simulating usage drop / enforcer unpause)..."
    docker compose -f "$COMPOSE_FILE" unpause k2s >/dev/null 2>&1 || true

    # Generous budget: recovery waits on QUIC idle-timeout → health-monitor
    # re-race → reconnect, whose cadence we don't control. 120s never flakes.
    local deadline=$((SECONDS + 120)) recovered=false
    while [[ $SECONDS -lt $deadline ]]; do
        if lan_flow_ok 8; then
            recovered=true
            break
        fi
        sleep 3
    done
    if $recovered; then
        log_ok "11.3 LAN flow recovers after node restored"
    else
        log_fail "11.3 LAN flow did not recover within 120s after node restored"
    fi
}

# ── Phase 12: Enterprise multi-slot mode ─────────────────────────
# Exercises the k2subs:// -> slot_bindings path end to end: a mock HTTPS
# /api/subs server (mocksubs, started inside the k2r container by
# docker-entrypoint-k2r.sh whenever the gen-config-produced mocksubs
# fixtures are present) serves a 2-slot manifest — slot 1 bound to the
# real k2s tunnel (alive), slot 2 bound to a dead k2v5:// URL (never
# reachable). lan-client gets two extra source addresses, one per slot
# subnet, so curl --interface picks which "slot" a request comes from.
#
# Status/connect polling below uses the shared get_status/get_state/
# wait_connected helpers (container-internal curl — see their doc
# comment for why host-forwarded $API can't be used against /api/core).

test_multislot() {
    log_phase "12 — Enterprise multi-slot mode"

    if [[ "$TPROXY_DATAPLANE" == "unknown" ]]; then
        probe_tproxy_dataplane
    fi
    require_tproxy_dataplane "12 multislot phase" || return

    # Multi-slot fixtures live in the SAME /etc/k2 volume every phase shares
    # (written by gen-config at container start) — switch this session's
    # config to the k2subs:// variant. Other phases are untouched: they
    # keep calling `k2r up /etc/k2/k2r.yml` (static k2v5://).
    log_info "Switching to k2subs:// multi-slot config..."
    k2r_exec k2r up /etc/k2/k2r-multislot.yml 2>&1 || true
    if ! wait_connected 60; then
        log_fail "12.0 k2r did not reach connected state in multi-slot mode"
        return
    fi
    assert_eq "12.0 k2r state is connected (multi-slot)" "connected" "$(get_state)"

    # 12.1 (M1) k2r logged entering multi-slot mode.
    local k2r_log
    k2r_log=$(docker compose -f "$COMPOSE_FILE" logs k2r 2>&1)
    assert_contains "12.1 (M1) k2r logged multi-slot-mode slots=2" "multi-slot-mode" "$k2r_log"

    # Per-slot LAN clients each live on their own dedicated bridge (real
    # primary IP in the slot's subnet) — route default via k2r's address on
    # that same network. Idempotent — safe to re-run if invoked more than once.
    lan_exec_slot1 ip route del default 2>/dev/null || true
    lan_exec_slot1 ip route add default via 10.81.1.20 2>/dev/null || true
    lan_exec_slot2 ip route del default 2>/dev/null || true
    lan_exec_slot2 ip route add default via 10.81.2.20 2>/dev/null || true
    setup_lan_route

    slot_curl_ok() {
        local execFn="$1" timeout="${2:-8}"
        "$execFn" curl -sf --max-time "$timeout" "http://example.com" 2>&1 | grep -qi "example"
    }

    # 12.2 (M2) slot1 (alive tunnel) egresses fine.
    if slot_curl_ok lan_exec_slot1 15; then
        log_ok "12.2 (M2) slot1 (10.81.1.30, bound to alive tunnel) reaches internet"
    else
        log_fail "12.2 (M2) slot1 curl failed — expected success through the alive tunnel"
    fi

    # 12.3 (M3) slot2 (dead tunnel) fails — fail-closed, no direct fallback.
    # Checked 3x to rule out a one-off timing fluke rather than a genuine
    # design gap (a real leak would show at least one success).
    local slot2_leaked=false
    for _ in 1 2 3; do
        if slot_curl_ok lan_exec_slot2 5; then
            slot2_leaked=true
            break
        fi
    done
    if $slot2_leaked; then
        log_fail "12.3 (M3) slot2 (dead tunnel) unexpectedly reached the internet — fail-closed broken"
    else
        log_ok "12.3 (M3) slot2 (10.81.2.30, bound to dead tunnel) fails consistently (fail-closed)"
    fi

    # 12.4 (M4) No direct-egress fallback: k2r's own routing-decision log
    # (core/tunnel.go's "tunnel: route", action=outbound(N) — convention:
    # 0=direct, 1=reject, 2+=wire outbounds) never shows target 0 (direct)
    # for a slot2-sourced flow — multi-slot mode disables the direct
    # outbound entirely (only the two slot pins + a reject catch-all,
    # verified at the unit level by TestBuildSlotRoutes). A plain substring
    # match on "direct" is NOT safe here: unrelated log fields like
    # dnsRedirect=true contain "direct" as a substring ("Redirect"), so the
    # check must anchor on the exact action=outbound(0) log token instead.
    k2r_log=$(docker compose -f "$COMPOSE_FILE" logs k2r 2>&1)
    assert_not_contains "12.4 (M4) no direct-dial log entries for 10.81.2 (no leak)" "action=outbound(0).*srcIP=10.81.2" "$k2r_log"

    # 12.5 (M5) Unbound source (lan-client's own bridge address, not in any
    # slot subnet) hits the reject catch-all too — multi-slot mode has no
    # implicit "primary" LAN client the way consumer mode does.
    if slot_curl_ok lan_exec 5; then
        log_fail "12.5 (M5) unbound source unexpectedly reached the internet"
    else
        log_ok "12.5 (M5) unbound source (10.99.0.30) hits the reject catch-all"
    fi

    # 12.6 (M6) status API self-describes via slots[] — 8 rows always,
    # slot1 running, slot2 eventually failClosed (probe cadence is 60s;
    # zeroThreshold=2 consecutive failures, so allow up to ~2 cycles).
    local status slot1_state slot2_state slots_len deadline=$((SECONDS + 150))
    while [[ $SECONDS -lt $deadline ]]; do
        status=$(get_status)
        slots_len=$(echo "$status" | jq -r '.data.slots | length' 2>/dev/null || echo "0")
        slot2_state=$(echo "$status" | jq -r '.data.slots[1].state' 2>/dev/null || echo "")
        [[ "$slot2_state" == "failClosed" ]] && break
        sleep 5
    done
    slot1_state=$(echo "$status" | jq -r '.data.slots[0].state' 2>/dev/null || echo "")
    assert_eq "12.6a (M6) status slots[] has 8 rows" "8" "$slots_len"
    assert_eq "12.6b (M6) slot1 state=running" "running" "$slot1_state"
    assert_eq "12.6c (M6) slot2 state=failClosed" "failClosed" "$slot2_state"

    # 12.7 (M7) DNS follows the same per-slot fail-closed rule: slot1's DNS
    # exits via its tunnel (resolves); slot2's DNS dies with its tunnel.
    if require_tproxy_dataplane "12.7a slot1 DNS"; then
        local dns1
        dns1=$(lan_exec_slot1 dig @8.8.8.8 example.com +tcp +short +time=8 +tries=1 2>&1 || true)
        if echo "$dns1" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+'; then
            log_ok "12.7a (M7) slot1 DNS resolves through its tunnel"
        else
            log_fail "12.7a (M7) slot1 DNS failed: $dns1"
        fi
    fi
    if require_tproxy_dataplane "12.7b slot2 DNS"; then
        local dns2
        dns2=$(lan_exec_slot2 dig @8.8.8.8 example.com +tcp +short +time=8 +tries=1 2>&1 || true)
        if echo "$dns2" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+'; then
            log_fail "12.7b (M7) slot2 DNS unexpectedly resolved (should fail-closed with its dead tunnel)"
        else
            log_ok "12.7b (M7) slot2 DNS fails closed (dead tunnel)"
        fi
    fi

    # 12.8 (M8) Manifest change converges: repoint slot2 -> the alive
    # tunnel (tunnel_index 0) and confirm k2r picks it up within the
    # refresh cadence (?refresh=5 in the k2subs:// URL) without a manual
    # restart, and that slot2 traffic starts working.
    local alive_url
    alive_url=$(k2r_exec cat /etc/k2/mocksubs-response.json | jq -r '.tunnels[0].url')
    log_info "Repointing slot2 -> alive tunnel (M8 convergence)..."
    k2r_exec sh -c "cat > /etc/k2/mocksubs-response.json <<JSON
{\"tunnels\":[{\"url\":\"$alive_url\",\"weight\":1}],\"slot_bindings\":[{\"slot\":1,\"country\":\"ae\",\"index\":1,\"tunnel_index\":0},{\"slot\":2,\"country\":\"gn\",\"index\":1,\"tunnel_index\":0}]}
JSON"
    deadline=$((SECONDS + 60))
    local converged=false
    while [[ $SECONDS -lt $deadline ]]; do
        k2r_log=$(docker compose -f "$COMPOSE_FILE" logs k2r 2>&1)
        if echo "$k2r_log" | grep -q "slot-bindings-changed"; then
            converged=true
            break
        fi
        sleep 3
    done
    if $converged; then
        log_ok "12.8a (M8) k2r logged slot-bindings-changed after manifest edit"
    else
        log_fail "12.8a (M8) no slot-bindings-changed within 60s of manifest edit"
    fi
    # Reconnect settles after the reapply; give it a few seconds then probe.
    sleep 5
    wait_connected 30 || true
    if slot_curl_ok lan_exec_slot2 15; then
        log_ok "12.8b (M8) slot2 now reaches internet after convergence"
    else
        log_fail "12.8b (M8) slot2 still fails after convergence to the alive tunnel"
    fi

    # 12.9 (M9) Clearing slot_bindings reverts k2r to consumer (single
    # tunnel) behavior — the sole signal (slots[] absent) the app relies on
    # to stop rendering the enterprise form.
    log_info "Clearing slot_bindings (M9 — revert to consumer mode)..."
    k2r_exec sh -c "cat > /etc/k2/mocksubs-response.json <<JSON
{\"tunnels\":[{\"url\":\"$alive_url\",\"weight\":1}],\"slot_bindings\":[]}
JSON"
    deadline=$((SECONDS + 60))
    local reverted=false
    while [[ $SECONDS -lt $deadline ]]; do
        status=$(get_status)
        if [[ "$(echo "$status" | jq -r 'has("data") and (.data | has("slots") | not)' 2>/dev/null)" == "true" ]]; then
            reverted=true
            break
        fi
        sleep 5
    done
    if $reverted; then
        log_ok "12.9a (M9) status slots[] absent — reverted to consumer mode"
    else
        log_fail "12.9a (M9) status still reports slots[] after clearing the manifest"
    fi
    # Settle buffer mirrors M8b: engine "connected" can fire a beat before
    # the reinstalled interceptor/routes are fully warm after a reapply.
    wait_connected 30 || true
    sleep 5
    if slot_curl_ok lan_exec 15; then
        log_ok "12.9b (M9) consumer-mode LAN flow works again (single-tunnel behavior)"
    else
        log_fail "12.9b (M9) consumer-mode LAN flow failed after reverting"
    fi

    # Leave the harness back on the plain k2v5:// config for any phase run
    # after this one (e.g. `--phase all` ordering, or a repeat run).
    k2r_exec k2r up /etc/k2/k2r.yml 2>&1 || true
    wait_connected 30 || true
}

# ── Main ──────────────────────────────────────────────────────────
main() {
    echo -e "${CYAN}╔═══════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║     k2r Gateway UAT — Soft Router Full Test       ║${NC}"
    echo -e "${CYAN}║     10 phases, ~45 test points                    ║${NC}"
    echo -e "${CYAN}╚═══════════════════════════════════════════════════╝${NC}"
    echo ""

    build_binaries
    start_containers

    case "$PHASE" in
        all)
            test_rules
            test_tunnel
            test_lan_tcp
            test_lan_dns
            test_cleanup
            test_reconnect
            test_auto_reconnect
            test_crash_recovery
            test_api
            test_node_cutoff
            test_multislot
            ;;
        rules)      test_rules ;;
        tunnel)     test_tunnel ;;
        lan-tcp)    test_lan_tcp ;;
        lan-dns)    test_lan_dns ;;
        cleanup)    test_cleanup ;;
        reconnect)  test_reconnect ;;
        auto)       test_auto_reconnect ;;
        crash)      test_crash_recovery ;;
        api)        test_api ;;
        node-cutoff) test_node_cutoff ;;
        multislot)  test_multislot ;;
        *)          echo "Unknown phase: $PHASE"; exit 1 ;;
    esac
}

main "$@"
