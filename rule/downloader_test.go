package rule

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeTestK2B creates a minimal valid k2b binary via WriteBundle and returns
// the raw bytes.
func makeTestK2B(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := WriteBundle(&buf, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	return buf.Bytes()
}

// TestBundlesForRoutes_Preset verifies that preset names map to the correct files.
func TestBundlesForRoutes_Preset(t *testing.T) {
	got := BundlesForConfig([]MatchConfig{
		{Preset: "overseas"},
	})
	if len(got) != 1 || got[0] != "overseas.k2b" {
		t.Fatalf("overseas preset: want [overseas.k2b], got %v", got)
	}

	got2 := BundlesForConfig([]MatchConfig{
		{Names: []string{"cn-sites"}},
	})
	if len(got2) != 1 || got2[0] != "cn-direct.k2b" {
		t.Fatalf("cn-sites name: want [cn-direct.k2b], got %v", got2)
	}
}

// TestBundlesForRoutes_AllMatch verifies All:true yields an empty list.
func TestBundlesForRoutes_AllMatch(t *testing.T) {
	got := BundlesForConfig([]MatchConfig{
		{All: true},
	})
	if len(got) != 0 {
		t.Fatalf("All:true: want empty, got %v", got)
	}
}

// TestBundlesForRoutes_InlineOnly verifies DomainSuffix-only configs yield an
// empty list.
func TestBundlesForRoutes_InlineOnly(t *testing.T) {
	got := BundlesForConfig([]MatchConfig{
		{DomainSuffix: []string{"example.com"}},
	})
	if len(got) != 0 {
		t.Fatalf("inline only: want empty, got %v", got)
	}
}

// TestDownload_BasicSuccess verifies that a valid k2b served by an httptest
// server is written to the cache directory.
func TestDownload_BasicSuccess(t *testing.T) {
	data := makeTestK2B(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))
	defer srv.Close()

	dir := t.TempDir()
	ctx := context.Background()
	err := Download(ctx, dir, []string{"overseas.k2b"}, []string{srv.URL + "/"})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "overseas.k2b"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("file content mismatch")
	}
}

// TestDownload_304NotModified verifies that a 304 response leaves the existing
// cached file unchanged.
func TestDownload_304NotModified(t *testing.T) {
	data := makeTestK2B(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Modified-Since") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "overseas.k2b")

	// Pre-place the file so it has a non-zero mtime.
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Set mtime in the past so If-Modified-Since is sent.
	old := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(dest, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	ctx := context.Background()
	err := Download(ctx, dir, []string{"overseas.k2b"}, []string{srv.URL + "/"})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("file content should be unchanged")
	}
}

// TestDownload_HappyEyeballs_FastestWins verifies that the fastest responding
// source wins the race.
func TestDownload_HappyEyeballs_FastestWins(t *testing.T) {
	data := makeTestK2B(t)
	fastHit := make(chan struct{}, 1)

	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))
	defer slowSrv.Close()

	fastSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fastHit <- struct{}{}
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))
	defer fastSrv.Close()

	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := Download(ctx, dir, []string{"overseas.k2b"}, []string{slowSrv.URL + "/", fastSrv.URL + "/"})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	select {
	case <-fastHit:
		// fast server was used
	default:
		t.Fatal("fast server should have been hit")
	}

	if _, err := os.Stat(filepath.Join(dir, "overseas.k2b")); err != nil {
		t.Fatalf("file should exist: %v", err)
	}
}

// TestDownload_AllFail_UseCached verifies that when all servers return 500 but
// a cached file exists, Download returns nil.
func TestDownload_AllFail_UseCached(t *testing.T) {
	data := makeTestK2B(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "overseas.k2b")
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := Download(ctx, dir, []string{"overseas.k2b"}, []string{srv.URL + "/"})
	if err != nil {
		t.Fatalf("Download with cached file should succeed, got: %v", err)
	}

	// File should still be there.
	if _, statErr := os.Stat(dest); statErr != nil {
		t.Fatalf("cached file should still exist: %v", statErr)
	}
}

// TestDownload_AllFail_NoCached_RetriesUntilCancel verifies that without a
// cached file and all servers failing, Download retries until context is
// cancelled (at least 2 attempts within a 3s window).
func TestDownload_AllFail_NoCached_RetriesUntilCancel(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := Download(ctx, dir, []string{"overseas.k2b"}, []string{srv.URL + "/"})
	if err == nil {
		t.Fatal("expected error when no cache and all servers fail")
	}
	if attempts < 2 {
		t.Fatalf("expected at least 2 attempts, got %d", attempts)
	}
}

// TestIsFresh_AllFresh verifies that recent files are considered fresh.
func TestIsFresh_AllFresh(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.k2b", "b.k2b"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !IsFresh(dir, []string{"a.k2b", "b.k2b"}, 24*time.Hour) {
		t.Fatal("expected fresh")
	}
}

// TestIsFresh_OneMissing verifies that a missing file means not fresh.
func TestIsFresh_OneMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.k2b"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsFresh(dir, []string{"a.k2b", "b.k2b"}, 24*time.Hour) {
		t.Fatal("expected not fresh when file missing")
	}
}

// TestIsFresh_OneStale verifies that a stale file means not fresh.
func TestIsFresh_OneStale(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.k2b", "b.k2b"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Age b.k2b past the threshold.
	old := time.Now().Add(-25 * time.Hour)
	os.Chtimes(filepath.Join(dir, "b.k2b"), old, old)

	if IsFresh(dir, []string{"a.k2b", "b.k2b"}, 24*time.Hour) {
		t.Fatal("expected not fresh when one file stale")
	}
}

// TestIsFresh_EmptyBundles verifies that empty input is always fresh.
func TestIsFresh_EmptyBundles(t *testing.T) {
	if !IsFresh("/nonexistent", nil, 24*time.Hour) {
		t.Fatal("expected fresh for nil bundles")
	}
	if !IsFresh("", []string{"a.k2b"}, 24*time.Hour) {
		t.Fatal("expected fresh for empty cacheDir")
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
