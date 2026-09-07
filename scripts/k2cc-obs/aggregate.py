import glob, re, sys, os
from collections import defaultdict, Counter
D = sys.argv[1]
nodes = {}
def parse(text):
    d = {'H': {}, 'ERR': []}
    for line in text.splitlines():
        if line.startswith('node='): d['node'] = line[5:].strip()
        elif line.startswith('health '): d['health'] = dict(kv.split('=',1) for kv in line[7:].split())
        elif line.startswith('since_start='): d['since'] = dict(kv.split('=',1) for kv in line.split())
        elif line.startswith('lines='): d['lines'] = {k: int(v) for k, v in (kv.split('=') for kv in line.split())}
        elif line.startswith('sum '): d['sum'] = {k: float(v) for k, v in (kv.split('=') for kv in line[4:].split())}
        elif re.match(r'^[A-F] ', line):
            d[line[0]] = {k: int(v) for k, v in (kv.split('=') for kv in line[2:].split())}
        elif line.startswith('H '):
            parts = line[2:].split(); name = parts[0]
            d['H'][name] = {k: int(v) for k, v in (kv.split('=') for kv in parts[1:])}
        elif line.startswith('X finals'):
            d['Xf'] = {k: int(v) for k, v in (kv.split('=') for kv in line[9:].split())}
        elif line.startswith('X udid'):
            d['Xu'] = {k: float(v) for k, v in (kv.split('=') for kv in line[7:].split())}
        elif line.startswith('ERR '):
            m = re.match(r'ERR (\d+) msg="([^"]+)"', line)
            if m: d['ERR'].append((int(m.group(1)), m.group(2)))
    return d
for f in sorted(glob.glob(os.path.join(D, '*.txt'))):
    d = parse(open(f).read())
    if 'node' in d and 'lines' in d: nodes[d['node']] = d
def region(n): return n.split('.')[0].split('-')[0].upper()
def pct(a, b): return f"{100*a/b:.0f}%" if b else "-"
print(f"nodes={len(nodes)}")
print("\n== HEALTH ==")
for n, d in sorted(nodes.items()):
    h = d['health']; s = d['since']
    print(f"{n:22s} {h['status']:14s} restarts={h['restarts']} mem={h['mem']:9s} panics={s['panics']} lines={d['lines']['lines']:6d} remotes={d['lines']['remotes']:5d} udids={d['lines']['udids']:3d} errs={s['errors_since_start']:6s} top_err={d['ERR'][0][1] if d['ERR'] else '-'}")
# fleet totals
T = defaultdict(float); L = Counter(); A = Counter(); B = Counter(); C = Counter(); Dd = Counter(); E = Counter(); F = Counter(); Xf = Counter(); Xu = defaultdict(float)
H = defaultdict(Counter)
for n, d in nodes.items():
    for k, v in d['sum'].items(): T[k] += v
    L.update(d['lines']); A.update(d.get('A', {})); B.update(d.get('B', {})); C.update(d.get('C', {})); Dd.update(d.get('D', {})); E.update(d.get('E', {})); F.update(d.get('F', {}))
    Xf.update(d.get('Xf', {}))
    for k, v in d.get('Xu', {}).items(): Xu[k] += v
    for hn, hv in d['H'].items(): H[hn].update(hv)
print("\n== FLEET TOTALS ==")
print(f"lines={L['lines']} finals={L['finals']} remotes={L['remotes']} udids={L['udids']} noudid_lines={L['noudid']} ({pct(L['noudid'], L['lines'])})")
print(f"MIs={int(T['mis'])} appLimited={pct(T['appLimited'], T['mis'])} bwAnchored={pct(T['bwAnchored'], T['mis'])} floor={pct(T['floor'], T['mis'])} slowStart={pct(T['slowStart'], T['mis'])} decision={pct(T['decision'], T['mis'])} rateAdj={int(T['rateAdj'])}")
print(f"bwDrop={int(T['bwDrop'])} bwRise={int(T['bwRise'])} ssSat={int(T['ssSat'])} probes={int(T['probes'])} sentGB={T['sentMB']/1024:.1f} ackedGB={T['ackedMB']/1024:.1f} windowH={T['windowS']/3600:.0f}")
lw = sum(d['sum']['lossW']*d['sum']['sentMB'] for d in nodes.values())/T['sentMB']
print(f"byte-weighted loss={lw:.2f}%  MIs/min of window={T['mis']/(T['windowS']/60):.1f}")
print("\n== PATTERN A (app-limited / bwEst) ==")
print(f"lines appLimited/mis>0.8: {pct(A['hiapp'], L['lines'])}  0.5-0.8: {pct(A['midapp'], L['lines'])}  <0.5: {pct(A['loapp'], L['lines'])}")
print(f"lines bwEst=0: {pct(A['bwzero_lines'], L['lines'])}   connections that NEVER got a bwEst: {A['remotes_bw_never']}/{L['remotes']} = {pct(A['remotes_bw_never'], L['remotes'])}")
print(f"rateP50 buckets (Mbps): " + ' '.join(f"{k}={pct(v, L['lines'])}" for k, v in sorted(H['rateP50'].items(), key=lambda kv: float(kv[0].lstrip('>')))))
print("\n== PATTERN B (floor / high loss) ==")
print(f"lines floor>0: {pct(B['floor_lines'], L['lines'])}; of those lossAgg>20%: {B['floor_hiloss']} ({pct(B['floor_hiloss'], B['floor_lines'])}); all lines lossAgg>20%: {pct(B['hiloss_lines'], L['lines'])}")
print(f"lossAgg buckets: " + ' '.join(f"{k}={pct(H['lossAgg'][k], L['lines'])}" for k in ['0','le1','le5','le20','gt20']))
print("\n== PATTERN C (queueing on anchored windows) ==")
print(f"anchored-dominated lines: {C['anchored_lines']}; rttP90 > 2×minRtt among them: {C['anchored_bloat']} ({pct(C['anchored_bloat'], C['anchored_lines'])})")
print("\n== PATTERN D (bwDrop with low loss) ==")
print(f"lines with bwDrop>0: {Dd['drop_lines']}; of those lossP50<=0.5%: {Dd['drop_lowloss']} ({pct(Dd['drop_lowloss'], Dd['drop_lines'])})")
print("\n== PATTERN E (bulk windows: MIs per minute) ==")
print(f"bulk lines (appLimited<50%, mis>=5): {E['bulk_lines']}  mis/min: <5: {E['bulkmispm_lt5']} <15: {E['bulkmispm_lt15']} <40: {E['bulkmispm_lt40']} >=40: {E['bulkmispm_ge40']}")
print(f"bulk tputP50 (Mbps): " + ' '.join(f"{k}={v}" for k, v in sorted(H['bulkTput'].items(), key=lambda kv: float(kv[0].lstrip('>')))))
print(f"bulk bwEst: " + ' '.join(f"{k}={v}" for k, v in H['bulkBw'].items()))
print("\n== PATTERN F (ACK artifacts) ==")
print(f"ackedMB>sentMB (sent>0.5MB): {F['ackgt']} ({pct(F['ackgt'], L['lines'])}); tputMax/tputP50>3: {F['tmaxdev']} ({pct(F['tmaxdev'], L['lines'])})")
print("\n== SESSIONS (final lines) ==")
print(f"uptime: " + ' '.join(f"{k}={pct(H['sessionLen'][k], L['finals'])}" for k in ['lt30s','lt5m','lt30m','lt2h','ge2h']))
print(f"finals by udid×length: udid_short={Xf['udid_short']} udid_long={Xf['udid_long']} noudid_short={Xf['noudid_short']} noudid_long={Xf['noudid_long']}")
print(f"bytes: udid lines={int(Xu['lines'])} sentGB={Xu['sentMB']/1024:.1f}  noudid lines={int(Xu['noudid_lines'])} sentGB={Xu['noudid_sentMB']/1024:.1f} ({pct(Xu['noudid_sentMB'], Xu['sentMB']+Xu['noudid_sentMB'])} of bytes)")
print(f"MIs per line: " + ' '.join(f"{k}={pct(H['misPerLine'][k], L['lines'])}" for k in ['0','1to4','5to19','20to59','ge60']))
print("\n== PATTERN G: per-region distributions (lines) ==")
R = defaultdict(lambda: defaultdict(Counter)); RL = Counter(); RS = defaultdict(float); RLW = defaultdict(float); RE = defaultdict(Counter)
for n, d in nodes.items():
    r = region(n); RL[r] += d['lines']['lines']; RS[r] += d['sum']['sentMB']; RLW[r] += d['sum']['lossW']*d['sum']['sentMB']
    for hn in ['rttP50','lossP50','lossP90','minRtt','tputP50']: R[r][hn].update(d['H'].get(hn, {}))
    RE[r].update(d.get('E', {}))
def top(c, keys_order=None, n=4, total=None):
    items = [(k, v) for k, v in c.items() if k != 'valid']
    items.sort(key=lambda kv: -kv[1])
    return ' '.join(f"{k}:{pct(v, total)}" for k, v in items[:n])
for r in sorted(RL):
    tot = RL[r]
    print(f"{r:3s} lines={tot:6d} sentGB={RS[r]/1024:5.1f} lossW={RLW[r]/RS[r]:.2f}%  rttP50[{top(R[r]['rttP50'], total=tot)}]  minRtt[{top(R[r]['minRtt'], total=R[r]['minRtt'].get('valid',0))}]  lossP90[{top(R[r]['lossP90'], total=tot)}]  bulk mis/min>=40: {pct(RE[r]['bulkmispm_ge40'], RE[r]['bulk_lines'])}")
print("\n== ERROR kinds fleet-wide (since start) ==")
EC = Counter()
for d in nodes.values():
    for c, m in d['ERR']: EC[m] += c
for m, c in EC.most_common(): print(f"{c:7d} {m}")
