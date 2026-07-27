// Package spike verifies that the Tessera design (docs/tessera/spec.md) is
// implementable on the uTLS version this repo already depends on.
//
// spec.md §10 item 5 lists this as the one open question that can invalidate
// the whole design: can we, on the client, (a) overwrite legacy_session_id in
// a browser-fingerprinted ClientHello and (b) obtain the private key behind
// the X25519 key_share that same ClientHello carries?
//
// (b) is the load-bearing half. The server derives the shared secret from the
// key_share it reads off the wire, so the client MUST use that exact private
// key — not a fresh one — or the two sides derive different keys and every
// connection silently falls through to the relay path.
//
// These tests are evidence, not implementation. They live under docs/ on
// purpose.
package spike

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	utls "github.com/metacubex/utls"
	"golang.org/x/crypto/hkdf"
)

const (
	frontSNI     = "www.example-front.com"
	sessionIDLen = 32
	credLen      = 16 // AEAD plaintext; 16 + 16 byte tag == sessionIDLen exactly
)

// ---------------------------------------------------------------------------
// Shared key derivation — spec §4.4. Both sides run this.
// ---------------------------------------------------------------------------

func deriveKeyNonce(shared, salt []byte) (key, nonce []byte) {
	r := hkdf.New(sha256.New, shared, salt, []byte("k2t/v1 auth"))
	out := make([]byte, 44)
	if _, err := r.Read(out); err != nil {
		panic(err)
	}
	return out[:32], out[32:44]
}

func aeadFor(key []byte) cipher.AEAD {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return gcm
}

func buildCredential(version byte, ts time.Time, shortID [8]byte) []byte {
	cred := make([]byte, credLen)
	cred[0] = version
	cred[1] = 0 // flags
	binary.BigEndian.PutUint32(cred[2:6], uint32(ts.Unix()/60))
	copy(cred[6:14], shortID[:])
	return cred
}

// ---------------------------------------------------------------------------
// Server-side ClientHello parsing — deliberately independent of uTLS, because
// the real server parses raw bytes off the wire with no uTLS involved.
// ---------------------------------------------------------------------------

type parsedHello struct {
	sessionID   []byte
	x25519Share []byte // the plain X25519 key_share entry (group 0x001d)
	allShares   map[uint16][]byte
	serverName  string
}

func parseClientHello(raw []byte) (*parsedHello, error) {
	// raw includes the 4-byte handshake header (type + uint24 length).
	if len(raw) < 4 || raw[0] != 0x01 {
		return nil, errors.New("not a ClientHello handshake message")
	}
	b := raw[4:]
	p := 0
	need := func(n int) error {
		if p+n > len(b) {
			return fmt.Errorf("truncated at %d (need %d, have %d)", p, n, len(b)-p)
		}
		return nil
	}

	if err := need(2 + 32 + 1); err != nil { // legacy_version + random + sid_len
		return nil, err
	}
	p += 2 + 32
	sidLen := int(b[p])
	p++
	if err := need(sidLen); err != nil {
		return nil, err
	}
	out := &parsedHello{sessionID: b[p : p+sidLen], allShares: map[uint16][]byte{}}
	p += sidLen

	if err := need(2); err != nil {
		return nil, err
	}
	csLen := int(binary.BigEndian.Uint16(b[p:]))
	p += 2 + csLen
	if err := need(1); err != nil {
		return nil, err
	}
	compLen := int(b[p])
	p += 1 + compLen

	if err := need(2); err != nil {
		return nil, err
	}
	extTotal := int(binary.BigEndian.Uint16(b[p:]))
	p += 2
	end := p + extTotal
	if end > len(b) {
		return nil, errors.New("extensions truncated")
	}

	for p+4 <= end {
		extType := binary.BigEndian.Uint16(b[p:])
		extLen := int(binary.BigEndian.Uint16(b[p+2:]))
		p += 4
		if p+extLen > end {
			return nil, errors.New("extension body truncated")
		}
		body := b[p : p+extLen]
		p += extLen

		switch extType {
		case 0x0000: // server_name
			if len(body) >= 5 {
				nameLen := int(binary.BigEndian.Uint16(body[3:]))
				if 5+nameLen <= len(body) {
					out.serverName = string(body[5 : 5+nameLen])
				}
			}
		case 0x0033: // key_share
			if len(body) < 2 {
				continue
			}
			q := 2
			for q+4 <= len(body) {
				group := binary.BigEndian.Uint16(body[q:])
				klen := int(binary.BigEndian.Uint16(body[q+2:]))
				q += 4
				if q+klen > len(body) {
					break
				}
				out.allShares[group] = body[q : q+klen]
				q += klen
			}
		}
	}
	out.x25519Share = out.allShares[0x001d] // X25519
	return out, nil
}

// ---------------------------------------------------------------------------
// Client side — build a Chrome-fingerprinted ClientHello and rewrite session_id
// in place, exactly as a real implementation would.
// ---------------------------------------------------------------------------

type clientResult struct {
	raw          []byte
	sessionID    []byte
	ecdheCurve   utls.CurveID
	ecdhePub     []byte
	sharedSecret []byte
}

func buildTesseraHello(t *testing.T, helloID utls.ClientHelloID, serverPub *ecdh.PublicKey) *clientResult {
	t.Helper()

	c, s := net.Pipe()
	defer c.Close()
	defer s.Close()

	uc := utls.UClient(c, &utls.Config{
		ServerName:         frontSNI,
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}, helloID)

	if err := uc.BuildHandshakeState(); err != nil {
		t.Fatalf("BuildHandshakeState: %v", err)
	}

	hello := uc.HandshakeState.Hello
	if hello == nil {
		t.Fatal("HandshakeState.Hello is nil — cannot rewrite session_id")
	}
	if len(hello.SessionId) != sessionIDLen {
		t.Fatalf("session_id is %d bytes, need exactly %d for the AEAD to fit",
			len(hello.SessionId), sessionIDLen)
	}

	ks := uc.HandshakeState.State13.KeyShareKeys
	if ks == nil || ks.Ecdhe == nil {
		t.Fatal("State13.KeyShareKeys.Ecdhe is nil — no reachable key_share private key")
	}

	shared, err := ks.Ecdhe.ECDH(serverPub)
	if err != nil {
		t.Fatalf("ECDH with server public key: %v", err)
	}

	clientPub := ks.Ecdhe.PublicKey().Bytes()
	key, nonce := deriveKeyNonce(shared, clientPub)
	cred := buildCredential(0x01, time.Now(), [8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	sealed := aeadFor(key).Seal(nil, nonce, cred, []byte(frontSNI))
	if len(sealed) != sessionIDLen {
		t.Fatalf("sealed credential is %d bytes, session_id holds %d", len(sealed), sessionIDLen)
	}

	// Rewrite in place, both in the struct and in the already-marshaled bytes,
	// so no re-marshal (and therefore no fingerprint drift) is needed.
	copy(hello.SessionId, sealed)
	off := sessionIDOffset(t, hello.Raw)
	copy(hello.Raw[off:off+sessionIDLen], sealed)

	return &clientResult{
		raw:          append([]byte(nil), hello.Raw...),
		sessionID:    sealed,
		ecdheCurve:   ks.CurveID,
		ecdhePub:     clientPub,
		sharedSecret: shared,
	}
}

// sessionIDOffset locates legacy_session_id inside the marshaled ClientHello:
// handshake header (4) + legacy_version (2) + random (32) + length byte (1).
func sessionIDOffset(t *testing.T, raw []byte) int {
	t.Helper()
	const off = 4 + 2 + 32 + 1
	if len(raw) < off+sessionIDLen {
		t.Fatalf("ClientHello too short (%d bytes) to contain session_id", len(raw))
	}
	if raw[off-1] != sessionIDLen {
		t.Fatalf("session_id length byte is %d, expected %d", raw[off-1], sessionIDLen)
	}
	return off
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestTesseraCredentialRoundTrip is the core feasibility proof: the server,
// parsing only raw wire bytes, recovers the credential the client sealed.
func TestTesseraCredentialRoundTrip(t *testing.T) {
	fingerprints := []struct {
		name string
		id   utls.ClientHelloID
	}{
		{"Chrome_120_PQ", utls.HelloChrome_120_PQ},
		{"Chrome_Auto", utls.HelloChrome_Auto},
		{"Firefox_120", utls.HelloFirefox_120},
		{"Safari_16_0", utls.HelloSafari_16_0},
	}

	for _, fp := range fingerprints {
		t.Run(fp.name, func(t *testing.T) {
			serverPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}

			cr := buildTesseraHello(t, fp.id, serverPriv.PublicKey())
			t.Logf("key_share curve on the private key struct: 0x%04x", uint16(cr.ecdheCurve))

			// --- server side: raw bytes only, no uTLS ---
			ph, err := parseClientHello(cr.raw)
			if err != nil {
				t.Fatalf("server failed to parse ClientHello: %v", err)
			}
			if ph.serverName != frontSNI {
				t.Errorf("outer SNI = %q, want %q", ph.serverName, frontSNI)
			}

			groups := make([]string, 0, len(ph.allShares))
			for g := range ph.allShares {
				groups = append(groups, fmt.Sprintf("0x%04x(%dB)", g, len(ph.allShares[g])))
			}
			t.Logf("key_share groups on the wire: %v", groups)

			if ph.x25519Share == nil {
				t.Fatal("no plain X25519 (0x001d) key_share on the wire — " +
					"server cannot derive the shared secret")
			}
			if got, want := len(ph.x25519Share), 32; got != want {
				t.Fatalf("X25519 share is %d bytes, want %d", got, want)
			}

			// The wire key_share MUST match the private key we used.
			if string(ph.x25519Share) != string(cr.ecdhePub) {
				t.Fatalf("X25519 key_share on the wire does not match "+
					"State13.KeyShareKeys.Ecdhe public key\n  wire: %x\n  ecdhe: %x",
					ph.x25519Share, cr.ecdhePub)
			}

			clientPub, err := ecdh.X25519().NewPublicKey(ph.x25519Share)
			if err != nil {
				t.Fatalf("parse client key_share: %v", err)
			}
			shared, err := serverPriv.ECDH(clientPub)
			if err != nil {
				t.Fatalf("server ECDH: %v", err)
			}
			if string(shared) != string(cr.sharedSecret) {
				t.Fatal("server and client derived different shared secrets")
			}

			key, nonce := deriveKeyNonce(shared, ph.x25519Share)
			opened, err := aeadFor(key).Open(nil, nonce, ph.sessionID, []byte(frontSNI))
			if err != nil {
				t.Fatalf("server failed to open the credential: %v", err)
			}
			if opened[0] != 0x01 {
				t.Errorf("version = %d, want 1", opened[0])
			}
			ts := binary.BigEndian.Uint32(opened[2:6])
			if delta := int64(uint32(time.Now().Unix()/60)) - int64(ts); delta < 0 || delta > 1 {
				t.Errorf("timestamp drift %d minutes", delta)
			}
			t.Logf("credential recovered: version=%d short_id=%x", opened[0], opened[6:14])
		})
	}
}

// TestUnauthenticatedHelloIsRejected proves the discriminator actually
// discriminates: an ordinary browser ClientHello (random session_id) must fail
// to open, sending it down the relay path.
func TestUnauthenticatedHelloIsRejected(t *testing.T) {
	serverPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	c, s := net.Pipe()
	defer c.Close()
	defer s.Close()
	uc := utls.UClient(c, &utls.Config{
		ServerName:         frontSNI,
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}, utls.HelloChrome_120_PQ)
	if err := uc.BuildHandshakeState(); err != nil {
		t.Fatal(err)
	}

	ph, err := parseClientHello(uc.HandshakeState.Hello.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if ph.x25519Share == nil {
		t.Skip("no X25519 share; covered by the round-trip test")
	}

	clientPub, err := ecdh.X25519().NewPublicKey(ph.x25519Share)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := serverPriv.ECDH(clientPub)
	if err != nil {
		t.Fatal(err)
	}
	key, nonce := deriveKeyNonce(shared, ph.x25519Share)
	if _, err := aeadFor(key).Open(nil, nonce, ph.sessionID, []byte(frontSNI)); err == nil {
		t.Fatal("a vanilla browser ClientHello authenticated — discriminator is broken")
	}
}

// TestCrossFrontReplayIsRejected proves the AAD binding: a credential sealed
// for one front cannot be replayed against a node fronting a different site.
func TestCrossFrontReplayIsRejected(t *testing.T) {
	serverPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cr := buildTesseraHello(t, utls.HelloChrome_120_PQ, serverPriv.PublicKey())

	ph, err := parseClientHello(cr.raw)
	if err != nil {
		t.Fatal(err)
	}
	clientPub, _ := ecdh.X25519().NewPublicKey(ph.x25519Share)
	shared, _ := serverPriv.ECDH(clientPub)
	key, nonce := deriveKeyNonce(shared, ph.x25519Share)

	if _, err := aeadFor(key).Open(nil, nonce, ph.sessionID, []byte("www.other-front.com")); err == nil {
		t.Fatal("credential opened under a different front — AAD binding does not hold")
	}
}

// TestFingerprintIsUnchanged proves the rewrite costs no fingerprint drift:
// every byte outside legacy_session_id must be identical, so JA3/JA4 — which
// never cover session_id contents — see exactly the browser's ClientHello.
func TestFingerprintIsUnchanged(t *testing.T) {
	serverPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	c, s := net.Pipe()
	defer c.Close()
	defer s.Close()
	uc := utls.UClient(c, &utls.Config{
		ServerName:         frontSNI,
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}, utls.HelloChrome_120_PQ)
	if err := uc.BuildHandshakeState(); err != nil {
		t.Fatal(err)
	}

	hello := uc.HandshakeState.Hello
	before := append([]byte(nil), hello.Raw...)

	ks := uc.HandshakeState.State13.KeyShareKeys
	shared, err := ks.Ecdhe.ECDH(serverPriv.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	key, nonce := deriveKeyNonce(shared, ks.Ecdhe.PublicKey().Bytes())
	sealed := aeadFor(key).Seal(nil, nonce,
		buildCredential(0x01, time.Now(), [8]byte{7}), []byte(frontSNI))

	off := sessionIDOffset(t, hello.Raw)
	copy(hello.SessionId, sealed)
	copy(hello.Raw[off:off+sessionIDLen], sealed)

	if len(hello.Raw) != len(before) {
		t.Fatalf("ClientHello length changed: %d -> %d", len(before), len(hello.Raw))
	}
	for i := range before {
		inSessionID := i >= off && i < off+sessionIDLen
		if !inSessionID && before[i] != hello.Raw[i] {
			t.Fatalf("byte %d changed outside session_id (0x%02x -> 0x%02x)",
				i, before[i], hello.Raw[i])
		}
	}
	t.Logf("only session_id bytes [%d:%d] differ; %d-byte ClientHello otherwise identical",
		off, off+sessionIDLen, len(before))
}

// TestHandshakeStillCompletes proves the rewritten session_id does not break
// TLS itself: a real Go TLS 1.3 server must complete the handshake with a
// ClientHello whose legacy_session_id we replaced.
func TestHandshakeStillCompletes(t *testing.T) {
	cert := selfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		errCh <- conn.(*tls.Conn).Handshake()
	}()

	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	serverPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	uc := utls.UClient(raw, &utls.Config{
		ServerName:         frontSNI,
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}, utls.HelloChrome_120_PQ)
	if err := uc.BuildHandshakeState(); err != nil {
		t.Fatal(err)
	}

	hello := uc.HandshakeState.Hello
	ks := uc.HandshakeState.State13.KeyShareKeys
	shared, err := ks.Ecdhe.ECDH(serverPriv.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	key, nonce := deriveKeyNonce(shared, ks.Ecdhe.PublicKey().Bytes())
	sealed := aeadFor(key).Seal(nil, nonce,
		buildCredential(0x01, time.Now(), [8]byte{9}), []byte(frontSNI))

	copy(hello.SessionId, sealed)
	off := sessionIDOffset(t, hello.Raw)
	copy(hello.Raw[off:off+sessionIDLen], sealed)

	if err := uc.Handshake(); err != nil {
		t.Fatalf("TLS handshake failed with rewritten session_id: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("server side handshake failed: %v", err)
	}
	t.Logf("handshake completed, version=0x%04x", uc.ConnectionState().Version)
}

func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: frontSNI},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{frontSNI},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
