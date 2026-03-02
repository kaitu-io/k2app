# K2ARC Vivace Congestion Control (wire/arc/)

Rate-based congestion control for QUIC, optimized for GFW environments.

Implements `quic-go`'s `CongestionControl` interface via `github.com/apernet/quic-go`.

## Algorithm

Gradient ascent on utility function: `U = Throughput - α * LossRate * Throughput - β * ΔRtt`

- **Rate-based** (not window-based like Cubic)
- **Monitor Intervals (MI)**: probe network capacity by sending at `rate ± ε`
- **GFW mode**: low α tolerates non-congestion packet loss (random drops by firewall)
- **Stale loss filtering**: MI tracks `FirstPacketNumber`/`LastPacketNumber`. `OnCongestionEvent` skips loss events where packet number < current MI's `FirstPacketNumber` (belongs to previous MI). Prevents MI boundary artifacts (LossRate > 100%) when loss notifications arrive after MI rotation.
- **LossRate clamp**: `LossRate()` capped at 1.0 — defensive guard against residual stale attribution
- Reference: PCC Vivace (NSDI '18)

## Key Parameters

- `DefaultMaxRate`: 500 Mbps upper bound
- `DefaultK2ARCEpsilon`: 0.15 (exploration step size — aggressive for fast convergence)
- `DefaultK2ARCBeta`: 0.1 (latency penalty — low for throughput-prioritized tunnel use)
- `arcMinRate`: 10 Mbps (upload floor), `arcMinDownloadRate`: 30 Mbps (download floor)

## Observability

`Snapshot()` returns `ARCSnapshot` (SendingRate, LossRate, Throughput, RTT, MinRTT, Cwnd, Mode, MIID) under `RLock`. Used by `QUICClient.WireStats()` for external metric access. Young MI (BytesSent < 100KB) uses `lastCompleted` values to avoid MI boundary artifacts (throughput/loss spikes from incomplete data).

## Files

- `arc.go` — `K2ARC` struct, MI-based rate control, utility gradient computation, `ARCSnapshot`/`Snapshot()`
- `arcc_test.go` — 29 test functions (convergence, loss tolerance, rate bounds, stale loss filtering, snapshot fallback)
- `helpers_test.go` — test fixtures

## Testing

```bash
go test ./wire/arc/...    # 29 tests
```
