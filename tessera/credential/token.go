package credential

import (
	"crypto/ecdh"
	"fmt"
)

// TokenLen is the size of the QUIC carrier: the client's ephemeral public key
// followed by the sealed credential.
//
// QUIC cannot reuse the TCP carrier. RFC 9001 §8.4 bans TLS middlebox
// compatibility mode, so every real QUIC client sends an empty
// legacy_session_id; putting 32 bytes there would be a field nothing else on
// the internet fills in. The Initial packet's Token field is cleartext,
// variable-length, and semantically "opaque bytes the server gave me earlier",
// which fits.
//
// Being variable-length buys something the TCP carrier cannot have: the client's
// public key travels with the sealed bytes instead of being fished out of the
// ClientHello's key_share. So the node decides without decrypting the Initial
// packet or parsing any TLS at all — a varint parse, one X25519, one AEAD open.
const TokenLen = PublicKeyLen + SealedLen

// SealToken builds the QUIC Initial token carrying a credential.
func SealToken(serverPub *ecdh.PublicKey, front string, c Credential) ([]byte, error) {
	clientPub, sealed, err := Seal(serverPub, front, c)
	if err != nil {
		return nil, err
	}
	token := make([]byte, 0, TokenLen)
	token = append(token, clientPub...)
	token = append(token, sealed...)
	return token, nil
}

// OpenToken runs the node's decision on a QUIC Initial token.
func (o *Opener) OpenToken(token []byte) (Credential, error) {
	if len(token) != TokenLen {
		return Credential{}, fmt.Errorf("%w: token is %d bytes, want %d", ErrNotOurs, len(token), TokenLen)
	}
	return o.Open(token[:PublicKeyLen], token[PublicKeyLen:])
}
