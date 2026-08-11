package quic

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"net"
	"sync/atomic"
	"testing"
	"time"

	tls "github.com/metacubex/utls"
	"github.com/metacubex/utls/dicttls"
	"github.com/stretchr/testify/require"

	"github.com/kaitu-io/kuic/congestion"
	"github.com/kaitu-io/kuic/internal/handshake"
	"github.com/kaitu-io/kuic/internal/testdata"
)

// This file is the gate for the whole fork: a ClientHello shaped to a
// fingerprint spec, carrying a real (not GREASE) Encrypted Client Hello, must
// complete a QUIC handshake. That combination is the one reported broken in
// other uTLS+QUIC integrations.
//
// The spec's transport parameters are authoritative for the wire — see
// wire.PopulateFromClientHelloSpec. A handshake that dies right after the
// ClientHello with TRANSPORT_PARAMETER_ERROR points there, and specifically at
// initial_source_connection_id, the one parameter the spec cannot supply.

const gateOuterSNI = "public.example"  // ECH outer name — what a censor sees
const gateInnerSNI = "localhost"      // ECH inner name — what the test cert is for
const gateALPN = "kuic-fingerprint-gate"

// echKeypair builds an ECHConfigList and the matching server key.
func echKeypair(t *testing.T, publicName string) (tls.EncryptedClientHelloKey, []byte) {
	t.Helper()

	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)

	pub := priv.PublicKey().Bytes()
	name := []byte(publicName)
	// HpkeSymmetricCipherSuite: kdf_id(2) = HKDF-SHA256, aead_id(2) = AES-128-GCM.
	suites := []byte{0x00, 0x01, 0x00, 0x01}

	inner := 1 + 2 + 2 + len(pub) + 2 + len(suites) + 1 + 1 + len(name) + 2
	cfg := make([]byte, 0, 4+inner)
	cfg = append(cfg, 0xfe, 0x0d) // version draft-13
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

	list := make([]byte, 2+len(cfg))
	binary.BigEndian.PutUint16(list[:2], uint16(len(cfg)))
	copy(list[2:], cfg)

	return tls.EncryptedClientHelloKey{Config: cfg, PrivateKey: priv.Bytes()}, list
}

// gateClientHelloSpec returns a fingerprint spec good enough to prove the
// mechanism: a full TLS 1.3 ClientHello carrying quic_transport_parameters.
//
// It is NOT a byte-accurate Chrome-over-QUIC parrot. metacubex/utls ships only
// TCP ClientHello presets, and Chrome's HTTP/3 ClientHello differs from its TCP
// one in extension set and order. Authoring the real Chrome-H3 spec (from a
// capture, or from uQUIC's published spec data) is the consumer's job; what
// this file proves is that whatever spec is supplied actually reaches the wire
// and interoperates, ECH included.
func gateClientHelloSpec(t *testing.T) *tls.ClientHelloSpec {
	t.Helper()
	return &tls.ClientHelloSpec{
		CipherSuites: []uint16{
			tls.GREASE_PLACEHOLDER,
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
		CompressionMethods: []uint8{0},
		Extensions: []tls.TLSExtension{
			&tls.UtlsGREASEExtension{},
			&tls.SNIExtension{},
			&tls.SupportedCurvesExtension{Curves: []tls.CurveID{
				tls.GREASE_PLACEHOLDER,
				tls.X25519MLKEM768,
				tls.X25519,
				tls.CurveP256,
			}},
			&tls.SupportedPointsExtension{SupportedPoints: []byte{0}},
			&tls.ALPNExtension{AlpnProtocols: []string{gateALPN}},
			&tls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []tls.SignatureScheme{
				tls.ECDSAWithP256AndSHA256,
				tls.PSSWithSHA256,
				tls.PKCS1WithSHA256,
				tls.ECDSAWithP384AndSHA384,
				tls.PSSWithSHA384,
				tls.PKCS1WithSHA384,
				tls.PSSWithSHA512,
				tls.PKCS1WithSHA512,
			}},
			&tls.KeyShareExtension{KeyShares: []tls.KeyShare{
				{Group: tls.CurveID(tls.GREASE_PLACEHOLDER), Data: []byte{0}},
				{Group: tls.X25519MLKEM768},
				{Group: tls.X25519},
			}},
			&tls.PSKKeyExchangeModesExtension{Modes: []uint8{tls.PskModeDHE}},
			&tls.SupportedVersionsExtension{Versions: []uint16{
				tls.GREASE_PLACEHOLDER,
				tls.VersionTLS13,
			}},
			// The load-bearing one. These values go on the wire as written —
			// the spec is authoritative, quic-go's own config does not
			// override them. The only one filled in per connection is
			// initial_source_connection_id, whose zero-length placeholder
			// PopulateFromClientHelloSpec replaces with this connection's real
			// SCID (RFC 9000 §7.3 requires them to match).
			//
			// A spec with an empty extension is not usable: it would advertise
			// no flow control at all.
			&tls.QUICTransportParametersExtension{
				TransportParameters: tls.TransportParameters{
					tls.InitialMaxStreamDataBidiLocal(6291456),
					tls.InitialMaxStreamDataBidiRemote(6291456),
					tls.InitialMaxStreamDataUni(6291456),
					tls.InitialMaxData(15728640),
					tls.InitialMaxStreamsBidi(100),
					tls.InitialMaxStreamsUni(103),
					tls.MaxIdleTimeout(30000),
					tls.MaxUDPPayloadSize(1472),
					tls.InitialSourceConnectionID([]byte{}),
				},
			},
			// Placeholder that utls swaps for the real encrypted_client_hello
			// extension when Config.EncryptedClientHelloConfigList is set. A
			// spec without an EncryptedClientHelloExtension cannot carry ECH.
			&tls.GREASEEncryptedClientHelloExtension{
				CandidateCipherSuites: []tls.HPKESymmetricCipherSuite{
					{KdfId: dicttls.HKDF_SHA256, AeadId: dicttls.AEAD_AES_128_GCM},
					{KdfId: dicttls.HKDF_SHA256, AeadId: dicttls.AEAD_CHACHA20_POLY1305},
				},
				CandidatePayloadLens: []uint16{223},
			},
			&tls.UtlsGREASEExtension{},
		},
		TLSVersMin: tls.VersionTLS13,
		TLSVersMax: tls.VersionTLS13,
	}
}

// gateServer starts an ECH-terminating QUIC server and returns its address.
func gateServer(t *testing.T, echKey tls.EncryptedClientHelloKey) net.Addr {
	t.Helper()

	// The certificate must be valid for the ECH *inner* name: that is the name
	// the client actually authenticates against once ECH is accepted.
	cert := testdata.GetTLSConfig().Certificates[0]

	ln, err := ListenAddr("127.0.0.1:0", &tls.Config{
		Certificates:             []tls.Certificate{cert},
		NextProtos:               []string{gateALPN},
		EncryptedClientHelloKeys: []tls.EncryptedClientHelloKey{echKey},
	}, &Config{})
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			_ = conn
		}
	}()
	return ln.Addr()
}

// TestFingerprintSpecWithRealECH_Handshakes is the gate. A shaped ClientHello
// plus a real ECH config must produce a completed handshake with ECH accepted.
//
// If this fails with a CRYPTO_ERROR or TRANSPORT_PARAMETER_ERROR right after
// the ClientHello, suspect u_clienthello.go: the ClientHello is being built
// from the spec's transport parameters instead of the connection's.
func TestFingerprintSpecWithRealECH_Handshakes(t *testing.T) {
	echKey, echList := echKeypair(t, gateOuterSNI)
	addr := gateServer(t, echKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := DialAddr(ctx, addr.String(), &tls.Config{
		ServerName:                     gateInnerSNI,
		NextProtos:                     []string{gateALPN},
		RootCAs:                        testdata.GetRootCA(),
		EncryptedClientHelloConfigList: echList,
		MinVersion:                     tls.VersionTLS13,
	}, &Config{
		NewClientHelloSpec: func() (*tls.ClientHelloSpec, error) { return gateClientHelloSpec(t), nil },
	})
	require.NoError(t, err, "shaped ClientHello + real ECH must complete the handshake")
	defer conn.CloseWithError(0, "")

	state := conn.ConnectionState().TLS
	require.True(t, state.HandshakeComplete)
	require.True(t, state.ECHAccepted, "ECH must be accepted, not silently bypassed")
	require.Equal(t, gateALPN, state.NegotiatedProtocol)
}

// TestFingerprintSpecMissingTransportParams_FailsLoudly pins the contract that
// an unusable spec fails the dial instead of quietly falling back to the
// unshaped handshake — silently not having the fingerprint you asked for is
// worse than not connecting.
func TestFingerprintSpecMissingTransportParams_FailsLoudly(t *testing.T) {
	echKey, echList := echKeypair(t, gateOuterSNI)
	addr := gateServer(t, echKey)

	spec := gateClientHelloSpec(t)
	filtered := spec.Extensions[:0]
	for _, ext := range spec.Extensions {
		if _, ok := ext.(*tls.QUICTransportParametersExtension); !ok {
			filtered = append(filtered, ext)
		}
	}
	spec.Extensions = filtered

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := DialAddr(ctx, addr.String(), &tls.Config{
		ServerName:                     gateInnerSNI,
		NextProtos:                     []string{gateALPN},
		RootCAs:                        testdata.GetRootCA(),
		EncryptedClientHelloConfigList: echList,
		MinVersion:                     tls.VersionTLS13,
	}, &Config{
		NewClientHelloSpec: func() (*tls.ClientHelloSpec, error) { return spec, nil },
	})
	require.ErrorIs(t, err, handshake.ErrNoQUICTransportParametersExtension)
}

// TestFingerprintSpecWithoutECH_Handshakes isolates the fingerprint mechanism
// from ECH, so a gate failure can be attributed to one or the other.
//
// It is also the only test here that covers the transport-parameter injection
// in u_clienthello.go, and the coverage is not incidental — delete the
// injection and this is the sole test in the file that goes red. ECH gives the
// server a second route to the parameters (the decrypted inner ClientHello,
// built from the bytes utls stashed internally rather than from the spec's
// extension), so every ECH test above stays green with the injection gone.
//
// Keep a no-ECH case here even if k2 only ever ships ECH. Losing it means an
// all-green suite that has stopped testing the thing this fork exists for.
// MAINTAINING.md has the full reasoning under "The transport-parameters trap".
func TestFingerprintSpecWithoutECH_Handshakes(t *testing.T) {
	ln, err := ListenAddr("127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{testdata.GetTLSConfig().Certificates[0]},
		NextProtos:   []string{gateALPN},
	}, &Config{})
	require.NoError(t, err)
	defer ln.Close()
	go func() {
		for {
			if _, err := ln.Accept(context.Background()); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := DialAddr(ctx, ln.Addr().String(), &tls.Config{
		ServerName: gateInnerSNI,
		NextProtos: []string{gateALPN},
		RootCAs:    testdata.GetRootCA(),
		MinVersion: tls.VersionTLS13,
	}, &Config{
		NewClientHelloSpec: func() (*tls.ClientHelloSpec, error) { return gateClientHelloSpec(t), nil },
	})
	require.NoError(t, err)
	defer conn.CloseWithError(0, "")
	require.True(t, conn.ConnectionState().TLS.HandshakeComplete)
}

// countingCC is a pluggable congestion controller that records whether the
// connection actually drove it. Everything else delegates to permissive
// defaults so it never becomes the reason a handshake stalls.
type countingCC struct {
	sent atomic.Int64
}

func (c *countingCC) SetRTTStatsProvider(congestion.RTTStatsProvider) {}
func (c *countingCC) TimeUntilSend(congestion.ByteCount) congestion.Time {
	return congestion.Now()
}
func (c *countingCC) HasPacingBudget(congestion.Time) bool { return true }
func (c *countingCC) OnPacketSent(congestion.Time, congestion.ByteCount, congestion.PacketNumber, congestion.ByteCount, bool) {
	c.sent.Add(1)
}
func (c *countingCC) CanSend(congestion.ByteCount) bool { return true }
func (c *countingCC) MaybeExitSlowStart()               {}
func (c *countingCC) OnPacketAcked(congestion.PacketNumber, congestion.ByteCount, congestion.ByteCount, congestion.Time) {
}
func (c *countingCC) OnCongestionEvent(congestion.PacketNumber, congestion.ByteCount, congestion.ByteCount) {
}
func (c *countingCC) OnRetransmissionTimeout(bool)                 {}
func (c *countingCC) SetMaxDatagramSize(congestion.ByteCount)      {}
func (c *countingCC) InSlowStart() bool                            { return true }
func (c *countingCC) InRecovery() bool                             { return false }
func (c *countingCC) GetCongestionWindow() congestion.ByteCount    { return 1 << 20 }
func (c *countingCC) OnCongestionEventEx(congestion.ByteCount, congestion.Time, []congestion.AckedPacketInfo, []congestion.LostPacketInfo) {
}

var _ congestion.CongestionControlEx = (*countingCC)(nil)

// TestFingerprintSpec_PluggableCongestionControl proves the fork's two features
// compose: shaping the ClientHello does not cost the caller the pluggable
// congestion controller. Congestion control lives in the transport and the
// fingerprint lives in TLS, so they should be orthogonal — this test is what
// makes "should be" into "is", because k2 needs both at once and would
// otherwise discover the conflict in production.
func TestFingerprintSpec_PluggableCongestionControl(t *testing.T) {
	echKey, echList := echKeypair(t, gateOuterSNI)
	addr := gateServer(t, echKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := DialAddr(ctx, addr.String(), &tls.Config{
		ServerName:                     gateInnerSNI,
		NextProtos:                     []string{gateALPN},
		RootCAs:                        testdata.GetRootCA(),
		EncryptedClientHelloConfigList: echList,
		MinVersion:                     tls.VersionTLS13,
	}, &Config{
		NewClientHelloSpec: func() (*tls.ClientHelloSpec, error) { return gateClientHelloSpec(t), nil },
	})
	require.NoError(t, err)
	defer conn.CloseWithError(0, "")

	cc := &countingCC{}
	conn.SetCongestionControl(cc)

	// Any traffic will do; the point is that packets route through the
	// installed controller rather than the built-in one.
	str, err := conn.OpenUniStream()
	require.NoError(t, err)
	_, err = str.Write(make([]byte, 4096))
	require.NoError(t, err)
	require.NoError(t, str.Close())

	require.Eventually(t, func() bool { return cc.sent.Load() > 0 }, 3*time.Second, 10*time.Millisecond,
		"the installed congestion controller never saw a packet — SetCongestionControl is not taking effect on the fingerprinted dial path")
}

// TestFingerprintSpecUnusableSNI_FailsLoudly covers a trap that costs hours
// when it is not caught here. utls fills SNIExtension.ServerName from
// tls.Config.ServerName, and quic-go fills an empty ServerName in from the dial
// address — so dialing an IP without an explicit ServerName ships an SNI
// containing an IP literal, which RFC 6066 forbids. The peer drops the
// ClientHello without replying and the dial reads as a plain timeout.
//
// Verified by construction: the same dial with ServerName "localhost" succeeds
// (TestFingerprintSpecWithoutECH_Handshakes), and with "127.0.0.1" it hung for
// the full context before this guard existed.
func TestFingerprintSpecUnusableSNI_FailsLoudly(t *testing.T) {
	ln, err := ListenAddr("127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{testdata.GetTLSConfig().Certificates[0]},
		NextProtos:   []string{gateALPN},
	}, &Config{})
	require.NoError(t, err)
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = DialAddr(ctx, ln.Addr().String(), &tls.Config{
		InsecureSkipVerify: true, // no ServerName: quic-go fills in the dial IP
		NextProtos:         []string{gateALPN},
		MinVersion:         tls.VersionTLS13,
	}, &Config{
		NewClientHelloSpec: func() (*tls.ClientHelloSpec, error) { return gateClientHelloSpec(t), nil },
	})
	require.ErrorIs(t, err, handshake.ErrSNIWithoutHostname)
}

// TestFingerprintSpecECHAllowsIPInnerSNI is the counterweight to
// TestFingerprintSpecUnusableSNI_FailsLoudly: the unusable-SNI guard must not
// fire when ECH is in play. With ECH the spec's server_name is overwritten
// with the config's public name and ServerName only reaches the encrypted
// inner ClientHello, where an IP literal works. Tunnel URLs may carry a raw IP
// as host, so a guard that rejected this would break working deployments.
func TestFingerprintSpecECHAllowsIPInnerSNI(t *testing.T) {
	echKey, echList := echKeypair(t, gateOuterSNI)
	addr := gateServer(t, echKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := DialAddr(ctx, addr.String(), &tls.Config{
		ServerName:                     "127.0.0.1", // IP literal, inner SNI only
		InsecureSkipVerify:             true,
		NextProtos:                     []string{gateALPN},
		EncryptedClientHelloConfigList: echList,
		MinVersion:                     tls.VersionTLS13,
	}, &Config{
		NewClientHelloSpec: func() (*tls.ClientHelloSpec, error) { return gateClientHelloSpec(t), nil },
	})
	require.NoError(t, err)
	defer conn.CloseWithError(0, "")
	require.True(t, conn.ConnectionState().TLS.ECHAccepted)
}

// fixedLenConnIDGenerator hands out connection IDs of a fixed non-zero length,
// which the default client transport does not: quic-go dials with a zero-length
// source connection ID, and so does Chrome.
type fixedLenConnIDGenerator struct{ length int }

func (g fixedLenConnIDGenerator) GenerateConnectionID() (ConnectionID, error) {
	b := make([]byte, g.length)
	if _, err := rand.Read(b); err != nil {
		return ConnectionID{}, err
	}
	return ConnectionIDFromBytes(b), nil
}

func (g fixedLenConnIDGenerator) ConnectionIDLen() int { return g.length }

// TestFingerprintSpecBackfillsSourceConnectionID covers the one parameter a
// spec author cannot supply and the only one kuic still writes into the spec.
//
// RFC 9000 §7.3 requires initial_source_connection_id to equal the packet's
// Source Connection ID. A spec carries a zero-length placeholder, and
// PopulateFromClientHelloSpec replaces it with this connection's real value.
//
// It needs its own test because the default transport makes the bug invisible:
// there the source connection ID is itself zero-length, so a back-fill that
// never ran is indistinguishable from one that did. Forcing an 8-byte
// connection ID separates them — get this wrong and the server answers
// TRANSPORT_PARAMETER_ERROR.
func TestFingerprintSpecBackfillsSourceConnectionID(t *testing.T) {
	echKey, echList := echKeypair(t, gateOuterSNI)
	addr := gateServer(t, echKey)

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	require.NoError(t, err)
	tr := &Transport{Conn: udpConn, ConnectionIDGenerator: fixedLenConnIDGenerator{length: 8}}
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := tr.Dial(ctx, addr, &tls.Config{
		ServerName:                     gateInnerSNI,
		NextProtos:                     []string{gateALPN},
		RootCAs:                        testdata.GetRootCA(),
		EncryptedClientHelloConfigList: echList,
		MinVersion:                     tls.VersionTLS13,
	}, &Config{
		NewClientHelloSpec: func() (*tls.ClientHelloSpec, error) { return gateClientHelloSpec(t), nil },
	})
	require.NoError(t, err, "the spec's zero-length initial_source_connection_id must be replaced with the real 8-byte one")
	defer conn.CloseWithError(0, "")
	require.True(t, conn.ConnectionState().TLS.HandshakeComplete)
}
