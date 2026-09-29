# usage: flood.py <pps> <seconds> <dst>  -- one socket, fire and count replies
import socket, sys, time, select
pps,secs=int(sys.argv[1]),float(sys.argv[2]); h,p=sys.argv[3].split(':')
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.connect((h,int(p))); s.setblocking(False)
sent=rx=0; seen=set(); t0=time.time(); nxt=t0
while time.time()-t0<secs:
    now=time.time()
    if now>=nxt:
        try: s.send(f"f{sent:06d}".encode()); sent+=1
        except BlockingIOError: pass
        nxt+=1.0/pps
    r,_,_=select.select([s],[],[],0.0005)
    if r:
        try:
            d=s.recv(2048); rx+=1; seen.add(d.split()[0])
        except Exception: pass
time.sleep(1)
try:
    while True:
        d=s.recv(2048); rx+=1; seen.add(d.split()[0])
except Exception: pass
print(f"FLOOD sent={sent} rx={rx} loss={(1-rx/max(sent,1))*100:.1f}% distinct_server_seen_src={len(seen)}")
