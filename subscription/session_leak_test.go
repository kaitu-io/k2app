package subscription

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

// sessionHTTPClient mirrors how Manager.Resolve builds a Session's HTTP
// stack — shared Transport between initial Fetch and refreshLoop. Tests
// pass in the httptest-derived TLS config so self-signed certs are trusted.
func sessionHTTPClient(t *testing.T, tlsCfg *tls.Config) (*http.Transport, *http.Client) {
	t.Helper()
	clone := tlsCfg.Clone()
	clone.MinVersion = tls.VersionTLS12
	tr := &http.Transport{TLSClientConfig: clone}
	return tr, &http.Client{Timeout: 5 * time.Second, Transport: tr}
}

// TestSession_CloseReleasesHTTPIdleConns is the regression test for the
// F2 goroutine leak. Before the fix, each Session constructed its own
// http.Transport without ever calling CloseIdleConnections — the pooled
// persistConn readLoop goroutines stayed alive past Session.Close(),
// leaking one goroutine per up/down cycle in production.
//
// This test creates a Session backed by a TLS httptest server, runs a
// Fetch (which primes an idle persistConn), closes the session, and
// asserts that the goroutine count settles back to its pre-fetch level.
func TestSession_CloseReleasesHTTPIdleConns(t *testing.T) {
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

	tr := &http.Transport{
		TLSClientConfig: srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone(),
	}
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{}
	}
	tr.TLSClientConfig.MinVersion = tls.VersionTLS12

	sess := &Session{
		sub:           sub,
		httpTransport: tr,
		httpClient:    &http.Client{Timeout: 5 * time.Second, Transport: tr},
	}

	baseline := runtime.NumGoroutine()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := sub.Fetch(ctx, sess.httpClient); err != nil {
		cancel()
		t.Fatalf("Fetch: %v", err)
	}
	cancel()

	sess.Close()

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
		t.Errorf("goroutine leak: baseline=%d final=%d (delta=%d, want <= baseline+1)",
			baseline, final, final-baseline)
	}
}

// TestSession_RefreshLoopClean exercises the production-shaped flow:
// N concurrent Sessions, each running StartRefresh with a 1s interval
// against a TLS httptest server. After letting the refresh loop fire
// several times, Close() is invoked and we assert no persistConn /
// refresh goroutines survive.
//
// Three sharper risks than the single-session test:
//  1. refresh loop actually re-uses the shared Transport (no per-refresh leak)
//  2. concurrent Close of multiple sessions doesn't race on Transport cleanup
//  3. Fetches that are in flight when ctx cancels still release idle conns
func TestSession_RefreshLoopClean(t *testing.T) {
	const N = 3

	var fetchCalls atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCalls.Add(1)
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

	sessions := make([]*Session, 0, N)
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
		sess := &Session{sub: sub, httpTransport: tr, httpClient: client}

		fetchCtx, fcancel := context.WithTimeout(ctx, 5*time.Second)
		if err := sub.Fetch(fetchCtx, sess.httpClient); err != nil {
			fcancel()
			t.Fatalf("initial Fetch[%d]: %v", i, err)
		}
		fcancel()

		sub.StartRefresh(ctx, func() *http.Client { return sess.httpClient })
		sessions = append(sessions, sess)
	}

	time.Sleep(2500 * time.Millisecond)

	if calls := fetchCalls.Load(); calls < int64(2*N) {
		t.Errorf("refresh loop not firing: fetchCalls=%d want >= %d", calls, 2*N)
	}

	cancel()

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

	deadline := time.Now().Add(3 * time.Second)
	var final int
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
