package config

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// Subscription manages a k2subs:// subscription URL: parsing, HTTP fetch,
// disk caching, weighted random server selection, and background refresh.
type Subscription struct {
	// Immutable after construction.
	raw       string
	endpoint  string        // https:// URL (no userinfo, no query)
	creds     string        // "user:pass"
	refresh   time.Duration // client-side fallback refresh interval
	cacheFile string        // disk cache path ("" = no disk cache)

	// State (mu-protected).
	mu        deadlock.Mutex
	tunnels   []Tunnel
	fetchedAt time.Time
	interval  time.Duration // effective refresh interval

	cancel context.CancelFunc
	done   chan struct{}
}

// Tunnel represents a single server endpoint in a subscription.
//
// Weight is retained for backward compatibility with pre-recommendScore Center
// responses. New code should prefer RecommendScore, which Pick consults first.
// One release after RecommendScore ships Center-side we can drop Weight.
type Tunnel struct {
	URL            string  `json:"url"`
	Weight         int     `json:"weight"`
	RecommendScore float64 `json:"recommendScore,omitempty"`
}

// effectiveWeight produces a positive sampling weight for pickWeighted.
// Prefers the canonical RecommendScore (float, [0,1]) when set; falls back to
// the legacy Weight int for old Center responses. Returns 0 when neither is
// positive — Pick's caller interprets that as "ineligible unless every
// candidate is zero" (uniform fallback).
func effectiveWeight(t Tunnel) float64 {
	if t.RecommendScore > 0 {
		return t.RecommendScore
	}
	if t.Weight > 0 {
		return float64(t.Weight)
	}
	return 0
}

// subsResponse is the JSON response from the subscription endpoint.
type subsResponse struct {
	Tunnels []Tunnel `json:"tunnels"`
	Refresh *int     `json:"refresh,omitempty"`
}

// NewSubscription parses a k2subs:// URL and returns a Subscription.
// If cacheDir is non-empty, loads any existing cache from disk.
func NewSubscription(raw string, cacheDir string) (*Subscription, error) {
	if raw == "" {
		return nil, fmt.Errorf("subscription: empty URL")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("subscription: parse URL: %w", err)
	}
	if u.Scheme != "k2subs" {
		return nil, fmt.Errorf("subscription: scheme %q, want k2subs", u.Scheme)
	}
	if u.User == nil {
		return nil, fmt.Errorf("subscription: missing credentials")
	}
	pass, ok := u.User.Password()
	if !ok || pass == "" {
		return nil, fmt.Errorf("subscription: missing password")
	}
	creds := u.User.Username() + ":" + pass

	// Parse refresh query param (seconds), default 1800.
	// `refresh` is a client-side directive — consumed here, NOT forwarded upstream.
	q := u.Query()
	refreshSec := 1800
	if v := q.Get("refresh"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("subscription: parse refresh=%q: %w", v, err)
		}
		refreshSec = n
	}
	q.Del("refresh")

	// Build endpoint: https scheme, no userinfo. Forward all non-refresh query
	// params (e.g. `country=jp` selects region at Center side).
	ep := &url.URL{
		Scheme:   "https",
		Host:     u.Host,
		Path:     u.Path,
		RawQuery: q.Encode(),
	}
	endpoint := ep.String()
	refreshDur := time.Duration(refreshSec) * time.Second

	s := &Subscription{
		raw:      raw,
		endpoint: endpoint,
		creds:    creds,
		refresh:  refreshDur,
		interval: refreshDur,
	}

	if cacheDir != "" {
		h := sha256.Sum256([]byte(raw))
		name := fmt.Sprintf("subs-%x.json", h[:4])
		s.cacheFile = filepath.Join(cacheDir, name)
		s.loadCache()
	}

	return s, nil
}

// Fetch fetches the subscription endpoint and updates tunnels.
// On failure, existing tunnels are preserved.
func (s *Subscription) Fetch(ctx context.Context, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return fmt.Errorf("subscription fetch: build request: %w", err)
	}

	user, pass, _ := strings.Cut(s.creds, ":")
	req.SetBasicAuth(user, pass)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("subscription fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		hint, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		if len(hint) > 0 {
			return fmt.Errorf("subscription fetch: status %d: %s", resp.StatusCode, strings.TrimSpace(string(hint)))
		}
		return fmt.Errorf("subscription fetch: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("subscription fetch: read body: %w", err)
	}

	var sr subsResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return fmt.Errorf("subscription fetch: parse JSON: %w", err)
	}

	// Inject credentials into each tunnel URL.
	for i := range sr.Tunnels {
		injected, err := injectCreds(sr.Tunnels[i].URL, s.creds)
		if err != nil {
			return fmt.Errorf("subscription fetch: inject creds into tunnel %d: %w", i, err)
		}
		sr.Tunnels[i].URL = injected
	}

	// Default weight: if ALL weights are 0, set all to 1.
	allZero := true
	for _, t := range sr.Tunnels {
		if t.Weight != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		for i := range sr.Tunnels {
			sr.Tunnels[i].Weight = 1
		}
	}

	// Determine effective interval.
	interval := s.refresh
	if sr.Refresh != nil {
		interval = time.Duration(*sr.Refresh) * time.Second
	}

	now := time.Now()

	s.mu.Lock()
	s.tunnels = sr.Tunnels
	s.fetchedAt = now
	s.interval = interval
	s.mu.Unlock()

	// Write cache (outside lock).
	if s.cacheFile != "" {
		s.writeCache(sr.Tunnels)
	}

	return nil
}

// Tunnels returns a copy of the current tunnel list.
func (s *Subscription) Tunnels() []Tunnel {
	s.mu.Lock()
	out := make([]Tunnel, len(s.tunnels))
	copy(out, s.tunnels)
	s.mu.Unlock()
	return out
}

// Pick selects a tunnel using weighted random selection, excluding URLs in
// exclude. Sampling weight comes from effectiveWeight (recommendScore first,
// legacy weight fallback). Candidates with zero effective weight are skipped
// unless nothing else is available, in which case they're treated uniformly.
func (s *Subscription) Pick(exclude []string) (Tunnel, error) {
	s.mu.Lock()
	tunnels := make([]Tunnel, len(s.tunnels))
	copy(tunnels, s.tunnels)
	s.mu.Unlock()

	if len(tunnels) == 0 {
		return Tunnel{}, fmt.Errorf("subscription pick: no tunnels available")
	}

	excSet := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		excSet[e] = struct{}{}
	}

	// Partition by effectiveWeight > 0 vs zero. Excluded candidates drop.
	var candidates []Tunnel
	var zeroWeight []Tunnel
	for _, t := range tunnels {
		if _, excluded := excSet[t.URL]; excluded {
			continue
		}
		if effectiveWeight(t) > 0 {
			candidates = append(candidates, t)
		} else {
			zeroWeight = append(zeroWeight, t)
		}
	}

	// Fall back to zero-weight candidates only if nothing positive remains —
	// preserves the pre-recommendScore invariant.
	allZero := false
	if len(candidates) == 0 {
		candidates = zeroWeight
		allZero = true
	}

	if len(candidates) == 0 {
		return Tunnel{}, fmt.Errorf("subscription pick: all tunnels excluded")
	}

	// Weighted random selection. Uniform when allZero is true.
	totalWeight := 0.0
	for _, c := range candidates {
		if allZero {
			totalWeight += 1
		} else {
			totalWeight += effectiveWeight(c)
		}
	}

	r := rand.Float64() * totalWeight
	for _, c := range candidates {
		w := 1.0
		if !allZero {
			w = effectiveWeight(c)
		}
		r -= w
		if r < 0 {
			return c, nil
		}
	}

	// Floating-point safety net — should only trip on accumulated drift.
	return candidates[len(candidates)-1], nil
}

// StartRefresh starts a background goroutine that periodically fetches the subscription.
// The clientFn is called each time to get an HTTP client (allows caller to control transport).
func (s *Subscription) StartRefresh(ctx context.Context, clientFn func() *http.Client) {
	s.mu.Lock()
	interval := s.interval
	s.mu.Unlock()

	if interval <= 0 {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	s.mu.Lock()
	s.cancel = cancel
	s.done = done
	s.mu.Unlock()

	go s.refreshLoop(ctx, clientFn, done)
}

// Close stops the background refresh goroutine and waits for it to exit.
// Safe to call multiple times.
func (s *Subscription) Close() {
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.done = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *Subscription) refreshLoop(ctx context.Context, clientFn func() *http.Client, done chan struct{}) {
	defer close(done)

	for {
		s.mu.Lock()
		interval := s.interval
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}

		fetchCtx, fetchCancel := context.WithTimeout(ctx, 30*time.Second)
		if err := s.Fetch(fetchCtx, clientFn()); err != nil {
			slog.Warn("DIAG: subs-refresh-fail", "endpoint", s.endpoint, "err", err)
		}
		fetchCancel()
	}
}

func (s *Subscription) loadCache() {
	if s.cacheFile == "" {
		return
	}
	data, err := os.ReadFile(s.cacheFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("subscription: cache read failed", "file", s.cacheFile, "err", err)
		}
		return
	}
	var tunnels []Tunnel
	if err := json.Unmarshal(data, &tunnels); err != nil {
		slog.Warn("subscription: cache corrupt, ignoring", "file", s.cacheFile, "err", err)
		return
	}
	s.tunnels = tunnels
}

func (s *Subscription) writeCache(tunnels []Tunnel) {
	if s.cacheFile == "" {
		return
	}
	data, err := json.Marshal(tunnels)
	if err != nil {
		return
	}

	dir := filepath.Dir(s.cacheFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		slog.Debug("subscription: cache dir create failed", "dir", dir, "err", err)
		return
	}
	tmp, err := os.CreateTemp(dir, ".subs-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, s.cacheFile); err != nil {
		os.Remove(tmpName)
	}
}

// injectCreds sets the userinfo on a URL string to the given "user:pass" credentials.
func injectCreds(rawURL, creds string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse tunnel URL: %w", err)
	}
	user, pass, _ := strings.Cut(creds, ":")
	u.User = url.UserPassword(user, pass)
	return u.String(), nil
}
