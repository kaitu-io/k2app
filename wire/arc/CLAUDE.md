# K2ARC Vivace Congestion Control (wire/arc/)

Rate-based congestion control for QUIC, optimized for GFW environments.

Implements `quic-go`'s `CongestionControl` interface via `github.com/apernet/quic-go`.

## Algorithm

Gradient ascent on utility function: `U = Throughput - α * LossRate * Throughput - β * ΔRtt`

- **Rate-based** (not window-based like Cubic)
- **Monitor Intervals (MI)**: probe network capacity by sending at `rate ± ε`
- **GFW mode**: low α tolerates non-congestion packet loss (random drops by firewall)
- Reference: PCC Vivace (NSDI '18)

## Key Parameters

- `DefaultMaxRate`: 500 Mbps upper bound
- `DefaultK2ARCEpsilon`: 0.15 (exploration step size — aggressive for fast convergence)
- `DefaultK2ARCBeta`: 0.1 (latency penalty — low for throughput-prioritized tunnel use)
- `arcMinRate`: 10 Mbps (upload floor), `arcMinDownloadRate`: 30 Mbps (download floor)

## Observability

`Snapshot()` returns `ARCSnapshot` (SendingRate, LossRate, Throughput, RTT, MinRTT, Cwnd, Mode, MIID) under `RLock`. Used by `QUICClient.WireStats()` for external metric access.

## Files

- `arc.go` — `K2ARC` struct, MI-based rate control, utility gradient computation, `ARCSnapshot`/`Snapshot()`
- `arcc_test.go` — 25 test functions (convergence, loss tolerance, rate bounds)
- `helpers_test.go` — test fixtures

## Testing

```bash
go test ./wire/arc/...    # 25 tests
```
