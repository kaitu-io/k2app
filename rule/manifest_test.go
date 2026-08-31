package rule

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRemoteNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool // is a strictly newer than b?
	}{
		{"2026-06-02T00:31:05Z", "2026-06-01T00:31:05Z", true},
		{"2026-06-02T00:31:05Z", "2026-06-02T00:31:05Z", false},
		{"2026-06-01T00:31:05Z", "2026-06-02T00:31:05Z", false},
		{"2026-06-02T00:31:05Z", "", true},            // any vs unknown local
		{"garbage", "2026-06-02T00:31:05Z", false},    // unparseable a → treated oldest
		{"2026-06-02", "2026-06-02T00:31:05Z", false}, // legacy date < timestamped
		// Embed-seeded local version carries the " [embedded]" suffix and is
		// unparseable BY DESIGN — any valid remote must win so the embedded
		// floor gets lifted. Pins the local-side reason the fail-loud guard in
		// ensure.go checks ONLY the remote version.
		{"2026-06-02T00:31:05Z", "2026-05-31T00:00:00Z [embedded]", true},
	}
	for _, c := range cases {
		if got := remoteNewer(c.a, c.b); got != c.want {
			t.Errorf("remoteNewer(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestRaceManifest_PicksNewestAndSource(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"schemaVersion":2,"version":"2026-06-01T00:00:00Z","bundles":{"cn":{"sha256":"old","size":1,"ruleCount":5}}}`))
	}))
	defer old.Close()
	fresh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"schemaVersion":2,"version":"2026-06-02T00:00:00Z","bundles":{"cn":{"sha256":"new","size":1,"ruleCount":7}}}`))
	}))
	defer fresh.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, src, err := raceManifest(ctx, []string{old.URL + "/", fresh.URL + "/"})
	if err != nil {
		t.Fatalf("raceManifest: %v", err)
	}
	if m.Version != "2026-06-02T00:00:00Z" {
		t.Errorf("picked version %q, want the newer one", m.Version)
	}
	if m.Bundles["cn"].SHA256 != "new" {
		t.Errorf("picked cn sha %q, want new", m.Bundles["cn"].SHA256)
	}
	if src != fresh.URL+"/" {
		t.Errorf("src = %q, want the fresh mirror %q", src, fresh.URL+"/")
	}
}

func TestRaceManifest_AllFail(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer down.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := raceManifest(ctx, []string{down.URL + "/"}); err == nil {
		t.Fatal("expected error when all manifests fail")
	}
}

func TestRaceManifest_RespectsInternalBound(t *testing.T) {
	// A server that never responds (parks until the request ctx cancels).
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer hang.Close()

	// Caller passes a generous ctx; raceManifest's own 5s bound must fire first.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_, _, _ = raceManifest(ctx, []string{hang.URL + "/"})
		close(done)
	}()
	select {
	case <-done:
		// Returned — the internal bound worked.
	case <-time.After(7 * time.Second): // 5s bound + slack
		t.Fatal("raceManifest blocked past its internal 5s bound")
	}
}

func TestLocalManifestVersionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := writeLocalVersion(dir, "2026-06-02T00:31:05Z", false); err != nil {
		t.Fatal(err)
	}
	if v := readLocalManifestVersion(dir); v != "2026-06-02T00:31:05Z" {
		t.Errorf("read %q, want the written version", v)
	}
	if wasEmbedSeeded(dir) {
		t.Error("non-embed write must not look embed-seeded")
	}

	if err := writeLocalVersion(dir, "2026-06-02T00:31:05Z", true); err != nil {
		t.Fatal(err)
	}
	if !wasEmbedSeeded(dir) {
		t.Error("embed write must set the embed marker")
	}
	if v := readLocalManifestVersion(dir); v != "2026-06-02T00:31:05Z" {
		t.Errorf("embed read %q, want the version (marker stripped)", v)
	}
}

func TestEmbedSeedAlwaysUpgrades(t *testing.T) {
	dir := t.TempDir()
	// seedFromEmbed uses the "embedded" sentinel version (no real CDN version).
	if err := writeLocalVersion(dir, "embedded", true); err != nil {
		t.Fatal(err)
	}
	local := readLocalManifestVersion(dir) // "embedded"
	// Any well-formed CDN version must be newer than the embed sentinel, so the
	// updater always lifts the floor to the CDN ceiling.
	if !remoteNewer("2026-06-02T00:00:00Z", local) {
		t.Error("a real CDN version must out-rank the embed sentinel (floor would never lift)")
	}
	if !wasEmbedSeeded(dir) {
		t.Error("embed marker lost — updater force-refresh would not trigger")
	}
}

