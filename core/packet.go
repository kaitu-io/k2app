package core

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Packet framing format:
// ┌─────────────────┬──────────────────────┐
// │  Length (2 bytes)│  IP Packet (variable)│
// │  Big Endian     │  Raw IP packet        │
// └─────────────────┴──────────────────────┘

const (
	maxPacketSize = 65535 // Maximum IP packet size
	headerSize    = 2     // Length header size in bytes
)

// WritePacket writes a framed IP packet to the writer
// Format: [length:2][packet:length]
func WritePacket(w io.Writer, packet []byte) error {
	if len(packet) == 0 {
		return fmt.Errorf("empty packet")
	}
	if len(packet) > maxPacketSize {
		return fmt.Errorf("packet too large: %d bytes", len(packet))
	}

	// Write length header (2 bytes, big endian)
	header := make([]byte, headerSize)
	binary.BigEndian.PutUint16(header, uint16(len(packet)))

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	// Write packet data
	if _, err := w.Write(packet); err != nil {
		return fmt.Errorf("write packet: %w", err)
	}

	return nil
}

// ReadPacket reads a framed IP packet from the reader
// Returns the IP packet data (without length header)
func ReadPacket(r io.Reader) ([]byte, error) {
	// Read length header
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Parse length
	length := binary.BigEndian.Uint16(header)
	if length == 0 {
		return nil, fmt.Errorf("zero-length packet")
	}

	// Read packet data
	packet := make([]byte, length)
	if _, err := io.ReadFull(r, packet); err != nil {
		return nil, fmt.Errorf("read packet: %w", err)
	}

	return packet, nil
}
