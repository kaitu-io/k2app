package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/probe"
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
