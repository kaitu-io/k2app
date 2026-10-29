// Powered experiment for spec.md §13.4: does a timing residual survive once an
// attacker aggregates many connections?
//
// The socket-based harness in timing_test.go answers this at n=120 but goes
// noisy at n=2000 — single runs there swung between +6.8 and -3.0 points while
// the floor itself wandered, so it supported no conclusion either way. This
// file drops sockets and sleeps and models the latencies directly, which buys
// enough repetitions to put a confidence interval on the answer.
//
// Dropping OS and network noise is deliberate and conservative: real jitter
// lands on BOTH paths and adds common entropy, which pushes separability down.
// A channel that is closed in this noise-free model stays closed with noise, so
// this is an upper bound on what the attacker can do — not a best case.
//
// The relay distribution is log-normal. Real front handshake latencies measured
// in timing_test.go are long-tailed (p50 55-98ms against p5-p95 spreads of
// 100-314ms), which uniform jitter does not represent.
package spike

import (
	"math"
	"math/rand"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// Latency model
// ---------------------------------------------------------------------------

// relayModel is a log-normal node->front latency in milliseconds.
type relayModel struct {
	mu    float64 // log-space mean
	sigma float64 // log-space stddev
	shift float64 // fixed floor, ms
}

// realisticFront approximates www.apple.com as measured: p50 ~63ms with a long
// upper tail reaching ~370ms at p95.
var realisticFront = relayModel{mu: math.Log(45), sigma: 0.55, shift: 18}

func (m relayModel) sample(r *rand.Rand) float64 {
	return m.shift + math.Exp(m.mu+m.sigma*r.NormFloat64())
}

func (m relayModel) samples(r *rand.Rand, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = m.sample(r)
	}
	return out
}

// padStrategy is how the authenticated path picks its delay.
type padStrategy struct {
	name string
	// profileSize is how many relay samples the node has collected. Zero means
	// perfect knowledge: draw straight from the true distribution. That is not
	// implementable, but it is the correct null — see runResidual.
	profileSize int
	// smooth turns the profile from a discrete set of collected values into a
	// continuous kernel density estimate over them. Without it the pad can only
	// ever land on one of profileSize exact values while the relay path stays
	// continuous, and that discreteness is itself separable.
	smooth bool
}

// authSamples produces the latencies an observer would time on the
// authenticated path under this strategy.
func (p padStrategy) authSamples(r *rand.Rand, m relayModel, n int) []float64 {
	if p.profileSize == 0 {
		return m.samples(r, n)
	}
	profile := m.samples(r, p.profileSize)

	// Silverman's rule for the kernel bandwidth.
	var bw float64
	if p.smooth {
		bw = 1.06 * stddev(profile) * math.Pow(float64(len(profile)), -0.2)
	}

	out := make([]float64, n)
	for i := range out {
		v := profile[r.Intn(len(profile))]
		if p.smooth {
			v += r.NormFloat64() * bw
			if v < m.shift {
				v = m.shift // latency cannot fall below the fixed floor
			}
		}
		out[i] = v
	}
	return out
}

func stddev(xs []float64) float64 {
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(len(xs))
	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	return math.Sqrt(ss / float64(len(xs)-1))
}

// ---------------------------------------------------------------------------
// Separability, O(n log n)
// ---------------------------------------------------------------------------

// bestThresholdAccuracy is the accuracy of the best single-threshold classifier
// separating a from b. The O(n^2) version in timing_test.go is fine at n=120
// and far too slow at n=20000, so this sweeps a sorted merge instead.
func bestThresholdAccuracy(a, b []float64) float64 {
	type pt struct {
		v   float64
		isA bool
	}
	all := make([]pt, 0, len(a)+len(b))
	for _, v := range a {
		all = append(all, pt{v, true})
	}
	for _, v := range b {
		all = append(all, pt{v, false})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v < all[j].v })

	total := float64(len(a) + len(b))
	// Threshold below everything: no a counted as "a", all b counted as "b".
	aBelow, bBelow := 0, 0
	best := float64(len(b)) / total

	for i := 0; i < len(all); i++ {
		if all[i].isA {
			aBelow++
		} else {
			bBelow++
		}
		// Only evaluate at genuine value boundaries.
		if i+1 < len(all) && all[i+1].v == all[i].v {
			continue
		}
		correct := float64(aBelow + (len(b) - bBelow))
		acc := correct / total
		if acc < 0.5 {
			acc = 1 - acc
		}
		if acc > best {
			best = acc
		}
	}
	if best < 0.5 {
		best = 0.5
	}
	return best
}

// ---------------------------------------------------------------------------
// Statistics over repetitions
// ---------------------------------------------------------------------------

type interval struct{ mean, lo, hi float64 }

// meanCI is the mean with a 95% confidence interval (normal approximation;
// R=30 is comfortably enough for it).
func meanCI(xs []float64) interval {
	n := float64(len(xs))
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / n

	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	sd := math.Sqrt(ss / (n - 1))
	half := 1.96 * sd / math.Sqrt(n)
	return interval{mean, mean - half, mean + half}
}

// ---------------------------------------------------------------------------
// Experiment
// ---------------------------------------------------------------------------

const repetitions = 30

// runStrategy repeats the measurement R times and returns the CI of the raw
// separability an attacker would achieve.
func runStrategy(seed int64, m relayModel, p padStrategy, n int) interval {
	r := rand.New(rand.NewSource(seed))
	vals := make([]float64, 0, repetitions)
	for i := 0; i < repetitions; i++ {
		vals = append(vals, bestThresholdAccuracy(p.authSamples(r, m, n), m.samples(r, n)))
	}
	return meanCI(vals)
}

// metricBias is the null: with perfect knowledge the pad is drawn from the true
// distribution, so auth and relay ARE the same distribution and any measured
// separability is pure finite-sample bias in the statistic.
//
// This must be the null rather than each strategy's own same-distribution
// floor. A small profile makes the pad discrete, which inflates that floor —
// so comparing a strategy against its own floor builds the vulnerability into
// the null and hides it. That mistake reversed the reading of profile=200 on
// the first pass: it looked like the safest row and is in fact the worst.
func metricBias(seed int64, m relayModel, n int) interval {
	return runStrategy(seed, m, padStrategy{"perfect", 0, false}, n)
}

// TestResidualPowered is the experiment spec.md §10 item 8 asks for.
//
// For each row: is the achievable separability above the metric bias at the
// same n? If the CIs do not overlap and the strategy sits higher, an attacker
// aggregating that many connections recovers real signal.
func TestResidualPowered(t *testing.T) {
	if testing.Short() {
		t.Skip("long: 30 repetitions per configuration")
	}

	strategies := []padStrategy{
		{"完美知识(不可实现,即零假设)", 0, false},
		{"profile=200", 200, false},
		{"profile=2000", 2000, false},
		{"profile=20000", 20000, false},
		{"profile=200 + 核平滑", 200, true},
		{"profile=2000 + 核平滑", 2000, true},
	}

	for _, n := range []int{2000, 20000} {
		bias := metricBias(99, realisticFront, n)
		t.Logf("")
		t.Logf("=== 攻击者聚合 n=%d 条连接，每配置重复 %d 次 ===", n, repetitions)
		t.Logf("零假设(指标偏置) = %.2f%% [%.2f, %.2f]", bias.mean*100, bias.lo*100, bias.hi*100)
		t.Logf("%-28s %-24s %s", "填充策略", "可分性 (95% CI)", "超出零假设")

		for i, p := range strategies {
			got := runStrategy(int64(1000+i), realisticFront, p, n)
			excess := got.mean - bias.mean
			verdict := "落在零假设内"
			if got.lo > bias.hi {
				verdict = "★ 显著高于零假设 —— 存在可利用信号"
			}
			t.Logf("%-28s %5.2f%% [%5.2f, %5.2f]   %+5.2fpt  %s",
				p.name, got.mean*100, got.lo*100, got.hi*100, excess*100, verdict)
		}
	}
}

// TestProfileMustScaleWithAttackerBudget tests the pattern the first run
// suggested: a profile that is adequate at one observation budget stops being
// adequate at a larger one, because what the attacker is detecting is the
// sampling error of the node's empirical distribution, and more observations
// resolve a smaller error.
//
// If that holds there is no profile size that is simply "enough" — the required
// size tracks a budget we do not control, so the mitigation degrades rather
// than closes.
func TestProfileMustScaleWithAttackerBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	for _, n := range []int{2000, 20000, 200000} {
		bias := metricBias(99, realisticFront, n)
		t.Logf("")
		t.Logf("攻击者 n=%-7d  零假设=%.2f%% [%.2f, %.2f]", n, bias.mean*100, bias.lo*100, bias.hi*100)
		for _, mult := range []int{1, 10, 100} {
			size := n * mult
			got := runStrategy(int64(n+mult), realisticFront, padStrategy{"", size, false}, n)
			verdict := "落在零假设内"
			if got.lo > bias.hi {
				verdict = "★ 可利用"
			}
			t.Logf("   profile = %3d×n = %-8d  可分性=%.2f%% [%.2f, %.2f]  超出 %+.2fpt  %s",
				mult, size, got.mean*100, got.lo*100, got.hi*100, (got.mean-bias.mean)*100, verdict)
		}
	}
	t.Log("")
	t.Log("→ 若 profile=1×n 始终可利用而 10×n 始终安全，则所需规模随攻击者预算线性增长")
}

// TestPerfectKnowledgeHasNoResidual is the control. Drawing the pad straight
// from the true distribution must leave no residual; if it does, the harness
// itself is biased and every other row is meaningless.
func TestPerfectKnowledgeHasNoResidual(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	a := metricBias(7, realisticFront, 20000)
	b := metricBias(8, realisticFront, 20000)
	t.Logf("零假设自比对: %.3f%% [%.3f, %.3f] vs %.3f%% [%.3f, %.3f]",
		a.mean*100, a.lo*100, a.hi*100, b.mean*100, b.lo*100, b.hi*100)
	if a.lo > b.hi || b.lo > a.hi {
		t.Fatalf("two independent draws of the null disagree — the harness is unstable, " +
			"every row in TestResidualPowered is unreadable")
	}
}
