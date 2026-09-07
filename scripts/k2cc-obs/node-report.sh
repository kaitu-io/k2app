#!/bin/bash
# READ-ONLY: k2s health + fleet-aggregatable MI statistics from ALL cc-summary lines
# (rotated .gz + current). Output is k=v lines for local aggregation.
NAME=$(grep -E '^K2_NODE_NAME=' /apps/k2s/.env | cut -d= -f2-)
echo "node=$NAME"
echo "health restarts=$(docker inspect --format '{{.RestartCount}}' k2s) sidecar=$(docker inspect --format '{{.State.Health.Status}}' k2-sidecar) mem=$(docker stats --no-stream --format '{{.MemUsage}}' k2s | cut -d/ -f1 | tr -d ' ') status=$(docker ps --format '{{.Status}}' -f name=k2s | tr ' ' '_') image=$(docker ps --format '{{.Image}}' -f name=k2s | sed 's/.*://')"
START=$(docker inspect --format '{{.State.StartedAt}}' k2s | cut -c1-19)
ALL() { for f in $(ls -t /apps/k2s/logs/k2s-*.log.gz 2>/dev/null); do zcat "$f"; done; cat /apps/k2s/logs/k2s.log; }
echo "since_start=$START panics=$(ALL | grep -c 'panic in ccSummaryLoop') errors_since_start=$(ALL | awk -v c="$START" 'substr($0,6,19)>=c' | grep -c 'level=ERROR') auth_ok_since_start=$(ALL | awk -v c="$START" 'substr($0,6,19)>=c' | grep -c 'metadata auth OK')"
ALL | grep 'DIAG: cc-summary' | awk '
function kv(line, key,   m){ if (match(line, " "key"=[^ ]+")) { m=substr(line,RSTART+length(key)+2,RLENGTH-length(key)-2); gsub(/"/,"",m); return m } return "" }
function num(s){ if (s ~ /^>/) { sub(/^>/,"",s); return s+0.5 } gsub(/[^0-9.]/,"",s); return s+0 }
function lossb(v){ if (v==0) return "0"; if (v<=1) return "le1"; if (v<=5) return "le5"; if (v<=20) return "le20"; return "gt20" }
function bwb(v){ if (v==0) return "0"; if (v<5) return "lt5"; if (v<20) return "lt20"; if (v<50) return "lt50"; if (v<100) return "lt100"; return "ge100" }
function upb(v){ if (v<30) return "lt30s"; if (v<300) return "lt5m"; if (v<1800) return "lt30m"; if (v<7200) return "lt2h"; return "ge2h" }
function misb(v){ if (v==0) return "0"; if (v<5) return "1to4"; if (v<20) return "5to19"; if (v<60) return "20to59"; return "ge60" }
function mrb(v){ if (v<=50) return "le50"; if (v<=100) return "le100"; if (v<=150) return "le150"; if (v<=200) return "le200"; if (v<=300) return "le300"; return "gt300" }
{
  n++
  remote=kv($0,"remote"); udid=kv($0,"udid"); final=kv($0,"final"); seq=num(kv($0,"seq"))
  win=num(kv($0,"windowS")); up=num(kv($0,"uptimeS"))
  mis=num(kv($0,"mis")); app=num(kv($0,"appLimited")); bwa=num(kv($0,"bwAnchored")); fl=num(kv($0,"floor"))
  ss=num(kv($0,"slowStart")); dec=num(kv($0,"decision")); ra=num(kv($0,"rateAdj"))
  bwd=num(kv($0,"bwDrop")); bwr=num(kv($0,"bwRise")); sat=num(kv($0,"ssSat")); pr=num(kv($0,"probes"))
  sent=num(kv($0,"sentMB")); acked=num(kv($0,"ackedMB")); la=num(kv($0,"lossAgg"))
  lp50s=kv($0,"lossP50"); lp90s=kv($0,"lossP90"); lp50=num(lp50s); lmax=num(kv($0,"lossMax"))
  rp50s=kv($0,"rttP50"); rp90s=kv($0,"rttP90"); rp90=num(rp90s); mr=num(kv($0,"minRttMs"))
  ratep50s=kv($0,"rateP50"); tp50s=kv($0,"tputP50"); tp50=num(tp50s); tmax=num(kv($0,"tputMax"))
  bw=num(kv($0,"bwEst")); mode=kv($0,"mode")
  remotes[remote]=1; if (udid!="") udids[udid]=1; else noudid++
  if (final=="true") { finals++; upt[upb(up)]++; k=(udid==""?"noudid":"udid") "_" (up<30?"short":"long"); xt[k]++ }
  if (udid=="") { nou_lines++; nou_sent+=sent; nou_mis+=mis } else { u_lines++; u_sent+=sent; u_mis+=mis }
  Smis+=mis; Sapp+=app; Sbwa+=bwa; Sfl+=fl; Sss+=ss; Sdec+=dec; Sra+=ra; Sbwd+=bwd; Sbwr+=bwr; Ssat+=sat; Spr+=pr
  Ssent+=sent; Sacked+=acked; Swin+=win; Slostw+=la*sent
  misd[misb(mis)]++
  modes[mode]++; rtt50[rp50s]++; loss50[lp50s]++; loss90[lp90s]++; rate50[ratep50s]++; tput50[tp50s]++
  lossagg[lossb(la)]++; bwest[bwb(bw)]++
  if (mr<1000) { mrvalid++; minrtt[mrb(mr)]++ }
  if (bw>0) { bwpos++ } else { bwzero++ }
  if (bw > rmaxbw[remote]) rmaxbw[remote]=bw
  if (mis>0) {
    r=app/mis
    if (r>0.8) hiapp++; else if (r<0.5) loapp++; else midapp++
    if (r<0.5 && mis>=5) { bulk++; bulk_tput[tp50s]++; bulk_bw[bwb(bw)]++; if (win>0) { bmpm=mis/win*60; if (bmpm<5) bmis["lt5"]++; else if (bmpm<15) bmis["lt15"]++; else if (bmpm<40) bmis["lt40"]++; else bmis["ge40"]++ } }
    if (bwa/mis>0.5) { anch++; if (mr<1000 && rp90/mr>2) anch_bloat++ }
  }
  if (fl>0) { floorl++; if (la>20) floor_hiloss++ }
  if (bwd>0) { dropl++; if (lp50<=0.5) drop_lowloss++ }
  if (sent>0.5 && acked>sent) ackgt++
  if (tp50>=1 && tmax/tp50>3) tmaxdev++
  if (la>20) hiloss++
}
END {
  nrem=0; for (r in remotes) nrem++
  nud=0; for (u in udids) nud++
  rbw0=0; for (r in rmaxbw) if (rmaxbw[r]==0) rbw0++
  rbwmiss=nrem; for (r in rmaxbw) rbwmiss--   # remotes never seen with bw key (should be 0)
  printf "lines=%d finals=%d remotes=%d udids=%d noudid=%d\n", n, finals, nrem, nud, noudid
  printf "sum mis=%d appLimited=%d bwAnchored=%d floor=%d slowStart=%d decision=%d rateAdj=%d bwDrop=%d bwRise=%d ssSat=%d probes=%d sentMB=%.1f ackedMB=%.1f windowS=%d lossW=%.2f\n", Smis,Sapp,Sbwa,Sfl,Sss,Sdec,Sra,Sbwd,Sbwr,Ssat,Spr,Ssent,Sacked,Swin,(Ssent>0?Slostw/Ssent:0)
  printf "A hiapp=%d midapp=%d loapp=%d bwzero_lines=%d bwpos_lines=%d remotes_bw_never=%d\n", hiapp, midapp, loapp, bwzero, bwpos, rbw0
  printf "B floor_lines=%d floor_hiloss=%d hiloss_lines=%d\n", floorl, floor_hiloss, hiloss
  printf "C anchored_lines=%d anchored_bloat=%d\n", anch, anch_bloat
  printf "D drop_lines=%d drop_lowloss=%d\n", dropl, drop_lowloss
  printf "E bulk_lines=%d", bulk; for (k in bmis) printf " bulkmispm_%s=%d", k, bmis[k]; printf "\n"
  printf "F ackgt=%d tmaxdev=%d\n", ackgt, tmaxdev
  printf "H mode"; for (k in modes) printf " %s=%d", k, modes[k]; printf "\n"
  printf "H rttP50"; for (k in rtt50) printf " %s=%d", k, rtt50[k]; printf "\n"
  printf "H lossP50"; for (k in loss50) printf " %s=%d", k, loss50[k]; printf "\n"
  printf "H lossP90"; for (k in loss90) printf " %s=%d", k, loss90[k]; printf "\n"
  printf "H rateP50"; for (k in rate50) printf " %s=%d", k, rate50[k]; printf "\n"
  printf "H tputP50"; for (k in tput50) printf " %s=%d", k, tput50[k]; printf "\n"
  printf "H lossAgg"; for (k in lossagg) printf " %s=%d", k, lossagg[k]; printf "\n"
  printf "H bwEst"; for (k in bwest) printf " %s=%d", k, bwest[k]; printf "\n"
  printf "H minRtt valid=%d", mrvalid; for (k in minrtt) printf " %s=%d", k, minrtt[k]; printf "\n"
  printf "H misPerLine"; for (k in misd) printf " %s=%d", k, misd[k]; printf "\n"
  printf "H sessionLen"; for (k in upt) printf " %s=%d", k, upt[k]; printf "\n"
  printf "X finals"; for (k in xt) printf " %s=%d", k, xt[k]; printf "\n"
  printf "X udid lines=%d sentMB=%.1f mis=%d noudid_lines=%d noudid_sentMB=%.1f noudid_mis=%d\n", u_lines, u_sent, u_mis, nou_lines, nou_sent, nou_mis
  printf "H bulkTput"; for (k in bulk_tput) printf " %s=%d", k, bulk_tput[k]; printf "\n"
  printf "H bulkBw"; for (k in bulk_bw) printf " %s=%d", k, bulk_bw[k]; printf "\n"
}'
echo "--- error kinds since start (top4)"; ALL | awk -v c="$START" 'substr($0,6,19)>=c' | grep 'level=ERROR' | grep -o 'msg="[^"]*"' | sort | uniq -c | sort -rn | head -4 | awk '{printf "ERR %s %s\n",$1,substr($0,index($0,$2))}'
echo "--- top3 tputMax"; ALL | grep 'DIAG: cc-summary' | awk '{ if (match($0," tputMax=[0-9.]+")) { v=substr($0,RSTART+9,RLENGTH-9)+0; print v"\t"$0 } }' | sort -t$'\t' -k1,1 -rn | head -3 | cut -f2 | sed -E 's/^time=([^ ]+) level=INFO msg="DIAG: cc-summary" remote=[^ ]+ /\1 /' | cut -c1-330
