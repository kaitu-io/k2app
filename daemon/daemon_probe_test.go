package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
