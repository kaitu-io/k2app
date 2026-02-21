---
title: "k2 vs Hysteria2: Congestion Control Comparison"
date: 2026-02-21
summary: "An in-depth comparison of k2's proprietary adaptive congestion control algorithm versus Hysteria2's Brutal fixed-rate sending mechanism, analyzed across four dimensions: packet loss recovery, latency stability, bandwidth utilization, and fairness."
section: comparison
order: 7
draft: false
---

# k2 vs Hysteria2: Congestion Control Comparison

Congestion control is one of the most critical factors determining the performance of a tunnel protocol. Under high packet loss and high latency network conditions, different congestion control strategies produce dramatically different user experiences. This article compares k2 and Hysteria2 across four dimensions.

## Background

**k2** uses a proprietary adaptive congestion control algorithm. It is rate-based and finds the optimal sending rate by performing gradient ascent on a utility function, dynamically responding to changing network conditions.

**Hysteria2** uses a congestion control strategy called Brutal: the user specifies a maximum bandwidth cap, and the protocol sends at that fixed rate regardless of congestion signals.

---

## Dimension 1: Packet Loss Recovery

### k2

k2's proprietary adaptive congestion control algorithm maintains an adaptive loss penalty coefficient. When packet loss is detected, the algorithm distinguishes between congestion-induced loss and non-congestion loss (such as active packet dropping by censorship infrastructure):

- **Adaptive penalty coefficient**: Dynamically adjusted based on observed loss patterns, preventing misclassification of censorship-induced packet drops as network congestion.
- **Aggressive mode**: Enabled for high-censorship network environments, allowing higher sending rates to be maintained under a tolerable rate of non-congestion packet loss.
- **Probing mechanism**: After a rate reduction, periodically attempts higher rates to actively recover available bandwidth.

### Hysteria2

The Hysteria2 Brutal strategy ignores all packet loss signals and sends at a fixed rate. This means:

- No distinction between congestion-induced loss and other types of packet drops.
- No rate backoff; loss is handled entirely by retransmission.
- In high-loss networks, this can lead to retransmission storms that further degrade network conditions.

**Conclusion**: k2's adaptive packet loss handling is significantly more resilient in high-censorship, high-loss environments.

---

## Dimension 2: Latency Stability

### k2

k2's proprietary algorithm incorporates RTT (round-trip time) as an important input variable in its utility function. When buffer queues begin to build up (bufferbloat), rising RTT triggers a rate reduction to suppress latency degradation:

- **RTT awareness**: Real-time monitoring of round-trip time prevents excessive filling of network buffers.
- **Rate-based pacing**: Data packets are spread evenly over time, avoiding burst traffic that causes queue buildup.

### Hysteria2

Hysteria2's fixed-rate sending strategy is insensitive to RTT changes:

- Continuously fills the link at a constant rate, easily causing router buffer buildup (bufferbloat).
- On shared links, latency can fluctuate significantly as other traffic levels change.

**Conclusion**: k2's RTT-aware mechanism provides superior latency stability compared to Hysteria2's fixed-rate approach.

---

## Dimension 3: Bandwidth Utilization

### k2

k2's proprietary algorithm continuously probes for the optimal sending rate through gradient ascent:

- **No manual configuration needed**: The algorithm automatically discovers and tracks the actual available network bandwidth.
- **Probing mechanism**: After rate reductions, periodically attempts higher rates to dynamically recover available throughput.
- **Adapts to dynamic networks**: Performs better on mobile networks or shared links where bandwidth fluctuates frequently.

### Hysteria2

Hysteria2 requires the user to manually specify a target bandwidth:

- **Dependent on user configuration**: If the configured value is below actual available bandwidth, link utilization is suboptimal; if too high, it continuously causes congestion.
- **No automatic probing**: No capability to dynamically track bandwidth changes.
- **Limited use cases**: Works best only when the user can accurately determine the link capacity in advance.

**Conclusion**: k2's automatic bandwidth probing reduces configuration burden and maintains higher effective utilization in dynamic network environments.

---

## Dimension 4: Fairness

### k2

k2's rate-based pacing mechanism demonstrates good fairness when coexisting with other traffic:

- **TCP coexistence**: Rate adapts to avoid actively displacing bandwidth from other connections.
- **Multi-flow stability**: When multiple tunnel connections share a link, bandwidth is distributed relatively equitably across flows.

### Hysteria2

Hysteria2's Brutal strategy has significant fairness problems in multi-flow scenarios:

- **Crowds out other traffic**: Sends at a fixed rate without yielding to other flows, potentially starving TCP connections.
- **Congestion collapse risk**: In multi-flow concurrent scenarios, this can trigger congestion collapse, dramatically reducing overall throughput.

**Conclusion**: k2's rate-adaptive strategy is substantially fairer in multi-flow environments and does not actively disrupt other connections.

---

## Summary Table

| Dimension | k2 (Proprietary Adaptive Algorithm) | Hysteria2 (Brutal) |
|-----------|-------------------------------------|-------------------|
| Packet Loss Recovery | Adaptive penalty coefficient; distinguishes congestion vs. non-congestion loss | Ignores loss signals; relies on retransmission |
| Latency Stability | RTT-aware + rate pacing; suppresses bufferbloat | Fixed rate; prone to queue buildup |
| Bandwidth Utilization | Automatic optimal rate probing; no manual config needed | Requires manual setting; no dynamic probing |
| Fairness | Rate-adaptive; coexists peacefully with other flows | Fixed rate; tends to crowd out other traffic |

---

## Notes and Future Work

The comparisons above are based on algorithmic design analysis. Benchmark data will be published separately when conditions permit.

Full technical details of k2's proprietary adaptive congestion control algorithm will be disclosed on **open-source day**.
