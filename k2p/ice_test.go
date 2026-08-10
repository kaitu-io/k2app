package k2p

import (
	"context"
	tls "github.com/metacubex/utls"
	"io"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
	"github.com/kaitu-io/k2/wire"
	"github.com/pion/ice/v4"
	"github.com/pion/stun/v3"
	"go.uber.org/goleak"
)

// TestICE_QUICOverLoopback is the critical PoC: two ICE agents on loopback,
// QUIC handshake + stream round-trip over ice.Conn wrapped as PacketConn.
func TestICE_QUICOverLoopback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create two ICE agents (no STUN/TURN — host candidates only on loopback).
	agentA, err := ice.NewAgentWithOptions(
		ice.WithNetworkTypes([]ice.NetworkType{ice.NetworkTypeUDP4}),
	)
	if err != nil {
		t.Fatalf("create agent A: %v", err)
	}
	defer agentA.Close()

	agentB, err := ice.NewAgentWithOptions(
		ice.WithNetworkTypes([]ice.NetworkType{ice.NetworkTypeUDP4}),
	)
	if err != nil {
		t.Fatalf("create agent B: %v", err)
	}
	defer agentB.Close()

	// Exchange candidates directly (no signaling server needed for test).
	agentA.OnCandidate(func(c ice.Candidate) {
		if c != nil {
			agentB.AddRemoteCandidate(c)
		}
	})
	agentB.OnCandidate(func(c ice.Candidate) {
		if c != nil {
			agentA.AddRemoteCandidate(c)
		}
	})

	// Gather candidates.
	if err := agentA.GatherCandidates(); err != nil {
		t.Fatalf("gather A: %v", err)
	}
	if err := agentB.GatherCandidates(); err != nil {
		t.Fatalf("gather B: %v", err)
	}

	// Get credentials.
	ufragA, pwdA, _ := agentA.GetLocalUserCredentials()
	ufragB, pwdB, _ := agentB.GetLocalUserCredentials()

	// Connect: A dials, B accepts.
	connACh := make(chan *ice.Conn, 1)
	connBCh := make(chan *ice.Conn, 1)
	errCh := make(chan error, 2)

	go func() {
		conn, err := agentA.Dial(ctx, ufragB, pwdB)
		if err != nil {
			errCh <- err
			return
		}
		connACh <- conn
	}()
	go func() {
		conn, err := agentB.Accept(ctx, ufragA, pwdA)
		if err != nil {
			errCh <- err
			return
		}
		connBCh <- conn
	}()

	var connA, connB *ice.Conn
	for i := 0; i < 2; i++ {
		select {
		case c := <-connACh:
			connA = c
		case c := <-connBCh:
			connB = c
		case err := <-errCh:
			t.Fatalf("ICE connect: %v", err)
		case <-ctx.Done():
			t.Fatal("ICE connect timeout")
		}
	}

	// Wrap as PacketConn.
	pcA := newICEPacketConn(connA, nil)
	pcB := newICEPacketConn(connB, nil)

	// QUIC: B listens, A dials.
	certPair, err := wire.GenerateCertPair()
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	trB := &quic.Transport{Conn: pcB}
	defer trB.Close()
	tlsServerCfg := &tls.Config{
		NextProtos:   []string{"h3"},
		Certificates: []tls.Certificate{certPair.EC},
	}
	quicLn, err := trB.Listen(tlsServerCfg, wire.DefaultQUICConfig())
	if err != nil {
		t.Fatalf("QUIC listen: %v", err)
	}
	defer quicLn.Close()

	// Server: accept + read stream.
	serverDone := make(chan string, 1)
	go func() {
		qConn, err := quicLn.Accept(ctx)
		if err != nil {
			return
		}
		stream, err := qConn.AcceptStream(ctx)
		if err != nil {
			return
		}
		data, _ := io.ReadAll(stream)
		serverDone <- string(data)
	}()

	// Client: dial + write stream.
	trA := &quic.Transport{Conn: pcA}
	defer trA.Close()
	tlsClientCfg := &tls.Config{
		NextProtos:         []string{"h3"},
		InsecureSkipVerify: true,
	}
	// Use synthetic addr — icePacketConn.WriteTo ignores addr.
	qConn, err := trA.Dial(ctx, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, tlsClientCfg, wire.DefaultQUICConfig())
	if err != nil {
		t.Fatalf("QUIC dial over ICE: %v", err)
	}

	stream, err := qConn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	testPayload := "hello-quic-over-ice"
	stream.Write([]byte(testPayload))
	stream.Close()

	select {
	case got := <-serverDone:
		if got != testPayload {
			t.Errorf("server received %q, want %q", got, testPayload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to receive data")
	}

	qConn.CloseWithError(0, "test done")
}

func TestNewICEAgent_NoGoroutineLeak(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)

	agent, err := NewICEAgent(Config{})
	if err != nil {
		t.Fatal(err)
	}
	agent.Close()
}

func TestDefaultSTUNURLs(t *testing.T) {
	urls := DefaultSTUNURLs()
	if len(urls) == 0 {
		t.Fatal("expected at least one STUN URL")
	}
	for _, u := range urls {
		if u.Scheme != stun.SchemeTypeSTUN {
			t.Errorf("expected STUN scheme, got %v", u.Scheme)
		}
	}
}

func TestNewICEAgent_WithTURN(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)

	agent, err := NewICEAgent(Config{
		TURNURLs:     []string{"turn:turn.example.com:3478"},
		TURNUsername: "user",
		TURNPassword: "pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	agent.Close()
}
