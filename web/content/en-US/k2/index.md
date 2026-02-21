---
title: k2 Protocol Overview
date: 2026-02-21
summary: k2 is Kaitu's proprietary stealth tunnel protocol. QUIC+H3 primary transport, TCP-WebSocket fallback, Encrypted Client Hello, TLS fingerprint mimicry, certificate pinning.
section: getting-started
order: 1
draft: false
---

# k2 Protocol Overview

k2 is Kaitu's proprietary stealth network tunnel protocol, designed for use in high-censorship environments. It uses **QUIC/HTTP3** as its primary transport and automatically falls back to **TCP-WebSocket** when QUIC is blocked.

## Core Features

### Stealth Transport

- **ECH (Encrypted Client Hello)**: The real destination hostname (SNI) is encrypted inside the ECH extension of the TLS handshake. DPI systems only see the public hostname `cloudflare-ech.com`
- **TLS Fingerprint Mimicry**: Using the uTLS library, k2 mimics the TLS handshake of a real Chrome browser, making traffic indistinguishable from ordinary HTTPS browsing
- **Self-Signed Certificate + Pinning**: The server uses a self-signed certificate. Clients verify it via SHA-256 hash pinning — no CA trust chain required, and no Certificate Transparency log exposure
- **Active Probe Resistance**: Connections without ECH are transparently forwarded to the real website, so probers receive a valid HTTPS response

### Transport Layer

- **QUIC/H3 Primary**: Native QUIC multiplexing, low latency, excellent performance on lossy networks
- **TCP-WebSocket Fallback**: Automatic switchover when QUIC is blocked; smux multiplexes streams over a single WebSocket connection
- **Single Port :443**: QUIC and TCP share the same port, minimizing the attack surface
- **UDP Port Hopping**: The `hop=START-END` parameter causes the client to randomly rotate UDP ports, defeating port-based QoS throttling
- **Proprietary Adaptive Congestion Control**: Optimized for high packet-loss networks (mobile data, cross-border links)

### Identity and Authentication

- **k2v5 URL**: All connection parameters are encoded in a single URL: `k2v5://UDID:TOKEN@HOST:PORT?ech=...&pin=...`
- **Three-Layer Identity**: TCP destination IP (plaintext) → Outer SNI (plaintext, `cloudflare-ech.com`) → Inner SNI (ECH-encrypted)
- **Zero-Config Server**: On first run, k2s auto-generates all keys and certificates and prints a ready-to-use connection URL

## Quick Navigation

| Document | Description |
|----------|-------------|
| [1-Minute Quickstart](/k2/quickstart) | Start the server and connect in under a minute |
| [k2s Server Deployment](/k2/server) | Detailed server installation and configuration |
| [k2 Client Usage](/k2/client) | Client installation and common commands |
| [Protocol Technical Details](/k2/protocol) | Deep dive into the k2v5 protocol implementation |
| [Stealth Camouflage](/k2/stealth) | ECH, TLS fingerprinting, and active probe resistance |

## Supported Platforms

The k2 command-line client runs on **Linux** and **macOS**. The Kaitu desktop client (macOS/Windows) and mobile client (iOS/Android) ship with k2 built-in — no separate installation needed.

Visit the [download page](/install) to get the Kaitu client.
