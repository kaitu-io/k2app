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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// Subscription manages a k2subs:// subscription URL: parsing, HTTP fetch,
// disk caching, weighted random server selection, and background refresh.
type Subscription struct {
	// Immutable after construction.
	endpoint  string        // https:// URL (no userinfo, no query)
	refresh   time.Duration // client-side fallback refresh interval
	cacheFile string        // disk cache path ("" = no disk cache)

	// clientHeader, when non-empty, is sent as the X-K2-Client request header
	// on every Fetch (e.g. "kaitu-router/0.4.5 (linux; arm64)"). Lets Center's
	// /api/subs identify a k2r router and route to the gateway branch. Empty =
	// header omitted.
	clientHeader string

	// State (mu-protected).
	mu deadlock.Mutex
	// raw / creds were immutable pre-Phase-0. Center now rotates the tunnel
	// credential by embedding a fresh udid:token in every subs-response
	// tunnel URL (spec §4.3); Fetch adopts it here so the NEXT fetch's Basic
	// auth and the persisted k2subs:// URL both carry the new token.
	raw   string // k2subs:// URL, userinfo rewritten on rotation
	creds string // "user:pass", rotated by Fetch
	// onCredsRotate is invoked OUTSIDE mu after a fetch that adopted rotated
	// creds, with the previous and updated raw k2subs:// URL. Platform
	// shells persist newRaw (daemon persistedState / k2r applyCredential).
	onCredsRotate func(oldRaw, newRaw string)

	tunnels     []Tunnel
	fetchedAt   time.Time
	interval    time.Duration // effective refresh interval
	scoreSource ScoreSource   // optional, consulted by Pick when non-nil
	// controlKeyHash is the account-level router control-key hash delivered by
	// Center in the subs response ("" until first fetch carries one). Consumed
	// by the k2r gateway to gate its panel HTTP API.
	controlKeyHash   string
	onControlKeyHash func(string) // invoked OUTSIDE mu after a fetch that carried a hash

	// slotBindings is the enterprise multi-slot gateway manifest ("" tunnels
	// mode when nil/empty). Cross-repo wire contract with Center /api/subs.
	slotBindings   []SlotBinding
	onSlotBindings func([]SlotBinding) // invoked OUTSIDE mu after every fetch, empty included

	cancel context.CancelFunc
	done   chan struct{}
}

// Tunnel represents a single server endpoint in a subscription.
//
// Weight is retained for backward compatibility with pre-recommendScore Center
// responses. New code should prefer RecommendScore, which Pick consults first.
// One release after RecommendScore ships Center-side we can drop Weight.
type Tunnel struct {
	URL string `json:"url"`
	// Weight is the legacy integer weight (derived by Center from
	// round(RecommendScore * 100)). Preferred path: RecommendScore. Retained
	// for pre-recommendScore Center deployments.
	Weight int `json:"weight"`
	// RecommendScore is the canonical [0.0, 1.0] recommendation signal from
	// Center (api.ComputeRecommendScore). Higher = better. Non-cloud nodes
	// default to 0.5 (neutral). Zero here has no special meaning; when both
	// Weight and RecommendScore are zero, effectiveWeight falls back to a
	// pool-level "all zero" fallback in Pick.
	RecommendScore float64 `json:"recommendScore"`
}

// subsResponse is the JSON response from the subscription endpoint.
type subsResponse struct {
	Tunnels []Tunnel `json:"tunnels"`
	Refresh *int     `json:"refresh,omitempty"`
	// ControlKeyHash is the SHA-256 hex of the owning account's router
	// control key. Cross-repo contract with Center /api/subs (snake_case).
	ControlKeyHash string `json:"control_key_hash,omitempty"`
	// SlotBindings is the enterprise multi-slot gateway manifest. Non-empty
	// = k2r enters multi-slot mode. Cross-repo contract with Center
	// /api/subs (snake_case); consumer subs responses omit this field.
	SlotBindings []SlotBinding `json:"slot_bindings,omitempty"`
}

// SlotBinding maps one fixed router slot to a tunnel in the same subs
// response. Cross-repo wire contract with Center /api/subs (snake_case);
// enterprise multi-slot gateway mode. TunnelIndex indexes into the
// response's tunnels array (same-order invariant kept by s.tunnels).
type SlotBinding struct {
	Slot        int    `json:"slot"`
	Country     string `json:"country"`
	Index       int    `json:"index"`
	TunnelIndex int    `json:"tunnel_index"`
}

// ScoreSource is an optional per-URL adjustment consulted by Pick.
// Supplied by probe.Registry in production.
//
// Score convention (mirrors wire.ProbeStats.Score):
//
//	ok=false   no data — Pick treats as "no adjustment" (factor 1.0)
//	-1         sentinel (reachable but unmeasured, e.g. old k2s) — factor 1.0
//	 0         unreachable — tunnel excluded from the pool
//	(0,1]      normal — adjustment = score
type ScoreSource interface {
	Score(url string) (float64, bool)
}

// topKPoolSize caps how many adjusted candidates Pick samples from when a
// ScoreSource is installed. Prevents low-quality tunnels from diluting the
// selection while keeping diversity (survives one bad candidate).
const topKPoolSize = 5

// effectiveWeight returns the base selection weight for a tunnel. Prefers
// RecommendScore when non-zero (canonical [0,1] signal); falls back to the
// legacy integer Weight scaled by 0.01 for backward compatibility with
// pre-recommendScore Center responses. Returns 0 when both are 0 — Pick
// treats an all-zero pool via the zeroWeight fallback.
func effectiveWeight(t Tunnel) float64 {
	if t.RecommendScore > 0 {
		return t.RecommendScore
	}
	if t.Weight > 0 {
		return float64(t.Weight) * 0.01
	}
	return 0
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
		// Key the cache filename on endpoint (credential-stripped: no userinfo,
		// just scheme+host+path+query), NOT raw (which embeds udid:token).
		// Phase 0 credential rotation (spec §4.3) rewrites raw's userinfo in
		// place — same Subscription instance, s.cacheFile computed once here
		// stays correct across that in-memory rotation regardless. But a
		// process RESTART re-derives cacheFile from the *persisted* raw
		// (Task 7/8 write the rotated URL back to disk), and hashing raw
		// there would compute a different filename every restart-after-
		// rotation — loadCache() would miss and P5's cache fallback (see
		// subscription/resolve.go) would find nothing to degrade to. endpoint
		// never changes across rotation (only the userinfo does), so hashing
		// it keeps the filename stable across both in-memory rotation and any
		// later restart that starts from the rotated raw URL.
		h := sha256.Sum256([]byte(endpoint))
		name := fmt.Sprintf("subs-%x.json", h[:4])
		s.cacheFile = filepath.Join(cacheDir, name)
		s.loadCache()
	}

	return s, nil
}

// SetScoreSource installs an optional per-URL adjustment used by Pick.
// Pass nil to revert to pure recommendScore/weight behavior.
func (s *Subscription) SetScoreSource(src ScoreSource) {
	s.mu.Lock()
	s.scoreSource = src
	s.mu.Unlock()
}

// SetClientHeader sets the X-K2-Client request header sent on every Fetch
// (lets k2r identify itself as a router so Center routes to the gateway
// branch). Set once before StartRefresh; mu-guarded for the refresh goroutine.
func (s *Subscription) SetClientHeader(h string) {
	s.mu.Lock()
	s.clientHeader = h
	s.mu.Unlock()
}

// SetOnControlKeyHash registers a callback fired (outside the state lock)
// after every Fetch whose response carries a non-empty control_key_hash.
// Set before StartRefresh.
func (s *Subscription) SetOnControlKeyHash(fn func(string)) {
	s.mu.Lock()
	s.onControlKeyHash = fn
	s.mu.Unlock()
}

// ControlKeyHash returns the last control-key hash from Center ("" = none yet).
func (s *Subscription) ControlKeyHash() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.controlKeyHash
}

// SlotBindings returns a copy of the current slot binding manifest.
func (s *Subscription) SlotBindings() []SlotBinding {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SlotBinding, len(s.slotBindings))
	copy(out, s.slotBindings)
	return out
}

// SetOnSlotBindings registers a callback fired after every successful
// Fetch with the (possibly empty) manifest. Runs outside the state lock.
func (s *Subscription) SetOnSlotBindings(fn func([]SlotBinding)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onSlotBindings = fn
}

// Creds returns the current "user:pass" credential (rotates via Fetch).
func (s *Subscription) Creds() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creds
}

// RawURL returns the k2subs:// subscription URL carrying the CURRENT
// credentials — what platform shells should persist for restart/reconnect.
func (s *Subscription) RawURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw
}

// SetOnCredsRotate registers the rotation callback (fired outside mu).
// Set before StartRefresh.
func (s *Subscription) SetOnCredsRotate(fn func(oldRaw, newRaw string)) {
	s.mu.Lock()
	s.onCredsRotate = fn
	s.mu.Unlock()
}

// CacheAge reports how long ago the disk cache file was last written (file
// mtime). ok=false when no cache is configured or the file doesn't exist.
// cacheFile is immutable after construction — no lock needed.
func (s *Subscription) CacheAge() (time.Duration, bool) {
	if s.cacheFile == "" {
		return 0, false
	}
	fi, err := os.Stat(s.cacheFile)
	if err != nil {
		return 0, false
	}
	return time.Since(fi.ModTime()), true
}

// extractCreds returns the "user:pass" userinfo carried by the first tunnel
// URL that has one ("" when none). Center injects the same credential into
// every URL of a response, so the first hit is authoritative.
func extractCreds(tunnels []Tunnel) string {
	for _, t := range tunnels {
		u, err := url.Parse(t.URL)
		if err != nil || u.User == nil {
			continue
		}
		pass, ok := u.User.Password()
		if !ok || pass == "" || u.User.Username() == "" {
			continue
		}
		return u.User.Username() + ":" + pass
	}
	return ""
}

// SetTunnelsForTest replaces the current tunnel list. Tests only.
func (s *Subscription) SetTunnelsForTest(tunnels []Tunnel) {
	s.mu.Lock()
	s.tunnels = tunnels
	s.fetchedAt = time.Now()
	s.mu.Unlock()
}

// Fetch fetches the subscription endpoint and updates tunnels.
// On failure, existing tunnels are preserved.
func (s *Subscription) Fetch(ctx context.Context, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return fmt.Errorf("subscription fetch: build request: %w", err)
	}

	s.mu.Lock()
	creds := s.creds
	clientHeader := s.clientHeader
	s.mu.Unlock()

	user, pass, _ := strings.Cut(creds, ":")
	req.SetBasicAuth(user, pass)
	if clientHeader != "" {
		req.Header.Set("X-K2-Client", clientHeader)
	}

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

	// Phase 0 credential rotation (spec §4.3): adopt the credential Center
	// embedded in the response BEFORE re-injecting ours, so both the injected
	// tunnel URLs and the next fetch's Basic auth use the new token. Only
	// same-udid credentials are adopted (defense against a corrupted body).
	adopted := ""
	if newCreds := extractCreds(sr.Tunnels); newCreds != "" && newCreds != creds {
		curUser, _, _ := strings.Cut(creds, ":")
		newUser, _, _ := strings.Cut(newCreds, ":")
		if curUser == newUser {
			adopted = newCreds
			creds = newCreds
		}
	}

	// Inject credentials into each tunnel URL.
	for i := range sr.Tunnels {
		injected, err := injectCreds(sr.Tunnels[i].URL, creds)
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
	s.slotBindings = sr.SlotBindings
	s.fetchedAt = now
	s.interval = interval
	if sr.ControlKeyHash != "" {
		s.controlKeyHash = sr.ControlKeyHash
	}
	var oldRaw, newRaw string
	var credsCb func(string, string)
	if adopted != "" && s.creds != adopted {
		oldRaw = s.raw
		s.creds = adopted
		if nr, err := injectCreds(s.raw, adopted); err == nil {
			s.raw = nr
		}
		newRaw = s.raw
		credsCb = s.onCredsRotate
	}
	cb := s.onControlKeyHash
	cb2 := s.onSlotBindings
	s.mu.Unlock()

	// Callback runs outside the lock (concurrency constitution: no external
	// calls under a state lock).
	if cb != nil && sr.ControlKeyHash != "" {
		cb(sr.ControlKeyHash)
	}
	// Unconditional — an empty manifest is itself a meaningful change
	// (gateway must notice bindings being cleared).
	if cb2 != nil {
		cb2(sr.SlotBindings)
	}
	// oldRaw 只在上面锁区的采纳分支里被赋值，因此它非空 ⇔ 本次 Fetch 采纳了
	// 新凭据——用它做 "是否采纳" 的判据。DIAG 独立于回调是否注册。
	if oldRaw != "" {
		slog.Info("DIAG: subs-creds-rotate", "endpoint", s.endpoint)
	}
	if credsCb != nil {
		credsCb(oldRaw, newRaw)
	}

	// Write cache (outside lock).
	if s.cacheFile != "" {
		s.writeCache(sr.Tunnels, sr.SlotBindings)
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

// Pick selects a tunnel via weighted random. exclude are URLs to skip.
//
// Weight = effectiveWeight(t) * adjust(scoreSource, t.URL).
//
//	adjust(no data / sentinel -1) = 1.0   (no probe influence)
//	adjust(score > 0)             = score (linear, no squaring)
//	adjust(score == 0)            → tunnel excluded (unreachable)
//
// When a ScoreSource is active and more than topKPoolSize candidates remain,
// only the top K by weight are eligible (diversity without dilution).
func (s *Subscription) Pick(exclude []string) (Tunnel, error) {
	s.mu.Lock()
	tunnels := make([]Tunnel, len(s.tunnels))
	copy(tunnels, s.tunnels)
	src := s.scoreSource
	s.mu.Unlock()

	if len(tunnels) == 0 {
		return Tunnel{}, fmt.Errorf("subscription pick: no tunnels available")
	}

	excSet := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		excSet[e] = struct{}{}
	}

	type scored struct {
		t      Tunnel
		weight float64
	}

	var candidates []scored
	var zeroWeight []Tunnel
	for _, t := range tunnels {
		if _, ex := excSet[t.URL]; ex {
			continue
		}
		base := effectiveWeight(t)
		adj := 1.0
		probeExcluded := false
		if src != nil {
			if sc, ok := src.Score(t.URL); ok {
				switch {
				case sc < 0:
					adj = 1.0 // sentinel — no measurement, don't penalize
				case sc == 0:
					probeExcluded = true // unreachable — hard exclude
				default:
					adj = sc
				}
			}
		}
		if probeExcluded {
			continue // do not fall through to zeroWeight — probe says unreachable
		}
		eff := base * adj
		if eff > 0 {
			candidates = append(candidates, scored{t, eff})
		} else {
			zeroWeight = append(zeroWeight, t)
		}
	}

	allZero := false
	if len(candidates) == 0 {
		for _, t := range zeroWeight {
			candidates = append(candidates, scored{t, 1})
		}
		allZero = true
	}

	if len(candidates) == 0 {
		return Tunnel{}, fmt.Errorf("subscription pick: all tunnels excluded")
	}

	if src != nil && !allZero && len(candidates) > topKPoolSize {
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].weight > candidates[j].weight
		})
		candidates = candidates[:topKPoolSize]
	}

	totalWeight := 0.0
	for _, c := range candidates {
		totalWeight += c.weight
	}
	r := rand.Float64() * totalWeight
	for _, c := range candidates {
		r -= c.weight
		if r < 0 {
			return c.t, nil
		}
	}
	return candidates[len(candidates)-1].t, nil
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

// subsCacheFile is the on-disk cache format. Superseded the pre-multi-slot
// bare []Tunnel array; loadCache falls back to the legacy shape so caches
// written by older builds still load (bindings come back empty, which is
// correct — those builds never had bindings to lose).
type subsCacheFile struct {
	Tunnels      []Tunnel      `json:"tunnels"`
	SlotBindings []SlotBinding `json:"slot_bindings,omitempty"`
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
	var cf subsCacheFile
	if err := json.Unmarshal(data, &cf); err == nil && cf.Tunnels != nil {
		s.tunnels = cf.Tunnels
		s.slotBindings = cf.SlotBindings
		return
	}
	// Legacy shape: bare []Tunnel array (pre-multi-slot cache on disk).
	var tunnels []Tunnel
	if err := json.Unmarshal(data, &tunnels); err != nil {
		slog.Warn("subscription: cache corrupt, ignoring", "file", s.cacheFile, "err", err)
		return
	}
	s.tunnels = tunnels
}

func (s *Subscription) writeCache(tunnels []Tunnel, bindings []SlotBinding) {
	if s.cacheFile == "" {
		return
	}
	data, err := json.Marshal(subsCacheFile{Tunnels: tunnels, SlotBindings: bindings})
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
