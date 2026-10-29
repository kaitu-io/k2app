package subscription

import (
	"testing"

	"github.com/kaitu-io/k2/config"
)

// newTestSession constructs a Session whose underlying *config.Subscription
// is populated with the given URLs as Tunnels. No HTTP fetch, no cache —
// purely an in-memory scaffold for Manager unit tests.
func newTestSession(t *testing.T, urls []string) *Session {
	t.Helper()
	tunnels := make([]config.Tunnel, len(urls))
	for i, u := range urls {
		tunnels[i] = config.Tunnel{URL: u, Weight: 1}
	}
	sub := config.NewSubscriptionForTest(tunnels)
	return &Session{sub: sub}
}
