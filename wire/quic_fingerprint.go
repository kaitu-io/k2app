package wire

import (
	"crypto/rand"
	"fmt"
	"math/big"
	mrand "math/rand"

	tls "github.com/metacubex/utls"
)

// Chrome-over-QUIC ClientHello fingerprint.
//
// The QUIC leg used to hand quic-go a plain crypto/tls handshake while the
// TCP-WS leg wore a Chrome ClientHello — so the two legs of the same tunnel
// looked like two different clients, and the QUIC one looked like nothing on
// the public internet.
//
// The spec below is ported from refraction-networking/uquic's QUICChrome_146
// parrot (BSD-3-Clause), which was authored from a real Chrome 146 capture, and
// translated to metacubex/utls types. Chrome's HTTP/3 ClientHello is NOT its
// TCP one: TLS 1.3 suites only with no GREASE cipher, h3 ALPN, ALPS on the new
// 0x44cd codepoint, GREASE ECH, and a quic_transport_parameters extension that
// carries several Google-private parameters. utls's built-in Chrome presets are
// all TCP, so none of them is usable here.
//
// SCOPE — read before trusting this for evasion. This shapes the *ClientHello*.
// It does not reproduce Chrome's QUIC Initial *packet* structure: connection ID
// lengths, packet number encoding, CRYPTO frame fragmentation and datagram
// padding are still quic-go's. A fingerprinter working at the QUIC packet layer
// rather than the TLS layer can still separate this from Chrome. Closing that
// gap means porting uQUIC's packet-layer machinery into kuic.

// buildChromeH3Spec returns a fresh Chrome-146-over-QUIC ClientHelloSpec.
//
// Fresh per call, never cached: the GREASE parameter length, the google_initial_rtt
// value and both shuffles are drawn per connection, and kuic writes this
// connection's transport parameters into the returned extension. Sharing one
// spec across dials would both freeze the random draws and have concurrent
// dials overwrite each other's initial_source_connection_id.
func buildChromeH3Spec() (*tls.ClientHelloSpec, error) {
	greaseQTP, err := variableLengthGREASEQTP(0x10)
	if err != nil {
		return nil, fmt.Errorf("wire: GREASE transport parameter: %w", err)
	}
	initialRTT, err := chromeRandomInitialRTT()
	if err != nil {
		return nil, fmt.Errorf("wire: google_initial_rtt: %w", err)
	}

	// Chrome randomises the transport parameter order on every handshake, so a
	// fixed order would itself be the discriminator.
	tpExt := shuffleQUICTransportParameters(&tls.QUICTransportParametersExtension{
		TransportParameters: tls.TransportParameters{
			tls.InitialMaxStreamDataBidiRemote(6291456),
			tls.MaxDatagramFrameSize(65536),
			&tls.VersionInformation{
				ChoosenVersion:    tls.VERSION_1,
				AvailableVersions: []uint32{tls.VERSION_GREASE, tls.VERSION_1},
				LegacyID:          true,
			},
			tls.MaxUDPPayloadSize(1472),
			tls.MaxIdleTimeout(30000),
			initialRTT, // 0x3127 google_initial_rtt, new in Chrome 146
			tls.InitialMaxData(15728640),
			tls.InitialMaxStreamDataUni(6291456),
			tls.InitialSourceConnectionID([]byte{}),
			greaseQTP,
			&tls.FakeQUICTransportParameter{ // 0x3128 google_connection_options
				Id:  0x3128,
				Val: []byte{0x31, 0x30, 0x41, 0x46}, // "10AF"; was "RVCM" through Chrome 115
			},
			tls.InitialMaxStreamDataBidiLocal(6291456),
			tls.InitialMaxStreamsBidi(100),
			tls.InitialMaxStreamsUni(103),
		},
	})

	return &tls.ClientHelloSpec{
		TLSVersMin: tls.VersionTLS13,
		TLSVersMax: tls.VersionTLS13,
		CipherSuites: []uint16{
			// QUIC-only: TLS 1.3 suites, no GREASE cipher, no TLS 1.2 suites.
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
		CompressionMethods: []uint8{0},
		Extensions: tls.ShuffleChromeTLSExtensions([]tls.TLSExtension{
			tpExt,
			&tls.ALPNExtension{AlpnProtocols: []string{"h3"}},
			&tls.KeyShareExtension{KeyShares: []tls.KeyShare{
				{Group: tls.X25519MLKEM768}, // post-quantum hybrid; this is why Chrome's Initial spans two datagrams
				{Group: tls.X25519},
			}},
			&tls.SupportedCurvesExtension{Curves: []tls.CurveID{
				tls.X25519MLKEM768,
				tls.X25519,
				tls.CurveP256,
				tls.CurveP384,
			}},
			&tls.PSKKeyExchangeModesExtension{Modes: []uint8{tls.PskModeDHE}},
			&tls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []tls.SignatureScheme{
				tls.ECDSAWithP256AndSHA256,
				tls.PSSWithSHA256,
				tls.PKCS1WithSHA256,
				tls.ECDSAWithP384AndSHA384,
				tls.PSSWithSHA384,
				tls.PKCS1WithSHA384,
				tls.PSSWithSHA512,
				tls.PKCS1WithSHA512,
				tls.PKCS1WithSHA1,
			}},
			&tls.UtlsCompressCertExtension{Algorithms: []tls.CertCompressionAlgo{tls.CertCompressionBrotli}},
			// Chrome 133+ always offers ECH. This placeholder is also what utls
			// swaps for the real encrypted_client_hello extension when
			// Config.EncryptedClientHelloConfigList is set — without an
			// EncryptedClientHelloExtension in the spec, ECH is silently
			// dropped and the server answers missing_extension.
			tls.BoringGREASEECH(),
			&tls.ApplicationSettingsExtensionNew{SupportedProtocols: []string{"h3"}}, // 0x44cd, Chrome 133+
			&tls.SupportedVersionsExtension{Versions: []uint16{tls.VersionTLS13}},
			&tls.SNIExtension{},
		}),
	}, nil
}

// shuffleQUICTransportParameters permutes the parameter order in place.
// Ported from uQUIC. Uses math/rand deliberately: this is traffic shaping, not
// a security decision, and the order carries no secret.
func shuffleQUICTransportParameters(qtp *tls.QUICTransportParametersExtension) *tls.QUICTransportParametersExtension {
	mrand.Shuffle(len(qtp.TransportParameters), func(i, j int) {
		qtp.TransportParameters[i], qtp.TransportParameters[j] = qtp.TransportParameters[j], qtp.TransportParameters[i]
	})
	return qtp
}

// chromeRandomInitialRTT builds the google_initial_rtt (0x3127) parameter that
// Chrome 146 added, carrying a per-connection RTT estimate in microseconds.
// Ported from uQUIC.
func chromeRandomInitialRTT() (*tls.FakeQUICTransportParameter, error) {
	const minRTT, maxRTT = 1000, 20000
	r, err := rand.Int(rand.Reader, big.NewInt(maxRTT-minRTT))
	if err != nil {
		return nil, err
	}
	rtt := uint16(minRTT + r.Int64())
	// 2-byte QUIC varint: top two bits 01, remaining 14 bits carry the value.
	return &tls.FakeQUICTransportParameter{
		Id:  0x3127,
		Val: []byte{0x40 | byte(rtt>>8), byte(rtt)},
	}, nil
}

// variableLengthGREASEQTP builds a GREASE transport parameter of random length
// in [0, maxLen). Ported from uQUIC. A fixed-length GREASE parameter would be
// as much of a discriminator as no GREASE at all.
func variableLengthGREASEQTP(maxLen int64) (*tls.GREASETransportParameter, error) {
	if maxLen <= 1 {
		return &tls.GREASETransportParameter{}, nil
	}
	n, err := rand.Int(rand.Reader, big.NewInt(maxLen))
	if err != nil {
		return nil, err
	}
	return &tls.GREASETransportParameter{Length: uint16(n.Uint64())}, nil
}
