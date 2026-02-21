---
title: Protocol Technical Details
date: 2026-02-21
summary: k2v5 protocol internals — three-layer identity, ECH config forgery, certificate pinning, QUIC and TCP transport layers, proprietary adaptive congestion control.
section: technical
order: 5
draft: false
---

# Protocol Technical Details

This document is intended for readers who want to understand the internals of the k2v5 protocol. For a quick setup, see the [1-Minute Quickstart](quickstart).

## k2v5 URL Format

k2v5 encodes all connection parameters in a single URL:

```
k2v5://UDID:TOKEN@HOST:PORT?ech=ECH_CONFIG&pin=sha256:CERT_HASH&fp=FINGERPRINT&hop=PORT_RANGE
```

| Parameter | Description | Example |
|-----------|-------------|---------|
| `UDID` | Device identifier (used by the server to enforce device limits) | `abc123` |
| `TOKEN` | Authentication token | `tok456` |
| `HOST` | Server IP or hostname | `203.0.113.5` |
| `PORT` | Server port (typically 443) | `443` |
| `ech` | Base64-encoded ECH configuration | `AEX0...` |
| `pin` | SHA-256 hash of the server certificate | `sha256:abc...` |
| `fp` | TLS fingerprint type (chrome/firefox/safari/random) | `chrome` |
| `hop` | UDP port hopping range (optional) | `10000-20000` |

## Three-Layer Identity

A k2v5 connection exposes three layers of observable identity, each with different visibility:

```
Layer        Plaintext  Content
─────────────────────────────────────────────────────────
1. TCP dest  Yes        Server real IP address
2. Outer SNI Yes        cloudflare-ech.com (ECH public_name)
3. Inner SNI No         k2 server hostname (ECH-encrypted)
```

A network observer (ISP, firewall) can see layers 1 and 2. Layer 3 is fully encrypted by ECH and cannot be decrypted without the ECH private key.

## ECH Config Forgery

ECH (Encrypted Client Hello) is the core stealth mechanism of k2v5. k2s does not generate ECH configurations from scratch — it **derives them from real Cloudflare ECH configurations**:

1. Query the DNS HTTPS record for `cloudflare-ech.com` to obtain the current Cloudflare ECH template
2. Copy the `cipher_suites`, `kem_id`, and `public_name` fields verbatim
3. Increment `config_id` (to avoid collision with real Cloudflare configs)
4. Substitute k2s's own HPKE public key

Result: k2 traffic ECH configurations are structurally indistinguishable from ECH traffic directed at real Cloudflare services.

## Certificate and Pinning

k2s uses **self-signed certificates** with no CA dependency.

**Dual certificate design:**
- EC (Elliptic Curve) certificate: for algorithm diversity camouflage
- RSA certificate: compatibility with TLS clients that prefer RSA

**Certificate Pinning:**
- The `pin=sha256:HASH` in the connection URL is the SHA-256 hash of the server's public key
- Clients skip CA chain validation and directly compare the certificate hash
- Self-signed certificates are never submitted to Certificate Transparency (CT) logs, avoiding detection via CT log scanning

## TLS Record Padding

k2s periodically (every 24 hours) downloads the real certificate chain from `cloudflare-ech.com`, measures its TLS Record size distribution, and uses the same padding lengths when sending k2 TLS handshake records. This ensures that k2's handshake traffic characteristics (packet size distribution) match real Cloudflare HTTPS traffic.

## Transport Layer

### QUIC/H3 (Primary)

- HTTP/3 over QUIC
- Native stream multiplexing: multiple concurrent flows over a single QUIC connection
- No head-of-line blocking: a single lost packet does not stall other streams
- Proprietary adaptive congestion control optimizes throughput on high packet-loss networks (cross-border links, mobile data)

### TCP-WebSocket (Fallback)

- Activates automatically when QUIC is blocked by UDP filtering
- smux provides stream multiplexing over a single WebSocket connection
- The switchover is transparent to the application layer — no user action needed

### TransportManager

k2's internal `TransportManager` component presents a unified `Dialer` interface that:

1. Attempts to establish a QUIC connection first
2. Falls back to TCP-WebSocket on QUIC failure
3. Monitors connection health and triggers automatic reconnection

## UDP Port Hopping

When the URL contains a `hop=START-END` parameter, the k2 client randomly selects a UDP port within the specified range and rotates it periodically. This defeats port-based QoS throttling or blocking rules.

```
# Example: random port hopping between 10000 and 20000
k2v5://...@203.0.113.5:443?hop=10000-20000&...
```

## Server-Side ECH Routing

When k2s receives a TLS connection, it inspects the ClientHello:

- **ECH extension present**: Decrypt the inner ClientHello, verify credentials, route to the k2v5 tunnel handler
- **ECH extension absent**: Forward the raw TCP connection transparently to the real host for `public_name` (i.e., actual Cloudflare servers)

Non-ECH connections to k2s receive valid responses from real Cloudflare servers. An automated prober cannot distinguish k2s from a real Cloudflare endpoint.

---

Next: [Stealth Camouflage](stealth) — the threat model and how each mechanism addresses it.
