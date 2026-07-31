#!/bin/bash
# Aggregate `DIAG: auth-rollout` summaries across the fleet (spec §7.2 gate).
# Per-node and fleet totals of total/authed/cookie_hit/cold/unauthed/
# legacy_no_metadata over the window, plus unauthed_pct.
#
# Usage: ./auth-rollout-report.sh [--since=24h] [--node=IP]
# Requires: KAITU_CENTER_URL, KAITU_ACCESS_KEY
set -euo pipefail

SSH_USER="${KAITU_SSH_USER:-ubuntu}"
SSH_PORT="${KAITU_SSH_PORT:-1022}"
SSH_OPTS="-n -o ConnectTimeout=10 -o StrictHostKeyChecking=no -o BatchMode=yes -p $SSH_PORT"
SINCE="24h"
SINGLE_NODE=""
for arg in "$@"; do
  case "$arg" in
    --since=*) SINCE="${arg#--since=}" ;;
    --node=*) SINGLE_NODE="${arg#--node=}" ;;
    -h|--help) echo "Usage: $0 [--since=24h] [--node=IP]"; exit 0 ;;
    *) echo "Unknown option: $arg"; exit 1 ;;
  esac
done
if [ -z "${KAITU_CENTER_URL:-}" ] || [ -z "${KAITU_ACCESS_KEY:-}" ]; then
  echo "ERROR: KAITU_CENTER_URL and KAITU_ACCESS_KEY must be set"; exit 1
fi

NODE_LIST=$(curl -sf -H "X-Access-Key: $KAITU_ACCESS_KEY" -H "Content-Type: application/json" "$KAITU_CENTER_URL/app/nodes?pageSize=100" | python3 -c "
import json, sys
data = json.load(sys.stdin)
if data.get('code', -1) != 0:
    sys.exit(1)
for n in sorted(data['data']['items'], key=lambda x: x.get('name','')):
    ip = n.get('ipv4',''); name = n.get('name',''); tc = len(n.get('tunnels',[]))
    if not ip or tc == 0:
        continue
    print(f'{ip}|{name}')
")

SUM_AWK='{for(i=1;i<=NF;i++){if(split($i,kv,"=")==2 && kv[1] ~ /^(total|authed|cookie_hit|cold|unauthed|legacy_no_metadata)$/) s[kv[1]]+=kv[2]}} END{printf "total=%d authed=%d cookie_hit=%d cold=%d unauthed=%d legacy_no_metadata=%d\n", s["total"],s["authed"],s["cookie_hit"],s["cold"],s["unauthed"],s["legacy_no_metadata"]}'

declare -A FLEET=( [total]=0 [authed]=0 [cookie_hit]=0 [cold]=0 [unauthed]=0 [legacy_no_metadata]=0 )
NODES_SEEN=0; NODES_UNREACH=0; UNREACH_LIST=""
while IFS='|' read -r IP NAME; do
  [ -n "$SINGLE_NODE" ] && [ "$IP" != "$SINGLE_NODE" ] && continue
  LINE=$(ssh $SSH_OPTS "$SSH_USER@$IP" "sudo docker logs --since $SINCE k2s 2>&1 | grep 'DIAG: auth-rollout' | awk '$SUM_AWK'" 2>/dev/null || echo "SSH_ERR")
  # spec §6.4 fail-open guard: count auth-center-unreachable WARNs in the same window.
  UNREACH=$(ssh $SSH_OPTS "$SSH_USER@$IP" "sudo docker logs --since $SINCE k2s 2>&1 | grep -c 'DIAG: auth-center-unreachable'" 2>/dev/null || echo "?")
  printf '%-16s %-28s %s unreachable=%s\n' "$IP" "$NAME" "$LINE" "$UNREACH"
  if [ "$LINE" != "SSH_ERR" ]; then
    NODES_SEEN=$((NODES_SEEN + 1))
    case "$UNREACH" in ''|*[!0-9]*) ;; *) if [ "$UNREACH" -gt 0 ]; then NODES_UNREACH=$((NODES_UNREACH + 1)); UNREACH_LIST="$UNREACH_LIST $IP($UNREACH)"; fi ;; esac
    for kv in $LINE; do
      K="${kv%%=*}"; V="${kv#*=}"
      FLEET[$K]=$(( ${FLEET[$K]} + V ))
    done
  fi
done <<< "$NODE_LIST"

echo ""
echo "=== FLEET (window: $SINCE) ==="
echo "total=${FLEET[total]} authed=${FLEET[authed]} cookie_hit=${FLEET[cookie_hit]} cold=${FLEET[cold]} unauthed=${FLEET[unauthed]} legacy_no_metadata=${FLEET[legacy_no_metadata]}"
if [ "${FLEET[total]}" -gt 0 ]; then
  python3 -c "print(f'unauthed_pct={100*${FLEET[unauthed]}/${FLEET[total]}:.3f}%  authed_ratio={100*${FLEET[authed]}/${FLEET[total]}:.3f}%')"
fi

# spec §6.4: fail-open single-node alert. A node persistently unable to reach
# Center while the rest of the fleet can is NOT a Center outage — it's the abuse
# vector (degrading one controlled node's path to Center to stretch the 15-min
# revocation SLA toward the 6h grace cap). Center truly down => fleet-wide, and
# is a different (fleet) response. Run this with a SHORT window (--since=10m) as
# the alert cadence; any single-node unreachable>0 while peers are 0 is page-worthy.
if [ "$NODES_UNREACH" -gt 0 ]; then
  if [ "$NODES_UNREACH" -lt "$NODES_SEEN" ]; then
    echo "ALERT(page): ${NODES_UNREACH}/${NODES_SEEN} node(s) report auth-center-unreachable while peers reach Center —${UNREACH_LIST} — single-node isolation, investigate per spec §6.4 (NOT a Center outage)."
  else
    echo "NOTE: all ${NODES_SEEN} reachable node(s) report auth-center-unreachable — looks fleet-wide (Center-side), not the single-node abuse vector."
  fi
fi
