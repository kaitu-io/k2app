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

// These tests read the first Initial packet off the wire and assert on its long
// header. Nothing else can: every field here is invisible to a
// handshake-succeeds assertion, because getting them wrong produces a perfectly
// working connection that simply does not look like the client it claims to be.
//
// The long header is unencrypted up to and including the token, so a plain UDP
// listener can parse it without keys:
//
//	byte 0    1|1|TT|RR|PP    (TT=00 Initial, PP = packet number length - 1)
//	bytes 1-4 version
//	byte 5    DCID length,  then DCID
//	byte  ..  SCID length,  then SCID
//	          token length (varint), token
//	          payload length (varint), packet number (PP+1 bytes), payload
//
// The packet number and the PP bits are header-protected (RFC 9001 §5.4), so
// they cannot be read directly — an earlier version of this file tried, and got
// a plausible-looking wrong answer that even the control case failed. Initial
// header protection keys derive from the destination connection ID with a fixed
// salt, which is public, so the test derives them the same way the peer would.

// initialHeader is the parsed prefix of a client Initial packet.
type initialHeader struct {
	packetNumberLen int
	packetNumber    uint64
	destConnID      []byte
	srcConnID       []byte
}

// parseClientInitial parses the long header of a client Initial packet.
func parseClientInitial(t *testing.T, b []byte) initialHeader {
	t.Helper()
	require.Greater(t, len(b), 7, "packet too short to be an Initial")
	require.Equal(t, byte(0x80), b[0]&0x80, "not a long header packet")
	require.Equal(t, byte(0x00), b[0]&0x30, "not an Initial packet")

	var h initialHeader
	i := 5 // skip first byte + 4 version bytes

	dcidLen := int(b[i])
	i++
	require.LessOrEqual(t, i+dcidLen, len(b))
	h.destConnID = b[i : i+dcidLen]
	i += dcidLen

	scidLen := int(b[i])
	i++
	require.LessOrEqual(t, i+scidLen, len(b))
	h.srcConnID = b[i : i+scidLen]
	i += scidLen

	tokenLen, n := readVarint(t, b[i:])
	i += n + int(tokenLen)
	_, n = readVarint(t, b[i:]) // payload length
	i += n

	// Undo header protection over the first byte and the packet number.
	_, opener := handshake.NewInitialAEAD(
		protocol.ParseConnectionID(h.destConnID), protocol.PerspectiveServer, protocol.Version1)
	require.LessOrEqual(t, i+4+16, len(b), "packet too short to contain a header protection sample")
	sample := b[i+4 : i+4+16]
	firstByte := b[0]
	pnBytes := make([]byte, 4)
	copy(pnBytes, b[i:i+4])
	opener.DecryptHeader(sample, &firstByte, pnBytes)

	h.packetNumberLen = int(firstByte&0x03) + 1
	for _, c := range pnBytes[:h.packetNumberLen] {
		h.packetNumber = h.packetNumber<<8 | uint64(c)
	}
	return h
}

// readVarint decodes a QUIC variable-length integer, returning it and how many
// bytes it occupied.
func readVarint(t *testing.T, b []byte) (uint64, int) {
	t.Helper()
	require.NotEmpty(t, b)
	n := 1 << (b[0] >> 6)
	require.LessOrEqual(t, n, len(b))
	v := uint64(b[0] & 0x3f)
	for _, c := range b[1:n] {
		v = v<<8 | uint64(c)
	}
	return v, n
}

// captureFirstInitial dials a UDP address that nothing answers and returns the
// first packet the client sends. The dial is expected to fail; the packet is
// the point.
func captureFirstInitial(t *testing.T, conf *Config) initialHeader {
	t.Helper()

	sink, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer sink.Close()

	type result struct {
		b   []byte
		err error
	}
	got := make(chan result, 1)
	go func() {
		buf := make([]byte, 2000)
		require.NoError(t, sink.SetReadDeadline(time.Now().Add(5*time.Second)))
		n, _, err := sink.ReadFrom(buf)
		got <- result{b: buf[:n], err: err}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// The dial cannot succeed — nothing on the other end speaks QUIC.
	_, _ = DialAddr(ctx, sink.LocalAddr().String(), &tls.Config{
		ServerName: "localhost",
		NextProtos: []string{"h3"},
		RootCAs:    testdata.GetRootCA(),
		MinVersion: tls.VersionTLS13,
	}, conf)

	r := <-got
	require.NoError(t, r.err, "no Initial packet was sent")
	return parseClientInitial(t, r.b)
}

// TestInitialPacketSpec_DestConnIDLength pins the Initial's destination
// connection ID length.
//
// Without a spec quic-go picks a length uniformly at random in [8, 20] on every
// dial. That is not merely different from Chrome's constant 8 — a client whose
// connection ID length varies run to run is distinguishable from every real
// implementation, whatever length it happens to draw.
func TestInitialPacketSpec_DestConnIDLength(t *testing.T) {
	hdr := captureFirstInitial(t, &Config{
		InitialPacketSpec: &InitialPacketSpec{DestConnIDLength: 8},
	})
	require.Len(t, hdr.destConnID, 8)
}

// TestInitialPacketSpec_InitPacketNumber pins the first Initial's packet
// number. quic-go starts at 0, Chrome at 1. It is header-protected, but the
// protection key comes from the destination connection ID and a public salt, so
// any on-path observer can read it as cheaply as this test does.
func TestInitialPacketSpec_InitPacketNumber(t *testing.T) {
	hdr := captureFirstInitial(t, &Config{
		InitialPacketSpec: &InitialPacketSpec{InitPacketNumber: 1},
	})
	require.Equal(t, uint64(1), hdr.packetNumber)
}

// TestInitialPacketSpec_DefaultsUnchanged is the control. Without a spec the
// dial must behave exactly as upstream quic-go does: packet number 0, and a
// destination connection ID at least 8 bytes long (quic-go's random choice).
//
// It exists so the tests above cannot pass by accident — if quic-go already did
// what they assert, they would prove nothing about the spec.
func TestInitialPacketSpec_DefaultsUnchanged(t *testing.T) {
	hdr := captureFirstInitial(t, &Config{})
	require.Equal(t, uint64(0), hdr.packetNumber, "upstream starts Initial packet numbering at 0")
	require.GreaterOrEqual(t, len(hdr.destConnID), 8)
}
