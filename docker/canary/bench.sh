#!/bin/bash
# bench.sh — throughput + request reliability through the canary client (client.sh start first).
#
#   docker/canary/bench.sh <label>
#
# down: one connection, 100 MB from proof.ovh.net capped at 40 s (Mbps) — single-
#       stream throughput is what TCP-WS smux windows limit;
# up:   one connection, 32 MB POST to speed.cloudflare.com/__up (Mbps);
# req:  200 sequential + 200 at 20-way concurrency generate_204 (failures, p50/p95 s).
set -uo pipefail
L="${1:?label}"; SOCKS="${SOCKS:-127.0.0.1:10805}"
C=(curl -s --socks5-hostname "$SOCKS")
mbps() { awk -v b="$1" -v t="$2" 'BEGIN{ if (t>0) printf "%.1f", b*8/t/1e6; else print "nan" }'; }
for i in 1 2; do
  read -r code size t < <("${C[@]}" -o /dev/null --max-time 40 -w '%{http_code} %{size_download} %{time_total}\n' https://proof.ovh.net/files/100Mb.dat)
  echo "$L down#$i code=$code MB=$(awk -v s="$size" 'BEGIN{printf "%.0f", s/1e6}') Mbps=$(mbps "$size" "$t")"
done
head -c 32000000 /dev/urandom > "${TMPDIR:-/tmp}/k2s-canary-up.bin"
for i in 1 2; do
  read -r code size t < <("${C[@]}" -o /dev/null --max-time 60 -w '%{http_code} %{size_upload} %{time_total}\n' --data-binary @"${TMPDIR:-/tmp}/k2s-canary-up.bin" https://speed.cloudflare.com/__up)
  echo "$L up#$i code=$code MB=$(awk -v s="$size" 'BEGIN{printf "%.0f", s/1e6}') Mbps=$(mbps "$size" "$t")"
done
rm -f "${TMPDIR:-/tmp}/k2s-canary-up.bin"
req() { "${C[@]}" -o /dev/null --max-time 5 -w '%{http_code} %{time_total}\n' http://www.gstatic.com/generate_204 || echo "ERR 5"; }
export -f req; export SOCKS
summ() { python3 -c 'import sys
r=[l.split() for l in sys.stdin if l.strip()]; t=sorted(float(x[1]) for x in r)
print(f"{sys.argv[1]} n={len(r)} fail={sum(x[0]!=str(204) for x in r)} p50={t[len(t)//2]:.2f} p95={t[int(len(t)*.95)]:.2f}")' "$1"; }
for i in $(seq 1 200); do req; done | summ "$L req-seq"
seq 1 200 | xargs -P 20 -I{} bash -c 'curl -s --socks5-hostname "$SOCKS" -o /dev/null --max-time 5 -w "%{http_code} %{time_total}\n" http://www.gstatic.com/generate_204 || echo "ERR 5"' | summ "$L req-par20"
