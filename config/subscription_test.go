package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- Task 1: URL Parsing ---

func TestSubscription_NewFull(t *testing.T) {
	s, err := NewSubscription("k2subs://alice:secret@subs.example.com/api/v1?refresh=600", t.TempDir())
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	if s.endpoint != "https://subs.example.com/api/v1" {
		t.Errorf("endpoint = %q, want https://subs.example.com/api/v1", s.endpoint)
	}
	if s.creds != "alice:secret" {
		t.Errorf("creds = %q, want alice:secret", s.creds)
	}
	if s.refresh != 600*time.Second {
		t.Errorf("refresh = %v, want 600s", s.refresh)
	}
	if s.cacheFile == "" {
		t.Error("cacheFile should be set when cacheDir is non-empty")
	}
}

func TestSubscription_DefaultRefresh(t *testing.T) {
	s, err := NewSubscription("k2subs://u:p@host.com/path", "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	if s.refresh != 1800*time.Second {
		t.Errorf("refresh = %v, want 1800s", s.refresh)
	}
}

func TestSubscription_ForwardsNonRefreshQuery(t *testing.T) {
	// `refresh` is client-side (stripped); all other params forwarded to endpoint.
	s, err := NewSubscription("k2subs://u:p@host.com/api/subs?country=jp&refresh=120", "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	if s.endpoint != "https://host.com/api/subs?country=jp" {
		t.Errorf("endpoint = %q, want https://host.com/api/subs?country=jp", s.endpoint)
	}
	if s.refresh != 120*time.Second {
		t.Errorf("refresh = %v, want 120s", s.refresh)
	}
}

func TestSubscription_RefreshZero(t *testing.T) {
	s, err := NewSubscription("k2subs://u:p@host.com/path?refresh=0", "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	if s.refresh != 0 {
		t.Errorf("refresh = %v, want 0", s.refresh)
	}
}

func TestSubscription_WrongScheme(t *testing.T) {
	_, err := NewSubscription("https://u:p@host.com/path", "")
	if err == nil {
		t.Fatal("expected error for wrong scheme")
	}
}

func TestSubscription_MissingCredentials(t *testing.T) {
	_, err := NewSubscription("k2subs://host.com/path", "")
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
}

func TestSubscription_MissingPassword(t *testing.T) {
	_, err := NewSubscription("k2subs://useronly@host.com/path", "")
	if err == nil {
		t.Fatal("expected error for missing password")
	}
}

func TestSubscription_EmptyURL(t *testing.T) {
	_, err := NewSubscription("", "")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

// --- Task 2: Fetch + Disk Cache ---

func TestSubscription_Fetch(t *testing.T) {
	var gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		resp := subsResponse{
			Tunnels: []Tunnel{
				{URL: "k2v5://server1.example.com:443", Weight: 3},
				{URL: "k2v5://server2.example.com:443", Weight: 1},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Build a k2subs URL pointing at the test server.
	raw := "k2subs://alice:secret@" + srv.Listener.Addr().String() + "/api"
	s, err := NewSubscription(raw, "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	// Override endpoint to use the test server's URL (HTTPS).
	s.endpoint = srv.URL + "/api"

	ctx := context.Background()
	if err := s.Fetch(ctx, srv.Client()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Verify Basic Auth was sent.
	if gotAuth == "" {
		t.Error("no Authorization header sent")
	}

	tunnels := s.Tunnels()
	if len(tunnels) != 2 {
		t.Fatalf("got %d tunnels, want 2", len(tunnels))
	}

	// Verify credentials injected into tunnel URLs.
	for _, tun := range tunnels {
		if !contains(tun.URL, "alice:secret@") {
			t.Errorf("tunnel URL %q missing injected credentials", tun.URL)
		}
	}
}

func TestSubscriptionFetchSendsClientHeader(t *testing.T) {
	var gotHeader string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-K2-Client")
		// Fetch parses a JSON subsResponse — return a minimal valid body.
		json.NewEncoder(w).Encode(subsResponse{
			Tunnels: []Tunnel{{URL: "k2v5://h:443", Weight: 1}},
		})
	}))
	defer srv.Close()

	s := &Subscription{
		endpoint:     srv.URL,
		creds:        "udid:tok",
		clientHeader: "kaitu-router/0.4.5 (linux; arm64)",
	}
	if err := s.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotHeader != "kaitu-router/0.4.5 (linux; arm64)" {
		t.Fatalf("header not sent, got %q", gotHeader)
	}
}

func TestSubscriptionFetchOmitsEmptyClientHeader(t *testing.T) {
	var seen bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, seen = r.Header["X-K2-Client"]
		json.NewEncoder(w).Encode(subsResponse{
			Tunnels: []Tunnel{{URL: "k2v5://h:443", Weight: 1}},
		})
	}))
	defer srv.Close()

	s := &Subscription{endpoint: srv.URL, creds: "udid:tok"}
	if err := s.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if seen {
		t.Fatal("X-K2-Client header sent despite empty clientHeader")
	}
}

func TestSubscription_FetchServerRefreshOverride(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refresh := 300
		resp := subsResponse{
			Tunnels: []Tunnel{{URL: "k2v5://s1.example.com:443", Weight: 1}},
			Refresh: &refresh,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	raw := "k2subs://u:p@" + srv.Listener.Addr().String() + "/api?refresh=1800"
	s, err := NewSubscription(raw, "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	s.endpoint = srv.URL + "/api"

	if err := s.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	s.mu.Lock()
	interval := s.interval
	s.mu.Unlock()
	if interval != 300*time.Second {
		t.Errorf("interval = %v, want 300s", interval)
	}
}

func TestSubscription_FetchDefaultWeight(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Weights omitted (zero values in Go).
		resp := subsResponse{
			Tunnels: []Tunnel{
				{URL: "k2v5://s1.example.com:443"},
				{URL: "k2v5://s2.example.com:443"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	raw := "k2subs://u:p@" + srv.Listener.Addr().String() + "/api"
	s, err := NewSubscription(raw, "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	s.endpoint = srv.URL + "/api"

	if err := s.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	for _, tun := range s.Tunnels() {
		if tun.Weight != 1 {
			t.Errorf("tunnel weight = %d, want 1 (default)", tun.Weight)
		}
	}
}

func TestSubscription_FetchFailPreservesOldData(t *testing.T) {
	s := &Subscription{
		endpoint: "https://unreachable.invalid/api",
		creds:    "u:p",
	}
	s.tunnels = []Tunnel{{URL: "k2v5://old.example.com:443", Weight: 1}}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := s.Fetch(ctx, http.DefaultClient)
	if err == nil {
		t.Fatal("expected fetch to fail")
	}

	tunnels := s.Tunnels()
	if len(tunnels) != 1 || tunnels[0].URL != "k2v5://old.example.com:443" {
		t.Errorf("old data not preserved after failed fetch: %v", tunnels)
	}
}

func TestSubscription_DiskCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := subsResponse{
			Tunnels: []Tunnel{
				{URL: "k2v5://cached.example.com:443", Weight: 5},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	raw := "k2subs://u:p@" + srv.Listener.Addr().String() + "/api"

	// First subscription: fetch and cache.
	s1, err := NewSubscription(raw, dir)
	if err != nil {
		t.Fatalf("NewSubscription s1: %v", err)
	}
	s1.endpoint = srv.URL + "/api"
	if err := s1.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatalf("Fetch s1: %v", err)
	}

	// Verify cache file exists.
	if _, err := os.Stat(s1.cacheFile); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	// Second subscription with same URL: should load from cache.
	s2, err := NewSubscription(raw, dir)
	if err != nil {
		t.Fatalf("NewSubscription s2: %v", err)
	}

	tunnels := s2.Tunnels()
	if len(tunnels) != 1 {
		t.Fatalf("got %d tunnels from cache, want 1", len(tunnels))
	}
	if !contains(tunnels[0].URL, "cached.example.com") {
		t.Errorf("cached tunnel URL = %q, want cached.example.com", tunnels[0].URL)
	}
}

// TestSubscription_CacheCorruptIgnored locks in the UAT T15 contract:
// a corrupt cache file must NOT prevent NewSubscription from succeeding —
// it is logged and ignored, and a subsequent Fetch overwrites it.
func TestSubscription_CacheCorruptIgnored(t *testing.T) {
	dir := t.TempDir()
	raw := "k2subs://u:p@host.example.com/api"

	// Pre-create a corrupt cache file at the expected path.
	s0, err := NewSubscription(raw, dir)
	if err != nil {
		t.Fatalf("NewSubscription (probe): %v", err)
	}
	cachePath := s0.cacheFile
	if err := os.MkdirAll(dirOf(cachePath), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("{not-json"), 0600); err != nil {
		t.Fatalf("write corrupt cache: %v", err)
	}

	// NewSubscription must not fail; tunnels should be empty (corrupt ignored).
	s, err := NewSubscription(raw, dir)
	if err != nil {
		t.Fatalf("NewSubscription with corrupt cache: %v", err)
	}
	if got := len(s.Tunnels()); got != 0 {
		t.Errorf("tunnels after corrupt cache load = %d, want 0", got)
	}
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}

// TestSubscription_FetchErrorHintPassthrough locks in the UAT T07/T09 contract:
// when Center returns an HTTP 4xx with a plain-text body, the returned error
// must include BOTH the status code AND the body text so the daemon can
// surface a human-readable hint to the user.
func TestSubscription_FetchErrorHintPassthrough(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid credentials"))
	}))
	defer srv.Close()

	raw := "k2subs://u:p@" + srv.Listener.Addr().String() + "/api"
	s, err := NewSubscription(raw, "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	s.endpoint = srv.URL + "/api"

	err = s.Fetch(context.Background(), srv.Client())
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	msg := err.Error()
	if !strings.Contains(msg, "401") {
		t.Errorf("error missing status code: %q", msg)
	}
	if !strings.Contains(msg, "invalid credentials") {
		t.Errorf("error missing body hint: %q", msg)
	}
}

// --- Task 3: Weighted Pick ---

func TestSubscription_PickWeightedDistribution(t *testing.T) {
	s := &Subscription{}
	s.tunnels = []Tunnel{
		{URL: "k2v5://heavy.example.com:443", Weight: 3},
		{URL: "k2v5://light.example.com:443", Weight: 1},
	}

	counts := make(map[string]int)
	n := 10000
	for i := 0; i < n; i++ {
		tun, err := s.Pick(nil)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		counts[tun.URL]++
	}

	heavy := counts["k2v5://heavy.example.com:443"]
	light := counts["k2v5://light.example.com:443"]

	// Expected: 75% heavy, 25% light. Tolerance: 60-90% for heavy.
	heavyPct := float64(heavy) / float64(n) * 100
	if heavyPct < 60 || heavyPct > 90 {
		t.Errorf("heavy = %.1f%% (%d/%d), want 60-90%%", heavyPct, heavy, n)
	}
	_ = light // implicit check via total
}

func TestSubscription_PickExclude(t *testing.T) {
	s := &Subscription{}
	s.tunnels = []Tunnel{
		{URL: "k2v5://a.example.com:443", Weight: 1},
		{URL: "k2v5://b.example.com:443", Weight: 1},
	}

	for i := 0; i < 100; i++ {
		tun, err := s.Pick([]string{"k2v5://a.example.com:443"})
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if tun.URL == "k2v5://a.example.com:443" {
			t.Fatal("excluded tunnel was picked")
		}
	}
}

func TestSubscription_PickFallbackZeroWeight(t *testing.T) {
	s := &Subscription{}
	s.tunnels = []Tunnel{
		{URL: "k2v5://weighted.example.com:443", Weight: 5},
		{URL: "k2v5://zero.example.com:443", Weight: 0},
	}

	// Exclude weighted one — should fall back to weight=0.
	tun, err := s.Pick([]string{"k2v5://weighted.example.com:443"})
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if tun.URL != "k2v5://zero.example.com:443" {
		t.Errorf("expected zero-weight fallback, got %q", tun.URL)
	}
}

func TestSubscription_PickAllExcluded(t *testing.T) {
	s := &Subscription{}
	s.tunnels = []Tunnel{
		{URL: "k2v5://a.example.com:443", Weight: 1},
	}
	_, err := s.Pick([]string{"k2v5://a.example.com:443"})
	if err == nil {
		t.Fatal("expected error when all tunnels excluded")
	}
}

func TestSubscription_PickEmptyList(t *testing.T) {
	s := &Subscription{}
	_, err := s.Pick(nil)
	if err == nil {
		t.Fatal("expected error on empty tunnel list")
	}
}

// TestSubscription_PickRecommendScorePreferred verifies that when both
// RecommendScore and Weight are set, RecommendScore drives the distribution
// and Weight is ignored. This is the canonical-field invariant.
func TestSubscription_PickRecommendScorePreferred(t *testing.T) {
	s := &Subscription{}
	// Weight says "B dominates 10:1", RecommendScore says "A dominates 9:1".
	// The canonical field wins: ~90% A.
	s.tunnels = []Tunnel{
		{URL: "k2v5://a.example.com:443", Weight: 1, RecommendScore: 0.9},
		{URL: "k2v5://b.example.com:443", Weight: 10, RecommendScore: 0.1},
	}

	counts := make(map[string]int)
	n := 10000
	for i := 0; i < n; i++ {
		tun, err := s.Pick(nil)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		counts[tun.URL]++
	}

	aPct := float64(counts["k2v5://a.example.com:443"]) / float64(n) * 100
	if aPct < 80 || aPct > 95 {
		t.Errorf("A = %.1f%%, want 80-95%% (recommendScore 0.9 vs 0.1)", aPct)
	}
}

// TestSubscription_PickLegacyWeightFallback verifies that when RecommendScore
// is absent (pre-rollout Center responses), Pick falls back to Weight. This is
// the backward-compat contract for one release cycle.
func TestSubscription_PickLegacyWeightFallback(t *testing.T) {
	s := &Subscription{}
	s.tunnels = []Tunnel{
		{URL: "k2v5://a.example.com:443", Weight: 3}, // no RecommendScore set (zero value)
		{URL: "k2v5://b.example.com:443", Weight: 1},
	}

	counts := make(map[string]int)
	n := 10000
	for i := 0; i < n; i++ {
		tun, err := s.Pick(nil)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		counts[tun.URL]++
	}

	// Expected 3:1 — A ≈ 75%, tolerate 65-85%.
	aPct := float64(counts["k2v5://a.example.com:443"]) / float64(n) * 100
	if aPct < 65 || aPct > 85 {
		t.Errorf("A = %.1f%%, want 65-85%% (legacy weight 3:1)", aPct)
	}
}

func TestSubscription_PickZeroWeightSkipped(t *testing.T) {
	s := &Subscription{}
	s.tunnels = []Tunnel{
		{URL: "k2v5://weighted.example.com:443", Weight: 5},
		{URL: "k2v5://zero.example.com:443", Weight: 0},
	}

	// With both available, weight=0 should be skipped (weighted candidates exist).
	for i := 0; i < 100; i++ {
		tun, err := s.Pick(nil)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if tun.URL == "k2v5://zero.example.com:443" {
			t.Fatal("weight=0 tunnel should be skipped when weighted candidates exist")
		}
	}
}

// --- Task 4: Background Refresh + Close ---

func TestSubscription_BackgroundRefresh(t *testing.T) {
	var fetchCount atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		resp := subsResponse{
			Tunnels: []Tunnel{{URL: "k2v5://bg.example.com:443", Weight: 1}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	raw := "k2subs://u:p@" + srv.Listener.Addr().String() + "/api?refresh=1"
	s, err := NewSubscription(raw, "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	s.endpoint = srv.URL + "/api"

	client := srv.Client()
	s.StartRefresh(context.Background(), func() *http.Client { return client })

	// Wait enough for at least 2 fetches (refresh=1s, wait 2.5s).
	time.Sleep(2500 * time.Millisecond)
	s.Close()

	count := fetchCount.Load()
	if count < 2 {
		t.Errorf("fetchCount = %d, want >= 2", count)
	}
}

func TestSubscription_CloseIdempotent(t *testing.T) {
	s, err := NewSubscription("k2subs://u:p@host.com/api", "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}

	// Close without StartRefresh — should be safe.
	s.Close()
	s.Close()

	// Start and close multiple times.
	s.StartRefresh(context.Background(), func() *http.Client { return http.DefaultClient })
	s.Close()
	s.Close()
}

// --- Task 10: End-to-end integration test ---

func TestSubscription_EndToEnd(t *testing.T) {
	// Simulates the full flow: parse → fetch → pick → failover → cache.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "alice" || pass != "tok" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path != "/subs/team" {
			w.WriteHeader(404)
			return
		}
		refreshSecs := 300
		json.NewEncoder(w).Encode(subsResponse{
			Tunnels: []Tunnel{
				{URL: "k2v5://srv-a:443?ech=abc&pin=sha256:xyz", Weight: 60},
				{URL: "k2v5://srv-b:443?ech=def", Weight: 40},
				{URL: "k2v5://srv-backup:443?ech=ghi", Weight: 0},
			},
			Refresh: &refreshSecs,
		})
	}))
	defer srv.Close()

	subsURL := "k2subs://alice:tok@" + srv.URL[len("https://"):] + "/subs/team?refresh=1800"
	cacheDir := t.TempDir()

	// 1. Parse.
	sub, err := NewSubscription(subsURL, cacheDir)
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}

	// 2. Fetch.
	if err := sub.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// 3. Verify tunnels have injected credentials.
	tunnels := sub.Tunnels()
	if len(tunnels) != 3 {
		t.Fatalf("got %d tunnels, want 3", len(tunnels))
	}
	for _, tun := range tunnels {
		if !strings.Contains(tun.URL, "alice:tok@") {
			t.Errorf("credentials not injected in %q", tun.URL)
		}
	}

	// 4. Verify server refresh override.
	sub.mu.Lock()
	interval := sub.interval
	sub.mu.Unlock()
	if interval != 300*time.Second {
		t.Errorf("interval = %v, want 300s", interval)
	}

	// 5. Pick — should never return backup (weight=0) normally.
	for range 1000 {
		tun, err := sub.Pick(nil)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(tun.URL, "srv-backup") {
			t.Fatal("weight=0 server should not be picked normally")
		}
	}

	// 6. Failover — exclude both non-zero, should get backup.
	var excludeA, excludeB string
	for _, tun := range tunnels {
		if strings.Contains(tun.URL, "srv-a") {
			excludeA = tun.URL
		}
		if strings.Contains(tun.URL, "srv-b:") {
			excludeB = tun.URL
		}
	}
	backup, err := sub.Pick([]string{excludeA, excludeB})
	if err != nil {
		t.Fatalf("Pick with exclusions: %v", err)
	}
	if !strings.Contains(backup.URL, "srv-backup") {
		t.Errorf("expected backup server, got %q", backup.URL)
	}

	// 7. Cache survives restart.
	sub2, _ := NewSubscription(subsURL, cacheDir)
	if len(sub2.Tunnels()) != 3 {
		t.Fatalf("cache not loaded: got %d tunnels", len(sub2.Tunnels()))
	}
}

// --- ScoreSource integration ---

type mockScoreSource struct{ scores map[string]float64 }

func (m mockScoreSource) Score(url string) (float64, bool) {
	s, ok := m.scores[url]
	return s, ok
}

// newSubWithTunnels builds a Subscription with canned tunnels. Relies on the
// exported SetTunnelsForTest helper added in this task.
func newSubWithTunnels(t *testing.T, tunnels []Tunnel) *Subscription {
	t.Helper()
	s, err := NewSubscription("k2subs://u:p@host.com/sub", "")
	if err != nil {
		t.Fatal(err)
	}
	s.SetTunnelsForTest(tunnels)
	return s
}

func TestPick_NoScoreSource_BehavesAsBefore(t *testing.T) {
	s := newSubWithTunnels(t, []Tunnel{
		{URL: "k2v5://a", RecommendScore: 0.9},
		{URL: "k2v5://b", RecommendScore: 0.1},
	})
	counts := map[string]int{}
	for i := 0; i < 2000; i++ {
		p, err := s.Pick(nil)
		if err != nil {
			t.Fatal(err)
		}
		counts[p.URL]++
	}
	if counts["k2v5://a"] <= counts["k2v5://b"] {
		t.Errorf("expected a-wins bias (base weight), got %+v", counts)
	}
}

func TestPick_ScoreSource_AppliesAdjustment(t *testing.T) {
	// Under linear `base * score`:
	//   a: 0.9 * 0.1 = 0.09  (high base, low probe)
	//   b: 0.5 * 1.0 = 0.50  (moderate base, perfect probe)
	// Probe signal flips the expected ranking: b should dominate ~5.5×.
	s := newSubWithTunnels(t, []Tunnel{
		{URL: "k2v5://a", RecommendScore: 0.9},
		{URL: "k2v5://b", RecommendScore: 0.5},
	})
	s.SetScoreSource(mockScoreSource{scores: map[string]float64{
		"k2v5://a": 0.1,
		"k2v5://b": 1.0,
	}})
	counts := map[string]int{}
	for i := 0; i < 2000; i++ {
		p, _ := s.Pick(nil)
		counts[p.URL]++
	}
	if counts["k2v5://b"] <= counts["k2v5://a"] {
		t.Errorf("b should dominate after probe adjustment (linear), got %+v", counts)
	}
}

func TestPick_ScoreSource_UnreachableExcluded(t *testing.T) {
	s := newSubWithTunnels(t, []Tunnel{
		{URL: "k2v5://a", RecommendScore: 0.9},
		{URL: "k2v5://b", RecommendScore: 0.5},
	})
	s.SetScoreSource(mockScoreSource{scores: map[string]float64{
		"k2v5://a": 0,
		"k2v5://b": 0.5,
	}})
	for i := 0; i < 50; i++ {
		p, err := s.Pick(nil)
		if err != nil {
			t.Fatal(err)
		}
		if p.URL == "k2v5://a" {
			t.Fatalf("picked unreachable: %s", p.URL)
		}
	}
}

func TestPick_ScoreSource_SentinelNoOp(t *testing.T) {
	s := newSubWithTunnels(t, []Tunnel{
		{URL: "k2v5://a", RecommendScore: 0.9},
		{URL: "k2v5://b", RecommendScore: 0.1},
	})
	s.SetScoreSource(mockScoreSource{scores: map[string]float64{
		"k2v5://a": -1,
		"k2v5://b": -1,
	}})
	counts := map[string]int{}
	for i := 0; i < 2000; i++ {
		p, _ := s.Pick(nil)
		counts[p.URL]++
	}
	if counts["k2v5://a"] <= counts["k2v5://b"] {
		t.Errorf("sentinel should not penalize; got %+v", counts)
	}
}

func TestPick_ScoreSource_TopKLimit(t *testing.T) {
	tunnels := []Tunnel{}
	for _, c := range "abcdefg" {
		tunnels = append(tunnels, Tunnel{URL: "k2v5://" + string(c), RecommendScore: 1})
	}
	s := newSubWithTunnels(t, tunnels)
	s.SetScoreSource(mockScoreSource{scores: map[string]float64{
		"k2v5://a": 0.9,
		"k2v5://b": 0.8,
		"k2v5://c": 0.7,
		"k2v5://d": 0.6,
		"k2v5://e": 0.5,
		"k2v5://f": 0.4,
		"k2v5://g": 0.3,
	}})
	seen := map[string]bool{}
	for i := 0; i < 3000; i++ {
		p, _ := s.Pick(nil)
		seen[p.URL] = true
	}
	if seen["k2v5://f"] || seen["k2v5://g"] {
		t.Errorf("top-K=5 violated; seen = %+v", seen)
	}
	for _, u := range []string{"k2v5://a", "k2v5://b", "k2v5://c", "k2v5://d", "k2v5://e"} {
		if !seen[u] {
			t.Errorf("top-5 candidate %s never selected; seen = %+v", u, seen)
		}
	}
}

// --- Task N: slot_bindings (enterprise multi-slot gateway) ---

func TestFetch_SlotBindings(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(subsResponse{
			Tunnels: []Tunnel{
				{URL: "k2v5://a:b@1.1.1.1:443", Weight: 1},
				{URL: "k2v5://a:b@2.2.2.2:443", Weight: 1},
			},
			SlotBindings: []SlotBinding{
				{Slot: 1, Country: "ae", Index: 1, TunnelIndex: 0},
				{Slot: 5, Country: "gn", Index: 1, TunnelIndex: 1},
			},
		})
	}))
	defer srv.Close()

	subsURL := "k2subs://alice:tok@" + srv.URL[len("https://"):] + "/subs/team"
	s, err := NewSubscription(subsURL, "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}

	var cbGot []SlotBinding
	s.SetOnSlotBindings(func(b []SlotBinding) { cbGot = b })
	if err := s.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatal(err)
	}
	got := s.SlotBindings()
	if len(got) != 2 || got[0] != (SlotBinding{Slot: 1, Country: "ae", Index: 1, TunnelIndex: 0}) {
		t.Fatalf("SlotBindings = %+v", got)
	}
	if len(cbGot) != 2 {
		t.Fatalf("callback got %+v", cbGot)
	}
}

func TestCache_SlotBindingsRoundTrip_And_LegacyFallback(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(subsResponse{
			Tunnels: []Tunnel{
				{URL: "k2v5://a:b@1.1.1.1:443", Weight: 1},
			},
			SlotBindings: []SlotBinding{
				{Slot: 1, Country: "ae", Index: 1, TunnelIndex: 0},
			},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	subsURL := "k2subs://alice:tok@" + srv.URL[len("https://"):] + "/subs/team"

	// 新格式写入(Fetch 触发 writeCache)→读回(新 Subscription 走 loadCache)
	s1, err := NewSubscription(subsURL, dir)
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	if err := s1.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatal(err)
	}
	s2, err := NewSubscription(subsURL, dir)
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	if got := s2.SlotBindings(); len(got) != 1 || got[0].Country != "ae" {
		t.Fatalf("cache roundtrip: %+v", got)
	}
	if len(s2.Tunnels()) != 1 {
		t.Fatalf("cache roundtrip tunnels: %+v", s2.Tunnels())
	}

	// 旧格式(裸 []Tunnel JSON)可读、bindings 为空
	legacy := s1.cacheFile
	if err := os.WriteFile(legacy, []byte(`[{"url":"k2v5://a:b@1.1.1.1:443"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	s3, err := NewSubscription(subsURL, dir)
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	if len(s3.Tunnels()) != 1 || len(s3.SlotBindings()) != 0 {
		t.Fatalf("legacy fallback: tunnels=%d bindings=%d", len(s3.Tunnels()), len(s3.SlotBindings()))
	}
}

// --- Helpers ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

