# K2ARC Vivace Congestion Control (wire/arc/)

Rate-based congestion control for QUIC, optimized for GFW environments.

Implements `quic-go`'s `CongestionControlEx` interface (extended with `OnCongestionEventEx`).
quic-go auto-detects the extended interface and provides batched ACK+loss data per ACK frame.

## Algorithm

Gradient ascent on utility function: `U = Throughput - α * LossRate * Throughput - β * ΔRtt`

- **Rate-based** (not window-based like Cubic)
- **Monitor Intervals (MI)**: probe network capacity by sending at `rate ± ε`
- **AIMD-ε**: Adaptive step size via Additive Increase Multiplicative Decrease on ε. See [AIMD-ε section](#aimd-ε-adaptive-exploration-step-size) below.
- **GFW mode**: α=5.0 balances GFW loss tolerance with congestion sensitivity; β=1.0 makes RTT gradient effective
- **Stale loss filtering**: MI tracks `FirstPacketNumber`/`LastPacketNumber`. `OnCongestionEvent` skips loss events where packet number < current MI's `FirstPacketNumber` (belongs to previous MI). Prevents MI boundary artifacts (LossRate > 100%) when loss notifications arrive after MI rotation.
- **LossRate clamp**: `LossRate()` capped at 1.0 — defensive guard against residual stale attribution
- Reference: PCC Vivace (NSDI '18)

## Key Parameters

- `DefaultMaxRate`: 500 Mbps upper bound
- `DefaultK2ARCEpsilon`: 0.10 (base exploration step size, AIMD-ε adapts within [0.02, 0.20])
- `GFWK2CCAlpha`: 5.0 (GFW loss penalty — high enough for congestion gradient, tolerates constant 5% GFW loss)
- `DefaultK2CCBeta`: 1.0 (latency penalty — 10ms ΔRtt = 10 utility signal, same magnitude as loss)
- `arcMinRate`: 10 Mbps (upload floor), `arcMinDownloadRate`: 30 Mbps (download floor)
- `noiseEWMA`: EWMA of RTT coefficient of variation, cold-starts at 1.0 (conservative)

## AIMD-ε: Adaptive Exploration Step Size

### Theory

SGD optimal step size theory: `ε* ∝ |R - R*|^(1/3)` where R is current rate, R* is optimal rate. AIMD naturally approximates this schedule — far from optimum AI grows ε, near optimum direction reversals trigger MD to shrink ε.

### Formulas

**Utility gradient**: `∇U = U(MIₙ) - U(MIₙ₋₁)`

**Direction counters**: consecutive same-sign gradients. `confirmed` when counter ≥ 2.

**RTT Coefficient of Variation** (UDP-native noise estimator, no TCP HoL pollution):

```
CV = σ(RTT) / μ(RTT)    where σ, μ computed over RTT samples within one MI
```

**Noise baseline** (EWMA, updated only during building phase `counter < 2`):

```
noiseEWMA ← noiseEWMA × (1 - α) + CV × α     α = 0.1
```

**Signal classification threshold** (z-test at 95% confidence):

```
threshold = z × noiseEWMA × max(R / 1Mbps, 1.0)     z = 1.645
```

`R / 1Mbps` normalizes the gradient to utility scale (utility is in Mbps-order units).

### Rules

| # | Condition | Action | Formula |
|---|-----------|--------|---------|
| ① | Direction confirmed (counter ≥ 2) | Additive Increase | `ε ← ε + δ` |
| ② | Reversal + `|∇U| ≤ threshold` | Multiplicative Decrease (weak/noise) | `ε ← ε × γ` |
| ③ | Reversal + `|∇U| > threshold` | Fast Recovery (strong/regime change) | `ε ← ε₀` |
| ④ | Building (counter = 1, no reversal) | Hold | `ε ← ε` |

Clamp: `ε ∈ [ε_min, ε_max]`

### Constants (all derived from `ε₀ = DefaultK2ARCEpsilon = 0.10`)

| Symbol | Constant | Value | Derivation |
|--------|----------|-------|------------|
| ε₀ | `DefaultK2ARCEpsilon` | 0.10 | Base step size |
| ε_min | `epsilonMin` | 0.02 | ε₀ / 5 (convergence precision) |
| ε_max | `epsilonMax` | 0.20 | ε₀ × 2 (exploration ceiling) |
| δ | `epsilonDelta` | 0.02 | = ε_min (one AI step = one precision unit) |
| γ | `epsilonDecay` | 0.5 | Standard MD halving |
| z | `epsilonZThreshold` | 1.645 | 95% one-sided z-score |
| α | `noiseEWMAAlpha` | 0.1 | Slow-moving (10% new sample weight) |
| — | `coldStartNoise` | 1.0 | ε₀ × 10 (conservative → early reversals = weak → small ε) |

### Cold Start Behavior

`noiseEWMA` initializes at 1.0 (10× ε₀), making the threshold very high. All early reversals classify as **weak** → ε decreases via MD → conservative small probes. As real RTT CV data arrives (typically 0.05–0.20), noiseEWMA decays toward true noise floor, enabling accurate strong/weak classification.

## Throughput Anchor + Ceiling Hold

Fixes app-limited blindness when CC target rate R >> link bandwidth B.

### Problem

Slow start doubles rate past link bandwidth → app-limited detection (BytesSent < 50% expected) → `processCompletedMI` returns early → CC never works. Users with 100M/200M links permanently stuck at overshoot rate.

### Throughput Anchor

When app-limited MI detected:
1. Estimate bandwidth: `B_est = T / (1-L)` (EWMA, α=0.3)
2. Set `CurrentRate = bwEstimate` directly (bypass rate EWMA)
3. Exit slow start → decision mode
4. Reset direction counters

Convergence: 2-3 MIs (60-90ms at 30ms RTT).

### Ceiling Hold

At bandwidth ceiling, utility gradient ≈ 0 → probing alternates without confirmation → rate drifts. When `|gradient| < bwEstimate × 0.05` AND `throughput ≥ bwEstimate × 0.9`: hold rate, skip direction update.

### Dynamic α Clamping

With α=5.0, when loss > 20%: `1-αL < 0` → utility inverts → CC minimizes throughput. Fix: `α_eff = min(α, 0.8/L)` ensures `(1-αL) ≥ 0.2`.

## Global Ack Rate Tracker

Sliding-window tracker (5-second window, 1-second buckets) recording global ack/loss ratio
from `OnCongestionEventEx`. Independent of per-MI loss attribution. Observability only
(debug logging via `CCSnapshot.AckRate`) — does not drive rate decisions.

## Observability

`Snapshot()` returns `CCSnapshot` (SendingRate, LossRate, Throughput, RTT, MinRTT, Cwnd, Mode, MIID, Epsilon, AckRate, BwEstimate) under `RLock`. Used by `QUICClient.WireStats()` for external metric access. Young MI (BytesSent < 100KB) uses `lastCompleted` values to avoid MI boundary artifacts (throughput/loss spikes from incomplete data).

## Files

- `arc.go` — `K2ARC` struct, MI-based rate control, utility gradient computation, AIMD-ε, `ARCSnapshot`/`Snapshot()`
- `arcc_test.go` — ~50 test functions (convergence, loss tolerance, rate bounds, stale loss filtering, snapshot fallback, AIMD-ε, ackRate tracker, CongestionControlEx)
- `helpers_test.go` — test fixtures

## Testing

```bash
go test ./wire/arc/...    # ~50 tests
```
