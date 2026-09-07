# k2cc observability — fleet MI report

Read-only tooling for Phase 1 of `docs/superpowers/specs/2026-09-06-k2cc-observability-design.md`
(the spec lives in the k2 submodule's docs tree). Every k2s node ≥ `v0.4.10-581bed4f` logs one
`DIAG: cc-summary` line per QUIC connection per minute plus a `final=true` line at close; this
directory turns those lines into the spec §13 pattern numbers.

| File | Runs where | What |
|---|---|---|
| `fleet-report.sh` | your machine | SSH loop over `nodes.txt` (or `--ips=` / args), then both aggregators |
| `node-report.sh` | on the node (`sudo bash -s`) | health + every cc-summary line (rotated `.gz` + current) → pattern A–F counters, histograms, ERROR kinds |
| `node-sessions.sh` | on the node | same lines with probe connections (`final && uptimeS<30`) dropped + per-connection session table |
| `aggregate.py` / `aggregate-sessions.py` | your machine | fleet + per-region roll-ups of the two node outputs |
| `nodes.txt` | — | node IPs; refresh from the kaitu-center MCP `list_nodes` (skip `tunnels=[]`) |

Single node, no SSH from here: `exec_on_node(ip, "sudo bash -s", scriptPath="scripts/k2cc-obs/node-report.sh")`.
Quick invariants/volume gate for one node: `docker/scripts/cc-summary-check.sh`.

## Reading rules (they bite)

- **Drop the probes first.** ~89 % of connections live < 30 s, have exactly one line (their final),
  carry ~1 % of bytes and are the desktop daemon's server-probe handshakes. `node-sessions.sh` does
  this; `node-report.sh` numbers are raw.
- `minRttMs=1000` = never measured (all-app-limited sender), `bwRise=0`, `rateAdj=0`, `ssSat=0` are
  structural (see `k2/wire/k2cc/CLAUDE.md` "Observability"). `udid=""` = legacy / failed-verdict
  client under `K2_ENFORCE_AUTH=0`.
- `tputMax` is noise (tiny MIs report 400+ Mbps). Percentiles are histogram **upper edges**.
- Log rotation is 20 MB × 3 backups: a busy node keeps ~12–24 h; run the report before the window
  you care about rotates out.

## Baseline — 2026-09-07 12:00 UTC (10–20 h after rollout, 26 nodes)

Raw: 192 852 lines, 68 632 connections, 695 udids, 4.18 M MIs, 333 GB sent, byte-weighted loss 4.2 %,
35 % of lines / 21 % of bytes with `udid=""`. Sessions ≥ 30 s: 10 210 (76 % < 5 min, 69 % < 1 MB,
643 ≥ 100 MB).

| Pattern | Baseline | Verdict then |
|---|---|---|
| A app-limited | 81 % of MIs; 84 % of session windows > 80 % app-limited; 78 % of sessions never get a bwEst but carry 11.8 GB of 333 GB; sessions ≥ 10 MB get one 95 % of the time | estimator fed when it matters; real cost = idle 5 Mbps floor at burst start (`BytesSent < 10 KB` → `arcMinRate`) |
| B floor+loss | floor > 0 in 39 % of windows, only 1 % of those with lossAgg > 20 % | floors are app-limited anchoring; α tuning not supported |
| C queueing | 15 % of BW-anchored windows rttP90 > 2×minRtt | low priority |
| D bwDrop | 47 457 drops, 76 % in windows with lossP50 ≤ 0.5 % | confirmed phenomenon; cause (app dip vs flow control vs real drop) unresolved |
| E far-region MI rate | bulk windows ≥ 40 MI/min: AU 61 %, CA 68 %, JP/KR 78 %, US 83 %, HK 85 %, SG 86 % | not the bottleneck |
| F ACK artifacts | ackedMB > sentMB in 1 % of windows | ACK filtering not urgent |
| G region | avg minRtt / loss: KR 97 ms/1.9 %, JP 97/2.7, SG 107/2.7, HK 174/5.9 (anomalous), US 194/5.9, CA 252/5.8, AU 337/4.6; rttP50 ≈ 2× minRtt everywhere; lossP50 = 0 in 89 % of windows, lossP90 ≥ 10 % in 15–30 % | replace the benchmark matrix with ~100/200/300 ms × {0, 2, 5, 20} % bursty loss |

Session-window distributions: rateP50 5 Mbps bucket 75 %, 30 Mbps 8 %; tputP50 ≤ 1 Mbps 88 %;
bulk windows tputP50 ≤ 5 Mbps 65 %, bwEst mostly 5–50 Mbps. k2s RSS 21–175 MiB (highest on the
node with the most bytes and fewest connections: us-la.bwh.wm06).

## One-week review checklist (due 2026-09-14)

1. Run `fleet-report.sh`; note the window actually covered per node (rotation).
2. Is the probe share still ~89 % / 1 % of bytes? If the client changed, re-derive the filter.
3. Re-score A–G against the table above. Anything that flipped is more interesting than anything that held.
4. Region matrix incl. weekend + Beijing 02:00–06:00; is HK still worse than JP/KR/SG (then mtr the HK egress)?
5. Memory per node vs bytes and connections (leak = grows with connections, load = tracks bytes).
6. `udid=""` share — input for the enforce rollout.
7. Open questions from 2026-09-07: (a) known-capacity control download through 2–3 nodes and read its own cc-summary (algorithm-limited or link-limited?); (b) RTT cross-tab on bwDrop windows; (c) tputMax gate.
8. Decide Phase 2: per-connection A/B with a `variant=` key appended to the line; candidates in this order —
   idle floor keeps last bwEst / 30 M instead of 5 M; bwDrop needs 2 consecutive non-app-limited MIs;
   benchmark matrix from G. α/β tuning is deprioritized unless B/C flipped.
