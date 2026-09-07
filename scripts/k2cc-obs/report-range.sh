#!/bin/bash
# report-range.sh — build the fleet MI report from the raw lines collect.sh stored locally, for any
# time range, using the SAME node scripts in local-replay mode (identical numbers to fleet-report.sh
# for the same lines). No SSH.
#   scripts/k2cc-obs/report-range.sh [--from=UTC] [--to=UTC] [--data=DIR] [--keep=DIR]   (env PAR=4 parallel nodes)
#   times are UTC RFC3339 prefixes, e.g. --from=2026-09-07T00:00:00 --to=2026-09-14T00:00:00
set -uo pipefail
DIR=$(cd "$(dirname "$0")" && pwd)
DATA=${K2CC_OBS_DATA:-$HOME/k2cc-obs-data}; FROM=""; TO="9999"; KEEP=""
for a in "$@"; do case "$a" in --from=*) FROM="${a#--from=}";; --to=*) TO="${a#--to=}";; --data=*) DATA="${a#--data=}";; --keep=*) KEEP="${a#--keep=}";; -h|--help) sed -n 2,7p "$0"; exit 0;; esac; done
W=${KEEP:-$(mktemp -d "${TMPDIR:-/tmp}/k2cc-range.XXXXXX")}; mkdir -p "$W/mi" "$W/mi2" "$W/merged"
echo "range from=${FROM:-<start>} to=${TO} data=$DATA work=$W"
PAR="${PAR:-4}"
one() { # $1 = raw dir of one node. Pull files are named <pull-utc>.gz and contain only lines <= pull time,
        # so a file pulled before FROM cannot hold lines in range — skip it (keeps weekly runs cheap).
  local d=$1 ip name m f fts
  ip=$(basename "$d"); name=$(cat "$DATA/names/$ip" 2>/dev/null || echo "$ip"); m="$W/merged/$ip.gz"
  for f in "$d"/*.gz; do
    fts=$(basename "$f" .gz); fts="${fts:0:4}-${fts:4:2}-${fts:6:2}T${fts:9:2}:${fts:11:2}:${fts:13:2}"
    [ -n "$FROM" ] && [ "$fts" \< "$FROM" ] && continue
    gzip -dcf "$f"
  done | awk -v f="$FROM" -v t="$TO" '{ts=substr($0,6,24); if ((f=="" || ts>=f) && ts<t) print}' | sort -u | gzip -c > "$m"
  [ "$(gzip -dc "$m" | head -c1 | wc -c | tr -d ' ')" = 1 ] || { rm -f "$m"; return 0; }
  CC_LOCAL_FILE="$m" CC_NODE_NAME="$name" bash "$DIR/node-report.sh" > "$W/mi/$ip.txt"
  CC_LOCAL_FILE="$m" CC_NODE_NAME="$name" bash "$DIR/node-sessions.sh" > "$W/mi2/$ip.txt"
}
i=0
for d in "$DATA"/raw/*/; do one "$d" & i=$((i+1)); [ $((i % PAR)) -eq 0 ] && wait; done; wait
n=$(ls "$W"/mi/*.txt 2>/dev/null | wc -l | tr -d ' ')
echo "nodes_with_data=$n"
python3 "$DIR/aggregate.py" "$W/mi"; echo; python3 "$DIR/aggregate-sessions.py" "$W/mi2"
[ -n "$KEEP" ] || rm -rf "$W"
