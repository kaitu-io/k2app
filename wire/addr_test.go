package wire

import (
	"net"
	"testing"
)

func TestRawAddr(t *testing.T) {
	var a net.Addr = rawAddr("example.com:443")
	if a.Network() != "udp" {
		t.Fatalf("Network() = %q, want udp", a.Network())
	}
	if a.String() != "example.com:443" {
		t.Fatalf("String() = %q, want example.com:443", a.String())
	}
}
