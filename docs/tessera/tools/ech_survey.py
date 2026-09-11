#!/usr/bin/env python3
"""Survey real-world ECH deployments via DNS HTTPS (TYPE65) records.

macOS dig 9.10.6 does not know the HTTPS rrtype, so query TYPE65 and parse the
generic \\# <len> <hex> form ourselves.
"""
import subprocess
import re
from concurrent.futures import ThreadPoolExecutor

DOMAINS = [
    # Cloudflare-fronted, popular
    "cloudflare.com", "crypto.cloudflare.com", "cloudflare-ech.com", "www.cloudflare.com",
    "discord.com", "medium.com", "shopify.com", "zendesk.com", "udemy.com",
    "patreon.com", "canva.com", "upwork.com", "digitalocean.com", "gitlab.com",
    "hubspot.com", "namecheap.com", "trustpilot.com", "ted.com", "coinbase.com",
    "nordvpn.com", "proton.me", "1password.com", "stackoverflow.com",
    "producthunt.com", "hackerone.com", "bitwarden.com", "algolia.com",
    # Big tech / other possible ECH deployers
    "google.com", "www.google.com", "youtube.com", "bing.com", "www.bing.com",
    "microsoft.com", "apple.com", "www.apple.com", "amazon.com", "facebook.com",
    "instagram.com", "wikipedia.org", "mozilla.org", "firefox.com", "github.com",
    "fastly.com", "akamai.com", "netflix.com", "spotify.com", "reddit.com",
    "linkedin.com", "x.com", "tiktok.com", "openai.com", "chatgpt.com",
    "anthropic.com", "claude.ai", "vercel.com", "netlify.com", "wordpress.com",
    "wix.com", "squarespace.com", "substack.com", "notion.so", "figma.com",
    # Regional / APAC
    "line.me", "rakuten.co.jp", "naver.com", "kakao.com", "shopee.com",
    "grab.com", "lazada.com", "tokopedia.com",
    # AU / CA / HK plausible for our node regions
    "abc.net.au", "news.com.au", "cbc.ca", "canada.ca", "scmp.com",
    "afr.com", "smh.com.au", "theglobeandmail.com",
]


def query_type65(domain):
    try:
        out = subprocess.run(
            ["dig", "+short", "+timeout=3", "+tries=1", "@1.1.1.1", "-t", "TYPE65", domain],
            capture_output=True, text=True, timeout=10,
        ).stdout
    except Exception:
        return None
    # Generic form:  \# <len> <hex bytes, possibly wrapped across lines>
    m = re.search(r"\\#\s+\d+\s+([0-9A-Fa-f\s]+)", out)
    if not m:
        return None
    return bytes.fromhex(re.sub(r"\s+", "", m.group(1)))


def parse_svcb_params(rdata):
    """Yield (key, value) SvcParams from HTTPS/SVCB RDATA."""
    if len(rdata) < 3:
        return
    p = 2                                   # SvcPriority
    while p < len(rdata) and rdata[p] != 0:  # TargetName (uncompressed labels)
        p += 1 + rdata[p]
    p += 1
    while p + 4 <= len(rdata):
        key = int.from_bytes(rdata[p:p + 2], "big")
        ln = int.from_bytes(rdata[p + 2:p + 4], "big")
        yield key, rdata[p + 4:p + 4 + ln]
        p += 4 + ln


def parse_ech_configs(blob):
    """ECHConfigList -> [(config_id, public_name)]."""
    out, p = [], 2
    while p + 4 <= len(blob):
        ver = int.from_bytes(blob[p:p + 2], "big")
        ln = int.from_bytes(blob[p + 2:p + 4], "big")
        body = blob[p + 4:p + 4 + ln]
        p += 4 + ln
        if ver != 0xfe0d or len(body) < 8:
            continue
        try:
            q = 0
            cid = body[q]; q += 1
            q += 2                                              # kem_id
            pkl = int.from_bytes(body[q:q + 2], "big"); q += 2 + pkl
            csl = int.from_bytes(body[q:q + 2], "big"); q += 2 + csl
            q += 1                                              # max_name_len
            pnl = body[q]; q += 1
            out.append((cid, body[q:q + pnl].decode("utf-8", "replace")))
        except Exception:
            continue
    return out


def probe(domain):
    rdata = query_type65(domain)
    if not rdata:
        return domain, "no-record", []
    for key, val in parse_svcb_params(rdata):
        if key == 5:                                            # ech
            return domain, "ech", parse_ech_configs(val)
    return domain, "no-ech", []


def main():
    with ThreadPoolExecutor(max_workers=16) as ex:
        results = list(ex.map(probe, DOMAINS))

    names, no_ech, no_record = {}, [], []
    for domain, status, configs in results:
        if status == "no-record":
            no_record.append(domain)
        elif status == "no-ech":
            no_ech.append(domain)
        else:
            for cid, pn in configs:
                names.setdefault(pn, []).append((domain, cid))

    print("=" * 72)
    print("真实部署 ECH 的域名 —— 按 public_name 分组")
    print("=" * 72)
    for pn, entries in sorted(names.items(), key=lambda kv: -len(kv[1])):
        cids = sorted({c for _, c in entries})
        print(f"\npublic_name = {pn!r}   ({len(entries)} 个域名命中)")
        print(f"  实际 config_id 取值: {cids}")
        for d, c in sorted(entries):
            print(f"    {d:<30} config_id={c}")

    print(f"\n{'=' * 72}")
    print(f"有 HTTPS 记录但不带 ech= : {len(no_ech)}")
    print(f"  {', '.join(sorted(no_ech))}")
    print(f"\n无 HTTPS 记录            : {len(no_record)}")
    print(f"  {', '.join(sorted(no_record))}")
    print(f"\n不同 public_name 总数     : {len(names)}")


if __name__ == "__main__":
    main()
