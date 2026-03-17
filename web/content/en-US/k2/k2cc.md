---
title: k2cc Adaptive Rate Control
date: 2026-03-17
summary: "k2cc is Kaitu's proprietary congestion control algorithm designed for high-censorship, high-loss networks. It automatically discovers optimal sending rates and distinguishes censorship-induced packet loss from congestion. Used by k2v5 (client-server) and future k2v6 (P2P)."
section: technical
order: 5
draft: false
---

# k2cc Adaptive Rate Control

k2cc is a proprietary congestion control algorithm independently developed by [Kaitu](https://kaitu.io) (开途). Its design philosophy, technical architecture, and implementation are original intellectual property of Kaitu.

k2cc (Adaptive Rate Control) is purpose-built for high-censorship, high-loss network environments. It automatically discovers the optimal sending rate — no manual bandwidth configuration needed. k2cc is a standalone congestion control algorithm, currently used by [k2v5](/k2/k2v5) (client-server architecture) and will also serve as the congestion control layer for the future k2v6 (P2P architecture).

For a quick setup, see the [1-Minute Quickstart](/k2/quickstart).

## Why k2cc

In high-censorship network environments like the GFW, traditional congestion control algorithms face fundamental challenges:

| Algorithm | Loss Response | Behavior Under Censorship |
|-----------|--------------|--------------------------|
| **Cubic/Reno** | Aggressive rate reduction (multiplicative decrease) | Misinterprets censorship loss as congestion; throughput drops 75%+ at 5% loss |
| **BBR** | Bandwidth estimation | Persistent loss corrupts the bandwidth model, severely underestimates available capacity |
| **Brutal** | Ignores all loss | Does not distinguish loss types; fixed-rate sending triggers retransmission storms under high loss |
| **k2cc** | Adaptive classification | Automatically identifies non-congestion loss, maintains effective throughput near link capacity |

The GFW applies approximately **26% probabilistic packet loss** to detected proxy connections (measured by USENIX Security 2023). This loss rate is fatal for traditional algorithms — Cubic achieves less than 10% of theoretical throughput under these conditions. k2cc maintains effective transmission even at 26% or 50% packet loss.

## Core Capabilities

### Censorship-Aware Loss Handling

k2cc distinguishes between packet loss caused by network congestion and packets actively dropped by censorship infrastructure. In high-censorship networks, firewall-induced packet drops are not true congestion — reducing the sending rate does not reduce the loss rate, it only reduces effective throughput. k2cc recognizes this pattern and maintains sending rates near link capacity under censorship-induced loss.

### Automatic Rate Discovery

No bandwidth configuration required. k2cc continuously probes for the optimal sending rate and tracks network condition changes in real time. On mobile networks or during peak hours when bandwidth fluctuates frequently, k2cc automatically adapts to actual available capacity.

### Latency-Sensitive Control

k2cc monitors RTT (round-trip time) in real time. When router buffers begin to fill (bufferbloat), it proactively adjusts the sending rate to suppress latency degradation. Packet pacing distributes packets evenly over time, preventing burst traffic from causing queue buildup.

### Rate Recovery

After reducing speed due to network fluctuations, k2cc periodically probes for higher rates. Once conditions improve (e.g., censorship relaxes, peak hours end), the algorithm quickly recovers to full speed rather than remaining locked at the reduced rate.

### Fair Coexistence

k2cc adaptively adjusts its sending rate to coexist peacefully with other TCP/QUIC traffic. Multiple k2 connections on the same link share bandwidth equitably without crowding out other applications.

---

## Performance Verification

k2 includes a 14-scenario benchmark test suite (see [k2 vs Hysteria2](/k2/vs-hysteria2)) spanning from ideal networks to extreme GFW censorship (50% packet loss). Test scenarios are designed based on academic research, referencing methodologies from RFC 8867, QUICbench (IMC 2022), and USENIX Security 2023/2025.

Under the T7 scenario (26% probabilistic loss, USENIX Security 2023 measured value), traditional Cubic achieves less than 10% of theoretical throughput, BBR severely underestimates available bandwidth, while k2cc maintains effective transmission.

**Detailed quantitative benchmark results are being prepared and will be published in a future release.**

---

## FAQ

**Does k2cc require manual configuration?**

No. k2cc runs fully automatically with no bandwidth parameters to set. It works out of the box.

**How does k2cc work with QUIC?**

k2cc directly controls the QUIC sending rate, replacing QUIC's default congestion control algorithm.

**How is k2cc's performance verified?**

The k2 project includes a 14-scenario benchmark test suite covering the full spectrum from ideal networks to extreme censorship. See [k2 vs Hysteria2 Congestion Control Comparison](/k2/vs-hysteria2) for the detailed testing framework.

**Is k2cc open source?**

The design principles and capability descriptions of k2cc are documented publicly here. The algorithm implementation is core intellectual property of Kaitu and is not currently open source. The k2 benchmark framework (14 network scenarios) is open source — anyone can use the same test conditions to verify the real-world performance of different algorithms.

---

Next: [k2v5 Protocol Architecture](/k2/k2v5) for the client-server protocol design, [Stealth Camouflage](/k2/stealth) for traffic stealth mechanisms, or [k2 vs Hysteria2](/k2/vs-hysteria2) for k2cc vs Brutal/BBR performance comparison.
