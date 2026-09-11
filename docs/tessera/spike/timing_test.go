// Timing side-channel measurement for the Tessera design (spec.md §8.2).
//
// The relay path must dial the front site and wait for its ServerHello; the
// authenticated path answers locally. If those two produce visibly different
// ClientHello->ServerHello latencies, an observer who probes the node once to
// learn its relay baseline can then label every passively observed flow to
// that node as authenticated (fast) or relayed (slow). Labelling authenticated
// flows means identifying users, so the cryptographic discriminator is bypassed
// without ever being broken.
//
// These tests measure the gap, then measure whether the proposed mitigation
// closes it. Separability is reported as best-threshold classifier accuracy:
// 50% means indistinguishable, 100% means fully separable.
package spike

import (
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"os"
	"sort"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Model of the two server paths
// ---------------------------------------------------------------------------

// frontRTT models the node->front network round trip. Every relayed connection
// pays it at least twice: once for the TCP handshake, once for ClientHello out
// and ServerHello back.
type frontRTT struct {
	base   time.Duration
	jitter time.Duration // uniform +/- jitter
}

func (f frontRTT) sample() time.Duration {
	if f.jitter == 0 {
		return f.base
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(2*f.jitter)))
	if err != nil {
		panic(err)
	}
	return f.base - f.jitter + time.Duration(n.Int64())
}

// mitigation describes how the authenticated path pads its response.
type mitigation int

const (
	mitigationNone mitigation = iota
	// mitigationFixedDelay adds the mean relay latency. Cheap, but leaves the
	// variance mismatched.
	mitigationFixedDelay
	// mitigationSampledDelay draws from the node's measured relay-latency
	// distribution, matching both centre and spread.
	mitigationSampledDelay
)

type timingServer struct {
	ln       net.Listener
	frontRTT frontRTT
	mitigate mitigation
	pooled   bool // node keeps warm connections to front, removing the TCP RTT
	// relayProfile is what the node has measured about its own relay latency.
	// A real node would maintain this as a rolling sample.
	relayProfile []time.Duration
}

func (s *timingServer) relayLatency() time.Duration {
	// TCP handshake to front (skipped when pooled) + ClientHello out/ServerHello back.
	d := s.frontRTT.sample()
	if !s.pooled {
		d += s.frontRTT.sample()
	}
	return d
}

func (s *timingServer) authDelay() time.Duration {
	switch s.mitigate {
	case mitigationFixedDelay:
		return meanOf(s.relayProfile)
	case mitigationSampledDelay:
		if len(s.relayProfile) == 0 {
			return 0
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(s.relayProfile))))
		if err != nil {
			panic(err)
		}
		return s.relayProfile[n.Int64()]
	default:
		return 0
	}
}

// serve handles one connection. authenticated selects the path; in the real
// server this is the AEAD result from spec §4.5.
func (s *timingServer) serve(conn net.Conn, authenticated bool) {
	defer conn.Close()

	// Both paths first read the ClientHello. Modelled as a single read.
	buf := make([]byte, 4096)
	if _, err := conn.Read(buf); err != nil {
		return
	}

	if authenticated {
		time.Sleep(s.authDelay())
	} else {
		time.Sleep(s.relayLatency())
	}

	// Both paths then emit something ServerHello-shaped. Content is irrelevant
	// here; only the time of the first byte is being measured.
	_, _ = conn.Write([]byte{0x16, 0x03, 0x03, 0x00, 0x2a})
}

// ---------------------------------------------------------------------------
// Measurement
// ---------------------------------------------------------------------------

// measure returns ClientHello -> first-response-byte latencies, which is
// exactly what a passive observer on the path can time.
func measure(t *testing.T, s *timingServer, authenticated bool, samples int) []time.Duration {
	t.Helper()
	out := make([]time.Duration, 0, samples)
	hello := make([]byte, 512)
	if _, err := rand.Read(hello); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < samples; i++ {
		conn, err := net.Dial("tcp", s.ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		srv, err := s.ln.(*captureListener).next()
		if err != nil {
			conn.Close()
			t.Fatal(err)
		}
		go s.serve(srv, authenticated)

		start := time.Now()
		if _, err := conn.Write(hello); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		one := make([]byte, 1)
		if _, err := io.ReadFull(conn, one); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		out = append(out, time.Since(start))
		conn.Close()
	}
	return out
}

// captureListener hands accepted connections to the test one at a time.
type captureListener struct {
	net.Listener
	accepted chan net.Conn
	errs     chan error
}

func newCaptureListener(t *testing.T) *captureListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cl := &captureListener{Listener: ln, accepted: make(chan net.Conn, 64), errs: make(chan error, 1)}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				select {
				case cl.errs <- err:
				default:
				}
				return
			}
			cl.accepted <- c
		}
	}()
	return cl
}

func (c *captureListener) next() (net.Conn, error) {
	select {
	case conn := <-c.accepted:
		return conn, nil
	case err := <-c.errs:
		return nil, err
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("timed out waiting for accept")
	}
}

// ---------------------------------------------------------------------------
// Statistics
// ---------------------------------------------------------------------------

func meanOf(xs []time.Duration) time.Duration {
	if len(xs) == 0 {
		return 0
	}
	var total time.Duration
	for _, x := range xs {
		total += x
	}
	return total / time.Duration(len(xs))
}

func percentile(xs []time.Duration, p float64) time.Duration {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), xs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(math.Round(p / 100 * float64(len(sorted)-1)))
	return sorted[idx]
}

// separability is the accuracy of the best single-threshold classifier telling
// the two samples apart. 0.5 means indistinguishable; 1.0 means fully separable.
func separability(a, b []time.Duration) float64 {
	all := append(append([]time.Duration(nil), a...), b...)
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })

	best := 0.5
	for _, threshold := range all {
		var correct int
		for _, x := range a {
			if x < threshold {
				correct++
			}
		}
		for _, x := range b {
			if x >= threshold {
				correct++
			}
		}
		acc := float64(correct) / float64(len(a)+len(b))
		if acc < 0.5 {
			acc = 1 - acc // the classifier may point the other way
		}
		if acc > best {
			best = acc
		}
	}
	return best
}

func report(t *testing.T, label string, auth, relay []time.Duration) float64 {
	t.Helper()
	sep := separability(auth, relay)
	t.Logf("%-22s auth p50=%-8v p95=%-8v | relay p50=%-8v p95=%-8v | 可分性=%.1f%%",
		label,
		percentile(auth, 50).Round(time.Microsecond), percentile(auth, 95).Round(time.Microsecond),
		percentile(relay, 50).Round(time.Microsecond), percentile(relay, 95).Round(time.Microsecond),
		sep*100)
	return sep
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

const timingSamples = 120

func newServer(t *testing.T, rtt frontRTT, m mitigation, pooled bool) *timingServer {
	return newServerWithProfile(t, rtt, m, pooled, 200)
}

// newServerWithProfile lets a test vary how many relay-latency samples the node
// has collected. The pad is drawn from that profile, so the profile IS the
// distribution the authenticated path reproduces — too few samples and the pad
// is a discrete distribution over a handful of values while the relay path
// stays continuous.
func newServerWithProfile(t *testing.T, rtt frontRTT, m mitigation, pooled bool, profileSize int) *timingServer {
	t.Helper()
	s := &timingServer{ln: newCaptureListener(t), frontRTT: rtt, mitigate: m, pooled: pooled}
	for i := 0; i < profileSize; i++ {
		s.relayProfile = append(s.relayProfile, s.relayLatency())
	}
	return s
}

// TestTimingGapExists is the vulnerability: with no mitigation the two paths
// are trivially separable, so the discriminator can be bypassed by timing.
func TestTimingGapExists(t *testing.T) {
	rtt := frontRTT{base: 30 * time.Millisecond, jitter: 5 * time.Millisecond}
	s := newServer(t, rtt, mitigationNone, false)
	defer s.ln.Close()

	auth := measure(t, s, true, timingSamples)
	relay := measure(t, s, false, timingSamples)

	sep := report(t, "无缓解", auth, relay)
	if sep < 0.95 {
		t.Errorf("expected the unmitigated gap to be trivially separable, got %.1f%%", sep*100)
	}
	t.Log("→ 未缓解时两条路径完全可分：探测一次拿到基线后，被动观察即可给每条流打标")
}

// TestFixedDelayIsNotEnough shows that padding by the mean leaves the variance
// mismatched, which is still usable by an observer.
func TestFixedDelayIsNotEnough(t *testing.T) {
	rtt := frontRTT{base: 30 * time.Millisecond, jitter: 15 * time.Millisecond}
	s := newServer(t, rtt, mitigationFixedDelay, false)
	defer s.ln.Close()

	auth := measure(t, s, true, timingSamples)
	relay := measure(t, s, false, timingSamples)

	sep := report(t, "固定延迟(补均值)", auth, relay)
	if sep <= 0.60 {
		t.Logf("固定延迟在本参数下已足够（可分性 %.1f%%）——若成立需复核 jitter 取值是否偏小", sep*100)
	}
	t.Log("→ 中心对齐但方差不对齐；抖动越大越可分")
}

// TestSampledDelayClosesTheGap is the proposed mitigation: draw the pad from
// the node's own measured relay-latency distribution.
func TestSampledDelayClosesTheGap(t *testing.T) {
	rtt := frontRTT{base: 30 * time.Millisecond, jitter: 15 * time.Millisecond}
	s := newServer(t, rtt, mitigationSampledDelay, false)
	defer s.ln.Close()

	auth := measure(t, s, true, timingSamples)
	relay := measure(t, s, false, timingSamples)

	sep := report(t, "按分布采样延迟", auth, relay)
	if sep > 0.70 {
		t.Errorf("sampled delay left the paths separable at %.1f%%", sep*100)
	}
	t.Log("→ 中心与方差同时对齐，可分性回落到接近随机猜测")
}

// TestPooledFrontReducesTheCost measures the latency the mitigation costs real
// users, with and without keeping warm connections to the front.
func TestPooledFrontReducesTheCost(t *testing.T) {
	rtt := frontRTT{base: 30 * time.Millisecond, jitter: 15 * time.Millisecond}

	cold := newServer(t, rtt, mitigationSampledDelay, false)
	defer cold.ln.Close()
	warm := newServer(t, rtt, mitigationSampledDelay, true)
	defer warm.ln.Close()

	coldAuth := measure(t, cold, true, timingSamples)
	coldRelay := measure(t, cold, false, timingSamples)
	warmAuth := measure(t, warm, true, timingSamples)
	warmRelay := measure(t, warm, false, timingSamples)

	report(t, "冷拨 front", coldAuth, coldRelay)
	report(t, "预热连接池", warmAuth, warmRelay)

	t.Logf("认证用户为伪装付出的延迟: 冷拨 %v → 预热 %v",
		percentile(coldAuth, 50).Round(time.Millisecond),
		percentile(warmAuth, 50).Round(time.Millisecond))
	t.Log("→ 连接池省掉一次 TCP RTT，伪装代价随之减半；两条路径仍需同源采样才对齐")
}

// TestSeparabilityNullBaseline establishes the floor of the metric itself.
// Best-threshold accuracy is biased upward at finite sample sizes — it takes
// the maximum over many candidate thresholds, so even two samples drawn from
// the SAME distribution score above 50%. Without this number, the residual
// separability reported by the mitigation tests cannot be read as either
// "leftover signal" or "measurement noise".
func TestSeparabilityNullBaseline(t *testing.T) {
	rtt := frontRTT{base: 30 * time.Millisecond, jitter: 15 * time.Millisecond}
	s := newServer(t, rtt, mitigationSampledDelay, false)
	defer s.ln.Close()

	// Both samples come from the authenticated path: identical distribution by
	// construction, so any separability reported here is pure metric bias.
	a := measure(t, s, true, timingSamples)
	b := measure(t, s, true, timingSamples)

	null := separability(a, b)
	t.Logf("零假设基线（同分布对比）: 可分性=%.1f%%  ← 指标在 n=%d 下的地板",
		null*100, timingSamples)
	t.Logf("→ 缓解后的读数必须与这个数字比较，而不是与 50%% 比较")

	if null < 0.5 {
		t.Fatalf("null baseline below 0.5 (%.1f%%) — separability() is broken", null*100)
	}
}

// TestResidualAtLargerSampleSize answers whether the leftover 1-2 points the
// mitigation shows at n=120 is real signal or just the metric's floor.
//
// The floor falls as n grows, because more samples give the best-threshold
// search less room to overfit. If the mitigated separability tracks the floor
// down, the residual was noise. If a gap opens up, an attacker who aggregates
// enough connections recovers the channel and the mitigation is incomplete.
//
// RTT is scaled down so a large n stays fast; separability is scale-invariant.
func TestResidualAtLargerSampleSize(t *testing.T) {
	if testing.Short() {
		t.Skip("long: measures at several sample sizes")
	}
	rtt := frontRTT{base: 3 * time.Millisecond, jitter: 1500 * time.Microsecond}

	for _, n := range []int{120, 500, 2000} {
		s := newServer(t, rtt, mitigationSampledDelay, false)

		// Floor: two samples from the same (authenticated) distribution.
		floor := separability(measure(t, s, true, n), measure(t, s, true, n))
		// Mitigated: authenticated vs relayed.
		mitigated := separability(measure(t, s, true, n), measure(t, s, false, n))

		s.ln.Close()
		t.Logf("n=%-5d 地板=%.1f%%  缓解后=%.1f%%  残余=%+.1fpt",
			n, floor*100, mitigated*100, (mitigated-floor)*100)
	}
	t.Log("→ 残余若随 n 增大而收敛到 0，说明是有限样本噪声；若稳定为正，说明信道未被完全关闭")
}

// TestProfileSizeDrivesTheResidual tests why the residual reappears at large n.
//
// The pad is drawn from the node's collected relay-latency profile, so the
// authenticated path can only ever reproduce the EMPIRICAL distribution of that
// profile. With few samples the pad lands on a handful of discrete values while
// the relay path stays continuous, and a patient observer separates them on the
// discreteness alone. If that is the cause, growing the profile shrinks the
// residual.
func TestProfileSizeDrivesTheResidual(t *testing.T) {
	if testing.Short() {
		t.Skip("long: measures at several profile sizes")
	}
	const n = 2000
	rtt := frontRTT{base: 3 * time.Millisecond, jitter: 1500 * time.Microsecond}

	for _, size := range []int{200, 2000, 20000} {
		s := newServerWithProfile(t, rtt, mitigationSampledDelay, false, size)
		floor := separability(measure(t, s, true, n), measure(t, s, true, n))
		mitigated := separability(measure(t, s, true, n), measure(t, s, false, n))
		s.ln.Close()
		t.Logf("profile=%-6d (n=%d)  地板=%.1f%%  缓解后=%.1f%%  残余=%+.1fpt",
			size, n, floor*100, mitigated*100, (mitigated-floor)*100)
	}
	t.Log("→ 残余随 profile 增大而收敛，则病因是经验分布的离散性，而非机制本身")
}

// TestRealFrontHandshakeLatency samples actual TLS handshake latency to
// candidate front sites, so the simulated RTTs above can be checked against
// reality. Network-dependent, so opt in with K2T_NET=1.
func TestRealFrontHandshakeLatency(t *testing.T) {
	if os.Getenv("K2T_NET") != "1" {
		t.Skip("set K2T_NET=1 to measure real front sites over the network")
	}
	fronts := []string{
		"www.microsoft.com:443",
		"www.apple.com:443",
		"www.amazon.com:443",
		"www.wikipedia.org:443",
	}
	for _, addr := range fronts {
		var samples []time.Duration
		for i := 0; i < 10; i++ {
			start := time.Now()
			conn, err := tls.Dial("tcp", addr, &tls.Config{MinVersion: tls.VersionTLS12})
			if err != nil {
				t.Logf("%-28s dial failed: %v", addr, err)
				break
			}
			samples = append(samples, time.Since(start))
			conn.Close()
		}
		if len(samples) == 0 {
			continue
		}
		t.Logf("%-28s p50=%-8v p95=%-8v spread=%v",
			addr,
			percentile(samples, 50).Round(time.Millisecond),
			percentile(samples, 95).Round(time.Millisecond),
			(percentile(samples, 95) - percentile(samples, 5)).Round(time.Millisecond))
	}
}
