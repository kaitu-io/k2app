# TODO: IPv6 Reachability Detection & Provider Layer Control

## Background

In k2v5 multi-IP support (Happy Eyeballs), server DNS may return both A (IPv4) and AAAA (IPv6) records. IPv4 is always present, IPv6 is optional. When IPv6 records exist, it indicates the server supports IPv6, but the **network path** between client and server may not be IPv6-reachable (e.g., client ISP doesn't support IPv6, or intermediate routers drop IPv6).

## Problem

If the provider layer (TUN device) accepts IPv6 packets but the wire layer cannot deliver them (IPv6 path unreachable), packets will be silently dropped or cause connection timeouts. This degrades user experience — especially for apps that prefer IPv6 when AAAA records are available.

## Proposed Solution

Detect IPv6 reachability at startup and disable IPv6 at the provider layer if unreachable:

1. **Probe Phase** (during wire connection setup):
   - Attempt a short QUIC handshake to the server's IPv6 address (if available)
   - Timeout: 3-5 seconds
   - If handshake fails → mark IPv6 as unreachable

2. **Provider Layer Control**:
   - If IPv6 unreachable → configure TUN device to drop/ignore IPv6 packets
   - `sing-tun` supports `inet6Address` configuration — omit it to disable IPv6
   - Alternatively, filter at the DNS middleware level: suppress AAAA responses when IPv6 is unreachable

3. **Periodic Re-check** (optional):
   - Re-probe IPv6 every N minutes (e.g., 30 min)
   - If IPv6 becomes reachable → re-enable at provider layer
   - This handles mobile users switching between WiFi (IPv6) and cellular (IPv4-only)

## Design Considerations

- This is a **provider-layer concern**, not a wire protocol concern
- k2v5 wire protocol already supports both IPv4 and IPv6 addresses in stream/datagram framing
- The decision to enable/disable IPv6 should be transparent to the wire layer
- DNS middleware could also participate: if IPv6 disabled, filter out AAAA records before returning to apps

## Scope

- **Layer**: Provider (`provider/`) + DNS middleware (`core/dns/`)
- **Not in scope for k2v5 wire protocol design** — wire layer handles whatever addresses it receives
- **Priority**: Low — implement after core k2v5 wire layer is working

## References

- `provider/tun_unix.go` — TUN device configuration (sing-tun)
- `core/dns/middleware.go` — DNS pipeline (could filter AAAA)
- k2v5 design: `docs/plans/2026-02-11-k2v5-composite-protocol-design.md` §19 Multi-IP Support
