package config

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
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
type Tunnel struct {
	URL    string `json:"url"`
	Weight int    `json:"weight"`
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

	// Build endpoint: https scheme, no userinfo, no query.
	ep := &url.URL{
		Scheme: "https",
		Host:   u.Host,
		Path:   u.Path,
	}
	endpoint := ep.String()

	// Parse refresh query param (seconds), default 1800.
	refreshSec := 1800
	if v := u.Query().Get("refresh"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("subscription: parse refresh=%q: %w", v, err)
		}
		refreshSec = n
	}
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

// Pick selects a tunnel using weighted random selection, excluding URLs in exclude.
// Falls back to weight=0 tunnels if no weighted candidates remain.
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

	// Build weighted candidates (weight > 0, not excluded).
	var candidates []Tunnel
	var zeroWeight []Tunnel
	for _, t := range tunnels {
		if _, excluded := excSet[t.URL]; excluded {
			continue
		}
		if t.Weight > 0 {
			candidates = append(candidates, t)
		} else {
			zeroWeight = append(zeroWeight, t)
		}
	}

	// Fall back to weight=0 if no weighted candidates.
	if len(candidates) == 0 {
		candidates = zeroWeight
	}

	if len(candidates) == 0 {
		return Tunnel{}, fmt.Errorf("subscription pick: all tunnels excluded")
	}

	// Weighted random selection.
	totalWeight := 0
	for _, c := range candidates {
		w := c.Weight
		if w <= 0 {
			w = 1 // fallback weight=0 candidates treated as 1
		}
		totalWeight += w
	}

	r := rand.IntN(totalWeight)
	for _, c := range candidates {
		w := c.Weight
		if w <= 0 {
			w = 1
		}
		r -= w
		if r < 0 {
			return c, nil
		}
	}

	// Should not reach here, but return last candidate.
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
		_ = s.Fetch(fetchCtx, clientFn())
		fetchCancel()
	}
}

func (s *Subscription) loadCache() {
	if s.cacheFile == "" {
		return
	}
	data, err := os.ReadFile(s.cacheFile)
	if err != nil {
		return
	}
	var tunnels []Tunnel
	if err := json.Unmarshal(data, &tunnels); err != nil {
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
