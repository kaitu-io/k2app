#!/usr/bin/env python3
"""Verify a live tunnel gives games full-cone UDP, end to end, without TUN.

Runs k2 in proxy mode, opens ONE SOCKS5 UDP ASSOCIATE session from ONE client
source port, and STUN-binds against several distinct servers through it. A
tunnel fit for games must (a) actually deliver each packet to the peer it was
addressed to, and (b) present the SAME public mapping to all of them
(Endpoint Independent Mapping).

Usage:
    k2 run -c <proxy-mode config with proxy.listen 127.0.0.1:11080>
    # connect it, then:
    scripts/check-nat-type.py

Exit codes: 0 full cone · 1 multi-dest broken · 2 symmetric · 3 misdelivery.

Why (b) alone is not enough — this cost a false PASS on 2026-08-04: a server
that pins the session to a connected socket sends EVERY packet to the first
destination, so all replies come back through one socket with one identical
mapping and read as "full cone" while two of the three peers never heard from
us at all. The reply-source check in parse_reply() is what separates them.
"""
import socket, struct, os, sys, time

PROXY = ("127.0.0.1", 11080)

# Distinct operators, so a reply can be attributed to the peer it was asked of.
# Pre-resolved to IP literals on purpose: sending hostnames lets the server
# re-resolve, and two names can collapse to one address (stun.l.google.com and
# stun1.l.google.com do), which reads as misdelivery when it is one destination.
STUN_HOSTS = [
    ("stun.l.google.com", 19302),
    ("stun.cloudflare.com", 3478),
    ("stun.nextcloud.com", 3478),
]


def resolve_distinct():
    out, seen = [], set()
    for host, port in STUN_HOSTS:
        try:
            ip = socket.gethostbyname(host)
        except OSError as e:
            print(f"  skip {host}: {e}")
            continue
        if ip in seen:
            print(f"  skip {host}: resolves to {ip}, already used")
            continue
        seen.add(ip)
        out.append((ip, port, host))
    if len(out) < 2:
        sys.exit("need at least 2 resolvable, distinct STUN servers")
    return out


STUN_SERVERS = [(ip, port) for ip, port, _ in resolve_distinct()]
MAGIC = 0x2112A442


def socks5_udp_associate():
    t = socket.create_connection(PROXY, timeout=10)
    t.sendall(b"\x05\x01\x00")
    assert t.recv(2) == b"\x05\x00", "socks5 auth negotiation failed"
    # CMD=UDP ASSOCIATE, ATYP=IPv4, 0.0.0.0:0
    t.sendall(b"\x05\x03\x00\x01" + socket.inet_aton("0.0.0.0") + struct.pack("!H", 0))
    r = t.recv(10)
    assert r[1] == 0, f"udp associate rejected: rep={r[1]}"
    relay_ip = socket.inet_ntoa(r[4:8])
    relay_port = struct.unpack("!H", r[8:10])[0]
    if relay_ip == "0.0.0.0":
        relay_ip = PROXY[0]
    return t, (relay_ip, relay_port)


def stun_request(txid):
    return struct.pack("!HHI", 0x0001, 0, MAGIC) + txid


def wrap(host, port, payload):
    """SOCKS5 UDP request header (RSV|FRAG|ATYP|ADDR|PORT) + payload.

    IPv4 literal form (ATYP=0x01) so the server cannot re-resolve and collapse
    two destinations into one.
    """
    return b"\x00\x00\x00\x01" + socket.inet_aton(host) + struct.pack("!H", port) + payload


def parse_reply(data):
    """Return (reply_source_from_socks5_header, stun_msg_bytes).

    The SOCKS5 UDP reply header names the peer the packet came FROM. This is
    the discriminator that separates true multi-destination delivery from a
    connected-socket server that silently misdelivers every packet to the
    session's first destination: in the latter case all replies carry the
    SAME source, while the mapped address alone would look identical (one
    socket) and read as a false "full cone".
    """
    atyp = data[3]
    if atyp == 0x01:
        src = f"{socket.inet_ntoa(data[4:8])}:{struct.unpack('!H', data[8:10])[0]}"
        off = 4 + 4 + 2
    elif atyp == 0x03:
        dl = data[4]
        src = f"{data[5:5+dl].decode(errors='replace')}:{struct.unpack('!H', data[5+dl:7+dl])[0]}"
        off = 4 + 1 + dl + 2
    else:
        src = f"[{socket.inet_ntop(socket.AF_INET6, data[4:20])}]:{struct.unpack('!H', data[20:22])[0]}"
        off = 4 + 16 + 2
    return src, data[off:]


def parse_xor_mapped(msg):
    if len(msg) < 20 or struct.unpack("!I", msg[4:8])[0] != MAGIC:
        return None
    mlen = struct.unpack("!H", msg[2:4])[0]
    i, end = 20, 20 + mlen
    while i + 4 <= end and i + 4 <= len(msg):
        atype, alen = struct.unpack("!HH", msg[i:i + 4])
        val = msg[i + 4:i + 4 + alen]
        if atype == 0x0020 and len(val) >= 8:  # XOR-MAPPED-ADDRESS
            port = struct.unpack("!H", val[2:4])[0] ^ (MAGIC >> 16)
            ip = socket.inet_ntoa(bytes(a ^ b for a, b in
                                        zip(val[4:8], struct.pack("!I", MAGIC))))
            return ip, port
        i += 4 + alen + ((4 - alen % 4) % 4)
    return None


def main():
    ctrl, relay = socks5_udp_associate()
    print(f"UDP relay granted at {relay[0]}:{relay[1]}")
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    s.settimeout(6)
    s.bind(("127.0.0.1", 0))
    print(f"single client source port: {s.getsockname()[1]}\n")

    results, sources = [], []
    for host, port in STUN_SERVERS:
        txid = os.urandom(12)
        s.sendto(wrap(host, port, stun_request(txid)), relay)
        try:
            data, _ = s.recvfrom(2048)
            src, msg = parse_reply(data)
            mapped = parse_xor_mapped(msg)
            print(f"  asked {host}:{port:<6} | replied-by {src:<22} | mapped "
                  + (f"{mapped[0]}:{mapped[1]}" if mapped else "<no XOR-MAPPED-ADDRESS>"))
            results.append(mapped)
            sources.append(src)
        except socket.timeout:
            print(f"  asked {host}:{port:<6} | TIMEOUT (no reply)")
            results.append(None)
            sources.append(None)
        time.sleep(0.4)

    ok = [r for r in results if r]
    live_sources = [x for x in sources if x]
    print(f"\nreached {len(ok)}/{len(STUN_SERVERS)} destinations from one source port")
    if len(ok) < 2:
        print("VERDICT: FAIL — multi-destination broken")
        return 1

    # Distinct peers must have answered. Identical reply sources across
    # different asks mean the server pinned the session to its first
    # destination and misdelivered the rest (connected-socket behaviour) —
    # the mapped address would still look uniform and read as a false pass.
    if len(set(live_sources)) < len(live_sources):
        print(f"reply sources: {live_sources}")
        print("VERDICT: MISDELIVERY — replies came from fewer peers than we asked; "
              "the session is pinned to one destination (connected socket)")
        return 3

    ips = {r[0] for r in ok}
    ports = {r[1] for r in ok}
    print(f"distinct reply peers: {len(set(live_sources))}/{len(live_sources)} — real multi-dest")
    print(f"mapped IPs:   {ips}")
    print(f"mapped ports: {ports}")
    if len(ports) == 1 and len(ips) == 1:
        print("VERDICT: PASS — Endpoint Independent Mapping (full cone / cone NAT)")
        return 0
    print("VERDICT: SYMMETRIC — mapping differs per destination")
    return 2


if __name__ == "__main__":
    sys.exit(main())
