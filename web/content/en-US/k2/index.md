---
title: k2 Protocol Overview
date: 2026-02-21
summary: k2 is Kaitu's proprietary stealth tunnel protocol family. The current version, k2v5, features k2cc adaptive rate control, QUIC+H3 primary transport, TCP-WebSocket fallback, Encrypted Client Hello, and TLS fingerprint mimicry.
section: overview
order: 1
draft: false
---

# k2 Protocol Overview

**k2** is Kaitu's proprietary stealth network tunnel protocol family, designed for high-censorship environments. The protocol evolves across major versions, each representing a generation of core architecture. **k2v5** is the current production version — connection URLs start with `k2v5://`, and all Kaitu clients and the k2 CLI use k2v5 by default.

k2v5 features **k2cc (Adaptive Rate Control)**, a proprietary congestion control algorithm that automatically finds the optimal sending rate in high-loss, high-latency networks — no manual bandwidth configuration needed. It uses **QUIC/HTTP3** as the primary transport, with automatic **TCP-WebSocket** fallback when QUIC is blocked, combined with ECH encrypted SNI and TLS fingerprint mimicry to make tunnel traffic indistinguishable from real HTTPS browsing.

## k2v5 Core Features

### k2cc Adaptive Rate Control

k2cc is k2v5's key differentiator. Unlike traditional congestion control algorithms, k2cc **automatically discovers** the optimal sending rate:

| Capability | k2cc (k2v5) | Traditional (e.g. Brutal) |
|------------|------------|--------------------------|
| Bandwidth config | Fully automatic, zero-config | Manual bandwidth specification |
| Packet loss response | Distinguishes congestion from censorship loss | Ignores all loss signals |
| Latency control | RTT-aware, suppresses bufferbloat | Fixed rate, causes queue buildup |
| Network adaptation | Real-time bandwidth tracking | No dynamic probing |
| Fairness | Coexists peacefully with other traffic | Crowds out other connections |

k2cc's core innovation is **censorship-aware loss handling**: in high-censorship networks, most packet loss comes from firewalls actively dropping packets rather than true congestion. k2cc automatically distinguishes censorship-induced loss from congestion loss, avoiding unnecessary rate reduction and maintaining throughput far above traditional algorithms under GFW-like conditions.

For details, see [k2cc Adaptive Rate Control](/k2/protocol). For performance benchmarks, see [k2 vs Hysteria2](/k2/vs-hysteria2).

### Stealth Transport

k2v5 achieves traffic stealth through four layers of defense:

- **ECH (Encrypted Client Hello)**: Encrypts the real destination hostname inside the TLS handshake; DPI only sees a major CDN's public hostname
- **TLS Fingerprint Mimicry**: Uses uTLS to replicate Chrome/Firefox/Safari TLS handshake signatures
- **Traffic Pattern Matching**: TLS record padding lengths match real Cloudflare server responses exactly
- **Active Probe Resistance**: Non-ECH connections are transparently forwarded to the real website

For details, see [Stealth Camouflage](/k2/stealth).

### Zero-Config Deployment

One command starts the server — it auto-generates all keys and certificates and prints a ready-to-use connection URL. One command connects the client — k2cc automatically finds the optimal rate. No manual configuration needed.

```bash
# Server (30 seconds)
curl -fsSL https://kaitu.io/i/k2s | sudo sh
sudo k2s run

# Client (30 seconds)
curl -fsSL https://kaitu.io/i/k2 | sudo sh
sudo k2 up k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

## Transport Layer

- **QUIC/H3 Primary**: Native multiplexing, no head-of-line blocking, k2cc maintains high throughput on lossy networks
- **TCP-WebSocket Fallback**: Auto-switches when QUIC is blocked; smux provides stream multiplexing
- **Single Port :443**: QUIC and TCP share the same port, minimizing exposure
- **UDP Port Hopping**: `hop=START-END` parameter rotates UDP ports to defeat port-based QoS throttling

## Identity and Authentication

- **k2v5 URL**: All parameters in a single URL: `k2v5://UDID:TOKEN@HOST:PORT?ech=...&pin=...`
- **Three-Layer Identity**: TCP destination IP (plaintext) → Outer SNI (plaintext, CDN public hostname) → Inner SNI (ECH-encrypted)
- **Zero-Config Server**: Auto-generates all keys and certificates on first run, prints a ready-to-use URL

## Quick Navigation

| Document | Description |
|----------|-------------|
| [1-Minute Quickstart](/k2/quickstart) | Start the server and connect in under a minute |
| [k2s Server Deployment](/k2/server) | Detailed server installation and configuration |
| [k2 Client Usage](/k2/client) | Client installation and common commands |
| [k2cc Rate Control](/k2/protocol) | k2cc core capabilities, censorship awareness, auto rate probing |
| [Stealth Camouflage](/k2/stealth) | ECH, TLS fingerprinting, and active probe resistance |
| [k2 vs Hysteria2](/k2/vs-hysteria2) | k2cc vs Brutal/BBR congestion control comparison |
| [k2 vs VLESS+Reality](/k2/vs-reality) | Stealth approach and anti-blocking comparison |

## Supported Platforms

The k2 CLI runs on **Linux** and **macOS**. The Kaitu desktop client (macOS/Windows) and mobile client (iOS/Android) ship with k2 built-in — no separate installation needed.

Visit the [download page](/install) to get the Kaitu client.
