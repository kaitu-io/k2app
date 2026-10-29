package quic

// Ported from refraction-networking/uquic (BSD-3-Clause), heavily trimmed.
//
// uQUIC's uPacketPacker re-implements the whole of PackCoalescedPacket and adds a flight
// planner, a token synthesizer and PTO re-framing. kuic needs one thing from it: the
// Chrome-146 Initial where the ClientHello CRYPTO stream is cut into many small CRYPTO
// frames interleaved with PING and PADDING. That layout is per-datagram
// (QUICRandomFrames), not a tail-first flight, so none of the flight machinery is here.
//
// SCOPE — what this re-frames and what it deliberately does not:
//
//	re-framed   the fresh ClientHello flight: each Initial datagram carrying new CRYPTO
//	            popped from the initial stream (the packets a censor's first-packet
//	            classifier actually inspects).
//	base packer Initial ACK-only packets, Initial CRYPTO *retransmissions* (which come
//	            from the retransmission queue, not the initial stream) and PTO probes,
//	            and every Handshake/1-RTT packet. A retransmitted or probed Initial
//	            therefore goes out as quic-go's single CRYPTO frame — distinguishable
//	            from Chrome, but only under packet loss, where Chrome's own framing is
//	            already timing-perturbed. Re-framing those would risk desyncing the
//	            Initial ack handler (which tracks the contiguous range it popped) against
//	            a re-cut wire layout; the ClientHello, which is what matters, is covered.

import (
	"fmt"
	"sort"

	"github.com/kaitu-io/kuic/internal/handshake"
	"github.com/kaitu-io/kuic/internal/monotime"
	"github.com/kaitu-io/kuic/internal/protocol"
	"github.com/kaitu-io/kuic/internal/wire"
)

// uPacketPacker wraps the base packetPacker to re-frame the client's Initial CRYPTO into
// a real client's frame layout. Every method other than PackCoalescedPacket is inherited
// unchanged from the embedded packer.
type uPacketPacker struct {
	*packetPacker

	frameBuilder QUICFrameBuilder

	// initialDatagramIdx counts the Initial datagrams re-framed so far, so a
	// QUICFrameBuilderEx can vary its layout per datagram and place CRYPTO at the right
	// absolute stream offset for datagrams past the first.
	initialDatagramIdx int
}

func newUPacketPacker(base *packetPacker, frameBuilder QUICFrameBuilder) *uPacketPacker {
	return &uPacketPacker{packetPacker: base, frameBuilder: frameBuilder}
}

// PackCoalescedPacket re-frames the fresh ClientHello flight and delegates everything
// else to the base packer. See the SCOPE note above.
func (p *uPacketPacker) PackCoalescedPacket(onlyAck bool, maxSize protocol.ByteCount, now monotime.Time, v protocol.Version) (*coalescedPacket, error) {
	// The fresh ClientHello flight is exactly: not ACK-only, a builder installed, and new
	// CRYPTO waiting in the initial stream. That flight has no handshake keys yet, so its
	// Initial packets never coalesce with a later encryption level — which is what lets
	// this shim own the whole datagram and hand everything else back to the base packer
	// untouched. (Retransmissions come from the retransmission queue, so HasData is false
	// for them and they too fall through.)
	if !onlyAck && p.frameBuilder != nil && p.initialStream.HasData() {
		initialSealer, err := p.cryptoSetup.GetInitialSealer()
		if err != nil && err != handshake.ErrKeysDropped {
			return nil, err
		}
		if initialSealer != nil {
			return p.packReframedInitial(initialSealer, maxSize, now, v)
		}
	}
	return p.packetPacker.PackCoalescedPacket(onlyAck, maxSize, now, v)
}

// packReframedInitial packs one Initial datagram of the ClientHello flight, re-cutting the
// CRYPTO the base packer would have sent as a single frame into the builder's layout.
func (p *uPacketPacker) packReframedInitial(sealer sealer, maxSize protocol.ByteCount, now monotime.Time, v protocol.Version) (*coalescedPacket, error) {
	initialMaxSize := maxSize - protocol.ByteCount(sealer.Overhead())

	// Cap the CRYPTO popped for this datagram so that after re-framing — many CRYPTO
	// frames (each with its own type/offset/length overhead) plus PING, padded up to
	// rf.Length — the payload still fits its target. Without the cap, maybeGetCryptoPacket
	// fills the packet with as much CRYPTO as the raw budget allows, and the added frame
	// overhead then pushes the re-framed payload over. hdrLen matches how
	// appendReframedInitial sizes the header, so the arithmetic lines up. Only a
	// padding-requesting QUICRandomFrames needs this; other builders keep quic-go's sizing.
	hdrLen := p.getLongHeader(protocol.EncryptionInitial, v).GetLength(v)
	if rf, ok := p.frameBuilder.(*QUICRandomFrames); ok && rf.Length > 0 && rf.MinPADDING >= 1 {
		// Reserve room for everything re-framing adds beyond the raw CRYPTO bytes, so the
		// payload still has space to reach rf.Length with real PADDING frames: up to
		// MaxCRYPTO CRYPTO-frame headers (~5 bytes each — type + offset varint + length
		// varint, for a sub-16 KB ClientHello), up to MaxPING one-byte PING frames, and a
		// little slack. Upstream uQUIC uses a flat 16 bytes, which is smaller than
		// Chrome's MaxCRYPTO=14 worth of frame headers alone and leaves the first datagram
		// with no PADDING — a tell, since Chrome's first Initial carries several.
		const cryptoFrameHeader = 5
		const paddingSlack = 8
		reserve := protocol.ByteCount(rf.MaxCRYPTO)*cryptoFrameHeader + protocol.ByteCount(rf.MaxPING) + paddingSlack
		if budget := hdrLen + protocol.ByteCount(rf.Length) - reserve; budget > 0 && budget < initialMaxSize {
			initialMaxSize = budget
		}
	}

	// ackAllowed=false: the ClientHello flight has received nothing to acknowledge, and a
	// mixed-in ACK frame would not survive the CRYPTO-only re-framing below.
	hdr, pl := p.maybeGetCryptoPacket(initialMaxSize, protocol.EncryptionInitial, now, false, false, false, v)
	if pl.length == 0 {
		return nil, nil
	}
	cryptoData, baseOffset, ok := extractInitialCryptoData(pl)
	if !ok {
		// HasData() gated us here, so the pop must have produced CRYPTO. If it somehow did
		// not, the crypto has already left the stream — fail loudly rather than ship an
		// empty Initial and lose the ClientHello bytes.
		return nil, fmt.Errorf("kuic: re-framed Initial popped no CRYPTO frames to lay out")
	}

	idx := p.initialDatagramIdx
	var uPayload []byte
	var err error
	if ext, exOK := p.frameBuilder.(QUICFrameBuilderEx); exOK {
		uPayload, err = ext.BuildForDatagram(idx, cryptoData, baseOffset)
	} else {
		uPayload, err = p.frameBuilder.Build(cryptoData)
	}
	if err != nil {
		return nil, fmt.Errorf("kuic: build Initial frames: %w", err)
	}
	p.initialDatagramIdx++

	buffer := getPacketBuffer()
	pkt, err := p.appendReframedInitial(buffer, hdr, pl, uPayload, sealer, v)
	if err != nil {
		buffer.Release()
		return nil, err
	}
	return &coalescedPacket{buffer: buffer, longHdrPackets: []*longHeaderPacket{pkt}}, nil
}

// extractInitialCryptoData concatenates the CRYPTO frames of an Initial payload, in
// ascending stream order, into the contiguous slice the base packer popped, and returns
// the absolute stream offset of its first byte. The frames are already contiguous — the
// packer pops the initial stream in order — so this reconstructs exactly the range the
// builder must re-cut and re-emit, byte for byte, at the returned base offset.
func extractInitialCryptoData(pl payload) (data []byte, baseOffset uint64, ok bool) {
	var frames []*wire.CryptoFrame
	for _, f := range pl.frames {
		if cf, isCrypto := f.Frame.(*wire.CryptoFrame); isCrypto {
			frames = append(frames, cf)
		}
	}
	if len(frames) == 0 {
		return nil, 0, false
	}
	sort.Slice(frames, func(i, j int) bool { return frames[i].Offset < frames[j].Offset })
	baseOffset = uint64(frames[0].Offset)
	for _, cf := range frames {
		data = append(data, cf.Data...)
	}
	return data, baseOffset, true
}

// appendReframedInitial serializes an Initial packet whose frame payload the builder has
// already produced: header, the pre-built frame bytes, AEAD, and a post-AEAD floor to the
// QUIC minimum datagram size. It mirrors the base appendLongHeaderPacket, minus the
// framer/padding path (the builder owns the payload) and minus the base packer's fill to
// the full datagram — Chrome's Initials sit near 1231 bytes, not at the path MTU, and
// rf.Length already sizes them, so the floor only ever fires for a smaller builder.
func (p *uPacketPacker) appendReframedInitial(buffer *packetBuffer, header *wire.ExtendedHeader, pl payload, uPayload []byte, sealer sealer, v protocol.Version) (*longHeaderPacket, error) {
	pnLen := protocol.ByteCount(header.PacketNumberLen)
	header.Length = pnLen + protocol.ByteCount(sealer.Overhead()) + protocol.ByteCount(len(uPayload))

	startLen := len(buffer.Data)
	// The builder's payload size is bounded by the spec, not by quic-go's packet sizing,
	// so guard the packet buffer explicitly: append() past its capacity would move the
	// slice off buffer.Data's backing array and the reslice below would then panic.
	packetLen := int(header.GetLength(v)) + len(uPayload) + sealer.Overhead()
	if avail := cap(buffer.Data) - startLen; packetLen > avail {
		return nil, fmt.Errorf("kuic: re-framed Initial does not fit the packet buffer: %d-byte packet, %d over the %d bytes available", packetLen, packetLen-avail, avail)
	}
	raw := buffer.Data[startLen:]
	raw, err := header.Append(raw, v)
	if err != nil {
		return nil, err
	}
	payloadOffset := protocol.ByteCount(len(raw))
	raw = append(raw, uPayload...)
	raw = p.encryptPacket(raw, sealer, header.PacketNumber, payloadOffset, pnLen)
	buffer.Data = buffer.Data[:len(buffer.Data)+len(raw)]

	// RFC 9000 §14.1: a client Initial carrying CRYPTO must be at least 1200 bytes. Floor
	// with post-AEAD zero bytes (which decode as nothing outside the packet) rather than
	// the base packer's fill to the full datagram, so the datagram stays Chrome-sized.
	if len(buffer.Data) < protocol.MinInitialPacketSize {
		buffer.Data = append(buffer.Data, make([]byte, protocol.MinInitialPacketSize-len(buffer.Data))...)
	}

	if pn := p.pnManager.PopPacketNumber(protocol.EncryptionInitial); pn != header.PacketNumber {
		return nil, fmt.Errorf("packetPacker BUG: Peeked and Popped packet numbers do not match: expected %d, got %d", pn, header.PacketNumber)
	}
	return &longHeaderPacket{
		header:       header,
		ack:          pl.ack,
		frames:       pl.frames,
		streamFrames: pl.streamFrames,
		length:       protocol.ByteCount(len(raw)),
	}, nil
}
