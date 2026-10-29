#!/usr/bin/env bash
# Summarize one nslab.sh run (usage: nslab-analyze.sh .lab/results/<TAG>): DIAG timeline relative to injection, heartbeats, traffic per 30s window.
set -uo pipefail
OUT="$1"; DRV="$OUT/driver.log"; K2="$OUT/k2.log"; TR="$OUT/traffic.log"
inj=$(grep -oE 'injected at [0-9:]+' "$DRV" | head -1 | grep -oE '[0-9:]+$'); rem=$(grep -oE 'removed at [0-9:]+' "$DRV" | grep -oE '[0-9:]+$')
head -2 "$DRV" | cut -c1-250; echo "inject=$inj remove=$rem"
tosec() { IFS=: read -r h m s <<<"$1"; echo $((10#$h*3600 + 10#$m*60 + 10#${s%%.*})); }
inj_s=$(tosec "$inj")
echo "=== DIAG events (t = s after injection) ==="
grep -E 'DIAG: (transport-switch|channel-review|transport-rerace|transport-race-winner|transport-race-fail|echo-probe-fail|quic-handshake-fail|quic-datapath-timeout|connected|session-end)|health: |engine: (health critical|network reconnect|wake reconnect|reconnect)|backup probe|traffic (PAUSED|RESUMED)|shaping (REMOVED|RE-INJECTED)' "$K2" "$DRV" \
  | sed -E 's/^[^ ]*time=([0-9-]+)T([0-9:]+)\.[0-9]+Z[^ ]* level=[A-Z]+ msg=/\2 /; s/^[^ ]*driver.log:\[([0-9:]+)\] /\1 DRIVER: /' \
  | while read -r t rest; do s=$(tosec "$t"); printf 't=%+5d  %s\n' $((s-inj_s)) "$rest"; done | sort -k1.3n | cut -c1-230
echo "=== dial outcomes per 10s (t, ok, fail, timeouts) ==="
grep -E 'DIAG: proxy-dial-fail' "$K2" | sed -E 's/^time=([0-9-]+)T([0-9:]+)\.[0-9]+Z.*err="([^"]*)".*/\2 \3/' \
  | while read -r t rest; do s=$(tosec "$t"); w=$(( (s-inj_s) / 10 * 10 )); to=0; echo "$rest" | grep -qi "timeout\|deadline" && to=1; echo "$w $to"; done \
  | awk '{n[$1]++; to[$1]+=$2} END{for (w in n) printf "t=%+5d fails=%3d timeouts=%3d\n", w, n[w], to[w]}' | sort -k1.3n
echo "=== heartbeats ==="
grep 'DIAG: heartbeat' "$K2" | sed -E 's/^time=([0-9-]+)T([0-9:]+)\.[0-9]+Z[^ ]* /\2 /' \
  | while read -r t rest; do s=$(tosec "$t"); printf 't=%+5d %s %s %s %s %s %s %s %s\n' $((s-inj_s)) "$(grep -o 'health=[a-z]*' <<<"$rest")" "$(grep -o 'transport=[a-z]*' <<<"$rest")" "$(grep -o ' loss=[0-9.]*' <<<"$rest")" "$(grep -o 'dialOk=[0-9]*' <<<"$rest")" "$(grep -o 'dialFail=[0-9]*' <<<"$rest")" "$(grep -o 'activeChannel=[a-z]*' <<<"$rest")" "$(grep -o 'scoreQuic=[0-9.]*' <<<"$rest")" "$(grep -o 'scoreTcpws=[0-9.]*' <<<"$rest")"; done
echo "=== traffic per 30s window ==="
awk -v inj="$inj_s" 'function tosec(x,  a){split(x,a,":"); return a[1]*3600+a[2]*60+a[3]}
  { s=tosec($1)-inj; w=int((s<0? s-29 : s)/30)*30; n[w]++; if($2=="200") ok[w]++; lat[w]+=$3 }
  END { for (w in n) printf "%+5d..%+5d  ok=%3d/%3d  avgLat=%.2fs\n", w, w+29, ok[w]+0, n[w], lat[w]/n[w] }' "$TR" | sort -n
