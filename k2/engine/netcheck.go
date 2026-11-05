package engine

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/wire"
)

// Network verdict — answers "is the user's own network broken?" before the
// engine blames a node. A broken local network (interface down, no internet,
// captive portal) must never be mistaken for a dead node and trigger a
// failover; the engine holds with the verdict's error instead.

// netVerdict is the outcome of one network judgement.
type netVerdict int

const (
	netIfaceDown  netVerdict = 1 // OS reports no usable interface
	netNoInternet netVerdict = 2 // interface up, nothing on the internet answers
	netCaptive    netVerdict = 3 // something answers, but not what it should (portal / interception)
	netOnline     netVerdict = 4 // the local network reaches the internet
	// netUnknown: no netcheck targets configured (k2r gateway, CLI, an old
	// webapp) and no cheaper layer decided. The engine cannot judge the
	// network, so it does not gate on it: the replace loop behaves as it did
	// before the network verdict existed (Phase B proceeds, exhaustion fails
	// 503). Deliberately not "no internet": a blind hold would block failover
	// on a healthy network.
	netUnknown netVerdict = 5
)

func (v netVerdict) String() string {
	switch v {
	case netIfaceDown:
		return "iface_down"
	case netNoInternet:
		return "no_internet"
	case netCaptive:
		return "captive"
	case netOnline:
		return "online"
	case netUnknown:
		return "unknown"
	default:
		return "unknown(" + strconv.Itoa(int(v)) + ")"
	}
}

// blocksFailover reports whether the verdict says the user's network is
// broken, so the replace loop must hold instead of blaming the node.
// netOnline and netUnknown never block.
func (v netVerdict) blocksFailover() bool {
	return v != netOnline && v != netUnknown
}

// holdError is the error the engine shows while holding on this verdict.
// netOnline / netUnknown return nil: the network is fine or cannot be judged,
// so the fault is taken to lie elsewhere.
func (v netVerdict) holdError() *EngineError {
	switch v {
	case netIfaceDown:
		return newEngineError(ErrCodeNetworkUnavailable, "network interface down")
	case netNoInternet:
		return newEngineError(ErrCodeNoInternet, "no internet access")
	case netCaptive:
		return newEngineError(ErrCodeCaptivePortal, "captive portal or network interception detected")
	default:
		return nil
	}
}

// netEvidence is the cheap, already-known evidence consulted before any
// active probe.
type netEvidence struct {
	IfaceDown    bool      // OS / platform says no usable interface
	LastDirectOK time.Time // last successful direct (non-tunnel) round trip; zero = never
	Now          time.Time
	// SkipPassive ignores LastDirectOK. Set for the pre-exhaust judgement:
	// Phase B can burn through fast-failing candidates in well under
	// passiveFreshness, so a direct dial from just before the outage would
	// otherwise vouch for a network that has since died.
	SkipPassive bool
}

// passiveFreshness is how recent a direct success must be to count as proof
// the network is online without probing.
const passiveFreshness = 10 * time.Second

// checkResult is the outcome of probing one netcheck target.
type checkResult int

const (
	checkMatched       checkResult = 1 // expected status (and body, if configured) received
	checkOtherResponse checkResult = 2 // an HTTP response arrived, but not the expected one
	checkNoResponse    checkResult = 3 // DNS failure, connect failure or timeout
)

func (r checkResult) String() string {
	switch r {
	case checkMatched:
		return "matched"
	case checkOtherResponse:
		return "other"
	case checkNoResponse:
		return "noResp"
	default:
		return "unknown(" + strconv.Itoa(int(r)) + ")"
	}
}

// decideNet judges the network from evidence, escalating to active() only
// when the cheaper layers are inconclusive. Returns the verdict and which
// layer decided it: "iface", "passive", "active" or "no-targets".
//
// Passive evidence counts only when 0 <= Now-LastDirectOK <= passiveFreshness:
// a negative age (wall clock stepped backwards) proves nothing.
//
// Active rules: any matched → online; else any other response → captive;
// else all no-response → no internet. No targets at all (active() returns
// nothing) → unknown: the engine cannot judge the network and does not gate
// failover on it (see netUnknown).
func decideNet(ev netEvidence, active func() []checkResult) (netVerdict, string) {
	if ev.IfaceDown {
		return netIfaceDown, "iface"
	}
	if !ev.SkipPassive && !ev.LastDirectOK.IsZero() {
		if d := ev.Now.Sub(ev.LastDirectOK); d >= 0 && d <= passiveFreshness {
			return netOnline, "passive"
		}
	}
	rs := active()
	if len(rs) == 0 {
		return netUnknown, "no-targets"
	}
	other := false
	for _, r := range rs {
		switch r {
		case checkMatched:
			return netOnline, "active"
		case checkOtherResponse:
			other = true
		}
	}
	if other {
		return netCaptive, "active"
	}
	return netNoInternet, "active"
}

// netcheckTimeout bounds each target's probe.
const netcheckTimeout = 3 * time.Second

// netcheckUserAgent is the only header the checker sets — fixed, carries no
// user, device or version identifier.
const netcheckUserAgent = "k2-netcheck"

// netcheckBodyLimit caps how much of a response body is read when a target
// has no expected body (we only drain enough to finish the response).
const netcheckBodyLimit = 4096

// netChecker actively probes plain-HTTP reachability targets outside the
// tunnel. Stateless after construction; run is safe for concurrent use.
type netChecker struct {
	targets []config.NetcheckTarget
	client  *http.Client
}

// newNetChecker builds a checker that dials through dd (so on mobile the
// socket is protected from the tunnel) and resolves hostnames with resolve
// (the engine's direct DNS client), never the system resolver.
func newNetChecker(targets []config.NetcheckTarget, dd wire.DirectDialer,
	resolve func(ctx context.Context, host string) ([]netip.Addr, error)) *netChecker {
	tr := &http.Transport{
		Proxy:              nil, // never route a probe through an env proxy
		DisableKeepAlives:  true,
		DisableCompression: true, // no Accept-Encoding header
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialResolved(ctx, dd, resolve, network, addr)
		},
	}
	return &netChecker{
		targets: targets,
		client: &http.Client{
			Transport: tr,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// dialResolved resolves addr's host via resolve (IP literals skip it) and
// dials each address in turn through dd until one connects.
func dialResolved(ctx context.Context, dd wire.DirectDialer,
	resolve func(ctx context.Context, host string) ([]netip.Addr, error),
	network, addr string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, err
	}
	var ips []netip.Addr
	if ip, perr := netip.ParseAddr(host); perr == nil {
		ips = []netip.Addr{ip}
	} else {
		ips, err = resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("netcheck: no addresses for " + host)
		}
	}
	dial := (&net.Dialer{}).DialContext // nil dd: stdlib, like core.NewDirectOutbound(nil)
	if dd != nil {
		dial = dd.DialContext
	}
	var lastErr error
	for _, ip := range ips {
		conn, derr := dial(ctx, network, netip.AddrPortFrom(ip.Unmap(), uint16(port)).String())
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

// run probes every target concurrently, each bounded by netcheckTimeout and
// all bounded by ctx. Results and per-target elapsed times are in target order.
func (c *netChecker) run(ctx context.Context) ([]checkResult, []time.Duration) {
	rs := make([]checkResult, len(c.targets))
	ds := make([]time.Duration, len(c.targets))
	var wg sync.WaitGroup
	for i := range c.targets {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start := time.Now()
			rs[i] = c.checkOne(ctx, c.targets[i])
			ds[i] = time.Since(start)
		}(i)
	}
	wg.Wait()
	return rs, ds
}

func (c *netChecker) checkOne(ctx context.Context, t config.NetcheckTarget) checkResult {
	ctx, cancel := context.WithTimeout(ctx, netcheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return checkNoResponse
	}
	req.Header.Set("User-Agent", netcheckUserAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return checkNoResponse
	}
	defer resp.Body.Close()
	if resp.StatusCode != t.Status {
		return checkOtherResponse
	}
	if t.Body == "" {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, netcheckBodyLimit))
		return checkMatched
	}
	// Read one byte past the expected body so a longer body fails the match.
	b, err := io.ReadAll(io.LimitReader(resp.Body, int64(len(t.Body))+1))
	if err != nil || string(b) != t.Body {
		return checkOtherResponse
	}
	return checkMatched
}

// formatCheckResults renders per-target results as "host=result/<ms>ms" pairs
// for the DIAG: netcheck log line (ds may be shorter than rs: no duration).
func formatCheckResults(targets []config.NetcheckTarget, rs []checkResult, ds []time.Duration) string {
	n := len(targets)
	if len(rs) < n {
		n = len(rs)
	}
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		host := targets[i].URL
		if u, err := url.Parse(targets[i].URL); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
		part := host + "=" + rs[i].String()
		if i < len(ds) {
			part += "/" + strconv.FormatInt(ds[i].Milliseconds(), 10) + "ms"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}
