//go:build windows

package daemon

import (
	"encoding/binary"
	"syscall"
)

const ipUnicastIfWindows = 31

// readUnicastIfV4 reads back IP_UNICAST_IF (IPv4) from a Windows socket.
// sing's bind4 stores the ifIndex in network-byte order via a memcpy trick;
// GetsockoptInt returns the kernel-stored int as-is. Reverse the same encoding.
//
// Returns 0 if option was never set (kernel default = "use routing table").
func readUnicastIfV4(conn syscall.RawConn) (int, error) {
	var idx int
	var serr error
	ctrlErr := conn.Control(func(fd uintptr) {
		v, e := syscall.GetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, ipUnicastIfWindows)
		if e != nil {
			serr = e
			return
		}
		var bytes [4]byte
		binary.LittleEndian.PutUint32(bytes[:], uint32(v))
		idx = int(binary.BigEndian.Uint32(bytes[:]))
	})
	if ctrlErr != nil {
		return 0, ctrlErr
	}
	return idx, serr
}
