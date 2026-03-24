package k2p

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

func TestHome_HandleAwayConnection_Loopback(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	certPair, err := wire.GenerateCertPair()
	if err != nil {
		t.Fatal(err)
	}

	// Simulate away peer: create a UDP conn and send punch magic.
	awayConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer awayConn.Close()
	awayAddr := awayConn.LocalAddr().(*net.UDPAddr)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start handleAwayConnection in background — it will punch then listen.
	localNAT := NATResult{
		Type:      NATEasy,
		Reflexive: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0},
		Local:     &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)},
	}
	notify := PunchNotify{AwayAddr: awayAddr.String(), NATType: "easy"}

	listenerCh := make(chan wire.Listener, 1)
	errCh := make(chan error, 1)
	go func() {
		listener, err := handleAwayConnection(ctx, Config{
			TLSCert: &certPair.EC,
		}, localNAT, notify)
		if err != nil {
			errCh <- err
			return
		}
		listenerCh <- listener
	}()

	// Away side: respond to punch packets.
	go func() {
		buf := make([]byte, 64)
		for {
			n, from, readErr := awayConn.ReadFromUDP(buf)
			if readErr != nil {
				return
			}
			if n >= len(punchMagic) && string(buf[:len(punchMagic)]) == punchMagic {
				awayConn.WriteToUDP([]byte(punchMagic), from)
			}
		}
	}()

	// Wait for listener or error.
	select {
	case listener := <-listenerCh:
		if listener == nil {
			t.Fatal("expected non-nil listener")
		}
		// Verify the listener exists and can be closed cleanly.
		listener.Close()
	case err := <-errCh:
		t.Fatalf("handleAwayConnection: %v", err)
	case <-time.After(8 * time.Second):
		t.Fatal("timeout waiting for listener")
	}
}
