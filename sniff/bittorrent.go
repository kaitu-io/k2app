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
