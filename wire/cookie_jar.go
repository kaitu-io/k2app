package wire

import "github.com/sasha-s/go-deadlock"

// CookieJar is the client-side in-memory cookie store, keyed by the server's
// certificate pin fingerprint (Config.Pin) — NOT host:port, because one node
// can be reached over multiple IPs and ports (spec §5.2).
//
// Memory-only by design: a 15-minute credential is not worth a disk exposure
// surface; a process restart costs exactly one cold auth.
//
// One jar per OUTBOUND, created by the engine and shared by every dialer built
// for that outbound (all three RaceTransport candidates + the TCP-WS fallback
// client) — otherwise the race would trigger three cold auths. An empty pin
// (insecure/test configs) is safe as a key precisely because the jar is
// per-outbound.
//
// Concurrency: mu is a standalone state lock (map ops only). Registered in
// k2/CLAUDE.md Lock Ordering Graph.
//
// Methods are nil-receiver-safe so paths that have no jar (wire/probe.go,
// wire/quic_p2p.go, k2p) can pass nil without call-site branching.
type CookieJar struct {
	mu deadlock.Mutex
	m  map[string]string
}

// NewCookieJar creates an empty jar.
func NewCookieJar() *CookieJar {
	return &CookieJar{m: make(map[string]string)}
}

// Get returns the stored cookie for pin, or "".
func (j *CookieJar) Get(pin string) string {
	if j == nil {
		return ""
	}
	j.mu.Lock()
	c := j.m[pin]
	j.mu.Unlock()
	return c
}

// Put stores cookie under pin. Empty cookies are ignored — the server sends a
// cookie only right after a cold auth (spec §6.3), and an OK response without
// one must not erase a still-valid stored cookie.
func (j *CookieJar) Put(pin, cookie string) {
	if j == nil || cookie == "" {
		return
	}
	j.mu.Lock()
	j.m[pin] = cookie
	j.mu.Unlock()
}
