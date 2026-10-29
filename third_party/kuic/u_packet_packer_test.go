package quic

import (
	"context"
	"net"
	"testing"
	"time"

	tls "github.com/metacubex/utls"
	"github.com/stretchr/testify/require"

	"github.com/kaitu-io/kuic/internal/handshake"
	"github.com/kaitu-io/kuic/internal/protocol"
	"github.com/kaitu-io/kuic/internal/testdata"
)

// This is the wire-level guard for the Chrome-146 Initial framing: it dials, captures the
// real Initial datagrams the client puts on the wire, decrypts them with the Initial keys
// the way any on-path observer could (the keys derive from the destination connection ID
// and a public salt), and asserts on the *frames* — not just the long header the
// InitialPacketSpec tests already cover. A handshake-succeeds test proves none of this:
// quic-go's single big CRYPTO frame connects perfectly and looks nothing like Chrome.

// chromeLikeClientHello returns a ClientHelloSpec large enough to span two Initial
// datagrams — the X25519MLKEM768 key share alone is ~1200 bytes — which is the case the
// per-datagram re-framing exists for. It carries a QUICTransportParametersExtension
// because newClientConnection requires one.
func chromeLikeClientHello() func() (*tls.ClientHelloSpec, error) {
	return func() (*tls.ClientHelloSpec, error) {
		return &tls.ClientHelloSpec{
			TLSVersMin: tls.VersionTLS13,
			TLSVersMax: tls.VersionTLS13,
			CipherSuites: []uint16{
				tls.TLS_AES_128_GCM_SHA256,
				tls.TLS_AES_256_GCM_SHA384,
				tls.TLS_CHACHA20_POLY1305_SHA256,
			},
			CompressionMethods: []uint8{0},
			Extensions: []tls.TLSExtension{
				&tls.QUICTransportParametersExtension{TransportParameters: tls.TransportParameters{
					tls.InitialMaxData(15728640),
					tls.InitialMaxStreamDataBidiLocal(6291456),
					tls.InitialSourceConnectionID([]byte{}),
				}},
				// A large GREASE extension guarantees the ClientHello exceeds one Initial
				// datagram, so the two-datagram re-framing path is actually exercised —
				// the X25519MLKEM768 key share alone is not reliably enough here, and the
				// dial never completes anyway so the extension's contents are irrelevant.
				&tls.GenericExtension{Id: 0x0a0a, Data: make([]byte, 1200)},
				&tls.SNIExtension{},
				&tls.ALPNExtension{AlpnProtocols: []string{"h3"}},
				&tls.KeyShareExtension{KeyShares: []tls.KeyShare{
					{Group: tls.X25519MLKEM768},
					{Group: tls.X25519},
				}},
				&tls.SupportedCurvesExtension{Curves: []tls.CurveID{tls.X25519MLKEM768, tls.X25519}},
				&tls.SupportedVersionsExtension{Versions: []uint16{tls.VersionTLS13}},
				&tls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []tls.SignatureScheme{
					tls.ECDSAWithP256AndSHA256,
					tls.PSSWithSHA256,
					tls.PKCS1WithSHA256,
				}},
				&tls.PSKKeyExchangeModesExtension{Modes: []uint8{tls.PskModeDHE}},
			},
		}, nil
	}
}

// decryptedInitial is a client Initial after header parsing and AEAD decryption.
type decryptedInitial struct {
	hdr    initialHeader
	frames []parsedFrame
	size   int // UDP payload bytes of the whole datagram
}

// decodeInitial parses the long header of a client Initial, undoes header protection, and
// AEAD-opens the payload into its frames — everything a server does to read the packet.
func decodeInitial(t *testing.T, b []byte) decryptedInitial {
	t.Helper()
	require.Greater(t, len(b), 7)
	require.Equal(t, byte(0x80), b[0]&0x80, "not a long header packet")
	require.Equal(t, byte(0x00), b[0]&0x30, "not an Initial packet")

	var h initialHeader
	i := 5
	dcidLen := int(b[i])
	i++
	h.destConnID = b[i : i+dcidLen]
	i += dcidLen
	scidLen := int(b[i])
	i++
	h.srcConnID = b[i : i+scidLen]
	i += scidLen
	tokenLen, n := readVarint(t, b[i:])
	i += n + int(tokenLen)
	payloadLen, n := readVarint(t, b[i:])
	i += n
	pnOffset := i

	_, opener := handshake.NewInitialAEAD(
		protocol.ParseConnectionID(h.destConnID), protocol.PerspectiveServer, protocol.Version1)
	require.LessOrEqual(t, pnOffset+4+16, len(b), "packet too short for a header protection sample")
	sample := b[pnOffset+4 : pnOffset+4+16]
	firstByte := b[0]
	pnBytes := make([]byte, 4)
	copy(pnBytes, b[pnOffset:pnOffset+4])
	opener.DecryptHeader(sample, &firstByte, pnBytes)

	h.packetNumberLen = int(firstByte&0x03) + 1
	for _, c := range pnBytes[:h.packetNumberLen] {
		h.packetNumber = h.packetNumber<<8 | uint64(c)
	}

	// Associated data is the header in its unprotected form: recovered first byte, the
	// cleartext middle, and the recovered packet number. The ciphertext is the rest of
	// the QUIC packet as bounded by the Length field — never the whole datagram, whose
	// tail may be post-AEAD padding.
	aad := make([]byte, 0, pnOffset+h.packetNumberLen)
	aad = append(aad, firstByte)
	aad = append(aad, b[1:pnOffset]...)
	aad = append(aad, pnBytes[:h.packetNumberLen]...)

	ctStart := pnOffset + h.packetNumberLen
	ctEnd := pnOffset + int(payloadLen)
	require.LessOrEqual(t, ctEnd, len(b), "Length field runs past the datagram")
	plaintext, err := opener.Open(nil, b[ctStart:ctEnd], protocol.PacketNumber(h.packetNumber), aad)
	require.NoError(t, err, "Initial payload failed to decrypt")

	return decryptedInitial{hdr: h, frames: parseInitialFrames(t, plaintext), size: len(b)}
}

// captureInitials dials an address nothing answers and returns the first want datagrams
// the client emits. The dial cannot succeed; the datagrams are the point.
func captureInitials(t *testing.T, want int, conf *Config) [][]byte {
	t.Helper()
	sink, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer sink.Close()

	done := make(chan [][]byte, 1)
	go func() {
		var pkts [][]byte
		require.NoError(t, sink.SetReadDeadline(time.Now().Add(5*time.Second)))
		for len(pkts) < want {
			buf := make([]byte, 2000)
			n, _, err := sink.ReadFrom(buf)
			if err != nil {
				break
			}
			pkts = append(pkts, buf[:n])
		}
		done <- pkts
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = DialAddr(ctx, sink.LocalAddr().String(), &tls.Config{
		ServerName: "localhost",
		NextProtos: []string{"h3"},
		RootCAs:    testdata.GetRootCA(),
		MinVersion: tls.VersionTLS13,
	}, conf)

	pkts := <-done
	require.GreaterOrEqual(t, len(pkts), want, "client sent fewer Initial datagrams than expected")
	return pkts
}

func chrome146InitialSpec() *InitialPacketSpec {
	return &InitialPacketSpec{
		DestConnIDLength:        8,
		InitPacketNumber:        1,
		InitPacketNumberLengths: []PacketNumberLen{1, 2},
		FrameBuilder: &QUICRandomFrames{
			MinPING: 1, MaxPING: 4,
			MinCRYPTO: 6, MaxCRYPTO: 14,
			MinPADDING: 2, MaxPADDING: 6,
			Length: 1231 - 16,
		},
	}
}

// TestChrome146Initial_ReframedOnTheWire is the end-to-end wire assertion: the two
// Initial datagrams of the ClientHello flight carry Chrome's packet numbers and encoding
// lengths, are re-cut into many CRYPTO frames interleaved with PING and PADDING, sit near
// Chrome's 1231-byte size, and still deliver the complete ClientHello.
func TestChrome146Initial_ReframedOnTheWire(t *testing.T) {
	pkts := captureInitials(t, 2, &Config{
		NewClientHelloSpec: chromeLikeClientHello(),
		InitialPacketSpec:  chrome146InitialSpec(),
	})

	d0 := decodeInitial(t, pkts[0])
	d1 := decodeInitial(t, pkts[1])

	// Long header: Chrome's packet numbers and their encoding lengths.
	require.Equal(t, uint64(1), d0.hdr.packetNumber)
	require.Equal(t, 1, d0.hdr.packetNumberLen, "PN=1 must be 1-byte encoded")
	require.Equal(t, uint64(2), d1.hdr.packetNumber)
	require.Equal(t, 2, d1.hdr.packetNumberLen, "PN=2 must be 2-byte encoded")
	require.Len(t, d0.hdr.destConnID, 8)

	// Both datagrams sit within the QUIC minimum and the packet buffer, near Chrome's size.
	for i, d := range []decryptedInitial{d0, d1} {
		require.GreaterOrEqual(t, d.size, protocol.MinInitialPacketSize, "datagram %d below the QUIC minimum", i)
		require.LessOrEqual(t, d.size, protocol.MaxPacketBufferSize, "datagram %d over the packet buffer", i)
	}

	// The first datagram must be genuinely re-framed: quic-go would put one CRYPTO frame
	// here, so several CRYPTO frames plus PING and PADDING is the whole point.
	var nCrypto0, nPing0, nPad0 int
	for _, f := range d0.frames {
		switch f.typ {
		case 0x06:
			nCrypto0++
		case 0x01:
			nPing0++
		case 0x00:
			nPad0++
		}
	}
	require.Greater(t, nCrypto0, 1, "first Initial was not split into multiple CRYPTO frames")
	require.GreaterOrEqual(t, nPing0, 1, "first Initial carries no PING frame")
	require.GreaterOrEqual(t, nPad0, 1, "first Initial carries no PADDING frame")

	// The two datagrams together must carry the entire ClientHello, contiguously.
	type crange struct {
		off  uint64
		data []byte
	}
	var ranges []crange
	var total uint64
	for _, d := range []decryptedInitial{d0, d1} {
		for _, f := range d.frames {
			if f.typ != 0x06 {
				continue
			}
			ranges = append(ranges, crange{f.off, f.data})
			if end := f.off + uint64(len(f.data)); end > total {
				total = end
			}
		}
	}
	require.Greater(t, total, uint64(protocol.MinInitialPacketSize), "ClientHello should span more than one datagram")
	covered := make([]bool, total)
	for _, r := range ranges {
		for j := range r.data {
			require.False(t, covered[int(r.off)+j], "CRYPTO byte %d sent twice", int(r.off)+j)
			covered[int(r.off)+j] = true
		}
	}
	for j, ok := range covered {
		require.True(t, ok, "CRYPTO byte %d of %d never sent — ClientHello cannot reassemble", j, total)
	}
}
