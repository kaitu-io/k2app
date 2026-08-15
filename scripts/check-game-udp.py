#!/usr/bin/env python3
"""Play a real game protocol through a live tunnel and measure what a gamer feels.

check-nat-type.py proves the NAT shape with STUN. That is necessary but it is
not the business question, which is "can someone actually play". This script
answers that one directly: it speaks the Minecraft Bedrock (RakNet) unconnected
ping to several popular public game servers **through one SOCKS5 UDP session
from one client source port** — the exact shape of a game client that talks to
matchmaking and then to a game server without rebinding.

Bedrock's unconnected ping was chosen over Valve's A2S because it needs no
challenge handshake, no API key, and its reply carries a parseable MOTD with a
live player count — so a reply proves a real game server on the other end, not
just a socket that echoed something back.

Three things are measured, and all three must hold:

  1. Attribution — every reply must arrive from the server it was asked of.
     A node pinning the session to a connected socket misdelivers every packet
     to destination 1 and still looks healthy by any aggregate metric. This is
     the discriminator that catches the bug that breaks games; without it a
     hard failure reads as a clean pass.
  2. Payload — the reply is parsed down to the server name and player count.
     A reply that arrives but does not decode means the tunnel mangled or
     truncated the datagram, which reachability counts alone would score as
     a pass.
  3. Playability — sustained round-trips per destination, reported as loss and
     jitter. Games tolerate latency far better than loss and jitter, so a
     "reachable" verdict at 30% loss is still unplayable and must not read as
     success.

Usage:
    # tunnel already up in proxy mode with proxy.listen 127.0.0.1:11080
    scripts/check-game-udp.py [--rounds N] [--servers N] [--direct]

    --direct bypasses the tunnel, to prove the targets themselves are alive
    before blaming the tunnel for a failure.

Exit codes: 0 playable · 1 not enough servers reachable · 2 misdelivery ·
3 payload corruption · 4 reachable but loss/jitter beyond playable thresholds.
"""
import argparse
import socket
import struct
import statistics
import sys
import time

PROXY = ("127.0.0.1", 11080)

# Popular public Bedrock servers on distinct operators. Distinct operators
# matter: two names on one box share a NAT path and would not exercise
# multi-destination routing at all.
GAME_HOSTS = [
    ("geo.hivebedrock.network", 19132),   # The Hive
    ("play.nethergames.org", 19132),      # NetherGames
    ("play.cubecraft.net", 19132),        # CubeCraft
    ("play.galaxite.net", 19132),         # Galaxite
    ("play.lbsg.net", 19132),             # Lifeboat
]

RAKNET_MAGIC = b"\x00\xff\xff\x00\xfe\xfe\xfe\xfe\xfd\xfd\xfd\xfd\x12\x34\x56\x78"
PONG_ID = 0x1C

# A gamer's thresholds, not a network engineer's. Above these a session is not
# "degraded", it is unplayable, and the verdict must say so.
MAX_LOSS_PCT = 5.0
MAX_JITTER_MS = 50.0


def resolve_distinct(hosts):
    """Resolve to IP literals, dropping names that collapse onto an address
    already in use. Two names on one IP are ONE destination, and counting them
    as two turns a single-destination test into a false multi-dest pass —
    play.lbsg.net and sg.lbsg.net are the same box today."""
    out, seen = [], set()
    for host, port in hosts:
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
    return out


def socks5_udp_associate():
    t = socket.create_connection(PROXY, timeout=10)
    t.sendall(b"\x05\x01\x00")
    if t.recv(2) != b"\x05\x00":
        sys.exit("socks5 auth negotiation failed")
    t.sendall(b"\x05\x03\x00\x01" + socket.inet_aton("0.0.0.0") + struct.pack("!H", 0))
    r = t.recv(10)
    if r[1] != 0:
        sys.exit(f"udp associate rejected: rep={r[1]}")
    ip = socket.inet_ntoa(r[4:8])
    port = struct.unpack("!H", r[8:10])[0]
    return t, (PROXY[0] if ip == "0.0.0.0" else ip, port)


def wrap(ip, port, payload):
    """SOCKS5 UDP request header + payload, ATYP=0x01 so the node cannot
    re-resolve a hostname and collapse two destinations into one."""
    return b"\x00\x00\x00\x01" + socket.inet_aton(ip) + struct.pack("!H", port) + payload


def parse_socks_reply(data):
    """Return (reply_source 'ip:port', payload). The SOCKS5 reply header names
    the peer the datagram came FROM — the attribution discriminator."""
    atyp = data[3]
    if atyp == 0x01:
        src = f"{socket.inet_ntoa(data[4:8])}:{struct.unpack('!H', data[8:10])[0]}"
        off = 10
    elif atyp == 0x03:
        dl = data[4]
        src = f"{data[5:5+dl].decode(errors='replace')}:{struct.unpack('!H', data[5+dl:7+dl])[0]}"
        off = 7 + dl
    else:
        src = f"[{socket.inet_ntop(socket.AF_INET6, data[4:20])}]:{struct.unpack('!H', data[20:22])[0]}"
        off = 22
    return src, data[off:]


def ping_packet(seq):
    return (b"\x01" + struct.pack(">Q", seq) + RAKNET_MAGIC
            + struct.pack(">Q", 0x1234567890ABCDEF))


def pong_seq(payload):
    """The pong echoes the ping's 8-byte seq. This is the correlation ID that
    makes attribution sound.

    An earlier version of this script matched replies by arrival order — send,
    then assume the next datagram is the answer. With five destinations whose
    RTTs span 60ms to 330ms on one socket, a reply that misses its timeout is
    read by the NEXT exchange, and every later reply is off by one from then
    on. That produced a confident MISDELIVERY verdict against a node that was
    in fact delivering every packet correctly, and a median RTT of 8ms through
    a Hong Kong round trip was the only thing that gave it away. Correlate on
    the echoed seq; never on ordering."""
    if not payload or payload[0] != PONG_ID or len(payload) < 9:
        return None
    return struct.unpack(">Q", payload[1:9])[0]


def parse_pong(payload):
    """Parse an unconnected pong into (name, players, max). Returns None if it
    is not well formed — a truncated or mangled datagram must not be scored as
    a successful round-trip."""
    if not payload or payload[0] != PONG_ID or len(payload) < 35:
        return None
    try:
        strlen = struct.unpack(">H", payload[33:35])[0]
        motd = payload[35:35 + strlen].decode("utf-8", errors="replace")
        parts = motd.split(";")
        if len(parts) < 6:
            return None
        return parts[1], parts[4], parts[5]
    except (struct.error, IndexError):
        return None


class Transport:
    """Either a plain UDP socket (--direct) or one SOCKS5 UDP session. Both
    expose the same send/recv so the tunnelled and direct runs are the same
    test, and a --direct pass is real evidence the targets are alive."""

    def __init__(self, direct):
        self.direct = direct
        self.ctrl = None
        self.relay = None
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.sock.bind(("0.0.0.0" if direct else "127.0.0.1", 0))
        if not direct:
            self.ctrl, self.relay = socks5_udp_associate()

    @property
    def source_port(self):
        return self.sock.getsockname()[1]

    def send(self, ip, port, payload):
        if self.direct:
            self.sock.sendto(payload, (ip, port))
        else:
            self.sock.sendto(wrap(ip, port, payload), self.relay)

    def recv(self, timeout):
        """Read one datagram. Returns (reply_source, payload) or (None, None)."""
        self.sock.settimeout(timeout)
        try:
            data, frm = self.sock.recvfrom(4096)
        except OSError:
            return None, None
        if self.direct:
            return f"{frm[0]}:{frm[1]}", data
        return parse_socks_reply(data)

    def exchange(self, ip, port, payload, timeout):
        """One round-trip, correlated by the echoed seq rather than by arrival
        order. Datagrams that are not this request's answer are drained and
        discarded — they are late replies to earlier requests, and consuming
        one as if it were this reply is what produces phantom misdeliveries."""
        want = struct.unpack(">Q", payload[1:9])[0]
        t0 = time.monotonic()
        self.send(ip, port, payload)
        deadline = t0 + timeout
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return None, None, None
            src, body = self.recv(remaining)
            if src is None:
                return None, None, None
            if pong_seq(body) == want:
                return (time.monotonic() - t0) * 1000, src, body


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--rounds", type=int, default=30,
                    help="sustained round-trips per server (default 30)")
    ap.add_argument("--servers", type=int, default=4,
                    help="distinct game servers to hold concurrently (default 4)")
    ap.add_argument("--direct", action="store_true",
                    help="bypass the tunnel (target liveness control)")
    args = ap.parse_args()

    print("resolving game servers to distinct addresses")
    targets = resolve_distinct(GAME_HOSTS)[:args.servers]
    if len(targets) < 2:
        print("VERDICT: FAIL — fewer than 2 distinct servers resolved")
        return 1

    tp = Transport(args.direct)
    mode = "DIRECT (no tunnel)" if args.direct else f"tunnel via {PROXY[0]}:{PROXY[1]}"
    print(f"\nmode: {mode} · single client source port {tp.source_port}\n")

    print(f"[1/2] unconnected ping to {len(targets)} servers from one source port")
    live = []
    for seq, (ip, port, host) in enumerate(targets):
        rtt, src, body = tp.exchange(ip, port, ping_packet(seq), 4.0)
        asked = f"{ip}:{port}"
        if rtt is None:
            print(f"  {host:<26} {asked:<22} | TIMEOUT")
            continue
        info = parse_pong(body)
        name = info[0][:26] if info else "<UNPARSEABLE>"
        players = f"{info[1]}/{info[2]}" if info else "-"
        flag = "OK " if src == asked else "MISDELIVERED"
        print(f"  {host:<26} {asked:<22} | from {str(src):<22} | {rtt:6.1f}ms "
              f"| {flag} | {players:>12} | {name}")
        live.append({"asked": asked, "host": host, "src": src, "info": info})

    if len(live) < 2:
        print(f"\nVERDICT: FAIL — only {len(live)} server(s) answered; "
              "cannot test multi-destination")
        return 1

    misdelivered = [s for s in live if s["src"] != s["asked"]]
    if misdelivered:
        print(f"\n{len(misdelivered)}/{len(live)} replies came from a peer we "
              "never addressed — the session is pinned to one destination.")
        print("VERDICT: MISDELIVERY — this is the bug that breaks games")
        return 2

    corrupt = [s for s in live if s["info"] is None]
    if corrupt:
        print(f"\n{len(corrupt)}/{len(live)} replies arrived but did not decode "
              "as a Bedrock pong — datagrams are being mangled or truncated")
        print("VERDICT: PAYLOAD CORRUPTION")
        return 3

    print(f"\n[2/2] sustained play: {args.rounds} round-trips per server, "
          "interleaved on the same source port")
    stats = {s["asked"]: {"rtts": [], "lost": 0, "wrong_src": 0} for s in live}
    seq = 1000
    for _ in range(args.rounds):
        for s in live:
            ip, port = s["asked"].rsplit(":", 1)
            seq += 1
            rtt, src, body = tp.exchange(ip, int(port), ping_packet(seq), 2.0)
            st = stats[s["asked"]]
            if rtt is None or parse_pong(body) is None:
                st["lost"] += 1
            elif src != s["asked"]:
                st["wrong_src"] += 1
            else:
                st["rtts"].append(rtt)

    print(f"\n  {'server':<26} {'sent':>5} {'loss':>7} {'median':>9} {'jitter':>9}")
    worst_loss, worst_jitter, any_wrong = 0.0, 0.0, 0
    by_asked = {s["asked"]: s for s in live}
    for asked, st in stats.items():
        lost_pct = 100.0 * st["lost"] / args.rounds
        med = statistics.median(st["rtts"]) if st["rtts"] else float("nan")
        # Jitter as mean absolute successive difference — what a game engine's
        # buffer actually has to absorb, unlike a standard deviation.
        jit = (statistics.fmean(abs(b - a) for a, b in
                                zip(st["rtts"], st["rtts"][1:]))
               if len(st["rtts"]) > 1 else 0.0)
        any_wrong += st["wrong_src"]
        worst_loss = max(worst_loss, lost_pct)
        worst_jitter = max(worst_jitter, jit)
        print(f"  {by_asked[asked]['host']:<26} {args.rounds:>5} {lost_pct:>6.1f}% "
              f"{med:>7.1f}ms {jit:>7.1f}ms")

    print()
    if any_wrong:
        print(f"VERDICT: MISDELIVERY — {any_wrong} replies from the wrong peer "
              "during sustained play")
        return 2
    if worst_loss > MAX_LOSS_PCT or worst_jitter > MAX_JITTER_MS:
        print(f"VERDICT: DEGRADED — worst loss {worst_loss:.1f}% "
              f"(limit {MAX_LOSS_PCT}%), worst jitter {worst_jitter:.1f}ms "
              f"(limit {MAX_JITTER_MS}ms). Reachable but not playable.")
        return 4
    print(f"VERDICT: PASS — {len(live)} real game servers held concurrently on "
          f"one source port; worst loss {worst_loss:.1f}%, "
          f"worst jitter {worst_jitter:.1f}ms")
    return 0


if __name__ == "__main__":
    sys.exit(main())
