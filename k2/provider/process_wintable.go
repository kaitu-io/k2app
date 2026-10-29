package provider

// Windows MIB_*TABLE_OWNER_PID row layouts and their decoder. Deliberately
// NOT build-tagged (and deliberately not named *_windows.go, which would
// build-constrain it): everything here is pure byte manipulation, and keeping
// it portable lets the literal-offset layout tests run on every platform —
// including the macOS/Linux machines that actually execute this repo's test
// suite — instead of only on a Windows box. The syscalls that produce these
// buffers live in process_windows.go.

import (
	"encoding/binary"
	"fmt"
	"unsafe"
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

// tcp6RowOwnerPID mirrors MIB_TCP6ROW_OWNER_PID. Note the field order
// differs from the v4 row: State sits between RemotePort and OwningPID,
// and addresses are 16 raw bytes plus a scope ID.
type tcp6RowOwnerPID struct {
	LocalAddr     [16]byte
	LocalScopeID  uint32
	LocalPort     uint32
	RemoteAddr    [16]byte
	RemoteScopeID uint32
	RemotePort    uint32
	State         uint32
	OwningPID     uint32
}

// udp6RowOwnerPID mirrors MIB_UDP6ROW_OWNER_PID. No remote endpoint.
type udp6RowOwnerPID struct {
	LocalAddr    [16]byte
	LocalScopeID uint32
	LocalPort    uint32
	OwningPID    uint32
}

// Compile-time layout guards. parseOwnerTable reinterprets the raw kernel
// buffer as these structs via unsafe.Slice, so a field edit that changes the
// Go layout would silently mis-parse every row. Sizes are the iphlpapi.h
// struct sizes: MIB_TCPROW_OWNER_PID=24, MIB_UDPROW_OWNER_PID=12,
// MIB_TCP6ROW_OWNER_PID=56, MIB_UDP6ROW_OWNER_PID=28. Both directions
// asserted so any drift fails the build. These catch SIZE drift only — a
// same-size field reorder passes them; that failure mode is covered by the
// TestParseOwnerTable_*_LiteralOffsets tests, which decode hand-built byte
// buffers at literal offsets instead of going through the struct.
var (
	_ [24 - unsafe.Sizeof(tcpRowOwnerPID{})]byte
	_ [unsafe.Sizeof(tcpRowOwnerPID{}) - 24]byte
	_ [12 - unsafe.Sizeof(udpRowOwnerPID{})]byte
	_ [unsafe.Sizeof(udpRowOwnerPID{}) - 12]byte
	_ [56 - unsafe.Sizeof(tcp6RowOwnerPID{})]byte
	_ [unsafe.Sizeof(tcp6RowOwnerPID{}) - 56]byte
	_ [28 - unsafe.Sizeof(udp6RowOwnerPID{})]byte
	_ [unsafe.Sizeof(udp6RowOwnerPID{}) - 28]byte
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

// parseOwnerTable decodes a MIB_*TABLE_OWNER_PID buffer:
// [uint32 dwNumEntries][row × N] (rows are 4-byte aligned, so they start
// right after the count). Split from the syscall so the raw-byte decode is
// unit-testable against literal offsets, and bounds-checked so a short or
// inconsistent buffer returns an error instead of panicking or over-reading.
func parseOwnerTable[T any](buf []byte) ([]T, error) {
	if len(buf) < 4 {
		return nil, fmt.Errorf("owner table: buffer %d bytes, need at least 4", len(buf))
	}
	n := binary.LittleEndian.Uint32(buf[0:4])
	if n == 0 {
		return nil, nil
	}
	rowSize := int64(unsafe.Sizeof(*new(T)))
	if need := 4 + int64(n)*rowSize; int64(len(buf)) < need {
		return nil, fmt.Errorf("owner table: %d rows need %d bytes, have %d", n, need, len(buf))
	}
	raw := unsafe.Slice((*T)(unsafe.Pointer(&buf[4])), int(n))
	out := make([]T, n)
	copy(out, raw) // copy out of buf — buf is GC'd after return
	return out, nil
}
