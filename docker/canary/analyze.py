#!/usr/bin/env python3
"""analyze.py — per restart event: k2s down→ready, user-visible outage, failed probes.

  docker/canary/analyze.py <probe.log> <events.log>

outage = from the first failed probe's start to the end of the first good probe
after the last failure, within [t0-3 s, t0+25 s] (keep events ≥ 30 s apart).
"""
import re, sys
probes, longs, events = [], [], []
for line in open(sys.argv[1]):
    p = line.split()
    if p[0] == "S": probes.append((float(p[1]), float(p[2]), p[3] == "204"))
    elif p[0] == "L": longs.append((float(p[1]), float(p[2])))
for line in open(sys.argv[2]):
    m = re.match(r"EV (\S+) start=(\S+) cmd_done=(\S+) ready=(\S+)", line)
    if m: events.append((m[1], float(m[2]), float(m[4]) if m[4] != "timeout" else None))
print(f"{'event':22} {'k2s_down→ready':>14} {'outage_s':>9} {'fails':>6} {'long_broken':>12}")
for name, t0, t2 in events:
    win = [p for p in probes if t0 - 3 <= p[0] <= t0 + 25]
    fails = [p for p in win if not p[2]]
    outage = 0.0
    if fails:
        rec = next((p for p in win if p[2] and p[0] > fails[-1][0]), None)
        outage = (rec[1] - fails[0][0]) if rec else float("nan")
    broken = "yes" if any(t0 - 1 <= l[1] <= t0 + 25 for l in longs) else "no"
    print(f"{name:22} {(t2 - t0) if t2 else float('nan'):14.2f} {outage:9.2f} {len(fails):6d} {broken:>12}")
