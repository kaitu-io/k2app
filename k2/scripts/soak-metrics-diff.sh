#!/usr/bin/env bash
# soak-metrics-diff.sh — UAT leak soak harness via daemon /metrics.
#
# Usage:
#   k2 up   # (prereq) tunnel connected
#   scripts/soak-metrics-diff.sh [DURATION_MIN] [INTERVAL_MIN]
#
# Defaults: 60 min total, 15 min interval → 5 samples (t=0,15,30,45,60).
#
# Reads /metrics (goroutines, heap_alloc, heap_sys, gc_cycles) every tick.
# Compared to the pprof variant: no per-stack breakdown, but total-count
# divergence from baseline is enough to catch any leak — the soak is a
# regression gate, not a leak attribution tool.

set -euo pipefail

DURATION_MIN=${1:-60}
INTERVAL_MIN=${2:-15}
PORT=${K2_DAEMON_PORT:-1777}
BASE="http://127.0.0.1:${PORT}"
OUTDIR="/tmp/k2-soak-$(date -u +%Y%m%dT%H%M%SZ)"
CSV="$OUTDIR/metrics.csv"

mkdir -p "$OUTDIR"
echo "soak: output dir = $OUTDIR"
echo "soak: duration = ${DURATION_MIN}min, interval = ${INTERVAL_MIN}min"

if ! curl -fsS "$BASE/ping" > /dev/null 2>&1; then
    echo "error: daemon not reachable at :$PORT" >&2; exit 1
fi

state() {
    curl -fsS -X POST "$BASE/api/core" -d '{"action":"status"}' 2>/dev/null \
        | python3 -c 'import sys,json; d=json.load(sys.stdin).get("data",{}); print(d.get("state","?"))' 2>/dev/null || echo "?"
}

sample() {
    local label=$1
    local ts
    ts=$(date -u +%FT%TZ)
    local body
    body=$(curl -fsS "$BASE/metrics")
    echo "$body" > "$OUTDIR/metrics-${label}.json"
    python3 - "$label" "$ts" "$body" <<'PY' >> "$CSV"
import sys, json
label, ts, body = sys.argv[1], sys.argv[2], sys.argv[3]
d = json.loads(body).get("data", {})
row = [label, ts, str(d.get("goroutines","")), str(d.get("heap_alloc","")),
       str(d.get("heap_sys","")), str(d.get("gc_cycles",""))]
print(",".join(row))
PY
    local gr state_
    gr=$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["data"]["goroutines"])' "$body" 2>/dev/null || echo ?)
    state_=$(state)
    printf "[%-20s] goroutines=%-4s state=%s\n" "$label" "$gr" "$state_"
}

echo "label,timestamp,goroutines,heap_alloc,heap_sys,gc_cycles" > "$CSV"
sample "t+0-baseline"
BASE_GR=$(awk -F, 'NR==2{print $3}' "$CSV")

TICKS=$((DURATION_MIN / INTERVAL_MIN))
for i in $(seq 1 "$TICKS"); do
    echo "soak: sleeping ${INTERVAL_MIN}min for tick $i/$TICKS..."
    sleep "$((INTERVAL_MIN * 60))"
    sample "t+${i}x${INTERVAL_MIN}min"
done

echo ""
echo "=== soak summary ==="
cat "$CSV" | column -t -s,
echo ""
awk -F, -v base="$BASE_GR" '
NR==1 {next}
{ delta = $3 - base; printf "  %-25s goroutines=%-4s  delta=%+d\n", $1, $3, delta }
' "$CSV"

echo ""
FINAL_GR=$(tail -1 "$CSV" | awk -F, '{print $3}')
DELTA=$((FINAL_GR - BASE_GR))
echo "final goroutine delta from baseline: $DELTA"
if [ "${DELTA#-}" -gt 5 ]; then
    echo "RESULT: FAIL (|delta| > 5 — suspect leak)"
    exit 1
fi
echo "RESULT: PASS (|delta| ≤ 5)"
