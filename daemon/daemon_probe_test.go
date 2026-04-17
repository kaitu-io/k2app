package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/probe"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/wire"
)

func TestHandleProbe_Shape(t *testing.T) {
	d := New()

	var calls atomic.Int32
	d.probeFn = func(_ context.Context, url string, _ wire.DirectDialer, _ int, _ time.Duration) (wire.ProbeStats, error) {
		calls.Add(1)
		return wire.ProbeStats{
			Reachable: true, EchoSupported: true,
			AvgRttMs: 42, MinRttMs: 38, MaxRttMs: 55, JitterMs: 17, LossRate: 0,
			MeasuredAt: time.Now(),
		}, nil
	}

	params := map[string]any{
		"urls":      []any{"k2v5://u:t@host1:443?insecure=1", "k2v5://u:t@host2:443?insecure=1"},
		"timeoutMs": float64(5000),
	}
	w := newResponseRecorder()
	d.handleProbe(w, params)

	resp := w.decode(t)
	if resp.Code != 0 {
		t.Fatalf("code = %d, msg = %s", resp.Code, resp.Message)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("data is not a map: %T", resp.Data)
	}
	results, ok := data["results"].([]any)
	if !ok {
		t.Fatalf("results is not an array: %T", data["results"])
	}
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	if calls.Load() != 2 {
		t.Errorf("probeFn called %d times, want 2", calls.Load())
	}

	// First result shape check.
	r0 := results[0].(map[string]any)
	if r0["reachable"] != true {
		t.Errorf("reachable != true")
	}
	if r0["echoSupported"] != true {
		t.Errorf("echoSupported != true")
	}
	if r0["avgRttMs"].(float64) != 42 {
		t.Errorf("avgRttMs = %v, want 42", r0["avgRttMs"])
	}
	if r0["probeScore"].(float64) <= 0 {
		t.Errorf("probeScore = %v, want > 0", r0["probeScore"])
	}

	// Also: Registry was populated.
	for _, u := range []string{"k2v5://u:t@host1:443?insecure=1", "k2v5://u:t@host2:443?insecure=1"} {
		if _, ok := d.probeRegistry.Score(u); !ok {
			t.Errorf("registry missing %s", u)
		}
	}

	// Silence "probe imported and not used" when only referenced indirectly.
	_ = probe.NewRegistry
}

func TestHandleProbe_MissingURLs(t *testing.T) {
	d := New()
	w := newResponseRecorder()
	d.handleProbe(w, map[string]any{})
	resp := w.decode(t)
	if resp.Code != 400 {
		t.Errorf("code = %d, want 400", resp.Code)
	}
}

func TestHandleProbe_BadURLsType(t *testing.T) {
	d := New()
	w := newResponseRecorder()
	d.handleProbe(w, map[string]any{"urls": "not-a-list"})
	resp := w.decode(t)
	if resp.Code != 400 {
		t.Errorf("code = %d, want 400", resp.Code)
	}
}

// --- local test helper ---

type responseRecorder struct {
	body []byte
	hdr  http.Header
}

func newResponseRecorder() *responseRecorder { return &responseRecorder{hdr: http.Header{}} }

func (r *responseRecorder) Header() http.Header         { return r.hdr }
func (r *responseRecorder) Write(b []byte) (int, error) { r.body = append(r.body, b...); return len(b), nil }
func (r *responseRecorder) WriteHeader(int)             {}

func (r *responseRecorder) decode(t *testing.T) Response {
	t.Helper()
	var resp Response
	if err := json.Unmarshal(r.body, &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, string(r.body))
	}
	return resp
}

func TestResolveSubscriptions_InstallsScoreSource(t *testing.T) {
	// Preload registry with steering data. Two consecutive unreachable
	// records are required to confirm exclusion (see Registry flake-tolerance
	// threshold — a single zero is treated as no-data).
	d := New()
	d.probeRegistry.Record("k2v5://u:t@good:443", wire.ProbeStats{
		Reachable: true, EchoSupported: true, AvgRttMs: 20, JitterMs: 5, MeasuredAt: time.Now(),
	})
	d.probeRegistry.Record("k2v5://u:t@bad:443", wire.ProbeStats{
		Reachable: false, MeasuredAt: time.Now(),
	})
	d.probeRegistry.Record("k2v5://u:t@bad:443", wire.ProbeStats{
		Reachable: false, MeasuredAt: time.Now(),
	})

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()
		if user != "u" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		io.WriteString(w, `{"tunnels":[{"url":"k2v5://good:443","recommendScore":0.5},{"url":"k2v5://bad:443","recommendScore":0.5}]}`)
	}))
	defer server.Close()

	// Use the test server's TLS client (trusts the test cert) so
	// Subscription.Fetch can reach our https:// handler.
	d.subsHTTPClient = server.Client()

	subsURL := "k2subs://u:t@" + strings.TrimPrefix(server.URL, "https://")
	cfg := &config.ClientConfig{
		Routes: []config.RouteConfig{{Via: subsURL, Match: rule.MatchConfig{All: true}}},
	}

	subs, _, err := d.resolveSubscriptions(cfg)
	if err != nil {
		t.Fatalf("resolveSubscriptions: %v", err)
	}
	defer func() {
		for _, s := range subs {
			s.Close()
		}
	}()
	if len(subs) != 1 {
		t.Fatalf("subs len = %d, want 1", len(subs))
	}

	// Pick many times — injected registry should steer away from "bad:443".
	// Note: host returned from the test HTTP server is "good:443" / "bad:443"
	// (no userinfo); Subscription.Fetch injects "u:t@" credentials → final
	// URLs are "k2v5://u:t@good:443" / "k2v5://u:t@bad:443", which match our
	// preloaded registry entries.
	for i := 0; i < 50; i++ {
		p, err := subs[0].sub.Pick(nil)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(p.URL, "bad:443") {
			t.Fatalf("picked unreachable url on iteration %d: %s", i, p.URL)
		}
	}
}

// TestNextURL_ScoreSourceExcludesUnreachable verifies that when probe.Registry
// has hard-excluded a URL (two consecutive score=0 records), daemon's
// NextURL — the engine's OutboundProvider entry point — never returns
// that URL. This is the tunnel-rotation path: engine hits a bad server,
// calls NextURL; probe has already flagged another server as dead; the
// replacement loop must skip both.
//
// Covers the integration seam between probe.Registry (set via
// Subscription.SetScoreSource) and NextURL's exclude-set accumulation.
func TestNextURL_ScoreSourceExcludesUnreachable(t *testing.T) {
	d := New()
	// good:443 is healthy (echo-supported, low rtt).
	d.probeRegistry.Record("k2v5://u:t@good:443", wire.ProbeStats{
		Reachable: true, EchoSupported: true, AvgRttMs: 20, JitterMs: 5, MeasuredAt: time.Now(),
	})
	// bad:443 is hard-excluded — two consecutive unreachable records cross
	// the Registry flake-tolerance threshold.
	d.probeRegistry.Record("k2v5://u:t@bad:443", wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	d.probeRegistry.Record("k2v5://u:t@bad:443", wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	// ugly:443 is not in the registry — neutral weight (no probe data).
	// A third candidate is important: without it, all-zero pool fallback
	// could incorrectly re-admit bad:443.

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"tunnels":[
			{"url":"k2v5://good:443","recommendScore":0.5},
			{"url":"k2v5://bad:443","recommendScore":0.5},
			{"url":"k2v5://ugly:443","recommendScore":0.5}
		]}`)
	}))
	defer server.Close()
	d.subsHTTPClient = server.Client()

	subsURL := "k2subs://u:t@" + strings.TrimPrefix(server.URL, "https://")
	cfg := &config.ClientConfig{
		Routes: []config.RouteConfig{{Via: subsURL, Match: rule.MatchConfig{All: true}}},
	}

	subs, _, err := d.resolveSubscriptions(cfg)
	if err != nil {
		t.Fatalf("resolveSubscriptions: %v", err)
	}
	defer func() {
		for _, s := range subs {
			s.Close()
		}
	}()
	// Engine holds the session reference via d.subs. NextURL reads this.
	d.mu.Lock()
	d.subs = subs
	d.mu.Unlock()

	// Drive NextURL through the rotation. After resolveSubscriptions picks
	// an initial candidate (from non-excluded pool), we simulate engine
	// asking for replacement. Engine hands us the current URL; we must
	// never return bad:443 — either as the initial replacement or any
	// downstream replacement — because probe has hard-excluded it.
	stored := subs[0].sub.Tunnels()
	if len(stored) != 3 {
		t.Fatalf("stored tunnels = %d, want 3", len(stored))
	}

	// Pretend the engine started on good:443 and it failed. NextURL
	// should pick ugly:443 (the only remaining non-probe-excluded).
	seen := map[string]int{}
	current := "k2v5://u:t@good:443"
	for i := 0; i < 10; i++ {
		next, pickErr := d.NextURL(current, errors.New("simulated wire failure"))
		if pickErr != nil {
			// Exhaustion is acceptable — but only AFTER we've seen the
			// healthy candidate(s). If the very first call exhausts,
			// something's wrong.
			if i == 0 {
				t.Fatalf("NextURL exhausted on first call: %v (stored=%v)", pickErr, stored)
			}
			break
		}
		seen[next]++
		if strings.Contains(next, "bad:443") {
			t.Fatalf("NextURL returned probe-excluded URL on iteration %d: %s", i, next)
		}
		current = next
	}

	// We must have seen at least one non-excluded replacement before the
	// pool exhausted. Otherwise the test didn't actually exercise NextURL.
	if len(seen) == 0 {
		t.Fatal("NextURL never returned any URL — test didn't exercise the rotation path")
	}
}

// TestResolveSubscriptions_SessionOwnsHTTPTransport verifies the leak
// guard wired in during the origin/master merge: when d.subsHTTPClient is
// nil (production path), each subSession owns a private http.Transport
// whose pooled persistConns are released by subSession.Close(). A prior
// HEAD-side implementation lacked this; without it, every up/down cycle
// leaked one net/http readLoop goroutine (confirmed via pprof diff on
// soak tests).
//
// Uses cache pre-population so Fetch can fail gracefully (no TLS trust
// configured) while resolveSubscriptions still succeeds with the cached
// tunnel list — that path still exercises the transport construction.
func TestResolveSubscriptions_SessionOwnsHTTPTransport(t *testing.T) {
	// Unreachable HTTPS server — Fetch will fail with TLS trust error.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not reached", http.StatusInternalServerError)
	}))
	defer server.Close()

	subsURL := "k2subs://u:t@" + strings.TrimPrefix(server.URL, "https://")

	// Pre-populate the on-disk cache so Fetch failure is tolerated —
	// resolveSubscriptions only hard-fails if Fetch errors AND Tunnels()
	// is empty. Cache filename scheme lives in config/subscription.go
	// (sha256 prefix of the raw URL).
	subsSubdir := t.TempDir()
	tunnelsBlob, _ := json.Marshal([]config.Tunnel{
		{URL: "k2v5://cached.example:443", Weight: 1, RecommendScore: 0.5},
	})
	if err := os.WriteFile(filepath.Join(subsSubdir, subscriptionCacheName(subsURL)), tunnelsBlob, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	d := New()
	d.subsCacheDirOverride = subsSubdir
	// Intentionally leave d.subsHTTPClient = nil — exercises the
	// per-session Transport construction path.

	cfg := &config.ClientConfig{
		Routes: []config.RouteConfig{{Via: subsURL, Match: rule.MatchConfig{All: true}}},
	}

	subs, resolved, rsErr := d.resolveSubscriptions(cfg)
	if rsErr != nil {
		t.Fatalf("resolveSubscriptions (with cache): %v", rsErr)
	}
	defer func() {
		for _, s := range subs {
			s.Close() // must not panic
		}
	}()

	if len(subs) != 1 {
		t.Fatalf("subs len = %d, want 1", len(subs))
	}
	sess := subs[0]
	if sess.httpTransport == nil {
		t.Fatal("sess.httpTransport is nil — leak guard disabled in non-override path")
	}
	if sess.httpClient == nil {
		t.Fatal("sess.httpClient is nil")
	}
	if sess.httpClient.Transport != sess.httpTransport {
		t.Error("httpClient.Transport != sess.httpTransport — transport ownership fractured")
	}
	if len(resolved) == 0 {
		t.Error("resolved map empty — cached tunnel was not selected")
	}

	// Close must be safe to call twice (idempotency contract documented on subSession).
	sess.Close()
	sess.Close()
}

// subscriptionCacheName mirrors the deterministic filename
// config/subscription.go:158-161 computes for the disk cache file:
// "subs-" + first 4 bytes of sha256(raw) in hex + ".json".
// Kept in sync manually — a compatibility break there breaks this test.
func subscriptionCacheName(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("subs-%x.json", h[:4])
}
