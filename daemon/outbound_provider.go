package daemon

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/kaitu-io/k2/config"
	"github.com/sasha-s/go-deadlock"
)

// subSession wraps a k2subs:// Subscription with session-local state for
// server rotation: the URLs the engine has asked us to skip via
// OutboundProvider.NextURL. On each NextURL call the previously-tried URL
// is appended to excluded and sub.Pick is called with the running set.
//
// Lifetime: created by resolveSubscriptions during doUp. sub.Close is
// called in closeTunnel. Outlives no engine — new doUp always reconstructs.
//
// Lock discipline: excludeMu protects excluded ONLY. It is never held
// across engine calls, never acquired while holding d.mu. sub.Pick has
// its own internal mutex (config.Subscription.mu) that is independent.
type subSession struct {
	sub *config.Subscription

	excludeMu deadlock.Mutex
	excluded  []string
}

// ErrNoSubscription is returned by Daemon.NextURL when the engine asks for
// a replacement URL but daemon has no active subscription session that
// knows the currentURL. This is expected during the window between
// doDown and engine.Stop returning — the engine's replace loop is
// winding down.
var ErrNoSubscription = errors.New("daemon: no matching subscription for URL")

// NextURL implements engine.OutboundProvider. Called by engine's
// outboundReplaceLoop when a wire has persistently failed.
//
// Semantics:
//   - Find the subSession whose Subscription includes currentURL.
//   - Append currentURL to that session's excluded set.
//   - Call sub.Pick(excluded) to get the next candidate.
//   - Return the chosen tunnel URL, or an error (exhausted / unknown URL).
//
// Concurrency: the engine serializes NextURL calls via a single
// outboundReplaceLoop, so this method never re-enters itself concurrently
// for the same engine. Multiple Engine instances sharing one Daemon
// (untested in practice — daemon owns at most one engine) would
// synchronize on excludeMu per session.
//
// Lock order: d.mu.RLock (short, snapshot d.subs slice) → release →
// then per-session excludeMu + Subscription.mu (via sub.Pick). Never holds
// opMu — that would deadlock with doDown which holds opMu and calls
// engine.Stop which waits for the replace loop that called us.
func (d *Daemon) NextURL(currentURL string, reason error) (string, error) {
	d.mu.RLock()
	sessions := d.subs
	d.mu.RUnlock()

	sess := findSessionByURL(sessions, currentURL)
	if sess == nil {
		return "", ErrNoSubscription
	}

	sess.excludeMu.Lock()
	// Avoid duplicate entries. O(N) over a tiny list.
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
		slog.Warn("daemon: subscription exhausted",
			"excluded", len(excludedSnapshot),
			"reason", reason,
			"err", err)
		return "", fmt.Errorf("all subscribed tunnels failed: %w", err)
	}

	slog.Info("daemon: OutboundProvider replacement selected",
		"excludedCount", len(excludedSnapshot),
		"newURL", config.TruncateVia(tunnel.URL),
		"reason", reason)
	return tunnel.URL, nil
}

// findSessionByURL returns the subSession whose subscription has a tunnel
// matching url. Returns nil if no match. O(sessions × tunnels) but both
// factors are tiny (typically 1 session × <100 tunnels).
func findSessionByURL(sessions []*subSession, url string) *subSession {
	for _, s := range sessions {
		for _, t := range s.sub.Tunnels() {
			if t.URL == url {
				return s
			}
		}
	}
	return nil
}
