#!/usr/bin/env python3
"""Test whether a candidate front's NAME is on the GFW's TCP SNI blocklist.

This is spec.md §10 item 1 -- the zero-tolerance gate. If the front's name is
blocklisted, the GFW injects RST on the first packet and every connection dies
before any of Tessera's cryptography is reached.

## THIS MUST RUN FROM INSIDE MAINLAND CHINA

Direction matters and it is not symmetric. On 2026-07-28 we tried to measure
this from outside, by sending ClientHellos INTO China (to CERNET hosts on 443)
with blocklisted SNIs. It does not work:

    www.facebook.com   ok  ok  ok        (3 clean targets)
    twitter.com        ok  ok  ok        (3 clean targets)

Both are unambiguously blocked domains. Six clean readings, zero resets. The
GFW's TLS SNI filtering does not fire on connections initiated from outside
into China, so from there every "not blocked" reading is meaningless.

That attempt also produced a textbook false positive worth remembering: five of
six resets in the first run landed on ONE target host, across five different
SNIs, starting right after a blocked SNI was sent to it. That is residual
blocking -- after a hit the GFW blocks the (src,dst,dport) tuple for minutes --
and it makes a poisoned target read as "everything is blocked". The
poison-check below exists because of that.

## Usage

Run on a machine in mainland China, against an overseas host that listens on
443. Any of our own nodes works.

    python3 sni_gate_probe.py <overseas-ip> [<overseas-ip> ...]

Supply several IPs if you have them: after a hit, a target stays poisoned for
minutes, and rotating is the only way to keep throughput up.

## What this still cannot see

  * Provincial firewalls (Henan et al.) censor traffic LEAVING the province, so
    a single vantage point covers one province and one ISP only. Re-run per
    target province -- spec.md §2 A-tier.
  * The QUIC blocklist is a SEPARATE list (~58k FQDNs, since 2024-04-07). A name
    that passes here has NOT been cleared for the QUIC path.
"""
import socket
import ssl
import sys
import time

# Known-blocked: must RST, or the method is not working and nothing else counts.
POSITIVE = ["www.facebook.com", "twitter.com", "www.youtube.com"]
# Known-reachable from China: must NOT RST. Also used as the poison check.
NEGATIVE = ["www.microsoft.com", "www.bing.com"]
BENIGN = "www.microsoft.com"

CANDIDATES = [
    "s3.amazonaws.com", "storage.googleapis.com", "objects.githubusercontent.com",
    "cdn.jsdelivr.net", "unpkg.com", "cdnjs.cloudflare.com", "dl.google.com",
    "swcdn.apple.com", "updates.cdn-apple.com", "download.windowsupdate.com",
    "dl.delivery.mp.microsoft.com", "www.apple.com", "www.wikipedia.org",
]

RST, OK, TIMEOUT = "RST", "ok", "timeout"


def probe(ip, sni, timeout=8):
    """Send one ClientHello with the given SNI. Verification is off on purpose:
    the SNI deliberately does not match the target host, so a cert check would
    fail for a reason unrelated to what we are measuring. No data is exchanged."""
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    s = socket.socket()
    s.settimeout(timeout)
    try:
        s.connect((ip, 443))
    except (socket.timeout, TimeoutError):
        return TIMEOUT
    except OSError as e:
        return f"conn:{e.errno}"
    try:
        with ctx.wrap_socket(s, server_hostname=sni):
            return OK
    except ConnectionResetError:
        return RST
    except (socket.timeout, TimeoutError):
        return TIMEOUT
    except ssl.SSLError:
        # An alert still means our ClientHello was delivered and answered by the
        # real host. The GFW did not intervene.
        return OK
    except OSError as e:
        return RST if e.errno == 54 else f"err:{e.errno}"
    finally:
        try:
            s.close()
        except OSError:
            pass


def measure(ip, sni, settle=20):
    """One verdict, with the poison check that the 2026-07-28 run lacked.

    A reset only counts as "this SNI is blocklisted" if the SAME target answers
    a benign SNI immediately afterwards. If the benign probe also resets, the
    target is residually blocked from an earlier hit and the reading is void."""
    v = probe(ip, sni)
    if v != RST:
        return v, ""
    if probe(ip, BENIGN) == RST:
        time.sleep(settle)
        return "void", "目标被残留封锁污染，读数作废"
    return RST, ""


def run(names, tag, targets, state):
    for name in names:
        ip = targets[state["i"] % len(targets)]
        state["i"] += 1
        v, note = measure(ip, name)
        state.setdefault(tag, []).append(v)
        print(f"  {tag:8} {name:32} {v:8} {ip:16} {note}", flush=True)
        time.sleep(2)


def main(targets):
    print(f"目标: {', '.join(targets)}\n")
    state = {"i": 0}

    print("== 阳性对照（已知被封，必须 RST）==")
    run(POSITIVE, "对照/封", targets, state)
    print("\n== 阴性对照（已知可达，必须不 RST）==")
    run(NEGATIVE, "对照/通", targets, state)

    hits = state.get("对照/封", []).count(RST)
    misses = state.get("对照/通", []).count(RST)
    print(f"\n阳性 RST {hits}/{len(POSITIVE)}   阴性 RST {misses}/{len(NEGATIVE)}")
    if hits < len(POSITIVE):
        print("→ 阳性对照未全部命中：本观测点测不到 SNI 封锁，后续读数无意义。停。")
        return 1
    if misses:
        print("→ 阴性对照出现 RST：污染未被排除。停。")
        return 1
    print("→ 方法成立。\n")

    print("== 候选 front ==")
    run(CANDIDATES, "候选", targets, state)
    print("\n注：仅覆盖本省本运营商的 TCP 路径。QUIC 是另一张黑名单，未测。")
    return 0


if __name__ == "__main__":
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    sys.exit(main(sys.argv[1:]))
