// Package quicwire reads just enough of a QUIC packet for the node to decide
// which path a flow takes — spec §6.3.
//
// "Just enough" is the point. The node never decrypts, never reassembles
// CRYPTO frames, never terminates TLS. The Token field it needs sits in the
// cleartext part of the Initial packet, so the whole decision costs a header
// walk and two varint reads. Everything this package cannot parse is simply
// not ours, which is the safe answer: it goes to the front.
package quicwire

import "encoding/binary"

// Version1 is the QUIC version defined by RFC 9000.
const Version1 = 0x00000001

// maxConnIDLen is the longest connection ID QUIC v1 permits (RFC 9000 §17.2).
// A v1 packet claiming more is malformed, and treating it as parseable would
// let a crafted datagram walk us past the fields we mean to read.
const maxConnIDLen = 20

// Initial holds the cleartext fields of an Initial packet that the node acts on.
// The slices alias the caller's datagram; copy them to outlive it.
type Initial struct {
	DestConnID []byte
	SrcConnID  []byte
	Token      []byte
}

// ParseInitial reports whether d is a QUIC v1 Initial packet, and if so returns
// its cleartext header fields.
//
// It is false for everything else a UDP socket receives: short header packets,
// Handshake and 0-RTT packets, version negotiation, other QUIC versions, and
// garbage. Callers must treat false as "not ours" rather than as an error.
func ParseInitial(d []byte) (Initial, bool) {
	// Long header, fixed bit set. RFC 9000 §17.2: the fixed bit is 1 in every
	// packet a v1 endpoint sends, so a cleared bit means this is not QUIC as we
	// know it (or is deliberately obfuscated traffic, which is equally not ours).
	if len(d) < 7 || d[0]&0x80 == 0 || d[0]&0x40 == 0 {
		return Initial{}, false
	}
	if binary.BigEndian.Uint32(d[1:5]) != Version1 {
		return Initial{}, false
	}
	// Long packet type is bits 5-4 of the first byte; Initial is 0b00.
	if d[0]&0x30 != 0x00 {
		return Initial{}, false
	}

	p := 5
	dcid, p, ok := readConnID(d, p)
	if !ok {
		return Initial{}, false
	}
	scid, p, ok := readConnID(d, p)
	if !ok {
		return Initial{}, false
	}

	tokenLen, n, ok := readVarint(d[p:])
	if !ok {
		return Initial{}, false
	}
	p += n
	if uint64(len(d)-p) < tokenLen {
		return Initial{}, false
	}
	return Initial{DestConnID: dcid, SrcConnID: scid, Token: d[p : p+int(tokenLen)]}, true
}

func readConnID(d []byte, p int) (id []byte, next int, ok bool) {
	if p >= len(d) {
		return nil, 0, false
	}
	l := int(d[p])
	if l > maxConnIDLen {
		return nil, 0, false
	}
	p++
	if p+l > len(d) {
		return nil, 0, false
	}
	return d[p : p+l], p + l, true
}

// readVarint decodes a QUIC variable-length integer (RFC 9000 §16).
func readVarint(b []byte) (val uint64, n int, ok bool) {
	if len(b) == 0 {
		return 0, 0, false
	}
	size := 1 << (b[0] >> 6)
	if len(b) < size {
		return 0, 0, false
	}
	val = uint64(b[0] & 0x3f)
	for i := 1; i < size; i++ {
		val = val<<8 | uint64(b[i])
	}
	return val, size, true
}
