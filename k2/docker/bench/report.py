#!/usr/bin/env python3
"""Aggregate benchmark results into a performance report.

Reads JSON result files from each scenario and produces:
1. Summary table (per-scenario, per-protocol averages)
2. Three-way comparison (K2ARC vs Hy2 Brutal vs Hy2 BBR)
3. CSV export for plotting

Usage:
  python3 report.py results/
"""
import json
import os
import sys
from collections import defaultdict

SCENARIO_INFO = {
    "T0_ideal":        {"rtt": "<1ms",  "loss": "0%",   "bw": "unlimited", "desc": "Docker bridge baseline"},
    "T1_good":         {"rtt": "50ms",  "loss": "0.1%", "bw": "unlimited", "desc": "Same-continent, quality ISP"},
    "T2_fair":         {"rtt": "100ms", "loss": "1%",   "bw": "unlimited", "desc": "Cross-continent, stable"},
    "T3_poor":         {"rtt": "150ms", "loss": "2%",   "bw": "50 Mbps",  "desc": "Mediocre ISP"},
    "T4_bad":          {"rtt": "200ms", "loss": "5%",   "bw": "20 Mbps",  "desc": "Congested long-haul"},
    "T5_hostile":      {"rtt": "250ms", "loss": "10%",  "bw": "10 Mbps",  "desc": "Severely degraded"},
    "T6_gfw_normal":   {"rtt": "80ms",  "loss": "2%",   "bw": "50 Mbps",  "desc": "China cross-border baseline"},
    "T7_gfw_detected": {"rtt": "100ms", "loss": "26%",  "bw": "unlimited", "desc": "GFW probabilistic blocking"},
    "T8_gfw_extreme":  {"rtt": "200ms", "loss": "50%",  "bw": "10 Mbps",  "desc": "GFW extreme / sensitive date"},
    # China ISP-specific scenarios (APNIC 2020, CN2 benchmarks, community measurements)
    "C1_cmcc_south":   {"rtt": "120ms", "loss": "5%",   "bw": "30 Mbps",  "desc": "CMCC South off-peak → Japan"},
    "C2_cmcc_peak":    {"rtt": "200ms", "loss": "15%",  "bw": "8 Mbps",   "desc": "CMCC South 晚高峰 (8-11PM)"},
    "C3_ct_cn2":       {"rtt": "50ms",  "loss": "0.5%", "bw": "200 Mbps", "desc": "CT CN2 GIA premium → Japan"},
    "C4_ct_163_peak":  {"rtt": "100ms", "loss": "8%",   "bw": "30 Mbps",  "desc": "CT 163 backbone 晚高峰"},
    "C5_cu_peak":      {"rtt": "120ms", "loss": "6%",   "bw": "20 Mbps",  "desc": "CU congested → Japan"},
}

# Protocol display names and matching keys
PROTO_ORDER = ["k2_pcc", "hy2_brutal", "hy2_bbr"]
PROTO_LABELS = {"k2_pcc": "K2ARC", "hy2_brutal": "Hy2 Brutal", "hy2_bbr": "Hy2 BBR"}


def load_results(results_dir):
    all_data = {}
    for fname in sorted(os.listdir(results_dir)):
        if not fname.endswith(".json"):
            continue
        scenario = fname.replace(".json", "")
        with open(os.path.join(results_dir, fname)) as f:
            all_data[scenario] = json.load(f)
    return all_data


def compute_averages(results):
    by_proto = defaultdict(list)
    for r in results:
        if r.get("error"):
            continue
        by_proto[r["protocol"]].append(r)

    avgs = {}
    for proto, runs in by_proto.items():
        n = len(runs)
        if n == 0:
            continue
        avg_mbps = sum(r["avg_mbps"] for r in runs) / n
        avg_time = sum(r["total_time"] for r in runs) / n
        avg_bytes = sum(r["total_bytes"] for r in runs) / n
        timeouts = sum(1 for r in runs if r.get("timed_out"))

        all_speeds = []
        for r in runs:
            all_speeds.extend(s["kbps"] for s in r.get("speeds", []))
        if all_speeds:
            mean_kbps = sum(all_speeds) / len(all_speeds)
            variance = sum((s - mean_kbps) ** 2 for s in all_speeds) / len(all_speeds)
            cv = (variance ** 0.5) / mean_kbps if mean_kbps > 0 else 0
        else:
            cv = 0

        avgs[proto] = {
            "avg_mbps": round(avg_mbps, 2),
            "avg_time": round(avg_time, 1),
            "avg_bytes": int(avg_bytes),
            "rounds": n,
            "timeouts": timeouts,
            "speed_cv": round(cv, 3),
        }
    return avgs


def print_report(all_data):
    print()
    print("=" * 110)
    print("  K2ARC vs Hysteria2 Brutal vs Hysteria2 BBR — Performance Report")
    print("=" * 110)
    print()

    # === Test Matrix ===
    print("Test Matrix (RFC 8867, QUICbench IMC'22, USENIX Security 2023/2025)")
    print("-" * 95)
    print(f"{'Scenario':<18} {'RTT':>6} {'Loss':>6} {'BW Cap':>10} {'Description':<40}")
    print("-" * 95)
    for sc, info in SCENARIO_INFO.items():
        print(f"{sc:<18} {info['rtt']:>6} {info['loss']:>6} {info['bw']:>10} {info['desc']:<40}")
    print()

    # === Per-Scenario Results ===
    print("=" * 110)
    print("  Results by Scenario")
    print("=" * 110)
    print()
    print(f"{'Scenario':<18} {'Protocol':<16} {'Avg Mbps':>10} {'Time':>8} {'CV':>6} {'Rounds':>7} {'Timeout':>8}")
    print("-" * 80)

    scenario_avgs = {}
    for scenario in SCENARIO_INFO:
        if scenario not in all_data:
            print(f"{scenario:<18} {'(not run)':<16}")
            continue
        avgs = compute_averages(all_data[scenario])
        scenario_avgs[scenario] = avgs
        first = True
        for proto in PROTO_ORDER:
            if proto not in avgs:
                continue
            a = avgs[proto]
            sc_label = scenario if first else ""
            timeout_str = f"{a['timeouts']}/{a['rounds']}" if a['timeouts'] > 0 else "-"
            label = PROTO_LABELS.get(proto, proto)
            print(f"{sc_label:<18} {label:<16} {a['avg_mbps']:>9.2f} {a['avg_time']:>7.1f}s {a['speed_cv']:>5.3f} {a['rounds']:>6}  {timeout_str:>7}")
            first = False

    print()

    # === Three-Way Comparison ===
    print("=" * 110)
    print("  Three-Way Comparison")
    print("=" * 110)
    print()
    print(f"{'Scenario':<18} {'K2ARC':>10} {'Brutal':>10} {'BBR':>10} {'Best':>12} {'k2 vs Brutal':>14} {'k2 vs BBR':>12}")
    print("-" * 100)

    for scenario in SCENARIO_INFO:
        avgs = scenario_avgs.get(scenario, {})
        k2  = avgs.get("k2_pcc")
        brt = avgs.get("hy2_brutal")
        bbr = avgs.get("hy2_bbr")

        if k2 is None and brt is None and bbr is None:
            print(f"{scenario:<18} {'(not run)':>10}")
            continue

        k2_mbps  = k2["avg_mbps"]  if k2  else 0
        brt_mbps = brt["avg_mbps"] if brt else 0
        bbr_mbps = bbr["avg_mbps"] if bbr else 0

        candidates = {}
        if k2:  candidates["K2ARC"] = k2_mbps
        if brt: candidates["Brutal"] = brt_mbps
        if bbr: candidates["BBR"]    = bbr_mbps
        best = max(candidates, key=candidates.get) if candidates else "-"

        def delta_str(a, b):
            if a == 0 or b == 0:
                return "-"
            d = (a - b) / b * 100
            return f"{d:+.0f}%"

        k2_str  = f"{k2_mbps:.1f}"  if k2  else "-"
        brt_str = f"{brt_mbps:.1f}" if brt else "-"
        bbr_str = f"{bbr_mbps:.1f}" if bbr else "-"

        vs_brt = delta_str(k2_mbps, brt_mbps) if k2 and brt else "-"
        vs_bbr = delta_str(k2_mbps, bbr_mbps) if k2 and bbr else "-"

        print(f"{scenario:<18} {k2_str:>10} {brt_str:>10} {bbr_str:>10} {best:>12} {vs_brt:>14} {vs_bbr:>12}")

    print()
    print("Values are average throughput in Mbps. Higher is better.")
    print("k2 vs Brutal/BBR: positive = k2 faster, negative = k2 slower.")
    print("CV = Coefficient of Variation (lower = more stable throughput).")
    print()

    # === CSV export ===
    results_dir = sys.argv[1] if len(sys.argv) > 1 else "results"
    csv_path = os.path.join(results_dir, "report.csv")
    with open(csv_path, "w") as f:
        f.write("scenario,protocol,avg_mbps,avg_time_s,speed_cv,rounds,timeouts\n")
        for scenario in SCENARIO_INFO:
            avgs = scenario_avgs.get(scenario, {})
            for proto in PROTO_ORDER:
                if proto not in avgs:
                    continue
                a = avgs[proto]
                f.write(f"{scenario},{proto},{a['avg_mbps']},{a['avg_time']},{a['speed_cv']},{a['rounds']},{a['timeouts']}\n")
    print(f"CSV exported: {csv_path}")


def main():
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <results-dir>")
        sys.exit(1)

    results_dir = sys.argv[1]
    if not os.path.isdir(results_dir):
        print(f"Error: {results_dir} is not a directory")
        sys.exit(1)

    all_data = load_results(results_dir)
    if not all_data:
        print(f"No JSON result files found in {results_dir}")
        sys.exit(1)

    print(f"Loaded {len(all_data)} scenario results from {results_dir}")
    print_report(all_data)


if __name__ == "__main__":
    main()
