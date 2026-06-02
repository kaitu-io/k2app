package rule

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestVerifyAgainstManifest(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello-krs")
	if err := os.WriteFile(filepath.Join(dir, "cn.krs"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	good := &Manifest{Bundles: map[string]ManifestBundle{
		"cn": {SHA256: hex.EncodeToString(sum[:]), RuleCount: 10},
	}}
	if err := verifyAgainstManifest(dir, good); err != nil {
		t.Errorf("matching sha should verify: %v", err)
	}

	bad := &Manifest{Bundles: map[string]ManifestBundle{
		"cn": {SHA256: "deadbeef", RuleCount: 10},
	}}
	if err := verifyAgainstManifest(dir, bad); err == nil {
		t.Error("mismatched sha must fail verification")
	}

	zero := &Manifest{Bundles: map[string]ManifestBundle{
		"cn": {SHA256: hex.EncodeToString(sum[:]), RuleCount: 0},
	}}
	if err := verifyAgainstManifest(dir, zero); err == nil {
		t.Error("zero ruleCount must fail verification")
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

func TestVerifyAgainstManifest_BoundaryRegions(t *testing.T) {
	dir := t.TempDir()
	content := []byte("cn-bytes")
	if err := os.WriteFile(filepath.Join(dir, "cn.krs"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)

	// Manifest covers cn (matching) AND a region "us" with no local file.
	// Missing-local regions are allowed (caught later by missingConfiguredRegions).
	m := &Manifest{Bundles: map[string]ManifestBundle{
		"cn": {SHA256: hex.EncodeToString(sum[:]), RuleCount: 5},
		"us": {SHA256: "whatever", RuleCount: 5},
	}}
	if err := verifyAgainstManifest(dir, m); err != nil {
		t.Errorf("manifest covering a not-locally-present region must not fail: %v", err)
	}

	// Local region with NO manifest entry is allowed (manifest may not cover it).
	if err := os.WriteFile(filepath.Join(dir, "local-only.krs"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyAgainstManifest(dir, m); err != nil {
		t.Errorf("locally-present region absent from manifest must not fail: %v", err)
	}
}
