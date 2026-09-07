#!/bin/bash
# node-sessions.sh — READ-ONLY second pass: drop sub-30 s (probe) connections — they have exactly
# one line, their final — and re-aggregate; plus a per-connection session table. Same
# --since= / CC_LOCAL_FILE conventions as node-report.sh; aggregated by aggregate-sessions.py.
SINCE=""; for a in "$@"; do case "$a" in --since=*) SINCE="${a#--since=}";; esac; done
if [ -n "${CC_LOCAL_FILE:-}" ]; then
  echo "node=${CC_NODE_NAME:-local}"; echo "mem=-"
  ALL() { gzip -dcf "$CC_LOCAL_FILE"; }
else
  NAME=$(grep -E '^K2_NODE_NAME=' /apps/k2s/.env | cut -d= -f2-); echo "node=$NAME"
  echo "mem=$(docker stats --no-stream --format '{{.MemUsage}}' k2s | cut -d/ -f1 | tr -d ' ')"
  ALL() { for f in $(ls -t /apps/k2s/logs/k2s-*.log.gz 2>/dev/null); do zcat "$f"; done; cat /apps/k2s/logs/k2s.log; }
fi
LINES() { ALL | grep 'DIAG: cc-summary' | awk -v c="$SINCE" 'c=="" || substr($0,6,24) > c'; }
LINES | awk '
function kv(line, key,   m){ if (match(line, " "key"=[^ ]+")) { m=substr(line,RSTART+length(key)+2,RLENGTH-length(key)-2); gsub(/"/,"",m); return m } return "" }
function num(s){ if (s ~ /^>/) { sub(/^>/,"",s); return s+0.5 } gsub(/[^0-9.]/,"",s); return s+0 }
function bwb(v){ if (v==0) return "0"; if (v<5) return "lt5"; if (v<20) return "lt20"; if (v<50) return "lt50"; if (v<100) return "lt100"; return "ge100" }
function upb(v){ if (v<300) return "lt5m"; if (v<1800) return "lt30m"; if (v<7200) return "lt2h"; return "ge2h" }
function sentb(v){ if (v<1) return "lt1MB"; if (v<10) return "lt10MB"; if (v<100) return "lt100MB"; if (v<1000) return "lt1GB"; return "ge1GB" }
{
  remote=kv($0,"remote"); udid=kv($0,"udid"); final=kv($0,"final"); up=num(kv($0,"uptimeS"))
  mis=num(kv($0,"mis")); app=num(kv($0,"appLimited")); fl=num(kv($0,"floor")); bwa=num(kv($0,"bwAnchored"))
  sent=num(kv($0,"sentMB")); la=num(kv($0,"lossAgg")); bw=num(kv($0,"bwEst"))
  rp50=kv($0,"rateP50"); tp50=kv($0,"tputP50"); lp50=kv($0,"lossP50"); mr=num(kv($0,"minRttMs"))
  if (final=="true" && up<30) { probe_lines++; probe_sent+=sent; probe_mis+=mis; next }
  n++; Smis+=mis; Sapp+=app; Sfl+=fl; Sbwa+=bwa; Ssent+=sent; Slost+=la*sent
  if (mis>0) { r=app/mis; if (r>0.8) hiapp++; else if (r<0.5) loapp++; else midapp++ }
  if (bw==0) bwzero++
  if (fl>0) floorl++
  rate[rp50]++; tput[tp50]++; loss[lp50]++
  if (mr<1000) { mrv++; mrsum+=mr }
  rmaxup[remote]=(up>rmaxup[remote]?up:rmaxup[remote]); if (bw>rmaxbw[remote]) rmaxbw[remote]=bw
  rsent[remote]+=sent; rmis[remote]+=mis; rapp[remote]+=app; if (udid!="") rud[remote]=1; if (final=="true") rfin[remote]=1
}
END {
  printf "probe lines=%d sentMB=%.1f mis=%d\n", probe_lines, probe_sent, probe_mis
  printf "sess lines=%d mis=%d appLimited=%d floor=%d bwAnchored=%d sentMB=%.1f lossW=%.2f hiapp=%d midapp=%d loapp=%d bwzero=%d floor_lines=%d minRttAvg=%.0f\n", n, Smis, Sapp, Sfl, Sbwa, Ssent, (Ssent>0?Slost/Ssent:0), hiapp, midapp, loapp, bwzero, floorl, (mrv>0?mrsum/mrv:0)
  printf "H rateP50"; for (k in rate) printf " %s=%d", k, rate[k]; printf "\n"
  printf "H tputP50"; for (k in tput) printf " %s=%d", k, tput[k]; printf "\n"
  printf "H lossP50"; for (k in loss) printf " %s=%d", k, loss[k]; printf "\n"
  for (r in rmaxup) { sess++; if (rud[r]) sud++; if (!(r in rfin)) open++
    ub[upb(rmaxup[r])]++; sb[sentb(rsent[r])]++
    if (rmaxbw[r]==0) { nobw++; nobw_sent+=rsent[r] } else { bwyes_sent+=rsent[r]; bwb_[bwb(rmaxbw[r])]++ }
    if (rsent[r]>=10) { big++; if (rmaxbw[r]==0) big_nobw++; if (rmis[r]>0 && rapp[r]/rmis[r]>0.8) big_hiapp++ }
    if (rsent[r]>=100) { huge++; if (rmaxbw[r]==0) huge_nobw++ }
  }
  printf "S sessions=%d with_udid=%d still_open=%d never_bw=%d never_bw_sentMB=%.1f bw_sentMB=%.1f big10MB=%d big_nobw=%d big_hiapp=%d huge100MB=%d huge_nobw=%d\n", sess, sud, open, nobw, nobw_sent, bwyes_sent, big, big_nobw, big_hiapp, huge, huge_nobw
  printf "H sessUp"; for (k in ub) printf " %s=%d", k, ub[k]; printf "\n"
  printf "H sessSent"; for (k in sb) printf " %s=%d", k, sb[k]; printf "\n"
  printf "H sessMaxBw"; for (k in bwb_) printf " %s=%d", k, bwb_[k]; printf "\n"
}'
