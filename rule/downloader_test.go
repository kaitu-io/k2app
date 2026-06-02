package rule

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
