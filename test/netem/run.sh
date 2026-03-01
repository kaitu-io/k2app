#!/usr/bin/env bash
# Network degradation test controller for k2.
# Runs k2s + k2 in Docker, injects tc netem, verifies health/fallback/recovery.
#
# Usage:
#   SCENARIO=cmcc-4g ./run.sh              # predefined scenario
#   LOSS=30 DELAY=150 PROTOCOL=udp ./run.sh # custom parameters
#   SCENARIO=all ./run.sh                   # run all scenarios sequentially
#
# Prerequisites: docker, jq, curl, go
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
source "$SCRIPT_DIR/scenarios.sh"

# ── Tunable parameters (override via env) ──────────────────────────
: "${LOSS:=0}"                # packet loss %
: "${DELAY:=0}"               # latency ms
: "${JITTER:=0}"              # jitter ms
: "${RATE:=0}"                # bandwidth limit kbit (0=unlimited)
: "${MTU_LIMIT:=0}"           # drop UDP packets larger than this (0=unlimited)
: "${CORRUPT:=0}"             # corruption %
: "${DUPLICATE:=0}"           # duplication %
: "${REORDER:=0}"             # reordering %
: "${PROTOCOL:=udp}"          # udp | tcp | all
: "${SOAK:=30}"               # degradation duration (seconds)
: "${RECOVERY:=30}"           # recovery observation (seconds)
: "${TRAFFIC_INTERVAL:=0.5}"  # traffic generation interval (seconds)
: "${SCENARIO:=}"             # predefined scenario name
: "${CONNECT_TIMEOUT:=60}"    # max wait for connection (seconds)

# ── Colors ──────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

API="http://localhost:11777"
SOCKS="localhost:11080"
TRAFFIC_PID=""
COMPOSE_UP=false
REPORT_DIR="$SCRIPT_DIR/.reports"

# ── Cleanup on exit ─────────────────────────────────────────────────
cleanup() {
    [[ -n "$TRAFFIC_PID" ]] && kill "$TRAFFIC_PID" 2>/dev/null || true
    if $COMPOSE_UP; then
        echo -e "${CYAN}Cleaning up containers...${NC}"
        docker compose -f "$COMPOSE_FILE" down -v --timeout 5 2>/dev/null || true
    fi
}
trap cleanup EXIT

# ── Helper functions ────────────────────────────────────────────────

log_info()  { echo -e "${CYAN}→ $*${NC}"; }
log_ok()    { echo -e "${GREEN}✓ $*${NC}"; }
log_warn()  { echo -e "${YELLOW}⚠ $*${NC}"; }
log_fail()  { echo -e "${RED}✗ $*${NC}"; }

# ── Host cross-compile ─────────────────────────────────────────────

build_binaries() {
    local build_dir="$SCRIPT_DIR/.build"
    mkdir -p "$build_dir"
    log_info "Cross-compiling for linux/$(go env GOARCH)..."
    local ldflags="-s -w"
    (cd "$PROJECT_ROOT" && CGO_ENABLED=0 GOOS=linux go build -ldflags "$ldflags" -o "$build_dir/k2"         ./cmd/k2)
    (cd "$PROJECT_ROOT" && CGO_ENABLED=0 GOOS=linux go build -ldflags "$ldflags" -o "$build_dir/k2s"        ./cmd/k2s)
    (cd "$PROJECT_ROOT" && CGO_ENABLED=0 GOOS=linux go build -ldflags "$ldflags" -o "$build_dir/gen-config"  ./docker/gen-config)
    log_ok "Binaries ready ($(du -sh "$build_dir" | cut -f1))"
}

wait_connected() {
    log_info "Waiting for k2 to connect (timeout: ${CONNECT_TIMEOUT}s)..."
    local deadline=$((SECONDS + CONNECT_TIMEOUT))

    # Phase 1: wait for daemon HTTP API
    while [[ $SECONDS -lt $deadline ]]; do
        if curl -sf "$API/ping" >/dev/null 2>&1; then
            break
        fi
        sleep 1
    done

    if ! curl -sf "$API/ping" >/dev/null 2>&1; then
        log_fail "k2 daemon not reachable after ${CONNECT_TIMEOUT}s"
        docker compose -f "$COMPOSE_FILE" logs k2 2>&1 | tail -20
        return 1
    fi

    # Phase 1.5: trigger connection (daemon doesn't auto-connect from config file)
    local container
    container=$(docker compose -f "$COMPOSE_FILE" ps -q k2)
    log_info "Triggering connection..."
    docker exec "$container" k2 ctl up /etc/k2/client.yaml >/dev/null 2>&1 &

    # Phase 2: wait for tunnel connected (POST required for /api/core)
    while [[ $SECONDS -lt $deadline ]]; do
        local state
        state=$(curl -sf -X POST "$API/api/core" \
            -H "Content-Type: application/json" \
            -d '{"action":"status"}' 2>/dev/null | jq -r '.data.state // empty' 2>/dev/null || true)
        if [[ "$state" == "connected" ]]; then
            log_ok "k2 connected"
            return 0
        fi
        sleep 1
    done

    log_fail "k2 not connected after ${CONNECT_TIMEOUT}s"
    local last_state
    last_state=$(curl -sf -X POST "$API/api/core" \
        -H "Content-Type: application/json" \
        -d '{"action":"status"}' 2>/dev/null | jq -r '.data // empty' 2>/dev/null || echo "unreachable")
    echo "  Last status: $last_state"
    docker compose -f "$COMPOSE_FILE" logs k2 2>&1 | tail -20
    return 1
}

start_traffic() {
    log_info "Starting traffic generator (interval: ${TRAFFIC_INTERVAL}s)"
    (
        while true; do
            curl -sf --socks5-hostname "$SOCKS" --max-time 5 http://example.com >/dev/null 2>&1 || true
            sleep "$TRAFFIC_INTERVAL"
        done
    ) &
    TRAFFIC_PID=$!
}

stop_traffic() {
    if [[ -n "$TRAFFIC_PID" ]]; then
        kill "$TRAFFIC_PID" 2>/dev/null || true
        TRAFFIC_PID=""
    fi
}

# ── netem injection ─────────────────────────────────────────────────

build_netem_params() {
    local params=""
    [[ $LOSS -gt 0 ]]      && params+=" loss ${LOSS}%"
    [[ $DELAY -gt 0 ]]     && params+=" delay ${DELAY}ms"
    [[ $JITTER -gt 0 ]]    && params+=" ${JITTER}ms"  # jitter is a sub-param of delay
    [[ $RATE -gt 0 ]]      && params+=" rate ${RATE}kbit"
    [[ $CORRUPT -gt 0 ]]   && params+=" corrupt ${CORRUPT}%"
    [[ $DUPLICATE -gt 0 ]] && params+=" duplicate ${DUPLICATE}%"
    [[ $REORDER -gt 0 ]]   && params+=" reorder ${REORDER}%"
    echo "$params"
}

inject_netem() {
    local container
    container=$(docker compose -f "$COMPOSE_FILE" ps -q k2)
    local params
    params=$(build_netem_params)

    if [[ -z "$params" && $MTU_LIMIT -eq 0 ]]; then
        log_warn "No degradation parameters set — skipping netem injection"
        return 0
    fi

    if [[ -n "$params" ]]; then
        case "$PROTOCOL" in
            udp)
                docker exec "$container" bash -c "
                    tc qdisc add dev eth0 root handle 1: prio bands 3 priomap 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0
                    tc qdisc add dev eth0 parent 1:3 handle 30: netem $params
                    tc filter add dev eth0 protocol ip parent 1:0 prio 3 u32 match ip protocol 17 0xff flowid 1:3
                "
                ;;
            tcp)
                docker exec "$container" bash -c "
                    tc qdisc add dev eth0 root handle 1: prio bands 3 priomap 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0
                    tc qdisc add dev eth0 parent 1:3 handle 30: netem $params
                    tc filter add dev eth0 protocol ip parent 1:0 prio 3 u32 match ip protocol 6 0xff flowid 1:3
                "
                ;;
            all)
                docker exec "$container" tc qdisc add dev eth0 root netem $params
                ;;
        esac
    fi

    # MTU limit: drop large UDP packets via iptables
    if [[ $MTU_LIMIT -gt 0 ]]; then
        docker exec "$container" iptables -A OUTPUT -p udp --dport 443 -m length --length "${MTU_LIMIT}:" -j DROP
    fi

    log_info "Degradation injected: loss=${LOSS}% delay=${DELAY}ms jitter=${JITTER}ms rate=${RATE}kbit mtu=${MTU_LIMIT} protocol=${PROTOCOL}"
}

remove_netem() {
    local container
    container=$(docker compose -f "$COMPOSE_FILE" ps -q k2)
    docker exec "$container" tc qdisc del dev eth0 root 2>/dev/null || true
    docker exec "$container" iptables -F OUTPUT 2>/dev/null || true
    log_info "Degradation removed"
}

# ── Stats checking ──────────────────────────────────────────────────

get_stats() {
    curl -sf "$API/api/stats" 2>/dev/null || echo '{"data":[]}'
}

check_baseline() {
    local stats="$1"
    local count
    count=$(echo "$stats" | jq '.data | length' 2>/dev/null || echo 0)
    if [[ $count -eq 0 ]]; then
        log_warn "No stats available yet (daemon may still be initializing)"
        return 0
    fi
    local health
    health=$(echo "$stats" | jq -r '.data[-1].health // "unknown"' 2>/dev/null)
    if [[ "$health" == "healthy" ]]; then
        log_ok "Baseline: healthy"
    else
        log_warn "Baseline health: $health (may still be stabilizing)"
    fi
}

check_degraded() {
    local stats="$1"
    local count
    count=$(echo "$stats" | jq '.data | length' 2>/dev/null || echo 0)
    if [[ $count -lt 5 ]]; then
        log_warn "Too few stats samples ($count)"
        return 0
    fi

    # Check last 10 snapshots for signs of degradation
    local degraded
    degraded=$(echo "$stats" | jq '
        [.data[-10:][]] |
        map(select(
            .health != "healthy" or
            .fallback == true or
            .dg_hops > 0 or
            .dg_loss_rate > 0.1
        )) | length
    ' 2>/dev/null || echo 0)

    if [[ $degraded -gt 0 ]]; then
        local last
        last=$(echo "$stats" | jq '.data[-1] | {health, fallback, transport, dg_loss_rate, dg_hops, dg_timeouts}' 2>/dev/null)
        log_ok "Degradation detected ($degraded/10 samples affected)"
        echo "  Latest: $last"
    else
        log_warn "No degradation detected in stats (network may be too resilient for these parameters)"
        local last
        last=$(echo "$stats" | jq '.data[-1] | {health, fallback, transport, dg_loss_rate, dg_hops, loss_rate}' 2>/dev/null)
        echo "  Latest: $last"
    fi
}

check_recovered() {
    local stats="$1"
    local health
    health=$(echo "$stats" | jq -r '.data[-1].health // "unknown"' 2>/dev/null)
    local fallback
    fallback=$(echo "$stats" | jq -r '.data[-1].fallback // false' 2>/dev/null)
    local transport
    transport=$(echo "$stats" | jq -r '.data[-1].transport // "unknown"' 2>/dev/null)

    if [[ "$health" == "critical" ]]; then
        log_fail "Still critical after recovery period"
        echo "  health=$health fallback=$fallback transport=$transport"
        return 1
    fi

    if [[ "$health" == "healthy" && "$fallback" != "true" ]]; then
        log_ok "Fully recovered: health=$health transport=$transport"
    elif [[ "$fallback" == "true" ]]; then
        # TCP-WS fallback is acceptable if QUIC backoff hasn't expired yet
        log_ok "Recovered (TCP-WS fallback active — QUIC backoff may still be running)"
        echo "  health=$health fallback=$fallback transport=$transport"
    else
        log_ok "Recovered: health=$health fallback=$fallback transport=$transport"
    fi
}

# ── Report generation ─────────────────────────────────────────────

generate_scenario_report() {
    local name="$1" baseline="$2" degraded="$3" recovered="$4"

    jq -n \
      --arg name "$name" \
      --argjson loss "$LOSS" --argjson delay "$DELAY" \
      --argjson jitter "$JITTER" --argjson rate "$RATE" \
      --argjson mtu "$MTU_LIMIT" --arg protocol "$PROTOCOL" \
      --argjson soak "$SOAK" --argjson recovery "$RECOVERY" \
      --argjson baseline "$baseline" \
      --argjson degraded "$degraded" \
      --argjson recovered "$recovered" \
      '{
        scenario: $name,
        params: {loss_pct: $loss, delay_ms: $delay, jitter_ms: $jitter,
                 rate_kbit: $rate, mtu_limit: $mtu, protocol: $protocol,
                 soak_s: $soak, recovery_s: $recovery},
        baseline: ($baseline.data[-1:][0] // {}),
        degradation: {
          samples_total: ($degraded.data | length),
          last_10: [($degraded.data[-10:][]) | {health, fallback, transport,
                    loss_rate, dg_loss_rate, dg_hops, dg_timeouts, rtt_ms, cwnd}],
          peak_loss: ([($degraded.data[-10:][].loss_rate)] | max),
          peak_dg_loss: ([($degraded.data[-10:][].dg_loss_rate)] | max),
          max_rtt_ms: ([($degraded.data[-10:][].rtt_ms)] | max),
          max_hops: ([($degraded.data[-10:][].dg_hops)] | max),
          fallback_triggered: (any($degraded.data[-10:][]; .fallback == true)),
          health_states: [($degraded.data[-10:][].health)] | unique
        },
        recovery: {
          final: ($recovered.data[-1:][0] // {} | {health, fallback, transport,
                  loss_rate, dg_loss_rate, rtt_ms, cwnd}),
          quic_restored: (($recovered.data[-1:][0] // {}) |
                          .transport == "quic" and (.fallback | not))
        }
      }' > "$REPORT_DIR/${name}.json"

    log_info "Report: $REPORT_DIR/${name}.json"
}

generate_summary_report() {
    log_info "Generating summary report..."

    {
        echo "# k2 netem Test Report"
        echo ""
        echo "Date: $(date -u '+%Y-%m-%d %H:%M UTC')"
        echo "Commit: $(git -C "$PROJECT_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
        echo ""
        echo "## Scenario Matrix"
        echo ""
        echo "| Scenario | Params | Health States | Peak Loss | Peak RTT | Hops | Fallback | Recovery |"
        echo "|----------|--------|---------------|-----------|----------|------|----------|----------|"

        for f in "$REPORT_DIR"/*.json; do
            [[ -f "$f" ]] || continue
            jq -r '
              "| \(.scenario) " +
              "| L\(.params.loss_pct)% D\(.params.delay_ms)ms R\(.params.rate_kbit)k " +
              "| \(.degradation.health_states | join(",")) " +
              "| \((.degradation.peak_loss * 100 | floor))% / \((.degradation.peak_dg_loss * 100 | floor))% " +
              "| \((.degradation.max_rtt_ms | floor))ms " +
              "| \(.degradation.max_hops) " +
              "| \(if .degradation.fallback_triggered then "YES" else "no" end) " +
              "| \(if .recovery.quic_restored then "QUIC ✓" elif .recovery.final.health == "healthy" then "healthy" else .recovery.final.health end) |"
            ' "$f"
        done

        echo ""
        echo "## Optimization Signals"
        echo ""
        echo "| Signal | Scenarios | Recommendation |"
        echo "|--------|-----------|----------------|"

        # Hop ineffective: hops attempted but fallback still triggered
        for f in "$REPORT_DIR"/*.json; do
            [[ -f "$f" ]] || continue
            jq -r 'select(.degradation.max_hops > 0 and .degradation.fallback_triggered) |
              "| Hop ineffective | \(.scenario) | Hops=\(.degradation.max_hops) but fallback still triggered |"
            ' "$f"
        done

        # QUIC not restored after recovery
        for f in "$REPORT_DIR"/*.json; do
            [[ -f "$f" ]] || continue
            jq -r 'select(.recovery.quic_restored == false and .recovery.final.health != "critical") |
              "| QUIC not restored | \(.scenario) | Final: \(.recovery.final.transport) fallback=\(.recovery.final.fallback) |"
            ' "$f"
        done

        # No degradation detected
        for f in "$REPORT_DIR"/*.json; do
            [[ -f "$f" ]] || continue
            jq -r 'select(.degradation.health_states == ["healthy"]) |
              "| No degradation detected | \(.scenario) | L\(.params.loss_pct)% — thresholds may be too high |"
            ' "$f"
        done

        echo ""
        echo "## Per-Scenario Details"
        echo ""
        for f in "$REPORT_DIR"/*.json; do
            [[ -f "$f" ]] || continue
            local sname
            sname=$(jq -r '.scenario' "$f")
            echo "### $sname"
            echo '```json'
            jq '.degradation.last_10' "$f"
            echo '```'
            echo ""
        done

    } > "$REPORT_DIR/report.md"

    log_ok "Summary: $REPORT_DIR/report.md"
}

# ── Main test flow ──────────────────────────────────────────────────

run_single_test() {
    local scenario_name="${1:-custom}"
    mkdir -p "$REPORT_DIR"

    echo ""
    echo "═══════════════════════════════════════════════════════════════"
    echo "  Scenario: $scenario_name"
    echo "  Params: loss=${LOSS}% delay=${DELAY}ms jitter=${JITTER}ms rate=${RATE}kbit"
    echo "          mtu=${MTU_LIMIT} protocol=${PROTOCOL} corrupt=${CORRUPT}%"
    echo "          soak=${SOAK}s recovery=${RECOVERY}s"
    echo "═══════════════════════════════════════════════════════════════"
    echo ""

    # Baseline
    log_info "Checking baseline..."
    sleep 3  # let stats accumulate
    local baseline_stats
    baseline_stats=$(get_stats)
    check_baseline "$baseline_stats"

    # Start traffic
    start_traffic

    # Inject degradation
    inject_netem

    # Soak
    log_info "Soaking for ${SOAK}s..."
    sleep "$SOAK"

    # Check degradation
    log_info "Checking degradation..."
    local degraded_stats
    degraded_stats=$(get_stats)
    check_degraded "$degraded_stats"

    # Remove degradation
    remove_netem

    # Recovery
    log_info "Waiting ${RECOVERY}s for recovery..."
    sleep "$RECOVERY"

    # Check recovery
    log_info "Checking recovery..."
    local recovered_stats
    recovered_stats=$(get_stats)
    check_recovered "$recovered_stats"

    # Stop traffic
    stop_traffic

    # Generate scenario report
    generate_scenario_report "$scenario_name" "$baseline_stats" "$degraded_stats" "$recovered_stats"

    echo ""
    log_ok "Scenario '$scenario_name' completed"
    echo ""
}

main() {
    # Check prerequisites
    for cmd in docker jq curl go; do
        if ! command -v "$cmd" &>/dev/null; then
            log_fail "Required tool not found: $cmd"
            exit 1
        fi
    done

    # Host cross-compile (fast: uses local Go cache)
    build_binaries

    # Handle SCENARIO=all
    if [[ "${SCENARIO:-}" == "all" ]]; then
        log_info "Running all scenarios sequentially"
        echo ""

        # Start containers (images just COPY pre-built binaries)
        log_info "Starting containers..."
        docker compose -f "$COMPOSE_FILE" up -d --build
        COMPOSE_UP=true
        wait_connected

        local failed=0
        for s in $(all_scenarios); do
            load_scenario "$s"
            if ! run_single_test "$s"; then
                ((failed++))
            fi
            # Inter-scenario cooldown
            log_info "Cooldown 10s before next scenario..."
            sleep 10
        done

        generate_summary_report

        echo ""
        echo "═══════════════════════════════════════════════════════════════"
        if [[ $failed -eq 0 ]]; then
            log_ok "All scenarios passed"
        else
            log_fail "$failed scenario(s) had issues"
        fi
        echo "═══════════════════════════════════════════════════════════════"
        exit $failed
    fi

    # Load named scenario (if set)
    if [[ -n "${SCENARIO:-}" ]]; then
        load_scenario "$SCENARIO" || exit 1
    fi

    # Start containers (images just COPY pre-built binaries)
    log_info "Starting containers..."
    docker compose -f "$COMPOSE_FILE" up -d --build
    COMPOSE_UP=true

    # Wait for connection
    wait_connected

    # Run test
    run_single_test "${SCENARIO:-custom}"

    # Generate summary (single scenario)
    generate_summary_report
}

main "$@"
