package wire

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testDial builds a manager dial func that terminates TLS against the self-signed
// test node (skips ECH/pin via nodeOverrideTLS) and counts handshakes so tests
// can assert connection reuse. Mirrors relayFetchWithOverride's dial path.
func testDial(counter *int32) dialFunc {
	return func(ctx context.Context, n *RelayNode, centerHost string) (net.Conn, error) {
		if counter != nil {
			atomic.AddInt32(counter, 1)
		}
		return relayDial(ctx, &RelayRequest{IP: n.IP, Port: n.Port, CenterHost: centerHost}, nodeOverrideTLS())
	}
}

// TestRelayManager_ConcurrentRankAndRecordNoRace proves the manager is safe under
// its designed concurrent-usage model: the mobile plugin offloads relay calls onto
// a fixed thread pool, so multiple Fetch() calls hit the singleton at once —
// rankedSnapshot() (reader) races recordOutcome()/AddNodes() (writers) on the same
// per-node fields (consecFail/latencyEWMA/Score). Must be run with -race. Before the
// fix, the sort comparator read those fields with the lock released → data race.
func TestRelayManager_ConcurrentRankAndRecordNoRace(t *testing.T) {
	m := newRelayManager(nil, time.Second)
	m.AddNodes([]RelayNode{
		{IP: "1.1.1.1", Pin: "p", ECH: "e", Score: 0.9},
		{IP: "2.2.2.2", Pin: "p", ECH: "e", Score: 0.5},
		{IP: "3.3.3.3", Pin: "p", ECH: "e", Score: 0.1},
	})
	ips := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}
	const iters = 2000

	var wg sync.WaitGroup
	// Writers: mutate consecFail/latencyEWMA/successN/failN under the lock.
	for _, ip := range ips {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				n := m.findForTest(ip)
				m.recordOutcome(n, true, 5*time.Millisecond)
				m.recordOutcome(n, false, 0)
			}
		}(ip)
	}
	// Writer: churn Score via AddNodes (descriptor merge under the lock).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			m.AddNodes([]RelayNode{{IP: "1.1.1.1", Pin: "p", ECH: "e", Score: 0.7}})
		}
	}()
	// Readers: rank the pool, which reads the same fields the writers mutate.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				_ = m.rankedSnapshot()
			}
		}()
	}
	wg.Wait()
}

func TestRelayManager_AddNodesIncremental(t *testing.T) {
	m := newRelayManager(nil, time.Second)

	added, total := m.AddNodes([]RelayNode{
		{IP: "1.1.1.1", Pin: "p", ECH: "e"},
		{IP: "2.2.2.2", Pin: "p", ECH: "e"},
	})
	if added != 2 || total != 2 {
		t.Fatalf("first add: added=%d total=%d, want 2/2", added, total)
	}

	// Re-add an existing node + one new: only the new one counts as added.
	added, total = m.AddNodes([]RelayNode{
		{IP: "1.1.1.1", Pin: "p", ECH: "e"},
		{IP: "3.3.3.3", Pin: "p", ECH: "e"},
	})
	if added != 1 || total != 3 {
		t.Fatalf("incremental add: added=%d total=%d, want 1/3", added, total)
	}

	// Invalid descriptors (missing ip/pin/ech) are skipped, not added.
	added, _ = m.AddNodes([]RelayNode{
		{IP: "", Pin: "p", ECH: "e"},
		{IP: "4.4.4.4", Pin: "", ECH: "e"},
		{IP: "5.5.5.5", Pin: "p", ECH: ""},
	})
	if added != 0 {
		t.Fatalf("invalid nodes must be skipped: added=%d, want 0", added)
	}
}

func TestRelayManager_AddNodesPreservesHealth(t *testing.T) {
	m := newRelayManager(nil, time.Second)
	m.AddNodes([]RelayNode{{IP: "1.1.1.1", Pin: "p", ECH: "e"}})

	n := m.findForTest("1.1.1.1")
	if n == nil {
		t.Fatal("node not found after add")
	}
	m.recordOutcome(n, true, 42*time.Millisecond)
	m.recordOutcome(n, false, 0)

	// Re-adding the same IP (descriptor unchanged) must NOT reset accumulated health.
	m.AddNodes([]RelayNode{{IP: "1.1.1.1", Pin: "p", ECH: "e"}})
	n2 := m.findForTest("1.1.1.1")
	if n2.successN != 1 || n2.failN != 1 {
		t.Fatalf("health reset on re-add: successN=%d failN=%d, want 1/1", n2.successN, n2.failN)
	}
}

func TestRelayManager_ReusesConnectionForSameNode(t *testing.T) {
	var dials int32
	host, port := startTLSNode(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok:"+r.URL.Path)
	})
	m := newRelayManager(testDial(&dials), 5*time.Second)
	m.AddNodes([]RelayNode{{IP: host, Port: port, Pin: "p", ECH: "e"}})

	for i := 0; i < 3; i++ {
		resp, err := m.Fetch(context.Background(), &RelayRequest{
			CenterHost: "k2.52j.me", Method: "GET", Path: "/api/x",
		})
		if err != nil {
			t.Fatalf("fetch %d: %v", i, err)
		}
		if resp.Status != 200 {
			t.Fatalf("fetch %d: status=%d", i, resp.Status)
		}
	}
	if got := atomic.LoadInt32(&dials); got != 1 {
		t.Fatalf("expected exactly 1 handshake across 3 requests (keep-alive reuse), got %d", got)
	}
}

func TestRelayManager_SequentialFailover(t *testing.T) {
	// Bad node: the DIAL fails (node unreachable — the link never comes up, so the
	// request is never transmitted). That is the ONLY case Fetch may fail over on,
	// because it is the only case where re-issuing to another node can't double-
	// execute. Fetch must fail over to the good node and succeed.
	goodHost, goodPort := startTLSNode(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "good")
	})
	dial := func(ctx context.Context, n *RelayNode, centerHost string) (net.Conn, error) {
		if n.IP == "node-bad" {
			return nil, errors.New("dial refused") // link down — nothing sent
		}
		return relayDial(ctx, &RelayRequest{IP: goodHost, Port: goodPort, CenterHost: centerHost}, nodeOverrideTLS())
	}
	m := newRelayManager(dial, 5*time.Second)
	// Bad node has the higher cold-start prior → preferred first. Fetch must fail
	// over sequentially to the good node and succeed.
	m.AddNodes([]RelayNode{
		{IP: "node-bad", Pin: "p", ECH: "e", Score: 1.0},
		{IP: "node-good", Pin: "p", ECH: "e", Score: 0.5},
	})

	resp, err := m.Fetch(context.Background(), &RelayRequest{
		CenterHost: "k2.52j.me", Method: "GET", Path: "/",
	})
	if err != nil {
		t.Fatalf("failover fetch: %v", err)
	}
	if resp.Body != "good" {
		t.Fatalf("failover did not reach the good node: body=%q", resp.Body)
	}
	// The bad node accumulated a failure; the good node a success.
	if bad := m.findForTest("node-bad"); bad.consecFail == 0 {
		t.Fatalf("bad node should have recorded a failure")
	}
	if good := m.findForTest("node-good"); good.successN == 0 {
		t.Fatalf("good node should have recorded a success")
	}
}

func TestRelayManager_NoCrossNodeReissueAfterConnect(t *testing.T) {
	// At-most-once guard: the top-ranked node COMPLETES the TLS handshake (link is
	// up), then drops the connection mid-round-trip — so from the client's side the
	// request may already have reached Center. Fetch must NOT re-issue it to the
	// second, healthy node (that would double-execute a non-idempotent POST); it
	// must return an error and leave the healthy node untouched.
	var goodHits int32
	connectFailHost, connectFailPort := startTLSNode(t, func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		_ = conn.Close() // handshake already done → link was up, request delivered
	})
	goodHost, goodPort := startTLSNode(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&goodHits, 1)
		io.WriteString(w, "good")
	})
	realAddr := map[string]struct {
		host string
		port int
	}{
		"node-connect-fail": {connectFailHost, connectFailPort},
		"node-good":         {goodHost, goodPort},
	}
	dial := func(ctx context.Context, n *RelayNode, centerHost string) (net.Conn, error) {
		a := realAddr[n.IP]
		return relayDial(ctx, &RelayRequest{IP: a.host, Port: a.port, CenterHost: centerHost}, nodeOverrideTLS())
	}
	m := newRelayManager(dial, 5*time.Second)
	m.AddNodes([]RelayNode{
		{IP: "node-connect-fail", Pin: "p", ECH: "e", Score: 1.0},
		{IP: "node-good", Pin: "p", ECH: "e", Score: 0.5},
	})

	_, err := m.Fetch(context.Background(), &RelayRequest{
		CenterHost: "k2.52j.me", Method: "POST", Path: "/api/user/ticket", Body: `{"x":1}`,
	})
	if err == nil {
		t.Fatal("expected an error (connected node failed the round-trip), got nil")
	}
	if got := atomic.LoadInt32(&goodHits); got != 0 {
		t.Fatalf("at-most-once violated: request was re-issued to the good node %d time(s)", got)
	}
	// The connected node's link was healthy, so it must NOT be penalized.
	if n := m.findForTest("node-connect-fail"); n.consecFail != 0 {
		t.Fatalf("a link-up round-trip failure must not sink the node: consecFail=%d", n.consecFail)
	}
}

func TestRelayManager_FailoverReachesLaterNodeWithinBudget(t *testing.T) {
	// The two top-ranked nodes are black-holed (dial blocks until the per-node
	// timeout fires — the canonical censor behavior, not a fast reject). The sweep
	// must still reach the third, healthy node inside the overall budget. This is
	// the guard for the per-node-timeout sizing: it must be small enough (relative
	// to the Fetch budget) that consuming it on a black-holed node still leaves
	// room to try the rest of the pool.
	goodHost, goodPort := startTLSNode(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "good")
	})
	const perNode = 150 * time.Millisecond
	dial := func(ctx context.Context, n *RelayNode, centerHost string) (net.Conn, error) {
		if n.IP == "good" {
			return relayDial(ctx, &RelayRequest{IP: goodHost, Port: goodPort, CenterHost: centerHost}, nodeOverrideTLS())
		}
		<-ctx.Done() // black-hole: hang until the per-node timeout cancels the dial
		return nil, ctx.Err()
	}
	m := newRelayManager(dial, perNode)
	m.AddNodes([]RelayNode{
		{IP: "bh1", Pin: "p", ECH: "e", Score: 0.9},
		{IP: "bh2", Pin: "p", ECH: "e", Score: 0.8},
		{IP: "good", Pin: "p", ECH: "e", Score: 0.1},
	})

	// Budget comfortably covers three per-node attempts.
	ctx, cancel := context.WithTimeout(context.Background(), 3*perNode+2*time.Second)
	defer cancel()
	resp, err := m.Fetch(ctx, &RelayRequest{CenterHost: "k2.52j.me", Method: "GET", Path: "/"})
	if err != nil {
		t.Fatalf("failover within budget: %v", err)
	}
	if resp.Body != "good" {
		t.Fatalf("sweep did not reach the healthy node past two black-holed ones: body=%q", resp.Body)
	}
}

func TestRelayManager_PropagatesHeadersMethodBody(t *testing.T) {
	// Concern: relay-fetch must forward ALL headers (esp. Authorization) + method
	// + body verbatim through the manager/http.Transport path — a dropped auth
	// header would 401 an authenticated request. Observed via the echoed response.
	host, port := startTLSNode(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		io.WriteString(w, r.Method+"|"+r.Header.Get("Authorization")+"|"+r.Host+"|"+string(b))
	})
	m := newRelayManager(testDial(nil), 5*time.Second)
	m.AddNodes([]RelayNode{{IP: host, Port: port, Pin: "p", ECH: "e"}})

	resp, err := m.Fetch(context.Background(), &RelayRequest{
		CenterHost: "k2.52j.me",
		Method:     "POST",
		Path:       "/api/user/tickets/unread",
		Headers:    map[string]string{"Authorization": "Bearer tok-123", "X-K2-Client": "kaitu-service/0.4.6"},
		Body:       `{"q":1}`,
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if want := `POST|Bearer tok-123|k2.52j.me|{"q":1}`; resp.Body != want {
		t.Fatalf("header/method/body propagation: got %q want %q", resp.Body, want)
	}
}

func TestRelayManager_RankingReflectsOutcomes(t *testing.T) {
	m := newRelayManager(nil, time.Second)
	m.AddNodes([]RelayNode{
		{IP: "A", Pin: "p", ECH: "e", Score: 1.0},
		{IP: "B", Pin: "p", ECH: "e", Score: 0.1},
	})

	// Cold start: no measurements → higher server prior (A) ranks first.
	if first := m.rankedSnapshot()[0]; first.IP != "A" {
		t.Fatalf("cold ranking: got %q first, want A (higher prior)", first.IP)
	}

	// B proves fast + healthy; A fails repeatedly → B must float to the top.
	m.recordOutcome(m.findForTest("B"), true, 10*time.Millisecond)
	for i := 0; i < 3; i++ {
		m.recordOutcome(m.findForTest("A"), false, 0)
	}
	if first := m.rankedSnapshot()[0]; first.IP != "B" {
		t.Fatalf("post-outcome ranking: got %q first, want B (healthy+measured beats unhealthy)", first.IP)
	}
}
