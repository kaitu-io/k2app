//go:build darwin

package directdial

import "syscall"

const (
	ipBoundIF   = 25  // IP_BOUND_IF
	ipv6BoundIF = 125 // IPV6_BOUND_IF
)

func (d *Dialer) control() controlFunc {
	if d.ifIdx == 0 {
		return nil // no-op if interface not detected
	}
	idx := d.ifIdx
	return func(network, address string, c syscall.RawConn) error {
		var err error
		c.Control(func(fd uintptr) {
			if isIPv6Network(network) {
				err = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, ipv6BoundIF, idx)
			} else {
				err = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, ipBoundIF, idx)
			}
		})
		return err
	}
}
