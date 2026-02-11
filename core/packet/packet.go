package packet

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Info contains metadata extracted from an IP packet
type Info struct {
	SrcIP    string // Source IP address
	DstIP    string // Destination IP address
	SrcPort  uint16 // Source port
	DstPort  uint16 // Destination port
	Protocol string // "tcp", "udp", or "other"
}

// Parse extracts routing metadata from an IP packet
func Parse(packet []byte) (Info, error) {
	if len(packet) < 20 {
		return Info{}, fmt.Errorf("packet too small: %d bytes", len(packet))
	}

	// Parse IPv4 header (assuming IPv4 for now)
	version := packet[0] >> 4
	if version != 4 {
		return Info{}, fmt.Errorf("unsupported IP version: %d", version)
	}

	// Extract source and destination IPs
	srcIP := fmt.Sprintf("%d.%d.%d.%d", packet[12], packet[13], packet[14], packet[15])
	dstIP := fmt.Sprintf("%d.%d.%d.%d", packet[16], packet[17], packet[18], packet[19])

	// Extract protocol
	protocol := packet[9]
	var protoName string
	switch protocol {
	case 6:
		protoName = "tcp"
	case 17:
		protoName = "udp"
	default:
		protoName = "other"
	}

	// Extract ports (for TCP/UDP)
	var srcPort, dstPort uint16
	if (protocol == 6 || protocol == 17) && len(packet) >= 24 {
		// IP header length
		ihl := int(packet[0]&0x0F) * 4
		if len(packet) >= ihl+4 {
			srcPort = uint16(packet[ihl])<<8 | uint16(packet[ihl+1])
			dstPort = uint16(packet[ihl+2])<<8 | uint16(packet[ihl+3])
		}
	}

	return Info{
		SrcIP:    srcIP,
		DstIP:    dstIP,
		SrcPort:  srcPort,
		DstPort:  dstPort,
		Protocol: protoName,
	}, nil
}

// ExtractUDPPayload extracts UDP payload from an IP packet
// Returns the UDP data (without IP and UDP headers)
func ExtractUDPPayload(pkt []byte) ([]byte, error) {
	if len(pkt) < 20 {
		return nil, fmt.Errorf("packet too small for IP header: %d bytes", len(pkt))
	}

	// Parse IP header to get header length
	ihl := int(pkt[0]&0x0F) * 4 // IP Header Length in bytes
	if ihl < 20 || ihl > len(pkt) {
		return nil, fmt.Errorf("invalid IP header length: %d", ihl)
	}

	// Verify it's UDP (protocol 17)
	protocol := pkt[9]
	if protocol != 17 {
		return nil, fmt.Errorf("not a UDP packet: protocol %d", protocol)
	}

	// Check if we have enough data for UDP header
	if len(pkt) < ihl+8 {
		return nil, fmt.Errorf("packet too small for UDP header: %d bytes", len(pkt))
	}

	// UDP header is 8 bytes, payload starts after IP + UDP headers
	udpPayloadStart := ihl + 8
	if udpPayloadStart > len(pkt) {
		return nil, fmt.Errorf("invalid UDP offset: %d", udpPayloadStart)
	}

	return pkt[udpPayloadStart:], nil
}

// BuildUDPIP constructs a UDP/IP packet
// Parameters: srcIP, dstIP (string format "1.2.3.4"), srcPort, dstPort (uint16), payload ([]byte)
func BuildUDPIP(srcIP, dstIP string, srcPort, dstPort uint16, payload []byte) ([]byte, error) {
	// Parse IP addresses
	srcAddr := parseIPv4(srcIP)
	dstAddr := parseIPv4(dstIP)
	if srcAddr == nil || dstAddr == nil {
		return nil, fmt.Errorf("invalid IP address")
	}

	udpLen := 8 + len(payload)
	totalLen := 20 + udpLen

	pkt := make([]byte, totalLen)

	// Build IP header (20 bytes)
	pkt[0] = 0x45           // Version 4, IHL 5 (20 bytes)
	pkt[1] = 0              // DSCP, ECN
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], 0) // Identification
	binary.BigEndian.PutUint16(pkt[6:8], 0) // Flags, Fragment offset
	pkt[8] = 64             // TTL
	pkt[9] = 17             // Protocol: UDP
	// Checksum will be set to 0 (optional for UDP)
	binary.BigEndian.PutUint16(pkt[10:12], 0)
	copy(pkt[12:16], srcAddr)
	copy(pkt[16:20], dstAddr)

	// Build UDP header (8 bytes)
	binary.BigEndian.PutUint16(pkt[20:22], srcPort)
	binary.BigEndian.PutUint16(pkt[22:24], dstPort)
	binary.BigEndian.PutUint16(pkt[24:26], uint16(udpLen))
	binary.BigEndian.PutUint16(pkt[26:28], 0) // UDP checksum (0 = disabled)

	// Copy payload
	copy(pkt[28:], payload)

	// Calculate IP header checksum
	checksum := calculateIPChecksum(pkt[:20])
	binary.BigEndian.PutUint16(pkt[10:12], checksum)

	return pkt, nil
}

// parseIPv4 parses an IPv4 address string into a 4-byte array
func parseIPv4(ip string) []byte {
	var a, b, c, d byte
	_, err := fmt.Sscanf(ip, "%d.%d.%d.%d", &a, &b, &c, &d)
	if err != nil {
		return nil
	}
	return []byte{a, b, c, d}
}

// calculateIPChecksum calculates the IP header checksum
func calculateIPChecksum(header []byte) uint16 {
	sum := uint32(0)
	for i := 0; i < len(header); i += 2 {
		sum += uint32(header[i])<<8 | uint32(header[i+1])
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// WriteFramed writes a length-prefixed packet to a connection
// Format: [2 bytes length][packet data]
func WriteFramed(w io.Writer, pkt []byte) error {
	if len(pkt) == 0 {
		return fmt.Errorf("cannot write zero-length packet")
	}
	if len(pkt) > 65535 {
		return fmt.Errorf("packet too large: %d bytes", len(pkt))
	}

	// Write length prefix (2 bytes, big endian)
	header := make([]byte, 2)
	binary.BigEndian.PutUint16(header, uint16(len(pkt)))

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	// Write packet data
	if _, err := w.Write(pkt); err != nil {
		return fmt.Errorf("write packet: %w", err)
	}

	return nil
}

// ReadFramed reads a length-prefixed packet from a connection
// Format: [2 bytes length][packet data]
func ReadFramed(r io.Reader) ([]byte, error) {
	// Read length prefix
	lengthBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, lengthBuf); err != nil {
		return nil, fmt.Errorf("read length: %w", err)
	}

	length := binary.BigEndian.Uint16(lengthBuf)
	if length == 0 {
		return nil, fmt.Errorf("invalid zero-length packet")
	}

	// Read packet data
	pkt := make([]byte, length)
	if _, err := io.ReadFull(r, pkt); err != nil {
		return nil, fmt.Errorf("read packet: %w", err)
	}

	return pkt, nil
}
