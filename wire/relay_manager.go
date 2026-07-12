package wire

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// RelayManager owns the client-side authority for control-plane relay: it holds
// the working set of camouflage nodes, their live health, a continuously-updated
// preference ranking, and (via a per-node http.Transport) keep-alive connections
// that are reused across requests. The webapp no longer knows or picks a node —
// it feeds descriptors in via AddNodes and calls Fetch with only the HTTP request.
//
// Selection policy (per design): ONE active host at a time. Fetch tries the
// best-ranked node, reusing its pooled connection (no fresh uTLS handshake per
// request); on failure it fails over SEQUENTIALLY to the next-ranked node. The
// ranking is a fed-back dataset: client-observed latency/success dominates, with
// the server's recommendScore as a cold-start prior only.
//
// Its mutex is a standalone state lock (no nesting with engine/daemon locks). All
// network I/O (client.Do) and Transport.Close happen OUTSIDE the lock.
type RelayManager struct {
	mu         deadlock.Mutex
	nodes      []*relayNodeState
	active     *relayNodeState
	dial       dialFunc
	reqTimeout time.Duration
}

// dialFunc terminates TLS to a node for the given inner-SNI centerHost and
// returns the ready connection. Production = camouflageDial (uTLS+ECH+pin); tests
// inject a self-signed override. Called by each node's Transport.DialTLSContext,
// never under the manager lock.
type dialFunc func(ctx context.Context, n *RelayNode, centerHost string) (net.Conn, error)

// RelayNode is a camouflage-node descriptor fed in from the webapp
// (relay-add-nodes). Score is the server's recommendScore, used only as a
// cold-start ranking prior.
type RelayNode struct {
	IP    string  `json:"ip"`
	Port  int     `json:"port"`
	Pin   string  `json:"pin"`
	ECH   string  `json:"ech"`
	Score float64 `json:"score"`
}

// relayNodeState is the manager's per-node record: descriptor + live health +
// the reusable Transport/Client. Fields are guarded by RelayManager.mu.
type relayNodeState struct {
	RelayNode
	successN    int
	failN       int
	consecFail  int
	latencyEWMA time.Duration
	lastOK      time.Time
	lastFail    time.Time

	transport       *http.Transport
	client          *http.Client
	builtCenterHost string
}

// unhealthyThreshold: a node with this many consecutive failures sinks below all
// nodes that are not (yet) known-bad in the ranking.
const unhealthyThreshold = 3

// linkGot signals, per request, whether an HTTP connection (fresh or reused) was
// obtained before the request failed — i.e. whether the request may have been
// transmitted to Center. It is fed by an httptrace.GotConn hook (see doViaNode).
//
// GotConn is the right signal precisely because it fires synchronously inside the
// RoundTrip that client.Do waits on, AFTER a connection is in hand and BEFORE the
// request bytes are written. So:
//   - a fresh dial that fails, or a black-holed node whose dial is cut short by
//     the per-node timeout → GotConn never fires → link down → the request was
//     never sent → safe to fail over to another node.
//   - a reused keep-alive connection, or a fresh dial that succeeded then dropped
//     mid-round-trip → GotConn fired → the request may have reached Center → Fetch
//     must NOT re-issue it elsewhere (at-most-once).
//
// (httptrace.TLSHandshakeDone is NOT usable here: it does not fire when
// Transport.DialTLSContext is set, because we terminate TLS ourselves.) An
// atomic.Bool makes the read after client.Do returns race-free regardless of which
// transport goroutine invokes the hook.
type linkGot struct{ got atomic.Bool }

// defaultRelayReqTimeout bounds a single per-node attempt (dial+handshake+HTTP).
// Kept tight so a black-holed preferred node fails over quickly: a censored node
// that swallows packets consumes the full per-node timeout before Fetch moves on,
// so this value gates how many nodes the sequential sweep can reach inside the
// overall Fetch budget (RelayFetchJSON's 9s). At 2.5s the sweep reaches ~3 nodes
// (2.5×3 < 9) even when the top-ranked ones are black-holed — the exact failure
// the relay exists to survive; 5s only reached ~1.5. The webapp still wraps the
// whole call in its own relay-then-direct budget.
const defaultRelayReqTimeout = 2500 * time.Millisecond

// defaultRelayManager is the process singleton. camouflageDial does the
// production uTLS(ECH+pin) handshake. Both the mobile appext and the desktop
// daemon call through this one instance in their own process.
var defaultRelayManager = newRelayManager(camouflageDial, defaultRelayReqTimeout)

func newRelayManager(dial dialFunc, reqTimeout time.Duration) *RelayManager {
	if reqTimeout <= 0 {
		reqTimeout = defaultRelayReqTimeout
	}
	return &RelayManager{dial: dial, reqTimeout: reqTimeout}
}

// camouflageDial is the production dial: reuse relayDial (uTLS + ECH + enforced
// pin) via a synthetic RelayRequest carrying the node descriptor + inner SNI.
func camouflageDial(ctx context.Context, n *RelayNode, centerHost string) (net.Conn, error) {
	return relayDial(ctx, &RelayRequest{
		IP: n.IP, Port: n.Port, Pin: n.Pin, ECH: n.ECH, CenterHost: centerHost,
	}, nil)
}

// AddNodes incrementally merges descriptors into the pool, deduped by IP. Known
// nodes keep their accumulated health; a rotated pin/ech refreshes the descriptor
// and invalidates the cached connection. Returns how many were newly added and
// the resulting pool size. Invalid descriptors (missing ip/pin/ech) are skipped.
func (m *RelayManager) AddNodes(in []RelayNode) (added, total int) {
	var toClose []*http.Transport
	m.mu.Lock()
	idx := make(map[string]*relayNodeState, len(m.nodes))
	for _, n := range m.nodes {
		idx[n.IP] = n
	}
	for _, nn := range in {
		if nn.IP == "" || nn.Pin == "" || nn.ECH == "" {
			continue
		}
		if ex, ok := idx[nn.IP]; ok {
			if ex.Pin != nn.Pin || ex.ECH != nn.ECH {
				// Descriptor rotated → drop the stale connection, keep health.
				if ex.transport != nil {
					toClose = append(toClose, ex.transport)
				}
				ex.transport, ex.client, ex.builtCenterHost = nil, nil, ""
				ex.Pin, ex.ECH = nn.Pin, nn.ECH
			}
			if nn.Port != 0 {
				ex.Port = nn.Port
			}
			if nn.Score != 0 {
				ex.Score = nn.Score
			}
			continue
		}
		ns := &relayNodeState{RelayNode: nn}
		m.nodes = append(m.nodes, ns)
		idx[nn.IP] = ns
		added++
	}
	total = len(m.nodes)
	m.mu.Unlock()

	for _, t := range toClose {
		t.CloseIdleConnections()
	}
	return added, total
}

// Fetch performs one control-plane request through the best available node.
// req carries only CenterHost + HTTP fields (node selection is the manager's job).
//
// At-most-once across nodes. Fetch fails over to the next node ONLY when the
// current node's link failed (a fresh dial did not complete → the request was
// never transmitted to Center). If the node connected — whether over a fresh dial
// or a reused keep-alive connection — a subsequent error means the request MAY
// have reached Center (e.g. the response read was reset mid-flight), so Fetch does
// NOT re-issue it to another node; it returns the error and lets the caller's
// single direct fallback decide. This bounds a non-idempotent request to at most
// one relay execution (plus, at the webapp layer, one direct fallback) instead of
// one-per-node. Same-node stale-keep-alive retries remain safe: http.Transport
// only retries when no bytes were written, and GetBody is auto-set from the
// strings.Reader body. A link failure does not double-execute (nothing was sent).
func (m *RelayManager) Fetch(ctx context.Context, req *RelayRequest) (*RelayResponse, error) {
	order := m.rankedSnapshot()
	if len(order) == 0 {
		return nil, &relayError{msg: "relay: no nodes available"}
	}
	var lastErr error
	for _, n := range order {
		if ctx.Err() != nil {
			break
		}
		resp, lat, linkUp, err := m.doViaNode(ctx, n, req)
		if err == nil {
			m.recordOutcome(n, true, lat)
			m.setActive(n)
			slog.Info("DIAG: relay-fetch", "centerHost", req.CenterHost, "method", req.Method, "path", req.Path, "status", resp.Status, "nodeIP", n.IP)
			return resp, nil
		}
		lastErr = err
		if linkUp {
			// Connected, then the round-trip failed: the request may already have
			// reached Center. Do NOT re-issue to another node (at-most-once). The
			// node's link is healthy, so don't penalize it either.
			slog.Warn("DIAG: relay-fetch-fail", "centerHost", req.CenterHost, "path", req.Path, "nodeIP", n.IP, "linkUp", true, "err", err)
			break
		}
		// Link down: the node was unreachable and the request was never sent, so
		// it is safe to penalize it and fail over to the next node.
		m.recordOutcome(n, false, 0)
		slog.Warn("DIAG: relay-fetch-fail", "centerHost", req.CenterHost, "path", req.Path, "nodeIP", n.IP, "linkUp", false, "err", err)
	}
	if lastErr == nil {
		lastErr = &relayError{msg: "relay: all nodes exhausted"}
	}
	return nil, lastErr
}

// doViaNode issues the request over the node's reusable Client and reports whether
// the node's link was usable (see dialAttempt.linkUp). The Transport is built
// lazily (and rebuilt if the inner SNI changed) under the lock; the blocking
// client.Do runs with no lock held. The cached DialTLSContext closure records the
// dial outcome into the per-request *dialAttempt carried on the request context —
// never on shared node state — so concurrent requests don't clobber each other.
func (m *RelayManager) doViaNode(ctx context.Context, n *relayNodeState, req *RelayRequest) (resp *RelayResponse, lat time.Duration, linkUp bool, err error) {
	var stale *http.Transport
	m.mu.Lock()
	if n.transport == nil || n.builtCenterHost != req.CenterHost {
		if n.transport != nil {
			stale = n.transport
		}
		node := n.RelayNode // copy for the closure (immutable descriptor snapshot)
		centerHost := req.CenterHost
		t := &http.Transport{
			DialTLSContext: func(dctx context.Context, _, _ string) (net.Conn, error) {
				return m.dial(dctx, &node, centerHost)
			},
			MaxIdleConns:        4,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
		}
		n.transport = t
		n.client = &http.Client{Transport: t, Timeout: m.reqTimeout}
		n.builtCenterHost = centerHost
	}
	client := n.client
	m.mu.Unlock()

	if stale != nil {
		stale.CloseIdleConnections()
	}

	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if req.Body != "" {
		body = strings.NewReader(req.Body)
	}
	// lg records whether a connection was obtained (GotConn) before any failure.
	lg := &linkGot{}
	trace := &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { lg.got.Store(true) },
	}
	reqCtx := httptrace.WithClientTrace(ctx, trace)
	hreq, err := http.NewRequestWithContext(reqCtx, method, "https://"+req.CenterHost+req.Path, body)
	if err != nil {
		return nil, 0, false, err
	}
	hreq.Host = req.CenterHost
	for k, v := range req.Headers {
		hreq.Header.Set(k, v)
	}

	start := time.Now()
	resp2, err := client.Do(hreq)
	lat = time.Since(start)
	if err != nil {
		// linkUp = a connection was in hand → the request may have been sent.
		return nil, lat, lg.got.Load(), err
	}
	rr, err := captureResponse(resp2)
	if err != nil {
		// Response started but the body read failed → the request was transmitted.
		return nil, lat, true, err
	}
	return rr, lat, true, nil
}

// recordOutcome folds one attempt into a node's live health (EWMA latency on
// success, consecutive-failure count on failure).
func (m *RelayManager) recordOutcome(n *relayNodeState, ok bool, lat time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ok {
		n.successN++
		n.consecFail = 0
		n.lastOK = time.Now()
		if n.latencyEWMA == 0 {
			n.latencyEWMA = lat
		} else {
			// EWMA, alpha=0.3 (integer-safe: new*3 + old*7 / 10).
			n.latencyEWMA = (lat*3 + n.latencyEWMA*7) / 10
		}
	} else {
		n.failN++
		n.consecFail++
		n.lastFail = time.Now()
	}
}

func (m *RelayManager) setActive(n *relayNodeState) {
	m.mu.Lock()
	m.active = n
	m.mu.Unlock()
}

// rankedSnapshot returns the nodes in preference order: healthy before unhealthy;
// among healthy, measured-latency ascending, then higher server prior; unhealthy
// last, least-bad first. Client-observed health dominates the server prior.
func (m *RelayManager) rankedSnapshot() []*relayNodeState {
	// The lock is held across the sort: the comparator reads per-node health
	// fields (consecFail/latencyEWMA/Score) that recordOutcome/AddNodes mutate
	// under the same lock, so releasing before the sort is a data race (multiple
	// Fetch() calls run concurrently — the mobile plugin offloads relay onto a
	// thread pool). The comparator is a pure function of in-memory state — no I/O,
	// no external calls, no nested lock — so holding the state lock is compliant.
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*relayNodeState, len(m.nodes))
	copy(out, m.nodes)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ah, bh := a.consecFail < unhealthyThreshold, b.consecFail < unhealthyThreshold
		if ah != bh {
			return ah // healthy first
		}
		if !ah {
			// both unhealthy → least consecutive failures first
			return a.consecFail < b.consecFail
		}
		// both healthy: measured latency beats unmeasured; lower latency first
		am, bm := a.latencyEWMA > 0, b.latencyEWMA > 0
		if am != bm {
			return am
		}
		if am && a.latencyEWMA != b.latencyEWMA {
			return a.latencyEWMA < b.latencyEWMA
		}
		// unmeasured or equal latency → higher server prior first
		return a.Score > b.Score
	})
	return out
}

// findForTest returns the node state for an IP (test helper; same-package access).
func (m *RelayManager) findForTest(ip string) *relayNodeState {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.nodes {
		if n.IP == ip {
			return n
		}
	}
	return nil
}

// relayError is a small typed error for manager-level failures.
type relayError struct{ msg string }

func (e *relayError) Error() string { return e.msg }
