// Package sniff provides protocol detection by inspecting first bytes of connections.
// Detection algorithms extracted from github.com/sagernet/sing-box/common/sniff.
package sniff

import (
	"bytes"
	"encoding/binary"
	"io"
)

// IsBitTorrent checks if the TCP stream starts with a BitTorrent protocol handshake.
// Requires at least 20 bytes in the reader. Returns false on insufficient data.
// Reference: https://www.bittorrent.org/beps/bep_0003.html
func IsBitTorrent(reader io.Reader) bool {
	var first byte
	if err := binary.Read(reader, binary.BigEndian, &first); err != nil {
		return false
	}
	if first != 19 {
		return false
	}
	var protocol [19]byte
	n, _ := io.ReadFull(reader, protocol[:])
	return n == 19 && string(protocol[:]) == "BitTorrent protocol"
}

// IsUTP checks if a UDP packet is a uTP (BitTorrent transport) packet.
// Reference: https://www.bittorrent.org/beps/bep_0029.html
func IsUTP(packet []byte) bool {
	if len(packet) < 20 {
		return false
	}
	version := packet[0] & 0x0F
	ty := packet[0] >> 4
	if version != 1 || ty > 4 {
		return false
	}
	extension := packet[1]
	reader := bytes.NewReader(packet[20:])
	for extension != 0 {
		if err := binary.Read(reader, binary.BigEndian, &extension); err != nil {
			return false
		}
		if extension > 0x04 {
			return false
		}
		var length byte
		if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
			return false
		}
		if _, err := reader.Seek(int64(length), io.SeekCurrent); err != nil {
			return false
		}
	}
	return true
}

// IsUTPSyn reports whether a UDP packet is a uTP ST_SYN — the opener of every
// outgoing uTP connection. Stricter than IsUTP, which also accepts lookalikes
// such as a WireGuard handshake initiation (first byte 0x01, then zeros) and
// some QUIC short-header packets: a SYN has first byte exactly 0x41 (type 4,
// version 1), a well-formed extension chain, and no payload after it.
// Used where a false positive drops traffic (server-side blocking), not just
// reroutes it.
func IsUTPSyn(packet []byte) bool {
	if len(packet) < 20 || packet[0] != 0x41 {
		return false
	}
	rest := packet[20:]
	for ext := packet[1]; ext != 0; {
		if ext > 0x04 || len(rest) < 2 {
			return false
		}
		next, length := rest[0], int(rest[1])
		if len(rest) < 2+length {
			return false
		}
		rest = rest[2+length:]
		ext = next
	}
	return len(rest) == 0
}

// dhtSigWindow bounds how far into a packet IsDHT looks for the response
// signature: a BEP 42 response carries an "ip" entry (≤ 18 IPv6 bytes plus
// key) before "r", so the signature always sits within the first ~40 bytes.
const dhtSigWindow = 48

// IsDHT reports whether a UDP packet is a BitTorrent Mainline DHT message
// (BEP 5). KRPC messages are bencoded dicts with sorted keys, so a query
// starts "d1:ad2:id20:" and a response carries "1:rd2:id20:" right after an
// optional "ip" entry. Both signatures embed the 20-byte node id length,
// which keeps the match specific to DHT.
func IsDHT(packet []byte) bool {
	if len(packet) == 0 || packet[0] != 'd' {
		return false
	}
	if bytes.HasPrefix(packet, []byte("d1:ad2:id20:")) {
		return true
	}
	head := packet
	if len(head) > dhtSigWindow {
		head = head[:dhtSigWindow]
	}
	return bytes.Contains(head, []byte("1:rd2:id20:"))
}

// IsBitTorrentUDP reports whether a UDP packet opens BitTorrent traffic: a uTP
// SYN, a DHT message, or a UDP tracker connect. Meant for the first packet a
// session sends to a new destination.
func IsBitTorrentUDP(packet []byte) bool {
	return IsUTPSyn(packet) || IsDHT(packet) || IsUDPTracker(packet)
}

// IsUDPTracker checks if a UDP packet is a BitTorrent UDP Tracker connect request.
// Reference: https://www.bittorrent.org/beps/bep_0015.html
func IsUDPTracker(packet []byte) bool {
	if len(packet) < 16 {
		return false
	}
	if binary.BigEndian.Uint64(packet[:8]) != 0x41727101980 {
		return false
	}
	return binary.BigEndian.Uint32(packet[8:12]) == 0
}
