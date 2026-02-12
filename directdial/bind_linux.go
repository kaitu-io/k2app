//go:build linux && !android

package directdial

import "syscall"

func (d *Dialer) control() controlFunc {
	if d.ifName == "" {
		return nil
	}
	name := d.ifName
	return func(network, address string, c syscall.RawConn) error {
		var err error
		c.Control(func(fd uintptr) {
			// Requires CAP_NET_RAW or root — k2 already runs as root for TUN.
			err = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, name)
		})
		return err
	}
}
