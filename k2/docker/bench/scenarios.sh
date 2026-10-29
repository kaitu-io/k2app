#!/bin/bash
# Research-backed network condition scenarios using tc/netem.
# Applied on client eth0 outgoing — delays ACKs to test congestion control.
#
# References:
#   RFC 8867 - Test Cases for Evaluating Congestion Control
#   QUICbench (IMC 2022) - NUS SNL
#   GFW measurements - USENIX Security 2023/2025
#   WireGuard/OpenVPN benchmark - MDPI 2025
#   Censored Planet CCA study - FOCI 2025
#
# Tiers represent progressively degraded network conditions.
# netem delay is one-way (affects client→server ACK path).

apply_scenario() {
    local scenario="$1"
    case "$scenario" in

        # ================================================================
        # Baseline
        # ================================================================
        T0_ideal)
            echo "[T0] Ideal — no impairment (Docker bridge baseline)"
            ;;

        # ================================================================
        # Standard network tiers (RFC 8867 + QUICbench inspired)
        # ================================================================
        T1_good)
            echo "[T1] Good — 50ms RTT, 0.1% loss (same-continent, quality ISP)"
            tc qdisc add dev eth0 root netem delay 25ms 2ms loss 0.1%
            ;;

        T2_fair)
            echo "[T2] Fair — 100ms RTT, 1% loss (cross-continent, stable)"
            tc qdisc add dev eth0 root netem delay 50ms 8ms loss 1% 25%
            ;;

        T3_poor)
            echo "[T3] Poor — 150ms RTT, 2% loss, 50Mbps cap (mediocre ISP)"
            tc qdisc add dev eth0 root handle 1: netem delay 75ms 15ms loss 2% 25%
            tc qdisc add dev eth0 parent 1: handle 2: tbf rate 50mbit burst 128kb latency 300ms
            ;;

        T4_bad)
            echo "[T4] Bad — 200ms RTT, 5% loss, 20Mbps cap (congested long-haul)"
            tc qdisc add dev eth0 root handle 1: netem delay 100ms 25ms loss 5% 25%
            tc qdisc add dev eth0 parent 1: handle 2: tbf rate 20mbit burst 64kb latency 400ms
            ;;

        T5_hostile)
            echo "[T5] Hostile — 250ms RTT, 10% loss, 10Mbps cap (severely degraded)"
            tc qdisc add dev eth0 root handle 1: netem delay 125ms 40ms distribution normal loss 10% 25%
            tc qdisc add dev eth0 parent 1: handle 2: tbf rate 10mbit burst 32kb latency 500ms
            ;;

        # ================================================================
        # GFW-specific tiers (USENIX Security 2023/2025 measurements)
        # ================================================================
        T6_gfw_normal)
            echo "[T6] GFW Normal — 80ms RTT, 2% loss, 50Mbps (China cross-border baseline)"
            tc qdisc add dev eth0 root handle 1: netem delay 40ms 10ms loss 2% 25%
            tc qdisc add dev eth0 parent 1: handle 2: tbf rate 50mbit burst 128kb latency 300ms
            ;;

        T7_gfw_detected)
            echo "[T7] GFW Detected — 100ms RTT, 26% loss (probabilistic blocking per USENIX'23)"
            tc qdisc add dev eth0 root netem delay 50ms 20ms loss 26% 50%
            ;;

        T8_gfw_extreme)
            echo "[T8] GFW Extreme — 200ms RTT, 50% loss, 10Mbps (sensitive date/targeted)"
            tc qdisc add dev eth0 root handle 1: netem delay 100ms 50ms distribution normal loss 50% 25%
            tc qdisc add dev eth0 parent 1: handle 2: tbf rate 10mbit burst 32kb latency 500ms
            ;;

        # ================================================================
        # China ISP-specific scenarios
        # Research: APNIC 2020 (79% inbound CN < 1Mbps), USENIX Security 2023/2025,
        #           CN2 benchmarks (Simcentric), community loss measurements (6.9% US vs 0.04% domestic).
        # Gilbert-Elliott model: "loss X% Y%" — X=loss rate, Y=correlation (higher=burstier).
        # ================================================================
        C1_cmcc_south)
            echo "[C1] CMCC South off-peak — 120ms RTT, 5% loss (burst 33%), 30 Mbps (China Mobile → Japan, poor CMI)"
            tc qdisc add dev eth0 root handle 1: netem delay 60ms 30ms distribution normal loss 5% 33%
            tc qdisc add dev eth0 parent 1: handle 2: tbf rate 30mbit burst 64kb latency 300ms
            ;;

        C2_cmcc_peak)
            echo "[C2] CMCC South peak — 200ms RTT, 15% loss (burst 50%), 8 Mbps (晚高峰 8-11PM)"
            tc qdisc add dev eth0 root handle 1: netem delay 100ms 60ms distribution normal loss 15% 50%
            tc qdisc add dev eth0 parent 1: handle 2: tbf rate 8mbit burst 32kb latency 500ms
            ;;

        C3_ct_cn2)
            echo "[C3] CT CN2 GIA — 50ms RTT, 0.5% loss, 200 Mbps (China Telecom premium → Japan)"
            tc qdisc add dev eth0 root handle 1: netem delay 25ms 5ms loss 0.5%
            tc qdisc add dev eth0 parent 1: handle 2: tbf rate 200mbit burst 256kb latency 200ms
            ;;

        C4_ct_163_peak)
            echo "[C4] CT 163 peak — 100ms RTT, 8% loss (burst 33%), 30 Mbps (163 backbone 晚高峰)"
            tc qdisc add dev eth0 root handle 1: netem delay 50ms 20ms loss 8% 33%
            tc qdisc add dev eth0 parent 1: handle 2: tbf rate 30mbit burst 64kb latency 300ms
            ;;

        C5_cu_peak)
            echo "[C5] CU peak — 120ms RTT, 6% loss (burst 33%), 20 Mbps (China Unicom congested → Japan)"
            tc qdisc add dev eth0 root handle 1: netem delay 60ms 30ms distribution normal loss 6% 33%
            tc qdisc add dev eth0 parent 1: handle 2: tbf rate 20mbit burst 64kb latency 400ms
            ;;

        # ================================================================
        # Custom (pass NETEM_ARGS env var)
        # ================================================================
        custom)
            echo "[custom] $NETEM_ARGS"
            tc qdisc add dev eth0 root netem $NETEM_ARGS
            ;;

        *)
            echo "Unknown scenario: $scenario"
            echo "Available: T0_ideal T1_good T2_fair T3_poor T4_bad T5_hostile T6_gfw_normal T7_gfw_detected T8_gfw_extreme C1_cmcc_south C2_cmcc_peak C3_ct_cn2 C4_ct_163_peak C5_cu_peak custom"
            exit 1
            ;;
    esac
}
