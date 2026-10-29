package subscription

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kaitu-io/k2/config"
	"github.com/sasha-s/go-deadlock"
)

// Manager owns active Sessions for one engine session. It implements
// engine.OutboundProvider so the engine's outboundReplaceLoop can request
// a different k2v5:// URL when a route has persistently failed.
//
// Lifetime: one Manager per platform shell instance. Resolve() populates
// `sessions`; CloseAll() drains them. The shell calls these from doUp/doDown
// under its own lifecycle lock — Manager itself only protects Sessions slice.
//
// Lock order: mu (snapshot sessions slice) → release → per-Session excludeMu
// + Subscription.mu (via sub.Pick). NextURL never holds mu across sub.Pick.
type Manager struct {
	cacheDir   string
	httpClient HTTPClient
	scoreSrc   config.ScoreSource

	mu       deadlock.RWMutex
	sessions []*Session
}

// HTTPClient is the minimal http.Client surface Manager needs. http.Client
// satisfies it; tests can swap in fakes. The exact method set will be
// refined in a later task — this empty interface is a placeholder for the
// Manager constructor signature only.
type HTTPClient interface {
	// matches stdlib http.Client.Do signature subset Subscription.Fetch uses
}

// ErrNoSubscription is returned when NextURL receives a currentURL that
// no Session knows about — expected during the doDown/Stop wind-down
// window when the replace loop is still draining.
var ErrNoSubscription = errors.New("subscription: no matching session for URL")

// NewManager creates a Manager configured with cache directory, HTTP
// client (nil → default with TLS 1.2 minimum), and an optional ScoreSource
// (usually probe.Registry).
func NewManager(cacheDir string, httpClient HTTPClient, scoreSrc config.ScoreSource) *Manager {
	return &Manager{cacheDir: cacheDir, httpClient: httpClient, scoreSrc: scoreSrc}
}

// NextURL implements engine.OutboundProvider.
func (m *Manager) NextURL(currentURL string, reason error) (string, error) {
	m.mu.RLock()
	sessions := m.sessions
	m.mu.RUnlock()

	sess := findSessionByURL(sessions, currentURL)
	if sess == nil {
		return "", ErrNoSubscription
	}

	sess.excludeMu.Lock()
	already := false
	for _, u := range sess.excluded {
		if u == currentURL {
			already = true
			break
		}
	}
	if !already {
		sess.excluded = append(sess.excluded, currentURL)
	}
	excludedSnapshot := make([]string, len(sess.excluded))
	copy(excludedSnapshot, sess.excluded)
	sess.excludeMu.Unlock()

	tunnel, err := sess.sub.Pick(excludedSnapshot)
	if err != nil {
		slog.Warn("subscription: exhausted",
			"excluded", len(excludedSnapshot),
			"reason", reason,
			"err", err)
		return "", fmt.Errorf("all subscribed tunnels failed: %w", err)
	}

	slog.Info("subscription: replacement selected",
		"excludedCount", len(excludedSnapshot),
		"newURL", config.TruncateVia(tunnel.URL),
		"reason", reason)
	return tunnel.URL, nil
}

func findSessionByURL(sessions []*Session, url string) *Session {
	for _, s := range sessions {
		for _, t := range s.sub.Tunnels() {
			if t.URL == url {
				return s
			}
		}
	}
	return nil
}

// CandidateURLs returns the union of all tunnel URLs across active sessions.
// Order follows session insertion order with first-seen semantics across
// duplicates. Returns nil/empty when no sessions are active. Used by
// platform shells to feed background probe loops.
//
// Concurrency: snapshots sessions slice under m.mu.RLock, then releases the
// lock before calling sub.Tunnels() on each session. Subscription.mu is a
// standalone lock — never held while m.mu is held.
func (m *Manager) CandidateURLs() []string {
	m.mu.RLock()
	sessions := make([]*Session, len(m.sessions))
	copy(sessions, m.sessions)
	m.mu.RUnlock()

	seen := make(map[string]struct{})
	var urls []string
	for _, sess := range sessions {
		for _, t := range sess.sub.Tunnels() {
			if _, ok := seen[t.URL]; ok {
				continue
			}
			seen[t.URL] = struct{}{}
			urls = append(urls, t.URL)
		}
	}
	return urls
}

// SlotBindings returns the slot manifest from the first active session
// (k2r runs a single k2subs subscription). Empty = consumer mode.
func (m *Manager) SlotBindings() []config.SlotBinding {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, sess := range m.sessions {
		return sess.sub.SlotBindings()
	}
	return nil
}

// Tunnels returns the ordered tunnel list from the first active session.
// Order matches the subs response — SlotBinding.TunnelIndex indexes it.
func (m *Manager) Tunnels() []config.Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, sess := range m.sessions {
		return sess.sub.Tunnels()
	}
	return nil
}

// Sessions returns a snapshot of active sessions. Intended for platform
// shells that need to drive Session.sub.StartRefresh from doUp. The
// returned slice is a copy — mutating it does not affect Manager state,
// but the *Session pointers reference live state.
func (m *Manager) Sessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sessions := make([]*Session, len(m.sessions))
	copy(sessions, m.sessions)
	return sessions
}

// SessionsLen returns the active session count. Test-only helper used to
// assert that doDown / CloseAll has drained the slice.
func (m *Manager) SessionsLen() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// HTTPClient returns the http.Client a Session built (or was supplied with)
// during Resolve. Returns nil if not yet fetched. Used by platform shells
// to wire StartRefresh against the same Transport that Close will release.
func (s *Session) HTTPClient() *http.Client {
	return s.httpClient
}

// Subscription returns the underlying *config.Subscription. Used by
// platform shells to call StartRefresh and by tests that need to inspect
// the parsed tunnel list.
func (s *Session) Subscription() *config.Subscription {
	return s.sub
}
