// Package apernetcompat answers the deployment question that kuic's own test
// suite cannot: does a kuic client interoperate with the apernet/quic-go it was
// forked from, in both directions?
//
// This matters because the fork is rolled out client-first. Every deployed node
// runs an apernet/quic-go server built before kuic existed, and every deployed
// client keeps running apernet/quic-go until it updates. So the rollout crosses
// two mixed pairings, and each one has to work on its own:
//
//	new client (kuic, fingerprinted ClientHello)  ->  old server (apernet)
//	old client (apernet, plain crypto/tls)        ->  new server (kuic)
//
// The first is the one with teeth. kuic's client sends a ClientHello built from
// a ClientHelloSpec by metacubex/utls, and the peer terminating it is Go's
// stdlib crypto/tls, which never saw a utls type. If the shaped ClientHello,
// its transport parameters, or its ECH extension were subtly malformed, this is
// where it shows up — as a handshake that dies, not as a lint error.
//
// It lives in its own module because it needs both quic-go forks at once. They
// share a package name (quic) but not a module path, so they coexist under
// distinct import aliases; a single go.mod for kuic could not require the thing
// kuic replaced.
package apernetcompat

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	stdtls "crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	apernet "github.com/apernet/quic-go"
	kuic "github.com/kaitu-io/kuic"
	utls "github.com/metacubex/utls"
	"github.com/metacubex/utls/dicttls"
	"github.com/stretchr/testify/require"
)

const (
	outerSNI = "public.example" // ECH outer name — what an observer sees
	innerSNI = "localhost"      // ECH inner name — what the certificate is for
	alpn     = "kuic-compat"
)

// --- test PKI -------------------------------------------------------------

// selfSignedCert returns a certificate for innerSNI plus a pool trusting it.
// Both TLS engines get the same DER bytes, so a failure can never be blamed on
// the two sides disagreeing about the certificate.
func selfSignedCert(t *testing.T) (certDER []byte, key *ecdsa.PrivateKey, pool *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: innerSNI},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{innerSNI},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	certDER, err = x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(certDER)
	require.NoError(t, err)
	pool = x509.NewCertPool()
	pool.AddCert(cert)
	return certDER, key, pool
}

// echConfigList builds a draft-13 ECHConfigList and the matching private key.
//
// It returns raw bytes rather than either library's key type, because the two
// sides of these tests are different TLS engines: the same config has to be
// loadable as crypto/tls.EncryptedClientHelloKey and as
// utls.EncryptedClientHelloKey. Agreeing on bytes is the point.
func echConfigList(t *testing.T, publicName string) (config, privateKey, list []byte) {
	t.Helper()

	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)

	pub := priv.PublicKey().Bytes()
	name := []byte(publicName)
	suites := []byte{0x00, 0x01, 0x00, 0x01} // HKDF-SHA256 + AES-128-GCM

	inner := 1 + 2 + 2 + len(pub) + 2 + len(suites) + 1 + 1 + len(name) + 2
	cfg := make([]byte, 0, 4+inner)
	cfg = append(cfg, 0xfe, 0x0d) // version: draft-13
	cfg = append(cfg, byte(inner>>8), byte(inner))
	cfg = append(cfg, 42)         // config_id
	cfg = append(cfg, 0x00, 0x20) // kem_id: DHKEM(X25519, HKDF-SHA256)
	cfg = append(cfg, byte(len(pub)>>8), byte(len(pub)))
	cfg = append(cfg, pub...)
	cfg = append(cfg, byte(len(suites)>>8), byte(len(suites)))
	cfg = append(cfg, suites...)
	cfg = append(cfg, 64) // maximum_name_length
	cfg = append(cfg, byte(len(name)))
	cfg = append(cfg, name...)
	cfg = append(cfg, 0x00, 0x00) // no extensions

	list = make([]byte, 2+len(cfg))
	binary.BigEndian.PutUint16(list[:2], uint16(len(cfg)))
	copy(list[2:], cfg)

	return cfg, priv.Bytes(), list
}

// --- the fingerprint spec -------------------------------------------------

// compatSpec is the shape k2's wire/quic_fingerprint.go sends: TLS 1.3 suites
// with no GREASE cipher, a quic_transport_parameters extension, and a GREASE
// ECH placeholder for utls to swap for the real thing.
//
// It is deliberately NOT a copy of k2's full Chrome-146 spec. What is under
// test here is whether stdlib crypto/tls accepts a utls-built ClientHello at
// all — the structural requirements, not Chrome's exact extension order. A
// drifting copy of k2's spec would make this test fail for reasons that have
// nothing to do with cross-fork compatibility.
func compatSpec() (*utls.ClientHelloSpec, error) {
	return &utls.ClientHelloSpec{
		TLSVersMin: utls.VersionTLS13,
		TLSVersMax: utls.VersionTLS13,
		CipherSuites: []uint16{
			utls.TLS_AES_128_GCM_SHA256,
			utls.TLS_AES_256_GCM_SHA384,
			utls.TLS_CHACHA20_POLY1305_SHA256,
		},
		CompressionMethods: []uint8{0},
		Extensions: []utls.TLSExtension{
			// These values are what the peer actually reads. The spec is
			// authoritative for the wire; only the zero-length
			// initial_source_connection_id is filled in per connection.
			&utls.QUICTransportParametersExtension{
				TransportParameters: utls.TransportParameters{
					utls.InitialMaxStreamDataBidiLocal(6291456),
					utls.InitialMaxStreamDataBidiRemote(6291456),
					utls.InitialMaxStreamDataUni(6291456),
					utls.InitialMaxData(15728640),
					utls.InitialMaxStreamsBidi(100),
					utls.InitialMaxStreamsUni(103),
					utls.MaxIdleTimeout(30000),
					utls.MaxUDPPayloadSize(1472),
					utls.InitialSourceConnectionID([]byte{}),
					// Google-private and unknown-to-quic-go parameters ride
					// along untouched. Whether a stdlib crypto/tls peer
					// tolerates them is precisely what this matrix is for.
					&utls.FakeQUICTransportParameter{Id: 0x3128, Val: []byte{0x31, 0x30, 0x41, 0x46}},
				},
			},
			&utls.ALPNExtension{AlpnProtocols: []string{alpn}},
			&utls.KeyShareExtension{KeyShares: []utls.KeyShare{
				{Group: utls.X25519MLKEM768},
				{Group: utls.X25519},
			}},
			&utls.SupportedCurvesExtension{Curves: []utls.CurveID{
				utls.X25519MLKEM768,
				utls.X25519,
				utls.CurveP256,
			}},
			&utls.PSKKeyExchangeModesExtension{Modes: []uint8{utls.PskModeDHE}},
			&utls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []utls.SignatureScheme{
				utls.ECDSAWithP256AndSHA256,
				utls.PSSWithSHA256,
				utls.PKCS1WithSHA256,
				utls.ECDSAWithP384AndSHA384,
				utls.PSSWithSHA384,
			}},
			&utls.GREASEEncryptedClientHelloExtension{
				CandidateCipherSuites: []utls.HPKESymmetricCipherSuite{
					{KdfId: dicttls.HKDF_SHA256, AeadId: dicttls.AEAD_AES_128_GCM},
				},
				CandidatePayloadLens: []uint16{223},
			},
			&utls.SupportedVersionsExtension{Versions: []uint16{utls.VersionTLS13}},
			&utls.SNIExtension{},
		},
	}, nil
}

// twoDatagramSpec is compatSpec plus a ~1200-byte filler extension, so the
// ClientHello reliably spans two Initial datagrams and its SNI lands mid-stream.
// That is the shape that actually reproduced the ClientHello-scrambler bug: with a
// small, single-datagram ClientHello the scrambler's cut can fall harmlessly, so
// TestNewClientOldServer_ReframedInitial needs this to be a real regression guard
// rather than a lucky pass. The filler uses a GREASE extension ID (0x0a0a); the dial
// never depends on the server understanding it.
func twoDatagramSpec() (*utls.ClientHelloSpec, error) {
	spec, err := compatSpec()
	if err != nil {
		return nil, err
	}
	// Insert the filler before the trailing SNI so the SNI sits deep in the
	// ClientHello, where the scrambler cuts it across the datagram boundary.
	filler := &utls.GenericExtension{Id: 0x0a0a, Data: make([]byte, 1200)}
	ext := spec.Extensions
	spec.Extensions = append(ext[:len(ext)-1:len(ext)-1], filler, ext[len(ext)-1])
	return spec, nil
}

// --- servers --------------------------------------------------------------

// apernetServer starts an apernet/quic-go (crypto/tls) server. This stands in
// for a node running the binary that is already deployed.
func apernetServer(t *testing.T, certDER []byte, key *ecdsa.PrivateKey, echConfig, echKey []byte) net.Addr {
	t.Helper()

	tlsCfg := &stdtls.Config{
		Certificates: []stdtls.Certificate{{Certificate: [][]byte{certDER}, PrivateKey: key}},
		NextProtos:   []string{alpn},
	}
	// nil echConfig means "serve without ECH" — the no-ECH pairing needs a
	// server that has no ECH key at all, not one holding an empty key.
	if echConfig != nil {
		tlsCfg.EncryptedClientHelloKeys = []stdtls.EncryptedClientHelloKey{
			{Config: echConfig, PrivateKey: echKey},
		}
	}
	ln, err := apernet.ListenAddr("127.0.0.1:0", tlsCfg, &apernet.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go serveEchoApernet(ln)
	return ln.Addr()
}

// kuicServer starts a kuic (metacubex/utls) server — a node running the new
// binary, which the not-yet-updated clients in the field will also reach.
func kuicServer(t *testing.T, certDER []byte, key *ecdsa.PrivateKey, echConfig, echKey []byte) net.Addr {
	t.Helper()

	tlsCfg := &utls.Config{
		Certificates: []utls.Certificate{{Certificate: [][]byte{certDER}, PrivateKey: key}},
		NextProtos:   []string{alpn},
		EncryptedClientHelloKeys: []utls.EncryptedClientHelloKey{
			{Config: echConfig, PrivateKey: echKey},
		},
	}
	ln, err := kuic.ListenAddr("127.0.0.1:0", tlsCfg, &kuic.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go serveEchoKuic(ln)
	return ln.Addr()
}

func serveEchoApernet(ln *apernet.Listener) {
	for {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		go func() {
			for {
				str, err := conn.AcceptStream(context.Background())
				if err != nil {
					return
				}
				go func() { _, _ = io.Copy(str, str); str.Close() }()
			}
		}()
	}
}

func serveEchoKuic(ln *kuic.Listener) {
	for {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		go func() {
			for {
				str, err := conn.AcceptStream(context.Background())
				if err != nil {
					return
				}
				go func() { _, _ = io.Copy(str, str); str.Close() }()
			}
		}()
	}
}

// --- the matrix -----------------------------------------------------------

// TestNewClientOldServer_FingerprintedWithECH is the rollout-blocking case: a
// kuic client with a utls-shaped ClientHello and real ECH, against the
// apernet/quic-go server that is already running on every deployed node.
//
// A failure here means the fork cannot be shipped client-first — clients would
// have to wait for a fleet-wide server upgrade, or fall back to TCP-WS on every
// node until one landed.
func TestNewClientOldServer_FingerprintedWithECH(t *testing.T) {
	certDER, key, pool := selfSignedCert(t)
	echConfig, echKey, echList := echConfigList(t, outerSNI)
	addr := apernetServer(t, certDER, key, echConfig, echKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := kuic.DialAddr(ctx, addr.String(), &utls.Config{
		ServerName:                     innerSNI,
		NextProtos:                     []string{alpn},
		RootCAs:                        utlsPool(pool),
		EncryptedClientHelloConfigList: echList,
		MinVersion:                     utls.VersionTLS13,
	}, &kuic.Config{NewClientHelloSpec: compatSpec})
	require.NoError(t, err, "fingerprinted kuic client must handshake with an un-upgraded apernet server")
	defer conn.CloseWithError(0, "")

	state := conn.ConnectionState().TLS
	require.True(t, state.HandshakeComplete)
	require.True(t, state.ECHAccepted, "ECH must be accepted by the old server, not silently bypassed")
	require.Equal(t, alpn, state.NegotiatedProtocol)

	requireEchoWorksKuic(t, conn)
}

// TestNewClientOldServer_FingerprintedNoECH covers the pairing that actually
// exercises the transport-parameter injection in
// internal/handshake/u_clienthello.go.
//
// The ECH case above does not. Measured, not assumed: with the injection
// disabled, the ECH test still passes and only this one fails. The reason is
// that ECH gives utls a second route to the parameters — the server reads them
// from the decrypted inner ClientHello, which utls builds from the bytes
// UQUICConn.SetTransportParameters stashed internally, not from the spec's
// extension. Strip ECH and the spec's extension is the only source there is, so
// an empty one means no initial_source_connection_id and a conformant server
// answers TRANSPORT_PARAMETER_ERROR.
//
// That makes this the cross-fork guard for a hazard with no other alarm:
// utls caches the extension's marshaled bytes on first Len(), so if a future
// utls version measured the ClientHello before kuic injected, the injection
// would silently never reach the wire. Here that is a red test. Under ECH it
// would be an invisible loss of outer-ClientHello fidelity.
func TestNewClientOldServer_FingerprintedNoECH(t *testing.T) {
	certDER, key, pool := selfSignedCert(t)
	addr := apernetServer(t, certDER, key, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := kuic.DialAddr(ctx, addr.String(), &utls.Config{
		ServerName: innerSNI,
		NextProtos: []string{alpn},
		RootCAs:    utlsPool(pool),
		MinVersion: utls.VersionTLS13,
	}, &kuic.Config{NewClientHelloSpec: compatSpec})
	require.NoError(t, err, "the spec's transport parameters must carry this connection's real values")
	defer conn.CloseWithError(0, "")

	state := conn.ConnectionState().TLS
	require.True(t, state.HandshakeComplete)
	require.Equal(t, alpn, state.NegotiatedProtocol)

	requireEchoWorksKuic(t, conn)
}

// TestNewClientOldServer_ReframedInitial is the backward-compat gate for the
// Chrome-146 Initial framing (kuic's u_packet_packer.go): the client re-cuts its
// Initial CRYPTO into many frames interleaved with PING and PADDING, starts the
// packet numbers at 1 with {1,2} encoding lengths, and pins an 8-byte destination
// connection ID. All of that is a re-shaping of the QUIC packet layer the old
// apernet/quic-go server has never seen this client send — so it must still parse
// the re-cut CRYPTO frames, reassemble the ClientHello from them, and complete the
// handshake. A failure here means B2 cannot ship client-first.
//
// It handshakes many times, not once. The re-framer's CRYPTO fragmentation is
// randomized per connection, and the failure it guards against — kuic's ClientHello
// scrambler handing the re-framer non-contiguous stream ranges, which it lays out at
// wrong offsets so the peer can never reassemble the ClientHello — hit only some
// fraction of connections (≈5 in 7 when the bug was live). A single dial can draw a
// layout that happens to work; the loop makes a regression show up reliably.
func TestNewClientOldServer_ReframedInitial(t *testing.T) {
	certDER, key, pool := selfSignedCert(t)
	addr := apernetServer(t, certDER, key, nil, nil)

	spec := &kuic.InitialPacketSpec{
		DestConnIDLength:        8,
		InitPacketNumber:        1,
		InitPacketNumberLengths: []kuic.PacketNumberLen{kuic.PacketNumberLen1, kuic.PacketNumberLen2},
		FrameBuilder: &kuic.QUICRandomFrames{
			MinPING: 1, MaxPING: 4,
			MinCRYPTO: 6, MaxCRYPTO: 14,
			MinPADDING: 2, MaxPADDING: 6,
			Length: 1231 - 16,
		},
	}

	const iterations = 24
	for i := 0; i < iterations; i++ {
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			conn, err := kuic.DialAddr(ctx, addr.String(), &utls.Config{
				ServerName: innerSNI,
				NextProtos: []string{alpn},
				RootCAs:    utlsPool(pool),
				MinVersion: utls.VersionTLS13,
			}, &kuic.Config{
				NewClientHelloSpec: twoDatagramSpec,
				InitialPacketSpec:  spec,
			})
			require.NoErrorf(t, err, "iteration %d: a re-framed Chrome-146 Initial must handshake with an un-upgraded apernet server", i)
			defer conn.CloseWithError(0, "")

			state := conn.ConnectionState().TLS
			require.True(t, state.HandshakeComplete)
			require.Equal(t, alpn, state.NegotiatedProtocol)

			requireEchoWorksKuic(t, conn)
		}()
	}
}

// TestNewClientOldServer_Unfingerprinted covers the same pairing with no spec
// set. It separates two failure modes that would otherwise look identical: the
// fork being incompatible at all (this fails too) versus the fingerprinting
// path specifically being wrong (only the test above fails).
func TestNewClientOldServer_Unfingerprinted(t *testing.T) {
	certDER, key, pool := selfSignedCert(t)
	echConfig, echKey, echList := echConfigList(t, outerSNI)
	addr := apernetServer(t, certDER, key, echConfig, echKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := kuic.DialAddr(ctx, addr.String(), &utls.Config{
		ServerName:                     innerSNI,
		NextProtos:                     []string{alpn},
		RootCAs:                        utlsPool(pool),
		EncryptedClientHelloConfigList: echList,
		MinVersion:                     utls.VersionTLS13,
	}, &kuic.Config{})
	require.NoError(t, err)
	defer conn.CloseWithError(0, "")
	require.True(t, conn.ConnectionState().TLS.ECHAccepted)

	requireEchoWorksKuic(t, conn)
}

// TestOldClientNewServer is the other half of the rollout window: clients still
// running the shipped apernet/quic-go build, reaching a node that has already
// been upgraded to kuic. The engine swap is server-side too, so this is not
// automatically true.
func TestOldClientNewServer(t *testing.T) {
	certDER, key, pool := selfSignedCert(t)
	echConfig, echKey, echList := echConfigList(t, outerSNI)
	addr := kuicServer(t, certDER, key, echConfig, echKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := apernet.DialAddr(ctx, addr.String(), &stdtls.Config{
		ServerName:                     innerSNI,
		NextProtos:                     []string{alpn},
		RootCAs:                        pool,
		EncryptedClientHelloConfigList: echList,
		MinVersion:                     stdtls.VersionTLS13,
	}, &apernet.Config{})
	require.NoError(t, err, "an un-upgraded apernet client must still reach a kuic server")
	defer conn.CloseWithError(0, "")

	state := conn.ConnectionState().TLS
	require.True(t, state.HandshakeComplete)
	require.True(t, state.ECHAccepted)
	require.Equal(t, alpn, state.NegotiatedProtocol)

	str, err := conn.OpenStreamSync(ctx)
	require.NoError(t, err)
	requireEcho(t, str)
}

// requireEchoWorksKuic proves the connection carries data, not just a completed
// handshake. A fingerprint bug that corrupted transport parameters could leave
// the handshake looking fine and the data path broken.
func requireEchoWorksKuic(t *testing.T, conn *kuic.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	str, err := conn.OpenStreamSync(ctx)
	require.NoError(t, err)
	requireEcho(t, str)
}

// requireEcho writes a payload and requires it back. The stream type is
// io.ReadWriteCloser on both forks, so one helper serves both.
func requireEcho(t *testing.T, str io.ReadWriteCloser) {
	t.Helper()
	payload := []byte("kuic <-> apernet")
	_, err := str.Write(payload)
	require.NoError(t, err)
	require.NoError(t, str.Close())

	got, err := io.ReadAll(str)
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

// utlsPool converts an x509 pool for utls. Both engines take *x509.CertPool
// from the same standard library, so this is an identity — it exists to make
// that explicit at the call site rather than leaving a reader to wonder whether
// utls has its own pool type.
func utlsPool(p *x509.CertPool) *x509.CertPool { return p }
