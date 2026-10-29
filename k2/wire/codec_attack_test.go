package wire

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"
)

// FuzzReadSmuxUDPFrame drives the smux UDP frame parser against random input.
// The parser MUST NOT panic, hang, or allocate more than maxFrameLen bytes for
// any input — including malformed length prefixes, truncated frames, oversized
// addr lengths, and quirks at varint boundaries.
//
// Corpus covers known-tricky shapes; the fuzzer then explores random mutations.
func FuzzReadSmuxUDPFrame(f *testing.F) {
	// Valid frames of varying sizes.
	var goodSmall bytes.Buffer
	_ = WriteSmuxUDPFrame(&goodSmall, 1, "1.2.3.4:80", []byte("hi"))
	f.Add(goodSmall.Bytes())

	var goodEmptyAddr bytes.Buffer
	_ = WriteSmuxUDPFrame(&goodEmptyAddr, 7, "", []byte("payload"))
	f.Add(goodEmptyAddr.Bytes())

	// Truncated frames at every varint-boundary position.
	full := goodSmall.Bytes()
	for i := 1; i < len(full); i++ {
		f.Add(full[:i])
	}

	// Frame length declared but no bytes follow (truncation attack).
	f.Add([]byte{0x40, 0xff}) // varint 255, but no payload
	f.Add([]byte{0xff})       // varint requiring 8 bytes, partial

	// Frame length at maxFrameLen boundary (should be accepted but allocation
	// large — test ensures no panic, allocation gracefully returns truncation
	// error on short read).
	var maxAtBoundary bytes.Buffer
	_ = AppendVarintTo(&maxAtBoundary, uint64(maxFrameLen)) // declare max
	// don't write payload — reader must hit EOF, not OOM
	f.Add(maxAtBoundary.Bytes())

	// Frame length just above max — must reject without allocation.
	var overBound bytes.Buffer
	_ = AppendVarintTo(&overBound, uint64(maxFrameLen)+1)
	f.Add(overBound.Bytes())

	// Empty input.
	f.Add([]byte{})

	// All-zero (varint 0 frame).
	f.Add([]byte{0x00})

	// Random adversarial bytes.
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		br := bufio.NewReader(bytes.NewReader(data))
		// Don't care about result, only about no-panic / no-hang.
		// If ReadSmuxUDPFrame allocates >maxFrameLen, this would OOM the
		// test runner — Go's fuzzer reports that as failure.
		_, _ = ReadSmuxUDPFrame(br)
	})
}

// FuzzReadUDPFrame attacks the inner UDP frame parser directly (not via smux
// length prefix). Same robustness invariants as above.
func FuzzReadUDPFrame(f *testing.F) {
	var good bytes.Buffer
	good.Write(binary.BigEndian.AppendUint32(nil, 42)) // session_id
	good.Write([]byte{0x05})                            // addr_len = 5
	good.WriteString("1:2:3")                           // addr
	good.WriteString("payload-bytes")                   // payload
	f.Add(good.Bytes())

	// Empty.
	f.Add([]byte{})

	// session_id only — truncated before addr_len.
	f.Add([]byte{0, 0, 0, 1})

	// addr_len varint claims more than maxAddrLen.
	var overAddr bytes.Buffer
	overAddr.Write(binary.BigEndian.AppendUint32(nil, 1))
	_ = AppendVarintTo(&overAddr, uint64(maxAddrLen)+1) // over-bound declaration
	overAddr.WriteString("never read") // even if read attempt would extend, must reject
	f.Add(overAddr.Bytes())

	// addr_len at max boundary, then truncated.
	var atMaxAddr bytes.Buffer
	atMaxAddr.Write(binary.BigEndian.AppendUint32(nil, 1))
	_ = AppendVarintTo(&atMaxAddr, uint64(maxAddrLen))
	f.Add(atMaxAddr.Bytes())

	// Pure noise.
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ReadUDPFrame(bytes.NewReader(data))
	})
}

// FuzzReadStreamHeader attacks the stream type + addr header parser. This is
// the first thing a server reads on every new stream — robust failure here is
// critical to prevent malformed-handshake amplification.
func FuzzReadStreamHeader(f *testing.F) {
	var good bytes.Buffer
	good.WriteByte(StreamTypeTCP)
	_ = AppendVarintTo(&good, 11)
	good.WriteString("example.com")
	f.Add(good.Bytes())

	// Each stream type as a 1-byte input (no addr_len follows).
	for _, st := range []byte{StreamTypeTCP, StreamTypeUDP, StreamTypeMeta, StreamTypeEcho, 0xff} {
		f.Add([]byte{st})
	}

	// Empty + addr_len just below/at max.
	var atMax bytes.Buffer
	atMax.WriteByte(StreamTypeTCP)
	_ = AppendVarintTo(&atMax, uint64(maxAddrLen))
	f.Add(atMax.Bytes())

	var overMax bytes.Buffer
	overMax.WriteByte(StreamTypeTCP)
	_ = AppendVarintTo(&overMax, uint64(maxAddrLen)+1)
	f.Add(overMax.Bytes())

	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		br := bufio.NewReader(bytes.NewReader(data))
		_, _, _ = ReadStreamHeader(br)
	})
}

// FuzzReadMetadataFrame attacks the connection metadata (auth) parser.
// A malformed auth handshake must reject cleanly without allocating bounded by
// the per-field caps inside the parser.
func FuzzReadMetadataFrame(f *testing.F) {
	var good bytes.Buffer
	_ = WriteMetadataFrame(&good, MetadataFrame{UDID: "abc", Token: "xyz", Mode: ""})
	f.Add(good.Bytes())

	var goodGateway bytes.Buffer
	_ = WriteMetadataFrame(&goodGateway, MetadataFrame{UDID: "u", Token: "t", Mode: "gateway"})
	f.Add(goodGateway.Bytes())

	// Field count above 16 — must reject.
	f.Add([]byte{0x11}) // varint 17

	// Field count at max (16) with no further data.
	f.Add([]byte{0x10})

	// keyLen claims oversize.
	var overKey bytes.Buffer
	_ = AppendVarintTo(&overKey, 1)       // 1 field
	_ = AppendVarintTo(&overKey, 1000)    // key length way over 256
	f.Add(overKey.Bytes())

	// valLen claims oversize.
	var overVal bytes.Buffer
	_ = AppendVarintTo(&overVal, 1)
	_ = AppendVarintTo(&overVal, 3)
	overVal.WriteString("key")
	_ = AppendVarintTo(&overVal, 100000) // val length over 4096
	f.Add(overVal.Bytes())

	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		br := bufio.NewReader(bytes.NewReader(data))
		_, _ = ReadMetadataFrame(br)
	})
}

// FuzzReadStreamResponse attacks the server→client response parser.
func FuzzReadStreamResponse(f *testing.F) {
	f.Add([]byte{0x00}) // success
	// Legacy error with message.
	var legacy bytes.Buffer
	legacy.WriteByte(0x01)
	_ = AppendVarintTo(&legacy, 11)
	legacy.WriteString("test error.")
	f.Add(legacy.Bytes())
	// Coded error.
	var coded bytes.Buffer
	coded.WriteByte(0x02)
	_ = AppendVarintTo(&coded, 401)
	_ = AppendVarintTo(&coded, 11)
	coded.WriteString("auth denied")
	f.Add(coded.Bytes())
	// Unknown status byte.
	f.Add([]byte{0xff})
	// Oversize legacy err_len.
	var overLegacy bytes.Buffer
	overLegacy.WriteByte(0x01)
	_ = AppendVarintTo(&overLegacy, uint64(maxErrorMsgLen)+1)
	f.Add(overLegacy.Bytes())
	// Oversize coded err_len.
	var overCoded bytes.Buffer
	overCoded.WriteByte(0x02)
	_ = AppendVarintTo(&overCoded, 1)
	_ = AppendVarintTo(&overCoded, uint64(maxErrorMsgLen)+1)
	f.Add(overCoded.Bytes())

	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		br := bufio.NewReader(bytes.NewReader(data))
		_, _, _, _ = ReadStreamResponse(br)
	})
}

// AppendVarintTo writes a varint to a bytes.Buffer-like writer. Helper for
// building fuzz corpus.
func AppendVarintTo(buf *bytes.Buffer, v uint64) error {
	tmp := AppendVarint(nil, v)
	_, err := buf.Write(tmp)
	return err
}

// TestAttackUDP_HugeFrameRejected_Boundary verifies that ReadSmuxUDPFrame
// rejects frameLen exactly one byte above maxFrameLen without attempting any
// allocation. Catches off-by-one in the bound check.
func TestAttackUDP_HugeFrameRejected_Boundary(t *testing.T) {
	// frame_len = maxFrameLen + 1, no payload.
	var buf bytes.Buffer
	_ = AppendVarintTo(&buf, uint64(maxFrameLen)+1)
	br := bufio.NewReader(&buf)
	_, err := ReadSmuxUDPFrame(br)
	if err == nil {
		t.Fatal("expected rejection of frameLen > maxFrameLen, got nil error")
	}
}

// TestAttackUDP_MaxFrameLen_Documents flags the current maxFrameLen as
// significantly larger than legitimate UDP traffic requires. This is a
// "documenting" test that PASSES today but pins the limit value so any
// future tightening (or accidental widening) is visible in diff review.
//
// Reasoning:
//   - UDP datagram payload max (RFC 768): 65507 bytes.
//   - Plus session_id (4) + addr_varint (≤2) + addr (≤maxAddrLen=4096) ≈ 70KB.
//   - Current cap: 16 MiB — ~230× larger than legitimate traffic.
//   - Tightening to 128 KiB would cover all legitimate cases with margin
//     and reduce per-frame DoS amplification by ~128×.
//
// This is intentionally a snapshot test; it does not advocate the change on
// its own. If the team decides to tighten, update the expected value and
// the comment above.
func TestAttackUDP_MaxFrameLen_Documents(t *testing.T) {
	const documentedSnapshot = 16 * 1024 * 1024
	if maxFrameLen != documentedSnapshot {
		t.Fatalf("maxFrameLen changed: got %d, snapshot %d — verify tightening is intentional",
			maxFrameLen, documentedSnapshot)
	}
}

// TestAttackStream_HeaderTruncation tries every truncation point of a valid
// stream header. Parser must return error (not panic, not hang).
func TestAttackStream_HeaderTruncation(t *testing.T) {
	var good bytes.Buffer
	good.WriteByte(StreamTypeTCP)
	_ = AppendVarintTo(&good, 11)
	good.WriteString("example.com")
	full := good.Bytes()

	for i := range full {
		t.Run("", func(t *testing.T) {
			br := bufio.NewReader(bytes.NewReader(full[:i]))
			_, _, err := ReadStreamHeader(br)
			if err == nil {
				t.Fatalf("expected error on truncation at offset %d, got nil", i)
			}
		})
	}
}
