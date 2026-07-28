package utlsquic

import (
	"bytes"
	"testing"

	"github.com/apernet/quic-go/quicvarint"
)

// blob assembles a transport-parameter block the way quic-go does.
func blob(t *testing.T, pairs ...any) []byte {
	t.Helper()
	var b []byte
	for i := 0; i < len(pairs); i += 2 {
		id := pairs[i].(uint64)
		v := pairs[i+1].([]byte)
		b = quicvarint.Append(b, id)
		b = quicvarint.Append(b, uint64(len(v)))
		b = append(b, v...)
	}
	return b
}

// TestTransportParametersSurviveTheRoundTrip is the guard on re-marshaling.
//
// quic-go marshals the parameters; this package takes them apart so uTLS can
// marshal them again. Those are the values that govern flow control, idle
// timeout and connection IDs, and a mangled block does not fail loudly -- it
// yields a connection running on limits neither side agreed to. So the bytes out
// must equal the bytes in.
//
// The cases cover the varint widths that appear in practice: a one-byte ID, a
// two-byte ID (grease_quic_bit at 0x2ab2), an empty value, and a value long
// enough to need a two-byte length.
func TestTransportParametersSurviveTheRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
	}{
		{"empty", nil},
		{"single", blob(t, uint64(0x01), []byte{0x50, 0x00})},
		{"zero-length value", blob(t, uint64(0x2ab2), []byte{})},
		{"multi-byte id", blob(t, uint64(0x2ab2), []byte{1}, uint64(0x0f), []byte{1, 2, 3, 4, 5, 6, 7, 8})},
		{"long value", blob(t, uint64(0x11), bytes.Repeat([]byte{0xab}, 200))},
		{"mixed", blob(t,
			uint64(0x01), []byte{0x50, 0x00},
			uint64(0x0f), []byte{1, 2, 3, 4},
			uint64(0x2ab2), []byte{},
			uint64(0x20), []byte{0x45, 0xc0},
		)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params, err := parseTransportParameters(tc.in)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := params.Marshal()
			if len(tc.in) == 0 && len(got) == 0 {
				return
			}
			if !bytes.Equal(got, tc.in) {
				t.Fatalf("re-marshal changed the block\n in: %x\nout: %x", tc.in, got)
			}
		})
	}
}

// TestTruncatedTransportParametersAreRejected pins the failure mode: a short
// block must be an error, not a silently dropped parameter.
func TestTruncatedTransportParametersAreRejected(t *testing.T) {
	full := blob(t, uint64(0x0f), []byte{1, 2, 3, 4, 5, 6, 7, 8})
	for _, cut := range []int{1, 2, len(full) - 1} {
		if _, err := parseTransportParameters(full[:cut]); err == nil {
			t.Fatalf("truncation to %d bytes was accepted", cut)
		}
	}
}
