package quic

import (
	"bytes"
	"testing"

	"github.com/kaitu-io/kuic/quicvarint"

	"github.com/stretchr/testify/require"
)

// parsedFrame is one frame decoded from a built Initial payload by the test's own
// independent parser. The reframing path exists to put bytes on the wire that a censor
// reads back frame by frame, so the guard has to read them back the same way rather than
// trust the builder's own bookkeeping — a builder that dropped a CRYPTO byte would still
// report a self-consistent frame list.
type parsedFrame struct {
	typ    byte
	off    uint64 // CRYPTO only
	data   []byte // CRYPTO only
	padLen int    // PADDING run length
}

// parseInitialFrames decodes a built Initial payload into PING (0x01), PADDING (0x00 run)
// and CRYPTO (0x06) frames. It rejects any other frame type: the reframer must emit only
// those three.
func parseInitialFrames(t *testing.T, b []byte) []parsedFrame {
	t.Helper()
	var frames []parsedFrame
	r := bytes.NewReader(b)
	for r.Len() > 0 {
		typ, err := r.ReadByte()
		require.NoError(t, err)
		switch typ {
		case 0x00: // PADDING — coalesce the run
			n := 1
			for r.Len() > 0 {
				next, _ := r.ReadByte()
				if next != 0x00 {
					r.UnreadByte()
					break
				}
				n++
			}
			frames = append(frames, parsedFrame{typ: 0x00, padLen: n})
		case 0x01: // PING
			frames = append(frames, parsedFrame{typ: 0x01})
		case 0x06: // CRYPTO
			off, err := quicvarint.Read(r)
			require.NoError(t, err)
			length, err := quicvarint.Read(r)
			require.NoError(t, err)
			data := make([]byte, length)
			_, err = r.Read(data)
			require.NoError(t, err)
			frames = append(frames, parsedFrame{typ: 0x06, off: off, data: data})
		default:
			t.Fatalf("unexpected frame type 0x%02x in Initial payload", typ)
		}
	}
	return frames
}

// reassemble stitches the CRYPTO frames back into the crypto stream, asserting they tile
// [baseOffset, baseOffset+len) with no gap and no overlap. This is the property that
// actually matters: a peer can only reconstruct the ClientHello if every byte is present
// exactly once at the right offset.
func reassemble(t *testing.T, frames []parsedFrame, baseOffset uint64, wantLen int) []byte {
	t.Helper()
	out := make([]byte, wantLen)
	covered := make([]bool, wantLen)
	for _, f := range frames {
		if f.typ != 0x06 {
			continue
		}
		local := int(f.off - baseOffset)
		require.GreaterOrEqual(t, local, 0, "CRYPTO offset %d below base %d", f.off, baseOffset)
		require.LessOrEqual(t, local+len(f.data), wantLen, "CRYPTO frame runs past the stream")
		for i, bb := range f.data {
			require.False(t, covered[local+i], "byte %d covered by two CRYPTO frames", local+i)
			covered[local+i] = true
			out[local+i] = bb
		}
	}
	for i, ok := range covered {
		require.True(t, ok, "byte %d covered by no CRYPTO frame", i)
	}
	return out
}

func TestQUICFramesBuild_SingleCryptoFrame(t *testing.T) {
	crypto := bytes.Repeat([]byte{0xAB}, 100)
	// Empty QUICFrames means "one CRYPTO frame carrying everything".
	payload, err := QUICFrames{}.Build(crypto)
	require.NoError(t, err)
	frames := parseInitialFrames(t, payload)
	require.Len(t, frames, 1)
	require.Equal(t, byte(0x06), frames[0].typ)
	require.Equal(t, crypto, reassemble(t, frames, 0, len(crypto)))
}

func TestQUICFramesBuildForDatagram_AppliesBaseOffset(t *testing.T) {
	crypto := bytes.Repeat([]byte{0xCD}, 40)
	const base = 1180
	payload, err := QUICFrames{
		QUICFrameCrypto{Offset: 0, Length: 10},
		QUICFrameCrypto{Offset: 10, Length: 0}, // remainder
	}.BuildForDatagram(1, crypto, base)
	require.NoError(t, err)
	frames := parseInitialFrames(t, payload)
	// The first CRYPTO frame's wire offset must be the base, not 0.
	require.Equal(t, uint64(base), frames[0].off)
	require.Equal(t, crypto, reassemble(t, frames, base, len(crypto)))
}

func TestQUICRandomFrames_ChromeParams_CoverAllBytesAndPadToLength(t *testing.T) {
	// Chrome 146's per-datagram framing.
	rf := &QUICRandomFrames{MinPING: 1, MaxPING: 4, MinCRYPTO: 6, MaxCRYPTO: 14, MinPADDING: 2, MaxPADDING: 6, Length: 1231 - 16}
	crypto := make([]byte, 1000)
	for i := range crypto {
		crypto[i] = byte(i)
	}
	// Run many times: the layout is randomized per call, so the invariants must hold on
	// every draw, not just one.
	for iter := 0; iter < 200; iter++ {
		const base = 1180
		payload, err := rf.BuildForDatagram(0, crypto, base)
		require.NoError(t, err)
		frames := parseInitialFrames(t, payload)

		var nCrypto, nPing, nPad int
		for _, f := range frames {
			switch f.typ {
			case 0x06:
				nCrypto++
			case 0x01:
				nPing++
			case 0x00:
				nPad++
			}
		}
		require.GreaterOrEqual(t, nCrypto, 6, "CRYPTO frame count below MinCRYPTO")
		require.Less(t, nCrypto, 14, "CRYPTO frame count at or above MaxCRYPTO")
		require.GreaterOrEqual(t, nPing, 1)
		require.Less(t, nPing, 4)
		// Padded to exactly the target length.
		require.Equal(t, int(rf.Length), len(payload), "payload not padded to Length")
		// And the ClientHello still reassembles byte-for-byte.
		require.Equal(t, crypto, reassemble(t, frames, base, len(crypto)))
	}
}

func TestQUICRandomFrames_RejectsBadBounds(t *testing.T) {
	rf := &QUICRandomFrames{MinCRYPTO: 0}
	_, err := rf.Build(make([]byte, 10))
	require.Error(t, err)
}
