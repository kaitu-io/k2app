# Wire Layer Vocabulary

Three-layer model for metrics, naming, and decision ownership.

## Layers

| Layer | Scope | Example Metrics | Prefix |
|-------|-------|-----------------|--------|
| **Wire** | k2 client ↔ k2s server (QUIC/TCP-WS transport) | packet loss (ACK-based), RTT, throughput, cwnd | `Wire*` or none when unambiguous |
| **Relay** | Application traffic through the tunnel to target IPs | UDP relay sent/recv ratio, relay timeouts | `Relay*` |
| **Tunnel** | Overall tunnel health assessment (engine layer) | smoothed wire loss, health level FSM | `Tunnel*` |

## Terminology

- **Datagram Relay**: The QUIC datagram-based path for forwarding user UDP packets. Distinct from QUIC's own wire-layer UDP transport. `ErrDatagramRelayUnhealthy` signals this path has degraded; `datagramRelayHealthy()` evaluates health from consecutive relay timeouts — sessions that sent and never received, and sends shed by the datagram budget; an answered session reaching its idle deadline is not a relay timeout. While unhealthy, `admitSession()` still admits one **trial session** per `relayTrialInterval`; its reply is what makes the path healthy again.

## Key Rule

> **Wire-layer decisions (reconnect, transport fallback) must only be driven by Wire-layer metrics (K2CC packet loss, QUIC handshake failure).** Relay-layer metrics (datagram sent/recv ratio) are observability only.

## Naming Conventions

- `PacketLossRate` — Wire-layer loss from K2CC (ACK-based, per monitor interval)
- `RelayUDPTimeouts` — Relay-layer consecutive timeout count
- `WireFallback` — TCP-WS serving traffic due to QUIC failure
- `handshakeFailStreak` — Consecutive QUIC handshake failures (drives fallback)
- `smoothWireLoss` — EWMA-smoothed wire loss in healthMonitor
- `onReconnect` — healthMonitor callback for full reconnect (the only corrective action)
- `K2CC*` — Congestion Control symbols (formerly K2ARC)
- `CCSnapshot` — Point-in-time CC state (formerly ARCSnapshot)
