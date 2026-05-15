//go:build windows

package provider

import (
	"encoding/binary"
)

// tcpRowOwnerPID mirrors MIB_TCPROW_OWNER_PID (iphlpapi.h).
// Fields stored in network byte order; use ntohs / uint32ToIPv4 to decode.
type tcpRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPID  uint32
}

// udpRowOwnerPID mirrors MIB_UDPROW_OWNER_PID. No remote endpoint.
type udpRowOwnerPID struct {
	LocalAddr uint32
	LocalPort uint32
	OwningPID uint32
}

// Constants from iphlpapi.h.
const (
	AF_INET                 = 2
	TCP_TABLE_OWNER_PID_ALL = 5
	UDP_TABLE_OWNER_PID     = 1
)

// ntohs converts the low 16 bits of n from network byte order to host order.
//
// Windows TCP/UDP table fields (dwLocalPort, dwRemotePort) store the port
// in the low 16 bits of a uint32, in network byte order. Example: port
// 54321 (0xD431 NBO bytes [D4,31]) is stored as low 16 bits of dwLocalPort
// = 0x31D4. ntohs(0x31D4) = 0xD431 = 54321.
func ntohs(n uint32) uint16 {
	v := uint16(n & 0xFFFF)
	return (v >> 8) | (v << 8)
}

// uint32ToIPv4 extracts 4 IPv4 bytes from a uint32 stored in Windows
// network byte order. Example: dwLocalAddr 0x0501A8C0 (LE uint32) means
// IP 192.168.1.5 — PutUint32(LittleEndian) writes [0xC0, 0xA8, 0x01, 0x05].
func uint32ToIPv4(n uint32) [4]byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], n)
	return b
}
