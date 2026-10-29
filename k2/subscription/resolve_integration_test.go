package subscription

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/probe"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/wire"
)

// TestResolve_InstallsScoreSource verifies that Manager.Resolve wires the
// configured ScoreSource (probe.Registry) into each parsed Subscription
// so subsequent Pick calls steer away from probe-excluded URLs.
//
// Two consecutive unreachable records are required to confirm exclusion
// (Registry flake-tolerance threshold treats a single zero as no-data).
func TestResolve_InstallsScoreSource(t *testing.T) {
	reg := probe.NewRegistry(15 * time.Minute)
	reg.Record("k2v5://u:t@good:443", wire.ProbeStats{
		Reachable: true, EchoSupported: true, AvgRttMs: 20, JitterMs: 5, MeasuredAt: time.Now(),
	})
	reg.Record("k2v5://u:t@bad:443", wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	reg.Record("k2v5://u:t@bad:443", wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()
		if user != "u" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"tunnels":[{"url":"k2v5://good:443","recommendScore":0.5},{"url":"k2v5://bad:443","recommendScore":0.5}]}`)
	}))
	defer server.Close()

	mgr := NewManager(t.TempDir(), server.Client(), reg)

	subsURL := "k2subs://u:t@" + strings.TrimPrefix(server.URL, "https://")
	cfg := &config.ClientConfig{
		Routes: []config.RouteConfig{{Via: subsURL, Match: rule.MatchConfig{All: true}}},
	}

	sessions, _, err := mgr.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer mgr.CloseAll()
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(sessions))
	}

	// Pick many times — injected registry should steer away from "bad:443".
	for i := 0; i < 50; i++ {
		p, err := sessions[0].Subscription().Pick(nil)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(p.URL, "bad:443") {
			t.Fatalf("picked unreachable url on iteration %d: %s", i, p.URL)
		}
	}
}

// TestNextURL_ScoreSourceExcludesUnreachable verifies that when probe.Registry
// has hard-excluded a URL (two consecutive score=0 records), Manager.NextURL
// — the engine's OutboundProvider entry point — never returns that URL.
//
// Covers the integration seam between probe.Registry (set via
// Manager scoreSrc → Subscription.SetScoreSource) and NextURL's exclude-set
// accumulation.
func TestNextURL_ScoreSourceExcludesUnreachable(t *testing.T) {
	reg := probe.NewRegistry(15 * time.Minute)
	// good:443 is healthy.
	reg.Record("k2v5://u:t@good:443", wire.ProbeStats{
		Reachable: true, EchoSupported: true, AvgRttMs: 20, JitterMs: 5, MeasuredAt: time.Now(),
	})
	// bad:443 is hard-excluded.
	reg.Record("k2v5://u:t@bad:443", wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	reg.Record("k2v5://u:t@bad:443", wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	// ugly:443 is not in the registry — neutral weight (no probe data).

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"tunnels":[
			{"url":"k2v5://good:443","recommendScore":0.5},
			{"url":"k2v5://bad:443","recommendScore":0.5},
			{"url":"k2v5://ugly:443","recommendScore":0.5}
		]}`)
	}))
	defer server.Close()

	mgr := NewManager(t.TempDir(), server.Client(), reg)

	subsURL := "k2subs://u:t@" + strings.TrimPrefix(server.URL, "https://")
	cfg := &config.ClientConfig{
		Routes: []config.RouteConfig{{Via: subsURL, Match: rule.MatchConfig{All: true}}},
	}

	sessions, _, err := mgr.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer mgr.CloseAll()

	stored := sessions[0].Subscription().Tunnels()
	if len(stored) != 3 {
		t.Fatalf("stored tunnels = %d, want 3", len(stored))
	}

	// Pretend the engine started on good:443 and it failed. NextURL must
	// never return bad:443 — it's probe-excluded.
	seen := map[string]int{}
	current := "k2v5://u:t@good:443"
	for i := 0; i < 10; i++ {
		next, pickErr := mgr.NextURL(current, errors.New("simulated wire failure"))
		if pickErr != nil {
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

	if len(seen) == 0 {
		t.Fatal("NextURL never returned any URL — test didn't exercise the rotation path")
	}
}

// TestResolve_SessionOwnsHTTPTransport verifies the F2 leak guard: when
// Manager has no httpClient configured (production path), each Session
// owns a private http.Transport whose pooled persistConns are released
// by Session.Close().
//
// Uses cache pre-population so Fetch can fail gracefully (no TLS trust
// configured) while Resolve still succeeds with the cached tunnel list —
// that path still exercises the transport construction.
func TestResolve_SessionOwnsHTTPTransport(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not reached", http.StatusInternalServerError)
	}))
	defer server.Close()

	subsURL := "k2subs://u:t@" + strings.TrimPrefix(server.URL, "https://")

	// Pre-populate the on-disk cache so Fetch failure is tolerated —
	// Resolve only hard-fails if Fetch errors AND Tunnels() is empty.
	subsSubdir := t.TempDir()
	tunnelsBlob, _ := json.Marshal([]config.Tunnel{
		{URL: "k2v5://cached.example:443", Weight: 1, RecommendScore: 0.5},
	})
	if err := os.WriteFile(filepath.Join(subsSubdir, subscriptionCacheName(t, subsURL)), tunnelsBlob, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	// Intentionally pass nil httpClient — exercises the per-session
	// Transport construction path.
	mgr := NewManager(subsSubdir, nil, nil)

	cfg := &config.ClientConfig{
		Routes: []config.RouteConfig{{Via: subsURL, Match: rule.MatchConfig{All: true}}},
	}

	sessions, resolved, rsErr := mgr.Resolve(cfg)
	if rsErr != nil {
		t.Fatalf("Resolve (with cache): %v", rsErr)
	}
	defer mgr.CloseAll()

	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(sessions))
	}
	sess := sessions[0]
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

	// Close must be safe to call twice.
	sess.Close()
	sess.Close()
}

// subscriptionCacheName asks config/ for the cache filename instead of
// re-deriving it.
//
// It used to mirror the formula — sha256(raw) — and rotted silently when the
// key moved to the credential-stripped endpoint (so the filename stays stable
// across credential rotation). The test kept writing the old name while
// production read the new one, so the stale-cache fallback this test exists to
// exercise was no longer being exercised at all; it only surfaced because
// Resolve then hard-failed. Derive, never mirror.
func subscriptionCacheName(t *testing.T, raw string) string {
	t.Helper()
	endpoint, err := config.SubscriptionEndpoint(raw)
	if err != nil {
		t.Fatalf("derive subscription endpoint: %v", err)
	}
	return config.SubscriptionCacheName(endpoint)
}
