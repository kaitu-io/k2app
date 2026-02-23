#!/bin/bash
# ============================================================================
# K2ARC vs Hysteria2 Brutal/BBR — Congestion Control Benchmark Suite
# ============================================================================
#
# Compares three QUIC congestion control algorithms under 14 network scenarios:
#
#   K2ARC          Adaptive rate-based CC (gradient ascent on utility function)
#   Hy2 Brutal      Fixed-rate CC (user-declared bandwidth, loss-compensating)
#   Hy2 BBR          Google BBR CC (adaptive, no bandwidth declaration)
#
# All traffic flows through Docker bridge + tc/netem for controlled conditions.
#
# Usage:
#   ./run.sh                                     # All 14 scenarios (T0-T8, C1-C5)
#   ./run.sh T0_ideal T7_gfw_detected            # Specific scenarios
#   ROUNDS=5 SIZE=100MB ./run.sh T0_ideal        # Override defaults
#   BRUTAL_MBPS=500 ./run.sh T0_ideal            # Lower Brutal declared BW
#
# Scenarios (based on RFC 8867, QUICbench IMC'22, USENIX Security 2023/2025):
#   T0_ideal        No impairment (Docker bridge baseline)
#   T1_good         50ms RTT, 0.1% loss (same-continent, quality ISP)
#   T2_fair         100ms RTT, 1% loss (cross-continent)
#   T3_poor         150ms RTT, 2% loss, 50Mbps cap
#   T4_bad          200ms RTT, 5% loss, 20Mbps cap
#   T5_hostile      250ms RTT, 10% loss, 10Mbps cap
#   T6_gfw_normal   80ms RTT, 2% loss, 50Mbps (China cross-border)
#   T7_gfw_detected 100ms RTT, 26% loss (GFW probabilistic blocking)
#   T8_gfw_extreme  200ms RTT, 50% loss, 10Mbps (sensitive date)
#
# China ISP scenarios (APNIC 2020, CN2 benchmarks, community measurements):
#   C1_cmcc_south   120ms RTT, 5% loss, 30Mbps (CMCC South → Japan)
#   C2_cmcc_peak    200ms RTT, 15% loss, 8Mbps (CMCC 晚高峰)
#   C3_ct_cn2       50ms RTT, 0.5% loss, 200Mbps (CT CN2 GIA premium)
#   C4_ct_163_peak  100ms RTT, 8% loss, 30Mbps (CT 163 backbone peak)
#   C5_cu_peak      120ms RTT, 6% loss, 20Mbps (CU congested → Japan)
#
# Environment variables:
#   ROUNDS       Number of rounds per protocol (default: 3)
#   SIZE         File size: 1MB, 10MB, 100MB, 2000MB (default: 2000MB)
#   TIMEOUT      Per-transfer timeout in seconds (default: 600)
#   BRUTAL_MBPS  Hy2 Brutal declared bandwidth in Mbps (default: 1000)
#   PROTOCOLS    Override protocol list (default: hy2_brutal:10003,hy2_bbr:10006,k2_pcc:10005)
#
# Output:
#   results/<scenario>.json   Per-scenario raw data
#   results/<scenario>.log    Per-scenario console log
#   results/report.csv        Aggregated CSV
#   stdout                    Formatted comparison report
# ============================================================================

set -e
cd "$(dirname "$0")"

ALL_SCENARIOS="T0_ideal T1_good T2_fair T3_poor T4_bad T5_hostile T6_gfw_normal T7_gfw_detected T8_gfw_extreme C1_cmcc_south C2_cmcc_peak C3_ct_cn2 C4_ct_163_peak C5_cu_peak"
SCENARIOS="${@:-$ALL_SCENARIOS}"

echo "=============================================="
echo "  K2ARC vs Hysteria2 Brutal/BBR Benchmark"
echo "=============================================="
echo "Scenarios:   $SCENARIOS"
echo "File size:   ${SIZE:-2000MB}"
echo "Rounds:      ${ROUNDS:-3}"
echo "Brutal BW:   ${BRUTAL_MBPS:-1000} Mbps"
echo ""

# Build once
echo "Building bench images..."
docker compose build

mkdir -p results

for scenario in $SCENARIOS; do
    echo ""
    echo "=============================================="
    echo "  Scenario: $scenario"
    echo "=============================================="
    SCENARIO=$scenario docker compose run --rm client 2>&1 | tee "results/${scenario}.log"
done

# Stop server
docker compose down

# Generate report
echo ""
echo "=============================================="
echo "  Generating report..."
echo "=============================================="
python3 report.py results/

echo ""
echo "All results in docker/bench/results/"
