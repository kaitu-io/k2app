# UDP echo for the k2r UDP lab. Replies carry the source address the server saw.
# Port 9000 echoes, 9001 never answers, 9002 answers only the first packet.
import socket, select, sys, time
socks=[]
for ip in ("44.44.44.44","44.44.44.45"):
    for port in (9000,9001,9002):
        s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.bind((ip,port)); socks.append(s)
n=0
while True:
    r,_,_=select.select(socks,[],[])
    for s in r:
        d,a=s.recvfrom(65535); n+=1
        ip,port=s.getsockname()
        print(f"{time.time():.3f} rx {ip}:{port} from {a[0]}:{a[1]} len={len(d)}",flush=True)
        # 9002 answers only the first packet of an exchange (client speaks last)
        if port==9000 or (port==9002 and d.startswith(b'p00000')):
            s.sendto(f"seen={a[0]}:{a[1]} at={ip}:{port} echo={d[:16].decode(errors='replace')}".encode(),a)
