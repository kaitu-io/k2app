package appext

import (
	"context"
	"net"
	"sync"
	"testing"
)

func TestProtectedDialer_CallsProtect(t *testing.T) {
	var mu sync.Mutex
	var protectedFDs []int

	pd := &protectedDialer{
		protect: func(fd int) bool {
			mu.Lock()
			protectedFDs = append(protectedFDs, fd)
			mu.Unlock()
			return true
		},
	}

	// Start a temporary listener to dial into.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	conn, err := pd.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(protectedFDs) == 0 {
		t.Fatal("protect was never called")
	}
	for _, fd := range protectedFDs {
		if fd <= 0 {
			t.Errorf("unexpected fd: %d", fd)
		}
	}
}

func TestProtectedDialer_ProtectFails(t *testing.T) {
	pd := &protectedDialer{
		protect: func(fd int) bool { return false },
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	_, err = pd.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err == nil {
		t.Fatal("expected error when protect returns false")
	}
}

func TestProtectedDialer_ListenPacket(t *testing.T) {
	var called bool
	pd := &protectedDialer{
		protect: func(fd int) bool {
			called = true
			return true
		},
	}

	pc, err := pd.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pc.Close()

	if !called {
		t.Fatal("protect was never called for ListenPacket")
	}
}

