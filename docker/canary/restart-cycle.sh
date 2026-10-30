#!/bin/bash
# restart-cycle.sh — scripted restarts on the canary host, one EV line each with
# ms timestamps for analyze.py. Mirrors node-upgrade.sh phase B (`up -d -t 2`).
#
#   sudo bash /apps/k2s-canary/restart-cycle.sh <label> <recreate|kill> <count> [interval_s] [image]
#
# recreate: `docker compose up -d --force-recreate -t 2` (with [image] it becomes
#           the new K2S_IMAGE first — an upgrade); kill: SIGKILL + start (crash path).
set -euo pipefail
LABEL="${1:?label}"; MODE="${2:?recreate|kill}"; N="${3:?count}"; GAP="${4:-30}"; IMG="${5:-}"
cd /apps/k2s-canary
LOG=/var/lib/docker/volumes/canary_k2v5-data/_data/k2s.log
now() { date +%s.%3N; }
ready_count() { grep -c "k2s server ready" $LOG; }
wait_ready() { local c=$1; for i in $(seq 1 600); do [ "$(ready_count)" -gt "$c" ] && { now; return; }; sleep 0.05; done; echo timeout; }
[ -n "$IMG" ] && echo "K2S_IMAGE=$IMG" > .env
for i in $(seq 1 "$N"); do
  c=$(ready_count); t0=$(now)
  if [ "$MODE" = kill ]; then docker kill -s KILL k2s >/dev/null; docker start k2s >/dev/null
  else docker compose up -d --force-recreate -t 2 >/dev/null 2>&1; fi
  t1=$(now); t2=$(wait_ready "$c")
  echo "EV $LABEL-$i start=$t0 cmd_done=$t1 ready=$t2"
  [ "$i" -lt "$N" ] && sleep "$GAP"
done
