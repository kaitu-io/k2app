// Package client assembles the Tessera side of a QUIC dial: a browser-shaped
// ClientHello (via uTLS) carrying a credential in the Initial packet's Token
// field.
//
// Both halves are needed and neither substitutes for the other. uTLS decides
// whether the handshake *looks* like a browser's; the token decides whether the
// node treats us as one of its own or hands us to the front. A dial with only
// the first is indistinguishable from a stranger and gets relayed; a dial with
// only the second gets in, wearing a fingerprint no browser produces.
package client

import (
	"crypto/ecdh"
	"errors"
	"time"

	"github.com/apernet/quic-go"
	utls "github.com/metacubex/utls"

	"github.com/kaitu-io/tessera/credential"
	"github.com/kaitu-io/tessera/utlsquic"
)

// InitialRTT is what a Tessera token reports as its RTT measurement.
//
// It is quic-go's own DefaultInitialRTT, which is what a connection with no
// token uses. The value is not cosmetic: quic-go feeds a token's RTT straight
// into the smoothed RTT estimate, so a zero would make this client retransmit
// its first flight far sooner than any other client on the network — a tell in
// the exact exchange a censor is watching.
const InitialRTT = 100 * time.Millisecond

// TokenStore hands quic-go a freshly minted Tessera credential for each
// connection attempt. It satisfies quic.TokenStore.
type TokenStore struct {
	// ServerPub is the node's long-term X25519 public key, from the k2t:// URL.
	ServerPub *ecdh.PublicKey
	// Front is the domain this node borrows. It is bound into the credential,
	// so it must match the node's own configuration exactly.
	Front string
	// ShortID identifies this client's group.
	ShortID [8]byte

	// now is swappable for tests.
	now func() time.Time
}

// Pop mints a credential. quic-go calls this once per connection, keyed by
// server name; the key is ignored because a credential is bound to the node's
// public key and front rather than to a name we might be dialing under.
//
// A nil return means "no token", which yields an ordinary QUIC dial. That is
// the honest failure: the node will relay us to the front and the connection
// will fail as a stranger's would, rather than half-succeeding in some state
// that stands out.
func (s *TokenStore) Pop(string) *quic.ClientToken {
	if s.ServerPub == nil || s.Front == "" {
		return nil
	}
	now := s.now
	if now == nil {
		now = time.Now
	}
	token, err := credential.SealToken(s.ServerPub, s.Front, credential.Credential{
		Version:   credential.Version1,
		Timestamp: now(),
		ShortID:   s.ShortID,
	})
	if err != nil {
		return nil
	}
	return quic.NewClientToken(token, InitialRTT)
}

// Put discards the server's NEW_TOKEN frames.
//
// Keeping one would be actively harmful: quic-go would offer it on the next
// dial in place of a Tessera credential, the node would fail to open it, and
// the connection would be relayed to the front. Resumption tokens are a
// performance feature we trade away for the carrier.
func (s *TokenStore) Put(string, *quic.ClientToken) {}

// Config returns the quic.Config for dialing a Tessera node: uTLS drives the
// handshake, and every attempt carries a fresh credential.
//
// serverPub, front and shortID come from the k2t:// URL. hello selects the
// browser fingerprint to wear.
func Config(serverPub *ecdh.PublicKey, front string, shortID [8]byte, hello utls.ClientHelloID) (*quic.Config, error) {
	if serverPub == nil {
		return nil, errors.New("tessera/client: nil server public key")
	}
	if front == "" {
		return nil, errors.New("tessera/client: empty front")
	}
	return &quic.Config{
		ClientTLSConnFactory: utlsquic.Factory(hello),
		TokenStore:           &TokenStore{ServerPub: serverPub, Front: front, ShortID: shortID},
	}, nil
}

var _ quic.TokenStore = (*TokenStore)(nil)
