# usage: client.py <mode> <n> <interval> [dst...]   mode: conn|unconn
import socket, sys, time
mode,n,iv=sys.argv[1],int(sys.argv[2]),float(sys.argv[3])
dsts=[(d.split(':')[0],int(d.split(':')[1])) for d in sys.argv[4:]] or [("44.44.44.44",9000)]
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.settimeout(2)
if mode=="conn": s.connect(dsts[0])
ok=0; seen=set()
for i in range(n):
    d=dsts[i%len(dsts)]
    p=f"p{i:05d}".encode()
    t=time.time()
    try:
        if mode=="conn": s.send(p)
        else: s.sendto(p,d)
        r,a=s.recvfrom(2048) if mode!="conn" else (s.recv(2048),d)
        ok+=1; txt=r.decode(); seen.add(txt.split()[0])
        if n<=20: print(f"{i} -> {d} reply_from={a} {txt} rtt={(time.time()-t)*1000:.1f}ms")
    except Exception as e:
        if n<=20: print(f"{i} -> {d} ERR {e}")
    time.sleep(iv)
print(f"SUMMARY sent={n} ok={ok} distinct_server_seen_src={len(seen)} local={s.getsockname()}")
if len(seen)<=6: print(sorted(seen))
