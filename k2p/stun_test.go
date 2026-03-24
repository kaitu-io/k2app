package k2p

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/pion/stun/v3"
)

// startTestSTUNServer runs a minimal STUN server on loopback that responds
// to Binding Requests with the client's source address as XOR-MAPPED-ADDRESS.
func startTestSTUNServer(t *testing.T) (*net.UDPConn, string) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen STUN server: %v", err)
	}

	t.Cleanup(func() { conn.Close() })

	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			req := new(stun.Message)
			req.Raw = buf[:n]
			if err := req.Decode(); err != nil {
				continue
			}
			resp, err := stun.Build(
				stun.NewTransactionIDSetter(req.TransactionID),
				stun.BindingSuccess,
				&stun.XORMappedAddress{IP: from.IP, Port: from.Port},
				stun.Fingerprint,
			)
			if err != nil {
				continue
			}
			conn.WriteToUDP(resp.Raw, from)
		}
	}()

	return conn, conn.LocalAddr().String()
}

func TestSTUNProbe_ReturnsReflexiveAddr(t *testing.T) {
	_, srvAddr := startTestSTUNServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := stunProbe(ctx, srvAddr, nil)
	if err != nil {
		t.Fatalf("stunProbe: %v", err)
	}
	if result.reflexive.IP == nil {
		t.Fatal("expected non-nil reflexive IP")
	}
	if !result.reflexive.IP.IsLoopback() {
		t.Errorf("expected loopback reflexive, got %v", result.reflexive)
	}
}

func TestSTUNProbe_WithExistingConn(t *testing.T) {
	_, srvAddr := startTestSTUNServer(t)

	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := stunProbe(ctx, srvAddr, conn)
	if err != nil {
		t.Fatalf("stunProbe with conn: %v", err)
	}
	if result.reflexive == nil {
		t.Fatal("expected non-nil reflexive")
	}
}

func TestSTUNProbe_BadServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := stunProbe(ctx, "127.0.0.1:1", nil)
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}
