//go:build !windows

package provider

import (
	"context"
	"syscall"
	"testing"
)

// newRawFD returns a raw pipe write-end fd with no os.File wrapper,
// mirroring the dup'd TUN fd the platform hands to the provider.
func newRawFD(t *testing.T) int {
	t.Helper()
	var p [2]int
	if err := syscall.Pipe(p[:]); err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() { syscall.Close(p[0]) })
	return p[1]
}

func fdIsClosed(t *testing.T, fd int) bool {
	t.Helper()
	dup, err := syscall.Dup(fd)
	if err != nil {
		return true
	}
	syscall.Close(dup)
	return false
}

// Close before Start must release the platform-supplied fd: sing-tun never
// consumed it, so nobody else will. Leaking it keeps the platform VPN
// (Android VpnService tun) alive against a dead engine.
func TestTUNProviderClose_UnstartedClosesFD(t *testing.T) {
	fd := newRawFD(t)
	p := NewTUNProvider(ProviderConfig{
		FileDescriptor: fd,
		IPv4Address:    "10.0.0.2/24",
		IPv6Address:    "fd00::2/64",
	})
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !fdIsClosed(t, fd) {
		syscall.Close(fd)
		t.Fatal("platform fd leaked by Close before Start")
	}
}

// Second Close must not close the (already released) fd number again — the
// number may have been reused by an unrelated descriptor.
func TestTUNProviderClose_Idempotent(t *testing.T) {
	fd := newRawFD(t)
	p := NewTUNProvider(ProviderConfig{
		FileDescriptor: fd,
		IPv4Address:    "10.0.0.2/24",
		IPv6Address:    "fd00::2/64",
	})
	if err := p.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Grab the freed fd number: POSIX open returns the lowest free fd, so
	// this usually lands on the number Close just released.
	probe, err := syscall.Open("/dev/null", syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	defer syscall.Close(probe)

	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if probe == fd && fdIsClosed(t, probe) {
		t.Fatal("second Close closed a reused fd number")
	}
}

// Start failing before tun.New (bad prefix) leaves the fd unconsumed;
// the subsequent Close must release it.
func TestTUNProviderClose_AfterFailedStartClosesFD(t *testing.T) {
	fd := newRawFD(t)
	p := NewTUNProvider(ProviderConfig{
		FileDescriptor: fd,
		IPv4Address:    "not-a-prefix",
		IPv6Address:    "fd00::2/64",
	})
	if err := p.Start(context.Background(), nil); err == nil {
		t.Fatal("Start with bad IPv4 prefix should fail")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !fdIsClosed(t, fd) {
		syscall.Close(fd)
		t.Fatal("platform fd leaked after failed Start + Close")
	}
}
