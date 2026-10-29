// Package subscription wraps k2subs:// subscription lifecycle for use by
// platform shells (daemon, gateway). It owns:
//   - HTTP transport for fetch + background refresh
//   - per-session exclude set used by OutboundProvider.NextURL
//   - the underlying *config.Subscription (parser + cache + refresh loop)
//
// Concurrency: Session.excludeMu protects only `excluded`. Never held
// across engine calls; never acquired while holding daemon/gateway.mu.
// sub.Pick has its own internal mutex (config.Subscription.mu).
package subscription

import (
	"net/http"

	"github.com/kaitu-io/k2/config"
	"github.com/sasha-s/go-deadlock"
)

// Session pairs a *config.Subscription with the per-engine exclude set
// and HTTP plumbing. One Session per k2subs:// route.
type Session struct {
	sub *config.Subscription

	httpTransport *http.Transport
	httpClient    *http.Client

	excludeMu deadlock.Mutex
	excluded  []string
}

// Close releases the subscription and its pooled HTTP idle connections.
// Safe to call repeatedly and on a nil receiver.
func (s *Session) Close() {
	if s == nil {
		return
	}
	if s.sub != nil {
		s.sub.Close()
	}
	if s.httpTransport != nil {
		s.httpTransport.CloseIdleConnections()
	}
}
