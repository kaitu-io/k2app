package subscription

import (
	"errors"
	"fmt"
	"log/slog"

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
