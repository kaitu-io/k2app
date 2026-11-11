#!/usr/bin/env bash
# fleet-pprof-collect.sh — collect one CPU profile (+ load snapshot) from every k2s node that has K2_PPROF=1.
#
#   fleet-pprof-collect.sh --nodes nodes.txt [--seconds 30] [--parallel 8] [--out DIR] [--heap]
#
# nodes.txt: `ip|name` per line (the same file fleet-pprof-rollout.sh uses).
# Read-only on the nodes: the profile is streamed through ssh straight from
# `docker exec k2s wget -qO- http://127.0.0.1:6060/...` to a local file — nothing is written
# on the node. A CPU profile costs a few percent of one core for <seconds> seconds.
#
# Output: DIR/<yyyymmdd-HHMMSS>/<name>.cpu.prof  (+ .heap.prof with --heap)  and <name>.meta
# (loadavg, host cpu split, k2s container cpu/mem at capture time — use it to weight/compare).
#
# Analyse (pprof merges several files):
#   cd k2 && go tool pprof -top -nodecount=40 DIR/<run>/*.cpu.prof
#   go tool pprof -http=:8080 DIR/<run>/*.cpu.prof          # flame graph in the browser
#   go tool pprof -top -cum -focus='kuic' DIR/<run>/*.cpu.prof
set -uo pipefail

NODES=""; SECONDS_N=30; PAR=8; OUT="$HOME/.cache/k2s-profiles"; HEAP=0
SSH_USER="${KAITU_SSH_USER:-ubuntu}"; SSH_PORT="${KAITU_SSH_PORT:-1022}"
while [ $# -gt 0 ]; do
  case "$1" in
    --nodes) NODES="$2"; shift 2 ;;
    --seconds) SECONDS_N="$2"; shift 2 ;;
    --parallel) PAR="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    --heap) HEAP=1; shift ;;
    *) echo "unknown arg: $1" >&2; exit 64 ;;
  esac
done
[ -f "${NODES:-}" ] || { echo "need --nodes FILE" >&2; exit 64; }
[[ "$SECONDS_N" =~ ^[0-9]+$ ]] && [ "$SECONDS_N" -ge 5 ] && [ "$SECONDS_N" -le 120 ] || { echo "--seconds must be 5..120" >&2; exit 64; }

RUN="$OUT/$(date +%Y%m%d-%H%M%S)"; mkdir -p "$RUN"
SSHO=(-n -o ConnectTimeout=10 -o StrictHostKeyChecking=no -o BatchMode=yes -p "$SSH_PORT")
echo "[$(date '+%F %T')] collecting ${SECONDS_N}s CPU profile from $(grep -c . "$NODES") node(s), parallel=$PAR -> $RUN"

one() {  # $1 ip  $2 name
  local ip="$1" name="$2" f="$RUN/$2.cpu.prof"
  # meta first (cheap): loadavg, host cpu line twice 1s apart is overkill — container stats carry cpu%.
  ssh "${SSHO[@]}" "$SSH_USER@$ip" 'echo "ts=$(date -u +%FT%TZ)"; echo "loadavg=$(cut -d" " -f1-3 /proc/loadavg) cores=$(nproc)"; sudo docker stats --no-stream --format "k2s_cpu={{.CPUPerc}} k2s_mem={{.MemUsage}} net={{.NetIO}}" k2s 2>/dev/null' > "$RUN/$name.meta" 2>/dev/null
  if ssh "${SSHO[@]}" "$SSH_USER@$ip" "sudo docker exec k2s wget -qO- 'http://127.0.0.1:6060/debug/pprof/profile?seconds=$SECONDS_N'" > "$f" 2>"$RUN/$name.err" && [ -s "$f" ]; then
    if [ "$HEAP" = 1 ]; then ssh "${SSHO[@]}" "$SSH_USER@$ip" "sudo docker exec k2s wget -qO- 'http://127.0.0.1:6060/debug/pprof/heap'" > "$RUN/$name.heap.prof" 2>>"$RUN/$name.err"; fi
    echo "OK   $name ($(wc -c < "$f" | tr -d ' ') B)"; rm -f "$RUN/$name.err"
  else
    echo "FAIL $name: $(head -c 120 "$RUN/$name.err" | tr '\n' ' ')"; rm -f "$f"
  fi
}

N=0
while IFS='|' read -r ip name; do
  [ -n "${ip:-}" ] || continue
  one "$ip" "$name" &
  N=$((N+1)); [ $((N % PAR)) -eq 0 ] && wait
done < "$NODES"
wait
OKN=$(ls "$RUN"/*.cpu.prof 2>/dev/null | wc -l | tr -d ' ')
echo "[$(date '+%F %T')] done: $OKN/$N profiles in $RUN"
