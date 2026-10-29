package provider

// parseOwnerTable — decode tests against HAND-BUILT byte buffers at LITERAL
// offsets. The mock fetchers in process_windows_test.go construct rows by
// field name, so they agree with whatever layout the Go structs happen to
// have; only these tests catch a same-size field reorder (which the
// compile-time size guards cannot). Untagged so they run on every platform.
// Offsets are the iphlpapi.h layouts:
//
//	MIB_TCP6ROW_OWNER_PID: addr@0(16) scope@16 port@20 raddr@24(16)
//	                       rscope@40 rport@44 state@48 pid@52   (56 B)
//	MIB_UDP6ROW_OWNER_PID: addr@0(16) scope@16 port@20 pid@24   (28 B)
//	MIB_TCPROW_OWNER_PID:  state@0 addr@4 port@8 raddr@12 rport@16 pid@20 (24 B)
//	MIB_UDPROW_OWNER_PID:  addr@0 port@4 pid@8                  (12 B)

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

func TestNtohs_KnownValues(t *testing.T) {
	cases := []struct {
		in   uint32
		want uint16
	}{
		{0x000031D4, 54321}, // 0xD431 in NBO
		{0x00005000, 80},    // 0x0050 in NBO
		{0x0000BB01, 443},   // 0x01BB in NBO
		{0x00000000, 0},
		{0x0000FFFF, 0xFFFF},
	}
	for _, c := range cases {
		got := ntohs(c.in)
		if got != c.want {
			t.Errorf("ntohs(0x%08X) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestUint32ToIPv4_KnownValues(t *testing.T) {
	cases := []struct {
		in   uint32
		want [4]byte
	}{
		{0x0501A8C0, [4]byte{192, 168, 1, 5}},
		{0x0100007F, [4]byte{127, 0, 0, 1}},
		{0x00000000, [4]byte{0, 0, 0, 0}},
		{0x010101D5, [4]byte{213, 1, 1, 1}}, // 213.1.1.1 ← 0xD5010101 NBO
	}
	for _, c := range cases {
		got := uint32ToIPv4(c.in)
		if got != c.want {
			t.Errorf("uint32ToIPv4(0x%08X) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseOwnerTable_TCP6_LiteralOffsets(t *testing.T) {
	buf := make([]byte, 4+56)
	binary.LittleEndian.PutUint32(buf[0:], 1) // dwNumEntries
	row := buf[4:]
	local := netip.MustParseAddr("fdfe:dcba:9876::2").As16()
	remote := netip.MustParseAddr("2001:db8::9").As16()
	copy(row[0:16], local[:])                       // ucLocalAddr
	binary.LittleEndian.PutUint32(row[16:], 3)      // dwLocalScopeId
	binary.LittleEndian.PutUint32(row[20:], 0x31D4) // dwLocalPort (54321 NBO)
	copy(row[24:40], remote[:])                     // ucRemoteAddr
	binary.LittleEndian.PutUint32(row[40:], 0)      // dwRemoteScopeId
	binary.LittleEndian.PutUint32(row[44:], 0xBB01) // dwRemotePort (443 NBO)
	binary.LittleEndian.PutUint32(row[48:], 5)      // dwState (ESTABLISHED)
	binary.LittleEndian.PutUint32(row[52:], 8000)   // dwOwningPid

	rows, err := parseOwnerTable[tcp6RowOwnerPID](buf)
	if err != nil {
		t.Fatalf("parseOwnerTable: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.LocalAddr != local {
		t.Errorf("LocalAddr = %x, want %x", r.LocalAddr, local)
	}
	if r.LocalScopeID != 3 {
		t.Errorf("LocalScopeID = %d, want 3", r.LocalScopeID)
	}
	if ntohs(r.LocalPort) != 54321 {
		t.Errorf("LocalPort = %d, want 54321", ntohs(r.LocalPort))
	}
	if r.RemoteAddr != remote {
		t.Errorf("RemoteAddr = %x, want %x", r.RemoteAddr, remote)
	}
	if ntohs(r.RemotePort) != 443 {
		t.Errorf("RemotePort = %d, want 443", ntohs(r.RemotePort))
	}
	if r.State != 5 {
		t.Errorf("State = %d, want 5", r.State)
	}
	if r.OwningPID != 8000 {
		t.Errorf("OwningPID = %d, want 8000", r.OwningPID)
	}
}

func TestParseOwnerTable_UDP6_LiteralOffsets(t *testing.T) {
	buf := make([]byte, 4+28)
	binary.LittleEndian.PutUint32(buf[0:], 1)
	row := buf[4:]
	local := netip.MustParseAddr("::ffff:192.168.1.5").As16()
	copy(row[0:16], local[:])                       // ucLocalAddr
	binary.LittleEndian.PutUint32(row[16:], 0)      // dwLocalScopeId
	binary.LittleEndian.PutUint32(row[20:], 0x31D4) // dwLocalPort
	binary.LittleEndian.PutUint32(row[24:], 9000)   // dwOwningPid

	rows, err := parseOwnerTable[udp6RowOwnerPID](buf)
	if err != nil {
		t.Fatalf("parseOwnerTable: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.LocalAddr != local || ntohs(r.LocalPort) != 54321 || r.OwningPID != 9000 {
		t.Errorf("row = %+v, want local=%x port=54321 pid=9000", r, local)
	}
}

func TestParseOwnerTable_TCP4_LiteralOffsets(t *testing.T) {
	buf := make([]byte, 4+2*24)
	binary.LittleEndian.PutUint32(buf[0:], 2)
	for i, pid := range []uint32{7777, 8888} {
		row := buf[4+i*24:]
		binary.LittleEndian.PutUint32(row[0:], 5)           // dwState
		binary.LittleEndian.PutUint32(row[4:], 0x0501A8C0)  // dwLocalAddr 192.168.1.5
		binary.LittleEndian.PutUint32(row[8:], 0x31D4)      // dwLocalPort
		binary.LittleEndian.PutUint32(row[12:], 0x0100007F) // dwRemoteAddr 127.0.0.1
		binary.LittleEndian.PutUint32(row[16:], 0xBB01)     // dwRemotePort
		binary.LittleEndian.PutUint32(row[20:], pid)        // dwOwningPid
	}
	rows, err := parseOwnerTable[tcpRowOwnerPID](buf)
	if err != nil {
		t.Fatalf("parseOwnerTable: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].State != 5 || uint32ToIPv4(rows[0].LocalAddr) != [4]byte{192, 168, 1, 5} ||
		ntohs(rows[0].LocalPort) != 54321 || rows[0].OwningPID != 7777 {
		t.Errorf("row0 = %+v", rows[0])
	}
	if rows[1].OwningPID != 8888 {
		t.Errorf("row1 pid = %d, want 8888", rows[1].OwningPID)
	}
}

func TestParseOwnerTable_UDP4_LiteralOffsets(t *testing.T) {
	buf := make([]byte, 4+12)
	binary.LittleEndian.PutUint32(buf[0:], 1)
	row := buf[4:]
	binary.LittleEndian.PutUint32(row[0:], 0x00000000) // dwLocalAddr 0.0.0.0
	binary.LittleEndian.PutUint32(row[4:], 0x31D4)     // dwLocalPort
	binary.LittleEndian.PutUint32(row[8:], 9000)       // dwOwningPid
	rows, err := parseOwnerTable[udpRowOwnerPID](buf)
	if err != nil {
		t.Fatalf("parseOwnerTable: %v", err)
	}
	if len(rows) != 1 || uint32ToIPv4(rows[0].LocalAddr) != [4]byte{0, 0, 0, 0} ||
		ntohs(rows[0].LocalPort) != 54321 || rows[0].OwningPID != 9000 {
		t.Errorf("rows = %+v", rows)
	}
}

func TestParseOwnerTable_Bounds(t *testing.T) {
	// Too short for the count.
	if _, err := parseOwnerTable[udpRowOwnerPID]([]byte{1, 0}); err == nil {
		t.Error("2-byte buffer: want error, got nil")
	}
	// Count claims 2 rows but only 1 fits.
	buf := make([]byte, 4+12)
	binary.LittleEndian.PutUint32(buf[0:], 2)
	if _, err := parseOwnerTable[udpRowOwnerPID](buf); err == nil {
		t.Error("count=2 with 1 row of space: want error, got nil")
	}
	// Zero rows is fine.
	empty := make([]byte, 4)
	if rows, err := parseOwnerTable[udpRowOwnerPID](empty); err != nil || rows != nil {
		t.Errorf("count=0: got (%v, %v), want (nil, nil)", rows, err)
	}
}
