#!/bin/bash
# cc-summary-check.sh — read-only post-upgrade gate for k2cc observability
# (k2s images >= v0.4.10-581bed4f emit `DIAG: cc-summary` per QUIC connection:
# one line every 60 s + final=true at close; see k2/wire/k2cc/CLAUDE.md
# "Observability" for field semantics and the sentinel values).
#
# Reads /apps/k2s/logs/k2s.log directly — `docker logs k2s` is empty under the
# journald log driver. Lines are `time=<UTC RFC3339> level=… msg=…`.
#
# Usage (from the kaitu-center MCP, runs ON the node):
#   exec_on_node(ip, "sudo bash -s 60", scriptPath="docker/scripts/cc-summary-check.sh")
# Argument = look-back window in minutes (default 10). Prints counts, 5 sample
# lines, the spec §9 invariants (all must be 0 except noUdid, which counts
# legacy no-metadata clients under K2_ENFORCE_AUTH=0), totals and the
# mode / rttP50 / lossP50 / tputP50 distributions.
MIN="${1:-10}"
LOGF=/apps/k2s/logs/k2s.log
CUT=$(date -u -d "-${MIN} min" +%Y-%m-%dT%H:%M:%S)
LOG=$(awk -v c="$CUT" 'substr($0,6,19)>=c' "$LOGF")
echo "since=${MIN}m cutoff_utc=$CUT total_lines=$(printf '%s\n' "$LOG" | wc -l) bytes=$(printf '%s' "$LOG" | wc -c) file_size=$(stat -c %s "$LOGF")"
echo "cc_summary_lines=$(printf '%s\n' "$LOG" | grep -c 'DIAG: cc-summary') cc_summary_bytes=$(printf '%s\n' "$LOG" | grep 'DIAG: cc-summary' | wc -c)"
echo "metadata_auth_ok=$(printf '%s\n' "$LOG" | grep -c 'metadata auth OK') accept_stream_done=$(printf '%s\n' "$LOG" | grep -c 'AcceptStream done') panics=$(printf '%s\n' "$LOG" | grep -c 'panic in ccSummaryLoop') errors=$(printf '%s\n' "$LOG" | grep -c 'level=ERROR')"
echo
echo "== 5 sample lines =="
printf '%s\n' "$LOG" | grep 'DIAG: cc-summary' | head -5 | cut -c1-700
echo
printf '%s\n' "$LOG" | grep 'DIAG: cc-summary' | awk '
function kv(line, key,   m){ if (match(line, " "key"=[^ ]+")) { m=substr(line,RSTART+length(key)+2,RLENGTH-length(key)-2); gsub(/"/,"",m); return m } return "" }
function num(s){ gsub(/[^0-9.]/,"",s); return s+0 }
{
  n++
  remote=kv($0,"remote"); final=kv($0,"final"); seq=num(kv($0,"seq"))
  mis=num(kv($0,"mis")); app=num(kv($0,"appLimited")); ss=num(kv($0,"slowStart")); dec=num(kv($0,"decision")); ra=num(kv($0,"rateAdj"))
  lossAgg=num(kv($0,"lossAgg")); lossMax=num(kv($0,"lossMax")); floor=num(kv($0,"floor")); bwAnch=num(kv($0,"bwAnchored"))
  bwEst=num(kv($0,"bwEst")); bwDrop=num(kv($0,"bwDrop")); bwRise=num(kv($0,"bwRise")); udid=kv($0,"udid")
  mode=kv($0,"mode"); modes[mode]++
  if (app>mis) bad_app++
  if (ss+dec+ra!=mis) bad_modes++
  if (lossAgg>lossMax+0.05) bad_loss++
  if (mis==0 && final!="true") bad_zero_periodic++
  if (udid=="") no_udid++
  if (final=="true") { finals++; finals_per[remote]++ }
  if (remote in lastseq) { if (seq<=lastseq[remote]) bad_seq++ } ; lastseq[remote]=seq
  totmis+=mis; totapp+=app; totfloor+=floor; totbw+=bwAnch; totdrop+=bwDrop; totrise+=bwRise
  if (bwEst==0) bwzero++
  r=kv($0,"rttP50"); rtt[r]++
  l=kv($0,"lossP50"); lp[l]++
  t=kv($0,"tputP50"); tp[t]++
  if (app/ (mis>0?mis:1) > 0.8) hiapp++
}
END {
  printf "lines=%d remotes=%d finals=%d\n", n, length(lastseq), finals
  dupf=0; for (r in finals_per) if (finals_per[r]>1) dupf++
  printf "INVARIANTS: appLimited>mis=%d modeSum!=mis=%d lossAgg>lossMax=%d zeroMIperiodic=%d seqNonMonotonic=%d dupFinalPerRemote=%d noUdid=%d\n", bad_app, bad_modes, bad_loss, bad_zero_periodic, bad_seq, dupf, no_udid
  printf "TOTALS: mis=%d appLimited=%d (%.0f%%) floor=%d bwAnchored=%d bwDrop=%d bwRise=%d\n", totmis, totapp, (totmis>0?100*totapp/totmis:0), totfloor, totbw, totdrop, totrise
  printf "PATTERN-A: lines with appLimited/mis>0.8: %d/%d; lines with bwEst=0: %d/%d\n", hiapp, n, bwzero, n
  printf "mode: "; for (k in modes) printf "%s=%d ", k, modes[k]; printf "\n"
  printf "rttP50: "; for (k in rtt) printf "%s=%d ", k, rtt[k]; printf "\n"
  printf "lossP50: "; for (k in lp) printf "%s=%d ", k, lp[k]; printf "\n"
  printf "tputP50: "; for (k in tp) printf "%s=%d ", k, tp[k]; printf "\n"
}'
