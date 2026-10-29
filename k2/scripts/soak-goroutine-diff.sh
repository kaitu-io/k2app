#!/usr/bin/env bash
# soak-goroutine-diff.sh — UAT goroutine-leak soak harness.
#
# Usage:
#   K2_PPROF=1 k2 up         # (prereq) start daemon with pprof enabled
#   scripts/soak-goroutine-diff.sh [DURATION_MIN] [INTERVAL_MIN]
#
# Defaults: 60 min total, 15 min interval → 5 snapshots (t=0,15,30,45,60).
#
# Each tick:
#   1. Pull /debug/pprof/goroutine?debug=1 (full text with stack traces)
#   2. Record total goroutine count
#   3. Diff per-stack category counts vs baseline (t=0)
#
# Expected: goroutine count stable ±5 over the soak window. Unbounded
# growth in any single stack signature → leak.

set -euo pipefail

DURATION_MIN=${1:-60}
INTERVAL_MIN=${2:-15}
PORT=${K2_DAEMON_PORT:-1777}
ENDPOINT="http://127.0.0.1:${PORT}/debug/pprof/goroutine?debug=1"
OUTDIR="/tmp/k2-soak-$(date -u +%Y%m%dT%H%M%SZ)"

mkdir -p "$OUTDIR"
echo "soak: output dir = $OUTDIR"
echo "soak: duration = ${DURATION_MIN}min, interval = ${INTERVAL_MIN}min"

if ! curl -fsS "http://127.0.0.1:${PORT}/ping" > /dev/null 2>&1; then
    echo "error: daemon not reachable at :$PORT — start with K2_PPROF=1 first" >&2
    exit 1
fi

if ! curl -fsS "$ENDPOINT" > /dev/null 2>&1; then
    echo "error: /debug/pprof/goroutine returned non-2xx — daemon not built with K2_PPROF=1?" >&2
    exit 1
fi

snapshot() {
    local idx=$1
    local label=$2
    local path="$OUTDIR/snap-${idx}-${label}.txt"
    curl -fsS "$ENDPOINT" > "$path"
    local total
    total=$(head -1 "$path" | grep -oE '[0-9]+' | head -1)
    echo "[$label] total goroutines: $total (saved $path)"
    return 0
}

# Baseline (t=0)
snapshot 0 "baseline"
BASELINE_TOTAL=$(head -1 "$OUTDIR/snap-0-baseline.txt" | grep -oE '[0-9]+' | head -1)

TICKS=$((DURATION_MIN / INTERVAL_MIN))
for i in $(seq 1 "$TICKS"); do
    echo "soak: sleeping ${INTERVAL_MIN}min for tick $i/$TICKS..."
    sleep "$((INTERVAL_MIN * 60))"
    label="t+${i}x${INTERVAL_MIN}min"
    snapshot "$i" "$label"
done

echo ""
echo "=== soak summary ==="
for f in "$OUTDIR"/snap-*.txt; do
    label=$(basename "$f" .txt)
    total=$(head -1 "$f" | grep -oE '[0-9]+' | head -1)
    delta=$((total - BASELINE_TOTAL))
    printf "  %-30s total=%4d  delta=%+d\n" "$label" "$total" "$delta"
done

echo ""
echo "=== leaders (per-stack top counts, baseline vs final) ==="
final_snap=$(ls -t "$OUTDIR"/snap-*.txt | head -1)
# Extract "goroutine profile: total N" header-independent: group by top-of-stack frame.
# Each goroutine entry begins with a line like "N @ 0x... 0x..." — count N, then sum per unique stack (first 3 frames).
awk '
/^[0-9]+ @ / { count=$1; getline; sig=$0; getline; sig=sig"\n"$0; stacks[sig]+=count; next }
END { for (s in stacks) printf "%8d  %s\n", stacks[s], gensub(/\n/, " <- ", "g", s) }
' "$OUTDIR/snap-0-baseline.txt" | sort -rn | head -10 > "$OUTDIR/top-baseline.txt"
awk '
/^[0-9]+ @ / { count=$1; getline; sig=$0; getline; sig=sig"\n"$0; stacks[sig]+=count; next }
END { for (s in stacks) printf "%8d  %s\n", stacks[s], gensub(/\n/, " <- ", "g", s) }
' "$final_snap" | sort -rn | head -10 > "$OUTDIR/top-final.txt"

echo "-- baseline top-10 stacks --"
cat "$OUTDIR/top-baseline.txt"
echo "-- final top-10 stacks --"
cat "$OUTDIR/top-final.txt"

echo ""
echo "diff baseline vs final (unified):"
diff -u "$OUTDIR/top-baseline.txt" "$OUTDIR/top-final.txt" || true

echo ""
echo "full snapshots retained in $OUTDIR — inspect with:"
echo "  diff $OUTDIR/snap-0-baseline.txt $final_snap | head -200"
