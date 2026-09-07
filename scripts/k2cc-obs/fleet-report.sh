#!/bin/bash
# fleet-report.sh — READ-ONLY. Pull k2cc `DIAG: cc-summary` MI statistics from every k2s
# node over SSH (ubuntu@IP:1022 + sudo, same access as .claude/skills/kaitu-node-ops/*.sh)
# and aggregate them fleet-wide + per region. Nothing on the nodes is modified.
#
# Usage:
#   scripts/k2cc-obs/fleet-report.sh                 # nodes from nodes.txt next to this script
#   scripts/k2cc-obs/fleet-report.sh --ips=FILE      # one IP per line, '#' comments allowed
#   scripts/k2cc-obs/fleet-report.sh IP [IP...]      # explicit list
#   OUT=/path scripts/k2cc-obs/fleet-report.sh       # keep raw per-node outputs there (default /tmp/k2cc-obs-<ts>)
#
# Output: fleet health table, totals, spec §13 pattern A–G numbers, per-region matrix, then the
# session-only view (probe connections < 30 s dropped). How to read the fields and the sentinel
# values (minRttMs=1000, bwRise=0, rateAdj=0, udid="") : k2/wire/k2cc/CLAUDE.md "Observability"
# and README.md here. Baseline + review checklist: README.md.
set -uo pipefail
DIR=$(cd "$(dirname "$0")" && pwd)
OUT=${OUT:-/tmp/k2cc-obs-$(date -u +%Y%m%dT%H%M)}
SSH_USER="${KAITU_SSH_USER:-ubuntu}"; SSH_PORT="${KAITU_SSH_PORT:-1022}"; PAR="${PAR:-9}"
IPFILE="$DIR/nodes.txt"; IPS=()
for a in "$@"; do case "$a" in --ips=*) IPFILE="${a#--ips=}";; -h|--help) sed -n 2,16p "$0"; exit 0;; *) IPS+=("$a");; esac; done
if [ ${#IPS[@]} -eq 0 ]; then mapfile -t IPS < <(grep -vE '^\s*(#|$)' "$IPFILE"); fi
[ ${#IPS[@]} -gt 0 ] || { echo "no nodes"; exit 1; }
mkdir -p "$OUT/mi" "$OUT/mi2"
echo "nodes=${#IPS[@]} out=$OUT"
run() { # $1=node script, $2=output subdir; PAR nodes at a time (no xargs -I: BSD xargs caps the replaced arg at 255 bytes)
  local script=$1 sub=$2 n=0 ip
  for ip in "${IPS[@]}"; do
    ( ssh -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -p "$SSH_PORT" "$SSH_USER@$ip" 'sudo bash -s' \
        < "$DIR/$script" > "$OUT/$sub/$ip.txt" 2> "$OUT/$sub/$ip.err" || echo "SSH_FAIL $ip ($script)" ) &
    n=$((n+1)); [ $((n % PAR)) -eq 0 ] && wait
  done
  wait
}
run node-report.sh mi
run node-sessions.sh mi2
echo "fetched: $(ls "$OUT"/mi/*.txt | wc -l | tr -d ' ') report files, $(grep -L '^node=' "$OUT"/mi/*.txt 2>/dev/null | wc -l | tr -d ' ') empty"
python3 "$DIR/aggregate.py" "$OUT/mi"
echo
python3 "$DIR/aggregate-sessions.py" "$OUT/mi2"
