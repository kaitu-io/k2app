---
title: "k2cc vs BBR: Congestion Control Under Censorship"
date: 2026-03-17
summary: "How k2cc compares to Google BBR in high-loss, high-censorship networks. Why BBR's bandwidth estimation breaks down under censorship-induced packet loss, and how k2cc's censorship-aware design maintains effective throughput."
section: comparison
order: 8.5
draft: false
---

# k2cc vs BBR: Congestion Control Under Censorship

Google BBR (Bottleneck Bandwidth and Round-trip propagation time) is one of the most widely deployed modern congestion control algorithms, powering YouTube, Google Cloud, and other large-scale services. BBR excels in general internet scenarios — but it was not designed for censorship networks.

[k2cc](/k2/k2cc) is a proprietary congestion control algorithm developed by [Kaitu](https://kaitu.io), purpose-built for high-censorship, high-loss network environments. This article compares their real-world performance characteristics.

## BBR's Design Assumptions

BBR operates on two core assumptions:

1. **Packet loss primarily indicates network congestion** — increased loss means the link is saturated
2. **Bandwidth can be accurately estimated through periodic probing** — alternating ProbeRTT and ProbeBW phases

Both assumptions hold in normal internet environments. In censorship networks like the GFW, both break down.

## BBR's Limitations Under Censorship

### Bandwidth Estimation Corruption

The GFW applies approximately **26% probabilistic packet loss** to detected proxy connections (USENIX Security 2023 measured data). This loss is not caused by congestion — it is censorship infrastructure actively dropping packets.

BBR's bandwidth model cannot distinguish congestion loss from censorship loss. Sustained 26% loss causes BBR to:

- **Severely underestimate available bandwidth**: The model incorporates censorship loss as congestion signals, producing estimates far below actual link capacity
- **Enter a low-throughput cycle**: Underestimated bandwidth → reduced sending rate → inability to probe actual capacity

### ProbeRTT Performance Degradation

BBR enters a ProbeRTT phase approximately every 10 seconds, significantly reducing the sending rate to measure minimum RTT. Under censorship:

- The rate reduction during ProbeRTT causes noticeable throughput drops
- RTT measurements are unstable due to censorship-induced jitter
- Post-ProbeRTT recovery requires re-establishing the bandwidth model, extending the recovery period

### Slow Adaptation to Peak-Hour Degradation

Network conditions for users in China degrade significantly during peak hours (8-11 PM) — latency increases, loss rates rise, and bandwidth drops. BBR's probing cycle of approximately 10 seconds responds slowly to these minute-level fluctuations.

## k2cc Design Differences

| Dimension | k2cc | BBR |
|-----------|------|-----|
| **Design target** | Censorship network optimization | General internet optimization |
| **Loss classification** | Distinguishes congestion from censorship loss | Does not classify loss sources |
| **Configuration** | Zero-config, fully automatic | Zero-config, fully automatic |
| **Bandwidth probing** | Continuous real-time probing, fast adaptation | Periodic probing, ~10s per cycle |
| **Censored network throughput** | Maintains effective throughput near link capacity | Bandwidth estimation corrupted, underestimates capacity |
| **Latency control** | RTT-aware + pacing | RTT-aware + ProbeRTT |
| **Rate recovery** | Proactively probes higher rates, fast recovery | Requires re-establishing bandwidth model after ProbeRTT |
| **Fair coexistence** | Coexists peacefully with other traffic | Known to over-consume bandwidth when competing with Cubic |

## Performance Across Scenarios

### Ideal Networks (low loss, low latency)

Both algorithms quickly saturate link bandwidth. BBR performs well. k2cc shows no significant advantage here — censorship-aware mechanisms are not triggered in uncensored environments.

### Cross-Border Normal (1-5% loss, 100-200ms RTT)

BBR still works effectively, though its bandwidth probing is slower than k2cc. k2cc's continuous real-time probing offers an advantage when bandwidth fluctuates.

### Post-GFW Detection (26% probabilistic loss)

This is where the difference is most pronounced. BBR's bandwidth estimation is severely disrupted by sustained 26% loss, yielding throughput far below theoretical link capacity. k2cc's censorship-aware mechanism recognizes these losses as non-congestion signals and maintains sending rates near link capacity.

### Extreme Censorship (50% loss, high latency)

BBR is nearly unable to transmit effectively — its bandwidth model is completely corrupted. k2cc maintains effective throughput through multi-strategy adaptation. While absolute rates decrease, they remain far above BBR's output.

## When BBR Is Sufficient

BBR is an excellent general-purpose congestion control algorithm. If your network has no censorship interference (e.g., dedicated cross-border links, low-loss VPN), BBR is a strong choice.

k2cc's value is in **censored network scenarios** — when firewalls actively drop packets, the fundamental assumptions of traditional algorithms (including BBR) break down. k2cc's censorship-aware mechanism is specifically designed to address this.

## Performance Verification

k2 includes a 14-scenario benchmark test suite. The T7 scenario (GFW 26% probabilistic loss) and T8 scenario (extreme censorship 50% loss) directly demonstrate the difference between k2cc and BBR. See [k2 vs Hysteria2 Congestion Control Comparison](/k2/vs-hysteria2) for the testing framework.

**Detailed quantitative benchmark results are being prepared and will be published in a future release.**

---

## FAQ

**BBR is also adaptive — what's the fundamental difference from k2cc?**

The core difference is loss classification. BBR treats all packet loss as network state signals, with no ability to distinguish congestion from censorship. k2cc automatically identifies packets actively dropped by censorship infrastructure and avoids incorrectly reducing its sending rate.

**Is k2cc slower than BBR on uncensored networks?**

No. On ideal networks, k2cc performs comparably to BBR. The censorship-aware mechanism only changes behavior when non-congestion loss patterns are detected.

**Why does Hysteria2 use BBR as its default congestion control?**

Hysteria2 uses BBR when users don't declare bandwidth, and Brutal (fixed-rate) when they do. Both modes have issues: BBR underestimates bandwidth under censorship, and Brutal requires manual configuration with no loss discrimination. See [k2 vs Hysteria2](/k2/vs-hysteria2) for details.

**Where can I find k2cc's technical details?**

The design principles and core capabilities of k2cc are documented in [k2cc Adaptive Rate Control](/k2/k2cc). The algorithm implementation is original intellectual property of Kaitu.

---

Next: [k2cc Adaptive Rate Control](/k2/k2cc) for k2cc's core capabilities, or [k2 vs Hysteria2](/k2/vs-hysteria2) for a comprehensive three-way comparison of k2cc, Brutal, and BBR.
