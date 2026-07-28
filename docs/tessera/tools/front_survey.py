#!/usr/bin/env python3
"""Score candidate front sites for Tessera (spec.md §14.4, open question 4).

The revised criterion is NOT "big and famous". §14.4 replaced it with "a site
whose normal users already hold long, high-volume connections", and §5's L4 row
adds a second axis nobody had quantified: how anomalous is it for that SNI to
appear on an IP the censor did not expect?

Both are measurable, so measure them instead of arguing:

  A. 流量类别   — does the host serve large objects over keep-alive connections?
                  Proxy for "sustained connections are normal here".
  B. IP 集合    — how many distinct IPs and /24s serve this name, and do
                  resolvers disagree? A name spread over thousands of addresses
                  makes "SNI on an unexpected IP" weak evidence. A single-homed
                  name makes it decisive.
  C. 借壳兼容性 — TLS 1.3, reachable, ALPN, cert SAN breadth.

What this tool CANNOT measure, and what therefore cannot be settled here:

  * Whether the domain is blocked or SNI-filtered inside the censor's network.
    That is a HARD gate — a front whose name is itself filtered makes every
    connection fail on the first packet — and it needs a vantage point inside
    that network. See spec.md §14.4.
  * Reachability from the node. Measured here from a developer laptop, which is
    the wrong place. Re-run from a node before committing to a list.
"""
import re
import socket
import ssl
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor

RESOLVERS = ["1.1.1.1", "8.8.8.8", "9.9.9.9", "208.67.222.222"]

# Grouped by the hypothesis each group tests. The homepages are controls: §14.4
# predicts they FAIL the traffic-class axis, and this is where that prediction
# gets checked rather than asserted.
CANDIDATES = [
    # --- 控制组：§14.4 预测不合格（正常连接=取一次页面然后闲置） ---
    ("control", "www.apple.com", "/"),
    ("control", "www.microsoft.com", "/"),
    ("control", "www.wikipedia.org", "/"),

    # --- CDN / 下载端点：正常用法就是长时大流量 ---
    ("cdn", "swcdn.apple.com", "/"),
    ("cdn", "updates.cdn-apple.com", "/"),
    ("cdn", "download.windowsupdate.com", "/"),
    ("cdn", "dl.delivery.mp.microsoft.com", "/"),
    ("cdn", "cdn.jsdelivr.net", "/npm/vue@3/dist/vue.global.js"),
    ("cdn", "unpkg.com", "/react@18/umd/react.production.min.js"),
    ("cdn", "cdnjs.cloudflare.com", "/ajax/libs/jquery/3.7.1/jquery.min.js"),
    ("cdn", "dl.google.com", "/"),

    # --- 对象存储：长连接 + 大对象 ---
    ("storage", "s3.amazonaws.com", "/"),
    ("storage", "storage.googleapis.com", "/"),
    ("storage", "objects.githubusercontent.com", "/"),

    # --- 大型平台（多为被封锁，仅作 IP 集合对照） ---
    ("platform", "www.youtube.com", "/"),
    ("platform", "www.bing.com", "/"),
]


def resolve(domain, resolver):
    try:
        out = subprocess.run(
            ["dig", "+short", "+timeout=3", "+tries=1", f"@{resolver}", domain, "A"],
            capture_output=True, text=True, timeout=10,
        ).stdout
    except Exception:
        return set()
    return {l.strip() for l in out.splitlines()
            if re.fullmatch(r"\d+\.\d+\.\d+\.\d+", l.strip())}


def ip_spread(domain):
    """Distinct IPs and /24s across resolvers, plus resolver disagreement."""
    per_resolver = {}
    with ThreadPoolExecutor(max_workers=len(RESOLVERS)) as ex:
        for r, ips in zip(RESOLVERS, ex.map(lambda r: resolve(domain, r), RESOLVERS)):
            per_resolver[r] = ips
    allips = set().union(*per_resolver.values()) if per_resolver else set()
    slash24 = {".".join(ip.split(".")[:3]) for ip in allips}
    nonempty = [s for s in per_resolver.values() if s]
    # disagree = no single IP is returned by every resolver that answered
    disagree = bool(nonempty) and not set.intersection(*nonempty)
    return len(allips), len(slash24), disagree


def probe_tls(domain, path):
    """TLS version, ALPN, cert SAN count, and the size of one fetched object."""
    # Offer ONLY http/1.1: with h2 negotiated, an HTTP/1.1 request reads back
    # binary frames and every size below becomes noise. Cost the first run.
    ctx = ssl.create_default_context()
    ctx.set_alpn_protocols(["http/1.1"])
    try:
        raw = socket.create_connection((domain, 443), timeout=8)
    except Exception as e:
        return {"err": f"connect: {type(e).__name__}"}
    try:
        with ctx.wrap_socket(raw, server_hostname=domain) as s:
            cert = s.getpeercert()
            sans = len([v for k, v in cert.get("subjectAltName", ()) if k == "DNS"])
            info = {
                "tls": s.version(),
                "alpn": s.selected_alpn_protocol() or "-",
                "sans": sans,
            }
            req = (f"GET {path} HTTP/1.1\r\nHost: {domain}\r\n"
                   "User-Agent: Mozilla/5.0\r\nAccept: */*\r\n"
                   "Connection: keep-alive\r\n\r\n").encode()
            s.sendall(req)
            head = b""
            while b"\r\n\r\n" not in head and len(head) < 65536:
                chunk = s.recv(4096)
                if not chunk:
                    break
                head += chunk
            status = head.split(b"\r\n", 1)[0].decode(errors="replace") if head else "-"
            m = re.search(rb"[Cc]ontent-[Ll]ength:\s*(\d+)", head)
            hdr = head.split(b"\r\n\r\n")[0].lower()
            info["status"] = status.replace("HTTP/1.1 ", "")[:22]
            info["clen"] = int(m.group(1)) if m else -1
            info["chunked"] = b"chunked" in hdr
            return info
    except Exception as e:
        return {"err": f"tls: {type(e).__name__}"}
    finally:
        try:
            raw.close()
        except Exception:
            pass


def survey(entry):
    group, domain, path = entry
    ips, nets, disagree = ip_spread(domain)
    t = probe_tls(domain, path)
    return group, domain, ips, nets, disagree, t


def main():
    print(f"{'类别':<9} {'域名':<32} {'IP数':>5} {'/24数':>6} {'分歧':>5} "
          f"{'TLS':<8} {'ALPN':<9} {'SAN':>5} {'状态':<24} {'对象字节':>10}")
    print("-" * 130)
    with ThreadPoolExecutor(max_workers=8) as ex:
        for group, domain, ips, nets, disagree, t in ex.map(survey, CANDIDATES):
            if "err" in t:
                print(f"{group:<9} {domain:<32} {ips:>5} {nets:>6} {'是' if disagree else '否':>5} "
                      f"{t['err']}")
                continue
            print(f"{group:<9} {domain:<32} {ips:>5} {nets:>6} {'是' if disagree else '否':>5} "
                  f"{t['tls']:<8} {t['alpn']:<9} {t['sans']:>5} {t['status']:<24} "
                  f"{t['clen'] if t['clen'] >= 0 else ('chunked' if t['chunked'] else '-'):>10}")
    print()
    print("注：本机测量。'该域名在审查网络内是否被 SNI 过滤' 无法在此测得，而它是硬门槛（§14.4）。")
    print("    '从节点可达' 亦需在节点上复跑。")


if __name__ == "__main__":
    sys.exit(main())
