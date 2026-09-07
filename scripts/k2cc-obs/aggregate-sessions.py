#!/usr/bin/env python3
"""Aggregate node-sessions.sh outputs (one file per node) into fleet-level session stats.
Probe-like connections (final && uptimeS<30) are already dropped node-side."""
import glob, re, sys, os
from collections import Counter, defaultdict
D = sys.argv[1]
def region(n): return n.split('.')[0].split('-')[0].upper()
P=Counter(); Sf=defaultdict(float); H=defaultdict(Counter); Ssf=defaultdict(float); RH=defaultdict(lambda: defaultdict(Counter)); RS=defaultdict(Counter); mems={}
for f in glob.glob(os.path.join(D, '*.txt')):
    t=open(f).read(); m=re.search(r'node=(\S+)',t)
    if not m: continue
    node=m.group(1); reg=region(node)
    mm=re.search(r'mem=(\S+)',t); mems[node]=mm.group(1) if mm else '?'
    for line in t.splitlines():
        if line.startswith('probe '):
            for kv in line[6:].split(): k,v=kv.split('='); P[k]+=float(v)
        elif line.startswith('sess '):
            for kv in line[5:].split(): k,v=kv.split('='); Sf[k]+=float(v)
            mr=re.search(r'minRttAvg=(\d+)',line); RS[reg]['mr']+=int(mr.group(1)); RS[reg]['n']+=1
        elif line.startswith('S '):
            for kv in line[2:].split(): k,v=kv.split('='); Ssf[k]+=float(v)
        elif line.startswith('H '):
            parts=line[2:].split(); name=parts[0]
            for kv in parts[1:]: k,v=kv.split('='); H[name][k]+=int(v); RH[reg][name][k]+=int(v)
def pct(a,b): return f"{100*a/b:.0f}%" if b else "-"
L=int(Sf['lines'])
print("== FILTERED (connections >=30 s only) ==")
print(f"dropped probe-like lines={int(P['lines'])} (sentMB={P['sentMB']:.0f}, {pct(P['sentMB'], P['sentMB']+Sf['sentMB'])} of bytes, mis={int(P['mis'])})")
print(f"session lines={L} MIs={int(Sf['mis'])} appLimited={pct(Sf['appLimited'],Sf['mis'])} floor={pct(Sf['floor'],Sf['mis'])} bwAnchored={pct(Sf['bwAnchored'],Sf['mis'])} sentGB={Sf['sentMB']/1024:.1f}")
print(f"lines appLimited>0.8: {pct(Sf['hiapp'],L)}  0.5-0.8: {pct(Sf['midapp'],L)}  <0.5: {pct(Sf['loapp'],L)}   bwEst=0 lines: {pct(Sf['bwzero'],L)}   floor>0 lines: {pct(Sf['floor_lines'],L)}")
def dist(name):
    c=H[name]; items=sorted(c.items(), key=lambda kv: float(kv[0].lstrip('>')))
    return ' '.join(f"{k}={pct(v,L)}" for k,v in items if L and v/L>=0.005)
print("rateP50 (Mbps): "+dist('rateP50')); print("tputP50 (Mbps): "+dist('tputP50')); print("lossP50 (%):    "+dist('lossP50'))
S=int(Ssf['sessions'])
print(f"\n== SESSIONS >=30 s: {S} (with udid {pct(Ssf['with_udid'],S)}, still open {int(Ssf['still_open'])}) ==")
print("length: "+' '.join(f"{k}={pct(H['sessUp'][k],S)}" for k in ['lt5m','lt30m','lt2h','ge2h']))
print("bytes sent per session: "+' '.join(f"{k}={pct(H['sessSent'][k],S)}" for k in ['lt1MB','lt10MB','lt100MB','lt1GB','ge1GB']))
print(f"never got a bwEst: {int(Ssf['never_bw'])} ({pct(Ssf['never_bw'],S)}) carrying {Ssf['never_bw_sentMB']/1024:.1f} GB vs {Ssf['bw_sentMB']/1024:.1f} GB on sessions that did")
print(f"sessions >=10 MB: {int(Ssf['big10MB'])}; never bwEst: {int(Ssf['big_nobw'])} ({pct(Ssf['big_nobw'],Ssf['big10MB'])}), >80% app-limited: {int(Ssf['big_hiapp'])} ({pct(Ssf['big_hiapp'],Ssf['big10MB'])})")
print(f"sessions >=100 MB: {int(Ssf['huge100MB'])}; never bwEst: {int(Ssf['huge_nobw'])} ({pct(Ssf['huge_nobw'],Ssf['huge100MB'])})")
print("max bwEst of sessions that got one: "+' '.join(f"{k}={v}" for k,v in sorted(H['sessMaxBw'].items())))
print("\n== per-region (session lines): rateP50 / tputP50 top buckets ==")
for r in sorted(RH):
    tot=sum(RH[r]['rateP50'].values())
    rp=' '.join(f"{k}:{pct(v,tot)}" for k,v in sorted(RH[r]['rateP50'].items(), key=lambda kv:-kv[1])[:3])
    tp=' '.join(f"{k}:{pct(v,tot)}" for k,v in sorted(RH[r]['tputP50'].items(), key=lambda kv:-kv[1])[:3])
    print(f"{r:3s} lines={tot:6d} rateP50[{rp}] tputP50[{tp}] avgMinRtt≈{RS[r]['mr']//max(RS[r]['n'],1)}ms")
print("\nk2s mem per node: "+', '.join(f"{n}={m}" for n,m in sorted(mems.items())))
