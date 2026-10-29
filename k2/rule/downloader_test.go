package rule

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2-rules/krs"
)

// makeTestK2B creates a minimal valid k2b binary via krs.WriteBundle and returns
// the raw bytes.
func makeTestK2B(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := krs.WriteBundle(&buf, &krs.Bundle{Sets: []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	}})
	if err != nil {
		t.Fatalf("krs.WriteBundle: %v", err)
	}
	return buf.Bytes()
}

func TestFetchBundleToFile_StreamsAndCaps(t *testing.T) {
	body := makeTestK2B(t) // small valid bundle bytes (existing test helper)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out.bin")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := fetchBundleToFile(ctx, srv.URL+"/krs.tar.gz", time.Time{}, dst)
	if err != nil {
		t.Fatalf("fetchBundleToFile: %v", err)
	}
	if res != downloadResultOK {
		t.Fatalf("res = %v, want OK", res)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("written bytes != served bytes")
	}
}

func TestFetchBundleToFile_RejectsOversize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, (10<<20)+1024) // > 10 MB cap
		_, _ = w.Write(buf)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out.bin")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := fetchBundleToFile(ctx, srv.URL+"/krs.tar.gz", time.Time{}, dst)
	if err == nil {
		t.Fatal("expected oversize error, got nil")
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Errorf("oversize download must not leave a dst file behind")
	}
}

func TestRaceDownload_DistinctTempPaths(t *testing.T) {
	body := makeTestK2B(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "krs.tar.gz") {
			_, _ = w.Write(body)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	// Sources sharing a long common tail (the old bug collapsed these to one temp path).
	sources := []string{
		srv.URL + "/mirrorA/kaitu-io/k2-rules/releases/latest/download/",
		srv.URL + "/mirrorB/kaitu-io/k2-rules/releases/latest/download/",
		srv.URL + "/mirrorC/kaitu-io/k2-rules/releases/latest/download/",
	}
	dest := filepath.Join(t.TempDir(), "krs.tar.gz")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := raceDownload(ctx, dest, "krs.tar.gz", sources)
	if err != nil {
		t.Fatalf("raceDownload: %v", err)
	}
	if res != downloadResultOK {
		t.Fatalf("res = %v, want OK", res)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("winner dest is corrupted — temp paths collided")
	}
}

func TestFetchBundleToFile_NotModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "out.bin")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := fetchBundleToFile(ctx, srv.URL+"/krs.tar.gz", time.Now(), dst)
	if err != nil {
		t.Fatalf("fetchBundleToFile: %v", err)
	}
	if res != downloadResultNotModified {
		t.Fatalf("res = %v, want NotModified", res)
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Errorf("304 must not create dst")
	}
}

// TestBackoffDuration verifies the exponential backoff sequence and cap.
func TestBackoffDuration(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{10, 60 * time.Second}, // capped
		{20, 60 * time.Second}, // still capped
	}
	for _, c := range cases {
		got := backoffDuration(c.attempt)
		if got != c.want {
			t.Errorf("backoffDuration(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestRaceDownload_CapsConcurrency(t *testing.T) {
	const cap = 4
	const nSources = 8
	body := makeTestK2B(t)
	var inFlight, maxSeen atomic.Int32

	// gate parks every handler that reaches the server until the test releases
	// it. arrived signals each handler entry (buffered to nSources so a handler
	// never blocks on the send). Together they make concurrency observable
	// without any sleep-based timing: the test receives exactly `cap` arrivals
	// (deterministic — blocks until that many handlers are parked), then proves
	// no further handler can run while the gate is held.
	gate := make(chan struct{})
	var gateOnce sync.Once
	arrived := make(chan struct{}, nSources)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			old := maxSeen.Load()
			if cur <= old || maxSeen.CompareAndSwap(old, cur) {
				break
			}
		}
		arrived <- struct{}{}
		<-gate // park until released — proves these are concurrent, no sleep
		if strings.HasSuffix(r.URL.Path, "krs.tar.gz") {
			_, _ = w.Write(body)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	// Ensure the gate is always released even on an early t.Fatal, so parked
	// server goroutines can drain (avoids httptest.Server.Close hanging).
	defer gateOnce.Do(func() { close(gate) })

	sources := make([]string, nSources)
	for i := range sources {
		sources[i] = srv.URL + "/"
	}
	// Own a dedicated subdir for the download so cleanup is under our control:
	// raceDownload returns at winner-rename while loser goroutines may still be
	// writing/renaming temp files, which would race t.TempDir's auto-RemoveAll.
	destDir := filepath.Join(t.TempDir(), "dl")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(destDir, "krs.tar.gz")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := raceDownload(ctx, dest, "krs.tar.gz", sources)
		done <- err
	}()

	// Block until exactly `cap` handlers have entered and parked. This is fully
	// deterministic — no timeout, no poll. With the semaphore, exactly `cap`
	// goroutines get past the client-side gate to reach the server; the rest
	// block on the semaphore and never arrive.
	for i := 0; i < cap; i++ {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d/%d handlers reached the server — fetches not running", i, cap)
		}
	}
	// Overshoot guard: with a correct cap, handlers cap+1..nSources are blocked
	// on the semaphore and can NEVER reach the server while the gate holds the
	// first `cap` parked. A broken cap (no semaphore) starts all nSources
	// concurrently, so the surplus handlers have already sent to `arrived`
	// before they park — a (cap+1)th receive succeeds within microseconds. The
	// 1s window is a ~100x margin over even a heavily-loaded scheduler, not a
	// tuning knob.
	select {
	case <-arrived:
		t.Fatalf("a %dth handler ran concurrently — concurrency cap exceeded", cap+1)
	case <-time.After(1 * time.Second):
		// No further handler ran — the cap held.
	}
	if got := maxSeen.Load(); got > cap {
		t.Fatalf("max concurrent in-flight = %d, want <= %d", got, cap)
	}

	gateOnce.Do(func() { close(gate) }) // release; winner completes, losers cancel
	// raceDownload drains all its goroutines before returning (clean-FS
	// contract), so on <-done no straggler is still writing temp files into
	// destDir — t.TempDir's auto-cleanup can't race a live write.
	if err := <-done; err != nil {
		t.Fatalf("raceDownload: %v", err)
	}
	if got := maxSeen.Load(); got > cap {
		t.Errorf("post-run max concurrent = %d, want <= %d", got, cap)
	}
}
