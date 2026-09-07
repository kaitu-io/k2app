#!/bin/bash
# weekly-review.sh — launchd target for the one-week review (2026-09-14 09:07 local): builds the
# 7-day fleet MI report from the data collect.sh gathered, saves it under $K2CC_OBS_DATA/reports/,
# and raises a macOS notification. Idempotent per day (done-marker). Nothing touches the nodes.
set -uo pipefail
DIR=$(cd "$(dirname "$0")" && pwd)
DATA=${K2CC_OBS_DATA:-$HOME/k2cc-obs-data}; mkdir -p "$DATA/reports"
DAY=$(date -u +%F); OUT="$DATA/reports/week-ending-$DAY.txt"
[ -f "$OUT.done" ] && exit 0
FROM=$(date -u -v-7d +%FT%T 2>/dev/null || date -u -d '-7 days' +%FT%T)
{ echo "k2cc fleet MI report — 7 days ending $DAY (from $FROM UTC)"; echo "review checklist: scripts/k2cc-obs/README.md"; echo; "$DIR/report-range.sh" --from="$FROM"; echo; echo "== k2s memory trend (health.csv, MiB, first/last sample per node) =="; awk -F, '{n=$3; m=$4; sub(/MiB/,"",m); if (!(n in f)) f[n]=m; l[n]=m} END{for (n in f) printf "%-22s %s -> %s\n", n, f[n], l[n]}' "$DATA/health.csv" | sort; } > "$OUT" 2>&1
touch "$OUT.done"
osascript -e "display notification \"周报已生成：$OUT — 按 scripts/k2cc-obs/README.md 清单回顾\" with title \"k2cc 一周回顾到期\"" 2>/dev/null || true
echo "$OUT"
