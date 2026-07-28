// Package credential implements the Tessera discriminator credential — spec
// §4.4 (client construction) and §4.5 (node decision).
//
// The credential answers one question for the node: is this connection from one
// of our clients, or from anyone else? The answer must not be observable. Both
// outcomes consume identical bytes off the wire; the difference exists only
// inside an X25519 computation with the node's private key.
//
// The crypto is carrier-agnostic on purpose. TLS-over-TCP hides the sealed
// bytes in legacy_session_id and carries the client's ephemeral public key in
// the ClientHello's key_share (which a browser sends anyway); QUIC hides both in
// the Initial packet's Token field. Same derivation, same plaintext, different
// envelope — so Sealed/Open take the two parts separately and each carrier
// packs them its own way (see SealToken/ParseToken for the QUIC one).
package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	// Version1 is the only protocol version defined.
	Version1 = 0x01

	// PlaintextLen is the size of the credential before sealing (spec §4.4).
	PlaintextLen = 16
	// tagLen is the AES-GCM authentication tag.
	tagLen = 16
	// SealedLen is PlaintextLen + tagLen. On TCP this is exactly the 32 bytes
	// of a TLS 1.3 legacy_session_id, which is why that field fits with nothing
	// left over.
	SealedLen = PlaintextLen + tagLen

	// PublicKeyLen is the size of an X25519 public key.
	PublicKeyLen = 32

	// hkdfInfo binds the derivation to this protocol and version.
	hkdfInfo = "k2t/v1 auth"
)

// ErrNotOurs reports that a credential did not come from one of our clients.
//
// Every failure mode collapses into this one error on purpose: the node's
// reaction must be identical whichever check failed, because a reaction that
// varies is a signal an observer can drive. Callers get detail by unwrapping
// for logs, but must not branch on it. The only correct handling is: relay the
// connection to the front, exactly as for a wrong-length token or plain garbage.
var ErrNotOurs = errors.New("credential: not ours")

// Credential is the 16-byte plaintext sealed into the carrier.
type Credential struct {
	Version byte
	Flags   byte
	// Timestamp is truncated to minute precision by the wire format.
	Timestamp time.Time
	// ShortID identifies a client group, for multi-tenancy and revocation.
	ShortID [8]byte
}

func (c Credential) marshal() []byte {
	b := make([]byte, PlaintextLen)
	b[0] = c.Version
	b[1] = c.Flags
	binary.BigEndian.PutUint32(b[2:6], uint32(c.Timestamp.Unix()/60))
	copy(b[6:14], c.ShortID[:])
	// b[14:16] reserved, left zero.
	return b
}

func unmarshalCredential(b []byte) Credential {
	var c Credential
	c.Version = b[0]
	c.Flags = b[1]
	c.Timestamp = time.Unix(int64(binary.BigEndian.Uint32(b[2:6]))*60, 0)
	copy(c.ShortID[:], b[6:14])
	return c
}

// derive produces the AEAD key and nonce shared by both sides (spec §4.4).
//
// The nonce is deterministic, which is safe only because the key never repeats:
// it comes from an ECDH against a per-connection ephemeral client key, so a
// (key, nonce) pair is used exactly once. Reusing a client key across
// connections would break that and must never be done.
func derive(shared, clientPub []byte) (key, nonce []byte, err error) {
	out, err := hkdf.Key(sha256.New, shared, clientPub, hkdfInfo, 44)
	if err != nil {
		return nil, nil, err
	}
	return out[:32], out[32:44], nil
}

func aeadFor(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal builds a credential for one connection. It returns the client's
// ephemeral public key alongside the sealed bytes; how they travel is the
// carrier's business.
//
// front is bound as additional data, so a credential captured on the wire
// cannot be replayed against a node that borrows a different site's shell.
func Seal(serverPub *ecdh.PublicKey, front string, c Credential) (clientPub, sealed []byte, err error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	shared, err := priv.ECDH(serverPub)
	if err != nil {
		return nil, nil, err
	}
	clientPub = priv.PublicKey().Bytes()
	key, nonce, err := derive(shared, clientPub)
	if err != nil {
		return nil, nil, err
	}
	aead, err := aeadFor(key)
	if err != nil {
		return nil, nil, err
	}
	return clientPub, aead.Seal(nil, nonce, c.marshal(), []byte(front)), nil
}

// Opener is the node side of the decision. It is safe for concurrent use.
type Opener struct {
	priv     *ecdh.PrivateKey
	front    string
	window   time.Duration
	shortIDs map[[8]byte]struct{}
	replay   *replayCache

	// now is swappable for tests. Production leaves it nil, meaning time.Now.
	now func() time.Time
}

// OpenerConfig configures a node's discriminator.
type OpenerConfig struct {
	// PrivateKey is the node's long-term X25519 key. It never leaves the node.
	PrivateKey *ecdh.PrivateKey
	// Front is the domain whose shell this node borrows. It is bound into every
	// credential, so it must match what clients were given.
	Front string
	// ReplayWindow is how far a credential's timestamp may sit from now, in
	// either direction. Zero means the 10 minutes of spec §4.5.
	ReplayWindow time.Duration
	// ShortIDs is the set of client groups this node serves. Empty means the
	// node accepts none — a node with no configured groups authenticates
	// nobody, rather than everybody.
	ShortIDs [][8]byte
	// MaxReplayEntries bounds the replay cache. Zero picks a default.
	MaxReplayEntries int

	now func() time.Time
}

// DefaultReplayWindow is the ± tolerance on a credential's timestamp.
const DefaultReplayWindow = 10 * time.Minute

// NewOpener builds a node-side discriminator.
func NewOpener(cfg OpenerConfig) (*Opener, error) {
	if cfg.PrivateKey == nil {
		return nil, errors.New("credential: nil private key")
	}
	if cfg.Front == "" {
		// An empty front would still "work" cryptographically, and would
		// silently unbind every credential from the site being impersonated.
		return nil, errors.New("credential: empty front")
	}
	window := cfg.ReplayWindow
	if window == 0 {
		window = DefaultReplayWindow
	}
	if window < 0 {
		return nil, fmt.Errorf("credential: negative replay window %v", window)
	}
	ids := make(map[[8]byte]struct{}, len(cfg.ShortIDs))
	for _, id := range cfg.ShortIDs {
		ids[id] = struct{}{}
	}
	now := cfg.now
	if now == nil {
		now = time.Now
	}
	return &Opener{
		priv:     cfg.PrivateKey,
		front:    cfg.Front,
		window:   window,
		shortIDs: ids,
		replay:   newReplayCache(cfg.MaxReplayEntries, now),
		now:      now,
	}, nil
}

// Open runs the node's decision on one credential (spec §4.5 steps 3-6).
//
// A nil error means authenticated path; ErrNotOurs means borrowed-shell path.
// There is no third outcome, and the caller must not treat the unwrapped detail
// as anything but a log line.
//
// Note the ordering: the AEAD open comes before any lookup against node state.
// Only a party holding a valid credential ever reaches the short_id and replay
// maps, so their non-constant-time behaviour cannot be probed by an outsider.
func (o *Opener) Open(clientPub, sealed []byte) (Credential, error) {
	if len(clientPub) != PublicKeyLen || len(sealed) != SealedLen {
		return Credential{}, fmt.Errorf("%w: bad lengths (pub %d, sealed %d)", ErrNotOurs, len(clientPub), len(sealed))
	}
	cpub, err := ecdh.X25519().NewPublicKey(clientPub)
	if err != nil {
		return Credential{}, fmt.Errorf("%w: bad client public key: %v", ErrNotOurs, err)
	}
	shared, err := o.priv.ECDH(cpub)
	if err != nil {
		return Credential{}, fmt.Errorf("%w: ecdh: %v", ErrNotOurs, err)
	}
	key, nonce, err := derive(shared, clientPub)
	if err != nil {
		return Credential{}, fmt.Errorf("%w: derive: %v", ErrNotOurs, err)
	}
	aead, err := aeadFor(key)
	if err != nil {
		return Credential{}, fmt.Errorf("%w: aead: %v", ErrNotOurs, err)
	}
	plain, err := aead.Open(nil, nonce, sealed, []byte(o.front))
	if err != nil {
		return Credential{}, fmt.Errorf("%w: aead open", ErrNotOurs)
	}

	c := unmarshalCredential(plain)
	if c.Version != Version1 {
		return Credential{}, fmt.Errorf("%w: unknown version 0x%02x", ErrNotOurs, c.Version)
	}
	if skew := o.now().Sub(c.Timestamp); skew > o.window || skew < -o.window {
		return Credential{}, fmt.Errorf("%w: timestamp skew %v exceeds ±%v", ErrNotOurs, skew, o.window)
	}
	if _, ok := o.shortIDs[c.ShortID]; !ok {
		return Credential{}, fmt.Errorf("%w: short_id %x not served here", ErrNotOurs, c.ShortID)
	}
	// Last, because it has a side effect: a credential that fails a later check
	// must not consume its own replay slot.
	if !o.replay.admit(sealed, c.Timestamp.Add(o.window)) {
		return Credential{}, fmt.Errorf("%w: replayed", ErrNotOurs)
	}
	return c, nil
}
