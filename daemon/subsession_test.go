package daemon

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
)

// sessionHTTPClient mirrors how resolveSubscriptions builds a subSession's
// HTTP stack — shared Transport between initial Fetch and refreshLoop. Tests
// pass in the httptest-derived TLS config so self-signed certs are trusted.
func sessionHTTPClient(t *testing.T, tlsCfg *tls.Config) (*http.Transport, *http.Client) {
	t.Helper()
	clone := tlsCfg.Clone()
	clone.MinVersion = tls.VersionTLS12
	tr := &http.Transport{TLSClientConfig: clone}
	return tr, &http.Client{Timeout: 5 * time.Second, Transport: tr}
}

// TestSubSession_CloseReleasesHTTPIdleConns is the regression test for the
// F2 goroutine leak. Before the fix, each subSession constructed its own
// http.Transport without ever calling CloseIdleConnections — the pooled
// persistConn readLoop goroutines stayed alive past subSession.Close(),
// leaking one goroutine per up/down cycle in production.
//
// This test creates a subSession backed by a TLS httptest server, runs a
// Fetch (which primes an idle persistConn), closes the session, and
// asserts that the goroutine count settles back to its pre-fetch level.
// goleak in TestMain provides a second, stronger guard for leaks that
// escape this measurement window.
func TestSubSession_CloseReleasesHTTPIdleConns(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(struct {
			Tunnels []config.Tunnel `json:"tunnels"`
			Refresh int             `json:"refresh"`
		}{
			Tunnels: []config.Tunnel{{URL: "k2v5://t.example:443", Weight: 1}},
			Refresh: 0,
		})
	}))
	defer srv.Close()

	sub, err := config.NewSubscription(
		"k2subs://u:p@"+srv.Listener.Addr().String()+"/api",
		t.TempDir(),
	)
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}

	// The test server uses a self-signed cert. Mirror how daemon builds its
	// client but install httptest's cert trust so the fetch actually succeeds.
	tr := &http.Transport{
		TLSClientConfig: srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone(),
	}
	// Sanity: match daemon's MinVersion.
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{}
	}
	tr.TLSClientConfig.MinVersion = tls.VersionTLS12

	sess := &subSession{
		sub:           sub,
		httpTransport: tr,
		httpClient:    &http.Client{Timeout: 5 * time.Second, Transport: tr},
	}

	// Baseline goroutine count before fetch.
	baseline := runtime.NumGoroutine()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := sub.Fetch(ctx, sess.httpClient); err != nil {
		cancel()
		t.Fatalf("Fetch: %v", err)
	}
	cancel()

	// After fetch, http.Transport has a pooled persistConn + readLoop goroutine.
	// Close must release it.
	sess.Close()

	// persistConn.readLoop exits asynchronously after CloseIdleConnections;
	// give it a bounded window to wind down.
	deadline := time.Now().Add(2 * time.Second)
	var final int
	for {
		final = runtime.NumGoroutine()
		if final <= baseline+1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if final > baseline+1 {
		t.Errorf("goroutine leak: baseline=%d final=%d (delta=%d, want ≤ baseline+1)",
			baseline, final, final-baseline)
	}
}

// TestSubSession_RefreshLoopClean exercises the production-shaped flow:
// N concurrent subSessions, each running StartRefresh with a 1s interval
// against a TLS httptest server. After letting the refresh loop fire
// several times, Close() is invoked and we assert no persistConn /
// refresh goroutines survive. Two guards:
//
//   - goroutine count snapshot: bounded window after Close
//   - goleak.VerifyTestMain (in testmain_test.go) catches anything that
//     escapes this window
//
// This test covers three sharper risks than the single-session test:
//  1. refresh loop actually re-uses the shared Transport (no per-refresh leak)
//  2. concurrent Close of multiple sessions doesn't race on Transport cleanup
//  3. Fetches that are in flight when ctx cancels still release idle conns
func TestSubSession_RefreshLoopClean(t *testing.T) {
	const N = 3

	var fetchCalls atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCalls.Add(1)
		// refresh=1 second → refreshLoop fires ~1Hz
		one := 1
		_ = json.NewEncoder(w).Encode(struct {
			Tunnels []config.Tunnel `json:"tunnels"`
			Refresh *int            `json:"refresh,omitempty"`
		}{
			Tunnels: []config.Tunnel{{URL: "k2v5://t.example:443", Weight: 1}},
			Refresh: &one,
		})
	}))
	defer srv.Close()

	tlsCfg := srv.Client().Transport.(*http.Transport).TLSClientConfig
	baseline := runtime.NumGoroutine()

	sessions := make([]*subSession, 0, N)
	ctx, cancel := context.WithCancel(context.Background())

	for i := 0; i < N; i++ {
		sub, err := config.NewSubscription(
			"k2subs://u:p@"+srv.Listener.Addr().String()+"/api",
			t.TempDir(),
		)
		if err != nil {
			t.Fatalf("NewSubscription[%d]: %v", i, err)
		}

		tr, client := sessionHTTPClient(t, tlsCfg)
		sess := &subSession{sub: sub, httpTransport: tr, httpClient: client}

		fetchCtx, fcancel := context.WithTimeout(ctx, 5*time.Second)
		if err := sub.Fetch(fetchCtx, sess.httpClient); err != nil {
			fcancel()
			t.Fatalf("initial Fetch[%d]: %v", i, err)
		}
		fcancel()

		// Kick off the refresh loop — now the server's refresh=1 interval
		// is baked into the Subscription, so refreshLoop ticks at 1Hz.
		sub.StartRefresh(ctx, func() *http.Client { return sess.httpClient })
		sessions = append(sessions, sess)
	}

	// Let refresh loops run for ~2.5s so each session performs 2–3 refreshes.
	time.Sleep(2500 * time.Millisecond)

	// Sanity: refresh loop is actually firing. N initial fetches + at least
	// one refresh per session.
	if calls := fetchCalls.Load(); calls < int64(2*N) {
		t.Errorf("refresh loop not firing: fetchCalls=%d want >= %d", calls, 2*N)
	}

	// Cancel WHILE a refresh fetch may be in flight — exercises the
	// ctx-cancel-mid-fetch path for CloseIdleConnections.
	cancel()

	// Close all sessions concurrently to exercise the teardown race.
	done := make(chan struct{}, N)
	for _, s := range sessions {
		s := s
		go func() {
			s.Close()
			done <- struct{}{}
		}()
	}
	for i := 0; i < N; i++ {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("Close() hung")
		}
	}

	// Let refreshLoop's own goroutine drain (it awaits done after ctx.Done).
	// Subscription.Close already blocks on <-done, but allow a small margin
	// for persistConn.readLoop teardown after CloseIdleConnections.
	deadline := time.Now().Add(3 * time.Second)
	var final int
	// Tolerance: baseline + a few (test runtime has some noise on macOS).
	const tolerance = 2
	for {
		final = runtime.NumGoroutine()
		if final <= baseline+tolerance || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if final > baseline+tolerance {
		t.Errorf("goroutine leak after %d sessions + refresh + concurrent Close: baseline=%d final=%d (delta=%d, tolerance=%d)",
			N, baseline, final, final-baseline, tolerance)
	}
}
