# K2CC Vivace Congestion Control (wire/k2cc/)

Rate-based congestion control for QUIC, optimized for GFW environments.

Implements `quic-go`'s `CongestionControlEx` interface (extended with `OnCongestionEventEx`).
quic-go auto-detects the extended interface and provides batched ACK+loss data per ACK frame.

## Algorithm

Gradient ascent on utility function: `U = Throughput - α * LossRate * SendRate - β * ΔRtt`

- **Rate-based** (not window-based like Cubic). Pacing via `TimeUntilSend` at `CurrentRate`; cwnd is burst ceiling only (1.2× BDP)
- **Monitor Intervals (MI)**: probe network capacity by sending at `rate ± ε`. MinDuration = 2×RTT for QUIC loss detection visibility
- **AIMD-ε**: Adaptive step size via Additive Increase Multiplicative Decrease on ε. See [AIMD-ε section](#aimd-ε-adaptive-exploration-step-size) below.
- **GFW mode**: α=4.0 tolerates 25% background loss; β=1.0 makes RTT gradient effective
- **Stale loss filtering**: MI tracks `FirstPacketNumber`/`LastPacketNumber`. `OnCongestionEvent` skips loss events where packet number < current MI's `FirstPacketNumber` (belongs to previous MI). Prevents MI boundary artifacts (LossRate > 100%) when loss notifications arrive after MI rotation.
- **LossRate clamp**: `LossRate()` capped at 1.0 — defensive guard against residual stale attribution
- **Zero gradient = no signal**: `gradient == 0` does not update direction counters (neither increase nor decrease). Only clear positive/negative signals drive direction confirmation.
- Reference: PCC Vivace (NSDI '18)

## Key Parameters

- `DefaultMaxRate`: 500 Mbps upper bound
- `defaultEpsilon`: 0.12 (base exploration step size, AIMD-ε adapts within [0.03, 0.25])
- `arcRateEWMAAlpha`: 0.9 (rate EWMA smoothing — high for fast response)
- `gfwAlpha`: 4.0 (GFW loss penalty — tolerates constant 25% background loss before gradient goes flat)
- `defaultBeta`: 1.0 (latency penalty — 10ms ΔRtt = 10 utility signal, same magnitude as loss)
- `arcCwndMultiplier`: 1.2 (burst headroom only — K2CC paces via `TimeUntilSend`, cwnd is burst ceiling not rate control)
- `minAckRate`: 0.95 (minimal ackRate cwnd compensation — K2CC is adaptive, not Brutal-style fixed-rate)
- `arcMinRate`: 5 Mbps (rate floor for all directions)
- `noiseEWMA`: EWMA of RTT coefficient of variation, cold-starts at 1.2 (conservative)
- `slowStartSaturationRatio`: 0.7 (throughput growth < 70% of rate growth × slow start factor → link saturated)
- `bwCruiseGain`: 0.95 (5% below BW — zero congestion loss in cruise)
- `bwProbeUpGain`: 1.10 (10% above BW — detect increases)
- `bwDrainGain`: 0.75 (drain queue + measure BG loss)
- `bwWindowSize`: 8 (one full Cruise+ProbeUp+Drain probe cycle)

## AIMD-ε: Adaptive Exploration Step Size

### Theory

SGD optimal step size theory: `ε* ∝ |R - R*|^(1/3)` where R is current rate, R* is optimal rate. AIMD naturally approximates this schedule — far from optimum AI grows ε, near optimum direction reversals trigger MD to shrink ε.

### Formulas

**Utility gradient**: `∇U = U(MIₙ) - U(MIₙ₋₁)`

**Direction counters**: consecutive same-sign gradients. `confirmed` when counter ≥ 2. Zero gradient does not update counters.

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

### Constants (all derived from `ε₀ = DefaultK2ARCEpsilon = 0.12`)

| Symbol | Constant | Value | Derivation |
|--------|----------|-------|------------|
| ε₀ | `defaultEpsilon` | 0.12 | Base step size |
| ε_min | `epsilonMin` | 0.03 | ε₀ / 4 (convergence precision) |
| ε_max | `epsilonMax` | 0.25 | ε₀ × 2 (exploration ceiling) |
| δ | `epsilonDelta` | 0.03 | AI step size |
| γ | `epsilonDecay` | 0.5 | Standard MD halving |
| z | `epsilonZThreshold` | 1.645 | 95% one-sided z-score |
| α | `noiseEWMAAlpha` | 0.1 | Slow-moving (10% new sample weight) |
| — | `coldStartNoise` | 1.2 | ε₀ × 10 (conservative → early reversals = weak → small ε) |

### Cold Start Behavior

`noiseEWMA` initializes at 1.2 (10× ε₀), making the threshold very high. All early reversals classify as **weak** → ε decreases via MD → conservative small probes. As real RTT CV data arrives (typically 0.05–0.20), noiseEWMA decays toward true noise floor, enabling accurate strong/weak classification.

## Slow Start Saturation Detection

Slow start has 3 exit conditions:
1. **Rate ceiling**: `CurrentRate >= maxRate * 0.95` — no room to grow
2. **Utility drop**: `utility < lastUtility * 0.95` — congestion detected
3. **Throughput saturation**: `throughputGrowth < slowStartFactor * 0.7` — rate doubled but throughput didn't follow → link is full. Sets `CurrentRate = throughput` so gradient ascent starts near optimum.

Condition 3 fixes the core issue: when `maxRate=500M` and actual link is 50M, slow start previously overshot to 500M in 2 MIs (all subsequent MIs app-limited). Now it exits at the actual bandwidth.

## App-Limited Anchor (Stateless)

When MI is app-limited (BytesSent < 50% expected):
1. Compute `anchorRate = T / (1-L)` inline (no persistent EWMA state)
2. Apply `minRate` floor — BW-aware:
   - Transitional (BytesSent < 10KB): `arcMinRate` (5M)
   - BW known (`bwEst.valid()`): `bwEst / (1 - bgLoss)` (actual link capacity)
   - Cold start (no bwEst): `arcMinDownloadRate` (30M) for fast ramp-up
3. Set `CurrentRate = anchorRate`, exit slow start if active
4. Reset direction counters, preserve gradient context (`hasLastUtility = true`)
5. Update `probeBaseRate` to anchor rate

**BW-aware floor**: The 30M cold-start floor caused catastrophic over-sending on GFW-throttled links (~5M capacity) — 85% self-inflicted loss, DATAGRAM delivery dies, UDP relay fails. With BW estimator active, the floor tracks actual link capacity.

## Stable Probing (probeBaseRate)

During gradient ascent, when direction is not yet confirmed (counter < 2), probes alternate around a fixed `probeBaseRate`:
- Even MI: `rate = probeBaseRate * (1 + ε)`
- Odd MI: `rate = probeBaseRate * (1 - ε)`

This prevents EWMA-induced drift: without `probeBaseRate`, alternating `+ε` and `-ε` through `updateRate()` EWMA produces a net ~0.64% decrease per cycle, causing steady rate decay.

`probeBaseRate` is updated only when direction is confirmed (counter ≥ 2) and the rate actually moves.

## Rate-Based Utility (not throughput-based)

Utility function: `U = T - α * L * sendRate - β * ΔRtt` where `sendRate` is MI actual sending rate (`BytesSent*8/Duration`), not `TargetRate`. This ensures cwnd inflation is accounted for in the loss penalty — prevents feedback loop where cwnd sends more than CC rate → loss penalty underestimated → rate increases.

## BW Estimator (BBR-style Bandwidth Anchoring)

Sliding-window max filter over MI throughput (window = 8 MIs = 1 probe cycle). When valid (≥2 samples), replaces PCC gradient ascent with controlled BW-anchored probing:

- **Cruise** (6 MI): `rate = bwEst × 0.95` — 5% below measured BW, zero congestion loss
- **ProbeUp** (1 MI): `rate = bwEst × 1.10` — detect BW increases
- **Drain** (1 MI): `rate = bwEst × 0.75` — drain ProbeUp queue + measure background loss

BG loss detection: Drain phase rate << BW → any loss is pure background (GFW). `bgLossEWMA` corrects bwEst: `effectiveBW = maxThroughput / (1 - bgLoss)`.

BW decrease detection: Cruise throughput < expected × 0.5 → immediate bwEst reset (1 MI recovery).

PCC gradient logic preserved as fallback when bwEst is invalid (cold start, BW regime change).

## Global Ack Rate Tracker

Sliding-window tracker (5-second window, 1-second buckets) recording global ack/loss ratio
from `OnCongestionEventEx`. Independent of per-MI loss attribution.

**cwnd compensation** (minimal): `rateToCwnd` divides by `ackRate` but `clampedAckRate()` floors at 0.95 (max 5% compensation). K2CC is adaptive — it handles loss through utility gradient, not cwnd inflation. Previous Brutal-style floor (0.8, 25% compensation) fought the CC algorithm.

## Observability

Two read paths, both pure (RLock, no state mutation):

- `Snapshot() CCSnapshot` — instantaneous view (SendingRate, LossRate, Throughput, RTT, MinRTT, Cwnd, Mode, MIID, Epsilon, AckRate). Young MI (BytesSent < 100KB) uses `lastCompleted` values to avoid MI boundary artifacts.
- `Summary() CCSummary` — **cumulative** per-sender record (`summary.go`), fed once per completed MI from `processCompletedMI` (`recordCompletedMI` at entry, `recordMIOutcome` deferred at every exit): MI counters (total / app-limited / mode residency / BW-anchored / floor), event counters (BW drop/rise, slow-start saturation, rate probes), byte totals, four fixed-bucket histograms (loss %, RTT ms, target rate Mbps, throughput Mbps), exact `LossMax`/`TputMax`, and current values (`BWEst` bgLoss-corrected, `BGLoss`, `Epsilon`, `MinRTT`, `Mode`). Consumers keep their own previous copy and call `Sub(prev)` for a window; the accumulator never resets. `SenderID` is an epoch from a package counter — `Sub` treats a prev with a different epoch as zero (client reconnects swap the sender), and all subtraction saturates. `LossPercentile/RTTPercentile/RatePercentile/TputPercentile(p)` return the **upper edge** of the bucket holding the ⌈p·n⌉-th sample (`overflow=true` = open +∞ bucket, render with `FormatBucket` as `>500`). Consumers: server `wire/cc_summary.go` (`DIAG: cc-summary`), client `engine/health.go` heartbeat (`cc*` fields, **uplink**).

**Purity rules** (regression-guarded by `observability_test.go`): `MonitorInterval.Throughput()` never writes `IsAppLimited` — app-limited is decided exactly once by `appLimited()` at `processCompletedMI` entry (an observer calling `Throughput()` on the live MI used to flip the controller into the anchor path). `OnCongestionEventEx` writes the ackRate tracker under `mu` (readers hold RLock). Adding a `uint64` counter to `CCSummary` without adding it to `Sub()` fails `TestCCSummary_SubCoversEveryCounterField`.

**Three facts the counters expose** (pinned by tests / verified on the 2026-09-06 canary, not bugs to "fix" in isolation): (1) `BWRises` is structurally 0 — `processCompletedMI` feeds every non-app-limited MI's throughput into the sliding-max estimator *before* `processBWAnchored`, so ProbeUp's `throughput > estimate×1.05` can never hold; bandwidth increases are still tracked by that entry `addSample`, only the event never fires (`TestSummary_BWRiseIsTrackedByEstimatorNotEvent`). (2) The first MI is created on the first `OnPacketSent` with `MinDuration = 2×initialRTT = 200 ms`, and completion is only evaluated inside `OnPacketAcked` — a connection whose ACK flow ends inside that window never completes an MI (`MIs=0`, no cc-summary line). Short connections are silent by construction. (3) `UtilityFunction.MinRTT` starts at `time.Second` and `UpdateMinRTT` runs only on the non-app-limited path of `processCompletedMI` (after the app-limited early return), so an all-app-limited sender reports `MinRTT=1s` for its whole life — in a cc-summary line `minRttMs=1000` means "never measured", not a 1 s RTT (every browsing-only connection on the canary showed it). Trust `minRttMs` only once the connection has had non-app-limited MIs; `bwEst>0` is the practical tell. Moving the update onto the app-limited path would change the ΔRTT utility term, so it is a Phase 2 (θ) decision, not an observability fix.

## Files

- `k2cc.go` — `K2CCSender` struct, MI-based rate control, BW estimator, utility gradient, AIMD-ε, `CCSnapshot`/`Snapshot()`, `Summary()` + the `recordCompletedMI`/`recordMIOutcome` accumulator hooks
- `summary.go` — `CCSummary`, histogram edges, `Sub`, `FormatBucket`, the `SenderID` epoch counter
- `summary_test.go` / `observability_test.go` — histogram/Sub unit tests + reflection guard; Snapshot purity and `-race` mutation tests; accumulator path tests (each mirrors an existing control-path test)
- `k2cc_test.go` — ~68 test functions (convergence, loss tolerance, rate bounds, stale loss filtering, snapshot fallback, AIMD-ε, ackRate tracker, CongestionControlEx, app-limited anchor, stable probing, slow start saturation, BW estimator, BW-anchored probing)
- `k2cc_integration_test.go` — QUIC integration tests: real client+server bulk transfer with UDP relay (delay, loss, rate limiting), MI completion hook for per-MI diagnostics
- `helpers_test.go` — test fixtures

## Testing

```bash
go test ./wire/k2cc/...                                    # all tests (~63)
go test ./wire/k2cc/... -run TestIntegration -v            # integration tests only
go test ./wire/k2cc/... -run TestConvergence_Benchmark -v  # convergence: loss × RTT matrix (17 scenarios)
go test ./wire/k2cc/... -run TestDescent_Benchmark -v      # descent: rate starts above bandwidth (13 scenarios)
```

### Integration Test Architecture

`k2cc_integration_test.go` uses a UDP relay between QUIC client and server to simulate realistic network conditions:
- **Delay**: `delayFunc` abstraction — `stableDelay(d)` for fixed, `jitterDelay(base, maxJitter)` for ±segment-based jitter (100ms segments to avoid QUIC reordering false-positives)
- **Loss**: Configurable packet drop rate on both directions
- **Rate limiting**: Server write rate can be capped to simulate upstream bandwidth
- **MI hook**: `SetMICompletedHook()` collects per-MI data (BytesSent, BytesAcked, duration, throughput, app-limited) for analysis without parsing logs

### Convergence Benchmark (`TestConvergence_Benchmark`)

17 scenarios: 3 RTT profiles × loss matrix (0-35%). Measures convergence speed (MI count) and throughput efficiency (aggregate throughput / goodput). RTT profiles: near (HK 30ms), mid (JP/SG 150ms), far (US/EU 300ms) with ±20% jitter.

### Descent Benchmark (`TestDescent_Benchmark`)

13 scenarios: bandwidth caps (10-100M) × RTT × loss. Verifies algorithm descends from initial rate (150M) to actual bandwidth. Measures descent speed (MI count to ≤ 2× bandwidth) and throughput efficiency.
