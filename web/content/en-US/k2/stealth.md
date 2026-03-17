---
title: Stealth Camouflage
date: 2026-02-21
summary: How k2 stays invisible — ECH encrypted SNI, TLS fingerprint mimicry, traffic characteristic hiding, and active probe resistance. With threat model analysis.
section: technical
order: 7
draft: false
---

# Stealth Camouflage

k2 was built on a single premise: even in networks where deep packet inspection (DPI) and active probing are widely deployed, k2 traffic must be indistinguishable from ordinary HTTPS browsing. This document explains each stealth mechanism and the specific threat it counters.

## Threat Model

k2 addresses four categories of adversaries:

| Adversary | Capability | k2's Counter |
|-----------|------------|--------------|
| **Passive observer** (ISP, carrier) | Records IP, port, and packet sizes of all traffic | ECH hides SNI; traffic characteristics match Cloudflare HTTPS |
| **Active prober** | Sends TLS handshakes to suspicious IPs and inspects responses | Non-ECH connections proxied to real website |
| **IP-SNI cross-checker** | Correlates the destination IP with the SNI in each connection | Outer SNI is always `cloudflare-ech.com`; IP is a generic VPS |
| **CT log scanner** | Scans Certificate Transparency logs to find proxy server certificates | Self-signed certificate, no CA submission, no CT record |

## ECH: Encrypting the SNI

### Why SNI Is Critical

In a standard TLS handshake, the ClientHello message is sent in plaintext. It contains the SNI (Server Name Indication) field, which explicitly names the domain the client wants to reach. A firewall can read the SNI directly — no need to decrypt subsequent traffic — and block the connection.

### How ECH Works

ECH (Encrypted Client Hello) splits the ClientHello into two layers:

- **Outer ClientHello** (plaintext): Contains a decoy public SNI (`cloudflare-ech.com`), formatted to match Cloudflare's ECH configuration structure
- **Inner ClientHello** (ECH-encrypted): Contains the real target hostname, decryptable only by a server holding the ECH private key

```
What DPI sees in the TLS handshake:
  Outer SNI = cloudflare-ech.com  ← publicly visible
  ECH extension = [encrypted blob] ← real SNI is hidden here
```

k2s derives its ECH configuration from a real Cloudflare ECH record. The `cipher_suites`, `kem_id`, and `public_name` fields are copied verbatim from Cloudflare's live DNS HTTPS record. The result is structurally identical to real ECH traffic directed at Cloudflare.

## TLS Fingerprint Mimicry

A TLS fingerprint is derived by analyzing the combination of fields in a ClientHello: supported cipher suites, extension list, elliptic curves, and so on. Many proxy tools are fingerprinted because they use Go's standard `crypto/tls` library, which has a distinctive signature that firewalls can identify and block.

k2 uses the **uTLS** library to replicate real browser TLS fingerprints:

```bash
# Chrome fingerprint (default)
sudo k2 up k2v5://...?fp=chrome

# Firefox fingerprint
sudo k2 up k2v5://...?fp=firefox

# Safari fingerprint
sudo k2 up k2v5://...?fp=safari

# Random (rotates periodically)
sudo k2 up k2v5://...?fp=random
```

## Traffic Characteristic Hiding

Even with a matching SNI and fingerprint, the **packet size distribution** of TLS handshake records can betray a proxy. k2s counters this by periodically downloading the real certificate chain from `cloudflare-ech.com`, measuring its TLS Record sizes, and using identical padding lengths when sending k2 handshake records.

k2s also deploys **dual RSA + EC certificates**. Some detection heuristics flag VPS servers that only present EC certificates; the RSA + EC combination matches the behavior of real CDN infrastructure.

## Active Probe Resistance

Active probing means a censor sends connection requests directly to a suspicious server and studies the response. A server that returns an error, speaks an unusual protocol, or drops the connection immediately is flagged as a proxy.

k2s handles this as follows:

1. Inspect the ClientHello of every incoming TLS connection
2. If the ClientHello **has an ECH extension**: decrypt the inner ClientHello, verify credentials, route to the k2v5 tunnel handler
3. If the ClientHello **has no ECH extension**: forward the raw TCP connection transparently to the real server for `public_name` (actual Cloudflare IP, resolved from DNS)

A prober connecting without ECH receives a genuine response from a real Cloudflare server. There is no observable difference between k2s and a legitimate Cloudflare endpoint.

## Certificate Pinning and CT Log Avoidance

Traditional VPN and proxy tools typically obtain TLS certificates from a public CA. CA-issued certificates are recorded in Certificate Transparency (CT) logs — public databases that can be automatically scanned to discover and block proxy servers.

k2's approach:

- Uses **self-signed certificates** — no CA involved at any step
- Clients trust the specific certificate via the `pin=sha256:HASH` value in the connection URL, bypassing the CA trust chain entirely
- Self-signed certificates are never submitted to CT logs, leaving no publicly scannable record

## UDP Port Hopping

Some networks apply QoS rate-limiting or outright blocking to specific UDP ports. k2 supports hopping across a specified port range:

```
k2v5://...@server:443?hop=10000-20000&...
```

The client rotates its UDP port periodically, rendering fixed-port filtering rules ineffective.

## FAQ

**Does active probe resistance actually work?**

Yes. When the GFW sends a TLS connection without ECH to k2s, the server transparently forwards it to the real CDN. The GFW receives a legitimate HTTPS response — identical to connecting directly to that CDN. Automated probing scripts cannot distinguish k2s from a real CDN server. VLESS+Reality has a similar mechanism, but its TLS Session ID authentication data has been identified by academic research (USENIX Security 2025).

**Can TLS fingerprints be detected?**

Extremely unlikely. k2 uses uTLS to byte-level replicate real browser ClientHello characteristics. JA3/JA4 fingerprint analysis tools cannot distinguish k2 traffic from real Chrome/Firefox/Safari traffic. This is more robust than VLESS+Reality's fingerprint approach, which relies on borrowing real website certificates.

**What if the GFW blocks the ECH protocol entirely?**

Blocking ECH would affect all CDN services using ECH (Cloudflare, Google, etc.), causing massive collateral damage. The GFW's current strategy targets ECH key distribution via DNS. k2's countermeasure: the `ech=...` URL parameter contains the ECH key directly, completely bypassing DNS-based interference.

**Are self-signed certificates secure?**

Fully secure. k2 uses certificate pinning (`pin=sha256:HASH`) — the client directly verifies the server certificate's SHA-256 hash without relying on CA trust chains. Self-signed certificates never enter Certificate Transparency logs, avoiding detection via CT scanning — a stealth advantage over CA-issued certificates.

---

For the underlying implementation details, see [k2v5 Protocol Architecture](/k2/k2v5) and [k2cc Adaptive Rate Control](/k2/k2cc).
