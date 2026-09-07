#!/bin/bash
# collect.sh — READ-ONLY on nodes. Pull the raw `DIAG: cc-summary` lines every node has logged
# since the previous pull (per-node high-water mark on the line timestamp), store them gzipped
# under $K2CC_OBS_DATA/raw/<ip>/<utc>.gz, and append a k2s health sample (mem, restarts, status)
# to $K2CC_OBS_DATA/health.csv. Node logs rotate at 20 MB × 3, so a 6-hourly schedule keeps a
# complete record; this is what makes a one-week review possible. Idempotent; safe to rerun.
#
#   scripts/k2cc-obs/collect.sh [--ips=FILE | IP...]      (default nodes.txt; env K2CC_OBS_DATA, PAR)
# Designed for launchd (install-launchd.sh); logs to $K2CC_OBS_DATA/collect.log.
set -uo pipefail
DIR=$(cd "$(dirname "$0")" && pwd)
DATA=${K2CC_OBS_DATA:-$HOME/k2cc-obs-data}
SSH_USER="${KAITU_SSH_USER:-ubuntu}"; SSH_PORT="${KAITU_SSH_PORT:-1022}"; PAR="${PAR:-6}"
IPFILE="$DIR/nodes.txt"; IPS=()
for a in "$@"; do case "$a" in --ips=*) IPFILE="${a#--ips=}";; *) IPS+=("$a");; esac; done
if [ ${#IPS[@]} -eq 0 ]; then while IFS= read -r l; do case "$l" in ''|\#*) ;; *) IPS+=("$l");; esac; done < "$IPFILE"; fi
[ ${#IPS[@]} -gt 0 ] || { echo "no nodes in $IPFILE"; exit 1; }
mkdir -p "$DATA/raw" "$DATA/hwm" "$DATA/names" "$DATA/tmp" "$DATA/reports"
TS=$(date -u +%Y%m%dT%H%M%SZ)
log() { echo "$(date -u +%FT%TZ) $*" | tee -a "$DATA/collect.log"; }
SSH="ssh -o BatchMode=yes -o ConnectTimeout=12 -o StrictHostKeyChecking=accept-new -p $SSH_PORT"
one() { # $1=ip
  local ip=$1 hwm tmp out n newhwm h
  hwm=$(cat "$DATA/hwm/$ip" 2>/dev/null || true)
  tmp="$DATA/tmp/$ip.$TS.gz"
  # Node side: concatenate rotated + current, keep cc-summary lines newer than hwm, gzip. Nothing is written on the node.
  if ! $SSH "$SSH_USER@$ip" "sudo bash -c 'for f in \$(ls -t /apps/k2s/logs/k2s-*.log.gz 2>/dev/null); do zcat \"\$f\"; done; cat /apps/k2s/logs/k2s.log' 2>/dev/null | grep 'DIAG: cc-summary' | awk -v c='$hwm' 'c==\"\" || substr(\$0,6,24) > c' | gzip -c" > "$tmp" 2>/dev/null; then
    log "FAIL ssh/pull $ip"; rm -f "$tmp"; return 1
  fi
  if ! gzip -t "$tmp" 2>/dev/null; then log "FAIL corrupt gz $ip"; rm -f "$tmp"; return 1; fi
  n=$(gzip -dc "$tmp" | wc -l | tr -d ' ')
  if [ "$n" -gt 0 ]; then
    mkdir -p "$DATA/raw/$ip"; out="$DATA/raw/$ip/$TS.gz"; mv "$tmp" "$out"
    newhwm=$(gzip -dc "$out" | awk '{t=substr($0,6,24); if (t>m) m=t} END{print m}')
    [ -n "$newhwm" ] && echo "$newhwm" > "$DATA/hwm/$ip"
  else
    rm -f "$tmp"
  fi
  # Health sample + node name (one round trip).
  h=$($SSH "$SSH_USER@$ip" "echo \"\$(sudo grep -E '^K2_NODE_NAME=' /apps/k2s/.env | cut -d= -f2-),\$(sudo docker stats --no-stream --format '{{.MemUsage}}' k2s | cut -d/ -f1 | tr -d ' '),\$(sudo docker inspect --format '{{.RestartCount}}' k2s),\$(sudo docker inspect --format '{{.State.Health.Status}}' k2-sidecar),\$(sudo docker ps --format '{{.Status}}' -f name=k2s | tr ' ' '_'),\$(sudo docker ps --format '{{.Image}}' -f name=k2s | sed 's/.*://'),\$(ls /apps/k2s/logs/k2s-*.log.gz 2>/dev/null | wc -l | tr -d ' ')\"" 2>/dev/null)
  if [ -n "$h" ]; then echo "$TS,$ip,$h" >> "$DATA/health.csv"; echo "${h%%,*}" > "$DATA/names/$ip"; fi
  log "ok $ip lines=$n hwm=${newhwm:-$hwm}"
}
log "collect start nodes=${#IPS[@]} data=$DATA"
i=0
for ip in "${IPS[@]}"; do one "$ip" & i=$((i+1)); [ $((i % PAR)) -eq 0 ] && wait; done; wait
log "collect done"
# Daily rollup of the last 24 h (cheap; gives the weekly review day-by-day snapshots).
"$DIR/report-range.sh" --from="$(date -u -v-24H +%FT%T 2>/dev/null || date -u -d '-24 hours' +%FT%T)" > "$DATA/reports/last24h-$TS.txt" 2>&1 || log "WARN report-range failed"
