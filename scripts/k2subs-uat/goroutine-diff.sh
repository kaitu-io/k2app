#!/usr/bin/env bash
# goroutine-diff.sh — measure goroutine leak per up/down cycle.
#
# Requires daemon started with K2_PPROF=1 (see k2/daemon/pprof.go).
# Usage:
#   N=10 ./scripts/k2subs-uat/goroutine-diff.sh k2subs://UDID:TOKEN@.../api
# Defaults N=10. `via` defaults to the daemon's currently-active route
# (extract_creds must have been primed).

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/common.sh"

VIA="${1:-}"
N="${N:-10}"

# Sanity: pprof must be mounted.
if ! curl -sS --max-time 2 "$DAEMON_URL/debug/pprof/goroutine?debug=2" -o /dev/null; then
  echo "error: pprof not reachable at $DAEMON_URL/debug/pprof/ — restart daemon with K2_PPROF=1" >&2
  exit 2
fi

if [ -z "$VIA" ]; then
  # Try to reuse what the daemon is currently connected to.
  VIA=$(curl -sS -X POST "$DAEMON_URL/api/core" \
    -H 'Content-Type: application/json' -d '{"action":"status"}' \
    | python3 -c '
import json, sys
d = json.load(sys.stdin).get("data", {})
via = (d.get("config") or {}).get("routes", [{}])[0].get("via","")
print(via)
')
fi
if [ -z "$VIA" ]; then
  echo "error: no via URL — pass one as arg, or run do_up once before this script" >&2
  exit 2
fi
echo "via=$VIA  cycles=$N"

dump() {
  # Strip goroutine IDs + address hex so identical stacks collapse after counting.
  curl -sS "$DAEMON_URL/debug/pprof/goroutine?debug=2"
}

# Top-of-stack function name for each goroutine. pprof debug=2 separates
# goroutines with blank lines; the first non-header line inside each block is
# the top frame.
top_frames() {
  python3 - <<'PY'
import sys, re
buf = sys.stdin.read().split("\n\n")
for block in buf:
    lines = [l for l in block.splitlines() if l.strip()]
    if len(lines) < 2: continue
    header = lines[0]
    if not header.startswith("goroutine "): continue
    # First frame line looks like:  package.func(args)
    # followed by a file:line line. We keep the function name only.
    frame = lines[1].strip()
    # Strip argument list
    frame = re.sub(r"\(.*\)$", "", frame)
    print(frame)
PY
}

print_banner "BASELINE — dump goroutines"
dump > /tmp/pprof-before.txt
BEFORE_TOP="$(top_frames </tmp/pprof-before.txt | sort | uniq -c | sort -rn)"
BEFORE_N=$(goroutine_count)
echo "baseline goroutines: $BEFORE_N"

print_banner "CYCLE — $N × (down; up)"
for i in $(seq 1 "$N"); do
  do_down || true
  wait_poll 2
  do_up "$VIA" > /dev/null || true
  wait_poll 4
  AFTER_CY=$(goroutine_count)
  printf "  cycle %02d/%02d  goroutines=%s\n" "$i" "$N" "$AFTER_CY"
done

print_banner "POST — dump goroutines"
dump > /tmp/pprof-after.txt
AFTER_TOP="$(top_frames </tmp/pprof-after.txt | sort | uniq -c | sort -rn)"
AFTER_N=$(goroutine_count)

print_banner "DIFF"
echo "goroutines: before=$BEFORE_N  after=$AFTER_N  delta=$((AFTER_N - BEFORE_N))  per_cycle=$(( (AFTER_N - BEFORE_N) / N ))"
echo
echo "--- per top-frame count (after) - (before) ---"
python3 - <<PY
def load(s):
    out = {}
    for line in s.splitlines():
        line = line.strip()
        if not line: continue
        parts = line.split(None, 1)
        if len(parts) != 2: continue
        n, name = parts
        out[name] = out.get(name, 0) + int(n)
    return out
b = load("""$BEFORE_TOP""")
a = load("""$AFTER_TOP""")
keys = set(a) | set(b)
diffs = [(a.get(k,0) - b.get(k,0), k) for k in keys]
diffs.sort(reverse=True)
for d, k in diffs:
    if d == 0: continue
    sign = "+" if d > 0 else ""
    print(f"  {sign}{d:>4}  {k}")
PY
echo
echo "full dumps saved to /tmp/pprof-before.txt and /tmp/pprof-after.txt"
