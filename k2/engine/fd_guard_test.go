//go:build !windows

package engine

import (
	"syscall"
	"testing"

	"github.com/kaitu-io/k2/config"
)

// newRawFD returns a raw pipe write-end fd with no os.File wrapper (no
// finalizer), mirroring how the platform hands appext a dup'd TUN fd.
// The read end is closed via t.Cleanup.
func newRawFD(t *testing.T) int {
	t.Helper()
	var p [2]int
	if err := syscall.Pipe(p[:]); err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() { syscall.Close(p[0]) })
	return p[1]
}

// fdIsClosed reports whether fd no longer refers to an open descriptor.
func fdIsClosed(t *testing.T, fd int) bool {
	t.Helper()
	dup, err := syscall.Dup(fd)
	if err != nil {
		return true
	}
	syscall.Close(dup)
	return false
}

// A failure before the provider is even built (nil Client is the earliest
// return) must still release the platform TUN fd — appext has already given
// up its cleanup responsibility when engine.Start is called. A leaked fd
// keeps the Android VPN alive routing into a dead tunnel (device blackhole).
func TestEngineStart_NilClientClosesPlatformFD(t *testing.T) {
	fd := newRawFD(t)
	e := New()
	if err := e.Start(Config{FileDescriptor: fd}); err == nil {
		t.Fatal("Start with nil Client should return error")
	}
	if !fdIsClosed(t, fd) {
		syscall.Close(fd)
		t.Fatal("platform TUN fd leaked after nil-Client failure")
	}
}

// Outbound setup failure (unparsable k2v5 URL — the path a no-network Android
// device hits) happens after the provider wraps the fd: the provider must be
// closed and the fd released.
func TestEngineStart_OutboundSetupFailureClosesPlatformFD(t *testing.T) {
	fd := newRawFD(t)
	cfg := testConfig("k2v5://udid:token@127.0.0.1:notaport")
	cfg.FileDescriptor = fd
	e := New()
	if err := e.Start(cfg); err == nil {
		t.Fatal("Start with unparsable k2v5 URL should return error")
	}
	if !fdIsClosed(t, fd) {
		syscall.Close(fd)
		t.Fatal("platform TUN fd leaked after outbound-setup failure")
	}
}

// TUN mode with zero k2v5 outbounds (route table empty) fails after provider
// creation — same ownership rule applies.
func TestEngineStart_NoOutboundClosesPlatformFD(t *testing.T) {
	fd := newRawFD(t)
	cfg := Config{
		Client:         &config.ClientConfig{Server: "garbage"},
		FileDescriptor: fd,
	}
	e := New()
	if err := e.Start(cfg); err == nil {
		t.Fatal("Start with no k2v5 outbound should return error")
	}
	if !fdIsClosed(t, fd) {
		syscall.Close(fd)
		t.Fatal("platform TUN fd leaked after no-outbound failure")
	}
}

// A double Start receives a fresh dup'd fd from the platform; rejecting the
// call still transfers ownership — the rejected fd must be closed without
// touching the running session.
func TestEngineStart_DoubleStartClosesPlatformFD(t *testing.T) {
	e := New()
	e.mu.Lock()
	e.state = StateConnected
	e.mu.Unlock()

	fd := newRawFD(t)
	cfg := testConfig("k2v5://udid:token@127.0.0.1:12345?insecure=1")
	cfg.FileDescriptor = fd
	if err := e.Start(cfg); err == nil {
		t.Fatal("double Start should return error")
	}
	if !fdIsClosed(t, fd) {
		syscall.Close(fd)
		t.Fatal("platform TUN fd leaked after double-Start rejection")
	}

	e.mu.Lock()
	e.state = StateDisconnected
	e.mu.Unlock()
}
